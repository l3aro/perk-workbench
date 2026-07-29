// Package log writes notable application events to ~/.config/perk-workbench/event.log.
// File-based — survives TUI restart, no truncation in a single-line footer.
// Safe for concurrent calls (async Bubble Tea commands).
package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const fileName = "event.log"

var mu sync.Mutex

// path returns the absolute log-file path.
func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", fileName), nil
}

// Error writes a timestamped error entry with an operation context.
// Silent on failure (best-effort).
func Error(op string, err error) {
	if err == nil {
		return
	}
	write("ERROR", op+": "+err.Error())
}

// Printf writes a timestamped info entry. Silent on failure (best-effort).
func Printf(format string, args ...any) {
	write("INFO", fmt.Sprintf(format, args...))
}

func write(kind, msg string) {
	mu.Lock()
	defer mu.Unlock()

	p, err := path()
	if err != nil {
		return
	}
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return
	}
	f, e := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if e != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s: %s\n", time.Now().Format(time.RFC3339), kind, msg)
}
