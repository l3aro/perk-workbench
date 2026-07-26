package workbench

import (
	"context"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestBrowseForm_enterOpensSelectedRow(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if !model.browseForm.active() || model.browseForm.form == nil || model.browseForm.values.fields[1] != "first" {
		t.Fatalf("browse form = %#v, status = %q, want selected row", model.browseForm, model.Status)
	}
}

func TestBrowseForm_iOpensCellEditor(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1 // select the "name" column

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	// Then — cell editor opened
	if model.cellEditor == nil || !model.cellEditor.active() {
		t.Fatalf("cellEditor = %v, want active cell editor", model.cellEditor)
	}
	if got, want := model.cellEditor.columnName, "name"; got != want {
		t.Fatalf("cellEditor column = %q, want %q", got, want)
	}
	if got, want := model.cellEditor.editedVal, "first"; got != want {
		t.Fatalf("cellEditor value = %q, want %q", got, want)
	}
}

func TestBrowseForm_cellEditorUsesModelWidth(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 120, 24)
	model.browseColumn = 1

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	// width 120 -> editorWidth=88 -> tableViewportWidth=80
	if got, want := model.cellEditor.width, 80; got != want {
		t.Fatalf("cell editor width = %d, want %d", got, want)
	}
}

func TestBrowseForm_cellEditorEnterSubmitsConfirmation(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	if model.cellEditor == nil || !model.cellEditor.confirming || model.cellEditor.confirm == nil {
		t.Fatal("save did not open the cell update confirmation")
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("Enter did not submit the cell update confirmation")
	}
	model = resolveBrowseCommand(model, command())
	if model.cellEditor != nil {
		t.Fatal("Enter did not submit the cell update confirmation")
	}
}

func TestBrowseForm_normalModeNavigatesWithoutMutatingValues(t *testing.T) {
	// Given
	model := openBrowseRow(t, 1)

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'x', Text: "x"})

	// Then
	if got := model.browseForm.form.GetFocusedField().GetKey(); got != "value-1" {
		t.Fatalf("focused field = %q, want value-1", got)
	}
	if got := model.browseForm.values.fields[1]; got != "second" {
		t.Fatalf("normal mode changed value = %q, want second", got)
	}
}

func TestBrowseForm_nKeySetsFocusedColumnToNull(t *testing.T) {
	// Given
	model := openBrowseRow(t, 1)

	// When — focus name column (index 1) and press n
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'n', Text: "n"})

	// Then
	if !model.browseForm.values.nulls[1] {
		t.Fatal("name field nulls[1] should be true after pressing n")
	}
	statement, err := model.browseForm.updateStatement("items")
	if err != nil {
		t.Fatalf("update statement: %v", err)
	}
	if want := "UPDATE `items` SET `name` = NULL WHERE `id` = '2'"; statement != want {
		t.Fatalf("statement = %q, want %q", statement, want)
	}
}

func TestBrowseForm_enterEditModeClearsNullFlag(t *testing.T) {
	// Given
	model := openBrowseRow(t, 0)

	// Mark id column (field 0) as NULL
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.browseForm.values.nulls[0] {
		t.Fatal("id nulls[0] should be true before entering edit mode")
	}

	// When — enter edit mode (clears null for focused field, enters huh)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})

	// Then
	if model.browseForm.values.nulls[0] {
		t.Fatal("nulls[0] should be false after entering edit mode on that field")
	}
}

func TestBrowseForm_savesOnlyAfterPositiveHuhConfirmation(t *testing.T) {
	// Given
	model := openBrowseRow(t, 1)
	model.browseForm.values.fields[1] = "edited"

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.browseForm.active() || model.browseForm.confirmation != nil {
		t.Fatal("negative save confirmation changed the form")
	}
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.browseForm.active() {
		t.Fatal("saved row form remained open")
	}
	result, err := model.Database.Execute(model.appContext, "SELECT name FROM items WHERE id = 2")
	if err != nil {
		t.Fatalf("selecting saved row: %v", err)
	}
	if got := *result.Rows[0][0]; got != "edited" {
		t.Fatalf("name = %q, want edited", got)
	}
	if got := model.browse.Rows()[1][1]; got != "edited" {
		t.Fatalf("browse row = %#v, want refreshed edited value", model.browse.Rows()[1])
	}
	if got := model.browse.Cursor(); got != 1 {
		t.Fatalf("browse cursor = %d, want saved row cursor 1", got)
	}
}

func TestBrowseForm_nThenF5ThenYSavesRowWithNull(t *testing.T) {
	// Given — open row, focus name field, press n to null it
	model := openBrowseRow(t, 1)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'n', Text: "n"})

	// When — F5, then y to confirm save
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then — form closed, name is NULL in database
	if model.browseForm.active() {
		t.Fatal("form should be closed after save")
	}
	result, err := model.Database.Execute(model.appContext, "SELECT name FROM items WHERE id = 2")
	if err != nil {
		t.Fatalf("selecting saved row: %v", err)
	}
	if result.Rows[0][0] != nil {
		t.Fatalf("name = %q, want NULL", *result.Rows[0][0])
	}
}

func TestBrowseForm_positiveDiscardConfirmationClosesWithoutPersistence(t *testing.T) {
	// Given
	model := openBrowseRow(t, 1)
	model.browseForm.values.fields[1] = "discarded"

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.browseForm.active() || model.browseForm.confirmation != nil {
		t.Fatal("negative discard confirmation changed the form")
	}
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.browseForm.active() {
		t.Fatal("positive discard confirmation left the form open")
	}
	result, err := model.Database.Execute(model.appContext, "SELECT name FROM items WHERE id = 2")
	if err != nil {
		t.Fatalf("selecting discarded row: %v", err)
	}
	if got := *result.Rows[0][0]; got != "second" {
		t.Fatalf("name = %q, want second", got)
	}
}

