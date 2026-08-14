package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestMouseClick_pressReleaseActivatesOnce verifies one physical mouse
// click on a sidebar table row fires exactly one selection: the press
// activates the table (and moves focus to the workspace), the trailing
// release neither re-selects it nor yanks focus back to the sidebar. A
// lone release (no pending press) still focuses the sidebar pane.
func TestMouseClick_pressReleaseActivatesOnce(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
		{Database: "office", Type: "table", Name: "orders"},
	})
	model = resizeModel(model, 120, 40)

	// The tree is expanded with customers at index 1 and orders at 2; the
	// workspace starts on SQL. Click once on the customers row.
	model.schema.component.List.Select(1)
	rowY := findRenderedRow(t, model, "customers")

	// When — press selects the table; the returned command is the
	// selectSchemaTableBy load batch.
	updated, pressCmd := model.Update(tea.MouseClickMsg{X: 2, Y: rowY, Button: tea.MouseLeft})
	model = updated.(Model)
	if pressCmd == nil {
		t.Fatal("press did not return a selection command")
	}
	if model.SelectedTable != "office.customers" {
		t.Fatalf("SelectedTable = %q after press, want office.customers", model.SelectedTable)
	}
	if model.WorkspaceTarget.Kind != core.WorkspaceTable {
		t.Fatalf("workspace target = %#v after press, want table", model.WorkspaceTarget)
	}

	// When — the trailing release must not activate the table again.
	updated, releaseCmd := model.Update(tea.MouseReleaseMsg{X: 2, Y: rowY, Button: tea.MouseLeft})
	model = updated.(Model)
	if releaseCmd != nil {
		t.Fatal("release returned a command; the press already activated the table")
	}

	// Then — the selection survives and focus stays on the workspace, the
	// state the press's table selection established.
	if model.SelectedTable != "office.customers" {
		t.Fatalf("SelectedTable = %q after release, want office.customers", model.SelectedTable)
	}
	if model.WorkspaceTarget.Kind != core.WorkspaceTable {
		t.Fatalf("workspace target = %#v after release, want table", model.WorkspaceTarget)
	}
	if model.Focus != focusWorkspace {
		t.Fatalf("focus = %v after release, want focusWorkspace", model.Focus)
	}
}

// TestMouseClick_compactPressReleaseDoesNotReenterWorkspace verifies the
// compact-layout variant: after a sidebar press selects a table, focus
// moves to the workspace, so the trailing release must not route through
// the workspace branch (which would re-activate the tab row).
func TestMouseClick_compactPressReleaseDoesNotReenterWorkspace(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
		{Database: "office", Type: "table", Name: "orders"},
	})
	// Compact: the sidebar and workspace share one full-width pane, routed
	// by the focused pane instead of by x coordinates.
	model = resizeModel(model, 80, 24)
	if !model.layout.compact {
		t.Fatal("fixture: model is not compact at 80x24")
	}

	// When — press selects the customers row in the sidebar.
	model.schema.component.List.Select(1)
	rowY := findRenderedRow(t, model, "customers")
	updated, pressCmd := model.Update(tea.MouseClickMsg{X: 2, Y: rowY, Button: tea.MouseLeft})
	model = updated.(Model)
	if pressCmd == nil {
		t.Fatal("press did not return a selection command")
	}
	if model.SelectedTable != "office.customers" {
		t.Fatalf("SelectedTable = %q after press, want office.customers", model.SelectedTable)
	}

	// When — the trailing release arrives at the same position; focus is
	// now on the workspace pane.
	updated, releaseCmd := model.Update(tea.MouseReleaseMsg{X: 2, Y: rowY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — no second activation, focus stays on the workspace.
	if releaseCmd != nil {
		t.Fatal("release returned a command; the press already activated the table")
	}
	if model.Focus != focusWorkspace {
		t.Fatalf("focus = %v after release, want focusWorkspace", model.Focus)
	}
	if model.SelectedTable != "office.customers" {
		t.Fatalf("SelectedTable = %q after release, want office.customers", model.SelectedTable)
	}

	// When — a second sidebar press, then the release lands on the compact
	// workspace tab row (Y=2, where the tab row renders). Focus is on the
	// workspace after the press, so without the trailing-release guard the
	// release would route into the workspace branch and switch to the SQL
	// tab (X=2 hits the first tab).
	model.Focus = focusSchema
	model.schema.component.List.Select(2)
	ordersY := model.schema.component.RowY(2, model.schemaLayout()) + 1 // contentY -> terminal Y
	updated, pressCmd = model.Update(tea.MouseClickMsg{X: 2, Y: ordersY, Button: tea.MouseLeft})
	model = updated.(Model)
	if pressCmd == nil {
		t.Fatal("second press did not return a selection command")
	}
	updated, releaseCmd = model.Update(tea.MouseReleaseMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	model = updated.(Model)
	if releaseCmd != nil {
		t.Fatal("tab-row release returned a command; the sidebar press already activated the table")
	}
	if model.Tab != tableOpenTargetTab() {
		t.Fatalf("tab = %v after tab-row release, want the landing tab %v", model.Tab, tableOpenTargetTab())
	}
	if model.SelectedTable != "office.orders" {
		t.Fatalf("SelectedTable = %q, want office.orders", model.SelectedTable)
	}
}

// TestMouseClick_loneReleaseFocusesSidebar verifies a release without a
// preceding sidebar press still focuses the sidebar pane (matching the
// palette swallow tests), without activating any item.
func TestMouseClick_loneReleaseFocusesSidebar(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
	})
	model = resizeModel(model, 120, 40)
	model.Focus = focusWorkspace

	// When — a lone release on the sidebar row.
	rowY := findRenderedRow(t, model, "customers")
	updated, cmd := model.Update(tea.MouseReleaseMsg{X: 2, Y: rowY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the sidebar pane is focused but no table is selected.
	if cmd != nil {
		t.Fatal("lone release returned a command")
	}
	if model.Focus != focusSchema {
		t.Fatalf("focus = %v, want focusSchema", model.Focus)
	}
	if model.SelectedTable != "" {
		t.Fatalf("SelectedTable = %q, want empty", model.SelectedTable)
	}
}
