package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

func TestForeignKeysTab_togglesRelationshipDiagram_withIncomingAndOutgoingKeys(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.structureColumns = []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "customer_id"}}
	model.foreignKeyInfo = []sharedsql.ForeignKeyInfo{{Columns: []string{"customer_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}}}
	model.referencingForeignKeyInfo = []sharedsql.ReferencingForeignKeyInfo{{Table: "items", ForeignKeyInfo: sharedsql.ForeignKeyInfo{Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}}}
	model = resizeModel(model, 80, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := model.foreignKeysView()

	// Then
	for _, text := range []string{"orders", "customers", "items"} {
		if !strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, want %q", view, text)
		}
	}
	if got := strings.Count(ansi.Strip(view), "▼"); got != 2 {
		t.Fatalf("diagram connector count = %d, want 2 in %q", got, view)
	}
	if strings.Contains(view, "customer_id") || strings.Contains(view, "order_id") {
		t.Fatalf("diagram view = %q, want table-only connectors", view)
	}
}

func TestForeignKeysTab_rendersSelfReferenceOnce(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "tree", tabForeignKeys, focusWorkspace
	model.foreignKeyInfo = []sharedsql.ForeignKeyInfo{{Columns: []string{"parent_id"}, ReferenceTable: "tree", ReferenceColumns: []string{"id"}}}
	model.referencingForeignKeyInfo = []sharedsql.ReferencingForeignKeyInfo{{Table: "tree", ForeignKeyInfo: model.foreignKeyInfo[0]}}
	model = resizeModel(model, 80, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then
	if got := strings.Count(view, "↺"); got != 1 {
		t.Fatalf("self relationship count = %d, want 1 in %q", got, view)
	}
}

func TestForeignKeysTab_boundsFallbackRowsToTheWorkspace(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders_with_an_unusually_long_table_name_that_exceeds_the_workspace_viewport_width", tabForeignKeys, focusWorkspace
	model.foreignKeyInfo = []sharedsql.ForeignKeyInfo{{Columns: []string{"a_very_long_foreign_key_name"}, ReferenceTable: "a_very_long_reference_table_name", ReferenceColumns: []string{"a_very_long_primary_key_name"}}}
	model.referencingForeignKeyInfo = []sharedsql.ReferencingForeignKeyInfo{{Table: "a_very_long_referencing_table_name", ForeignKeyInfo: sharedsql.ForeignKeyInfo{Columns: []string{"another_very_long_foreign_key_name"}, ReferenceTable: "orders", ReferenceColumns: []string{"a_very_long_primary_key_name"}}}}
	model = resizeModel(model, 100, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > model.tableViewportWidth {
			t.Fatalf("fallback line width = %d, want <= %d: %q", ansi.StringWidth(line), model.tableViewportWidth, line)
		}
	}
	if got := len(strings.Split(view, "\n")); got > max(model.workspaceHeight-2, 1) {
		t.Fatalf("fallback line count = %d, want <= %d", got, max(model.workspaceHeight-2, 1))
	}
}

func TestForeignKeysTab_usesRelationshipList_whenDiagramExceedsWideHeight(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.structureColumns = []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}}
	model.foreignKeyInfo = []sharedsql.ForeignKeyInfo{
		{Columns: []string{"customer_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}},
		{Columns: []string{"invoice_id"}, ReferenceTable: "invoices", ReferenceColumns: []string{"id"}},
	}
	model.referencingForeignKeyInfo = []sharedsql.ReferencingForeignKeyInfo{
		{Table: "items", ForeignKeyInfo: sharedsql.ForeignKeyInfo{Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}},
		{Table: "payments", ForeignKeyInfo: sharedsql.ForeignKeyInfo{Columns: []string{"order_id"}, ReferenceTable: "orders", ReferenceColumns: []string{"id"}}},
	}
	model = resizeModel(model, 100, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := model.foreignKeysView()

	// Then
	if !strings.Contains(view, "Press f for full-screen diagram") {
		t.Fatalf("wide relationship view = %q, want full-screen affordance", view)
	}
}
