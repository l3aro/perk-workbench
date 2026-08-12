package sqlite

import (
	"context"
	"slices"
	"strings"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestService_ListForeignKeysAll_groupsByTable(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	for _, statement := range []string{
		"CREATE TABLE customers (id INTEGER PRIMARY KEY, code TEXT UNIQUE)",
		"CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id), code TEXT REFERENCES customers(code))",
		"CREATE TABLE items (id INTEGER PRIMARY KEY, order_id INTEGER REFERENCES orders(id))",
	} {
		if _, err := service.Execute(ctx, statement); err != nil {
			t.Fatalf("executing %q: %v", statement, err)
		}
	}

	// When
	byTable, err := service.ListForeignKeysAll(ctx)

	// Then — every declaring table is a key, each FK keeps its columns and
	// resolves to the same data as the per-table listing.
	if err != nil {
		t.Fatalf("ListForeignKeysAll() error = %v", err)
	}
	if len(byTable) != 2 {
		t.Fatalf("declaring tables = %v, want [items orders]", len(byTable))
	}
	orders := byTable["orders"]
	if len(orders) != 2 {
		t.Fatalf("orders FKs = %d, want 2", len(orders))
	}
	byColumn := map[string]sharedsql.ForeignKeyInfo{}
	for _, foreignKey := range orders {
		byColumn[strings.Join(foreignKey.Columns, ",")] = foreignKey
	}
	customerKey, ok := byColumn["customer_id"]
	if !ok || customerKey.ReferenceTable != "customers" || !slices.Equal(customerKey.ReferenceColumns, []string{"id"}) {
		t.Fatalf("customer FK = %+v, want customer_id → customers.id", customerKey)
	}
	codeKey, ok := byColumn["code"]
	if !ok || codeKey.ReferenceTable != "customers" || !slices.Equal(codeKey.ReferenceColumns, []string{"code"}) {
		t.Fatalf("code FK = %+v, want code → customers.code", codeKey)
	}
	perTable, err := service.ListForeignKeys(ctx, "orders")
	if err != nil {
		t.Fatalf("ListForeignKeys() error = %v", err)
	}
	if len(perTable) != len(orders) {
		t.Fatalf("all-map FKs = %d, per-table = %d", len(orders), len(perTable))
	}
	for index := range orders {
		if orders[index].ID != perTable[index].ID || !slices.Equal(orders[index].Columns, perTable[index].Columns) || !slices.Equal(orders[index].ReferenceColumns, perTable[index].ReferenceColumns) {
			t.Fatalf("FK %d mismatch: all = %+v, per-table = %+v", index, orders[index], perTable[index])
		}
	}
	if _, ok := byTable["customers"]; ok {
		t.Fatal("customers declares no foreign keys but appears in the map")
	}
}

func TestService_ListForeignKeysAll_resolvesOmittedReferenceColumns(t *testing.T) {
	// Given — a foreign key that omits its target columns; the driver must
	// resolve them from the referenced table's primary key.
	service := newMemoryService(t)
	ctx := context.Background()
	for _, statement := range []string{
		"CREATE TABLE parents (id INTEGER PRIMARY KEY, code TEXT)",
		"CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parents)",
	} {
		if _, err := service.Execute(ctx, statement); err != nil {
			t.Fatalf("executing %q: %v", statement, err)
		}
	}

	// When
	byTable, err := service.ListForeignKeysAll(ctx)

	// Then
	if err != nil {
		t.Fatalf("ListForeignKeysAll() error = %v", err)
	}
	children := byTable["children"]
	if len(children) != 1 || !slices.Equal(children[0].ReferenceColumns, []string{"id"}) {
		t.Fatalf("children = %+v, want parent_id → parents.id", children)
	}
}

func TestService_ListIndexesAll_keysEveryTable(t *testing.T) {
	// Given
	service := newMemoryService(t)
	ctx := context.Background()
	for _, statement := range []string{
		"CREATE TABLE plain (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE TABLE composite (a TEXT, b TEXT, PRIMARY KEY (a, b))",
		"CREATE TABLE indexed (id INTEGER, email TEXT, note TEXT)",
		"CREATE UNIQUE INDEX indexed_email ON indexed (email)",
		"CREATE INDEX indexed_note ON indexed (note)",
		"CREATE TABLE empty (value TEXT)",
	} {
		if _, err := service.Execute(ctx, statement); err != nil {
			t.Fatalf("executing %q: %v", statement, err)
		}
	}

	// When
	byTable, err := service.ListIndexesAll(ctx)

	// Then — every table is keyed, primary keys lead, named indexes follow,
	// and tables without indexes still appear with an empty list.
	if err != nil {
		t.Fatalf("ListIndexesAll() error = %v", err)
	}
	for _, table := range []string{"plain", "composite", "indexed", "empty"} {
		if _, ok := byTable[table]; !ok {
			t.Fatalf("ListIndexesAll() misses table %q", table)
		}
	}
	plain := byTable["plain"]
	if len(plain) != 1 || plain[0].Name != "PRIMARY" || !plain[0].PrimaryKey || !slices.Equal(plain[0].Columns, []string{"id"}) {
		t.Fatalf("plain = %+v, want PRIMARY(id)", plain)
	}
	composite := byTable["composite"]
	if len(composite) != 1 || !composite[0].PrimaryKey || !slices.Equal(composite[0].Columns, []string{"a", "b"}) {
		t.Fatalf("composite = %+v, want PRIMARY(a, b)", composite)
	}
	indexed := byTable["indexed"]
	if len(indexed) != 2 {
		t.Fatalf("indexed has %d indexes, want 2: %+v", len(indexed), indexed)
	}
	byName := map[string]sharedsql.IndexInfo{}
	for _, index := range indexed {
		byName[index.Name] = index
	}
	emailIndex, ok := byName["indexed_email"]
	if !ok || !emailIndex.Unique || !slices.Equal(emailIndex.Columns, []string{"email"}) {
		t.Fatalf("indexed_email = %+v, want unique indexed_email(email)", emailIndex)
	}
	noteIndex, ok := byName["indexed_note"]
	if !ok || noteIndex.Unique || !slices.Equal(noteIndex.Columns, []string{"note"}) {
		t.Fatalf("indexed_note = %+v, want indexed_note(note)", noteIndex)
	}
	if len(byTable["empty"]) != 0 {
		t.Fatalf("empty = %+v, want no indexes", byTable["empty"])
	}
	// The all-map matches the per-table listing for one table.
	perTable, err := service.ListIndexes(ctx, "indexed")
	if err != nil {
		t.Fatalf("ListIndexes() error = %v", err)
	}
	for index := range perTable {
		if perTable[index].Name != indexed[index].Name || perTable[index].Unique != indexed[index].Unique || !slices.Equal(perTable[index].Columns, indexed[index].Columns) {
			t.Fatalf("index %d mismatch: all = %+v, per-table = %+v", index, indexed[index], perTable[index])
		}
	}
}

func TestService_ListForeignKeysAll_matchesListForeignKeysOnEveryTable(t *testing.T) {
	// Given — a schema mixing self-references, chained references, and
	// multiple FKs on one table.
	service := newMemoryService(t)
	ctx := context.Background()
	for _, statement := range []string{
		"CREATE TABLE tree (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES tree(id))",
		"CREATE TABLE a (id INTEGER PRIMARY KEY)",
		"CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES a(id))",
		"CREATE TABLE c (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES a(id), b_id INTEGER REFERENCES b(id))",
	} {
		if _, err := service.Execute(ctx, statement); err != nil {
			t.Fatalf("executing %q: %v", statement, err)
		}
	}

	// When
	byTable, err := service.ListForeignKeysAll(ctx)

	// Then — every table's entry equals its per-table listing.
	if err != nil {
		t.Fatalf("ListForeignKeysAll() error = %v", err)
	}
	for table, foreignKeys := range byTable {
		perTable, err := service.ListForeignKeys(ctx, table)
		if err != nil {
			t.Fatalf("ListForeignKeys(%q) error = %v", table, err)
		}
		if len(foreignKeys) != len(perTable) {
			t.Fatalf("%q: all-map FKs = %d, per-table = %d", table, len(foreignKeys), len(perTable))
		}
		for index := range foreignKeys {
			if foreignKeys[index].ReferenceTable != perTable[index].ReferenceTable || !slices.Equal(foreignKeys[index].Columns, perTable[index].Columns) || !slices.Equal(foreignKeys[index].ReferenceColumns, perTable[index].ReferenceColumns) {
				t.Fatalf("%q FK %d mismatch: all = %+v, per-table = %+v", table, index, foreignKeys[index], perTable[index])
			}
		}
	}
	if len(byTable["tree"]) != 1 || byTable["tree"][0].ReferenceTable != "tree" {
		t.Fatalf("tree = %+v, want self-reference", byTable["tree"])
	}
}
