package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// mysqlScopeObjects is a mixed MySQL fixture: two databases, each with a
// table and a view.
func mysqlScopeObjects() []sharedsql.SchemaObject {
	return []sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers", RowCount: int64Ptr(12500)},
		{Database: "office", Type: "view", Name: "vip_customers"},
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
		{Database: "analytics", Type: "view", Name: "daily_events"},
	}
}

// postgresScopeObjects is a mixed PostgreSQL fixture: two populated
// schemas and one empty schema under the connected database.
func postgresScopeObjects() []sharedsql.SchemaObject {
	return []sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts", RowCount: int64Ptr(490000)},
		{Database: "main", Type: "table", Name: "public.orders"},
		{Database: "main", Type: "schema", Name: "archive"},
		{Database: "main", Type: "table", Name: "archive.audit"},
		{Database: "main", Type: "schema", Name: "staging"},
	}
}

// schemaObjectsEqual compares schema object lists by value (RowCount is a
// pointer the fixtures recreate per call).
func schemaObjectsEqual(a, b []sharedsql.SchemaObject) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Database != b[i].Database || a[i].Type != b[i].Type || a[i].Name != b[i].Name {
			return false
		}
		if (a[i].RowCount == nil) != (b[i].RowCount == nil) {
			return false
		}
		if a[i].RowCount != nil && *a[i].RowCount != *b[i].RowCount {
			return false
		}
	}
	return true
}

func int64Ptr(n int64) *int64 { return &n }

// TestScopeBrowse_objectListFiltering drives the scope object list for
// every target kind: only the target's own table/view or collection rows
// appear, with name, kind, and abbreviated row count rendered.
func TestScopeBrowse_objectListFiltering(t *testing.T) {
	t.Run("MySQL database scope", func(t *testing.T) {
		model := serverProductModel(t, "MySQL", &createDatabaseStub{})
		_ = model.setSchemaObjects(mysqlScopeObjects())
		model.selectDatabaseTarget("office")

		want := []sharedsql.SchemaObject{
			{Database: "office", Type: "table", Name: "customers", RowCount: int64Ptr(12500)},
			{Database: "office", Type: "view", Name: "vip_customers"},
		}
		assertScopeObjects(t, model, want)
		view := ansi.Strip(model.workspaceView())
		for _, present := range []string{"customers", "table", "12.5k", "vip_customers", "view"} {
			if !strings.Contains(view, present) {
				t.Fatalf("workspace view misses %q: %q", present, view)
			}
		}
		for _, absent := range []string{"analytics", "events", "daily_events"} {
			if strings.Contains(view, absent) {
				t.Fatalf("workspace view leaks %q: %q", absent, view)
			}
		}
	})

	t.Run("PostgreSQL database scope", func(t *testing.T) {
		model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
		_ = model.setSchemaObjects(postgresScopeObjects())
		model.selectDatabaseTarget("main")
		model = resizeModel(model, 120, 40)

		want := []sharedsql.SchemaObject{
			{Database: "main", Type: "table", Name: "public.accounts", RowCount: int64Ptr(490000)},
			{Database: "main", Type: "table", Name: "public.orders"},
			{Database: "main", Type: "table", Name: "archive.audit"},
		}
		assertScopeObjects(t, model, want)
		view := ansi.Strip(model.workspaceView())
		for _, present := range []string{"public.accounts", "490k", "archive.audit"} {
			if !strings.Contains(view, present) {
				t.Fatalf("workspace view misses %q: %q", present, view)
			}
		}
		// The empty staging schema has no table rows to leak.
		if strings.Contains(view, "staging") {
			t.Fatalf("workspace view leaks the empty schema %q: %q", "staging", view)
		}
	})

	t.Run("PostgreSQL schema scope", func(t *testing.T) {
		model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
		_ = model.setSchemaObjects(postgresScopeObjects())
		model.selectSchemaTarget("main", "archive")

		want := []sharedsql.SchemaObject{
			{Database: "main", Type: "table", Name: "archive.audit"},
		}
		assertScopeObjects(t, model, want)
		view := ansi.Strip(model.workspaceView())
		if !strings.Contains(view, "archive.audit") || strings.Contains(view, "accounts") {
			t.Fatalf("schema scope view = %q, want archive.audit without accounts", view)
		}
	})

	t.Run("MongoDB database scope", func(t *testing.T) {
		model := serverProductModel(t, "MongoDB", &createDatabaseStub{})
		_ = model.setSchemaObjects([]sharedsql.SchemaObject{
			{Database: "mydb", Type: "database", Name: "mydb"},
			{Database: "mydb", Type: "collection", Name: "users"},
			{Database: "mydb", Type: "collection", Name: "orders"},
		})
		model.selectDatabaseTarget("mydb")

		want := []sharedsql.SchemaObject{
			{Database: "mydb", Type: "collection", Name: "users"},
			{Database: "mydb", Type: "collection", Name: "orders"},
		}
		assertScopeObjects(t, model, want)
		view := ansi.Strip(model.workspaceView())
		if !strings.Contains(view, "users") || !strings.Contains(view, "collection") || !strings.Contains(view, "2 objects") {
			t.Fatalf("MongoDB scope view = %q, want users/collection rows with a count", view)
		}
	})
}

