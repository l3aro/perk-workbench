package workbench

import (
	"context"
	"fmt"
	"reflect"
	"slices"
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

	if got, want := model.cellEditor.width, 66; got != want {
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
	wantValues := []sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueNull}}}
	if values := model.browseForm.rowValues(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("rowValues = %#v, want %#v", values, wantValues)
	}
	wantKey := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "2"}}}
	if key, err := model.browseForm.keyValues(); err != nil || !reflect.DeepEqual(key, wantKey) {
		t.Fatalf("keyValues = %#v, %v; want %#v", key, err, wantKey)
	}
	if got, want := model.browseForm.preview(), "Table: items\nKey:\n  id = \"2\"\nChanges:\n  name = NULL"; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
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

func TestBrowseForm_discardWithoutChangesClosesWithoutConfirmation(t *testing.T) {
	// Given — row open, no edits made
	model := openBrowseRow(t, 1)

	// When — Escape to discard
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Then — form closes directly, no confirmation
	if model.browseForm.active() || model.browseForm.confirmation != nil {
		t.Fatal("unchanged discard opened a confirmation")
	}
	result, err := model.Database.Execute(model.appContext, "SELECT name FROM items WHERE id = 2")
	if err != nil {
		t.Fatalf("selecting discarded row: %v", err)
	}
	if got := *result.Rows[0][0]; got != "second" {
		t.Fatalf("name = %q, want second", got)
	}
}

func TestBrowseForm_discardFromButtonBarNormalizesFormMode(t *testing.T) {
	// Given — vim off: the row form opens in insert mode; Tab twice moves
	// focus to the Save/Cancel bar, l selects Cancel.
	model := readyBrowseModel(t)
	model.vimMode = false
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyTab})
	if !model.formMode.editing() {
		t.Fatal("row form should open in insert mode without vim mode")
	}
	_ = model.browseForm.form.NextField() // id -> name (last field)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyTab})
	if !model.formMode.buttonsFocused {
		t.Fatal("Tab did not focus the button bar")
	}
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if model.formMode.buttonChoice != 1 {
		t.Fatalf("button choice = %d, want Cancel", model.formMode.buttonChoice)
	}

	// When — Enter activates Cancel (replayed Escape) on an unchanged form
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	// Then — form closes directly and form mode is normalized
	if model.browseForm.active() || model.browseForm.confirmation != nil {
		t.Fatal("bar Cancel on unchanged form opened a confirmation")
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("form mode = %d, want normal after close", model.formMode.mode)
	}
	if model.formMode.buttonsFocused {
		t.Fatal("button bar stayed focused after form closed")
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
			model.browseForm.values.fields[1] = "changed" // dirty a field so a real update reaches the fake
			model.browseForm.saving = true
			model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: affected}}

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

// browseWriteService is a RowWriter-capable fake that reports a fixed
// RowsAffected, used to prove the workbench's RowsAffected == 1 contract.
type browseWriteService struct {
	sharedsql.Service
	result sharedsql.Result
}

func (s browseWriteService) WriteCapabilities() sharedsql.WriteCapabilities {
	return sharedsql.WriteCapabilities{RowWriter: true}
}

func (s browseWriteService) UpdateRow(context.Context, string, []sharedsql.RowValue, []sharedsql.RowValue) (sharedsql.Result, error) {
	return s.result, nil
}

func (s browseWriteService) InsertRow(context.Context, string, []sharedsql.RowValue) (sharedsql.Result, error) {
	return s.result, nil
}

func (s browseWriteService) DeleteRow(context.Context, string, []sharedsql.RowValue) (sharedsql.Result, error) {
	return s.result, nil
}

// browseExecuteService is a capability-less fake: Execute works, but no
// row/document write interface exists, so any browse write must be
// rejected with the capability error.
type browseExecuteService struct {
	sharedsql.Service
	result sharedsql.Result
}

func (s browseExecuteService) Execute(context.Context, string) (sharedsql.Result, error) {
	return s.result, nil
}

// TestBrowseForm_staleRowActionReportsCapabilityError proves a forced
// write on a service without row capability fails with the capability
// error instead of a broken statement.
func TestBrowseForm_staleRowActionReportsCapabilityError(t *testing.T) {
	model := openBrowseRow(t, 1)
	model.browseForm.values.fields[1] = "changed"
	model.browseForm.saving = true
	model.Database = browseExecuteService{result: sharedsql.Result{RowsAffected: 1}}

	updated, _ := model.Update(model.updateBrowseRow()())
	model = updated.(Model)

	if !strings.Contains(model.Status, "row editing is not supported by") {
		t.Fatalf("status = %q, want capability error", model.Status)
	}
	if !model.browseForm.active() || model.browseForm.saving {
		t.Fatalf("form = %#v, want retained form after capability error", model.browseForm)
	}
}

// TestBrowseForm_tabReachesButtonsFromInsertMode guards the vim-off flow
// for the row editor: Tab on the last field focuses the Save/Cancel bar
// without leaving insert mode, and k returns with typing intact.
func TestBrowseForm_tabReachesButtonsFromInsertMode(t *testing.T) {
	model := openBrowseRow(t, 0)
	model.formMode.beginHuh(model.browseForm.focus()) // insert mode, vim off
	_ = model.browseForm.form.NextField()             // id -> name (last field)

	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyTab})
	if !model.formMode.buttonsFocused || model.formMode.mode != formModeInsert {
		t.Fatalf("tab on last field: bar=%t mode=%d, want focused/insert", model.formMode.buttonsFocused, model.formMode.mode)
	}

	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if model.formMode.buttonsFocused || model.formMode.mode != formModeInsert {
		t.Fatalf("k from bar: bar=%t mode=%d, want unfocused/insert", model.formMode.buttonsFocused, model.formMode.mode)
	}

	// j is content on the name field, not field navigation.
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := model.browseForm.values.fields[1]; got != "firstj" {
		t.Fatalf("name = %q, want %q", got, "firstj")
	}
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
	return driveCommand(model, command)
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

