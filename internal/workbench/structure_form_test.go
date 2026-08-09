package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestStructureForm_buttonsNavigableFromLastField(t *testing.T) {
	model := openColumn(t, "name", "TEXT")

	// j to the last field (attributes), then j again onto the button bar.
	for range 4 {
		model = updateColumn(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if got := model.columnForm.form.GetFocusedField().GetKey(); got != "attributes" {
		t.Fatalf("focused field = %q, want attributes", got)
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !model.formMode.buttonsFocused || model.formMode.buttonChoice != 0 {
		t.Fatalf("bar = focused:%t choice:%d, want focused on Save", model.formMode.buttonsFocused, model.formMode.buttonChoice)
	}

	// h/l switch the choice between Save and Cancel.
	model = updateColumn(model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if model.formMode.buttonChoice != 1 {
		t.Fatalf("button choice = %d, want 1 (Cancel)", model.formMode.buttonChoice)
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if model.formMode.buttonChoice != 0 {
		t.Fatalf("button choice = %d, want 0 (Save)", model.formMode.buttonChoice)
	}

	// k returns to the last field.
	model = updateColumn(model, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if model.formMode.buttonsFocused {
		t.Fatal("k on the button bar did not return to the fields")
	}
	if got := model.columnForm.form.GetFocusedField().GetKey(); got != "attributes" {
		t.Fatalf("focused field = %q, want attributes", got)
	}

	// Enter on the focused Save button opens the save confirmation.
	model = updateColumn(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.columnForm.confirming() || !model.columnForm.confirmationSave {
		t.Fatalf("form = confirming:%t save:%t, want confirming save", model.columnForm.confirming(), model.columnForm.confirmationSave)
	}
	if model.formMode.mode != formModeConfirm {
		t.Fatalf("mode = %d, want confirm", model.formMode.mode)
	}
}

func TestStructureForm_barConfirmationDismissKeepsBarFocus(t *testing.T) {
	model := openColumn(t, "name", "TEXT")

	// Navigate onto the Save button and activate it.
	for range 5 {
		model = updateColumn(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if !model.formMode.buttonsFocused {
		t.Fatal("fixture: expected the button bar focused")
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.columnForm.confirming() || !model.columnForm.confirmationSave {
		t.Fatalf("form = confirming:%t save:%t, want confirming save", model.columnForm.confirming(), model.columnForm.confirmationSave)
	}

	// Dismiss the dialog (move to No, then Enter): the bar must keep focus,
	// with the field underneath blurred.
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyRight})
	if !model.columnForm.confirming() {
		t.Fatal("right arrow dismissed the confirmation")
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.columnForm.confirming() {
		t.Fatal("Enter on No did not dismiss the confirmation")
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal after dismissal", model.formMode.mode)
	}
	if !model.formMode.buttonsFocused {
		t.Fatal("dismissing a bar-initiated confirmation lost the bar focus")
	}

	// Enter on the still-focused bar activates again.
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.columnForm.confirming() {
		t.Fatal("Enter on the retained bar focus did not re-open the confirmation")
	}
}

func TestStructureForm_usesHuhControlsForColumnEditing(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", PrimaryKey: 1}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if form.form == nil {
		t.Fatal("column editor did not create a Huh form")
	}
	if form.form.GetFocusedField().GetKey() != "name" {
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
	model.columnForm.values.name = "renamed"
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

func TestStructureForm_discardWithoutChangesClosesWithoutConfirmation(t *testing.T) {
	// Given — column form open, no edits made
	model := openColumn(t, "name", "TEXT")

	// When — Escape to discard
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Then — form closes directly, no confirmation, mode normalized
	if model.columnForm.active() || model.columnForm.confirming() {
		t.Fatal("unchanged discard opened a confirmation")
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("form mode = %d, want normal", model.formMode.mode)
	}
}

func TestStructureForm_newColumnDiscardWithoutChangesClosesWithoutConfirmation(t *testing.T) {
	// Given — new column form open, no edits made
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if !model.columnForm.active() || !model.columnForm.isNew {
		t.Fatal("fixture: 'a' did not open a new column form")
	}

	// When — Escape to discard
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Then — form closes directly, no confirmation
	if model.columnForm.active() || model.columnForm.confirming() {
		t.Fatal("unchanged new-column discard opened a confirmation")
	}
}

// TestColumnForm_clearingDefaultCountsAsChange guards the presence
// transition: a column with a default that gets cleared would persist as
// "drop default", so the discard path must treat it as a change — while a
// pristine form stays unchanged.
func TestColumnForm_clearingDefaultCountsAsChange(t *testing.T) {
	// Given — column with a non-empty default
	form := newColumnForm(sqlite.ColumnInfo{Name: "status", Type: "TEXT", DefaultValue: stringPointer("active")}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if form.hasChanges() {
		t.Fatal("pristine form reported changes")
	}

	// When — the user clears the default field
	form.values.defaultValue = ""

	// Then — the clear is a change (a save would drop the default)
	if !form.hasChanges() {
		t.Fatal("cleared default not detected as a change")
	}
}

// TestColumnForm_whitespaceDefaultStaysPristine guards the raw-equality
// baseline: a default that is only whitespace must not make an untouched
// form look changed, while clearing it still does.
func TestColumnForm_whitespaceDefaultStaysPristine(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "note", Type: "TEXT", DefaultValue: stringPointer(" ")}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if form.hasChanges() {
		t.Fatal("pristine whitespace-default form reported changes")
	}
	form.values.defaultValue = ""
	if !form.hasChanges() {
		t.Fatal("cleared whitespace default not detected as a change")
	}
}

func TestStructureForm_confirmationMouseReleaseUsesScreenCoordinates(t *testing.T) {
	// Given
	model := resizeModel(openColumn(t, "name", "TEXT"), 100, 30)
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	dialog := model.columnForm.confirmation
	if dialog == nil {
		t.Fatal("save confirmation = nil")
	}
	layout := dialog.layout(model.width, model.height)

	// When
	model = updateColumn(model, tea.MouseReleaseMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseNone})

	// Then
	if !model.columnForm.saving {
		t.Fatal("mouse release did not confirm the column save")
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

func TestStructureForm_normalModeNavigatesFields(t *testing.T) {
	model := openColumn(t, "name", "TEXT")

	model = updateColumn(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got, want := model.columnForm.form.GetFocusedField().GetKey(), "type"; got != want {
		t.Fatalf("focused field after j = %q, want %q", got, want)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode after normal navigation = %d, want normal", model.formMode.mode)
	}

	model = updateColumn(model, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if got, want := model.columnForm.form.GetFocusedField().GetKey(), "name"; got != want {
		t.Fatalf("focused field after k = %q, want %q", got, want)
	}
}

func TestStructureForm_viewportTracksFocusedField(t *testing.T) {
	model := resizeModel(openColumn(t, "name", "TEXT"), 100, 12)

	for range 3 {
		model = resolveColumnCommand(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if got, want := model.columnForm.form.GetFocusedField().GetKey(), "default"; got != want {
		t.Fatalf("focused field after navigation = %q, want %q", got, want)
	}
	view := model.structureView()
	height := max(model.workspaceHeight-5, 1)
	if model.compact {
		height = max(model.height-9, 1)
	}
	if got := len(strings.Split(view, "\n")); got > height {
		t.Fatalf("structure form viewport lines = %d, want at most %d", got, height)
	}
	if model.columnForm.scrollOffset == 0 {
		t.Fatal("structure form did not scroll to the focused field")
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
	requestID := startQuery(t, &model)
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !model.Running() || model.columnForm.confirming() {
		t.Fatal("running-query escape did not take precedence")
	}
	_, _ = model.Update(queryCanceledMsg{requestID: requestID})
}

// TestStructureForm_vimOffEditColumnEntersInsert guards the bug where
// editing an existing column opened the form in normal mode even with vim
// mode disabled, forcing a manual i/Enter before typing. Editing must enter
// insert mode without vim mode, exactly like every other form.
func TestStructureForm_vimOffEditColumnEntersInsert(t *testing.T) {
	model := readyModel(t)
	model.vimMode = false
	model.SelectedTable, model.Tab = "items", tabStructure
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = resolveColumnCommand(updated.(Model), tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.formMode.editing() {
		t.Fatalf("vim-off edit opened mode = %d, want insert", model.formMode.mode)
	}
	// Typing must reach the focused Name field: mode alone could be set by
	// beginHuh while the field stayed blurred.
	model = updateColumn(model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := model.columnForm.values.name; got == "name" {
		t.Fatalf("typed text did not reach the Name field, values.name = %q", got)
	}

	// With vim mode on the same flow must stay in normal mode.
	model = readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	updated, _ = model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = resolveColumnCommand(updated.(Model), tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.formMode.editing() {
		t.Fatalf("vim-on edit opened mode = %d, want normal", model.formMode.mode)
	}
}

// TestStructureForm_tabReachesButtonsFromInsertMode guards the vim-off
// flow: Tab on the last field must focus the Save/Cancel bar while staying
// in insert mode, so the bar is reachable without Escape. Bar keys keep
// working there, and k returns to the field with insert intact — j then
// types instead of navigating.
func TestStructureForm_tabReachesButtonsFromInsertMode(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model.formMode.beginHuh(model.columnForm.focus()) // insert mode, vim off
	for range 4 {
		_ = model.columnForm.form.NextField() // attributes (last field)
	}

	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyTab})
	if !model.formMode.buttonsFocused || model.formMode.mode != formModeInsert {
		t.Fatalf("tab on last field: bar=%t mode=%d, want focused/insert", model.formMode.buttonsFocused, model.formMode.mode)
	}

	// h/l still switch the choice while the bar is focused in insert mode.
	model = updateColumn(model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if model.formMode.buttonChoice != 1 {
		t.Fatalf("button choice = %d, want 1 (Cancel)", model.formMode.buttonChoice)
	}

	// Shift+Tab returns to the last field, keeping insert mode.
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if model.formMode.buttonsFocused || model.formMode.mode != formModeInsert {
		t.Fatalf("shift+tab from bar: bar=%t mode=%d, want unfocused/insert", model.formMode.buttonsFocused, model.formMode.mode)
	}

	// j is content on the field, not field navigation.
	model = updateColumn(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := model.columnForm.values.attributes; got != "j" {
		t.Fatalf("attributes = %q, want %q", got, "j")
	}
}

// TestStructureForm_insertModeBarEnterActivatesChoice guards the replay
// path: Enter on Cancel from insert mode must reach the discard
// confirmation, not be eaten as an insert-mode Escape.
func TestStructureForm_insertModeBarEnterActivatesChoice(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model.formMode.beginHuh(model.columnForm.focus()) // insert mode, vim off
	// Type a real edit into the name field: a direct values assignment would
	// be overwritten by Huh's internal input buffer on the next field step.
	for _, character := range "renamed" {
		model = updateColumn(model, tea.KeyPressMsg{Code: character, Text: string(character)})
	}
	for range 4 {
		_ = model.columnForm.form.NextField() // attributes (last field)
	}
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyTab})
	model = updateColumn(model, tea.KeyPressMsg{Code: 'l', Text: "l"}) // Cancel
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !model.columnForm.confirming() || model.columnForm.confirmationSave {
		t.Fatalf("form = confirming:%t save:%t, want confirming discard", model.columnForm.confirming(), model.columnForm.confirmationSave)
	}
	if model.formMode.mode != formModeConfirm {
		t.Fatalf("mode = %d, want confirm", model.formMode.mode)
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
	return driveCommand(model, command)
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

func TestStructureForm_attributesFieldIsSeededFromColumnInfo(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", Attributes: "GENERATED STORED", PrimaryKey: 1}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if form.values.attributes != "GENERATED STORED" {
		t.Fatalf("form attributes = %q, want GENERATED STORED", form.values.attributes)
	}
}

func TestStructureForm_fieldCountIncludesAttributes(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", PrimaryKey: 1}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if want := len(form.values.parameters) + 5; form.fieldCount() != want {
		t.Fatalf("fieldCount() = %d, want %d", form.fieldCount(), want)
	}
}

func TestStructureForm_attributesFieldIsSelectWhenTypeDeclaresOptions(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "BIGINT"}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "PostgreSQL"}))
	_ = form.form.Init()

	for range form.fieldCount() - 1 {
		_ = form.nextField()
	}
	field := form.form.GetFocusedField()
	if got, want := field.GetKey(), "attributes"; got != want {
		t.Fatalf("last field key = %q, want %q", got, want)
	}
	if _, ok := field.(*huh.Select[string]); !ok {
		t.Fatalf("attributes field = %T, want *huh.Select[string]", field)
	}
}

func TestStructureForm_attributesFieldStaysInputWithoutOptions(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "name", Type: "TEXT"}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	_ = form.form.Init()

	for range form.fieldCount() - 1 {
		_ = form.nextField()
	}
	if _, ok := form.form.GetFocusedField().(*huh.Select[string]); ok {
		t.Fatal("attributes field = *huh.Select[string], want editable input for SQLite TEXT")
	}
}

func TestStructureForm_attributesSelectRebuildsWhenTypeChanges(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "value", Type: "TEXT"}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "PostgreSQL"}))
	form.selectType(2, nil) // BIGINT
	form.rebuildForm()
	_ = form.form.Init()

	for range form.fieldCount() - 1 {
		_ = form.nextField()
	}
	if _, ok := form.form.GetFocusedField().(*huh.Select[string]); !ok {
		t.Fatalf("attributes field after type change = %T, want *huh.Select[string]", form.form.GetFocusedField())
	}
}

func TestStructureForm_attributesSelectPreservesSeededValueOutsideOptions(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "BIGINT", Attributes: "IDENTITY ALWAYS"}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "PostgreSQL"}))
	if got, want := form.values.attributes, "IDENTITY ALWAYS"; got != want {
		t.Fatalf("form attributes = %q, want %q", got, want)
	}
	change, err := form.change()
	if err != nil {
		t.Fatalf("change() error = %v", err)
	}
	if change.Attributes != nil {
		t.Fatalf("change attributes = %v, want nil (unchanged)", change.Attributes)
	}
}

func typeIndexByName(t *testing.T, types []sharedsql.ColumnType, name string) int {
	t.Helper()
	for index, typeDefinition := range types {
		if typeDefinition.Name == name {
			return index
		}
	}
	t.Fatalf("type %q not in catalog", name)
	return -1
}

func TestStructureForm_attributesResetWhenTypeChangeDropsOption(t *testing.T) {
	types := sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "MySQL"})
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INT"}, types)
	form.values.attributes = "AUTO_INCREMENT"

	form.selectType(typeIndexByName(t, types, "TEXT"), nil)
	if got := form.values.attributes; got != "" {
		t.Fatalf("attributes after INT->TEXT = %q, want empty", got)
	}
	form.rebuildForm()
	def, err := form.columnDef()
	if err != nil {
		t.Fatalf("columnDef() error = %v", err)
	}
	if def.Attributes != nil {
		t.Fatalf("columnDef attributes = %v, want nil (no stale AUTO_INCREMENT)", def.Attributes)
	}
}

func TestStructureForm_attributesKeptWhenTypeChangeKeepsOption(t *testing.T) {
	types := sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "MySQL"})
	form := newColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INT"}, types)
	form.values.attributes = "AUTO_INCREMENT"

	form.selectType(typeIndexByName(t, types, "BIGINT"), nil)
	if got, want := form.values.attributes, "AUTO_INCREMENT"; got != want {
		t.Fatalf("attributes after INT->BIGINT = %q, want %q", got, want)
	}
}