// TestScopeBrowse_paletteHidesTableRowCommands guards against stale table
// actions remaining executable while Browse renders a database/schema object list.
func TestScopeBrowse_paletteHidesTableRowCommands(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(mysqlScopeObjects())
	model.selectDatabaseTarget("office")

	hidden := map[CommandID]bool{
		"browse.edit":       true,
		"browse.edit_cell":  true,
		"browse.refine":     true,
		"browse.reset":      true,
		"browse.sort":       true,
		"browse.next_page":  true,
		"browse.prev_page":  true,
		"browse.insert_row": true,
		"cell.view":         true,
		"cell.yank":         true,
	}
	for _, item := range newCommandPalette(model).items {
		if hidden[item.id] {
			t.Fatalf("scope object list palette includes table-row command %q", item.id)
		}
	}
}

func assertScopeObjects(t *testing.T, model Model, want []sharedsql.SchemaObject) {
	t.Helper()
	if !model.browse.component.ObjectListMode() {
		t.Fatal("browse pane is not in object-list mode")
	}
	if got := model.browse.component.Objects; !schemaObjectsEqual(got, want) {
		t.Fatalf("scope objects = %#v, want %#v", got, want)
	}
}

// TestScopeBrowse_enterOpensTable drives Enter on a listed object: the
// existing selectSchemaTableBy path opens the normal table workspace
// (table target, landing tab, pending row browse) and leaves object mode.
func TestScopeBrowse_enterOpensTable(t *testing.T) {
	t.Run("MySQL", func(t *testing.T) {
		model := serverProductModel(t, "MySQL", &createDatabaseStub{})
		_ = model.setSchemaObjects(mysqlScopeObjects())
		model.selectDatabaseTarget("office")

		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)

		if got, want := model.SelectedTable, "office.customers"; got != want {
			t.Fatalf("SelectedTable = %q, want %q", got, want)
		}
		if model.Tab != tableOpenTargetTab() {
			t.Fatalf("Tab = %v, want the table open target %v", model.Tab, tableOpenTargetTab())
		}
		if model.WorkspaceTarget.Kind != core.WorkspaceTable {
			t.Fatalf("workspace target = %v, want table", model.WorkspaceTarget.Kind)
		}
		if model.browse.component.ObjectListMode() {
			t.Fatal("browse pane stayed in object-list mode after opening")
		}
		if !model.browse.component.Pending {
			t.Fatal("browse row load is not pending for the opened table")
		}
	})

	t.Run("PostgreSQL opens the cursor row", func(t *testing.T) {
		model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
		_ = model.setSchemaObjects(postgresScopeObjects())
		model.selectSchemaTarget("main", "public")

		// Move the cursor to the second object, then open it.
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updated.(Model)
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)

		if got, want := model.SelectedTable, "public.orders"; got != want {
			t.Fatalf("SelectedTable = %q, want %q", got, want)
		}
	})

	t.Run("MongoDB collection does not open", func(t *testing.T) {
		model := serverProductModel(t, "MongoDB", &createDatabaseStub{})
		_ = model.setSchemaObjects([]sharedsql.SchemaObject{
			{Database: "mydb", Type: "database", Name: "mydb"},
			{Database: "mydb", Type: "collection", Name: "users"},
		})
		model.selectDatabaseTarget("mydb")

		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)

		if model.SelectedTable != "" {
			t.Fatalf("Enter on a collection opened %q; only tables/views open", model.SelectedTable)
		}
		if !model.browse.component.ObjectListMode() {
			t.Fatal("Enter on a collection left object-list mode")
		}
	})
}

