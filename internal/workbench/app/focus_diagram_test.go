package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// diagramFixture builds a ready model on the indexes tab with a
// three-table schema graph: orders -> users (outgoing), invoices -> orders
// (incoming), and indexes on every table.
func diagramFixture(t *testing.T) Model {
	t.Helper()
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabIndexes, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"orders":   {{ID: "orders_user_id_fkey", Columns: []string{"user_id"}, ReferenceTable: "users", ReferenceColumns: []string{"id"}}},
		"invoices": {{ID: "invoices_order_id_fkey", Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}},
	}
	model.schema.indexesAll = map[string][]sharedsql.IndexInfo{
		"orders": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "orders_user_id_idx", Unique: true, Columns: []string{"user_id"}},
		},
		"users": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
		},
		"invoices": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "invoices_order_id_idx", Columns: []string{"order_id"}},
		},
	}
	return resizeModel(model, 80, 24)
}

func TestIndexesTab_togglesDiagram_withIndexCards(t *testing.T) {
	// Given
	model := diagramFixture(t)

	// When — press g on the indexes tab to toggle the diagram.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.indexesView())

	// Then — the hub and both neighbors render with their indexes; the
	// relationship diagram stays off.
	if !model.schema.component.Structure.IndexDiagram {
		t.Fatal("g did not enable the indexes diagram")
	}
	if model.schema.component.Structure.RelationshipDiagram {
		t.Fatal("indexes diagram leaked into the foreign-key diagram flag")
	}
	for _, text := range []string{"orders", "users", "invoices", "🔑 PRIMARY (id)", "🔒 orders_user_id_idx (user_id)", "invoices_order_id_idx (order_id)"} {
		if !strings.Contains(view, text) {
			t.Fatalf("indexes diagram view = %q, want %q", view, text)
		}
	}
	if got := strings.Count(view, "┌"); got != 3 {
		t.Fatalf("card count = %d, want 3 in %q", got, view)
	}
	// Toggle back off returns the table view.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	if model.schema.component.Structure.IndexDiagram {
		t.Fatal("g did not disable the indexes diagram")
	}
	if view := ansi.Strip(model.indexesView()); strings.Contains(view, "┌─ orders") || strings.Contains(view, "🔑 PRIMARY") {
		t.Fatalf("table view = %q, want the indexes table", view)
	}
}

func TestIndexesTab_diagramFallsBackToListWhenTooWide(t *testing.T) {
	// Given — a neighbor card wider than the workspace viewport.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabIndexes, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"orders": {{Columns: []string{"user_id"}, ReferenceTable: "a_table_with_a_really_really_really_very_long_name_that_cannot_fit", ReferenceColumns: []string{"id"}}},
	}
	model.schema.indexesAll = map[string][]sharedsql.IndexInfo{
		"orders": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
		"a_table_with_a_really_really_really_very_long_name_that_cannot_fit": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
	}
	model = resizeModel(model, 60, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.indexesView())

	// Then — the flat list keeps every line inside the viewport.
	if !strings.Contains(view, "indexes") {
		t.Fatalf("fallback view = %q, want the indexes list", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > model.layout.tableViewportWidth {
			t.Fatalf("fallback line width = %d, want <= %d: %q", ansi.StringWidth(line), model.layout.tableViewportWidth, line)
		}
	}
}

func TestDiagramDepth_keysWidenTheFocusRing(t *testing.T) {
	// Given — a two-hop chain in each direction and the FK diagram active.
	model := diagramFixture(t)
	model.schema.foreignKeysAll["users"] = []sharedsql.ForeignKeyInfo{{Columns: []string{"team_id"}, ReferenceTable: "teams", ReferenceColumns: []string{"id"}}}
	model.schema.foreignKeysAll["payments"] = []sharedsql.ForeignKeyInfo{{Columns: []string{"invoice_id"}, ReferenceTable: "invoices", ReferenceColumns: []string{"id"}}}
	model = resizeModel(model, 80, 30)
	model.Tab, model.Focus = tabForeignKeys, focusWorkspace
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)

	// When — depth 1 shows only direct neighbors.
	view := ansi.Strip(model.foreignKeysView())
	for _, text := range []string{"users", "invoices"} {
		if !strings.Contains(view, text) {
			t.Fatalf("depth-1 diagram = %q, want %q", view, text)
		}
	}
	for _, text := range []string{"teams", "payments"} {
		if strings.Contains(view, text) {
			t.Fatalf("depth-1 diagram = %q, must not contain %q", view, text)
		}
	}

	// When — } widens the ring one hop.
	updated, _ = model.Update(tea.KeyPressMsg{Code: '}', Text: "}"})
	model = updated.(Model)
	if model.schema.component.Structure.DiagramDepth != 2 {
		t.Fatalf("depth = %d, want 2", model.schema.component.Structure.DiagramDepth)
	}
	view = ansi.Strip(model.foreignKeysView())
	for _, text := range []string{"teams", "payments"} {
		if !strings.Contains(view, text) {
			t.Fatalf("depth-2 diagram = %q, want %q", view, text)
		}
	}

	// When — { narrows the ring again.
	updated, _ = model.Update(tea.KeyPressMsg{Code: '{', Text: "{"})
	model = updated.(Model)
	if model.schema.component.Structure.DiagramDepth != 1 {
		t.Fatalf("depth = %d, want 1", model.schema.component.Structure.DiagramDepth)
	}
	view = ansi.Strip(model.foreignKeysView())
	if strings.Contains(view, "teams") {
		t.Fatalf("depth-1 diagram = %q, must not contain teams", view)
	}
}



