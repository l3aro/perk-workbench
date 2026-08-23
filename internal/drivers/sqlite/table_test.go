package sqlite

import (
	"context"
	"fmt"
	plugindriver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"slices"
	"strings"
	"testing"
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
	if !slices.Equal(columns[0].Indexes, []plugindriver.IndexKind{plugindriver.IndexPrimaryKey, plugindriver.IndexUnique}) || !slices.Equal(columns[1].Indexes, []plugindriver.IndexKind{plugindriver.IndexUnique}) || !slices.Equal(columns[2].Indexes, []plugindriver.IndexKind{plugindriver.IndexRegular}) {
		t.Fatalf("TableInfo() indexes = %#v, want primary, unique, and regular index metadata", columns)
	}

	result, err := service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{Columns: []string{"id", "name", "note"}, Limit: 25})
	if err != nil {
		t.Fatalf("BrowseTable() error = %v", err)
	}
	if len(result.Rows) != 25 || result.Columns[0] != "id" || !result.HasMore {
		t.Fatalf("BrowseTable() = %#v, want first page without a total row count", result)
	}
	if len(result.UntruncatedRows) != len(result.Rows) {
		t.Fatalf("BrowseTable() UntruncatedRows = %d, want %d (matching Rows)", len(result.UntruncatedRows), len(result.Rows))
	}

	result, err = service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{Columns: []string{"id", "name", "note"}, Offset: 25, Limit: 25})
	if err != nil {
		t.Fatalf("BrowseTable() second page error = %v", err)
	}
	if len(result.Rows) != 1 || result.HasMore {
		t.Fatalf("BrowseTable() = %#v, want final page with no next page", result)
	}
	if len(result.UntruncatedRows) != len(result.Rows) {
		t.Fatalf("BrowseTable() second page UntruncatedRows = %d, want %d (matching Rows)", len(result.UntruncatedRows), len(result.Rows))
	}
}
func TestServiceBrowseTable_limit500UsesExtraRowWithoutTruncating(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	for index := range 501 {
		if _, err := service.Execute(ctx, fmt.Sprintf("INSERT INTO items (id) VALUES (%d)", index)); err != nil {
			t.Fatalf("inserting row %d: %v", index, err)
		}
	}

	result, err := service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{Columns: []string{"id"}, Limit: 500})
	if err != nil {
		t.Fatalf("BrowseTable() error = %v", err)
	}
	if len(result.Rows) != 500 || len(result.UntruncatedRows) != 500 {
		t.Fatalf("BrowseTable() rows = %d/%d, want 500/500", len(result.Rows), len(result.UntruncatedRows))
	}
	if !result.HasMore || result.Truncated {
		t.Fatalf("BrowseTable() HasMore/Truncated = %t/%t, want true/false", result.HasMore, result.Truncated)
	}
}

