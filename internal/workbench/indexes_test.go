package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestIndexForm_usesHuhControlsForRequiredFields(t *testing.T) {
	// Given
	form := newIndexForm(nil)

	// Then
	if form.form == nil || form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("index form = %#v, want Huh fields focused on name", form)
	}
}

func TestIndexForm_huhInputUpdatesIndexChange(t *testing.T) {
	// Given
	model := openIndexEditor(t, readyIndexesModel(t), nil)

	// When
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, character := range "items_name" {
		model = updateIndexForm(model, tea.KeyPressMsg{Code: character, Text: string(character)})
	}
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, character := range "name" {
		model = updateIndexForm(model, tea.KeyPressMsg{Code: character, Text: string(character)})
	}
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	change, err := model.indexForm.change()

	// Then
	if err != nil || change.Name != "items_name" || !equalStrings(change.Columns, []string{"name"}) {
		t.Fatalf("change/error = %#v/%v", change, err)
	}
}

func TestIndexForm_blankRequiredFieldsCannotReachSaveConfirmation(t *testing.T) {
	for _, test := range []struct {
		values indexFormValues
		field  string
	}{
		{values: indexFormValues{columns: "name", kind: indexKindNormal}, field: "name"},
		{values: indexFormValues{name: "items_name", kind: indexKindNormal}, field: "columns"},
	} {
		t.Run(test.field, func(t *testing.T) {
			// Given
			model := openIndexEditor(t, readyIndexesModel(t), nil)
			model.indexForm.values.name, model.indexForm.values.columns, model.indexForm.values.kind = test.values.name, test.values.columns, test.values.kind

			// When
			model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyF5})

			// Then
			field := model.indexForm.form.GetFocusedField()
			if model.indexForm.confirming() || field.GetKey() != test.field || field.Error() == nil {
				t.Fatalf("confirmation/focused error = %t/%q/%v, want false/%q/error", model.indexForm.confirming(), field.GetKey(), field.Error(), test.field)
			}
		})
	}
}

func TestIndexForm_invalidSaveRendersActiveHuhFieldError(t *testing.T) {
	// Given
	model := openIndexEditor(t, readyIndexesModel(t), nil)
	model.indexForm.setWidth(80)
	model.indexForm.values.columns = "name"

	// When
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyF5})

	// Then
	field := model.indexForm.form.GetFocusedField()
	if model.indexForm.confirming() || field.GetKey() != "name" || field.Error() == nil || !strings.Contains(model.indexForm.View(), "index name is required") {
		t.Fatalf("index form = %q, want active name field error", model.indexForm.View())
	}
}

func TestIndexForm_huhSelectPrimaryCreatesIndexWithoutName(t *testing.T) {
	// Given
	model := openIndexEditor(t, readyIndexesModel(t), nil)

	// When
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, character := range "code" {
		model = updateIndexForm(model, tea.KeyPressMsg{Code: character, Text: string(character)})
	}
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := model.indexForm.values.kind; got != indexKindPrimary {
		t.Fatalf("selected kind = %q, want %q", got, indexKindPrimary)
	}
	change, err := model.indexForm.change()
	if err != nil || change.Name != "PRIMARY" || !change.PrimaryKey || change.Unique || !equalStrings(change.Columns, []string{"code"}) {
		t.Fatalf("change/error = %#v/%v", change, err)
	}
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.indexForm.active() || len(model.indexes.Rows()) != 1 || model.indexes.Rows()[0][0] != "PRIMARY" || model.indexes.Rows()[0][1] != "primary key" || model.indexes.Rows()[0][2] != "code" {
		t.Fatalf("primary index rows = %#v", model.indexes.Rows())
	}
}

