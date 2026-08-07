package workbench

import (
	"context"
	"database/sql"
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
	// Tree-marker tests assert the geometric fallback glyphs, so the suite
	// runs with Nerd Font icons off; the Nerd Font path is covered by its
	// own test that opts back in.
	SetAppConfig(Config{NerdFont: boolPtr(false)})
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestQueryLog_persistsInSQLiteAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS", "1")
	entry := queryLogEntry{startedAt: time.Now(), statement: "SELECT current", duration: time.Millisecond, message: "completed"}
	if err := saveQueryLog(path, "conn-a", entry); err != nil {
		t.Fatal(err)
	}
	db, err := openQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO query_log(connection_id, started_at, statement, duration, message, status) VALUES (?, ?, ?, ?, ?, ?)`,
		"conn-a", time.Now().AddDate(0, 0, -2).UnixNano(), "SELECT expired", 0, "completed", "success")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := loadQueryLog(path, "conn-a"); len(got) != 1 || got[0].statement != entry.statement {
		t.Fatalf("query log = %#v, want %#v", got, []queryLogEntry{entry})
	}
}

func TestQueryLog_scopesEntriesByConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	aEntry := queryLogEntry{startedAt: time.Now(), statement: "SELECT for A", duration: time.Millisecond, message: "completed"}
	bEntry := queryLogEntry{startedAt: time.Now(), statement: "SELECT for B", duration: time.Millisecond, message: "completed"}
	if err := saveQueryLog(path, "conn-a", aEntry); err != nil {
		t.Fatal(err)
	}
	if err := saveQueryLog(path, "conn-b", bEntry); err != nil {
		t.Fatal(err)
	}
	if err := saveQueryLog(path, "conn-a", queryLogEntry{startedAt: time.Now(), statement: "SELECT for A again", duration: time.Millisecond, message: "completed"}); err != nil {
		t.Fatal(err)
	}

	gotA := loadQueryLog(path, "conn-a")
	if len(gotA) != 2 || gotA[0].statement != "SELECT for A again" || gotA[1].statement != "SELECT for A" {
		t.Fatalf("scope conn-a entries = %#v, want the two A entries newest first", gotA)
	}
	gotB := loadQueryLog(path, "conn-b")
	if len(gotB) != 1 || gotB[0].statement != "SELECT for B" {
		t.Fatalf("scope conn-b entries = %#v, want only B's entry", gotB)
	}
	if got := loadQueryLog(path, ""); len(got) != 0 {
		t.Fatalf("empty scope entries = %#v, want none", got)
	}
}

func TestQueryLog_migratesLegacyRowsIntoGeneratedScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the pre-scope schema: query_log without connection_id.
	if _, err := db.Exec(`CREATE TABLE query_log (id INTEGER PRIMARY KEY, started_at INTEGER NOT NULL, statement TEXT NOT NULL, duration INTEGER NOT NULL, message TEXT NOT NULL, status TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO query_log (started_at, statement, duration, message, status) VALUES (?, 'SELECT legacy', 0, 'completed', 'success')`, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// A fresh connection scope must not see the legacy row.
	if got := loadQueryLog(path, "fresh-scope"); len(got) != 0 {
		t.Fatalf("fresh scope entries = %#v, want none", got)
	}

	// The legacy row landed in exactly one nonempty generated scope.
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT DISTINCT connection_id FROM query_log`)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0] == "" {
		t.Fatalf("legacy scopes = %#v, want exactly one nonempty scope", scopes)
	}
	if got := loadQueryLog(path, scopes[0]); len(got) != 1 || got[0].statement != "SELECT legacy" {
		t.Fatalf("legacy scope entries = %#v, want the legacy statement", got)
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
	if err := saveQueryLog(path, "conn-a", entry); err != nil {
		t.Fatal(err)
	}
	model := New("", context.Background(), testOpen, false)
	// No connection is open yet, so the persisted entries must not surface.
	if got := model.queryLogEntries; len(got) != 0 {
		t.Fatalf("query log before a connection = %#v, want none", got)
	}
	if got := model.queryLog.Rows(); len(got) != 0 {
		t.Fatalf("query log rows before a connection = %#v, want none", got)
	}
	// The scoped load (what updateOpen runs after recordConnection) shows them.
	model.connectionID = "conn-a"
	model.queryLogEntries = loadQueryLog(model.queryLogPath, model.connectionID)
	model.renderQueryLog()
	if got := model.queryLogEntries; len(got) != 1 || got[0].statement != entry.statement {
		t.Fatalf("loaded query log = %#v, want %#v", got, []queryLogEntry{entry})
	}
	if got := model.queryLog.Rows(); len(got) != 1 || got[0][2] != entry.statement {
		t.Fatalf("query log rows = %#v, want persisted statement", got)
	}
}

func TestQueryLogRetentionDays_defaultsToThirty(t *testing.T) {
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS", "invalid")
	if got := queryLogRetentionDays(); got != defaultQueryLogRetentionDays {
		t.Fatalf("retention days = %d, want %d", got, defaultQueryLogRetentionDays)
	}
}

func TestQueryLog_sharedDBWaitsForConcurrentWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	entry := queryLogEntry{startedAt: time.Now(), statement: "SELECT concurrent", duration: time.Millisecond, message: "completed"}
	blocker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	conn, err := blocker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	saved := make(chan error, 1)
	go func() {
		close(started)
		saved <- saveQueryLog(path, "conn-a", entry)
	}()
	<-started
	time.Sleep(200 * time.Millisecond) // let the save block on the write lock
	if _, err := conn.ExecContext(context.Background(), `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err := <-saved; err != nil {
		t.Fatalf("saveQueryLog failed under concurrent writer: %v", err)
	}
	if got := loadQueryLog(path, "conn-a"); len(got) != 1 || got[0].statement != entry.statement {
		t.Fatalf("query log = %#v, want concurrent entry", got)
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
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE", "invalid")
	if got := queryLogPageSize(); got != defaultQueryLogPageSize {
		t.Fatalf("page size = %d, want %d", got, defaultQueryLogPageSize)
	}
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE", "7")
	if got, want := queryLogPageSize(), 7; got != want {
		t.Fatalf("page size = %d, want %d", got, want)
	}
}
