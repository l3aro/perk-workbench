package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

func TestIndexesTab_loadsAndManagesIndexes(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (id INTEGER, name TEXT, category TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{{Name: "items_name", Columns: []string{"name"}}}})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	model.indexForm.name.SetValue("items_category")
	model.indexForm.columns.SetValue("category")
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
	if model.indexForm.active() {
		t.Fatal("saved index form remained active")
	}
	if got := model.indexes.Rows(); len(got) != 1 || got[0][0] != "items_category" || got[0][2] != "category" {
		t.Fatalf("index rows = %#v, want items_category(category)", got)
	}

	// When
	model.indexes.SetCursor(0)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.indexForm.name.SetValue("items_category_unique")
	model.indexForm.unique = true
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, refresh = model.Update(command())
	model = updated.(Model)
	model = updateFromCommand(model, refresh)

	// Then
	if got := model.indexes.Rows(); len(got) != 1 || got[0][0] != "items_category_unique" || got[0][1] != "unique" {
		t.Fatalf("index rows after edit = %#v, want items_category_unique(unique)", got)
	}

	// When
	model.indexes.SetCursor(0)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, refresh = model.Update(command())
	model = updated.(Model)
	model = updateFromCommand(model, refresh)

	// Then
	if got := model.indexes.Rows(); len(got) != 0 {
		t.Fatalf("index rows after delete = %#v, want no rows", got)
	}
}

func TestIndexesTab_managesPrimaryKey(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (id INTEGER, code TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: nil})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	model.indexForm.columns.SetValue("id")
	for range 3 {
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = updated.(Model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: ' '})
	model = updated.(Model)
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
	if got := model.indexes.Rows(); len(got) != 1 || got[0][0] != "PRIMARY" || got[0][1] != "primary key" || got[0][2] != "id" {
		t.Fatalf("index rows = %#v, want PRIMARY(id)", got)
	}

	// When
	model.indexes.SetCursor(0)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.indexForm.columns.SetValue("code")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, refresh = model.Update(command())
	model = updated.(Model)
	model = updateFromCommand(model, refresh)

	// Then
	if got := model.indexes.Rows(); len(got) != 1 || got[0][2] != "code" {
		t.Fatalf("index rows after edit = %#v, want PRIMARY(code)", got)
	}

	// When
	model.indexes.SetCursor(0)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, refresh = model.Update(command())
	model = updated.(Model)
	model = updateFromCommand(model, refresh)

	// Then
	if got := model.indexes.Rows(); len(got) != 0 {
		t.Fatalf("index rows after delete = %#v, want no primary key", got)
	}
}
