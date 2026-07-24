package workbench

import (
	"strings"
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
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	model.foreignKeyForm.values.columns = "parent_id"
	model.foreignKeyForm.values.referenceTable = "parents"
	model.foreignKeyForm.values.referenceColumns = "id"
	model.foreignKeyForm.values.onDelete = "CASCADE"
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.foreignKeyForm.active() || model.foreignKeyForm.confirming() || len(model.foreignKeys.Rows()) != 0 {
		t.Fatal("negative create confirmation changed foreign keys")
	}
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.foreignKeyForm.active() {
		t.Fatal("saved foreign-key form remained active")
	}
	if got := model.foreignKeys.Rows(); len(got) != 1 || got[0][1] != "parent_id" || got[0][2] != "parents" || got[0][3] != "id" || got[0][4] != "CASCADE" {
		t.Fatalf("foreign-key rows = %#v, want parent_id references parents(id) on delete cascade", got)
	}

	// When
	model.foreignKeys.SetCursor(0)
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model.foreignKeyForm.values.columns = "code"
	model.foreignKeyForm.values.referenceColumns = "code"
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.foreignKeyForm.active() || model.foreignKeyForm.confirming() || model.foreignKeys.Rows()[0][1] != "parent_id" {
		t.Fatal("negative edit confirmation changed foreign keys")
	}
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if got := model.foreignKeys.Rows(); len(got) != 1 || got[0][1] != "code" || got[0][3] != "code" {
		t.Fatalf("foreign-key rows after edit = %#v, want code references parents(code)", got)
	}

	// When
	model.foreignKeys.SetCursor(0)
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.foreignKeyForm.active() || model.foreignKeyForm.confirming() || len(model.foreignKeys.Rows()) != 1 {
		t.Fatal("negative delete confirmation changed foreign keys")
	}
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if got := model.foreignKeys.Rows(); len(got) != 0 {
		t.Fatalf("foreign-key rows after delete = %#v, want no foreign keys", got)
	}
}

func TestForeignKeyForm_requiredFieldsRenderAndBlockConfirmation(t *testing.T) {
	// Given
	form := newForeignKeyForm(nil)
	form.keybindings = DefaultKeybindings()
	form.setWidth(80)
	_ = form.form.Init()

	// Then
	for _, label := range []string{"Columns*", "Reference table*", "Reference columns*"} {
		if !strings.Contains(form.View(), label) {
			t.Fatalf("foreign-key form view missing required label %q: %s", label, form.View())
		}
	}

	for _, test := range []struct {
		columns, referenceTable, referenceColumns, field string
	}{
		{referenceTable: "parents", referenceColumns: "id", field: "columns"},
		{columns: "parent_id", referenceColumns: "id", field: "reference-table"},
		{columns: "parent_id", referenceTable: "parents", field: "reference-columns"},
	} {
		t.Run(test.field, func(t *testing.T) {
			// Given
			form := newForeignKeyForm(nil)
			form.keybindings = DefaultKeybindings()
			form.setWidth(80)
			_ = form.form.Init()
			form.values.columns, form.values.referenceTable, form.values.referenceColumns = test.columns, test.referenceTable, test.referenceColumns

			// When
			_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyF5}, &formModeController{})

			// Then
			field := form.form.GetFocusedField()
			if form.confirming() || field.GetKey() != test.field || field.Error() == nil {
				t.Fatalf("confirmation/focused error = %t/%q/%v, want false/%q/error", form.confirming(), field.GetKey(), field.Error(), test.field)
			}
		})
	}
}

func TestForeignKeyForm_invalidColumnListsBlockConfirmation(t *testing.T) {
	for _, test := range []struct {
		columns, referenceColumns, field string
	}{
		{columns: "parent_id,", referenceColumns: "id", field: "columns"},
		{columns: "parent_id,parent_id", referenceColumns: "id,id", field: "columns"},
		{columns: "parent_id", referenceColumns: "id,", field: "reference-columns"},
		{columns: "parent_id,code", referenceColumns: "id", field: "reference-columns"},
	} {
		t.Run(test.field, func(t *testing.T) {
			// Given
			form := newForeignKeyForm(nil)
			form.keybindings = DefaultKeybindings()
			form.values.columns, form.values.referenceTable, form.values.referenceColumns = test.columns, "parents", test.referenceColumns

			// When
			_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyF5}, &formModeController{})

			// Then
			field := form.form.GetFocusedField()
			if form.confirming() || field.GetKey() != test.field || field.Error() == nil {
				t.Fatalf("confirmation/focused error = %t/%q/%v, want false/%q/error", form.confirming(), field.GetKey(), field.Error(), test.field)
			}
		})
	}
}

func TestForeignKeyForm_huhInputsBuildForeignKeyChange(t *testing.T) {
	// Given
	form := newForeignKeyForm(nil)
	form.keybindings = DefaultKeybindings()
	controller := &formModeController{}
	_ = form.form.Init()

	// When
	for _, value := range []string{"parent_id", "parents", "id"} {
		_, _ = form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, controller)
		for _, character := range value {
			_, _ = form.Update(tea.KeyPressMsg{Code: character, Text: string(character)}, controller)
		}
		_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, controller)
		_, _ = form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, controller)
	}
	change, err := form.change()

	// Then
	if err != nil || !equalStrings(change.Columns, []string{"parent_id"}) || change.ReferenceTable != "parents" || !equalStrings(change.ReferenceColumns, []string{"id"}) || change.OnDelete != "NO ACTION" || change.OnUpdate != "NO ACTION" {
		t.Fatalf("change/error = %#v/%v", change, err)
	}
}

func TestForeignKeyForm_normalModeDoesNotMutateHuhValues(t *testing.T) {
	// Given
	form := newForeignKeyForm(nil)
	_ = form.form.Init()

	// When
	_, _ = form.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, &formModeController{})

	// Then
	if got := form.values.columns; got != "" {
		t.Fatalf("normal-mode input changed columns to %q", got)
	}
}

func updateForeignKeyForm(model Model, message tea.Msg) Model {
	updated, _ := model.Update(message)
	return updated.(Model)
}

func resolveForeignKeyCommand(model Model, message tea.Msg) Model {
	updated, command := model.Update(message)
	model = updated.(Model)
	for range 4 {
		if command == nil {
			return model
		}
		message = command()
		if batch, ok := message.(tea.BatchMsg); ok {
			for _, next := range batch {
				model = resolveForeignKeyCommand(model, next())
			}
			return model
		}
		updated, command = model.Update(message)
		model = updated.(Model)
	}
	return model
}