func TestStructureForm_attributesKeptWhenTypeChangeIsFreeText(t *testing.T) {
	types := sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "MySQL"})
	form := newColumnForm(sqlite.ColumnInfo{Name: "name", Type: "VARCHAR(50)"}, types)
	form.values.attributes = "COMMENT 'updated'"

	form.selectType(typeIndexByName(t, types, "TEXT"), nil)
	if got, want := form.values.attributes, "COMMENT 'updated'"; got != want {
		t.Fatalf("free-text attributes after VARCHAR->TEXT = %q, want %q", got, want)
	}
}

func TestStructureForm_navigationReachesAttributesField(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "name", Type: "TEXT", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	_ = form.form.Init()

	for range form.fieldCount() - 1 {
		_ = form.nextField()
	}
	if got, want := form.form.GetFocusedField().GetKey(), "attributes"; got != want {
		t.Fatalf("last field key = %q, want %q", got, want)
	}
}

func TestStructureForm_changeIncludesAttributesWhenEdited(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "price", Type: "DECIMAL(10,2)", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.values.attributes = "GENERATED STORED"
	change, err := form.change()
	if err != nil {
		t.Fatalf("change() error = %v", err)
	}
	if change.Attributes == nil {
		t.Fatal("change().Attributes = nil, want non-nil")
	}
	if *change.Attributes != "GENERATED STORED" {
		t.Fatalf("change().Attributes = %q, want GENERATED STORED", *change.Attributes)
	}
}

