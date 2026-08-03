package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// clickFormButton delivers a mouse press on the form button bar and resolves
// the synthesized key command it returns.
func clickFormButton(model Model, x, y int) Model {
	updated, command := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil {
		return model
	}
	message := command()
	if message == nil {
		return model
	}
	updated, _ = model.Update(message)
	return updated.(Model)
}

// workspaceButtonRowY returns the screen y of the workspace button bar.
func workspaceButtonRowY(model Model) int {
	return model.workspaceHeight - 3 // contentY = workspaceHeight-4, plus header
}

// connectionButtonRowY returns the screen y of the connection button bar.
func connectionButtonRowY(model Model) int {
	return model.height - 5 // contentY = height-6, plus header
}

func TestFormButtonAt_hitTest(t *testing.T) {
	saveWidth := ansi.StringWidth(formSaveButtonStyle.Render("Save"))
	cancelWidth := ansi.StringWidth(formCancelButtonStyle.Render("Cancel"))
	gap := saveWidth + 1

	for relX, want := range map[int]string{
		-1:                    "",
		0:                     "save",
		saveWidth - 1:         "save",
		saveWidth:             "", // separator space
		gap:                   "cancel",
		gap + cancelWidth - 1: "cancel",
		gap + cancelWidth:     "",
	} {
		if got := formButtonAt(relX); got != want {
			t.Fatalf("formButtonAt(%d) = %q, want %q", relX, got, want)
		}
	}
}

func TestFormButtonsBar_focusedHighlightsChoice(t *testing.T) {
	if view := formButtonsBar(true, 1); !strings.Contains(view, formButtonFocusedStyle.Render("Cancel")) || strings.Contains(view, formButtonFocusedStyle.Render("Save")) {
		t.Fatalf("focused bar = %q, want Cancel highlighted only", view)
	}
	if view := formButtonsBar(false, 0); strings.Contains(view, formButtonFocusedStyle.Render("Save")) || strings.Contains(view, formButtonFocusedStyle.Render("Cancel")) {
		t.Fatalf("unfocused bar = %q, want no highlight", view)
	}
}

