package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
)

func TestBrowseForm_enterOpensSelectedRow(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if !model.browse.component.Form.Active() || model.browse.component.Form.Form == nil || model.browse.component.Form.Values.Fields[1] != "first" {
		t.Fatalf("browse form = %#v, status = %q, want selected row", model.browse.component.Form, model.Status)
	}
}

func TestBrowseForm_eOpensSelectedRow(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)

	if !model.browse.component.Form.Active() || model.browse.component.Form.Form == nil || model.browse.component.Form.Values.Fields[1] != "first" {
		t.Fatalf("browse form = %#v, status = %q, want selected row", model.browse.component.Form, model.Status)
	}
}

func TestBrowse_dDirectOpensDeleteConfirmation(t *testing.T) {
	model := readyBrowseModel(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)

	if model.overlay.deleteConfirm == nil {
		t.Fatal("d did not open delete confirmation")
	}
}

func TestBrowseForm_iOpensCellEditor(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.SelectedColumn = 1 // select the "name" column

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	// Then — cell editor opened
	if model.browse.component.CellEditor == nil || !model.browse.component.CellEditor.Active() {
		t.Fatalf("cellEditor = %v, want active cell editor", model.browse.component.CellEditor)
	}
	if got, want := model.browse.component.CellEditor.ColumnName, "name"; got != want {
		t.Fatalf("cellEditor column = %q, want %q", got, want)
	}
	if got, want := model.browse.component.CellEditor.EditedVal, "first"; got != want {
		t.Fatalf("cellEditor value = %q, want %q", got, want)
	}
}

func TestBrowseForm_cellEditorUsesModelWidth(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 120, 24)
	model.browse.component.SelectedColumn = 1

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	if got, want := model.browse.component.CellEditor.Width, 66; got != want {
		t.Fatalf("cell editor width = %d, want %d", got, want)
	}
}

func TestBrowseForm_cellEditorEnterSubmitsConfirmation(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.SelectedColumn = 1

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	if model.browse.component.CellEditor == nil || !model.browse.component.CellEditor.Confirming || model.browse.component.CellEditor.Confirm == nil {
		t.Fatal("save did not open the cell update confirmation")
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("Enter did not submit the cell update confirmation")
	}
	model = resolveBrowseCommand(model, command())
	if model.browse.component.CellEditor != nil {
		t.Fatal("Enter did not submit the cell update confirmation")
	}
}

