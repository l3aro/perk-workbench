package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
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
	// own test that opts back in. Notification popup dismissals are
	// immediate in tests so status-changing commands (which batch the
	// dismiss tick) never block command-driving helpers on the real
	// 10-second timer.
	// Notification popup dismissals are immediate in tests so
	// status-changing commands (which batch the dismiss tick) never block
	// command-driving helpers on the real 10-second timer.
	notification.DismissTick = func(generation uint64, _ time.Duration) tea.Cmd {
		return func() tea.Msg { return notification.DismissMsg{Generation: generation} }
	}
	SetAppConfig(Config{NerdFont: boolPtr(false)})
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestNew_loadsPersistedQueryLog(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path, err := queryLogPath()
	if err != nil {
		t.Fatal(err)
	}
	entry := queryLogEntry{StartedAt: time.Now(), Statement: "SELECT persisted", Duration: time.Millisecond, Message: "completed", Status: "success"}
	store, err := querylog.Open(path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append("conn-a", entry, queryLogLimit); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	model := New("", context.Background(), testOpen, false)
	// No connection is open yet, so the persisted entries must not surface.
	if got := model.queryLog.component.Entries; len(got) != 0 {
		t.Fatalf("query log before a connection = %#v, want none", got)
	}
	if got := model.queryLog.component.Table.Rows(); len(got) != 0 {
		t.Fatalf("query log rows before a connection = %#v, want none", got)
	}
	// The scoped load (what updateOpen runs after recordConnection) shows them.
	model.connectionID = "conn-a"
	store, err = querylog.Open(path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	model.queryLog.component.Entries, err = store.Load(model.connectionID, queryLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	model.queryLog.component.Render()
	if got := model.queryLog.component.Entries; len(got) != 1 || got[0].Statement != entry.Statement {
		t.Fatalf("loaded query log = %#v, want %#v", got, []queryLogEntry{entry})
	}
	if got := model.queryLog.component.Table.Rows(); len(got) != 1 || got[0][2] != entry.Statement {
		t.Fatalf("query log rows = %#v, want persisted statement", got)
	}
}

func TestQueryLogRetentionDays_defaultsToThirty(t *testing.T) {
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS", "invalid")
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
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE", "invalid")
	if got := queryLogPageSize(); got != defaultQueryLogPageSize {
		t.Fatalf("page size = %d, want %d", got, defaultQueryLogPageSize)
	}
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE", "7")
	if got, want := queryLogPageSize(), 7; got != want {
		t.Fatalf("page size = %d, want %d", got, want)
	}
}
