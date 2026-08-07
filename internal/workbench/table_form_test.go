package workbench

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// runTableCommand executes one command and its children through the model,
// expanding tea.BatchMsg like the runtime, until the model settles. The bound
// contains blink/validation tick chains that the runtime would keep re-arming.
func runTableCommand(model Model, command tea.Cmd) Model {
	for range 8 {
		if command == nil {
			return model
		}
		message := command()
		if message == nil {
			return model
		}
		if batch, ok := message.(tea.BatchMsg); ok {
			for _, child := range batch {
				model = runTableCommand(model, child)
			}
			return model
		}
		updated, next := model.Update(message)
		model, command = updated.(Model), next
	}
	return model
}

func createTableInSchema(t *testing.T, model Model, table string) Model {
	t.Helper()
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE "+table+" (id INTEGER)"); err != nil {
		t.Fatalf("creating table %s: %v", table, err)
	}
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "table", Name: table},
	})
	return model
}

func findRenderedRow(t *testing.T, model Model, label string) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	for y, line := range lines {
		if strings.Contains(line, label) {
			return y
		}
	}
	t.Fatalf("rendered view does not contain %q", label)
	return -1
}

func schemaTableNames(t *testing.T, model Model) []string {
	t.Helper()
	objects, err := model.Database.ListSchema(context.Background())
	if err != nil {
		t.Fatalf("listing schema: %v", err)
	}
	var names []string
	for _, object := range objects {
		if object.Type == "table" {
			names = append(names, object.Name)
		}
	}
	return names
}

func TestSchemaRename_viaM_renamesAndRefreshesSidebar(t *testing.T) {
	// Given: a schema with one table "old" and schema focus on it.
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "old")
	model.schema.Select(1)

	// When: m opens the rename popup prefilled with the selected table.
	updated, command := model.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if !model.tableForm.active() || model.tableForm.name != "old" || model.tableForm.objectKind != tableFormTable || model.tableForm.database != "main" {
		t.Fatalf("rename popup = %+v, want prefilled old", model.tableForm)
	}

	// When: rename to "new" and save; the confirmation carries the exact DDL.
	model.tableForm.name = "new"
	updated, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	model = updated.(Model)
	if !model.tableForm.confirming() || model.formMode.mode != formModeConfirm {
		t.Fatalf("ctrl+s did not confirm: confirming=%t mode=%d", model.tableForm.confirming(), model.formMode.mode)
	}
	if got, want := model.tableForm.confirmation.description, `ALTER TABLE "old" RENAME TO "new"`; got != want {
		t.Fatalf("confirmation description = %q, want %q", got, want)
	}

	// When: accept; the ALTER runs and the sidebar refreshes.
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if model.tableForm.active() || model.tableFormRunning {
		t.Fatal("popup still open after successful rename")
	}
	names := schemaTableNames(t, model)
	if len(names) != 1 || names[0] != "new" {
		t.Fatalf("database tables = %v, want only new", names)
	}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "└ new") || strings.Contains(view, "└ old") {
		t.Fatalf("sidebar did not refresh: %q", view)
	}
}

func TestSchemaRename_unchangedNameClosesWithoutSQL(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "old")
	model.schema.Select(1)

	updated, command := model.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	model = updated.(Model)
	model = runTableCommand(model, command)

	// When: save with the unchanged name.
	updated, command = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then: the popup closes with no confirmation, no query, no mutation.
	if model.tableForm.active() {
		t.Fatal("unchanged rename left the popup open")
	}
	if command != nil {
		t.Fatalf("unchanged rename produced a command: %v", command)
	}
	if model.tableForm.confirming() || model.Running() {
		t.Fatal("unchanged rename opened a confirmation or query")
	}
	if names := schemaTableNames(t, model); len(names) != 1 || names[0] != "old" {
		t.Fatalf("database mutated by unchanged rename: %v", names)
	}
}

func TestSchemaAddTable_viaA_createsAndRefreshesSidebar(t *testing.T) {
	for _, selection := range []string{"table", "root"} {
		t.Run(selection, func(t *testing.T) {
			model := resizeModel(readyModel(t), 100, 24)
			model.Focus = focusSchema
			model = createTableInSchema(t, model, "accounts")
			if selection == "table" {
				model.schema.Select(1)
			} else {
				model.schema.Select(0)
			}

			// When: a opens an empty popup for the selection's database.
			updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
			model = updated.(Model)
			model = runTableCommand(model, command)
			if !model.tableForm.active() || model.tableForm.name != "" || model.tableForm.objectKind != tableFormTable || model.tableForm.database != "main" {
				t.Fatalf("create popup = %+v, want empty in main", model.tableForm)
			}
			model.Focus = focusWorkspace
			model.Tab = tabSQL
			model.formMode.mode = formModeInsert
			for _, key := range "created" {
				updated, _ = model.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
				model = updated.(Model)
			}
			if model.tableForm.name != "created" {
				t.Fatalf("typed table name = %q", model.tableForm.name)
			}
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			if got, want := model.tableForm.confirmation.description, `CREATE TABLE "created" (id INTEGER PRIMARY KEY)`; got != want {
				t.Fatalf("confirmation description = %q, want %q", got, want)
			}
			updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			model = runTableCommand(model, command)
			if model.tableFormRunning || model.Running() {
				t.Fatal("create query still running")
			}
			if model.tableForm.active() {
				t.Fatalf("successful create left popup open: %+v", model.tableForm)
			}
			if names := schemaTableNames(t, model); len(names) != 2 || names[0] != "accounts" || names[1] != "created" {
				t.Fatalf("database tables = %v, want accounts and created", names)
			}
		})
	}
}

