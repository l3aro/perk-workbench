package workbench

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestInsertForm_aOpensEmptyInsertForm(t *testing.T) {
	// Given
	model := readyBrowseModel(t)

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})

	// Then — insert form open, every field on DEFAULT, no row required
	if !model.browseForm.active() || !model.browseForm.inserting {
		t.Fatal("a did not open the insert form")
	}
	for index := range model.browseForm.values.defaults {
		if !model.browseForm.values.defaults[index] {
			t.Fatalf("field %d not on DEFAULT after open", index)
		}
		if model.browseForm.values.nulls[index] {
			t.Fatalf("field %d starts NULL, want default", index)
		}
		if model.browseForm.values.fields[index] != "" {
			t.Fatalf("field %d = %q, want empty", index, model.browseForm.values.fields[index])
		}
	}
}

func TestInsertForm_allDefaultsProduceNoValues(t *testing.T) {
	// Given — pristine insert form
	form, err := newInsertBrowseForm([]string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	form.table = "items"

	// When
	values := form.rowValues()

	// Then — every column omitted so engine defaults apply
	if len(values) != 0 {
		t.Fatalf("rowValues = %#v, want none", values)
	}
	if got, want := form.preview(), "Table: items"; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestInsertForm_typedFieldProducesStringValue(t *testing.T) {
	// Given
	form, err := newInsertBrowseForm([]string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	form.table = "items"
	form.values.defaults[1] = false
	form.values.fields[1] = "first"

	// When
	values := form.rowValues()

	// Then — DEFAULT fields are omitted so auto-increment applies
	want := []sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "first"}}}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("rowValues = %#v, want %#v", values, want)
	}
	if got, want := form.preview(), "Table: items\nValues:\n  name = \"first\""; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestInsertForm_emptyStringStaysValue(t *testing.T) {
	// Given — field explicitly moved to VALUE, then cleared to ""
	form, err := newInsertBrowseForm([]string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	form.table = "items"
	form.values.defaults[1] = false
	form.values.fields[1] = ""

	// When
	values := form.rowValues()

	// Then — "" must reach the INSERT as a real value, not be omitted
	want := []sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: ""}}}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("rowValues = %#v, want %#v", values, want)
	}
}

func TestInsertForm_nullStateProducesNullValue(t *testing.T) {
	// Given — id set to NULL, name typed
	form, err := newInsertBrowseForm([]string{"id", "name"})
	if err != nil {
		t.Fatal(err)
	}
	form.table = "items"
	form.values.defaults[0] = false
	form.values.nulls[0] = true
	form.values.defaults[1] = false
	form.values.fields[1] = "x"

	// When
	values := form.rowValues()

	// Then
	want := []sharedsql.RowValue{
		{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueNull}},
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "x"}},
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("rowValues = %#v, want %#v", values, want)
	}
	if got, want := form.preview(), "Table: items\nValues:\n  id = NULL\n  name = \"x\""; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestInsertForm_previewGoQuotesStrings(t *testing.T) {
	// Given — a quote-containing value
	form, err := newInsertBrowseForm([]string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	form.table = "items"
	form.values.defaults[0] = false
	form.values.fields[0] = "O'Brien"

	// When
	got := form.preview()

	// Then — the preview Go-quotes the scalar; the driver binds the value
	if want := "Table: items\nValues:\n  name = \"O'Brien\""; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestInsertForm_rejectsEmptyColumnSet(t *testing.T) {
	if _, err := newInsertBrowseForm(nil); err == nil || !strings.Contains(err.Error(), "no columns") {
		t.Fatalf("error = %v, want no-columns rejection", err)
	}
}

func TestInsertForm_setDefaultReturnsFieldToOmission(t *testing.T) {
	// Given — a typed field, then N returns it to DEFAULT
	model := readyBrowseModel(t)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
	model.browseForm.values.fields[0] = "5"
	model.browseForm.values.defaults[0] = false

	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'N', Text: "N"})

	// Then
	if !model.browseForm.values.defaults[0] || model.browseForm.values.nulls[0] || model.browseForm.values.fields[0] != "" {
		t.Fatalf("field 0 = defaults %t nulls %t value %q, want default", model.browseForm.values.defaults[0], model.browseForm.values.nulls[0], model.browseForm.values.fields[0])
	}
	if values := model.browseForm.rowValues(); len(values) != 0 {
		t.Fatalf("rowValues = %#v, want none after returning to DEFAULT", values)
	}
}

