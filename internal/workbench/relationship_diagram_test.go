package workbench

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
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
	view := ansi.Strip(model.foreignKeysView())

	// Then — the selected table is the hub: the referencing table above it,
	// the referenced table below, both connectors labeled with the mapping.
	for _, text := range []string{"orders", "customers", "items", "🔑 id", "🔗 customer_id", "order_id → id", "customer_id → id"} {
		if !strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, want %q", view, text)
		}
	}
	if got := strings.Count(view, "┌"); got != 3 {
		t.Fatalf("card count = %d, want 3 in %q", got, view)
	}
	// The outgoing connector attaches to the selected card's bottom border.
	if got := strings.Count(view, "┬"); got != 1 {
		t.Fatalf("connector join count = %d, want 1 in %q", got, view)
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

	// Then — the self-loop renders once on the selected card, with mapping.
	if got := strings.Count(view, "↺"); got != 1 {
		t.Fatalf("self relationship count = %d, want 1 in %q", got, view)
	}
	if !strings.Contains(view, "↺ parent_id → id") {
		t.Fatalf("self relationship = %q, want column mapping", view)
	}
}

func TestForeignKeysTab_mergesMultipleForeignKeysToTheSameTable(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.foreignKeyInfo = []sharedsql.ForeignKeyInfo{
		{Columns: []string{"customer_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}},
		{Columns: []string{"billing_id"}, ReferenceTable: "customers", ReferenceColumns: []string{"id"}},
	}
	model = resizeModel(model, 80, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then — one card for the neighbor, both mappings in its caption.
	if got := strings.Count(view, "┌"); got != 2 {
		t.Fatalf("card count = %d, want 2 in %q", got, view)
	}
	for _, text := range []string{"customer_id → id", "billing_id → id"} {
		if !strings.Contains(view, text) {
			t.Fatalf("diagram view = %q, want %q", view, text)
		}
	}
}

func TestForeignKeysTab_boundsFallbackRowsToTheWorkspace(t *testing.T) {
	// Given — a diagram so wide the hub cannot fit the workspace viewport.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	model.foreignKeyInfo = []sharedsql.ForeignKeyInfo{
		{Columns: []string{"a_very_long_foreign_key_column_name"}, ReferenceTable: "a_very_long_reference_table_name", ReferenceColumns: []string{"a_very_long_primary_key_column_name"}},
		{Columns: []string{"another_very_long_foreign_key_column_name"}, ReferenceTable: "another_very_long_reference_table_name", ReferenceColumns: []string{"another_very_long_primary_key_column_name"}},
	}
	model = resizeModel(model, 100, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	view := ansi.Strip(model.foreignKeysView())

	// Then — the list fallback stays inside the workspace viewport.
	if !strings.Contains(view, "relationships") {
		t.Fatalf("fallback view = %q, want relationship list", view)
	}
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
	// Given — a table with many key columns so the hub is taller than the
	// workspace viewport.
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "orders", tabForeignKeys, focusWorkspace
	columns := make([]sharedsql.ColumnInfo, 0, 24)
	for index := range 24 {
		columns = append(columns, sharedsql.ColumnInfo{Name: fmt.Sprintf("column_number_%02d", index), PrimaryKey: index + 1})
	}
	model.structureColumns = columns
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