func TestFormButtonsBar_rendersOnlyWhileFormActive(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabStructure
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	model = updateColumn(model, tableInfoMsg{table: "items", columns: []sharedsql.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = resizeModel(model, 100, 24)

	// No form on the tab? No buttons.
	if view := ansi.Strip(model.workspaceView()); strings.Contains(view, "Save") || strings.Contains(view, "Cancel") {
		t.Fatalf("workspace view without form shows buttons: %q", view)
	}

	model = updateColumn(model, tea.KeyPressMsg{Code: tea.KeyEnter}) // open the column form
	_ = model.columnForm.form.Init()
	view := ansi.Strip(model.workspaceView())
	lines := strings.Split(view, "\n")
	if actions, gap, footer := strings.TrimSpace(lines[len(lines)-3]), strings.TrimSpace(lines[len(lines)-2]), strings.TrimSpace(lines[len(lines)-1]); !strings.Contains(actions, "Save") || !strings.Contains(actions, "Cancel") || gap != "" || !strings.HasPrefix(footer, "NORMAL") {
		t.Fatalf("workspace form footer = %q / %q / %q, want Save/Cancel, gap, bottom-left NORMAL", actions, gap, footer)
	}
}

func TestColumnForm_mouseSaveStartsSaveConfirmation(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = resizeModel(model, 100, 24)
	x := model.schemaWidth + 1 + 2 // inside " Save "

	model = clickFormButton(model, x, workspaceButtonRowY(model))

	if !model.columnForm.confirming() || !model.columnForm.confirmationSave {
		t.Fatalf("column form = confirming:%t save:%t, want confirming save", model.columnForm.confirming(), model.columnForm.confirmationSave)
	}
	if model.formMode.mode != formModeConfirm {
		t.Fatalf("mode = %d, want confirm", model.formMode.mode)
	}
}

func TestColumnForm_mouseSaveAfterMouseEditSaves(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = resizeModel(model, 100, 24)

	// Mouse-enter insert mode on the Name value line (view line 1, y=5).
	updated, _ := model.Update(tea.MouseClickMsg{X: 40, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 40, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatal("double click did not enter insert mode")
	}
	for _, ch := range "renamed" {
		model = updateColumn(model, tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}

	model = clickFormButton(model, model.schemaWidth+1+2, workspaceButtonRowY(model))

	if !model.columnForm.confirming() || !model.columnForm.confirmationSave {
		t.Fatalf("Save after mouse edit = confirming:%t save:%t, want confirming save", model.columnForm.confirming(), model.columnForm.confirmationSave)
	}
	if got := model.columnForm.values.name; !strings.Contains(got, "renamed") {
		t.Fatalf("name = %q, want typed text kept", got)
	}
}

func TestIndexForm_mouseSaveAfterMouseEditSaves(t *testing.T) {
	model := readyIndexesModel(t)
	model = openIndexEditor(t, model, nil)
	model.indexForm.values.columns = "id"
	model = resizeModel(model, 100, 24)

	// Mouse-enter insert mode on the Name value line (view line 1, y=5).
	updated, _ := model.Update(tea.MouseClickMsg{X: 40, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 40, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatal("double click did not enter insert mode")
	}
	for _, ch := range "idx_name" {
		model = updateIndexForm(model, tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}

	model = clickFormButton(model, model.schemaWidth+1+2, workspaceButtonRowY(model))

	if !model.indexForm.confirming() || !model.indexForm.confirmationSave {
		t.Fatalf("Save after mouse edit = confirming:%t save:%t, want confirming save", model.indexForm.confirming(), model.indexForm.confirmationSave)
	}
	if got := model.indexForm.values.name; !strings.Contains(got, "idx_name") {
		t.Fatalf("name = %q, want typed text kept", got)
	}
}

func TestBrowseFilterForm_mouseSaveCommitsEditAndApplies(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = resizeModel(model, 100, 26)

	// Mouse-enter edit on the Rows limit row (view line 3, screen y=7).
	updated, _ := model.Update(tea.MouseClickMsg{X: 60, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 60, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.browseFilterForm.editing {
		t.Fatal("double click did not start editing the rows limit")
	}
	model.browseFilterForm.input.SetValue("3")

	model = clickFormButton(model, model.schemaWidth+1+2, workspaceButtonRowY(model))

	if model.browseFilterForm != nil {
		t.Fatal("Save did not apply the filter form")
	}
	if got := model.browseSettings.limit; got != 3 {
		t.Fatalf("limit = %d, want 3", got)
	}
}

func TestConnectionForm_mouseSaveAfterMouseEditConnects(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver = driverSQLite
	model.connection.values.target = ":memory:"
	_ = model.connection.rebuildForm()
	_ = model.connection.form.Init()
	model = resizeModel(model, 100, 24)

	// Mouse-enter insert mode on the Name field (view line 5, screen y=7).
	updated, _ := model.Update(tea.MouseClickMsg{X: 40, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 40, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatal("double click did not enter insert mode")
	}

	model = clickFormButton(model, model.schemaWidth+1+2, connectionButtonRowY(model))

	if model.connection.confirmation == nil {
		t.Fatal("Save after mouse edit did not start the connect confirmation")
	}
}

func TestColumnForm_mouseCancelStartsDiscardConfirmation(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = resizeModel(model, 100, 24)
	x := model.schemaWidth + 1 + 8 // inside " Cancel "

	model = clickFormButton(model, x, workspaceButtonRowY(model))

	if !model.columnForm.confirming() || model.columnForm.confirmationSave {
		t.Fatalf("column form = confirming:%t save:%t, want confirming discard", model.columnForm.confirming(), model.columnForm.confirmationSave)
	}
}

func TestColumnForm_mouseSaveReleaseConsumedByDialog(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = resizeModel(model, 100, 24)
	x := model.schemaWidth + 1 + 2
	y := workspaceButtonRowY(model)
	model = clickFormButton(model, x, y)

	updated, _ := model.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)

	if !model.columnForm.confirming() {
		t.Fatal("trailing release closed the save confirmation")
	}
	if model.formButtonHit {
		t.Fatal("dialog-consumed release left the swallow flag set")
	}
}

func TestIndexForm_mouseSaveShowsValidationError(t *testing.T) {
	model := readyIndexesModel(t)
	model = openIndexEditor(t, model, nil)
	model = resizeModel(model, 100, 24)
	x := model.schemaWidth + 1 + 2

	model = clickFormButton(model, x, workspaceButtonRowY(model))

	if model.indexForm.confirming() {
		t.Fatal("blank index name reached the save confirmation")
	}
	field := model.indexForm.form.GetFocusedField()
	if field.GetKey() != "name" || field.Error() == nil || !strings.Contains(model.indexForm.View(), "index name is required") {
		t.Fatalf("index form = %q, want active name field error", model.indexForm.View())
	}
}

func TestBrowseForm_mouseSaveStartsSaveConfirmation(t *testing.T) {
	model := openBrowseRow(t, 0)
	model = resizeModel(model, 100, 24)
	x := model.schemaWidth + 1 + 2

	model = clickFormButton(model, x, workspaceButtonRowY(model))

	if !model.browseForm.confirming() || !model.browseForm.confirmationSave {
		t.Fatalf("browse form = confirming:%t save:%t, want confirming save", model.browseForm.confirming(), model.browseForm.confirmationSave)
	}
}

func TestBrowseFilterForm_mouseButtonsApplyAndDiscard(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = resizeModel(model, 100, 24)
	if model.browseFilterForm == nil {
		t.Fatal("filter form did not open")
	}
	y := workspaceButtonRowY(model)

	// Save = apply filters, closing the form.
	model = clickFormButton(model, model.schemaWidth+1+2, y)
	if model.browseFilterForm != nil {
		t.Fatal("Save click did not apply the filter form")
	}

	// Cancel = discard, closing the form.
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = resizeModel(model, 100, 24)
	model = clickFormButton(model, model.schemaWidth+1+8, y)
	if model.browseFilterForm != nil {
		t.Fatal("Cancel click did not discard the filter form")
	}
}

func TestFormButtonPress_swallowsTrailingRelease(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = resizeModel(model, 100, 24)
	x := model.schemaWidth + 1 + 2
	y := workspaceButtonRowY(model)

	model = clickFormButton(model, x, y)
	cursor := model.browse.Cursor()

	// The release trailing the Save press must not click the pane underneath.
	updated, _ := model.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formButtonHit {
		t.Fatal("release was not swallowed")
	}
	if got := model.browse.Cursor(); got != cursor {
		t.Fatalf("swallowed release moved browse cursor %d -> %d", cursor, got)
	}

	// The swallow is one-shot: a later real click presses normally.
	updated, _ = model.Update(tea.MouseClickMsg{X: 40, Y: 6, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.browse.Cursor(); got == cursor {
		t.Fatal("press after the swallow did not click the browse table")
	}
}

func TestForeignKeyForm_mouseSaveAndCancelConfirmations(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "children", tabForeignKeys
	model = resizeModel(model, 100, 24)
	_ = model.openForeignKeyForm(nil)
	_ = model.foreignKeyForm.form.Init()
	model.foreignKeyForm.values.columns = "parent_id"
	model.foreignKeyForm.values.referenceTable = "parents"
	model.foreignKeyForm.values.referenceColumns = "id"
	y := workspaceButtonRowY(model)

	model = clickFormButton(model, model.schemaWidth+1+2, y)
	if !model.foreignKeyForm.confirming() || !model.foreignKeyForm.confirmationSave {
		t.Fatalf("foreign-key form = confirming:%t save:%t, want confirming save", model.foreignKeyForm.confirming(), model.foreignKeyForm.confirmationSave)
	}

	// Cancel starts the discard confirmation without field validation.
	model = readyModel(t)
	model.SelectedTable, model.Tab = "children", tabForeignKeys
	model = resizeModel(model, 100, 24)
	_ = model.openForeignKeyForm(nil)
	_ = model.foreignKeyForm.form.Init()
	model = clickFormButton(model, model.schemaWidth+1+8, y)
	if !model.foreignKeyForm.confirming() || model.foreignKeyForm.confirmationSave {
		t.Fatalf("foreign-key form = confirming:%t save:%t, want confirming discard", model.foreignKeyForm.confirming(), model.foreignKeyForm.confirmationSave)
	}
}

// workspacePaneFormButtonsRow returns the row two lines above the mode footer.
func workspacePaneFormButtonsRow(model Model) string {
	lines := strings.Split(ansi.Strip(model.workspaceView()), "\n")
	if len(lines) < 3 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-3])
}

func TestIndexForm_viewportKeepsButtonsVisible(t *testing.T) {
	model := readyIndexesModel(t)
	model = openIndexEditor(t, model, nil)
	model = resizeModel(model, 100, 24)

	if row := workspacePaneFormButtonsRow(model); !strings.Contains(row, "Save") || !strings.Contains(row, "Cancel") {
		t.Fatalf("index form action row = %q, want Save/Cancel visible", row)
	}

	// Navigating to the Kind select must scroll the viewport, not push the
	// button bar off the pane.
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateIndexForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if model.indexForm.scrollOffset != 6 {
		t.Fatalf("index form scroll offset = %d, want 6 (Kind title)", model.indexForm.scrollOffset)
	}
	if row := workspacePaneFormButtonsRow(model); !strings.Contains(row, "Save") || !strings.Contains(row, "Cancel") {
		t.Fatalf("scrolled index form action row = %q, want Save/Cancel visible", row)
	}
}

func TestForeignKeyForm_viewportKeepsButtonsVisible(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "children", tabForeignKeys
	_ = model.openForeignKeyForm(nil)
	_ = model.foreignKeyForm.form.Init()
	model = resizeModel(model, 100, 24)

	if row := workspacePaneFormButtonsRow(model); !strings.Contains(row, "Save") || !strings.Contains(row, "Cancel") {
		t.Fatalf("FK form action row = %q, want Save/Cancel visible", row)
	}

	// Navigating to the On update select must scroll the viewport.
	for range 4 {
		model = updateForeignKeyForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if model.foreignKeyForm.scrollOffset != 16 {
		t.Fatalf("FK form scroll offset = %d, want 16 (On update title)", model.foreignKeyForm.scrollOffset)
	}
	if row := workspacePaneFormButtonsRow(model); !strings.Contains(row, "Save") || !strings.Contains(row, "Cancel") {
		t.Fatalf("scrolled FK form action row = %q, want Save/Cancel visible", row)
	}
}

func TestConnectionForm_mouseSaveStartsConnectConfirmation(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver = driverSQLite
	model.connection.values.target = ":memory:"
	_ = model.connection.rebuildForm()
	_ = model.connection.form.Init()
	model = resizeModel(model, 100, 24)

	model = clickFormButton(model, model.schemaWidth+1+2, connectionButtonRowY(model))

	if model.connection.confirmation == nil {
		t.Fatal("Save click did not start the connect confirmation")
	}
}

func TestConnectionForm_mouseCancelSwitchesToProfiles(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver = driverSQLite
	model.connection.values.target = ":memory:"
	_ = model.connection.rebuildForm()
	_ = model.connection.form.Init()
	model = resizeModel(model, 100, 24)

	model = clickFormButton(model, model.schemaWidth+1+8, connectionButtonRowY(model))

	if model.connection.focus != connectionFocusRecent {
		t.Fatalf("Cancel click focus = %d, want profiles list", model.connection.focus)
	}
}

func TestConnectionForm_mouseCancelAfterMouseEditSwitchesToProfiles(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver = driverSQLite
	model.connection.values.target = ":memory:"
	_ = model.connection.rebuildForm()
	_ = model.connection.form.Init()
	model = resizeModel(model, 100, 24)

	// Mouse-enter insert mode on the Name field (view line 5, screen y=7).
	updated, _ := model.Update(tea.MouseClickMsg{X: 40, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 40, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatal("double click did not enter insert mode")
	}
	if got := model.connection.values.name; got != "Local database" && got != "" {
		t.Fatalf("fixture name = %q, want untouched", got)
	}

	model = clickFormButton(model, model.schemaWidth+1+8, connectionButtonRowY(model))

	if model.connection.focus != connectionFocusRecent {
		t.Fatalf("Cancel after mouse edit focus = %d, want profiles list", model.connection.focus)
	}
	if got := model.connection.values.name; strings.Contains(got, "1") {
		t.Fatalf("Cancel typed into the field: name = %q", got)
	}
}

func TestConnectionForm_buttonsRenderInPaneFooter(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model = resizeModel(model, 100, 24)

	view := ansi.Strip(model.connectionPaneView(model.height - 6))
	if !strings.Contains(view, "Save") || !strings.Contains(view, "Cancel") {
		t.Fatalf("connection pane view = %q, want Save/Cancel buttons", view)
	}
}

func TestCellEditor_mouseSaveStartsConfirmation(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = resizeModel(model, 100, 24)
	e := model.cellEditor
	if e == nil {
		t.Fatal("cell editor did not open")
	}
	contentLines := len(strings.Split(e.input.View(), "\n")) + 1
	dialogW := min(e.width, 94)
	dialogH := min(contentLines, 18)
	boxX := (100 - dialogW - 2) / 2
	boxY := (24 - dialogH - 2) / 2

	updated, _ := model.Update(tea.MouseClickMsg{X: boxX + 1 + 2, Y: boxY + dialogH, Button: tea.MouseLeft})
	model = updated.(Model)

	if !model.cellEditor.confirming {
		t.Fatal("Save click did not start the cell save confirmation")
	}
	if !model.formButtonHit {
		t.Fatal("Save click did not arm the release swallow")
	}
}

func TestCellEditor_mouseCancelClosesEditor(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseColumn = 1
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = resizeModel(model, 100, 24)
	e := model.cellEditor
	contentLines := len(strings.Split(e.input.View(), "\n")) + 1
	dialogW := min(e.width, 94)
	dialogH := min(contentLines, 18)
	boxX := (100 - dialogW - 2) / 2
	boxY := (24 - dialogH - 2) / 2

	updated, _ := model.Update(tea.MouseClickMsg{X: boxX + 1 + 8, Y: boxY + dialogH, Button: tea.MouseLeft})
	model = updated.(Model)

	if model.cellEditor != nil {
		t.Fatal("Cancel click did not close the cell editor")
	}
	// The trailing release must not click the browse table underneath.
	cursor := model.browse.Cursor()
	updated, _ = model.Update(tea.MouseReleaseMsg{X: boxX + 1 + 8, Y: boxY + dialogH, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.browse.Cursor(); got != cursor {
		t.Fatalf("release after Cancel moved browse cursor %d -> %d", cursor, got)
	}
}