func TestBrowseForm_rejectsRowsWithoutPrimaryKey(t *testing.T) {
	// Given
	// When
	_, err := newBrowseForm([]string{"name"}, []*string{stringPointer("first")}, []sqlite.ColumnInfo{{Name: "name"}})

	// Then
	if err == nil || !strings.Contains(err.Error(), "primary key") {
		t.Fatalf("error = %v, want primary-key rejection", err)
	}
}

func TestBrowseForm_retainsFormAfterZeroOrMultipleAffectedRows(t *testing.T) {
	for _, affected := range []int64{0, 2} {
		t.Run("affected="+strconv.FormatInt(affected, 10), func(t *testing.T) {
			// Given
			model := openBrowseRow(t, 1)
			model.browseForm.values.fields[1] = "changed" // dirty a field so a real UPDATE reaches the mock DB
			model.browseForm.saving = true
			model.Database = browseExecuteService{result: sharedsql.Result{RowsAffected: affected}}

			// When
			updated, _ := model.Update(model.updateBrowseRow()())
			model = updated.(Model)

			// Then
			if !model.browseForm.active() || model.browseForm.saving {
				t.Fatalf("form = %#v, want retained unsaved form", model.browseForm)
			}
		})
	}
}

type browseExecuteService struct {
	sharedsql.Service
	result sharedsql.Result
}

func (s browseExecuteService) Execute(context.Context, string) (sharedsql.Result, error) {
	return s.result, nil
}

func openBrowseRow(t *testing.T, row int) Model {
	t.Helper()
	model := readyBrowseModel(t)
	model.browse.SetCursor(row)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	return model
}

func updateBrowseForm(model Model, message tea.Msg) Model {
	updated, _ := model.Update(message)
	return updated.(Model)
}

func resolveBrowseCommand(model Model, message tea.Msg) Model {
	updated, command := model.Update(message)
	model = updated.(Model)
	for range 4 {
		if command == nil {
			return model
		}
		message = command()
		if batch, ok := message.(tea.BatchMsg); ok {
			for _, next := range batch {
				model = resolveBrowseCommand(model, next())
			}
			return model
		}
		updated, command = model.Update(message)
		model = updated.(Model)
	}
	return model
}

func readyBrowseModel(t *testing.T) Model {
	t.Helper()
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "items", tabBrowse, focusWorkspace
	model.focusActiveTable()
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "INSERT INTO items (id, name) VALUES (1, 'first')"); err != nil {
		t.Fatalf("inserting row: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "INSERT INTO items (id, name) VALUES (2, 'second')"); err != nil {
		t.Fatalf("inserting row: %v", err)
	}
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}, {Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	updated, _ = model.Update(browseTableMsg{table: "items", page: 0, result: sqlite.Result{Columns: []string{"id", "name"}, Rows: [][]*string{{stringPointer("1"), stringPointer("first")}, {stringPointer("2"), stringPointer("second")}}}})
	model = updated.(Model)
	model.browse.SetCursor(0)
	return model
}

func TestBrowse_y_yanks_current_cell_value(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1 // select the "name" column
	model.browse.SetCursor(0)

	// When — y yanks current cell
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — cell value copied to clipboard
	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("expected copy command")
	}
}

func TestBrowse_commaOpensContextMenu(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)

	if model.contextMenu == nil || !model.contextMenu.visible {
		t.Fatal("comma did not open the context menu")
	}
	if got, want := len(model.contextMenu.options), 4; got != want {
		t.Fatalf("context menu options = %d, want %d", got, want)
	}
	if got, want := model.contextMenu.options[0].keys, "y"; got != want {
		t.Errorf("copy-cell shortcut = %q, want %q", got, want)
	}
	if got, want := model.contextMenu.options[3].keys, "d"; got != want {
		t.Errorf("delete-row shortcut = %q, want %q", got, want)
	}
}

func TestBrowse_contextMenuYCopiesSelectedCell(t *testing.T) {
	model := readyBrowseModel(t)
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)

	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("y did not return a copy command")
	}
}

func TestBrowse_contextMenuDOpensDeleteConfirmation(t *testing.T) {
	model := readyBrowseModel(t)
	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)

	if model.deleteConfirm == nil {
		t.Fatal("d did not open delete confirmation")
	}
}

func TestBrowse_contextMenuJAndKNavigateOptions(t *testing.T) {
	model := readyBrowseModel(t)
	model.contextMenu = &contextMenuModel{options: []menuOption{{}, {}}, visible: true}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if got, want := model.contextMenu.selected, 1; got != want {
		t.Fatalf("context menu selection = %d, want %d after j", got, want)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	model = updated.(Model)
	if got, want := model.contextMenu.selected, 0; got != want {
		t.Fatalf("context menu selection = %d, want %d after k", got, want)
	}
}

func TestBrowse_y_yanks_cursor_start_column_by_default(t *testing.T) {
	model := readyBrowseModel(t)
	// browseColumn defaults to 0 (id column)
	model.browse.SetCursor(0)

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — copies "1" (value of id column)
	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("expected copy command")
	}
}

func TestBrowse_y_yanks_moved_cell_value(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1    // name column
	model.browse.SetCursor(1) // second row "second"

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — copies "second"
	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("expected copy command")
	}
}
