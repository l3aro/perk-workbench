package schema

import (
	"reflect"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestRingsFromMap_directNeighborsNeverSuppressed(t *testing.T) {
	// Given — T is a direct outgoing neighbor of H AND reachable on the
	// top side at depth 2 (T references A, A references H). The depth-1
	// seed must claim it for the bottom side before the top expansion.
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"H": {{Columns: []string{"t_id"}, ReferenceTable: "T"}},
		"A": {{Columns: []string{"h_id"}, ReferenceTable: "H"}},
		"X": {{Columns: []string{"a_id"}, ReferenceTable: "A"}},
		"T": {{Columns: []string{"a_id"}, ReferenceTable: "A"}},
	}

	// When
	top, bottom := ringsFromMap(foreignKeys, "H", 3)

	// Then — T stays on the bottom ring it belongs to, with labels.
	wantTop := [][]string{{"A"}, {"X"}}
	wantBottom := [][]string{{"T"}}
	if !reflect.DeepEqual(top, wantTop) {
		t.Fatalf("top = %v, want %v", top, wantTop)
	}
	if !reflect.DeepEqual(bottom, wantBottom) {
		t.Fatalf("bottom = %v, want %v", bottom, wantBottom)
	}
}

func TestRingsFromMap_expandsBothSides(t *testing.T) {
	// Given — two hops of references in each direction.
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"orders":   {{Columns: []string{"user_id"}, ReferenceTable: "users"}},
		"users":    {{Columns: []string{"team_id"}, ReferenceTable: "teams"}},
		"invoices": {{Columns: []string{"order_id"}, ReferenceTable: "orders"}},
		"receipts": {{Columns: []string{"invoice_id"}, ReferenceTable: "invoices"}},
	}

	// When
	top, bottom := ringsFromMap(foreignKeys, "orders", 2)

	// Then — top holds tables referencing orders, then their references;
	// bottom holds orders' targets, then their targets.
	wantTop := [][]string{{"invoices"}, {"receipts"}}
	wantBottom := [][]string{{"users"}, {"teams"}}
	if !reflect.DeepEqual(top, wantTop) {
		t.Fatalf("top = %v, want %v", top, wantTop)
	}
	if !reflect.DeepEqual(bottom, wantBottom) {
		t.Fatalf("bottom = %v, want %v", bottom, wantBottom)
	}
}

func TestRingsFromMap_depthOneStopsAtNeighbors(t *testing.T) {
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"orders": {{Columns: []string{"user_id"}, ReferenceTable: "users"}},
		"users":  {{Columns: []string{"team_id"}, ReferenceTable: "teams"}},
	}
	top, bottom := ringsFromMap(foreignKeys, "orders", 1)
	if len(top) != 0 || !reflect.DeepEqual(bottom, [][]string{{"users"}}) {
		t.Fatalf("rings = top %v bottom %v, want only depth-1 bottom", top, bottom)
	}
}

func TestRingsFromMap_ignoresSelfReferences(t *testing.T) {
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"tree": {
			{Columns: []string{"parent_id"}, ReferenceTable: "tree"},
			{Columns: []string{"root_id"}, ReferenceTable: "root"},
		},
	}
	top, bottom := ringsFromMap(foreignKeys, "tree", 2)
	if len(top) != 0 || !reflect.DeepEqual(bottom, [][]string{{"root"}}) {
		t.Fatalf("rings = top %v bottom %v, want only the root target", top, bottom)
	}
}

func TestRingsFromMap_twoSidedNeighborGoesToTop(t *testing.T) {
	// Given — T references H and H references T: T is directly adjacent on
	// both sides. The tie rule puts it on the top (referencing) ring.
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"H": {{Columns: []string{"t_id"}, ReferenceTable: "T"}},
		"T": {{Columns: []string{"h_id"}, ReferenceTable: "H"}},
	}

	// When
	top, bottom := ringsFromMap(foreignKeys, "H", 2)

	// Then
	if !reflect.DeepEqual(top, [][]string{{"T"}}) || len(bottom) != 0 {
		t.Fatalf("rings = top %v bottom %v, want T on the top ring only", top, bottom)
	}
}

func TestRingsFromMap_caseInsensitiveIdentifiers(t *testing.T) {
	// Given — SQLite/MySQL identifiers are case-insensitive: the hub is
	// selected as "Orders" while the map keys and reference names use
	// different cases.
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"invoices": {{Columns: []string{"order_id"}, ReferenceTable: "ORDERS"}},
		"ORDERS":   {{Columns: []string{"user_id"}, ReferenceTable: "Users"}},
	}

	// When
	top, bottom := ringsFromMap(foreignKeys, "Orders", 2)

	// Then — neighbors are found and displayed with their map casing.
	if !reflect.DeepEqual(top, [][]string{{"invoices"}}) || !reflect.DeepEqual(bottom, [][]string{{"Users"}}) {
		t.Fatalf("rings = top %v bottom %v, want case-insensitive neighbors", top, bottom)
	}
}

func TestDiagramHubKey_matchesCaseInsensitively(t *testing.T) {
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"ORDERS": {{Columns: []string{"user_id"}, ReferenceTable: "Users"}},
	}
	if got := diagramHubKey(foreignKeys, "orders"); got != "ORDERS" {
		t.Fatalf("diagramHubKey = %q, want ORDERS", got)
	}
	if got := diagramTableKey(foreignKeys, "orders"); got != "ORDERS" {
		t.Fatalf("diagramTableKey = %q, want ORDERS", got)
	}
	indexes := map[string][]sharedsql.IndexInfo{"ORDERS": nil}
	if got := diagramIndexKey(indexes, "orders"); got != "ORDERS" {
		t.Fatalf("diagramIndexKey = %q, want ORDERS", got)
	}
}

func TestMergeRow_marksEveryCenter(t *testing.T) {
	// Two stubs merging into two centers: every center gets a stem, the
	// bar spans the outermost columns.
	got := mergeRow([]int{2, 8}, []int{4, 6}, 12, true)
	want := "  └─┬─┬─┘   "
	if got != want {
		t.Fatalf("mergeRow = %q, want %q", got, want)
	}
}

func TestMergeRow_singleStubJogsToNearestCenter(t *testing.T) {
	got := mergeRow([]int{1}, []int{5}, 8, false)
	want := " ┌───┴  "
	if got != want {
		t.Fatalf("mergeRow = %q, want %q", got, want)
	}
}
