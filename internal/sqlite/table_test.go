package sqlite

import (
	"context"
	"slices"
	"strings"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
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

	result, err := service.BrowseTable(ctx, "items", sharedsql.BrowseOptions{Columns: []string{"id", "name", "note"}, Limit: 25})
	if err != nil {
		t.Fatalf("BrowseTable() error = %v", err)
	}
	if len(result.Rows) != 25 || result.Columns[0] != "id" || !result.HasMore {
		t.Fatalf("BrowseTable() = %#v, want first page without a total row count", result)
	}

	result, err = service.BrowseTable(ctx, "items", sharedsql.BrowseOptions{Columns: []string{"id", "name", "note"}, Offset: 25, Limit: 25})
	if err != nil {
		t.Fatalf("BrowseTable() second page error = %v", err)
	}
	if len(result.Rows) != 1 || result.HasMore {
		t.Fatalf("BrowseTable() = %#v, want final page with no next page", result)
	}
}

func TestServiceBrowseTable_filters_sorts_and_limits(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Ada", "Bob", "Adele"} {
		if _, err := service.Execute(ctx, "INSERT INTO items (name) VALUES ('"+name+"')"); err != nil {
			t.Fatal(err)
		}
	}

	result, err := service.BrowseTable(ctx, "items", sharedsql.BrowseOptions{
		Columns: []string{"id", "name"}, Filter: "ad", Sorts: []sharedsql.BrowseSort{{Column: "name", Descending: true}}, Limit: 1,
	})
	if err != nil {
		t.Fatalf("BrowseTable() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][1] == nil || *result.Rows[0][1] != "Adele" || !result.HasMore {
		t.Fatalf("BrowseTable() = %#v, want first descending filtered row with another row available", result)
	}

	if _, err := service.Execute(ctx, "CREATE TABLE ranked (group_name TEXT, rank INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for _, values := range []string{"('a', 1)", "('a', 2)", "('b', 1)"} {
		if _, err := service.Execute(ctx, "INSERT INTO ranked VALUES "+values); err != nil {
			t.Fatal(err)
		}
	}
	result, err = service.BrowseTable(ctx, "ranked", sharedsql.BrowseOptions{
		Columns: []string{"group_name", "rank"},
		Sorts:   []sharedsql.BrowseSort{{Column: "group_name", Descending: true}, {Column: "rank", Descending: true}},
		Limit:   3,
	})
	if err != nil {
		t.Fatalf("browsing multi-sort table: %v", err)
	}
	if got := []string{*result.Rows[0][0], *result.Rows[0][1], *result.Rows[1][0], *result.Rows[1][1]}; !slices.Equal(got, []string{"b", "1", "a", "2"}) {
		t.Fatalf("multi-sort rows = %#v, want b/1 then a/2", got)
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
	result, err := service.BrowseTable(ctx, "items", sharedsql.BrowseOptions{Columns: []string{"id", "name", "note"}, Limit: 25})
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

func TestServiceAlterColumn_rejectsAttributesChange(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE "items" (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	attrs := "COMMENT 'desc'"
	err := service.AlterColumn(ctx, "items", sharedsql.ColumnChange{
		PreviousName: "name",
		Name:         "name",
		Type:         "TEXT",
		Nullable:     false,
		Attributes:   &attrs,
	})
	if err == nil {
		t.Fatal("AlterColumn() with changed attributes = nil, want error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("AlterColumn() error = %q, want 'not supported'", err)
	}
	columns, err := service.TableInfo(ctx, "items")
	if err != nil {
		t.Fatalf("reading table info after failed alteration: %v", err)
	}
	if len(columns) != 2 || columns[1].Name != "name" || columns[1].Type != "TEXT" {
		t.Fatalf("TableInfo() = %#v, want unchanged schema after failed alteration", columns)
	}
}