func TestSchemaTableForm_enterSavesFromNormalMode(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "accounts")
	model.schema.Select(1)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	model.tableForm.name = "created"
	model.formMode.mode = formModeNormal

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if !model.tableForm.confirming() {
		t.Fatal("Enter in normal mode did not open confirmation")
	}
}

func TestSchemaTableForm_blankNameShowsValidationError(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "accounts")

	model.schema.Select(1)

	updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	model.tableForm.name = "   "

	// When: save a blank name.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then: no confirmation, the popup stays, and the validation error shows.
	if model.tableForm.confirming() {
		t.Fatal("blank name opened a confirmation")
	}
	if !model.tableForm.active() {
		t.Fatal("blank name closed the popup")
	}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "table name is required") {
		t.Fatalf("validation error not rendered: %q", view)
	}
}

func TestTableForm_isCompact(t *testing.T) {
	form := newTableForm("main", "")
	form.setWidth(100)
	form.setHeight(18)

	if lines := len(strings.Split(ansi.Strip(form.View()), "\n")); lines >= form.height {
		t.Fatalf("table form rendered %d lines at height %d", lines, form.height)
	}
}

func TestSchemaTableForm_declinedConfirmationKeepsPopup(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "accounts")
	model.schema.Select(1)

	updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	model.tableForm.name = "created"
	updated, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	model = updated.(Model)
	if !model.tableForm.confirming() {
		t.Fatal("ctrl+s did not open the confirmation")
	}

	// When: decline.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)

	// Then: the popup stays editable with the typed name, schema unchanged.
	if model.tableForm.confirming() || !model.tableForm.active() {
		t.Fatalf("decline closed the popup: confirming=%t active=%t", model.tableForm.confirming(), model.tableForm.active())
	}
	if model.tableForm.name != "created" {
		t.Fatalf("decline lost the typed name: %q", model.tableForm.name)
	}
	if names := schemaTableNames(t, model); len(names) != 1 || names[0] != "accounts" {
		t.Fatalf("declined create mutated the database: %v", names)
	}
}

func TestSchemaDelete_viaD_dropsTable(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "old")
	model.schema.Select(1)

	// When: d opens the delete confirmation with the exact DROP.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.deleteConfirm == nil {
		t.Fatal("d did not open the delete confirmation")
	}
	if !strings.Contains(model.deleteConfirm.description, `DROP TABLE "old"`) {
		t.Fatalf("delete description = %q, want DROP TABLE \"old\"", model.deleteConfirm.description)
	}

	// When: decline; the table stays.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if model.deleteConfirm != nil {
		t.Fatal("decline left the confirmation open")
	}
	if names := schemaTableNames(t, model); len(names) != 1 || names[0] != "old" {
		t.Fatalf("declined delete mutated the database: %v", names)
	}

	// When: accept; the DROP runs and the sidebar refreshes.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if names := schemaTableNames(t, model); len(names) != 0 {
		t.Fatalf("accepting delete left tables: %v", names)
	}
	if view := ansi.Strip(model.View().Content); strings.Contains(view, "└ old") {
		t.Fatalf("sidebar still shows old: %q", view)
	}
}

func TestSchemaContextMenu_renameDeleteViaRightClick(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "old")
	tableY := findRenderedRow(t, model, "└ old")

	// When: right-click the table row.
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: tableY, Button: tea.MouseRight})
	model = updated.(Model)

	// Then: the menu carries Rename and Delete with the row's target.
	menu := model.contextMenu
	if menu == nil || !menu.visible {
		t.Fatal("right-click did not open a menu")
	}
	if len(menu.options) != 2 || menu.options[0].label != "Rename table" || menu.options[0].keys != "r" ||
		menu.options[1].label != "Delete table" || menu.options[1].keys != "d" {
		t.Fatalf("menu options = %+v, want Rename (r) and Delete (d)", menu.options)
	}
	if menu.database != "main" || menu.table != "old" {
		t.Fatalf("menu target = %s.%s, want main.old", menu.database, menu.table)
	}

	// When: r opens the prefilled rename popup.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if model.contextMenu != nil {
		t.Fatal("menu stayed open after r")
	}
	if !model.tableForm.active() || model.tableForm.name != "old" {
		t.Fatalf("r did not open the rename popup: %+v", model.tableForm)
	}

	// Close the popup (Escape exits insert mode, then closes).
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.tableForm.active() {
		t.Fatal("Escape did not close the popup")
	}

	// When: right-click again and press d; the delete confirmation appears.
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: tableY, Button: tea.MouseRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.deleteConfirm == nil || !strings.Contains(model.deleteConfirm.description, `DROP TABLE "old"`) {
		t.Fatalf("d did not open the delete confirmation: %+v", model.deleteConfirm)
	}

	// When: decline once, then accept; the table is dropped and refreshed.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if names := schemaTableNames(t, model); len(names) != 1 || names[0] != "old" {
		t.Fatalf("decline removed the table: %v", names)
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: tableY, Button: tea.MouseRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if names := schemaTableNames(t, model); len(names) != 0 {
		t.Fatalf("accepting delete left tables: %v", names)
	}
}

func TestSchemaContextMenu_rootAndBlankSpaceAdd(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "accounts")
	rootY := findRenderedRow(t, model, "▾ main")

	// When: right-click the database root row.
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: rootY, Button: tea.MouseRight})
	model = updated.(Model)

	// Then: only Add table, targeted at that root.
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 1 || menu.options[0].label != "Add table" || menu.options[0].keys != "a" || menu.database != "main" {
		t.Fatalf("root menu = %+v, want Add table in main", menu)
	}

	// When: a opens the empty create popup for the root's database.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if !model.tableForm.active() || model.tableForm.name != "" || model.tableForm.database != "main" {
		t.Fatalf("root add popup = %+v, want empty in main", model.tableForm)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// When: right-click blank space below the rows; Add uses the current
	// selection's database.
	model.schema.Select(1)
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: 20, Button: tea.MouseRight})
	model = updated.(Model)
	if model.contextMenu == nil || len(model.contextMenu.options) != 1 || model.contextMenu.options[0].action != "add_table" || model.contextMenu.database != "main" {
		t.Fatalf("blank-space menu = %+v, want Add in main", model.contextMenu)
	}
}

