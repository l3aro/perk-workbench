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

func TestDiagramClick_refocusesOnNeighborCard(t *testing.T) {
	// Given — the FK diagram with one incoming neighbor above the hub.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.schema.component.Structure.Columns = []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}}
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"invoices": {{Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)

	// Locate the invoices card in the rendered diagram: its title line and
	// its left border column.
	view := ansi.Strip(model.foreignKeysView())
	titleLine := -1
	titleX := -1
	for index, line := range strings.Split(view, "\n") {
		if column := strings.Index(line, "┌─ invoices"); column >= 0 {
			titleLine, titleX = index, column
			break
		}
	}
	if titleLine < 0 {
		t.Fatalf("diagram view = %q, want an invoices card", view)
	}

	// When — click inside the card. Screen Y = contentY + 1, and the
	// diagram starts at contentY 2, so a card line at diagram Y i is at
	// screen Y i+3.
	updated, _ = model.Update(tea.MouseClickMsg{X: titleX + 3, Y: titleLine + 3, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the focus follows the click: the table switches, the tab and
	// diagram stay active.
	if !strings.EqualFold(model.SelectedTable, "invoices") {
		t.Fatalf("SelectedTable = %q, want invoices", model.SelectedTable)
	}
	if model.Tab != tabForeignKeys {
		t.Fatalf("tab = %v, want foreign keys", model.Tab)
	}
	if !model.schema.component.Structure.RelationshipDiagram {
		t.Fatal("diagram turned off after refocus")
	}
}

func TestDiagramClick_ignoresGapsBetweenCards(t *testing.T) {
	// Given — two outgoing neighbors so there is a gap between the cards.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"orders": {
			{Columns: []string{"user_id"}, ReferenceTable: "users", ReferenceColumns: []string{"id"}},
			{Columns: []string{"team_id"}, ReferenceTable: "teams", ReferenceColumns: []string{"id"}},
		},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)

	// Click on the connector row between the two cards (inside the pane
	// body, diagram row 0): no card is there, so nothing refocuses.
	updated, _ = model.Update(tea.MouseClickMsg{X: 20, Y: 3, Button: tea.MouseLeft})
	model = updated.(Model)
	if !strings.EqualFold(model.SelectedTable, "orders") {
		t.Fatalf("gap click changed the selection to %q", model.SelectedTable)
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

func TestDiagramClick_mysqlCardNamesAreQualified(t *testing.T) {
	// Given — a MySQL connection: bulk cache keys are bare table names
	// while the selected table is database-qualified.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "office.orders", tabForeignKeys, focusWorkspace
	model.databaseInfo = sharedsql.DatabaseInfo{Product: "MySQL", Version: "8.0"}
	model.Target = "mysql:root:secret@tcp(db.example.test:3306)/office"
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"invoices": {{Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}},
	}
	model = resizeModel(model, 80, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)

	// Locate the invoices card in the rendered diagram.
	view := ansi.Strip(model.foreignKeysView())
	titleLine, titleX := -1, -1
	for index, line := range strings.Split(view, "\n") {
		if column := strings.Index(line, "┌─ invoices"); column >= 0 {
			titleLine, titleX = index, column
			break
		}
	}
	if titleLine < 0 {
		t.Fatalf("diagram view = %q, want an invoices card", view)
	}

	// When — click the card.
	updated, _ = model.Update(tea.MouseClickMsg{X: titleX + 3, Y: titleLine + 3, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the selection is database-qualified, so the schema tree's
	// open-path highlight stays in sync.
	if !strings.EqualFold(model.SelectedTable, "office.invoices") {
		t.Fatalf("SelectedTable = %q, want office.invoices", model.SelectedTable)
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