func TestDiagramToggles_areMutuallyExclusive(t *testing.T) {
	// Given — the FK diagram is on.
	model := diagramFixture(t)
	model.Tab, model.Focus = tabForeignKeys, focusWorkspace
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	if !model.schema.component.Structure.RelationshipDiagram {
		t.Fatal("FK diagram did not enable")
	}

	// When — the indexes diagram turns on.
	model.Tab = tabIndexes
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)

	// Then — only the indexes diagram is active, so clicks hit-test
	// against the index cards' geometry.
	if !model.schema.component.Structure.IndexDiagram {
		t.Fatal("indexes diagram did not enable")
	}
	if model.schema.component.Structure.RelationshipDiagram {
		t.Fatal("FK diagram stayed on with the indexes diagram")
	}
}


func TestForeignKeysDiagram_showsCardinalityAndDirection(t *testing.T) {
	// Given — orders references customers twice (customer_id non-unique,
	// billing_id unique → mixed, so the card-level relation is N:1) and
	// invoices references orders (N:1).
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"orders": {
			{Columns: []string{"customer_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}},
			{Columns: []string{"billing_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}},
		},
		"invoices": {{Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}},
	}
	model.schema.indexesAll = map[string][]sharedsql.IndexInfo{
		"orders": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "orders_billing_key", Unique: true, Columns: []string{"billing_id"}},
		},
		"customers": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
		"invoices":  {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then — every connector between the hub and a neighbor carries the
	// relation as endpoint labels: "(N)" beside the child (FK holder),
	// "(1)" beside the parent (referenced table), and an upward arrow
	// glyph between them pointing from (1) to (N). The card mappings stay
	// on the cards.
	for _, text := range []string{
		"(N)",
		"(1)",
		"▲",
		"orders.customer_id → id", // outgoing: the hub's key is the left side
		"order_id → orders.id",    // incoming: the hub's key is the right side
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, want %q", view, text)
		}
	}
	if strings.Contains(view, "─▶") {
		t.Fatalf("diagram view = %q, the arrow must be the vertical glyph, not a horizontal text arrow", view)
	}
}

func TestForeignKeysDiagram_marksUniqueRelationOneToOne(t *testing.T) {
	// Given — a unique FK: a single unique index over exactly the FK
	// columns turns the edge into 1:1.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "users", tabForeignKeys, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"passports": {{Columns: []string{"user_id"}, ReferenceTable: "users", ReferenceColumns: []string{"id"}}},
	}
	model.schema.indexesAll = map[string][]sharedsql.IndexInfo{
		"passports": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "passports_user_key", Unique: true, Columns: []string{"user_id"}},
		},
		"users": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then — the 1:1 connector shows "(1)" at both ends: no N side at
	// all.
	if got := strings.Count(view, "(1)"); got != 2 {
		t.Fatalf("diagram view = %q, want the 1:1 label at both ends", view)
	}
	for _, text := range []string{"(N)", "─▶"} {
		if strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, must not claim an N relation", view)
		}
	}
}