func TestServiceBrowseTable_filters_sorts_and_limits(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, score INTEGER, seen DATE, note TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, values := range []string{
		"('Ada', 10, '2024-01-01', NULL)",
		"('Adele', 20, '2025-01-01', 'memo')",
		"('Bob', 30, '2026-01-01', '')",
		"('', 5, '2023-01-01', 'other')",
	} {
		if _, err := service.Execute(ctx, "INSERT INTO items (name, score, seen, note) VALUES "+values); err != nil {
			t.Fatal(err)
		}
	}

	result, err := service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{
		Columns: []string{"id", "name", "score", "seen", "note"},
		Filters: []plugindriver.BrowseFilter{{Column: "name", Operator: BrowseFilterLike, Value: "Ad%"}},
		Sorts:   []plugindriver.BrowseSort{{Column: "name", Descending: true}},
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("browsing LIKE filter: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][1] == nil || *result.Rows[0][1] != "Adele" || !result.HasMore {
		t.Fatalf("LIKE result = %#v, want first descending Ada%% match", result)
	}

	result, err = service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{
		Columns: []string{"id", "name", "score", "seen", "note"},
		Filters: []plugindriver.BrowseFilter{
			{Column: "name", Operator: BrowseFilterNotLike, Value: "Ada%"},
			{Column: "score", Operator: BrowseFilterGreater, Value: "10"},
			{Column: "seen", Operator: BrowseFilterGreaterEqual, Value: "2026-01-01"},
		},
		Limit: 4,
	})
	if err != nil {
		t.Fatalf("browsing combined filters: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][1] == nil || *result.Rows[0][1] != "Bob" {
		t.Fatalf("combined filter result = %#v, want only Bob", result)
	}

	result, err = service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{
		Columns: []string{"id", "name", "score", "seen", "note"},
		Filters: []plugindriver.BrowseFilter{{Column: "note", Operator: BrowseFilterIsNull}},
		Limit:   4,
	})
	if err != nil || len(result.Rows) != 1 || result.Rows[0][1] == nil || *result.Rows[0][1] != "Ada" {
		t.Fatalf("IS NULL result = %#v, error = %v, want only Ada", result, err)
	}

	result, err = service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{
		Columns: []string{"id", "name", "score", "seen", "note"},
		Filters: []plugindriver.BrowseFilter{{Column: "note", Operator: BrowseFilterEqual, Value: ""}},
		Limit:   4,
	})
	if err != nil || len(result.Rows) != 1 || result.Rows[0][1] == nil || *result.Rows[0][1] != "Bob" {
		t.Fatalf("empty equality result = %#v, error = %v, want only Bob", result, err)
	}

	for _, filter := range []plugindriver.BrowseFilter{
		{Column: "missing", Operator: BrowseFilterEqual, Value: "x"},
		{Column: "name", Operator: string("DROP TABLE"), Value: "x"},
	} {
		if _, err := service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{Columns: []string{"id", "name", "score", "seen", "note"}, Filters: []plugindriver.BrowseFilter{filter}, Limit: 4}); err == nil {
			t.Fatalf("BrowseTable(%#v) error = nil, want validation error", filter)
		}
	}

	if _, err := service.Execute(ctx, "CREATE TABLE ranked (group_name TEXT, rank INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for _, values := range []string{"('a', 1)", "('a', 2)", "('b', 1)"} {
		if _, err := service.Execute(ctx, "INSERT INTO ranked VALUES "+values); err != nil {
			t.Fatal(err)
		}
	}
	result, err = service.BrowseTable(ctx, "ranked", plugindriver.BrowseOptions{
		Columns: []string{"group_name", "rank"},
		Sorts:   []plugindriver.BrowseSort{{Column: "group_name", Descending: true}, {Column: "rank", Descending: true}},
		Limit:   3,
	})
	if err != nil {
		t.Fatalf("browsing multi-sort table: %v", err)
	}
	if got := []string{*result.Rows[0][0], *result.Rows[0][1], *result.Rows[1][0], *result.Rows[1][1]}; !slices.Equal(got, []string{"b", "1", "a", "2"}) {
		t.Fatalf("multi-sort rows = %#v, want b/1 then a/2", got)
	}
}

