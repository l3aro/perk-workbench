package sqlite

import (
	"context"
	"testing"
)

func TestServiceTableInfoAndBrowse(t *testing.T) {
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, `CREATE TABLE "items" (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note TEXT DEFAULT 'new')`); err != nil {
		t.Fatalf("creating table: %v", err)
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

	result, err := service.BrowseTable(ctx, "items", 25, 25)
	if err != nil {
		t.Fatalf("BrowseTable() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Columns[0] != "id" {
		t.Fatalf("BrowseTable() = %#v, want second page with one row", result)
	}
}
