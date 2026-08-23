package app

import (
	"os"
	"path/filepath"
	"strconv"
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