func TestServiceBrowseTable_patternFilters(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE pets (name TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, values := range []string{
		"('rez_abc_')",
		"('rez_ab')",
		"('rez_abc')",
		"('dog_1')",
		"('dog2')",
	} {
		if _, err := service.Execute(ctx, "INSERT INTO pets (name) VALUES "+values); err != nil {
			t.Fatal(err)
		}
	}
	columns := []string{"name"}

	// PATTERN is shell-wildcard semantics: * any run, ? one char, and _ is
	// literal. rez_*_ must not match rez_ab or rez_abc.
	result, err := service.BrowseTable(ctx, "pets", plugindriver.BrowseOptions{
		Columns: columns,
		Filters: []plugindriver.BrowseFilter{{Column: "name", Operator: BrowseFilterPattern, Value: "rez_*_"}},
		Limit:   4,
	})
	if err != nil {
		t.Fatalf("browsing PATTERN filter: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] == nil || *result.Rows[0][0] != "rez_abc_" {
		t.Fatalf("PATTERN rez_*_ result = %#v, want only rez_abc_", result)
	}

	// ? matches exactly one character: dog? fits dog2, not dog_1.
	result, err = service.BrowseTable(ctx, "pets", plugindriver.BrowseOptions{
		Columns: columns,
		Filters: []plugindriver.BrowseFilter{{Column: "name", Operator: BrowseFilterPattern, Value: "dog?"}},
		Limit:   4,
	})
	if err != nil {
		t.Fatalf("browsing PATTERN dog?: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] == nil || *result.Rows[0][0] != "dog2" {
		t.Fatalf("PATTERN dog? result = %#v, want only dog2", result)
	}

	// % and _ are literal in PATTERN: 100% must not act as a LIKE wildcard.
	if _, err := service.Execute(ctx, "INSERT INTO pets (name) VALUES ('100%')"); err != nil {
		t.Fatal(err)
	}
	result, err = service.BrowseTable(ctx, "pets", plugindriver.BrowseOptions{
		Columns: columns,
		Filters: []plugindriver.BrowseFilter{{Column: "name", Operator: BrowseFilterPattern, Value: "100%"}},
		Limit:   4,
	})
	if err != nil {
		t.Fatalf("browsing PATTERN 100%%: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] == nil || *result.Rows[0][0] != "100%" {
		t.Fatalf("PATTERN 100%% result = %#v, want only the literal 100%% row", result)
	}

	result, err = service.BrowseTable(ctx, "pets", plugindriver.BrowseOptions{
		Columns: columns,
		Filters: []plugindriver.BrowseFilter{{Column: "name", Operator: BrowseFilterNotPattern, Value: "rez*"}},
		Limit:   4,
	})
	if err != nil {
		t.Fatalf("browsing NOT PATTERN filter: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("NOT PATTERN rez* result = %#v, want the three non-rez rows", result)
	}

	for _, filter := range []plugindriver.BrowseFilter{
		{Column: "name", Operator: string("DROP TABLE"), Value: "x"},
	} {
		if _, err := service.BrowseTable(ctx, "pets", plugindriver.BrowseOptions{Columns: columns, Filters: []plugindriver.BrowseFilter{filter}, Limit: 4}); err == nil {
			t.Fatalf("BrowseTable(%#v) error = nil, want validation error", filter)
		}
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
	result, err := service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{Columns: []string{"id", "name", "note"}, Limit: 25})
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
	err := service.AlterColumn(ctx, "items", plugindriver.ColumnChange{
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

func TestServiceAddColumn_addsNewColumn(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := service.Execute(ctx, `INSERT INTO items (name) VALUES ('first')`); err != nil {
		t.Fatalf("inserting row: %v", err)
	}

	if err := service.AddColumn(ctx, "items", plugindriver.ColumnDef{Name: "note", Type: "TEXT", Nullable: true}); err != nil {
		t.Fatalf("AddColumn() error = %v", err)
	}

	columns, err := service.TableInfo(ctx, "items")
	if err != nil {
		t.Fatalf("reading table info after add: %v", err)
	}
	if len(columns) != 3 || columns[2].Name != "note" || columns[2].Type != "TEXT" || !columns[2].Nullable {
		t.Fatalf("TableInfo() = %#v, want note column", columns)
	}

	result, err := service.BrowseTable(ctx, "items", plugindriver.BrowseOptions{Columns: []string{"id", "name", "note"}, Limit: 25})
	if err != nil {
		t.Fatalf("browsing table after add: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][2] != nil {
		t.Fatalf("BrowseTable() = %#v, want existing row with NULL note", result)
	}
}

func TestServiceAddColumn_rejectsMissingName(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if err := service.AddColumn(ctx, "items", plugindriver.ColumnDef{Name: "", Type: "TEXT"}); err == nil {
		t.Fatal("AddColumn(empty name) = nil, want error")
	}
}