// createDatabaseStub accepts CREATE DATABASE statements and reports a fixed
// schema listing, standing in for a MySQL/PostgreSQL connection.
type createDatabaseStub struct {
	sharedsql.Service
	statements []string
	objects    []sharedsql.SchemaObject
}

func (s *createDatabaseStub) Close() error { return nil }

func (s *createDatabaseStub) Execute(_ context.Context, statement string) (sharedsql.Result, error) {
	s.statements = append(s.statements, statement)
	return sharedsql.Result{}, nil
}

func (s *createDatabaseStub) ListSchema(_ context.Context) ([]sharedsql.SchemaObject, error) {
	return s.objects, nil
}

// serverProductModel builds a ready model backed by a stub reporting the
// given server product (MySQL or PostgreSQL).
func serverProductModel(t *testing.T, product string, stub *createDatabaseStub) Model {
	t.Helper()
	model := New("", context.Background(), func(_ context.Context, _ string) (sharedsql.Opened, error) {
		return sharedsql.Opened{Service: stub, Info: sharedsql.DatabaseInfo{Product: product, Version: "16"}}, nil
	}, false)
	model.queryLogPath = t.TempDir() + "/data.db"
	model.queryLogEntries = nil
	model.renderQueryLog()
	model.State, model.Database = stateReady, stub
	model.databaseInfo = sharedsql.DatabaseInfo{Product: product}
	model.Focus = focusSchema
	return resizeModel(model, 100, 24)
}

func TestSchemaCreateDatabase_shiftAOpensDatabaseForm(t *testing.T) {
	for _, product := range []string{"MySQL", "PostgreSQL"} {
		t.Run(product, func(t *testing.T) {
			model := serverProductModel(t, product, &createDatabaseStub{})
			updated, command := model.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
			model = updated.(Model)
			model = runTableCommand(model, command)
			if !model.tableForm.active() || model.tableForm.objectKind != tableFormDatabase {
				t.Fatalf("Shift+A did not open the create-database popup: %+v", model.tableForm)
			}
		})
	}
}

func TestSchemaCreateDatabase_ignoredForSQLite(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	model = updated.(Model)
	if model.tableForm.active() {
		t.Fatal("Shift+A opened a create-database popup on SQLite")
	}
}

func TestPostgresTree_connectedRootRecognizedWithoutSchemas(t *testing.T) {
	model := New("", context.Background(), func(_ context.Context, _ string) (sharedsql.Opened, error) {
		return sharedsql.Opened{}, nil
	}, false)
	model.Target = "postgres://alice:secret@db.example.test:5433/employees?sslmode=disable"
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "employees", Type: "database", Name: "employees"},
		{Database: "employers", Type: "database", Name: "employers"},
	})

	// After dropping the connected database's last schema, the target URL
	// still identifies the connected root; it must not regain Edit/Delete.
	if !model.databaseRootConnected("employees") {
		t.Fatal("connected root not recognized without schema children")
	}
	if model.databaseRootConnected("employers") {
		t.Fatal("unconnected root recognized as connected")
	}
}

func TestSchemaCreateDatabase_flowConfirmsRunsAndRefreshes(t *testing.T) {
	tests := []struct {
		product string
		quote   string
	}{
		{product: "PostgreSQL", quote: `"`},
		{product: "MySQL", quote: "`"},
	}
	for _, tt := range tests {
		t.Run(tt.product, func(t *testing.T) {
			stub := &createDatabaseStub{}
			model := serverProductModel(t, tt.product, stub)

			updated, command := model.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
			model = updated.(Model)
			model = runTableCommand(model, command)
			model.tableForm.name = "orders"

			// Save opens the confirmation with the exact DDL.
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			if !model.tableForm.confirming() {
				t.Fatal("Enter did not open the create-database confirmation")
			}
			want := "CREATE DATABASE " + tt.quote + "orders" + tt.quote
			if got := model.tableForm.confirmation.description; got != want {
				t.Fatalf("confirmation DDL = %q, want %q", got, want)
			}

			// Accept: the DDL runs and the sidebar refreshes with the
			// new database.
			stub.objects = []sharedsql.SchemaObject{{Database: "orders", Type: "database", Name: "orders"}}
			updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			model = runTableCommand(model, command)
			if model.tableForm.active() || model.tableFormRunning {
				t.Fatal("popup still open after successful create database")
			}
			if len(stub.statements) != 1 || stub.statements[0] != want {
				t.Fatalf("executed statements = %#v, want %q", stub.statements, want)
			}
			if !model.expandedDatabases["orders"] {
				t.Fatal("new database is not in the refreshed sidebar")
			}
			if view := ansi.Strip(model.View().Content); !strings.Contains(view, "orders") {
				t.Fatalf("sidebar did not show the new database: %q", view)
			}
		})
	}
}

