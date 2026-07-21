package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestStructureForm_savesColumnOnlyAfterTrueConfirmation(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	if _, err := model.Database.Execute(model.appContext, `CREATE TABLE items (name TEXT)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then
	if !model.columnForm.active() {
		t.Fatal("structure enter did not open the column form")
	}

	// Given
	model.columnForm.name.SetValue("title")

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then
	if !model.columnForm.active() || model.columnForm.confirming() {
		t.Fatal("false confirmation changed the form")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, refresh := model.Update(command())
	model = updated.(Model)
	model = updateFromCommand(model, refresh)

	// Then
	if model.columnForm.active() {
		t.Fatal("true confirmation did not close the form after saving")
	}
	if got := model.structure.Rows(); len(got) != 1 || got[0][0] != "title" {
		t.Fatalf("structure rows = %#v, want saved title column", got)
	}
}

func TestStructureForm_escapeConfirmsDiscard(t *testing.T) {
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
		t.Fatal("escape did not request discard confirmation")
	}
}

func TestStructureForm_iOpensSelectedColumn(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	// Then
	if !model.columnForm.active() || model.columnForm.previousName != "name" {
		t.Fatalf("column form = %#v, want selected name column", model.columnForm)
	}
}

func TestStructureForm_usesNormalAndInsertModeNavigation(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	model = updated.(Model)

	// Then
	if model.columnForm.mode != columnFormNormal || model.columnForm.name.Value() != "namex" {
		t.Fatalf("form state = mode:%d name:%q, want normal mode and edited name", model.columnForm.mode, model.columnForm.name.Value())
	}
	if model.columnForm.focus != columnFieldDefault {
		t.Fatalf("form focus = %d, want default field after G", model.columnForm.focus)
	}
}

func TestStructureForm_escapeDoesNotCancelRunningQuery(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	canceled := false
	model.running, model.cancel = true, func() { canceled = true }

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if canceled {
		t.Fatal("escape canceled the running query instead of opening discard confirmation")
	}
	if !model.columnForm.confirming() {
		t.Fatal("escape did not open discard confirmation")
	}
}
