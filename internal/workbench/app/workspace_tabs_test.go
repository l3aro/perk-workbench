package app

import (
	"slices"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestWorkspaceTabs_targetPolicy drives the tab policy for every target
// kind: the exact tab set, H/L wrapping only over visible tabs, and
// tab-row clicks selecting only rendered tabs.
func TestWorkspaceTabs_targetPolicy(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*testing.T) Model
		labels []string
	}{
		{name: "no target", labels: []string{"SQL"}, setup: func(t *testing.T) Model {
			return readyModel(t)
		}},
		{name: "table", labels: []string{"SQL", "Browse", "Columns", "Indexes", "Foreign Keys"}, setup: func(t *testing.T) Model {
			model := readyModel(t)
			model.SelectTable("projects")
			return model
		}},
		{name: "MySQL database", labels: []string{"SQL", "Browse", "Diagram"}, setup: func(t *testing.T) Model {
			model := readyModel(t)
			model.databaseInfo = sharedsql.DatabaseInfo{Product: "MySQL"}
			model.SelectDatabase("office")
			return model
		}},
		{name: "MongoDB database", labels: []string{"SQL", "Browse"}, setup: func(t *testing.T) Model {
			model := readyModel(t)
			model.databaseInfo = sharedsql.DatabaseInfo{Product: "MongoDB"}
			model.SelectDatabase("mydb")
			return model
		}},
		{name: "PostgreSQL connected database", labels: []string{"SQL", "Browse", "Diagram"}, setup: func(t *testing.T) Model {
			model := readyModel(t)
			model.databaseInfo = sharedsql.DatabaseInfo{Product: "PostgreSQL"}
			model.SelectDatabase("main")
			return model
		}},
		{name: "PostgreSQL schema", labels: []string{"SQL", "Browse", "Diagram"}, setup: func(t *testing.T) Model {
			model := readyModel(t)
			model.databaseInfo = sharedsql.DatabaseInfo{Product: "PostgreSQL"}
			model.SelectSchema("main", "public")
			return model
		}},
		{name: "non-PostgreSQL schema", labels: []string{"SQL"}, setup: func(t *testing.T) Model {
			model := readyModel(t)
			model.databaseInfo = sharedsql.DatabaseInfo{Product: "MySQL"}
			model.SelectSchema("office", "public")
			return model
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name+"/tab set", func(t *testing.T) {
			model := tc.setup(t)
			tabs := model.workspaceTabs()
			labels, _ := model.workspaceTabMeta(tabs)
			if !slices.Equal(labels, tc.labels) {
				t.Fatalf("workspace tab labels = %#v, want %#v", labels, tc.labels)
			}
		})
		t.Run(tc.name+"/H/L wrap", func(t *testing.T) {
			model := tc.setup(t)
			model.Focus = focusWorkspace
			tabs := model.workspaceTabs()
			for index, tab := range tabs {
				model.Tab = tab.standard
				model.workspace.active = ""
				updated, _ := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
				got := updated.(Model)
				if !got.workspaceTabActive(tabs[(index+1)%len(tabs)]) {
					t.Fatalf("L from %v = tab %v, want %v", tab, got.Tab, tabs[(index+1)%len(tabs)])
				}
				model.Tab = tab.standard
				model.workspace.active = ""
				updated, _ = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
				got = updated.(Model)
				if !got.workspaceTabActive(tabs[(index+len(tabs)-1)%len(tabs)]) {
					t.Fatalf("H from %v = tab %v, want %v", tab, got.Tab, tabs[(index+len(tabs)-1)%len(tabs)])
				}
			}
		})
		t.Run(tc.name+"/tab row clicks", func(t *testing.T) {
			model := tc.setup(t)
			model.Focus = focusWorkspace
			model = resizeModel(model, 80, 24)
			if !model.layout.compact {
				t.Fatal("test setup did not produce the compact layout")
			}
			tabs := model.workspaceTabs()
			_, widths := model.workspaceTabMeta(tabs)
			tabY := renderedRowY(t, model, tc.labels[0])
			cx := 2 // pane left border (1) + left padding (1)
			for i, tab := range tabs {
				x := cx + widths[i]/2
				updated, _ := model.Update(tea.MouseClickMsg{X: x, Y: tabY, Button: tea.MouseLeft})
				model = updated.(Model)
				if !model.workspaceTabActive(tab) {
					t.Fatalf("click on %q = tab %v, want %v", tc.labels[i], model.Tab, tab)
				}
				cx += widths[i]
			}
			// A click past the last rendered tab changes nothing.
			before := model.Tab
			updated, _ := model.Update(tea.MouseClickMsg{X: cx + 4, Y: tabY, Button: tea.MouseLeft})
			model = updated.(Model)
			if model.Tab != before {
				t.Fatalf("click past the tabs = %v, want unchanged %v", model.Tab, before)
			}
		})
	}
}

