package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestFormMode_normalSQLTextDoesNotMutateHuhValue(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "" {
		t.Fatalf("normal-mode text changed SQL to %q", got)
	}
}

func TestFormMode_iRoutesSQLTextToHuhUntilEscape(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL

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
	if got := model.editor.value; got != "S" {
		t.Fatalf("SQL value = %q, want S after insert then escape", got)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("SQL escape mode = %d, want normal", model.formMode.mode)
	}
}

func TestFormMode_confirmEscapeReturnsToNormal(t *testing.T) {
	// Given
	model := readyModel(t)
	model.formMode.beginConfirm()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if model.formMode.mode != formModeNormal {
		t.Fatalf("confirm escape mode = %d, want normal", model.formMode.mode)
	}
}

func TestFormMode_confirmModeDoesNotMutateSQLText(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model.editor.setValue("SELECT ")
	model.editor.text.Focus()
	model.formMode.beginConfirm()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "SELECT " {
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

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if !model.columnForm.confirming() {
		t.Fatal("normal escape did not reach existing discard handling")
	}
}

func TestFormMode_runningQueryEscapePrecedesSQLInsert(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	canceled := false
	model.running, model.cancel = true, func() { canceled = true }

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if !canceled {
		t.Fatal("running-query escape did not cancel the query")
	}
	if model.formMode.mode != formModeInsert {
		t.Fatalf("running-query escape changed SQL mode to %d, want insert", model.formMode.mode)
	}
}
