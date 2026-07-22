package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestForeignKeysTab_loadsAndManagesForeignKeys(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "children", tabForeignKeys, focusWorkspace
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE parents (id INTEGER PRIMARY KEY, code TEXT UNIQUE)"); err != nil {
		t.Fatalf("creating parents: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE children (parent_id INTEGER, code TEXT)"); err != nil {
		t.Fatalf("creating children: %v", err)
	}
	updated, _ := model.Update(foreignKeysLoadedMsg{table: "children", foreignKeys: nil})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	model.foreignKeyForm.columns.SetValue("parent_id")
	model.foreignKeyForm.referenceTable.SetValue("parents")
	model.foreignKeyForm.referenceColumns.SetValue("id")
	model.foreignKeyForm.onDelete.SetValue("CASCADE")
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
	if model.foreignKeyForm.active() {
		t.Fatal("saved foreign-key form remained active")
	}
	if got := model.foreignKeys.Rows(); len(got) != 1 || got[0][1] != "parent_id" || got[0][2] != "parents" || got[0][3] != "id" || got[0][4] != "CASCADE" {
		t.Fatalf("foreign-key rows = %#v, want parent_id references parents(id) on delete cascade", got)
	}

	// When
	model.foreignKeys.SetCursor(0)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.foreignKeyForm.columns.SetValue("code")
	model.foreignKeyForm.referenceColumns.SetValue("code")
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
	if got := model.foreignKeys.Rows(); len(got) != 1 || got[0][1] != "code" || got[0][3] != "code" {
		t.Fatalf("foreign-key rows after edit = %#v, want code references parents(code)", got)
	}

	// When
	model.foreignKeys.SetCursor(0)
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
	if got := model.foreignKeys.Rows(); len(got) != 0 {
		t.Fatalf("foreign-key rows after delete = %#v, want no foreign keys", got)
	}
}
