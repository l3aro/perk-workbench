package workbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "perk-workbench-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestQueryLog_persistsInSQLiteAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	t.Setenv("PERK_QUERY_LOG_RETENTION_DAYS", "1")
	entry := queryLogEntry{startedAt: time.Now(), statement: "SELECT current", duration: time.Millisecond, message: "completed"}
	if err := saveQueryLog(path, entry); err != nil {
		t.Fatal(err)
	}
	db, err := openQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO query_log(started_at, statement, duration, message, status) VALUES (?, ?, ?, ?, ?)`,
		time.Now().AddDate(0, 0, -2).UnixNano(), "SELECT expired", 0, "completed", "success")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := loadQueryLog(path); len(got) != 1 || got[0].statement != entry.statement {
		t.Fatalf("query log = %#v, want %#v", got, []queryLogEntry{entry})
	}
}

func TestNew_loadsPersistedQueryLog(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path, err := queryLogPath()
	if err != nil {
		t.Fatal(err)
	}
	entry := queryLogEntry{startedAt: time.Now(), statement: "SELECT persisted", duration: time.Millisecond, message: "completed"}
	if err := saveQueryLog(path, entry); err != nil {
		t.Fatal(err)
	}
	model := New("", context.Background(), testOpen)
	if got := model.queryLogEntries; len(got) != 1 || got[0].statement != entry.statement {
		t.Fatalf("loaded query log = %#v, want %#v", got, []queryLogEntry{entry})
	}
	if got := model.queryLog.Rows(); len(got) != 1 || got[0][2] != entry.statement {
		t.Fatalf("query log rows = %#v, want persisted statement", got)
	}
}

func TestQueryLogRetentionDays_defaultsToThirty(t *testing.T) {
	t.Setenv("PERK_QUERY_LOG_RETENTION_DAYS", "invalid")
	if got := queryLogRetentionDays(); got != defaultQueryLogRetentionDays {
		t.Fatalf("retention days = %d, want %d", got, defaultQueryLogRetentionDays)
	}
}

func TestQueryLogPath_usesXDGConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path, err := queryLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configDir, "perk-workbench", "data.db"); path != want {
		t.Fatalf("query log path = %q, want %q", path, want)
	}
}

func TestQueryLogPageSize_defaultsToTwentyFive(t *testing.T) {
	t.Setenv("PERK_QUERY_LOG_PAGE_SIZE", "invalid")
	if got := queryLogPageSize(); got != defaultQueryLogPageSize {
		t.Fatalf("page size = %d, want %d", got, defaultQueryLogPageSize)
	}
	t.Setenv("PERK_QUERY_LOG_PAGE_SIZE", "7")
	if got, want := queryLogPageSize(), 7; got != want {
		t.Fatalf("page size = %d, want %d", got, want)
	}
}
