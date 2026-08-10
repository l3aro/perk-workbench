package log

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLevels_writeTheirKind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	SetLevel(LevelDebug)
	t.Cleanup(func() { SetLevel(LevelInfo) })

	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("op", os.ErrNotExist)
	Printf("printf %d", 7)

	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	for _, want := range []string{"DEBUG: debug message", "INFO: info message", "WARN: warn message", "ERROR: op: file does not exist", "INFO: printf 7"} {
		if !strings.Contains(content, want) {
			t.Fatalf("event.log = %q, want it to contain %q", content, want)
		}
	}
	if strings.Contains(content, "INFO: op:") {
		t.Fatalf("event.log = %q, Error must not be written as INFO", content)
	}
}

func TestLevels_metadata(t *testing.T) {
	for _, tc := range []struct {
		level Level
		kind  string
		title string
	}{
		{LevelDebug, "DEBUG", "Debug"},
		{LevelInfo, "INFO", "Info"},
		{LevelWarn, "WARN", "Warning"},
		{LevelError, "ERROR", "Error"},
	} {
		if got := tc.level.String(); got != tc.kind {
			t.Fatalf("Level(%d).String() = %q, want %q", tc.level, got, tc.kind)
		}
		if got := tc.level.Title(); got != tc.title {
			t.Fatalf("Level(%d).Title() = %q, want %q", tc.level, got, tc.title)
		}
	}
}

func TestNotifier_receivesWrittenEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	SetLevel(LevelDebug)
	t.Cleanup(func() { SetLevel(LevelInfo) })

	var got []Entry
	SetNotifier(func(entry Entry) {
		got = append(got, entry)
	})
	t.Cleanup(func() { SetNotifier(nil) })

	Debug("debug message")
	Warn("warn message")
	Error("op", os.ErrNotExist)

	if len(got) != 3 {
		t.Fatalf("notifier received %d entries, want 3", len(got))
	}
	if got[0].Level != LevelDebug || got[0].Message != "debug message" {
		t.Fatalf("entry 0 = %#v, want LevelDebug debug message", got[0])
	}
	if got[1].Level != LevelWarn || got[1].Message != "warn message" {
		t.Fatalf("entry 1 = %#v, want LevelWarn warn message", got[1])
	}
	if got[2].Level != LevelError || got[2].Message != "op: file does not exist" {
		t.Fatalf("entry 2 = %#v, want LevelError op context", got[2])
	}
	for _, entry := range got {
		if entry.Time.IsZero() {
			t.Fatalf("entry %#v has no timestamp", entry)
		}
	}
}

func TestNotifier_nilErrorSkipsEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	called := false
	SetNotifier(func(Entry) { called = true })
	t.Cleanup(func() { SetNotifier(nil) })

	Error("nil test", nil)

	if called {
		t.Fatal("notifier called for a nil error")
	}
	if _, err := os.Stat(filepath.Join(dir, "perk-workbench", "event.log")); err == nil {
		t.Fatal("file created for nil error")
	}
}

func TestWritesConcurrently(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	SetLevel(LevelDebug)
	t.Cleanup(func() { SetLevel(LevelInfo) })

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			Error("test", os.ErrNotExist)
			Printf("iteration %d", n)
			Debug("debug iteration")
		}(i)
	}
	wg.Wait()

	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("log file is empty")
	}
	if lines := strings.Count(string(b), "\n"); lines != 60 {
		t.Fatalf("event.log has %d lines, want 60 (20 error + 20 info + 20 debug)", lines)
	}
}

func TestErrorNilIsNoop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	Error("nil test", nil)

	if _, err := os.Stat(filepath.Join(dir, "perk-workbench", "event.log")); err == nil {
		t.Fatal("file created for nil error")
	}
}

func TestDefaultLevel_dropsDebug(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	Debug("debug message")
	Info("info message")

	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "INFO: info message") {
		t.Fatalf("event.log = %q, want the INFO line", content)
	}
	if strings.Contains(content, "DEBUG") {
		t.Fatalf("event.log = %q, must not contain the DEBUG line at the default level", content)
	}
}

func TestSetLevel_filtersBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	SetLevel(LevelWarn)
	t.Cleanup(func() { SetLevel(LevelInfo) })

	var got []Entry
	SetNotifier(func(entry Entry) { got = append(got, entry) })
	t.Cleanup(func() { SetNotifier(nil) })

	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("op", os.ErrNotExist)

	if len(got) != 2 {
		t.Fatalf("notifier received %d entries, want 2 (warn + error)", len(got))
	}
	if got[0].Level != LevelWarn || got[1].Level != LevelError {
		t.Fatalf("notified levels = %v, %v, want warn then error", got[0].Level, got[1].Level)
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	for _, want := range []string{"WARN: warn message", "ERROR: op: file does not exist"} {
		if !strings.Contains(content, want) {
			t.Fatalf("event.log = %q, want it to contain %q", content, want)
		}
	}
	for _, absent := range []string{"DEBUG:", "INFO: info message"} {
		if strings.Contains(content, absent) {
			t.Fatalf("event.log = %q, must not contain %q", content, absent)
		}
	}
}
