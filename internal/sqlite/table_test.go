package sqlite

import (
	"context"
	"slices"
	"testing"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func TestServiceTableInfoAndBrowse(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE "items" (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note TEXT DEFAULT 'new')`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := service.Execute(ctx, `CREATE UNIQUE INDEX items_id_name_unique ON items(id, name)`); err != nil {
		t.Fatalf("creating unique index: %v", err)
	}
	if _, err := service.Execute(ctx, `CREATE INDEX items_note_index ON items(note)`); err != nil {
		t.Fatalf("creating index: %v", err)
	}
	for index := range 26 {
		if _, err := service.Execute(ctx, "INSERT INTO items (name) VALUES ('item')"); err != nil {
			t.Fatalf("inserting row %d: %v", index, err)
		}
	}

	columns, err := service.TableInfo(ctx, "items")
	if err != nil {
		t.Fatalf("TableInfo() error = %v", err)
	}
	if len(columns) != 3 || columns[1].Name != "name" || columns[1].Nullable || columns[2].DefaultValue == nil || *columns[2].DefaultValue != "'new'" {
		t.Fatalf("TableInfo() = %#v, want column details", columns)
	}
	if !slices.Equal(columns[0].Indexes, []sharedsql.IndexKind{sharedsql.IndexPrimaryKey, sharedsql.IndexUnique}) || !slices.Equal(columns[1].Indexes, []sharedsql.IndexKind{sharedsql.IndexUnique}) || !slices.Equal(columns[2].Indexes, []sharedsql.IndexKind{sharedsql.IndexRegular}) {
		t.Fatalf("TableInfo() indexes = %#v, want primary, unique, and regular index metadata", columns)
	}

	result, err := service.BrowseTable(ctx, "items", 25, 25)
	if err != nil {
		t.Fatalf("BrowseTable() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Columns[0] != "id" || result.TotalRows != 26 {
		t.Fatalf("BrowseTable() = %#v, want second page with one row", result)
	}
}

func TestServiceTableInfo_reportsGeneratedColumnAttribute(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE metrics (quantity INTEGER, doubled INTEGER GENERATED ALWAYS AS (quantity * 2) STORED)`); err != nil {
		t.Fatalf("creating generated-column table: %v", err)
	}

	columns, err := service.TableInfo(ctx, "metrics")
	if err != nil {
		t.Fatalf("TableInfo() error = %v", err)
	}
	if len(columns) != 2 || columns[1].Name != "doubled" || columns[1].Attributes != "GENERATED STORED" {
		t.Fatalf("TableInfo() = %#v, want generated stored attribute", columns)
	}
}

func TestServiceAlterColumn_rebuildsSchemaAndRetainsRows(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT 'untitled', note TEXT)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := service.Execute(ctx, `CREATE INDEX items_note ON items(note)`); err != nil {
		t.Fatalf("creating index: %v", err)
	}
	if _, err := service.Execute(ctx, `INSERT INTO items (name, note) VALUES ('first', 'kept')`); err != nil {
		t.Fatalf("inserting row: %v", err)
	}

	// When
	err := service.AlterColumn(ctx, "items", ColumnChange{
		Name:         "title",
		PreviousName: "name",
		Type:         "VARCHAR(40)",
		Nullable:     true,
	})

	// Then
	if err != nil {
		t.Fatalf("AlterColumn() error = %v", err)
	}
	columns, err := service.TableInfo(ctx, "items")
	if err != nil {
		t.Fatalf("reading altered table info: %v", err)
	}
	if len(columns) != 3 || columns[1].Name != "title" || columns[1].Type != "VARCHAR(40)" || !columns[1].Nullable || columns[1].DefaultValue != nil {
		t.Fatalf("TableInfo() = %#v, want altered title column", columns)
	}
	result, err := service.BrowseTable(ctx, "items", 0, 25)
	if err != nil {
		t.Fatalf("browsing altered table: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][1] == nil || *result.Rows[0][1] != "first" || result.Rows[0][2] == nil || *result.Rows[0][2] != "kept" {
		t.Fatalf("BrowseTable() = %#v, want retained row values", result)
	}
}

func TestServiceAlterColumn_retainsNameWhenRebuildCannotSafelyProceed(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE items (name TEXT CHECK(length(name) > 0))`); err != nil {
		t.Fatalf("creating table: %v", err)
	}

	// When
	err := service.AlterColumn(ctx, "items", ColumnChange{PreviousName: "name", Name: "title", Type: "VARCHAR(40)", Nullable: true})

	// Then
	if err == nil {
		t.Fatal("AlterColumn() error = nil, want unsupported constraint failure")
	}
	columns, err := service.TableInfo(ctx, "items")
	if err != nil {
		t.Fatalf("reading table info after failed alteration: %v", err)
	}
	if len(columns) != 1 || columns[0].Name != "name" {
		t.Fatalf("TableInfo() = %#v, want original name after failed alteration", columns)
	}
}