func TestSchemaContextMenu_blankSpaceOffersCreateDatabase(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})

	// Without any schema items or selection, blank-space right-click
	// offers Add new database only; the A shortcut opens the popup.
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: 20, Button: tea.MouseRight})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || !menu.visible {
		t.Fatal("blank-space right-click did not open a menu")
	}
	if len(menu.options) != 1 || menu.options[0].action != "create_database" || menu.options[0].label != "Add new database" || menu.options[0].keys != "A" {
		t.Fatalf("blank-space menu = %+v, want Add new database", menu.options)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	model = updated.(Model)
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormDatabase {
		t.Fatal("menu shortcut A did not open the create-database popup")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// With a selection the blank menu still offers Add new database only:
	// the selection's own menu appears on its row.
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
	})
	model.schema.Select(1)
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: 20, Button: tea.MouseRight})
	model = updated.(Model)
	menu = model.contextMenu
	if menu == nil || len(menu.options) != 1 || menu.options[0].action != "create_database" {
		t.Fatalf("selected blank-space menu = %+v, want Add new database", menu.options)
	}
}

// postgresTreeModel builds a ready PostgreSQL model whose sidebar shows two
// databases, with the connected database ("main") holding schema "public"
// and its table.
func postgresTreeModel(t *testing.T) Model {
	t.Helper()
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
		{Database: "archive", Type: "database", Name: "archive"},
	})
	return model
}

func TestPostgresTree_rendersDatabaseSchemaAndTableLevels(t *testing.T) {
	model := postgresTreeModel(t)
	view := ansi.Strip(model.View().Content)
	for _, label := range []string{"▾ main", "  ▾ public", "    └ accounts", "▾ archive"} {
		if !strings.Contains(view, label) {
			t.Fatalf("postgres sidebar = %q, want %q", view, label)
		}
	}
}

func TestPostgresTree_schemaToggleCollapsesTables(t *testing.T) {
	model := postgresTreeModel(t)

	// Click the schema row: its tables hide behind a collapsed marker.
	schemaY := findRenderedRow(t, model, "  ▾ public")
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: schemaY, Button: tea.MouseLeft})
	model = updated.(Model)
	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "  ▸ public") || strings.Contains(view, "└ accounts") {
		t.Fatalf("collapsed postgres sidebar = %q, want ▸ public without tables", view)
	}

	// Enter on the selected schema node expands it again.
	model.schema.Select(1)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	view = ansi.Strip(model.View().Content)
	if !strings.Contains(view, "  ▾ public") || !strings.Contains(view, "    └ accounts") {
		t.Fatalf("re-expanded postgres sidebar = %q, want public with tables", view)
	}
}

// reconnectPostgresModel builds a ready PostgreSQL model whose sidebar
// shows employees (connected, with a public schema) and employers roots,
// capturing every target the injected open function receives.
func reconnectPostgresModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	opened := &[]string{}
	model := New("", context.Background(), func(_ context.Context, target string) (sharedsql.Opened, error) {
		*opened = append(*opened, target)
		return sharedsql.Opened{Service: &createDatabaseStub{}, Info: sharedsql.DatabaseInfo{Product: "PostgreSQL", Version: "16"}}, nil
	}, false)
	model.Target = "postgres://alice:secret@db.example.test:5433/employees?sslmode=disable"
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "employees", Type: "database", Name: "employees"},
		{Database: "employees", Type: "schema", Name: "public"},
		{Database: "employers", Type: "database", Name: "employers"},
	})
	model = resizeModel(model, 100, 30)
	model.State, model.Focus = stateReady, focusSchema
	model.databaseInfo = sharedsql.DatabaseInfo{Product: "PostgreSQL", Version: "16"}
	return model, opened
}

func TestPostgresTree_enterOnUnconnectedRootReconnects(t *testing.T) {
	model, opened := reconnectPostgresModel(t)

	// When — Enter on the employers root.
	model.schema.Select(2)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — the session reopens against employers.
	if model.State != stateOpening {
		t.Fatalf("state = %v, want opening", model.State)
	}
	if command == nil {
		t.Fatal("Enter on an unconnected root returned no command")
	}
	command()
	if len(*opened) != 1 || (*opened)[0] != "postgres://alice:secret@db.example.test:5433/employers?sslmode=disable" {
		t.Fatalf("reopened target = %v, want the employees URL rewritten to employers", *opened)
	}
}

func TestPostgresTree_enterOnConnectedRootToggles(t *testing.T) {
	model, opened := reconnectPostgresModel(t)

	// Enter on the connected employees root collapses it instead of
	// reconnecting.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.State != stateReady || len(*opened) != 0 {
		t.Fatalf("connected root Enter = state %v, opens %v, want ready and no reopen", model.State, *opened)
	}
	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "▸ employees") || strings.Contains(view, "▾ employees") {
		t.Fatalf("sidebar = %q, want collapsed employees root", view)
	}
}

func TestPostgresTree_doubleClickOnUnconnectedRootReconnects(t *testing.T) {
	model, opened := reconnectPostgresModel(t)

	// A double-click on the employers root reconnects, matching the
	// recent list's double-click-to-load convention.
	model.schema.Select(2)
	y := findRenderedRow(t, model, "▾ employers")
	updated, command := model.Update(tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, command = model.Update(tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)

	if model.State != stateOpening {
		t.Fatalf("state = %v, want opening after double-click", model.State)
	}
	if command == nil {
		t.Fatal("double-click on an unconnected root returned no command")
	}
	command()
	if len(*opened) != 1 || (*opened)[0] != "postgres://alice:secret@db.example.test:5433/employers?sslmode=disable" {
		t.Fatalf("reopened target = %v, want the employees URL rewritten to employers", *opened)
	}

	// A single click on the same row must not reconnect: the latch is
	// reset by the completed double-click, and the toggle command it
	// returns must not reopen the database.
	model.State, model.Focus = stateReady, focusSchema
	model.lastFormClickTime = time.Time{}
	opensBefore := len(*opened)
	updated, _ = model.Update(tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.State != stateReady || len(*opened) != opensBefore {
		t.Fatalf("single click = state %v, opens %d, want ready and no reopen", model.State, len(*opened))
	}
}

func TestPostgresTree_rebuildKeepsCursorOnConnectedRoot(t *testing.T) {
	model, _ := reconnectPostgresModel(t)

	// Simulate the reopen against employers completing: updateOpen sets
	// the target from the reopened URL, then rebuilds the tree.
	model.Target = "postgres://alice:secret@db.example.test:5433/employers?sslmode=disable"
	model.databaseInfo = sharedsql.DatabaseInfo{Product: "PostgreSQL", Version: "16"}
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "employees", Type: "database", Name: "employees"},
		{Database: "employers", Type: "database", Name: "employers"},
		{Database: "employers", Type: "schema", Name: "public"},
	})

	// employees still sorts first; the cursor must land on the connected
	// employers root, not on the first item.
	if got := model.schema.Index(); got != 1 {
		t.Fatalf("cursor = %d, want the connected employers root at 1", got)
	}
}

