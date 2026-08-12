package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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
	model.schema.component.Structure.ForeignKeyForm.Values.Columns = "parent_id"
	model.schema.component.Structure.ForeignKeyForm.Values.ReferenceTable = "parents"
	model.schema.component.Structure.ForeignKeyForm.Values.ReferenceColumns = "id"
	model.schema.component.Structure.ForeignKeyForm.Values.OnDelete = "CASCADE"
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.schema.component.Structure.ForeignKeyForm.Active() || model.schema.component.Structure.ForeignKeyForm.Confirming() || len(model.schema.component.Structure.ForeignKeys.Rows()) != 0 {
		t.Fatal("negative create confirmation changed foreign keys")
	}
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.schema.component.Structure.ForeignKeyForm.Active() {
		t.Fatal("saved foreign-key form remained active")
	}
	if got := model.schema.component.Structure.ForeignKeys.Rows(); len(got) != 1 || got[0][1] != "parent_id" || got[0][2] != "parents" || got[0][3] != "id" || got[0][4] != "CASCADE" {
		t.Fatalf("foreign-key rows = %#v, want parent_id references parents(id) on delete cascade", got)
	}

	// When
	model.schema.component.Structure.ForeignKeys.SetCursor(0)
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model.schema.component.Structure.ForeignKeyForm.Values.Columns = "code"
	model.schema.component.Structure.ForeignKeyForm.Values.ReferenceColumns = "code"
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.schema.component.Structure.ForeignKeyForm.Active() || model.schema.component.Structure.ForeignKeyForm.Confirming() || model.schema.component.Structure.ForeignKeys.Rows()[0][1] != "parent_id" {
		t.Fatal("negative edit confirmation changed foreign keys")
	}
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if got := model.schema.component.Structure.ForeignKeys.Rows(); len(got) != 1 || got[0][1] != "code" || got[0][3] != "code" {
		t.Fatalf("foreign-key rows after edit = %#v, want code references parents(code)", got)
	}

	// When
	model.schema.component.Structure.ForeignKeys.SetCursor(0)
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if model.overlay.deleteConfirm == nil || len(model.schema.component.Structure.ForeignKeys.Rows()) != 1 {
		t.Fatal("d did not open delete confirmation")
	}
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if model.overlay.deleteConfirm != nil || len(model.schema.component.Structure.ForeignKeys.Rows()) != 1 {
		t.Fatal("negative delete confirmation changed foreign keys")
	}
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = resolveForeignKeyCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if got := model.schema.component.Structure.ForeignKeys.Rows(); len(got) != 0 {
		t.Fatalf("foreign-key rows after delete = %#v, want no foreign keys", got)
	}
}

func TestForeignKeyForm_requiredFieldsRenderAndBlockConfirmation(t *testing.T) {
	// Given
	form := schema.NewForeignKeyForm(nil)
	form.SetKeys(DefaultKeybindings())
	form.SetKeys(DefaultKeybindings())
	form.SetWidth(80)
	_ = form.Form.Init()

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
			form := schema.NewForeignKeyForm(nil)
			form.SetKeys(DefaultKeybindings())
			form.SetKeys(DefaultKeybindings())
			form.SetWidth(80)
			_ = form.Form.Init()
			form.Values.Columns, form.Values.ReferenceTable, form.Values.ReferenceColumns = test.columns, test.referenceTable, test.referenceColumns

			// When
			_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyF5}, &formModeController{})

			// Then
			field := form.Form.GetFocusedField()
			if form.Confirming() || field.GetKey() != test.field || field.Error() == nil {
				t.Fatalf("confirmation/focused error = %t/%q/%v, want false/%q/error", form.Confirming(), field.GetKey(), field.Error(), test.field)
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
			form := schema.NewForeignKeyForm(nil)
			form.SetKeys(DefaultKeybindings())
			form.SetKeys(DefaultKeybindings())
			form.Values.Columns, form.Values.ReferenceTable, form.Values.ReferenceColumns = test.columns, "parents", test.referenceColumns

			// When
			_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyF5}, &formModeController{})

			// Then
			field := form.Form.GetFocusedField()
			if form.Confirming() || field.GetKey() != test.field || field.Error() == nil {
				t.Fatalf("confirmation/focused error = %t/%q/%v, want false/%q/error", form.Confirming(), field.GetKey(), field.Error(), test.field)
			}
		})
	}
}

func TestForeignKeyForm_huhInputsBuildForeignKeyChange(t *testing.T) {
	// Given
	form := schema.NewForeignKeyForm(nil)
	form.SetKeys(DefaultKeybindings())
	form.SetKeys(DefaultKeybindings())
	controller := &formModeController{}
	_ = form.Form.Init()

	// When
	for _, value := range []string{"parent_id", "parents", "id"} {
		_, _ = form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, controller)
		for _, character := range value {
			_, _ = form.Update(tea.KeyPressMsg{Code: character, Text: string(character)}, controller)
		}
		_, _ = form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, controller)
		_, _ = form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, controller)
	}
	change, err := form.Change()

	// Then
	if err != nil || !equalStrings(change.Columns, []string{"parent_id"}) || change.ReferenceTable != "parents" || !equalStrings(change.ReferenceColumns, []string{"id"}) || change.OnDelete != "NO ACTION" || change.OnUpdate != "NO ACTION" {
		t.Fatalf("change/error = %#v/%v", change, err)
	}
}

func TestForeignKeyForm_normalModeDoesNotMutateHuhValues(t *testing.T) {
	// Given
	form := schema.NewForeignKeyForm(nil)
	form.SetKeys(DefaultKeybindings())
	_ = form.Form.Init()

	// When
	_, _ = form.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, &formModeController{})

	// Then
	if got := form.Values.Columns; got != "" {
		t.Fatalf("normal-mode input changed columns to %q", got)
	}
}

func TestForeignKeyForm_discardWithoutChangesClosesWithoutConfirmation(t *testing.T) {
	// Given — new foreign-key form open, no edits made
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "children", tabForeignKeys, focusWorkspace
	updated, _ := model.Update(foreignKeysLoadedMsg{table: "children", foreignKeys: nil})
	model = updated.(Model)
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.schema.component.Structure.ForeignKeyForm.Active() {
		t.Fatal("fixture: 'n' did not open a new foreign-key form")
	}

	// When — Escape to discard
	model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Then — form closes directly, no confirmation, mode normalized
	if model.schema.component.Structure.ForeignKeyForm.Active() || model.schema.component.Structure.ForeignKeyForm.Confirming() {
		t.Fatal("unchanged discard opened a confirmation")
	}
	if model.overlay.formMode.Mode != formModeNormal {
		t.Fatalf("form mode = %d, want normal", model.overlay.formMode.Mode)
	}
}

func updateForeignKeyForm(model Model, message tea.Msg) Model {
	updated, _ := model.Update(message)
	return updated.(Model)
}

func resolveForeignKeyCommand(model Model, message tea.Msg) Model {
	updated, command := model.Update(message)
	model = updated.(Model)
	return driveCommand(model, command)
}
