package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestFormMode_normalSQLTextDoesNotMutateHuhValue(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.queryLog.editor.value; got != "" {
		t.Fatalf("normal-mode text changed SQL to %q", got)
	}
}

func TestFormMode_iRoutesSQLTextToHuhUntilEscape(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	bindings, err := NewKeybindings(map[string][]string{"form.edit": {"z"}})
	if err != nil {
		t.Fatalf("NewKeybindings: %v", err)
	}
	model.SetKeybindings(bindings)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.queryLog.editor.value; got != "S" {
		t.Fatalf("SQL value = %q, want S after insert then escape", got)
	}
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("SQL escape mode = %d, want normal", model.overlay.formMode.Mode)
	}
}

func TestFormMode_confirmEscapeReturnsToNormal(t *testing.T) {
	// Given
	model := readyModel(t)
	model.overlay.formMode.BeginConfirm()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("confirm escape mode = %d, want normal", model.overlay.formMode.Mode)
	}
}

func TestFormMode_confirmModeDoesNotMutateSQLText(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model.queryLog.editor.setValue("SELECT ")
	model.queryLog.editor.text.Focus()
	model.overlay.formMode.BeginConfirm()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.queryLog.editor.value; got != "SELECT " {
		t.Fatalf("confirm-mode text changed SQL to %q", got)
	}
}

func TestFormMode_normalEscapeOpensExistingDiscard(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.schema.component.Structure.ColumnForm.Values.Name = "renamed"

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if !model.schema.component.Structure.ColumnForm.Confirming() {
		t.Fatal("normal escape did not reach existing discard handling")
	}
}

func TestFormMode_runningQueryEscapePrecedesSQLInsert(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	requestID := startQuery(t, &model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	if !model.Running() {
		t.Fatal("running-query escape did not keep the request active until completion")
	}
	updated, _ = model.Update(queryCanceledMsg{requestID: requestID})
	if model.overlay.formMode.Mode != formModeInsert {
		t.Fatalf("running-query escape changed SQL mode to %d, want insert", model.overlay.formMode.Mode)
	}
}

// TestFormMode_confirmEnterDoesNotEnterInsert exercises the flow:
// form normal mode → open discard confirmation → select an option with arrow
// keys → press Enter. Enter must complete the dialog, not trigger form.edit
// (which would enter insert mode on the underlying form).
func TestFormMode_confirmEnterDoesNotEnterInsert(t *testing.T) {
	// Given — column form open, discard confirmation showing
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	// Press Enter → opens column form (deferred Init cmd but form exists immediately)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.schema.component.Structure.ColumnForm.Values.Name = "renamed"
	// Press Escape → triggers discard confirmation (formModeConfirm)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if !model.schema.component.Structure.ColumnForm.Confirming() {
		t.Fatal("Escape did not reach discard confirmation")
	}
	if model.overlay.formMode.Mode != formModeConfirm {
		t.Fatalf("form mode = %d, want confirm", model.overlay.formMode.Mode)
	}

	// When — navigate to "No" (second option) then press Enter
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	if !model.schema.component.Structure.ColumnForm.Confirming() {
		t.Fatal("right arrow dismissed the confirmation")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — dialog completed with "cancel" (No), form stays open, mode is normal
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("Enter on confirmation set mode = %d, want normal", model.overlay.formMode.Mode)
	}
	if model.schema.component.Structure.ColumnForm.Confirming() {
		t.Fatal("Enter on confirmation did not clear the dialog")
	}
	if !model.schema.component.Structure.ColumnForm.Active() {
		t.Fatal("Enter on confirmation closed the form (cancel action should keep it open)")
	}
}

func TestFormMode_confirmEnterDefaultYes_closesForm(t *testing.T) {
	// Given — column form open, discard confirmation showing ("Yes" selected)
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.schema.component.Structure.ColumnForm.Values.Name = "renamed"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if !model.schema.component.Structure.ColumnForm.Confirming() {
		t.Fatal("Escape did not reach discard confirmation")
	}

	// When — Enter on default "Yes"
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — dialog completed with "confirm", form closes, mode is normal
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("Enter on confirm set mode = %d, want normal", model.overlay.formMode.Mode)
	}
	if model.schema.component.Structure.ColumnForm.Active() {
		t.Fatal("Enter on confirm did not close the form")
	}
}

// TestFormMode_quitDialogFromSQLTab_EnterCancelStaysNormal exercises the bug:
// SQL tab normal mode → Ctrl+Q opens quitDialog (Disconnect/Quit/Cancel) →
// navigate to Cancel → Enter. Enter must complete the dialog, not trigger
// formRouteParent → form.edit (which enters insert mode on the SQL editor).
func TestFormMode_quitDialogFromSQLTab_EnterCancelStaysNormal(t *testing.T) {
	// Given — SQL tab in normal mode
	model := readyModel(t)
	model = resizeModel(model, 80, 24)
	model.Focus, model.Tab = focusWorkspace, tabSQL

	// When — Ctrl+Q opens quitDialog
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	model = updated.(Model)

	if model.overlay.quitDialog == nil {
		t.Fatal("Ctrl+Q did not open quit dialog")
	}
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("form mode = %d, want normal after quit dialog opened", model.overlay.formMode.Mode)
	}

	// When — navigate to "Cancel" (third option, index 2), then Enter
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — dialog cleared, formMode unchanged (not insert)
	if model.overlay.quitDialog != nil {
		t.Fatal("Enter on quit dialog did not clear the dialog")
	}
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("Enter on quit dialog set mode = %d, want normal", model.overlay.formMode.Mode)
	}
}
