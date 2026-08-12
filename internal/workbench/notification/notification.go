// Package notification owns notification history persistence: the scoped
// SQLite store, level migration, and retention pruning. It has no Bubble
// Tea dependency; popup, history, and rendering belong to the workbench
// notification UI.
package notification

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Entry is one captured status or log notification. ID is the SQLite row
// ID when the entry was persisted for a connection scope, 0 otherwise.
// Level is notificationLevelNone (0) for status messages, or log.Level + 1
// for entries captured from the event log.
type Entry struct {
	ID          int64
	CreatedAt   time.Time
	Title       string
	Description string
	Level       int
}

// Store is a lazily opened scoped notification database shared by every
// save and load. It owns exactly one *sql.DB.
type Store struct {
	db            *sql.DB
	retentionDays int
}

// Open opens (creating and migrating if needed) the notification database
// at path. retentionDays is the resolved history window applied on each
// prune.
func Open(path string, retentionDays int) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)"}).String())
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS notifications (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		level INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, err
	}
	// Older databases predate the level column; add it so history keeps the
	// severity of logged events. Pre-existing rows stay neutral (0).
	rows, err := db.Query(`PRAGMA table_info(notifications)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	hasLevel := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil && name == "level" {
			hasLevel = true
		}
	}
	rows.Close()
	if !hasLevel {
		if _, err = db.Exec(`ALTER TABLE notifications ADD COLUMN level INTEGER NOT NULL DEFAULT 0`); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS notifications_connection_created ON notifications (connection_id, created_at DESC, id DESC)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, retentionDays: retentionDays}, nil
}

// Load returns the retained entries for one connection scope, newest
// first. An empty scope never reads unscoped rows and returns no entries.
func (s *Store) Load(connectionID string, limit int) ([]Entry, error) {
	if connectionID == "" {
		return nil, nil
	}
	if err := s.prune(time.Now(), connectionID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, created_at, title, description, level FROM notifications WHERE connection_id = ? ORDER BY created_at DESC, id DESC`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		var createdAt int64
		var entry Entry
		if rows.Scan(&entry.ID, &createdAt, &entry.Title, &entry.Description, &entry.Level) != nil {
			continue
		}
		entry.CreatedAt = time.Unix(0, createdAt)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Append persists one entry for a connection scope and returns the
// inserted row ID. It rejects an empty scope so an unscoped row is never
// written. limit matches the querylog.Store signature; notification
// history keeps every retained row, so the limit is not applied.
func (s *Store) Append(connectionID string, entry Entry, _ int) (int64, error) {
	if connectionID == "" {
		return 0, errors.New("notifications require a connection scope")
	}
	if err := s.prune(time.Now(), connectionID); err != nil {
		return 0, err
	}
	result, err := s.db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description, level) VALUES (?, ?, ?, ?, ?)`,
		connectionID, entry.CreatedAt.UnixNano(), entry.Title, entry.Description, entry.Level)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Close releases the underlying database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) prune(now time.Time, connectionID string) error {
	_, err := s.db.Exec(`DELETE FROM notifications WHERE created_at < ? AND connection_id = ?`,
		now.AddDate(0, 0, -s.retentionDays).UnixNano(), connectionID)
	return err
}
