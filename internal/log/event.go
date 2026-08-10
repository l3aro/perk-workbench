// Package log writes notable application events to ~/.config/perk-workbench/event.log.
// File-based — survives TUI restart, no truncation in a single-line footer.
// Entries below the configured level (SetLevel, built-in default Info) are
// dropped before reaching the file or the notifier.
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

// Level is the severity of a log entry, ordered from least to most severe.
type Level uint8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the uppercase kind written to the log file.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// Title returns the display name used in notifications.
func (l Level) Title() string {
	switch l {
	case LevelDebug:
		return "Debug"
	case LevelWarn:
		return "Warning"
	case LevelError:
		return "Error"
	default:
		return "Info"
	}
}

// Entry is one logged event. Time is the instant the entry was created.
type Entry struct {
	Time    time.Time
	Level   Level
	Message string
}

var (
	mu       sync.Mutex
	notifier func(Entry)
	minLevel = LevelInfo // entries below this severity are dropped
)

// SetNotifier registers a callback invoked after each entry is written to
// the log file. The callback runs synchronously in the caller's goroutine,
// so it must be fast and must not itself call into this package. A nil
// callback disables notifications.
func SetNotifier(fn func(Entry)) {
	mu.Lock()
	notifier = fn
	mu.Unlock()
}

// SetLevel configures the minimum severity written to the event log and
// delivered to the notifier. Entries below the level are dropped entirely.
// The built-in default is Info; config.json "log_level" sets it at startup.
func SetLevel(level Level) {
	mu.Lock()
	minLevel = level
	mu.Unlock()
}

// Debug writes a timestamped debug entry. Silent on failure (best-effort).
func Debug(msg string) { write(LevelDebug, msg) }

// Info writes a timestamped info entry. Silent on failure (best-effort).
func Info(msg string) { write(LevelInfo, msg) }

// Warn writes a timestamped warning entry. Silent on failure (best-effort).
func Warn(msg string) { write(LevelWarn, msg) }

// Error writes a timestamped error entry with an operation context.
// Silent on failure (best-effort); a nil error is a no-op.
func Error(op string, err error) {
	if err == nil {
		return
	}
	write(LevelError, op+": "+err.Error())
}

// Printf writes a timestamped info entry. Silent on failure (best-effort).
func Printf(format string, args ...any) {
	Info(fmt.Sprintf(format, args...))
}

func write(level Level, msg string) {
	entry := Entry{Time: time.Now(), Level: level, Message: msg}

	mu.Lock()
	if level < minLevel {
		mu.Unlock()
		return
	}
	p, err := path()
	if err == nil {
		if e := os.MkdirAll(filepath.Dir(p), 0755); e == nil {
			if f, e := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); e == nil {
				fmt.Fprintf(f, "[%s] %s: %s\n", entry.Time.Format(time.RFC3339), level.String(), msg)
				f.Close()
			}
		}
	}
	cb := notifier
	mu.Unlock()

	if cb != nil {
		cb(entry)
	}
}

// path returns the absolute log-file path.
func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", fileName), nil
}
