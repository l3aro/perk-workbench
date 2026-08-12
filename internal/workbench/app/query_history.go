package app

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
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

// queryLogStore returns the model's persistent query-log store, opened
// lazily on first use and reused for every save and load.
func (m *Model) queryLogStore() *querylog.Store {
	if m.queryLog.store == nil && m.queryLog.path != "" {
		if store, err := querylog.Open(m.queryLog.path, queryLogRetentionDays()); err == nil {
			m.queryLog.store = store
		}
	}
	return m.queryLog.store
}

// loadQueryLogEntries loads the retained entries for one connection
// scope through the model's store, converting nothing: entries are the
// store's own type. A missing or failing store yields no entries.
func loadQueryLogEntries(store *querylog.Store, connectionID string) []queryLogEntry {
	entries, err := store.Load(connectionID, queryLogLimit)
	if err != nil {
		return nil
	}
	return entries
}
