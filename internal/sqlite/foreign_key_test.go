package sqlite

import (
	"context"
	"slices"
	"testing"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func TestService_managesForeignKeys(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE parents (id INTEGER PRIMARY KEY, code TEXT UNIQUE)"); err != nil {
		t.Fatalf("creating parents: %v", err)
	}
	if _, err := service.Execute(ctx, "CREATE TABLE children (parent_id INTEGER, code TEXT)"); err != nil {
		t.Fatalf("creating children: %v", err)
	}
	if _, err := service.Execute(ctx, "INSERT INTO children VALUES (1, 'a')"); err != nil {
		t.Fatalf("inserting child: %v", err)
	}
	change := sharedsql.ForeignKeyChange{Columns: []string{"parent_id"}, ReferenceTable: "parents", ReferenceColumns: []string{"id"}, OnDelete: "CASCADE", OnUpdate: "NO ACTION"}

	// When
	if err := service.CreateForeignKey(ctx, "children", change); err != nil {
		t.Fatalf("CreateForeignKey() error = %v", err)
	}
	foreignKeys, err := service.ListForeignKeys(ctx, "children")

	// Then
	if err != nil {
		t.Fatalf("ListForeignKeys() error = %v", err)
	}
	if len(foreignKeys) != 1 || !slices.Equal(foreignKeys[0].Columns, []string{"parent_id"}) || foreignKeys[0].ReferenceTable != "parents" || !slices.Equal(foreignKeys[0].ReferenceColumns, []string{"id"}) || foreignKeys[0].OnDelete != "CASCADE" {
		t.Fatalf("ListForeignKeys() = %#v, want parent_id references parents(id) on delete cascade", foreignKeys)
	}

	// When
	replacement := sharedsql.ForeignKeyChange{Columns: []string{"code"}, ReferenceTable: "parents", ReferenceColumns: []string{"code"}, OnDelete: "RESTRICT", OnUpdate: "CASCADE"}
	if err := service.ReplaceForeignKey(ctx, "children", foreignKeys[0].ID, replacement); err != nil {
		t.Fatalf("ReplaceForeignKey() error = %v", err)
	}
	if err := service.DropForeignKey(ctx, "children", foreignKeys[0].ID); err != nil {
		t.Fatalf("DropForeignKey() error = %v", err)
	}
	foreignKeys, err = service.ListForeignKeys(ctx, "children")

	// Then
	if err != nil {
		t.Fatalf("ListForeignKeys() after drop error = %v", err)
	}
	if len(foreignKeys) != 0 {
		t.Fatalf("ListForeignKeys() after drop = %#v, want no foreign keys", foreignKeys)
	}
	result, err := service.Execute(ctx, "SELECT * FROM children")
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("child data after migrations = %#v, %v; want one row", result.Rows, err)
	}
}

func TestService_dropsInlineForeignKey(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	if _, err := service.Execute(ctx, "CREATE TABLE parents (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating parents: %v", err)
	}
	if _, err := service.Execute(ctx, "CREATE TABLE children (parent_id INTEGER REFERENCES parents(id), value TEXT)"); err != nil {
		t.Fatalf("creating children: %v", err)
	}
	foreignKeys, err := service.ListForeignKeys(ctx, "children")
	if err != nil {
		t.Fatalf("ListForeignKeys() error = %v", err)
	}

	// When
	if err := service.DropForeignKey(ctx, "children", foreignKeys[0].ID); err != nil {
		t.Fatalf("DropForeignKey() error = %v", err)
	}
	foreignKeys, err = service.ListForeignKeys(ctx, "children")

	// Then
	if err != nil {
		t.Fatalf("ListForeignKeys() after drop error = %v", err)
	}
	if len(foreignKeys) != 0 {
		t.Fatalf("ListForeignKeys() after drop = %#v, want no foreign keys", foreignKeys)
	}
}