func TestForeignKeysDiagram_bottomFanOutKeepsPerEdgeLabels(t *testing.T) {
	// Given — the hub references two tables on the bottom side with
	// different uniqueness: customers via a plain FK (N) and passports
	// via a unique FK (1:1). The child is the shared hub, so the child
	// labels stay per edge at the stub columns rather than collapsing
	// onto the hub column.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"orders": {
			{Columns: []string{"customer_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}},
			{Columns: []string{"passport_id"}, ReferenceTable: "passports", ReferenceColumns: []string{"id"}},
		},
	}
	model.schema.indexesAll = map[string][]sharedsql.IndexInfo{
		"orders": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "orders_passport_key", Unique: true, Columns: []string{"passport_id"}},
		},
		"customers": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
		"passports": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then — the bottom fan-out keeps every endpoint label per edge at
	// the stub columns, in the documented row order: merge bar, child
	// row, arrow row, parent row, cards. The child row carries both
	// labels ("(N)" for customers, "(1)" for the unique passports edge);
	// the arrow row has one glyph per edge; the parent row one "(1)" per
	// card.
	lines := strings.Split(view, "\n")
	find := func(fragment string) int {
		for index, line := range lines {
			if strings.Contains(line, fragment) {
				return index
			}
		}
		t.Fatalf("diagram view = %q, want a line containing %q", view, fragment)
		return -1
	}
	mergeIndex := find("┴")
	childIndex := find("(N)")
	arrowIndex := find("▲")
	// The child row carries the unique edge's "(1)" too, so the parent
	// row is the row after the arrow row with one "(1)" per card, and
	// the shaft row sits between arrow and parent.
	parentIndex := -1
	shaftIndex := -1
	for index := arrowIndex + 1; index < len(lines); index++ {
		if strings.Contains(lines[index], "│") && shaftIndex < 0 {
			shaftIndex = index
		}
		if strings.Count(lines[index], "(1)") == 2 {
			parentIndex = index
			break
		}
	}
	if parentIndex < 0 {
		t.Fatalf("diagram view = %q, want a parent row after the arrow row", view)
	}
	if !(mergeIndex < childIndex && childIndex < arrowIndex && arrowIndex < shaftIndex && shaftIndex < parentIndex) {
		t.Fatalf("diagram view = %q, row order must be merge < child < arrow < parent, got %d < %d < %d < %d", view, mergeIndex, childIndex, arrowIndex, parentIndex)
	}
	if got := strings.Count(lines[childIndex], "(N)"); got != 1 {
		t.Fatalf("child row = %q, want one (N) for the plain edge, got %d", lines[childIndex], got)
	}
	if got := strings.Count(lines[childIndex], "(1)"); got != 1 {
		t.Fatalf("child row = %q, want one (1) for the unique edge, got %d", lines[childIndex], got)
	}
	if got := strings.Count(lines[arrowIndex], "▲"); got != 2 {
		t.Fatalf("arrow row = %q, want one glyph per edge, got %d", lines[arrowIndex], got)
	}
	if got := strings.Count(lines[parentIndex], "(1)"); got != 2 {
		t.Fatalf("parent row = %q, want one (1) per card, got %d", lines[parentIndex], got)
	}
	if !(parentIndex < find("customers") && parentIndex < find("passports")) {
		t.Fatalf("diagram view = %q, the parent row must sit above the cards", view)
	}
	for _, text := range []string{"customer_id → id", "passport_id → id"} {
		if !strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, want %q", view, text)
		}
	}
}

func TestForeignKeysDiagram_omitsCardinalityWithoutIndexCache(t *testing.T) {
	// Given — the same graph but no index cache: labels are omitted rather
	// than guessed; the arrowheads stay.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"orders": {{Columns: []string{"customer_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}}},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then — without the index cache no cardinality is guessed: no
	// endpoint labels and no arrow glyph; the mapping stays.
	for _, text := range []string{"(N)", "(1)", "▲"} {
		if strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, must not render %q without the index cache", view, text)
		}
	}
	if !strings.Contains(view, "customer_id → id") {
		t.Fatalf("diagram view = %q, want the column mapping", view)
	}
}

func TestCommandPalette_diagramCommandsScopedToActiveDiagram(t *testing.T) {
	// Given — on the indexes tab without a diagram.
	model := diagramFixture(t)
	palette := newCommandPalette(model)
	found := map[CommandID]bool{}
	for _, item := range palette.items {
		found[item.id] = true
	}
	if !found["indexes.toggle_diagram"] {
		t.Fatal("palette lacks indexes.toggle_diagram on the indexes tab")
	}
	if found["diagram.depth_up"] {
		t.Fatal("palette offers diagram.depth_up without an active diagram")
	}

	// When — the diagram is active.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	found = map[CommandID]bool{}
	for _, item := range newCommandPalette(model).items {
		found[item.id] = true
	}
	if !found["diagram.depth_up"] || !found["diagram.depth_down"] {
		t.Fatal("palette lacks the depth commands with an active diagram")
	}
}
