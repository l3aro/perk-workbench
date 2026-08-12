// Package querylog owns query-log history persistence: the scoped SQLite
// store, legacy migration, saved-queries import, and retention pruning.
// It has no Bubble Tea dependency; paging and rendering belong to the
// workbench query-log UI.
package querylog

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// Entry is one recorded query execution.
type Entry struct {
	StartedAt time.Time
	Statement string
	Duration  time.Duration
	Message   string
	Status    string // "success", "failed", "canceled"
}

// Store is a lazily opened scoped query-log database shared by every
// save and load. It owns exactly one *sql.DB.
type Store struct {
	db            *sql.DB
	retentionDays int
}

// Open opens (creating and migrating if needed) the query-log database at
// path. retentionDays is the resolved history window applied on each
// prune; 0 discards history for a scope immediately.
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
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS query_log (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL,
		statement TEXT NOT NULL,
		duration INTEGER NOT NULL,
		message TEXT NOT NULL,
		status TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	legacyScope, err := migrate(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`SELECT 1 FROM saved_queries LIMIT 1`); err == nil {
		// The one-time saved_queries import lands in the legacy quarantine
		// scope, never in a connection's view.
		scope := legacyScope
		if scope == "" {
			scope, err = profile.NewID()
			if err != nil {
				db.Close()
				return nil, err
			}
		}
		if _, err = db.Exec(`INSERT INTO query_log(connection_id, started_at, statement, duration, message, status)
			SELECT ?, saved_at * 1000000000, statement, 0, 'saved query', 'success' FROM saved_queries`, scope); err != nil {
			db.Close()
			return nil, err
		}
	}
	_, _ = db.Exec(`DROP TABLE IF EXISTS saved_queries`)
	return &Store{db: db, retentionDays: retentionDays}, nil
}

// Load returns the retained entries for one connection scope, newest
// first. An empty scope never reads unscoped rows and returns no entries.
func (s *Store) Load(connectionID string, limit int) ([]Entry, error) {
	// Without a profile scope there is nothing safe to read: an empty scope
	// must never surface unscoped rows.
	if connectionID == "" {
		return nil, nil
	}
	if err := s.prune(time.Now(), connectionID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT started_at, statement, duration, message, status FROM query_log WHERE connection_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, connectionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		var startedAt, duration int64
		var entry Entry
		if rows.Scan(&startedAt, &entry.Statement, &duration, &entry.Message, &entry.Status) != nil {
			continue
		}
		entry.StartedAt = time.Unix(0, startedAt)
		entry.Duration = time.Duration(duration)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Append persists one entry for a connection scope: prune by retention,
// insert, then trim to the limit. It rejects an empty scope so an
// unscoped row is never written.
func (s *Store) Append(connectionID string, entry Entry, limit int) error {
	if connectionID == "" {
		return errors.New("query log requires a connection scope")
	}
	if err := s.prune(time.Now(), connectionID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO query_log(connection_id, started_at, statement, duration, message, status) VALUES (?, ?, ?, ?, ?, ?)`,
		connectionID, entry.StartedAt.UnixNano(), entry.Statement, int64(entry.Duration), entry.Message, entry.Status); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM query_log WHERE id IN (
		SELECT id FROM query_log WHERE connection_id = ? ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?
	)`, connectionID, limit)
	return err
}

// Close releases the underlying database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) prune(now time.Time, connectionID string) error {
	if s.retentionDays == 0 {
		_, err := s.db.Exec(`DELETE FROM query_log WHERE connection_id = ?`, connectionID)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM query_log WHERE started_at < ? AND connection_id = ?`, now.AddDate(0, 0, -s.retentionDays).UnixNano(), connectionID)
	return err
}

// migrate adds the connection_id scope column to legacy tables and
// quarantines every pre-scope row behind one generated scope. It returns
// that scope so the saved_queries import can share it.
func migrate(db *sql.DB) (string, error) {
	hasColumn, err := hasColumn(db)
	if err != nil {
		return "", err
	}
	if !hasColumn {
		if _, err := db.Exec(`ALTER TABLE query_log ADD COLUMN connection_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return "", err
		}
	}
	var legacy int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_log WHERE connection_id = ''`).Scan(&legacy); err != nil {
		return "", err
	}
	legacyScope := ""
	if legacy > 0 {
		legacyScope, err = profile.NewID()
		if err != nil {
			return "", err
		}
		if _, err := db.Exec(`UPDATE query_log SET connection_id = ? WHERE connection_id = ''`, legacyScope); err != nil {
			return "", err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS query_log_connection_started ON query_log (connection_id, started_at DESC, id DESC)`)
	return legacyScope, err
}

func hasColumn(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(query_log)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == "connection_id" {
			return true, nil
		}
	}
	return false, rows.Err()
}
