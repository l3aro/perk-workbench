package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	modernsqlite "modernc.org/sqlite"
)

func TestServiceExecuteInsertReturning(t *testing.T) {
	// Given
	service, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if _, err := service.Execute(context.Background(), "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}

	// When
	result, err := service.Execute(context.Background(), "INSERT INTO items (name) VALUES ('first') RETURNING id, name")

	// Then
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("Execute() rows = %d, want 1", len(result.Rows))
	}
	if result.RowsAffected != 1 {
		t.Fatalf("Execute() rows affected = %d, want 1", result.RowsAffected)
	}
}

func TestServiceExecute(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	for _, statement := range []string{
		"CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)",
		"INSERT INTO items (name) VALUES ('one'), ('two')",
	} {
		if _, err := service.Execute(ctx, statement); err != nil {
			t.Fatalf("setup Execute(%q) error = %v", statement, err)
		}
	}

	tests := []struct {
		name         string
		statement    string
		wantColumns  []string
		wantRows     int
		wantAffected int64
	}{
		{"select nullable cells", "SELECT id, name, NULL AS absent FROM items ORDER BY id", []string{"id", "name", "absent"}, 2, 0},
		{"cte select", "WITH ids AS (SELECT id FROM items) SELECT id FROM ids ORDER BY id", []string{"id"}, 2, 0},
		{"cte dml", "WITH value(name) AS (SELECT 'three') INSERT INTO items (name) SELECT name FROM value RETURNING id, name", []string{"id", "name"}, 1, 1},
		{"read pragma", "PRAGMA user_version", []string{"user_version"}, 1, 0},
		{"write pragma", "PRAGMA user_version = 7", nil, 0, 0},
		{"comment prefix", "-- query comment\nSELECT 1 AS value", []string{"value"}, 1, 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			result, err := service.Execute(ctx, test.statement)

			// Then
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !slicesEqual(result.Columns, test.wantColumns) {
				t.Fatalf("Execute() columns = %q, want %q", result.Columns, test.wantColumns)
			}
			if len(result.Rows) != test.wantRows {
				t.Fatalf("Execute() rows = %d, want %d", len(result.Rows), test.wantRows)
			}
			if result.RowsAffected != test.wantAffected {
				t.Fatalf("Execute() rows affected = %d, want %d", result.RowsAffected, test.wantAffected)
			}
			if result.Duration <= 0 {
				t.Fatalf("Execute() duration = %v, want positive", result.Duration)
			}
		})
	}

	result, err := service.Execute(ctx, "SELECT id, name, NULL AS absent FROM items ORDER BY id LIMIT 1")
	if err != nil {
		t.Fatalf("reading nullable cell: %v", err)
	}
	if result.Rows[0][2] != nil {
		t.Fatalf("NULL cell = %q, want nil", *result.Rows[0][2])
	}

	// When
	result, err = service.Execute(ctx, "WITH RECURSIVE n(value) AS (VALUES(1) UNION ALL SELECT value + 1 FROM n WHERE value < 501) SELECT value FROM n")

	// Then
	if err != nil {
		t.Fatalf("executing bounded query: %v", err)
	}
	if len(result.Rows) != maxRows || !result.Truncated {
		t.Fatalf("bounded query rows/truncated = %d/%t, want %d/true", len(result.Rows), result.Truncated, maxRows)
	}

	result, err = service.Execute(ctx, "UPDATE items SET name = 'updated' WHERE id = 1")
	if err != nil {
		t.Fatalf("updating row: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("UPDATE rows affected = %d, want 1", result.RowsAffected)
	}
	result, err = service.Execute(ctx, "CREATE TABLE after_dml (id INTEGER)")
	if err != nil {
		t.Fatalf("creating table after DML: %v", err)
	}
	if result.RowsAffected != 0 {
		t.Fatalf("DDL rows affected = %d, want 0", result.RowsAffected)
	}
}

func TestServiceExecuteCancellation(t *testing.T) {
	// Given
	entered := make(chan struct{})
	var enteredOnce sync.Once
	functionName := fmt.Sprintf("test_tick_%d", time.Now().UnixNano())
	if err := modernsqlite.RegisterScalarFunction(functionName, 1, func(_ *modernsqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		enteredOnce.Do(func() { close(entered) })
		return args[0], nil
	}); err != nil {
		t.Fatalf("registering scalar function: %v", err)
	}
	service := newMemoryService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)

	// When
	go func() {
		_, err := service.Execute(ctx, fmt.Sprintf("WITH RECURSIVE x(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM x WHERE n<100000000) SELECT sum(%s(n)) FROM x", functionName))
		errs <- err
	}()
	select {
	case <-entered:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("scalar function was not entered")
	}

	// Then
	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled query did not return")
	}
}

func TestReturnsRows(t *testing.T) {
	for statement, want := range map[string]bool{
		"SELECT 1":                                 true,
		"-- query comment\nSELECT 1":               true,
		"/* query comment */ SELECT 1":             true,
		"SHOW TABLES":                              true,
		"DESCRIBE items":                           true,
		"EXPLAIN SELECT 1":                         true,
		"WITH ids AS (SELECT 1) SELECT * FROM ids": true,
		"INSERT INTO items VALUES (1)":             false,
		"UPDATE items SET name = 'x'":              false,
	} {
		if got := returnsRows(statement); got != want {
			t.Errorf("returnsRows(%q) = %t, want %t", statement, got, want)
		}
	}
}