func TestBrowseForm_savesOnlyAfterPositiveHuhConfirmation(t *testing.T) {
	// Given
	model := openBrowseRow(t, 1)
	model.browse.component.Form.Values.Fields[1] = "edited"

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.browse.component.Form.Active() || model.browse.component.Form.Confirmation != nil {
		t.Fatal("negative save confirmation changed the form")
	}
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.browse.component.Form.Active() {
		t.Fatal("saved row form remained open")
	}
	result, err := model.Database.Execute(model.appContext, "SELECT name FROM items WHERE id = 2")
	if err != nil {
		t.Fatalf("selecting saved row: %v", err)
	}
	if got := *result.Rows[0][0]; got != "edited" {
		t.Fatalf("name = %q, want edited", got)
	}
	if got := model.browse.component.Table.Rows()[1][1]; got != "edited" {
		t.Fatalf("browse row = %#v, want refreshed edited value", model.browse.component.Table.Rows()[1])
	}
	if got := model.browse.component.Table.Cursor(); got != 1 {
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
	if model.browse.component.Form.Active() {
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
	model.browse.component.Form.Values.Fields[1] = "discarded"

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.browse.component.Form.Active() || model.browse.component.Form.Confirmation != nil {
		t.Fatal("negative discard confirmation changed the form")
	}
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.browse.component.Form.Active() {
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
	if model.browse.component.Form.Active() || model.browse.component.Form.Confirmation != nil {
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
	if !model.overlay.formMode.Editing() {
		t.Fatal("row form should open in insert mode without vim mode")
	}
	_ = model.browse.component.Form.Form.NextField() // id -> name (last field)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyTab})
	if !model.overlay.formMode.ButtonsFocused {
		t.Fatal("Tab did not focus the button bar")
	}
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if model.overlay.formMode.ButtonChoice != 1 {
		t.Fatalf("button choice = %d, want Cancel", model.overlay.formMode.ButtonChoice)
	}

	// When — Enter activates Cancel (replayed Escape) on an unchanged form
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	// Then — form closes directly and form mode is normalized
	if model.browse.component.Form.Active() || model.browse.component.Form.Confirmation != nil {
		t.Fatal("bar Cancel on unchanged form opened a confirmation")
	}
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("form mode = %d, want normal after close", model.overlay.formMode.Mode)
	}
	if model.overlay.formMode.ButtonsFocused {
		t.Fatal("button bar stayed focused after form closed")
	}
}

func TestBrowseForm_retainsFormAfterZeroOrMultipleAffectedRows(t *testing.T) {
	for _, affected := range []int64{0, 2} {
		t.Run("affected="+strconv.FormatInt(affected, 10), func(t *testing.T) {
			// Given
			model := openBrowseRow(t, 1)
			model.browse.component.Form.Values.Fields[1] = "changed" // dirty a field so a real update reaches the fake
			model.browse.component.Form.Saving = true
			model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: affected}}

			// When
			updated, _ := model.Update(model.updateBrowseRow()())
			model = updated.(Model)

			// Then
			if !model.browse.component.Form.Active() || model.browse.component.Form.Saving {
				t.Fatalf("form = %#v, want retained unsaved form", model.browse.component.Form)
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
	model.browse.component.Form.Values.Fields[1] = "changed"
	model.browse.component.Form.Saving = true
	model.Database = browseExecuteService{result: sharedsql.Result{RowsAffected: 1}}

	updated, _ := model.Update(model.updateBrowseRow()())
	model = updated.(Model)

	if !strings.Contains(model.Status, "row editing is not supported by") {
		t.Fatalf("status = %q, want capability error", model.Status)
	}
	if !model.browse.component.Form.Active() || model.browse.component.Form.Saving {
		t.Fatalf("form = %#v, want retained form after capability error", model.browse.component.Form)
	}
}

// TestBrowseForm_writeLogsPreferNativeStatement proves successful row
// writes log the service's returned native statement when one is present
// and keep the generic preview when it is empty (compiled-in drivers and
// older plugins). The preview is never replayable for non-SQL backends,
// so the plugin's exact command must win whenever the service returns
// one.
func TestBrowseForm_writeLogsPreferNativeStatement(t *testing.T) {
	const native = "RENAME key user:2 user:3"
	for _, test := range []struct {
		name string
		want string
		run  func(t *testing.T) (Model, tea.Msg)
	}{
		{
			name: "update logs the native statement",
			want: native,
			run: func(t *testing.T) (Model, tea.Msg) {
				model := openBrowseRow(t, 1)
				model.browse.component.Form.Values.Fields[1] = "edited"
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1, Statement: native}}
				return model, model.updateBrowseRow()()
			},
		},
		{
			name: "update keeps the preview without a statement",
			want: "Table: items\nKey:\n  id = \"2\"\nChanges:\n  name = \"edited\"",
			run: func(t *testing.T) (Model, tea.Msg) {
				model := openBrowseRow(t, 1)
				model.browse.component.Form.Values.Fields[1] = "edited"
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1}}
				return model, model.updateBrowseRow()()
			},
		},
		{
			name: "insert logs the native statement",
			want: native,
			run: func(t *testing.T) (Model, tea.Msg) {
				model := readyBrowseModel(t)
				model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
				model.browse.component.Form.Values.Defaults[1] = false
				model.browse.component.Form.Values.Fields[1] = "third"
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1, Statement: native}}
				return model, model.insertBrowseRow()()
			},
		},
		{
			name: "insert keeps the preview without a statement",
			want: "Table: items\nValues:\n  name = \"third\"",
			run: func(t *testing.T) (Model, tea.Msg) {
				model := readyBrowseModel(t)
				model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
				model.browse.component.Form.Values.Defaults[1] = false
				model.browse.component.Form.Values.Fields[1] = "third"
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1}}
				return model, model.insertBrowseRow()()
			},
		},
		{
			name: "delete logs the native statement",
			want: native,
			run: func(t *testing.T) (Model, tea.Msg) {
				model := readyBrowseModel(t)
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1, Statement: native}}
				return model, model.deleteRow()()
			},
		},
		{
			name: "delete keeps the preview without a statement",
			want: "Table: items\nKey:\n  id = \"1\"",
			run: func(t *testing.T) (Model, tea.Msg) {
				model := readyBrowseModel(t)
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1}}
				return model, model.deleteRow()()
			},
		},
		{
			name: "cell update logs the native statement",
			want: native,
			run: func(t *testing.T) (Model, tea.Msg) {
				model := readyBrowseModel(t)
				model.browse.component.SelectedColumn = 1
				model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
				model.browse.component.CellEditor.EditedVal = "changed"
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1, Statement: native}}
				message := model.executeCellUpdate()()
				// The save flow closes the editor before the async result
				// lands, so the result message routes to the root handler.
				model.browse.component.CloseCellEditor()
				return model, message
			},
		},
		{
			name: "cell update keeps the preview without a statement",
			want: "Table: items\nKey:\n  id = \"1\"\nChanges:\n  name = \"changed\"",
			run: func(t *testing.T) (Model, tea.Msg) {
				model := readyBrowseModel(t)
				model.browse.component.SelectedColumn = 1
				model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
				model.browse.component.CellEditor.EditedVal = "changed"
				model.Database = browseWriteService{result: sharedsql.Result{RowsAffected: 1}}
				message := model.executeCellUpdate()()
				// The save flow closes the editor before the async result
				// lands, so the result message routes to the root handler.
				model.browse.component.CloseCellEditor()
				return model, message
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, message := test.run(t)
			updated, _ := model.Update(message)
			model = updated.(Model)
			entries := model.queryLog.component.Entries
			if len(entries) == 0 {
				t.Fatalf("query log = %#v, want a write entry", model.queryLog.component.Entries)
			}
			// The query log is newest-first: the write entry is the head.
			if got, want := entries[0].Statement, test.want; got != want {
				t.Fatalf("query log statement = %q, want %q", got, want)
			}
		})
	}
}