// TestScopeBrowse_doubleClickOpensTable drives a double-click on a listed
// object: the second click at the same position opens the table
// workspace, matching the Enter keybinding.
func TestScopeBrowse_doubleClickOpensTable(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(mysqlScopeObjects())
	model.selectDatabaseTarget("office")

	// Workspace pane: header y=1, tab row y=2, blank y=3, table header
	// y=4, first object row y=5.
	x, y := model.layout.schemaWidth+5, 5
	updated, _ := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)

	if got, want := model.SelectedTable, "office.customers"; got != want {
		t.Fatalf("double-click SelectedTable = %q, want %q", got, want)
	}
	if model.browse.component.ObjectListMode() {
		t.Fatal("browse pane stayed in object-list mode after double-click")
	}

	// A single click on a later row only selects it.
	model = scopeModelWithOffice(t)
	x2, y2 := model.layout.schemaWidth+5, 6 // second object row
	updated, _ = model.Update(tea.MouseClickMsg{X: x2, Y: y2, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.browse.component.Table.Cursor(); got != 1 {
		t.Fatalf("single click cursor = %d, want 1 (second object)", got)
	}
	if model.SelectedTable != "" {
		t.Fatalf("single click opened %q, want selection only", model.SelectedTable)
	}
}

func scopeModelWithOffice(t *testing.T) Model {
	t.Helper()
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(mysqlScopeObjects())
	model.selectDatabaseTarget("office")
	return model
}

// TestScopeBrowse_objectCrudActions drives the object-list context menu:
// right-click and the "," key offer Add/Edit/Delete with the scope
// qualification, and the existing handlers (table form, delete
// confirmation) receive the qualified target.
func TestScopeBrowse_objectCrudActions(t *testing.T) {
	t.Run("MySQL menu and add form", func(t *testing.T) {
		model := scopeModelWithOffice(t)
		updated, _ := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 5, Y: 5, Button: tea.MouseRight})
		model = updated.(Model)

		menu := model.overlay.contextMenu
		if menu == nil || len(menu.options) != 3 || menu.options[0].action != "add_table" || menu.options[1].action != "rename_table" || menu.options[2].action != "delete_table" || menu.database != "office" || menu.table != "customers" {
			t.Fatalf("object menu = %+v, want Add/Edit/Delete in office on customers", menu)
		}

		// a dispatches through the existing table form, qualified with
		// the scope database.
		updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		model = updated.(Model)
		model = runTableCommand(model, command)
		form := model.schema.component.Structure.TableForm
		if !form.Active() || form.Database != "office" || form.Name != "" {
			t.Fatalf("add form = %+v, want active create form in office", form)
		}
	})

	t.Run("MySQL rename form", func(t *testing.T) {
		model := scopeModelWithOffice(t)
		updated, _ := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 5, Y: 5, Button: tea.MouseRight})
		model = updated.(Model)
		updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		model = updated.(Model)
		model = runTableCommand(model, command)
		form := model.schema.component.Structure.TableForm
		if !form.Active() || form.Database != "office" || form.OriginalName != "customers" {
			t.Fatalf("rename form = %+v, want customers in office", form)
		}
	})

	t.Run("MySQL delete confirmation", func(t *testing.T) {
		model := scopeModelWithOffice(t)
		updated, _ := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 5, Y: 5, Button: tea.MouseRight})
		model = updated.(Model)
		updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
		model = updated.(Model)
		if model.overlay.deleteConfirm == nil || model.overlay.deleteConfirm.Description != "DROP TABLE `office`.`customers`" {
			t.Fatalf("delete confirmation = %+v, want DROP TABLE `office`.`customers`", model.overlay.deleteConfirm)
		}
	})

	t.Run("PostgreSQL schema qualification", func(t *testing.T) {
		model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
		_ = model.setSchemaObjects(postgresScopeObjects())
		model.selectSchemaTarget("main", "public")

		updated, _ := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 5, Y: 5, Button: tea.MouseRight})
		model = updated.(Model)
		menu := model.overlay.contextMenu
		if menu == nil || len(menu.options) != 3 || menu.database != "public" || menu.table != "public.accounts" {
			t.Fatalf("object menu = %+v, want Add/Edit/Delete qualified by public", menu)
		}

		updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
		model = updated.(Model)
		if model.overlay.deleteConfirm == nil || model.overlay.deleteConfirm.Description != `DROP TABLE "public"."accounts"` {
			t.Fatalf("delete confirmation = %+v, want DROP TABLE \"public\".\"accounts\"", model.overlay.deleteConfirm)
		}
	})

	t.Run("MongoDB menu and delete", func(t *testing.T) {
		model := serverProductModel(t, "MongoDB", &createDatabaseStub{})
		_ = model.setSchemaObjects([]sharedsql.SchemaObject{
			{Database: "mydb", Type: "database", Name: "mydb"},
			{Database: "mydb", Type: "collection", Name: "users"},
		})
		model.selectDatabaseTarget("mydb")

		updated, _ := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 5, Y: 5, Button: tea.MouseRight})
		model = updated.(Model)
		menu := model.overlay.contextMenu
		if menu == nil || len(menu.options) != 3 || menu.options[0].action != "add_table" || menu.options[1].action != "rename_table" || menu.options[2].action != "delete_table" || menu.database != "mydb" || menu.table != "users" {
			t.Fatalf("collection menu = %+v, want Add/Edit/Delete in mydb on users", menu)
		}

		updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
		model = updated.(Model)
		if model.overlay.deleteConfirm == nil || model.overlay.deleteConfirm.Description != `DROP TABLE "users"` {
			t.Fatalf("delete confirmation = %+v, want DROP TABLE \"users\"", model.overlay.deleteConfirm)
		}
	})

	t.Run("context menu key opens the same menu", func(t *testing.T) {
		model := scopeModelWithOffice(t)
		updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
		model = updated.(Model)
		menu := model.overlay.contextMenu
		if menu == nil || len(menu.options) != 3 || menu.database != "office" || menu.table != "customers" {
			t.Fatalf("context-menu key opened %+v, want the customers menu", menu)
		}
	})
}

