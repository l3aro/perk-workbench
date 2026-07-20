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