func TestPostgresTree_contextMenuConnectSwitchesDatabase(t *testing.T) {
	model, opened := reconnectPostgresModel(t)

	// Right-click the employers root: Connect is offered first.
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: findRenderedRow(t, model, "▾ employers"), Button: tea.MouseRight})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 4 || menu.options[0].action != "connect_database" || menu.options[1].action != "create_database" || menu.options[2].action != "rename_database" || menu.options[3].action != "delete_database" || menu.database != "employers" {
		t.Fatalf("root menu = %+v, want Connect, Add new database, Edit database, Delete database on employers", menu)
	}

	// Enter activates Connect: the session reopens against employers.
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("state = %v, want opening", model.State)
	}
	if command == nil {
		t.Fatal("Connect returned no command")
	}
	command()
	if len(*opened) != 1 || (*opened)[0] != "postgres://alice:secret@db.example.test:5433/employers?sslmode=disable" {
		t.Fatalf("reopened target = %v, want the employees URL rewritten to employers", *opened)
	}
}

func TestPostgresTree_reconnectDoesNotRecordProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model, opened := reconnectPostgresModel(t)

	// Enter on employers reopens; the switch must not add a profile.
	model.schema.Select(2)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("Enter on an unconnected root returned no command")
	}
	message := command()
	updated, _ = model.Update(message)
	model = updated.(Model)

	if len(model.recentConnections) != 0 {
		t.Fatalf("reconnect recorded %d profiles, want none", len(model.recentConnections))
	}
	if len(*opened) != 1 {
		t.Fatalf("reopened %v targets, want 1", *opened)
	}
}

func TestPostgresTargetFor_escapesDatabaseNameOnce(t *testing.T) {
	model := New("", context.Background(), func(_ context.Context, _ string) (sharedsql.Opened, error) {
		return sharedsql.Opened{}, nil
	}, false)
	model.Target = "postgres://alice:secret@db.example.test:5433/employees?sslmode=disable"
	got := model.postgresTargetFor("my db")
	want := "postgres://alice:secret@db.example.test:5433/my%20db?sslmode=disable"
	if got != want {
		t.Fatalf("postgresTargetFor() = %q, want %q", got, want)
	}
	if target := model.postgresTargetFor("x"); strings.Contains(target, "%25") {
		t.Fatalf("postgresTargetFor() double-escaped the database name: %q", target)
	}
}

func TestSchemaAddTable_onPostgresSchemaNodeTargetsSchema(t *testing.T) {
	model := postgresTreeModel(t)
	model.schema.Select(1) // the public schema node

	// a opens the create popup with the schema as its target.
	updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if !model.tableForm.active() || model.tableForm.database != "public" {
		t.Fatalf("add popup = %+v, want target public", model.tableForm)
	}

	// Saving confirms the schema-qualified DDL.
	model.tableForm.name = "new"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if got, want := model.tableForm.confirmation.description, `CREATE TABLE "public"."new" (id INTEGER PRIMARY KEY)`; got != want {
		t.Fatalf("confirmation DDL = %q, want %q", got, want)
	}
}

func TestSchemaContextMenu_mysqlRootOffersCreateDatabaseAndAddTable(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "app", Type: "database", Name: "app"},
	})
	rootY := findRenderedRow(t, model, "▾ app")

	// Right-click the database root: Create database then Add table.
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: rootY, Button: tea.MouseRight})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 3 || menu.options[0].action != "create_database" || menu.options[0].keys != "A" || menu.options[1].action != "add_table" || menu.options[2].action != "delete_database" || menu.database != "app" {
		t.Fatalf("mysql root menu = %+v, want Add new database, Add new table, Delete database in app", menu.options)
	}

	// A opens the create-database popup.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	model = updated.(Model)
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormDatabase {
		t.Fatal("A did not open the create-database popup")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// a opens the add-table popup targeted at the root's database.
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: rootY, Button: tea.MouseRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormTable || model.tableForm.database != "app" {
		t.Fatalf("a did not open the add-table popup in app: %+v", model.tableForm)
	}
}

func TestSchemaAddTable_ignoredOnPostgresDatabaseRoot(t *testing.T) {
	model := postgresTreeModel(t)

	// A database root has no schema to create in: a is a no-op, but
	// right-click still offers Create database.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.tableForm.active() {
		t.Fatal("a opened an add-table popup on a PostgreSQL database root")
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: findRenderedRow(t, model, "▾ archive"), Button: tea.MouseRight})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 4 || menu.options[0].action != "connect_database" || menu.options[1].action != "create_database" || menu.options[2].action != "rename_database" || menu.options[3].action != "delete_database" || menu.database != "archive" {
		t.Fatalf("database-root right-click menu = %+v, want Connect, Add new database, Edit database, Delete database", menu)
	}

	// The connected database root keeps its menu: Add new database and
	// Add new schema only, since PostgreSQL cannot rename or drop the
	// active database.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: findRenderedRow(t, model, "▾ main"), Button: tea.MouseRight})
	model = updated.(Model)
	menu = model.contextMenu
	if menu == nil || len(menu.options) != 2 || menu.options[0].action != "create_database" || menu.options[1].action != "create_schema" {
		t.Fatalf("connected-root right-click menu = %+v, want Add new database and Add new schema", menu)
	}
}

