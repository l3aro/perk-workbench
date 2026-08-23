package sqlite

import (
	"context"
	"testing"
)

func BenchmarkSQLiteExecute(b *testing.B) {
	b.Run("SELECT", func(b *testing.B) {
		b.ReportAllocs()
		service := benchmarkSQLiteExecuteService(b,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)",
			"INSERT INTO items (value) VALUES (1), (2), (3), (4)",
		)
		ctx := context.Background()
		const statement = "SELECT id, value FROM items ORDER BY id"
		for b.Loop() {
			if _, err := service.Execute(ctx, statement); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("DML", func(b *testing.B) {
		b.ReportAllocs()
		service := benchmarkSQLiteExecuteService(b,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)",
			"INSERT INTO items (id, value) VALUES (1, 0)",
		)
		ctx := context.Background()
		const statement = "UPDATE items SET value = CASE value WHEN 0 THEN 1 ELSE 0 END WHERE id = 1"
		for b.Loop() {
			if _, err := service.Execute(ctx, statement); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("DML_RETURNING", func(b *testing.B) {
		b.ReportAllocs()
		service := benchmarkSQLiteExecuteService(b,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)",
			"INSERT INTO items (id, value) VALUES (1, 0)",
		)
		ctx := context.Background()
		const statement = "UPDATE items SET value = CASE value WHEN 0 THEN 1 ELSE 0 END WHERE id = 1 RETURNING id, value"
		for b.Loop() {
			if _, err := service.Execute(ctx, statement); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("TRIGGER", func(b *testing.B) {
		b.ReportAllocs()
		service := benchmarkSQLiteExecuteService(b,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, value INTEGER NOT NULL, touched INTEGER NOT NULL DEFAULT 0)",
			"INSERT INTO items (id, value) VALUES (1, 0)",
		)
		if _, err := service.db.ExecContext(context.Background(), "CREATE TRIGGER items_touch AFTER UPDATE OF value ON items BEGIN UPDATE items SET touched = 1 WHERE id = NEW.id; END"); err != nil {
			b.Fatalf("setup trigger: %v", err)
		}
		ctx := context.Background()
		const statement = "UPDATE items SET value = CASE value WHEN 0 THEN 1 ELSE 0 END WHERE id = 1"
		for b.Loop() {
			if _, err := service.Execute(ctx, statement); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("DDL", func(b *testing.B) {
		b.ReportAllocs()
		service := benchmarkSQLiteExecuteService(b,
			"CREATE TABLE items (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)",
			"INSERT INTO items (id, value) VALUES (1, 0)",
		)
		ctx := context.Background()
		const statement = "CREATE INDEX IF NOT EXISTS items_value_idx ON items(value)"
		for b.Loop() {
			if _, err := service.Execute(ctx, statement); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkSQLiteExecuteService(b *testing.B, setup ...string) *Service {
	b.Helper()
	service, err := Open(context.Background(), ":memory:")
	if err != nil {
		b.Fatal(err)
	}
	for _, statement := range setup {
		if _, err := service.Execute(context.Background(), statement); err != nil {
			_ = service.Close()
			b.Fatalf("setup %q: %v", statement, err)
		}
	}
	b.Cleanup(func() {
		if err := service.Close(); err != nil {
			b.Errorf("closing SQLite benchmark service: %v", err)
		}
	})
	return service
}