func TestInsertForm_typingSelectsValue(t *testing.T) {
	// Given — vim off: the form opens directly in insert mode
	model := readyBrowseModel(t)
	model.vimMode = false
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !model.formMode.editing() {
		t.Fatal("insert form did not open in insert mode without vim mode")
	}

	// When — type into the focused (first) field
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'x', Text: "x"})

	// Then — typing left the DEFAULT state
	if model.browseForm.values.defaults[0] {
		t.Fatal("typing did not leave the DEFAULT state")
	}
	if got := model.browseForm.values.fields[0]; got != "x" {
		t.Fatalf("field 0 = %q, want x", got)
	}
	want := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "x"}}}
	if values := model.browseForm.rowValues(); !reflect.DeepEqual(values, want) {
		t.Fatalf("rowValues = %#v, want %#v", values, want)
	}
}

func TestInsertForm_savesInsertedRow(t *testing.T) {
	// Given — insert form with a typed name
	model := readyBrowseModel(t)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
	model.browseForm.values.defaults[1] = false
	model.browseForm.values.fields[1] = "third"

	// When — save and confirm
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then — form closed, row inserted with the engine-assigned id
	if model.browseForm.active() {
		t.Fatal("insert form remained open after save")
	}
	var insertEntry *queryLogEntry
	for index := range min(len(model.queryLogEntries), 3) {
		if model.queryLogEntries[index].message == "inserted 1 row" {
			insertEntry = &model.queryLogEntries[index]
		}
	}
	if insertEntry == nil {
		t.Fatalf("query log = %#v, want inserted 1 row entry", model.queryLogEntries)
	}
	if got, want := insertEntry.statement, "Table: items\nValues:\n  name = \"third\""; got != want {
		t.Fatalf("query log statement = %q, want preview %q", got, want)
	}
	result, err := model.Database.Execute(model.appContext, "SELECT id, name FROM items ORDER BY id")
	if err != nil {
		t.Fatalf("selecting rows: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(result.Rows))
	}
	if got, want := *result.Rows[2][0], "3"; got != want {
		t.Fatalf("new id = %q, want %q", got, want)
	}
	if got, want := *result.Rows[2][1], "third"; got != want {
		t.Fatalf("new name = %q, want %q", got, want)
	}
	if got := model.browse.Rows(); len(got) != 3 {
		t.Fatalf("browse rows = %d, want refreshed 3", len(got))
	}
}

func TestInsertForm_allDefaultsInsertsEngineRow(t *testing.T) {
	// Given — pristine insert form
	model := readyBrowseModel(t)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})

	// When — save a pure-default row
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then — DEFAULT VALUES ran and the auto-increment id advanced
	if model.browseForm.active() {
		t.Fatal("insert form remained open after save")
	}
	result, err := model.Database.Execute(model.appContext, "SELECT COUNT(*) FROM items")
	if err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if got, want := *result.Rows[0][0], "3"; got != want {
		t.Fatalf("row count = %q, want %q", got, want)
	}
}

func TestInsertForm_readOnlyKeepsFormOpen(t *testing.T) {
	// Given — read-only connection with a typed field
	model := readyBrowseModel(t)
	model.ReadOnly = true
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
	model.browseForm.values.defaults[1] = false
	model.browseForm.values.fields[1] = "third"

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then — rejected, form and input preserved
	if !model.browseForm.active() || model.browseForm.saving {
		t.Fatalf("form = %#v, want retained unsaved insert form", model.browseForm)
	}
	if !strings.Contains(model.Status, "read-only") {
		t.Fatalf("status = %q, want read-only rejection", model.Status)
	}
}
