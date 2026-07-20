package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenUsesReadWriteEscapedFileDSN(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "db with ? mark.sqlite")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("creating database file: %v", err)
	}

	// When
	service, err := Open(context.Background(), path)

	// Then
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if service.rawTarget != path || service.dsn == path || !strings.Contains(service.dsn, "mode=rw") || !strings.Contains(service.dsn, "%3F") {
		t.Fatalf("Open() raw target/dsn = %q/%q, want retained raw path and escaped read-write DSN", service.rawTarget, service.dsn)
	}
}

func TestOpenMissingFileDoesNotCreate(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "missing.sqlite")

	// When
	_, err := Open(context.Background(), path)

	// Then
	if err == nil {
		t.Fatal("Open() error = nil, want missing file error")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing database stat error = %v, want not exist", statErr)
	}
}

func TestOpenMySQLRejectsInvalidDSN(t *testing.T) {
	service, err := OpenMySQL(context.Background(), "not-a-mysql-dsn")
	if err == nil {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
		t.Fatal("OpenMySQL() error = nil, want invalid DSN error")
	}
}
