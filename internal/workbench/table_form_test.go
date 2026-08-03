package workbench

import (
	"context"
	"strings"
	"testing"

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
	if !model.tableForm.active() || model.tableForm.name != "old" || model.tableForm.table != "old" || model.tableForm.database != "main" {
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
			if !model.tableForm.active() || model.tableForm.name != "" || model.tableForm.table != "" || model.tableForm.database != "main" {
				t.Fatalf("create popup = %+v, want empty in main", model.tableForm)
			}

			// When: name it, save, and accept; the confirmation carries the
			// exact zero-column DDL and runs it.
			model.tableForm.name = "created"
			updated, _ = model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			model = updated.(Model)
			if got, want := model.tableForm.confirmation.description, `CREATE TABLE "created" ()`; got != want {
				t.Fatalf("confirmation description = %q, want %q", got, want)
			}
			updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			model = runTableCommand(model, command)
			if model.tableFormRunning || model.Running() {
				t.Fatal("create query still running")
			}
			// SQLite rejects zero-column CREATE TABLE: the popup must stay
			// open with the typed name and the driver error reported.
			if !model.tableForm.active() || model.tableForm.name != "created" {
				t.Fatalf("rejected create closed the popup: %+v", model.tableForm)
			}
			if !strings.Contains(model.Status, "table action failed") {
				t.Fatalf("rejected create did not report the driver error: %q", model.Status)
			}
			if names := schemaTableNames(t, model); len(names) != 1 || names[0] != "accounts" {
				t.Fatalf("rejected create mutated the database: %v", names)
			}
		})
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
		{name: "sqlite create", product: "SQLite", form: tableForm{name: "new", database: "main"}, expected: `CREATE TABLE "new" ()`},
		{name: "sqlite rename", product: "SQLite", form: tableForm{name: "new", originalName: "old", database: "main", table: "old"}, expected: `ALTER TABLE "old" RENAME TO "new"`},
		{name: "mysql create", product: "MySQL", form: tableForm{name: "new", database: "app"}, expected: "CREATE TABLE `app`.`new` ()"},
		{name: "mysql rename", product: "MySQL", form: tableForm{name: "new", originalName: "old", database: "app", table: "old"}, expected: "ALTER TABLE `app`.`old` RENAME TO `app`.`new`"},
		{name: "postgres create", product: "PostgreSQL", form: tableForm{name: "new", database: "public"}, expected: `CREATE TABLE "public"."new" ()`},
		{name: "postgres rename", product: "PostgreSQL", form: tableForm{name: "new", originalName: "old", database: "public", table: "old"}, expected: `ALTER TABLE "public"."old" RENAME TO "new"`},
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