func TestStructureForm_changeOmitsAttributesWhenUnchanged(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "price", Type: "DECIMAL(10,2)", Attributes: "GENERATED VIRTUAL", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	change, err := form.change()
	if err != nil {
		t.Fatalf("change() error = %v", err)
	}
	if change.Attributes != nil {
		t.Fatalf("change().Attributes = %v, want nil (unchanged)", *change.Attributes)
	}
}

func TestStructureForm_changeIncludesEmptyAttributesWhenCleared(t *testing.T) {
	form := newColumnForm(sqlite.ColumnInfo{Name: "price", Type: "DECIMAL(10,2)", Attributes: "GENERATED STORED", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.values.attributes = ""
	change, err := form.change()
	if err != nil {
		t.Fatalf("change() error = %v", err)
	}
	if change.Attributes == nil {
		t.Fatal("change().Attributes = nil, want non-nil after clearing")
	}
	if *change.Attributes != "" {
		t.Fatalf("change().Attributes = %q, want empty string", *change.Attributes)
	}
}

func TestNewEmptyColumnForm_opensWithDefaults(t *testing.T) {
	form := newEmptyColumnForm(sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	if !form.isNew {
		t.Fatal("newEmptyColumnForm.isNew = false, want true")
	}
	if !form.active() {
		t.Fatal("newEmptyColumnForm.active() = false, want true")
	}
	if form.values.name != "" {
		t.Fatalf("form.values.name = %q, want empty", form.values.name)
	}
	if !form.values.nullable {
		t.Fatal("new empty column defaults to NOT NULL; want nullable default")
	}
}

func TestStructureForm_aKeyOpensEmptyColumnForm(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}, {Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	model.Focus = focusWorkspace

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)

	if !model.columnForm.active() {
		t.Fatal("pressing a in structure view did not open column form")
	}
	if !model.columnForm.isNew {
		t.Fatal("column form opened via a is not marked as new")
	}
	if model.columnForm.values.name != "" {
		t.Fatalf("new column form has pre-filled name = %q, want empty", model.columnForm.values.name)
	}
}

func TestNewColumnForm_columnDefReturnsValidDef(t *testing.T) {
	form := newEmptyColumnForm(sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.values.name = "note"
	form.values.typeName = "TEXT"
	form.typeChanged = true

	def, err := form.columnDef()
	if err != nil {
		t.Fatalf("columnDef() error = %v", err)
	}
	if def.Name != "note" || def.Type != "TEXT" || !def.Nullable {
		t.Fatalf("columnDef() = %#v, want {note TEXT nullable}", def)
	}
}

func TestNewColumnForm_columnDefRejectsBlankName(t *testing.T) {
	form := newEmptyColumnForm(sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.values.typeName = "TEXT"
	form.typeChanged = true

	if _, err := form.columnDef(); err == nil {
		t.Fatal("columnDef() with blank name = nil, want error")
	}
}

func TestAddColumnFlow_fullEndToEnd(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "INSERT INTO items (name) VALUES ('first')"); err != nil {
		t.Fatalf("inserting row: %v", err)
	}
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}, {Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	model.Focus = focusWorkspace

	// Press a to open empty column form
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	_ = model.columnForm.form.Init()
	if !model.columnForm.active() {
		t.Fatal("'a' did not open column form")
	}

	// Populate form values directly (skipping Huh navigation for reliability)
	model.columnForm.values.name = "note"
	model.columnForm.values.typeName = "TEXT"
	model.columnForm.typeChanged = true

	// F5 to trigger save confirmation
	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyF5})
	if !model.columnForm.confirming() {
		t.Fatal("F5 did not open confirmation dialog")
	}

	// Confirm save — resolve the command chain
	model = resolveColumnCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	// Form must be closed after save
	if model.columnForm.active() {
		t.Fatal("column form still active after save")
	}

	// Verify column exists in database
	columns, err := model.Database.TableInfo(model.appContext, "items")
	if err != nil {
		t.Fatalf("reading table info: %v", err)
	}
	found := false
	for _, col := range columns {
		if col.Name == "note" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("added column 'note' not found in table info: %#v", columns)
	}

	// Query log should contain an ADD COLUMN entry
	foundLog := false
	for _, entry := range model.queryLogEntries {
		if strings.Contains(entry.statement, "ADD COLUMN") {
			foundLog = true
			break
		}
	}
	if !foundLog {
		t.Fatal("query log missing ADD COLUMN entry")
	}
}
