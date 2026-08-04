package workbench

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const defaultQueryLogRetentionDays = 30

const defaultQueryLogPageSize = 25

func queryLogPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", "data.db"), nil
}

func queryLogRetentionDays() int {
	if days, err := strconv.Atoi(os.Getenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS")); err == nil && days >= 0 {
		return days
	}
	if appConfig.QueryLogRetentionDays > 0 {
		return appConfig.QueryLogRetentionDays
	}
	return defaultQueryLogRetentionDays
}

func queryLogPageSize() int {
	if size, err := strconv.Atoi(os.Getenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE")); err == nil && size >= 1 {
		return min(size, queryLogLimit)
	}
	if appConfig.QueryLogPageSize > 0 {
		return min(appConfig.QueryLogPageSize, queryLogLimit)
	}
	return defaultQueryLogPageSize
}

// queryLogDB returns the model's persistent query-log database, opened lazily
// on first use and reused for every save. Previously every query completion
// opened a fresh connection and re-ran the full migration/import sequence on
// the UI goroutine.
func (m *Model) queryLogDB() *sql.DB {
	if m.queryLogDatabase == nil && m.queryLogPath != "" {
		if db, err := openQueryLog(m.queryLogPath); err == nil {
			m.queryLogDatabase = db
		}
	}
	return m.queryLogDatabase
}

func loadQueryLog(path, connectionID string) []queryLogEntry {
	// Without a profile scope there is nothing safe to read: an empty scope
	// must never surface unscoped rows.
	if connectionID == "" {
		return nil
	}
	db, err := openQueryLog(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	if pruneQueryLog(db, time.Now(), connectionID) != nil {
		return nil
	}
	rows, err := db.Query(`SELECT started_at, statement, duration, message, status FROM query_log WHERE connection_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, connectionID, queryLogLimit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []queryLogEntry
	for rows.Next() {
		var startedAt, duration int64
		var entry queryLogEntry
		if rows.Scan(&startedAt, &entry.statement, &duration, &entry.message, &entry.status) != nil {
			continue
		}
		entry.startedAt = time.Unix(0, startedAt)
		entry.duration = time.Duration(duration)
		entries = append(entries, entry)
	}
	return entries
}

func saveQueryLog(path, connectionID string, entry queryLogEntry) error {
	db, err := openQueryLog(path)
	if err != nil {
		return err
	}
	defer db.Close()
	return saveQueryLogDB(db, connectionID, entry)
}

// saveQueryLogDB persists one entry through an already-open query-log
// database: prune by retention, insert, then trim to the in-memory cap.
func saveQueryLogDB(db *sql.DB, connectionID string, entry queryLogEntry) error {
	// Never persist query history without a profile scope; the caller keeps
	// the entry in memory only.
	if connectionID == "" {
		return errors.New("query log requires a connection scope")
	}
	if err := pruneQueryLog(db, time.Now(), connectionID); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO query_log(connection_id, started_at, statement, duration, message, status) VALUES (?, ?, ?, ?, ?, ?)`,
		connectionID, entry.startedAt.UnixNano(), entry.statement, int64(entry.duration), entry.message, entry.status); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM query_log WHERE id IN (
		SELECT id FROM query_log WHERE connection_id = ? ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?
	)`, connectionID, queryLogLimit)
	return err
}

func openQueryLog(path string) (*sql.DB, error) {
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
	legacyScope, err := migrateQueryLog(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`SELECT 1 FROM saved_queries LIMIT 1`); err == nil {
		// The one-time saved_queries import lands in the legacy quarantine
		// scope, never in a connection's view.
		scope := legacyScope
		if scope == "" {
			scope, err = newConnectionID()
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
	return db, nil
}

// migrateQueryLog adds the connection_id scope column to legacy tables and
// quarantines every pre-scope row behind one generated scope. It returns that
// scope so the saved_queries import can share it.
func migrateQueryLog(db *sql.DB) (string, error) {
	hasColumn, err := queryLogHasColumn(db)
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
		legacyScope, err = newConnectionID()
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

func queryLogHasColumn(db *sql.DB) (bool, error) {
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

func pruneQueryLog(db *sql.DB, now time.Time, connectionID string) error {
	days := queryLogRetentionDays()
	if days == 0 {
		_, err := db.Exec(`DELETE FROM query_log WHERE connection_id = ?`, connectionID)
		return err
	}
	_, err := db.Exec(`DELETE FROM query_log WHERE started_at < ? AND connection_id = ?`, now.AddDate(0, 0, -days).UnixNano(), connectionID)
	return err
}