// TestWriteLogStatement_blankVsNonblank proves the native statement wins
// only when nonblank: whitespace-only plugin output must not suppress the
// preview, and a nonblank statement is kept verbatim (not trimmed).
func TestWriteLogStatement_blankVsNonblank(t *testing.T) {
	preview := "Table: keys\nKey:\n  key = \"user:2\"\nChanges:\n  key = \"user:3\""
	tests := []struct {
		name      string
		statement string
		want      string
	}{
		{name: "blank keeps the preview", statement: "", want: preview},
		{name: "whitespace keeps the preview", statement: "  \n\t ", want: preview},
		{name: "native statement wins", statement: "RENAME key user:2 user:3", want: "RENAME key user:2 user:3"},
		{name: "native statement with padding stays verbatim", statement: "  RENAME key user:2 user:3  ", want: "  RENAME key user:2 user:3  "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := writeLogStatement(preview, sharedsql.Result{Statement: test.statement}); got != test.want {
				t.Fatalf("writeLogStatement = %q, want %q", got, test.want)
			}
		})
	}
}

// TestBrowseForm_tabReachesButtonsFromInsertMode guards the vim-off flow
// for the row editor: Tab on the last field focuses the Save/Cancel bar

func openBrowseRow(t *testing.T, row int) Model {
	t.Helper()
	model := readyBrowseModel(t)
	model.browse.component.Table.SetCursor(row)
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
	model.browse.component.Table.SetCursor(0)
	return model
}

