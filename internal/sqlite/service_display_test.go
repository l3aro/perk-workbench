package sqlite

import (
	"context"
	"testing"
)

func TestServiceExecuteDisplayCells(t *testing.T) {
	// Given
	service := newMemoryService(t)

	// When
	result, err := service.Execute(context.Background(), "SELECT char(27) || '[31mred' || char(27) || '[0m' || char(13) || char(10) || 'blue' AS label")

	// Then
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := *result.Rows[0][0]; got != "red blue" {
		t.Fatalf("display cell = %q, want %q", got, "red blue")
	}

	result, err = service.Execute(context.Background(), "SELECT printf('%.*c', 301, 'x')")
	if err != nil {
		t.Fatalf("executing long cell: %v", err)
	}
	if got := len([]rune(*result.Rows[0][0])); got != maxRunes {
		t.Fatalf("display cell rune count = %d, want %d", got, maxRunes)
	}
}

func TestSanitizeDisplay(t *testing.T) {
	// Given
	input := "\x1b]8;;https://example.test\aopen\x1b]8;;\a\x00\r\ntext"

	// When
	got := SanitizeDisplay(input)

	// Then
	if got != "open text" {
		t.Fatalf("SanitizeDisplay() = %q, want %q", got, "open text")
	}
}

func TestDisplayRowBytes(t *testing.T) {
	row := displayRow([]any{[]byte("Mur"), nil})
	if got := *row[0]; got != "Mur" {
		t.Fatalf("display row byte cell = %q, want %q", got, "Mur")
	}
	if row[1] != nil {
		t.Fatalf("display row NULL cell = %q, want nil", *row[1])
	}
}

func TestListSchema(t *testing.T) {
	// Given
	service := newMemoryService(t)
	for _, statement := range []string{
		"CREATE TABLE zebra (id INTEGER)",
		"CREATE TABLE alpha (id INTEGER)",
		"CREATE VIEW visible AS SELECT id FROM alpha",
	} {
		if _, err := service.Execute(context.Background(), statement); err != nil {
			t.Fatalf("setup Execute(%q) error = %v", statement, err)
		}
	}

	// When
	objects, err := service.ListSchema(context.Background())

	// Then
	if err != nil {
		t.Fatalf("ListSchema() error = %v", err)
	}
	want := []SchemaObject{{Type: "table", Name: "alpha"}, {Type: "table", Name: "zebra"}, {Type: "view", Name: "visible"}}
	if !schemaEqual(objects, want) {
		t.Fatalf("ListSchema() = %#v, want %#v", objects, want)
	}
}
