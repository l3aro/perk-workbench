package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk/internal/sql"
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

func TestStructureForm_typeSelectionBuildsDecimalDeclaration(t *testing.T) {
	// Given
	form := newColumnForm(sqlite.ColumnInfo{Name: "price", Type: "INTEGER", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.focus = columnFieldType

	// When
	_, _ = form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	for form.typeOptions[form.typePicker].Name != "DECIMAL" {
		_, _ = form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	form.parameters[0].SetValue("12")
	form.parameters[1].SetValue("2")
	change, err := form.change()

	// Then
	if err != nil {
		t.Fatalf("change() error = %v", err)
	}
	if change.Type != "DECIMAL(12,2)" {
		t.Errorf("type = %q, want DECIMAL(12,2)", change.Type)
	}
}

func TestStructureForm_keepsExistingTypeWhenOnlyRenamingColumn(t *testing.T) {
	// Given
	form := newColumnForm(sqlite.ColumnInfo{Name: "title", Type: "varchar(120)", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.name.SetValue("name")

	// When
	change, err := form.change()

	// Then
	if err != nil {
		t.Fatalf("change() error = %v", err)
	}
	if change.Type != "varchar(120)" {
		t.Errorf("type = %q, want existing declaration", change.Type)
	}
}

func TestStructureForm_parsesSpacedNumericDeclaration(t *testing.T) {
	// Given
	form := newColumnForm(sqlite.ColumnInfo{Name: "amount", Type: "NUMERIC (10,2)", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))

	// Then
	if len(form.parameters) != 2 || form.parameters[0].Value() != "10" || form.parameters[1].Value() != "2" {
		t.Fatalf("parameters = %#v, want editable precision 10 and scale 2", form.parameters)
	}
}

func TestStructureForm_replacesNumericParameterOnEdit(t *testing.T) {
	// Given
	form := newColumnForm(sqlite.ColumnInfo{Name: "amount", Type: "DECIMAL(10,2)", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.focus = columnFieldParameterStart

	// When
	_, _ = form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	_, _ = form.Update(tea.KeyPressMsg{Code: '9', Text: "9"})

	// Then
	if got := form.parameters[0].Value(); got != "9" {
		t.Errorf("precision = %q, want replacement value 9", got)
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
	if model.columnForm.focus != model.columnForm.defaultField() {
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

func TestStructureForm_defaultDisplayDistinguishesNoneFromCleared(t *testing.T) {
	// Given
	noDefault := newColumnForm(sqlite.ColumnInfo{Name: "a", Type: "TEXT"}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	hasDefault := newColumnForm(sqlite.ColumnInfo{Name: "a", Type: "TEXT", DefaultValue: ptr("now")}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))

	// Then
	if got, want := noDefault.defaultDisplay(), "(none)"; !strings.Contains(got, want) {
		t.Errorf("no-default display = %q, want contains %q", got, want)
	}
	if got, want := hasDefault.defaultDisplay(), "now"; !strings.Contains(got, want) {
		t.Errorf("with-default display = %q, want contains %q", got, want)
	}

	// When — user clears an existing default
	hasDefault.preset.SetValue("")

	// Then
	if got, want := hasDefault.defaultDisplay(), "(cleared)"; !strings.Contains(got, want) {
		t.Errorf("cleared display = %q, want contains %q", got, want)
	}
}

func ptr[T any](value T) *T { return &value }

func TestStructureForm_viewHasNoStrayPromptBetweenLabelAndValue(t *testing.T) {
	// Given
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", PrimaryKey: 1, Nullable: false}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.setWidth(40)

	// When
	plain := ansi.Strip(form.View())

	// Then
	for _, row := range strings.Split(plain, "\n") {
		if strings.Contains(row, " > ") {
			t.Errorf("row %q still contains the textinput default prompt", row)
		}
	}
	expectedNameRow := "Name" + strings.Repeat(" ", formLabelWidth-len("Name")+len(formFieldGap)) + "id"
	if !strings.Contains(plain, expectedNameRow) {
		t.Errorf("form view = %q, want a gap between the label and value", plain)
	}
	if !strings.Contains(form.View(), headerStyle.Padding(0, 0).Width(formLabelWidth).Render("Name")) {
		t.Errorf("form view = %q, want the focused label highlighted", form.View())
	}
}