func TestBrowse_y_yanks_current_cell_value(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.SelectedColumn = 1 // select the "name" column
	model.browse.component.Table.SetCursor(0)

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
	model.browse.component.SelectedColumn = 1 // select the "name" column
	model.browse.component.Table.SetCursor(0)
	full := strings.Repeat("x", 400)
	model.browse.component.Result = sqlite.Result{
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
		if _, tick := message.(notification.DismissMsg); tick {
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

	if model.overlay.contextMenu == nil || !model.overlay.contextMenu.visible {
		t.Fatal("comma did not open the context menu")
	}
	if got, want := len(model.overlay.contextMenu.options), 5; got != want {
		t.Fatalf("context menu options = %d, want %d", got, want)
	}
	if got, want := model.overlay.contextMenu.options[0].action, "insert_row"; got != want {
		t.Errorf("first option action = %q, want %q", got, want)
	}
	if got, want := model.overlay.contextMenu.options[0].keys, "a"; got != want {
		t.Errorf("insert-row shortcut = %q, want %q", got, want)
	}
	if got, want := model.overlay.contextMenu.options[4].keys, "d"; got != want {
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

	if model.overlay.deleteConfirm == nil {
		t.Fatal("d did not open delete confirmation")
	}
}

func TestBrowse_contextMenuJAndKNavigateOptions(t *testing.T) {
	model := readyBrowseModel(t)
	model.overlay.contextMenu = &contextMenuModel{options: []menuOption{{}, {}}, visible: true}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if got, want := model.overlay.contextMenu.selected, 1; got != want {
		t.Fatalf("context menu selection = %d, want %d after j", got, want)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	model = updated.(Model)
	if got, want := model.overlay.contextMenu.selected, 0; got != want {
		t.Fatalf("context menu selection = %d, want %d after k", got, want)
	}
}

func TestBrowse_y_yanks_cursor_start_column_by_default(t *testing.T) {
	model := readyBrowseModel(t)
	// browseColumn defaults to 0 (id column)
	model.browse.component.Table.SetCursor(0)

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
	model.browse.component.SelectedColumn = 1 // name column
	model.browse.component.Table.SetCursor(1) // second row "second"

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

	if model.browse.component.FilterForm == nil {
		t.Fatal("browse filter form = nil, want opened grid")
	}
}

func TestBrowse_loadUsesFiltersAndLimit(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.Settings = browse.Settings{Filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"}}, Limit: 1}

	updated, _ := model.Update(model.loadBrowse()())
	model = updated.(Model)

	if rows := model.browse.component.Table.Rows(); len(rows) != 1 || rows[0][1] != "second" {
		t.Fatalf("browse rows = %#v, want filtered row", rows)
	}
}

func TestBrowse_headerClickSortsColumnLikeS(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wide: the status line stays on one row
	model.focusActiveTable()

	columns := model.browse.component.Table.Columns()
	nameX := model.layout.schemaWidth + 1
	for _, column := range columns[:1] {
		nameX += column.Width + 2*spaceCompact
	}
	headerY := 4 // contentY=3 = the browse table header row

	// When — click the name column header.
	updated, command := model.Update(tea.MouseClickMsg{X: nameX, Y: headerY, Button: tea.MouseLeft})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())

	// Then — name sorts ascending, the column is selected, marker shows.
	if got := model.browse.component.Settings.Sorts; !slices.Equal(got, []browse.Sort{{Column: "name"}}) {
		t.Fatalf("browse sorts = %#v, want name ascending", got)
	}
	if got := model.browse.component.SelectedColumn; got != 1 {
		t.Fatalf("browse column = %d, want 1", got)
	}
	if got := model.browse.component.Table.Columns()[1].Title; got != "⌃ name" {
		t.Fatalf("sort title = %q, want %q", got, "⌃ name")
	}

	// When — click the same header again.
	updated, command = model.Update(tea.MouseClickMsg{X: nameX, Y: headerY, Button: tea.MouseLeft})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())

	// Then — it flips to descending.
	if got := model.browse.component.Settings.Sorts; !slices.Equal(got, []browse.Sort{{Column: "name", Desc: true}}) {
		t.Fatalf("browse sorts = %#v, want name descending", got)
	}

	// When — click the id header.
	updated, command = model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 1, Y: headerY, Button: tea.MouseLeft})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())

	// Then — id joins the sort chain after name, selection moves to it.
	if got := model.browse.component.Settings.Sorts; !slices.Equal(got, []browse.Sort{{Column: "name", Desc: true}, {Column: "id"}}) {
		t.Fatalf("browse sorts = %#v, want name desc then id asc", got)
	}
	if got := model.browse.component.SelectedColumn; got != 0 {
		t.Fatalf("browse column = %d, want 0", got)
	}
}

func TestBrowse_sCyclesSelectedColumnSort(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.SelectedColumn = 1

	for _, want := range []struct {
		column int
		sorts  []browse.Sort
		titles []string
	}{
		{column: 1, sorts: []browse.Sort{{Column: "name"}}, titles: []string{"id", "⌃ name"}},
		{column: 0, sorts: []browse.Sort{{Column: "name"}, {Column: "id"}}, titles: []string{"⌃ id", "⌃ name"}},
		{column: 1, sorts: []browse.Sort{{Column: "name", Desc: true}, {Column: "id"}}, titles: []string{"⌃ id", "⌄ name"}},
		{column: 1, sorts: []browse.Sort{{Column: "id"}}, titles: []string{"⌃ id", "name"}},
	} {
		model.browse.component.SelectedColumn = want.column
		updated, command := model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		model = updated.(Model)
		model = resolveBrowseCommand(model, command())
		if got := model.browse.component.Settings.Sorts; !slices.Equal(got, want.sorts) {
			t.Fatalf("browse sorts = %#v, want %#v", got, want.sorts)
		}
		for index, title := range want.titles {
			if got := model.browse.component.Table.Columns()[index].Title; got != title {
				t.Fatalf("sort title[%d] = %q, want %q", index, got, title)
			}
		}
	}
}