// TestWorkspaceTargets_databaseSelectionFromSidebar drives Enter on a MySQL
// root: the accordion toggles in the same interaction, the database target
// is set, prior table state clears, and the workspace defaults to Browse.
func TestWorkspaceTargets_databaseSelectionFromSidebar(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
	})

	// When — Enter on the analytics root.
	model.schema.component.List.Select(2)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — the accordion expanded analytics and collapsed office, and
	// the workspace targets the database on the Browse tab.
	if !model.schema.component.ExpandedDatabases["analytics"] || model.schema.component.ExpandedDatabases["office"] {
		t.Fatalf("accordion = %#v, want analytics expanded and office collapsed", model.schema.component.ExpandedDatabases)
	}
	if got, want := model.WorkspaceTarget, (core.WorkspaceTarget{Kind: core.WorkspaceDatabase, Database: "analytics"}); got != want {
		t.Fatalf("workspace target = %#v, want %#v", got, want)
	}
	if model.SelectedTable != "" {
		t.Fatalf("SelectedTable = %q, want cleared", model.SelectedTable)
	}
	if model.Tab != tabBrowse || model.Focus != focusWorkspace {
		t.Fatalf("workspace = tab %v focus %v, want Browse/workspace", model.Tab, model.Focus)
	}
}

// TestWorkspaceTargets_databaseClickFromSidebar drives a sidebar click on a
// MySQL root: the click path emits the same selection event as Enter.
func TestWorkspaceTargets_databaseClickFromSidebar(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
	})

	// When — a click on the analytics root row.
	rowY := findRenderedRow(t, model, "▣ analytics")
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: rowY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the root is selected and expanded with the database target.
	if !model.schema.component.ExpandedDatabases["analytics"] || model.schema.component.ExpandedDatabases["office"] {
		t.Fatalf("accordion = %#v, want analytics expanded and office collapsed", model.schema.component.ExpandedDatabases)
	}
	if got, want := model.WorkspaceTarget, (core.WorkspaceTarget{Kind: core.WorkspaceDatabase, Database: "analytics"}); got != want {
		t.Fatalf("workspace target = %#v, want %#v", got, want)
	}
	if model.Tab != tabBrowse || model.Focus != focusWorkspace || model.SelectedTable != "" {
		t.Fatalf("workspace = tab %v focus %v table %q, want Browse/workspace with no table", model.Tab, model.Focus, model.SelectedTable)
	}
}