// TestScopeBrowse_emptyScope verifies the empty scope: the pane renders
// its empty state, Enter opens nothing, and no object CRUD actions are
// available (context-menu key and right-click open no menu).
func TestScopeBrowse_emptyScope(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(postgresScopeObjects())
	// staging has no objects in this fixture.
	model.selectSchemaTarget("main", "staging")

	if !model.browse.component.ObjectListMode() {
		t.Fatal("browse pane is not in object-list mode")
	}
	if got := model.browse.component.Objects; len(got) != 0 {
		t.Fatalf("empty scope objects = %#v, want none", got)
	}
	view := ansi.Strip(model.workspaceView())
	if !strings.Contains(view, "no objects") {
		t.Fatalf("empty scope view = %q, want the no-objects state", view)
	}

	// Enter opens nothing.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.SelectedTable != "" {
		t.Fatalf("Enter on an empty scope opened %q", model.SelectedTable)
	}
	if model.browse.component.ObjectListMode() == false {
		t.Fatal("Enter on an empty scope left object mode")
	}

	// The context-menu key and a right-click open no menu.
	updated, _ = model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatalf("context-menu key opened a menu on an empty scope: %+v", model.overlay.contextMenu)
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 5, Y: 5, Button: tea.MouseRight})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatalf("right-click opened a menu on an empty scope: %+v", model.overlay.contextMenu)
	}
}

// TestScopeBrowse_scopeSelectionClearsRowBrowse verifies the scope object
// list replaces a previously loaded table-row browse: switching targets
// re-filters the list and stale row data is gone.
func TestScopeBrowse_scopeSelectionClearsRowBrowse(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(mysqlScopeObjects())
	// Open a table first: table-row browse state is loaded.
	model.selectDatabaseTarget("office")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.SelectedTable != "office.customers" {
		t.Fatalf("setup: SelectedTable = %q, want office.customers", model.SelectedTable)
	}

	// Selecting another scope clears the table workspace and re-filters.
	model.selectDatabaseTarget("analytics")
	if model.SelectedTable != "" {
		t.Fatalf("SelectedTable = %q, want cleared", model.SelectedTable)
	}
	if got := model.browse.component.Objects; len(got) != 2 || got[0].Name != "events" {
		t.Fatalf("analytics scope objects = %#v, want the analytics rows", got)
	}
	if model.Tab != tabBrowse {
		t.Fatalf("Tab = %v, want Browse", model.Tab)
	}
}