func TestSchemaContextMenu_postgresSchemaRowOffersSiblingChildEditDelete(t *testing.T) {
	model := postgresTreeModel(t)
	model.schema.Select(1) // the public schema row

	// , opens the schema menu: sibling, child, edit, delete.
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 4 {
		t.Fatalf("schema row menu = %+v, want four options", menu)
	}
	want := []struct{ label, action, keys string }{
		{"Add new schema", "create_schema", "A"},
		{"Add new table", "add_table", "a"},
		{"Edit schema", "rename_schema", "r"},
		{"Delete schema", "delete_schema", "d"},
	}
	for index, expected := range want {
		option := menu.options[index]
		if option.label != expected.label || option.action != expected.action || option.keys != expected.keys {
			t.Fatalf("option %d = %+v, want %+v", index, option, expected)
		}
	}
	if menu.database != "public" || menu.schema != "public" {
		t.Fatalf("menu target = %s schema %s, want public/public", menu.database, menu.schema)
	}

	// r opens the prefilled schema rename popup.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if model.contextMenu != nil {
		t.Fatal("menu stayed open after r")
	}
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormSchema || model.tableForm.name != "public" {
		t.Fatalf("r did not open the schema rename popup: %+v", model.tableForm)
	}
}

func TestSchemaContextMenu_postgresTableRowOffersSiblingEditDelete(t *testing.T) {
	model := postgresTreeModel(t)
	model.schema.Select(2) // the public.accounts table row

	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 3 {
		t.Fatalf("postgres table row menu = %+v, want three options", menu)
	}
	if menu.options[0].action != "add_table" || menu.options[1].action != "rename_table" || menu.options[2].action != "delete_table" {
		t.Fatalf("postgres table row menu = %+v, want Add new table, Edit table, Delete table", menu.options)
	}
	// The Add new table target is the table's schema, not the database.
	if menu.database != "public" || menu.table != "public.accounts" {
		t.Fatalf("postgres table menu target = %s table %s, want public/public.accounts", menu.database, menu.table)
	}

	// a opens the add popup inside that schema.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormTable || model.tableForm.database != "public" {
		t.Fatalf("a did not open the add popup in public: %+v", model.tableForm)
	}
}

func TestSchemaContextMenu_emptySidebarCommaOffersAddNewDatabase(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})

	// , with no sidebar rows still opens the blank menu.
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || len(menu.options) != 1 || menu.options[0].action != "create_database" || menu.options[0].label != "Add new database" {
		t.Fatalf("empty-sidebar comma menu = %+v, want Add new database", menu)
	}
}

func TestSchemaCreateSchema_flowConfirmsRunsAndRefreshes(t *testing.T) {
	stub := &createDatabaseStub{}
	model := serverProductModel(t, "PostgreSQL", stub)
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
	})
	model.schema.Select(1)
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormSchema {
		t.Fatal("A did not open the create-schema popup")
	}

	// Save opens the confirmation with the exact DDL.
	model.tableForm.name = "audit"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !model.tableForm.confirming() {
		t.Fatal("Enter did not open the create-schema confirmation")
	}
	if got, want := model.tableForm.confirmation.description, `CREATE SCHEMA "audit"`; got != want {
		t.Fatalf("confirmation DDL = %q, want %q", got, want)
	}

	// Accept: the DDL runs and the sidebar refreshes with the schema.
	stub.objects = []sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "schema", Name: "audit"},
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if model.tableForm.active() || model.tableFormRunning {
		t.Fatal("popup still open after successful create schema")
	}
	if len(stub.statements) != 1 || stub.statements[0] != `CREATE SCHEMA "audit"` {
		t.Fatalf("executed statements = %#v, want CREATE SCHEMA \"audit\"", stub.statements)
	}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "audit") {
		t.Fatalf("sidebar did not show the new schema: %q", view)
	}
}

func TestSchemaRenameSchema_flowConfirmsRunsAndRefreshes(t *testing.T) {
	stub := &createDatabaseStub{}
	model := serverProductModel(t, "PostgreSQL", stub)
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
	})
	model.schema.Select(1)
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if !model.tableForm.active() || model.tableForm.name != "public" {
		t.Fatalf("r did not open the prefilled rename popup: %+v", model.tableForm)
	}

	model.tableForm.name = "renamed"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if got, want := model.tableForm.confirmation.description, `ALTER SCHEMA "public" RENAME TO "renamed"`; got != want {
		t.Fatalf("confirmation DDL = %q, want %q", got, want)
	}

	stub.objects = []sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "renamed"},
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if model.tableForm.active() || model.tableFormRunning {
		t.Fatal("popup still open after successful rename schema")
	}
	if len(stub.statements) != 1 || stub.statements[0] != `ALTER SCHEMA "public" RENAME TO "renamed"` {
		t.Fatalf("executed statements = %#v, want the ALTER SCHEMA", stub.statements)
	}
	if view := ansi.Strip(model.View().Content); strings.Contains(view, "  ▾ public") || !strings.Contains(view, "renamed") {
		t.Fatalf("sidebar did not show the renamed schema: %q", view)
	}
}