// TestWorkspaceTargets_selectionClearsTableState seeds a table workspace
// (rows, structure, diagrams, an editing form mode) and verifies a scope
// selection clears every table-owned piece before landing on Browse.
func TestWorkspaceTargets_selectionClearsTableState(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
	})
	model.SelectTable("customers")
	model.browse.component.Result = sharedsql.Result{Columns: []string{"id"}, Rows: [][]*string{{stringPointer("1")}}}
	model.browse.component.Table.SetRows([]table.Row{{"1"}})
	model.browse.component.Pending = true
	model.browse.component.Structure = []sharedsql.ColumnInfo{{Name: "id"}}
	model.schema.component.Structure.Columns = []sharedsql.ColumnInfo{{Name: "id"}}
	model.schema.component.Structure.RelationshipDiagram = true
	model.schema.component.Structure.IndexDiagram = true
	model.overlay.formMode.Mode = formModeInsert

	// When — Enter on the analytics root from the sidebar.
	model.Focus = focusSchema
	model.schema.component.List.Select(2)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — the target is the database with every table-owned piece gone.
	if model.SelectedTable != "" || model.WorkspaceTarget.Kind != core.WorkspaceDatabase {
		t.Fatalf("target = %#v with table %q, want a cleared database target", model.WorkspaceTarget, model.SelectedTable)
	}
	if len(model.browse.component.Result.Rows) != 0 {
		t.Fatalf("browse kept prior table result rows: %#v", model.browse.component.Result.Rows)
	}
	if !model.browse.component.ObjectListMode() || len(model.browse.component.Table.Rows()) != 1 {
		t.Fatalf("browse did not replace prior rows with the scoped object list: mode %t rows %#v", model.browse.component.ObjectListMode(), model.browse.component.Table.Rows())
	}
	if model.browse.component.Pending || model.browse.component.FilterForm != nil || model.browse.component.Form.Active() {
		t.Fatalf("browse kept prior table state: pending %t filter %t form %t", model.browse.component.Pending, model.browse.component.FilterForm != nil, model.browse.component.Form.Active())
	}
	if model.schema.component.Structure.Columns != nil || model.schema.component.Structure.RelationshipDiagram || model.schema.component.Structure.IndexDiagram {
		t.Fatalf("structure kept prior table state: columns %#v diagrams %t/%t", model.schema.component.Structure.Columns, model.schema.component.Structure.RelationshipDiagram, model.schema.component.Structure.IndexDiagram)
	}
	if model.schema.component.Structure.ColumnForm.Active() || model.overlay.formMode.Mode != formModeNormal || model.overlay.formMode.ButtonsFocused {
		t.Fatalf("form state survived the scope switch: form %t mode %v buttons %t", model.schema.component.Structure.ColumnForm.Active(), model.overlay.formMode.Mode, model.overlay.formMode.ButtonsFocused)
	}
	if model.Tab != tabBrowse || model.Focus != focusWorkspace {
		t.Fatalf("workspace = tab %v focus %v, want Browse/workspace", model.Tab, model.Focus)
	}
}

// TestWorkspaceTargets_schemaSelectionFromSidebar drives Enter on a
// PostgreSQL schema: the accordion toggles and the schema target is set.
func TestWorkspaceTargets_schemaSelectionFromSidebar(t *testing.T) {
	model := postgresTreeModel(t)

	// public is expanded at load; Enter on it collapses it and selects
	// the schema target.
	model.schema.component.List.Select(1)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.schema.component.ExpandedSchemas[model.schemaExpansionKey("main", "public")] {
		t.Fatal("public schema stayed expanded after its toggle")
	}
	if got, want := model.WorkspaceTarget, (core.WorkspaceTarget{Kind: core.WorkspaceSchema, Database: "main", Schema: "public"}); got != want {
		t.Fatalf("workspace target = %#v, want %#v", got, want)
	}
	if model.SelectedTable != "" || model.Tab != tabBrowse || model.Focus != focusWorkspace {
		t.Fatalf("workspace = table %q tab %v focus %v, want cleared Browse/workspace", model.SelectedTable, model.Tab, model.Focus)
	}
}

// TestWorkspaceTargets_unconnectedPostgresRootReconnects verifies the
// preserved exception: activating an unconnected PostgreSQL root schedules
// reconnection and exposes no database/schema target (no Browse/Diagram).
func TestWorkspaceTargets_unconnectedPostgresRootReconnects(t *testing.T) {
	model, _ := reconnectPostgresModel(t)

	// When — Enter on the employers root, which is not the connected
	// database.
	model.schema.component.List.Select(2)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — the switch is in flight and the workspace target stays none.
	if !model.reconnectPending {
		t.Fatal("unconnected root did not schedule reconnection")
	}
	if command == nil {
		t.Fatal("unconnected root returned no open command")
	}
	if model.WorkspaceTarget.Kind != core.WorkspaceNone {
		t.Fatalf("workspace target = %#v, want none before reconnection completes", model.WorkspaceTarget)
	}
	if model.Tab != tabQuery || model.SelectedTable != "" {
		t.Fatalf("workspace = tab %v table %q, want SQL with no table", model.Tab, model.SelectedTable)
	}
}
