package workbench

import (
	"database/sql"
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
	days, err := strconv.Atoi(os.Getenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS"))
	if err != nil || days < 0 {
		return defaultQueryLogRetentionDays
	}
	return days
}

func queryLogPageSize() int {
	size, err := strconv.Atoi(os.Getenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE"))
	if err != nil || size < 1 {
		return defaultQueryLogPageSize
	}
	return min(size, queryLogLimit)
}

func loadQueryLog(path string) []queryLogEntry {
	db, err := openQueryLog(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	if pruneQueryLog(db, time.Now()) != nil {
		return nil
	}
	rows, err := db.Query(`SELECT started_at, statement, duration, message, status FROM query_log ORDER BY started_at DESC, id DESC LIMIT ?`, queryLogLimit)
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

func saveQueryLog(path string, entry queryLogEntry) error {
	db, err := openQueryLog(path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := pruneQueryLog(db, time.Now()); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO query_log(started_at, statement, duration, message, status) VALUES (?, ?, ?, ?, ?)`,
		entry.startedAt.UnixNano(), entry.statement, int64(entry.duration), entry.message, entry.status); err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM query_log WHERE id IN (
		SELECT id FROM query_log ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?
	)`, queryLogLimit)
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
		started_at INTEGER NOT NULL,
		statement TEXT NOT NULL,
		duration INTEGER NOT NULL,
		message TEXT NOT NULL,
		status TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`INSERT INTO query_log(started_at, statement, duration, message, status)
		SELECT saved_at * 1000000000, statement, 0, 'saved query', 'success' FROM saved_queries`); err != nil {
		if _, tableErr := db.Exec(`SELECT 1 FROM saved_queries LIMIT 1`); tableErr == nil {
			db.Close()
			return nil, err
		}
	}
	_, _ = db.Exec(`DROP TABLE IF EXISTS saved_queries`)
	return db, nil
}

func pruneQueryLog(db *sql.DB, now time.Time) error {
	days := queryLogRetentionDays()
	if days == 0 {
		_, err := db.Exec(`DELETE FROM query_log`)
		return err
	}
	_, err := db.Exec(`DELETE FROM query_log WHERE started_at < ?`, now.AddDate(0, 0, -days).UnixNano())
	return err
}