func TestSchemaRenameDatabase_flowConfirmsRunsAndRefreshes(t *testing.T) {
	stub := &createDatabaseStub{}
	model := postgresTreeModel(t)
	model.Database = stub
	model.schema.Select(3) // the archive root
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormDatabase || model.tableForm.name != "archive" {
		t.Fatalf("r did not open the prefilled database rename popup: %+v", model.tableForm)
	}

	model.tableForm.name = "holdings"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if got, want := model.tableForm.confirmation.description, `ALTER DATABASE "archive" RENAME TO "holdings"`; got != want {
		t.Fatalf("confirmation DDL = %q, want %q", got, want)
	}

	stub.objects = []sharedsql.SchemaObject{
		{Database: "holdings", Type: "database", Name: "holdings"},
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if model.tableForm.active() || model.tableFormRunning {
		t.Fatal("popup still open after successful rename database")
	}
	if len(stub.statements) != 1 || stub.statements[0] != `ALTER DATABASE "archive" RENAME TO "holdings"` {
		t.Fatalf("executed statements = %#v, want the ALTER DATABASE", stub.statements)
	}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "holdings") {
		t.Fatalf("sidebar did not show the renamed database: %q", view)
	}
}

func TestSchemaDeleteSchema_confirmsRestrictAndRefreshes(t *testing.T) {
	stub := &createDatabaseStub{}
	model := postgresTreeModel(t)
	model.Database = stub
	model.schema.Select(1) // the public schema row
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.deleteConfirm == nil || model.deleteConfirm.description != `DROP SCHEMA "public" RESTRICT` {
		t.Fatalf("d did not open the DELETE SCHEMA confirmation: %+v", model.deleteConfirm)
	}

	stub.objects = []sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "archive", Type: "database", Name: "archive"},
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if len(stub.statements) != 1 || stub.statements[0] != `DROP SCHEMA "public" RESTRICT` {
		t.Fatalf("executed statements = %#v, want the DROP SCHEMA", stub.statements)
	}
	if view := ansi.Strip(model.View().Content); strings.Contains(view, "▾ public") {
		t.Fatalf("sidebar still shows the dropped schema: %q", view)
	}
}

func TestSchemaDeleteDatabase_confirmsAndRefreshes(t *testing.T) {
	tests := []struct {
		product string
		quote   string
	}{
		{product: "PostgreSQL", quote: `"`},
		{product: "MySQL", quote: "`"},
	}
	for _, tt := range tests {
		t.Run(tt.product, func(t *testing.T) {
			stub := &createDatabaseStub{}
			model := serverProductModel(t, tt.product, stub)
			_ = model.setSchemaObjects([]sharedsql.SchemaObject{
				{Database: "app", Type: "database", Name: "app"},
			})
			model.schema.Select(0)
			updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
			model = updated.(Model)
			want := "DROP DATABASE " + tt.quote + "app" + tt.quote
			if model.deleteConfirm == nil || model.deleteConfirm.description != want {
				t.Fatalf("d did not open the DELETE DATABASE confirmation: %+v", model.deleteConfirm)
			}

			updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
			model = updated.(Model)
			model = runTableCommand(model, command)
			if len(stub.statements) != 1 || stub.statements[0] != want {
				t.Fatalf("executed statements = %#v, want %q", stub.statements, want)
			}
		})
	}
}

func TestSchemaContextMenu_shiftedShortcutMatchesRealTerminal(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	if model.contextMenu == nil || len(model.contextMenu.options) != 1 || model.contextMenu.options[0].keys != "A" {
		t.Fatalf("blank menu = %+v, want Add new database with A", model.contextMenu)
	}

	// A real terminal reports shift+a with the shift modifier; the menu
	// must still trigger the A shortcut.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "A", Mod: tea.ModShift})
	model = updated.(Model)
	if model.contextMenu != nil {
		t.Fatal("menu stayed open after shifted A")
	}
	if !model.tableForm.active() || model.tableForm.objectKind != tableFormDatabase {
		t.Fatal("shifted A did not open the create-database popup")
	}
}

func TestSchemaRenameTable_postgresPrefillsBareName(t *testing.T) {
	stub := &createDatabaseStub{}
	model := serverProductModel(t, "PostgreSQL", stub)
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
	})
	model.schema.Select(2) // the accounts table row
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	model = runTableCommand(model, command)

	// The popup edits the bare name; the qualified name stays the target.
	if !model.tableForm.active() || model.tableForm.name != "accounts" || model.tableForm.originalName != "accounts" || model.tableForm.table != "public.accounts" {
		t.Fatalf("rename popup = %+v, want bare accounts prefilled", model.tableForm)
	}
	model.tableForm.name = "customers"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if got, want := model.tableForm.confirmation.description, `ALTER TABLE "public"."accounts" RENAME TO "customers"`; got != want {
		t.Fatalf("confirmation DDL = %q, want %q", got, want)
	}

	stub.objects = []sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.customers"},
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if model.tableForm.active() || model.tableFormRunning {
		t.Fatal("popup still open after successful rename")
	}
	if len(stub.statements) != 1 || stub.statements[0] != `ALTER TABLE "public"."accounts" RENAME TO "customers"` {
		t.Fatalf("executed statements = %#v, want the ALTER TABLE", stub.statements)
	}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "customers") || strings.Contains(view, "accounts") {
		t.Fatalf("sidebar did not show the renamed table: %q", view)
	}
}

