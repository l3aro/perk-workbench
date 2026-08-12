package sqlite

import (
	"context"
	"slices"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestService_managesNamedIndexes(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE items (id INTEGER, name TEXT, category TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	change := sharedsql.IndexChange{Name: "items_name", Columns: []string{"name"}}

	// When
	if err := service.CreateIndex(ctx, "items", change); err != nil {
		t.Fatalf("CreateIndex() error = %v", err)
	}
	indexes, err := service.ListIndexes(ctx, "items")

	// Then
	if err != nil {
		t.Fatalf("ListIndexes() error = %v", err)
	}
	if len(indexes) != 1 || indexes[0].Name != "items_name" || indexes[0].Unique || !slices.Equal(indexes[0].Columns, []string{"name"}) {
		t.Fatalf("ListIndexes() = %#v, want items_name(name)", indexes)
	}

	// When
	replacement := sharedsql.IndexChange{Name: "items_category_unique", Unique: true, Columns: []string{"category", "name"}}
	if err := service.ReplaceIndex(ctx, "items", "items_name", replacement); err != nil {
		t.Fatalf("ReplaceIndex() error = %v", err)
	}
	if err := service.DropIndex(ctx, "items", "items_category_unique"); err != nil {
		t.Fatalf("DropIndex() error = %v", err)
	}
	indexes, err = service.ListIndexes(ctx, "items")

	// Then
	if err != nil {
		t.Fatalf("ListIndexes() after drop error = %v", err)
	}
	if len(indexes) != 0 {
		t.Fatalf("ListIndexes() after drop = %#v, want no named indexes", indexes)
	}
}

func TestService_managesPrimaryKey(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE items (id INTEGER, code TEXT, name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}

	// When
	if err := service.CreateIndex(ctx, "items", sharedsql.IndexChange{PrimaryKey: true, Columns: []string{"id", "code"}}); err != nil {
		t.Fatalf("CreateIndex() primary key error = %v", err)
	}
	indexes, err := service.ListIndexes(ctx, "items")

	// Then
	if err != nil {
		t.Fatalf("ListIndexes() error = %v", err)
	}
	if len(indexes) != 1 || !indexes[0].PrimaryKey || !slices.Equal(indexes[0].Columns, []string{"id", "code"}) {
		t.Fatalf("ListIndexes() = %#v, want PRIMARY(id, code)", indexes)
	}

	// When
	if err := service.ReplaceIndex(ctx, "items", "PRIMARY", sharedsql.IndexChange{PrimaryKey: true, Columns: []string{"code"}}); err != nil {
		t.Fatalf("ReplaceIndex() primary key error = %v", err)
	}
	if err := service.DropIndex(ctx, "items", "PRIMARY"); err != nil {
		t.Fatalf("DropIndex() primary key error = %v", err)
	}
	indexes, err = service.ListIndexes(ctx, "items")

	// Then
	if err != nil {
		t.Fatalf("ListIndexes() after drop error = %v", err)
	}
	if len(indexes) != 0 {
		t.Fatalf("ListIndexes() after drop = %#v, want no indexes", indexes)
	}
}

func TestService_rejectsPrimaryKeyChangeWithForeignKeyDependents(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE parents (id INTEGER PRIMARY KEY, code TEXT)"); err != nil {
		t.Fatalf("creating parents table: %v", err)
	}
	if _, err := service.Execute(ctx, "CREATE TABLE children (parent_id INTEGER REFERENCES parents(id))"); err != nil {
		t.Fatalf("creating children table: %v", err)
	}

	// When
	err := service.ReplaceIndex(ctx, "parents", "PRIMARY", sharedsql.IndexChange{PrimaryKey: true, Columns: []string{"code"}})

	// Then
	if err == nil {
		t.Fatal("ReplaceIndex() error = nil, want foreign-key dependency failure")
	}
	indexes, listErr := service.ListIndexes(ctx, "parents")
	if listErr != nil {
		t.Fatalf("ListIndexes() error = %v", listErr)
	}
	if len(indexes) != 1 || !indexes[0].PrimaryKey || !slices.Equal(indexes[0].Columns, []string{"id"}) {
		t.Fatalf("ListIndexes() = %#v, want original PRIMARY(id)", indexes)
	}
}
