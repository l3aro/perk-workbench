package sqlite

import (
	"context"
	"testing"
)

func TestServiceRejects(t *testing.T) {
	// Given
	service := newMemoryService(t)
	if _, err := service.Execute(context.Background(), "CREATE TABLE guard (value INTEGER)"); err != nil {
		t.Fatalf("creating guard table: %v", err)
	}

	tests := []struct {
		name      string
		statement string
	}{
		{"empty", " \t\n "},
		{"comments only", "-- comment\n/* comment */"},
		{"multiple statements", "INSERT INTO guard VALUES (1); INSERT INTO guard VALUES (2)"},
		{"tokens after semicolon", "SELECT 1; SELECT 2"},
		{"second semicolon", "SELECT 1;;"},
		{"trigger", "CREATE TRIGGER guard_insert AFTER INSERT ON guard BEGIN SELECT 1; END"},
		{"temporary trigger", "CREATE TEMP TRIGGER guard_insert AFTER INSERT ON guard BEGIN SELECT 1; END"},
		{"or replace trigger", "CREATE OR REPLACE TRIGGER guard_insert AFTER INSERT ON guard BEGIN SELECT 1; END"},
		{"if not exists trigger", "CREATE IF NOT EXISTS TRIGGER guard_insert AFTER INSERT ON guard BEGIN SELECT 1; END"},
		{"malformed sql", "SELEC 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			_, err := service.Execute(context.Background(), test.statement)

			// Then
			if err == nil {
				t.Fatalf("Execute(%q) error = nil, want error", test.statement)
			}
		})
	}

	for _, statement := range []string{
		"SELECT 'single;quote'",
		"SELECT 'doubled '' quote; value'",
		"SELECT /* ; */ 1",
		"SELECT -- ;\n1",
		"CREATE TABLE [semi;colon] (value INTEGER)",
		"CREATE TABLE `tick``;name` (value INTEGER)",
		"CREATE TABLE \"quote\"\";name\" (value INTEGER)",
		"CREATE INDEX guard_idx ON guard (value)",
	} {
		if _, err := service.Execute(context.Background(), statement); err != nil {
			t.Fatalf("Execute(%q) error = %v, want semicolon accepted", statement, err)
		}
	}

	result, err := service.Execute(context.Background(), "SELECT count(*) FROM guard")
	if err != nil {
		t.Fatalf("checking rejected script: %v", err)
	}
	if got := *result.Rows[0][0]; got != "0" {
		t.Fatalf("rejected script inserted %s rows, want 0", got)
	}
}

func TestServiceValidate(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("creating fixture table: %v", err)
	}

	// Then — prepared against the real schema, never executed.
	for _, statement := range []string{
		"SELECT 1",
		"SELECT name FROM items",
		"INSERT INTO items (name) VALUES ('x')",
		"CREATE TABLE other (id INTEGER)",
	} {
		if err := service.Validate(ctx, statement); err != nil {
			t.Errorf("Validate(%q) error = %v, want nil", statement, err)
		}
	}
	if count, err := service.Execute(ctx, "SELECT count(*) FROM items"); err != nil || *count.Rows[0][0] != "0" {
		t.Fatalf("Validate mutated data: rows = %v, err = %v", count, err)
	}

	for _, statement := range []string{
		"SELECT * FROM missing_table",
		"SELECT missing_column FROM items",
		"SELEC 1",
		"CREATE TRIGGER items_insert AFTER INSERT ON items BEGIN SELECT 1; END",
		" ",
	} {
		if err := service.Validate(ctx, statement); err == nil {
			t.Errorf("Validate(%q) error = nil, want error", statement)
		}
	}
}