func TestIndexForm_savesEditsAndDeletesOnlyAfterPositiveConfirmation(t *testing.T) {
	// Given
	model := openIndexEditor(t, readyIndexesModel(t), nil)
	model.indexForm.values.name, model.indexForm.values.columns = "items_category", "category"

	// When
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.indexForm.active() || model.indexForm.confirming() {
		t.Fatal("negative save confirmation changed the index form")
	}
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.indexForm.active() || len(model.indexes.Rows()) != 1 || model.indexes.Rows()[0][0] != "items_category" {
		t.Fatalf("created indexes = %#v", model.indexes.Rows())
	}

	// When
	model.indexes.SetCursor(0)
	model = openIndexEditor(t, model, &sharedsql.IndexInfo{Name: "items_category", Columns: []string{"category"}})
	model.indexForm.values.name, model.indexForm.values.columns, model.indexForm.values.kind = "items_category_unique", "category", indexKindUnique
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if got := model.indexes.Rows(); len(got) != 1 || got[0][0] != "items_category_unique" || got[0][1] != "unique" {
		t.Fatalf("edited indexes = %#v", got)
	}

	// When
	model = openIndexEditor(t, model, &sharedsql.IndexInfo{Name: "items_category_unique", Unique: true, Columns: []string{"category"}})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !model.indexForm.active() || model.indexForm.confirming() {
		t.Fatal("negative delete confirmation changed the index form")
	}
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if got := model.indexes.Rows(); len(got) != 0 {
		t.Fatalf("indexes after delete = %#v", got)
	}
}

func TestIndexForm_primaryAndUniqueChangesAreMutuallyExclusive(t *testing.T) {
	for kind, want := range map[string]sharedsql.IndexChange{
		indexKindUnique:  {Name: "items_code", Unique: true, Columns: []string{"code"}},
		indexKindPrimary: {Name: "PRIMARY", PrimaryKey: true, Columns: []string{"code"}},
	} {
		t.Run(kind, func(t *testing.T) {
			// Given
			form := newIndexForm(nil)
			form.values.name, form.values.columns, form.values.kind = "items_code", "code", kind

			// When
			change, err := form.change()

			// Then
			if err != nil || change.Name != want.Name || change.Unique != want.Unique || change.PrimaryKey != want.PrimaryKey || !equalStrings(change.Columns, want.Columns) {
				t.Fatalf("change/error = %#v/%v, want %#v", change, err, want)
			}
		})
	}
}

func TestIndexForm_positiveDiscardConfirmationClosesWithoutPersistence(t *testing.T) {
	// Given
	model := openIndexEditor(t, readyIndexesModel(t), nil)
	model.indexForm.values.name, model.indexForm.values.columns = "items_category", "category"

	// When
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = resolveIndexCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Then
	if model.indexForm.active() || len(model.indexes.Rows()) != 0 {
		t.Fatalf("discard form/indexes = %#v/%#v", model.indexForm, model.indexes.Rows())
	}
}

func TestIndexForm_discardWithoutChangesClosesWithoutConfirmation(t *testing.T) {
	// Given — new index form open, no edits made
	model := openIndexEditor(t, readyIndexesModel(t), nil)

	// When — Escape to discard
	model = updateIndexForm(model, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Then — form closes directly, no confirmation, mode normalized
	if model.indexForm.active() || model.indexForm.confirming() {
		t.Fatal("unchanged discard opened a confirmation")
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("form mode = %d, want normal", model.formMode.mode)
	}
}

func readyIndexesModel(t *testing.T) Model {
	t.Helper()
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (id INTEGER, name TEXT, category TEXT, code TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(indexesLoadedMsg{table: "items"})
	return updated.(Model)
}

func openIndexEditor(t *testing.T, model Model, index *sharedsql.IndexInfo) Model {
	t.Helper()
	if index != nil {
		model.indexForm = newIndexForm(index)
		_ = model.indexForm.form.Init()
		return model
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	return updated.(Model)
}

func updateIndexForm(model Model, message tea.Msg) Model {
	updated, _ := model.Update(message)
	return updated.(Model)
}

func resolveIndexCommand(model Model, message tea.Msg) Model {
	updated, command := model.Update(message)
	model = updated.(Model)
	return driveCommand(model, command)
}
