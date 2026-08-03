package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	for range 4 {
		if command == nil {
			return model
		}
		message = command()
		if batch, ok := message.(tea.BatchMsg); ok {
			for _, next := range batch {
				model = resolveColumnCommand(model, next())
			}
			return model
		}
		updated, command = model.Update(message)
		model = updated.(Model)
	}
	return model
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
