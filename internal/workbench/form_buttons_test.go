package workbench

import (
	"context"
	"strconv"
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
	// The focused button must use the focus color, rendered identically to
	// the action buttons' focus cue — not the teal selection color.
	if got := formButtonFocusedStyle.Render("Save"); got != connectionActionFocusedStyle.Render("Save") {
		t.Fatalf("focused form button = %q, want the shared focus color", got)
	}
	// Only the chosen button is lit: with the bar focused, the unchosen
	// button must drop to the dim base style instead of keeping the teal
	// primary highlight.
	if view := formButtonsBar(true, 0); strings.Contains(view, formSaveButtonStyle.Render("Save")) || !strings.Contains(view, formCancelButtonStyle.Render("Cancel")) {
		t.Fatalf("focused bar choice 0 = %q, want Save lit and Cancel dimmed", view)
	}
	if view := formButtonsBar(true, 1); strings.Contains(view, formSaveButtonStyle.Render("Save")) || !strings.Contains(view, formCancelButtonStyle.Render("Save")) {
		t.Fatalf("focused bar choice 1 = %q, want Cancel lit and Save dimmed", view)
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
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
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
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
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

func TestColumnForm_mouseCancelStartsDiscardConfirmation(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model.columnForm.values.name = "renamed"
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
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 6, Button: tea.MouseLeft})
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
	model.foreignKeyForm.values.columns = "parent_id"
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

// TestConnectionForm_viewportKeepsActionsReachable guards the connection
// form at heights where the field list overflows the pane body: field
// navigation must scroll the focused field into view so the action buttons
// stay reachable instead of being clipped away.
func TestConnectionForm_viewportKeepsActionsReachable(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver = driverMySQL
	model.connection.values.host, model.connection.values.port = "localhost", "5432"
	model.connection.values.user = "postgres"
	_ = model.connection.rebuildForm()
	_ = model.connection.form.Init()
	model = resizeModel(model, 100, 24)

	// A MySQL form is taller than the pane body: the action buttons start
	// below the fold and are clipped at the top of the form.
	if view := ansi.Strip(model.connectionPaneView(model.height - 6)); strings.Contains(view, connectionActionConnect) {
		t.Fatalf("tall connection form at the top of the pane shows the action buttons")
	}

	// driver name host port username password database tls readOnly action
	for range 9 {
		model = updateConnectionForm(model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	if got := model.connection.form.GetFocusedField().GetKey(); got != "action" {
		t.Fatalf("focused field = %q, want action", got)
	}
	view := ansi.Strip(model.connectionPaneView(model.height - 6))
	if !strings.Contains(view, connectionActionTest) || !strings.Contains(view, connectionActionConnect) {
		t.Fatalf("scrolled connection pane view = %q, want action buttons visible", view)
	}
	if strings.Contains(view, "Save") || strings.Contains(view, "Cancel") {
		t.Fatalf("scrolled connection pane view = %q, want no Save/Cancel footer", view)
	}
}

func updateConnectionForm(model Model, message tea.Msg) Model {
	updated, _ := model.Update(message)
	return updated.(Model)
}

func TestConnectionForm_paneFooterHasNoButtons(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model = resizeModel(model, 100, 24)

	view := ansi.Strip(model.connectionPaneView(model.height - 6))
	if strings.Contains(view, "Save") || strings.Contains(view, "Cancel") {
		t.Fatalf("connection pane view = %q, want no Save/Cancel buttons", view)
	}
}

// TestConnectionScreen_titledPanesKeepModeBadgeVisible guards the pane
// geometry at the smallest non-compact height: titledPane swaps the top
// border for the title row, so the form view's padded height must still
// leave the mode-badge footer inside the pane body — with no Save/Cancel
// button row on the connection screen.
func TestConnectionScreen_titledPanesKeepModeBadgeVisible(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.focus = connectionFocusForm
	model = resizeModel(model, 100, 24)

	view := ansi.Strip(model.contentView())
	if !strings.Contains(view, "Profiles <1>") || !strings.Contains(view, "Connection <2>") {
		t.Fatalf("connection screen = %q, want titled pane overlays", view)
	}
	if strings.Contains(view, "Save") || strings.Contains(view, "Cancel") {
		t.Fatalf("connection screen = %q, want no Save/Cancel footer", view)
	}

	footer := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "NORMAL") {
			footer = line
		}
	}
	if footer == "" {
		t.Fatalf("connection screen = %q, want mode-badge footer", view)
	}
}

func TestIndexForm_mouseWheelScrollsViewport(t *testing.T) {
	model := readyIndexesModel(t)
	model = openIndexEditor(t, model, nil)
	model = resizeModel(model, 100, 24)
	start := model.indexForm.scrollOffset

	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.indexForm.scrollOffset != start+1 {
		t.Fatalf("wheel down scroll offset = %d, want %d", model.indexForm.scrollOffset, start+1)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.indexForm.scrollOffset != start {
		t.Fatalf("wheel up scroll offset = %d, want %d", model.indexForm.scrollOffset, start)
	}

	// Wheeling past the bottom clamps at the last full window.
	model.indexForm.scrollOffset = 1 << 20
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	lines := len(strings.Split(model.indexForm.View(), "\n"))
	if want := lines - model.formViewportHeight(); model.indexForm.scrollOffset != want {
		t.Fatalf("wheel at bottom scroll offset = %d, want %d", model.indexForm.scrollOffset, want)
	}
}

func TestBrowseFilterForm_mouseWheelScrollsRowsNotHeader(t *testing.T) {
	columns := make([]sharedsql.ColumnInfo, 12)
	for index := range columns {
		columns[index] = sharedsql.ColumnInfo{Name: "col" + strconv.Itoa(index), Type: "TEXT"}
	}
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabBrowse
	model.structureColumns = columns
	model = resizeModel(model, 100, 26)
	_ = model.openBrowseFilterForm()

	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.browseFilterForm.scrollOffset != 1 {
		t.Fatalf("wheel down scroll offset = %d, want 1", model.browseFilterForm.scrollOffset)
	}
	// The wheel must advance the field rows, not double-scroll the already
	// windowed view: the header stays pinned and the first field row shows
	// the row at the new offset.
	lines := strings.Split(ansi.Strip(model.workspaceView()), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[2]), "Column") {
		t.Fatalf("filter view line 2 = %q, want pinned header", lines[2])
	}
	if !strings.Contains(lines[3], "col1") {
		t.Fatalf("filter view line 3 = %q, want first row col1", lines[3])
	}
}

func TestColumnForm_mouseWheelMovesFieldFocus(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.columnForm.focusedField() != 1 {
		t.Fatalf("wheel down focused field = %d, want 1", model.columnForm.focusedField())
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.columnForm.focusedField() != 0 {
		t.Fatalf("wheel up focused field = %d, want 0", model.columnForm.focusedField())
	}

	// Wheeling down past the last field stays put instead of moving onto
	// the button bar.
	for range model.columnForm.fieldCount() {
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		model = updated.(Model)
	}
	if model.formMode.buttonsFocused {
		t.Fatal("wheel down past the last field focused the button bar")
	}
	if got := model.columnForm.focusedField(); got != model.columnForm.fieldCount()-1 {
		t.Fatalf("wheel down past the last field = %d, want %d", got, model.columnForm.fieldCount()-1)
	}
}

func TestConnectionForm_mouseWheelMovesFieldFocus(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 30)
	_ = model.newConnection()
	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if got := model.connection.form.GetFocusedField().GetKey(); got != "name" {
		t.Fatalf("wheel down focused field = %q, want name", got)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if got := model.connection.form.GetFocusedField().GetKey(); got != "driver" {
		t.Fatalf("wheel up focused field = %q, want driver", got)
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
