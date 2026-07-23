package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestStructureForm_usesHuhControlsForColumnEditing(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", PrimaryKey: 1}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if form.form == nil {
		t.Fatal("column editor did not create a Huh form")
	}
	if form.form.GetFocusedField().GetKey() != "name" || form.primaryKey != 1 {
		t.Fatalf("column Huh form has unexpected initial state: %#v", form)
	}
}

func TestStructureForm_huhInputUpdatesPersistedChange(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = updateColumn(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateColumn(model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := model.columnForm.values.name; got != "namex" {
		t.Fatalf("Huh name = %q, want namex", got)
	}
	change, err := model.columnForm.change()
	if err != nil || change.Name != "namex" {
		t.Fatalf("change/error = %#v/%v", change, err)
	}
}

func TestStructureForm_positiveSaveConfirmationPersistsChange(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = typeColumnText(model, "title")

	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.columnForm.active() || model.columnForm.confirming() {
		t.Fatal("negative save confirmation changed the form")
	}

	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if model.columnForm.active() || model.structure.Rows()[0][0] != "title" {
		t.Fatalf("saved form/rows = %#v/%#v", model.columnForm, model.structure.Rows())
	}
}

func TestStructureForm_positiveDiscardConfirmationClosesForm(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.columnForm.active() || model.columnForm.confirming() {
		t.Fatal("negative discard confirmation changed the form")
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if model.columnForm.active() {
		t.Fatal("positive discard confirmation did not close the form")
	}
}

func TestStructureForm_normalInputCannotMutateAndEscapeReturnsToNormal(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = updateColumn(model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if model.columnForm.values.name != "name" {
		t.Fatalf("normal mode changed name to %q", model.columnForm.values.name)
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if model.formMode.mode != formModeInsert {
		t.Fatalf("column mode = %d, want insert", model.formMode.mode)
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updateColumn(model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if model.columnForm.values.name != "namex" || model.formMode.mode != formModeNormal {
		t.Fatalf("name/mode = %q/%d", model.columnForm.values.name, model.formMode.mode)
	}
}

func TestStructureForm_invalidValuesCannotReachConfirmation(t *testing.T) {
	model := openColumn(t, "price", "DECIMAL(10,2)")
	model = updateColumn(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: tea.KeyTab})
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: tea.KeyTab})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	if model.columnForm.confirmation != nil || model.columnForm.validationError == "" {
		t.Fatal("invalid decimal parameters reached confirmation")
	}
	model.columnForm.values.typeName, model.columnForm.typeChanged = "", true
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	if model.columnForm.confirmation != nil || !strings.Contains(model.columnForm.validationError, "type") {
		t.Fatalf("invalid type error = %q", model.columnForm.validationError)
	}
}

func TestStructureForm_blankNameCannotReachConfirmation(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = updateColumn(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for range "name" {
		model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	if model.columnForm.confirmation != nil || !strings.Contains(model.columnForm.validationError, "name") {
		t.Fatalf("blank name validation = %q", model.columnForm.validationError)
	}
}

func TestStructureForm_preservesParameterizedColumnChange(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "amount", Type: "NUMERIC (10,2)", Nullable: true, DefaultValue: ptr("0")}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if !equalStrings(form.values.parameters, []string{"10", "2"}) {
		t.Fatalf("parameters = %#v", form.values.parameters)
	}
	form.values.typeName, form.typeChanged, form.values.parameters, form.values.name = "DECIMAL", true, []string{"12", "2"}, "price"
	change, err := form.change()
	if err != nil || change.Type != "DECIMAL(12,2)" || change.DefaultValue == nil || *change.DefaultValue != "0" {
		t.Fatalf("change/error = %#v/%v", change, err)
	}
}

func TestStructureForm_escapeCancelsRunningQueryBeforeDiscard(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	canceled := false
	model.running, model.cancel = true, func() { canceled = true }
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !canceled || model.columnForm.confirming() {
		t.Fatal("running-query escape did not take precedence")
	}
}

func openColumn(t *testing.T, name, typeName string) Model {
	t.Helper()
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items ("+name+" "+typeName+")"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: name, Type: typeName, Nullable: true}}})
	model = updateColumn(updated.(Model), tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = model.columnForm.form.Init()
	return model
}

func updateColumn(model Model, message tea.Msg) Model {
	updated, _ := model.Update(message)
	return updated.(Model)
}

func resolveColumnCommand(model Model, message tea.Msg) Model {
	updated, command := model.Update(message)
	model = updated.(Model)
	for range 4 {
		if command == nil {
			return model
		}
		message = command()
		if batch, ok := message.(tea.BatchMsg); ok {
			for _, next := range batch {
				model = resolveColumnCommand(model, next())
			}
			return model
		}
		updated, command = model.Update(message)
		model = updated.(Model)
	}
	return model
}

func typeColumnText(model Model, value string) Model {
	model = updateColumn(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for range "name" {
		model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, character := range value {
		model = updateColumn(model, tea.KeyPressMsg{Code: character, Text: string(character)})
	}
	return updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func ptr[T any](value T) *T { return &value }