func TestSchemaContextMenu_viewsExposeNoMenu(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE old (id INTEGER)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "CREATE VIEW v1 AS SELECT 1"); err != nil {
		t.Fatalf("creating view: %v", err)
	}
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "table", Name: "old"},
		{Database: "main", Type: "view", Name: "v1"},
	})
	model.schema.Select(2)
	viewY := findRenderedRow(t, model, "└ v1")

	// Right-click and the d/r/m/a keys do nothing on a view.
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: viewY, Button: tea.MouseRight})
	model = updated.(Model)
	if model.contextMenu != nil {
		t.Fatalf("view right-click opened a menu: %+v", model.contextMenu)
	}
	for _, key := range []tea.KeyPressMsg{
		{Code: 'd', Text: "d"}, {Code: 'r', Text: "r"}, {Code: 'm', Text: "m"}, {Code: 'a', Text: "a"},
	} {
		updated, _ = model.Update(key)
		model = updated.(Model)
	}
	if model.tableForm.active() || model.deleteConfirm != nil {
		t.Fatal("view exposed a table action")
	}
}

func TestSchemaContextMenu_filteredMapping(t *testing.T) {
	model := resizeModel(New("", context.Background(), testOpen, false), 100, 24)
	model.State, model.Focus = stateReady, focusSchema
	model.schema.SetItems([]list.Item{
		schemaItem{title: "accounts", description: "table", database: "main", table: "accounts", kind: "table"},
		schemaItem{title: "queue_1", description: "table", database: "main", table: "queue_1", kind: "table"},
	})
	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)
	model = updateFromCommand(model, command)
	updated, command = model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)
	model = updateFromCommand(model, command)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// The filtered render still maps to the right item.
	rowY := findRenderedRow(t, model, "queue_1")
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: rowY, Button: tea.MouseRight})
	model = updated.(Model)
	menu := model.contextMenu
	if menu == nil || menu.table != "queue_1" || len(menu.options) != 2 {
		t.Fatalf("filtered right-click menu = %+v, want queue_1 table menu", menu)
	}
}

func TestTableFormStatement_quoting(t *testing.T) {
	model := readyModel(t)
	tests := []struct {
		name     string
		product  string
		form     tableForm
		expected string
	}{
		{name: "sqlite create", product: "SQLite", form: tableForm{name: "new", database: "main"}, expected: `CREATE TABLE "new" (id INTEGER PRIMARY KEY)`},
		{name: "sqlite rename", product: "SQLite", form: tableForm{name: "new", originalName: "old", table: "old", database: "main", objectKind: tableFormTable}, expected: `ALTER TABLE "old" RENAME TO "new"`},
		{name: "mysql create", product: "MySQL", form: tableForm{name: "new", database: "app"}, expected: "CREATE TABLE `app`.`new` (id INTEGER PRIMARY KEY)"},
		{name: "mysql rename", product: "MySQL", form: tableForm{name: "new", originalName: "old", database: "app", objectKind: tableFormTable}, expected: "ALTER TABLE `app`.`old` RENAME TO `app`.`new`"},
		{name: "postgres create", product: "PostgreSQL", form: tableForm{name: "new", database: "public"}, expected: `CREATE TABLE "public"."new" (id INTEGER PRIMARY KEY)`},
		{name: "postgres rename", product: "PostgreSQL", form: tableForm{name: "new", originalName: "old", table: "public.old", database: "main", objectKind: tableFormTable}, expected: `ALTER TABLE "public"."old" RENAME TO "new"`},
		{name: "postgres schema create", product: "PostgreSQL", form: tableForm{name: "audit", objectKind: tableFormSchema}, expected: `CREATE SCHEMA "audit"`},
		{name: "postgres schema create dotted", product: "PostgreSQL", form: tableForm{name: "audit.logs", objectKind: tableFormSchema}, expected: `CREATE SCHEMA "audit.logs"`},
		{name: "postgres schema rename", product: "PostgreSQL", form: tableForm{name: "new", originalName: "audit", objectKind: tableFormSchema}, expected: `ALTER SCHEMA "audit" RENAME TO "new"`},
		{name: "postgres database create", product: "PostgreSQL", form: tableForm{name: "orders", objectKind: tableFormDatabase}, expected: `CREATE DATABASE "orders"`},
		{name: "postgres database rename", product: "PostgreSQL", form: tableForm{name: "sales", originalName: "orders", objectKind: tableFormDatabase}, expected: `ALTER DATABASE "orders" RENAME TO "sales"`},
		{name: "postgres database rename dotted", product: "PostgreSQL", form: tableForm{name: "sales.old", originalName: "orders.x", objectKind: tableFormDatabase}, expected: `ALTER DATABASE "orders.x" RENAME TO "sales.old"`},
		{name: "mysql database create", product: "MySQL", form: tableForm{name: "orders", objectKind: tableFormDatabase}, expected: "CREATE DATABASE `orders`"},
		{name: "mysql database rename unsupported", product: "MySQL", form: tableForm{name: "sales", originalName: "orders", objectKind: tableFormDatabase}, expected: ""},
		{name: "sqlite schema unsupported", product: "SQLite", form: tableForm{name: "audit", objectKind: tableFormSchema}, expected: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model.databaseInfo.Product = test.product
			if got := test.form.statement(model); got != test.expected {
				t.Fatalf("statement = %q, want %q", got, test.expected)
			}
		})
	}
}

func TestSchemaTableActions_palette(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	model = createTableInSchema(t, model, "old")

	// The palette lists the three table actions in schema focus.
	palette := newCommandPalette(model)
	available := map[CommandID]bool{}
	for _, item := range palette.items {
		available[item.id] = true
	}
	for _, id := range []CommandID{"schema.add_table", "schema.rename_table", "schema.delete_table"} {
		if !available[id] {
			t.Fatalf("palette missing %s", id)
		}
	}

	// Selecting rename from the palette opens the prefilled popup.
	model.schema.Select(1)
	updated, command := model.handlePaletteCommand("schema.rename_table")
	model = updated.(Model)
	model = runTableCommand(model, command)
	if !model.tableForm.active() || model.tableForm.name != "old" {
		t.Fatalf("palette rename did not open the popup: %+v", model.tableForm)
	}
}
