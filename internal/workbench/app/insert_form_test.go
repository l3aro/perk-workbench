package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInsertForm_aOpensEmptyInsertForm(t *testing.T) {
	// Given
	model := readyBrowseModel(t)

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})

	// Then — insert form open, every field on DEFAULT, no row required
	if !model.browse.component.Form.Active() || !model.browse.component.Form.Inserting {
		t.Fatal("a did not open the insert form")
	}
	for index := range model.browse.component.Form.Values.Defaults {
		if !model.browse.component.Form.Values.Defaults[index] {
			t.Fatalf("field %d not on DEFAULT after open", index)
		}
		if model.browse.component.Form.Values.Nulls[index] {
			t.Fatalf("field %d starts NULL, want default", index)
		}
		if model.browse.component.Form.Values.Fields[index] != "" {
			t.Fatalf("field %d = %q, want empty", index, model.browse.component.Form.Values.Fields[index])
		}
	}
}

func TestInsertForm_savesInsertedRow(t *testing.T) {
	// Given — insert form with a typed name
	model := readyBrowseModel(t)
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
	model.browse.component.Form.Values.Defaults[1] = false
	model.browse.component.Form.Values.Fields[1] = "third"

	// When — save and confirm
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then — form closed, row inserted with the engine-assigned id
	if model.browse.component.Form.Active() {
		t.Fatal("insert form remained open after save")
	}
	var insertEntry *queryLogEntry
	for index := range min(len(model.queryLog.component.Entries), 3) {
		if model.queryLog.component.Entries[index].Message == "inserted 1 row" {
			insertEntry = &model.queryLog.component.Entries[index]
		}
	}
	if insertEntry == nil {
		t.Fatalf("query log = %#v, want inserted 1 row entry", model.queryLog.component.Entries)
	}
	if got, want := insertEntry.Statement, "Table: items\nValues:\n  name = \"third\""; got != want {
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
	if got := model.browse.component.Table.Rows(); len(got) != 3 {
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
	if model.browse.component.Form.Active() {
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
	model.browse.component.Form.Values.Defaults[1] = false
	model.browse.component.Form.Values.Fields[1] = "third"

	// When
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then — rejected, form and input preserved
	if !model.browse.component.Form.Active() || model.browse.component.Form.Saving {
		t.Fatalf("form = %#v, want retained unsaved insert form", model.browse.component.Form)
	}
	if !strings.Contains(model.Status, "read-only") {
		t.Fatalf("status = %q, want read-only rejection", model.Status)
	}
}