func TestBrowse_y_yanks_full_value_not_display_trimmed(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1 // select the "name" column
	model.browse.SetCursor(0)
	full := strings.Repeat("x", 400)
	model.browseResult = sqlite.Result{
		Columns:         []string{"id", "name"},
		Rows:            [][]*string{{stringPointer("1"), stringPointer(cellText(full))}, {stringPointer("2"), stringPointer("second")}},
		UntruncatedRows: [][]*string{{stringPointer("1"), stringPointer(full)}, {stringPointer("2"), stringPointer("second")}},
	}

	// When — y yanks the selected cell
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — the full untruncated value is copied, not the display-trimmed one
	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := copiedText(command), full; got != want {
		t.Fatalf("copied cell = %q, want full untruncated value %q", got, want)
	}
}

// copiedText runs a copy command and returns the clipboard text it carries.
func copiedText(command tea.Cmd) string {
	for _, message := range executeCommandAll(command) {
		if _, tick := message.(notificationDismissMsg); tick {
			continue
		}
		if text := fmt.Sprint(message); text != "<nil>" {
			return text
		}
	}
	return ""
}

func TestBrowse_commaOpensContextMenu(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)

	if model.contextMenu == nil || !model.contextMenu.visible {
		t.Fatal("comma did not open the context menu")
	}
	if got, want := len(model.contextMenu.options), 5; got != want {
		t.Fatalf("context menu options = %d, want %d", got, want)
	}
	if got, want := model.contextMenu.options[0].action, "insert_row"; got != want {
		t.Errorf("first option action = %q, want %q", got, want)
	}
	if got, want := model.contextMenu.options[0].keys, "a"; got != want {
		t.Errorf("insert-row shortcut = %q, want %q", got, want)
	}
	if got, want := model.contextMenu.options[4].keys, "d"; got != want {
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

func TestBrowse_refineOpensFilterGrid(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)

	if model.browseFilterForm == nil {
		t.Fatal("browse filter form = nil, want opened grid")
	}
}

func TestBrowse_loadUsesFiltersAndLimit(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseSettings = browseSettings{filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"}}, limit: 1}

	updated, _ := model.Update(model.loadBrowse()())
	model = updated.(Model)

	if rows := model.browse.Rows(); len(rows) != 1 || rows[0][1] != "second" {
		t.Fatalf("browse rows = %#v, want filtered row", rows)
	}
}

func TestBrowse_headerClickSortsColumnLikeS(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wide: the status line stays on one row
	model.focusActiveTable()

	columns := model.browse.Columns()
	nameX := model.schemaWidth + 1
	for _, column := range columns[:1] {
		nameX += column.Width + 2*spaceCompact
	}
	headerY := 4 // contentY=3 = the browse table header row

	// When — click the name column header.
	updated, command := model.Update(tea.MouseClickMsg{X: nameX, Y: headerY, Button: tea.MouseLeft})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())

	// Then — name sorts ascending, the column is selected, marker shows.
	if got := model.browseSettings.sorts; !slices.Equal(got, []browseSort{{column: "name"}}) {
		t.Fatalf("browse sorts = %#v, want name ascending", got)
	}
	if got := model.browseColumn; got != 1 {
		t.Fatalf("browse column = %d, want 1", got)
	}
	if got := model.browse.Columns()[1].Title; got != "⌃ name" {
		t.Fatalf("sort title = %q, want %q", got, "⌃ name")
	}

	// When — click the same header again.
	updated, command = model.Update(tea.MouseClickMsg{X: nameX, Y: headerY, Button: tea.MouseLeft})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())

	// Then — it flips to descending.
	if got := model.browseSettings.sorts; !slices.Equal(got, []browseSort{{column: "name", desc: true}}) {
		t.Fatalf("browse sorts = %#v, want name descending", got)
	}

	// When — click the id header.
	updated, command = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 1, Y: headerY, Button: tea.MouseLeft})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())

	// Then — id joins the sort chain after name, selection moves to it.
	if got := model.browseSettings.sorts; !slices.Equal(got, []browseSort{{column: "name", desc: true}, {column: "id"}}) {
		t.Fatalf("browse sorts = %#v, want name desc then id asc", got)
	}
	if got := model.browseColumn; got != 0 {
		t.Fatalf("browse column = %d, want 0", got)
	}
}

func TestBrowse_sCyclesSelectedColumnSort(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1

	for _, want := range []struct {
		column int
		sorts  []browseSort
		titles []string
	}{
		{column: 1, sorts: []browseSort{{column: "name"}}, titles: []string{"id", "⌃ name"}},
		{column: 0, sorts: []browseSort{{column: "name"}, {column: "id"}}, titles: []string{"⌃ id", "⌃ name"}},
		{column: 1, sorts: []browseSort{{column: "name", desc: true}, {column: "id"}}, titles: []string{"⌃ id", "⌄ name"}},
		{column: 1, sorts: []browseSort{{column: "id"}}, titles: []string{"⌃ id", "name"}},
	} {
		model.browseColumn = want.column
		updated, command := model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		model = updated.(Model)
		model = resolveBrowseCommand(model, command())
		if got := model.browseSettings.sorts; !slices.Equal(got, want.sorts) {
			t.Fatalf("browse sorts = %#v, want %#v", got, want.sorts)
		}
		for index, title := range want.titles {
			if got := model.browse.Columns()[index].Title; got != title {
				t.Fatalf("sort title[%d] = %q, want %q", index, got, title)
			}
		}
	}
}
