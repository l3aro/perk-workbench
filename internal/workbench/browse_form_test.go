package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestBrowseForm_enterAndIOpenSelectedRow(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: 'i', Text: "i"}} {
		t.Run(key.String(), func(t *testing.T) {
			// Given
			model := readyBrowseModel(t)

			// When
			updated, _ := model.Update(key)
			model = updated.(Model)

			// Then
			if !model.browseForm.active() || model.browseForm.inputs[1].Value() != "first" {
				t.Fatalf("browse form = %#v, status = %q, want selected row", model.browseForm, model.Status)
			}
		})
	}
}

func TestBrowseForm_savesConfirmedRowChange(t *testing.T) {
	// Given
	model := readyBrowseModel(t)
	model.browse.SetCursor(1)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	model.browseForm.inputs[1].SetValue("edited")

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

func TestBrowseForm_alignsValuesWhenAColumnNameExceedsTheLabelWidth(t *testing.T) {
	// Given
	form, err := newBrowseForm(
		[]string{"id", "very_long_column_name"},
		[]*string{stringPointer("1"), stringPointer("two")},
		[]sqlite.ColumnInfo{{Name: "id", PrimaryKey: 1}},
	)
	if err != nil {
		t.Fatalf("new browse form: %v", err)
	}
	form.setWidth(20)

	// When
	rows := strings.Split(ansi.Strip(form.View()), "\n")

	// Then
	firstValue := strings.Index(rows[0], "1")
	secondValue := strings.Index(rows[1], "two")
	if firstValue != formLabelWidth+len(formFieldGap) || secondValue != firstValue {
		t.Errorf("value columns = %d and %d, want %d", firstValue, secondValue, formLabelWidth+len(formFieldGap))
	}
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
