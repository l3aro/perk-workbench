package workbench

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type browseFormAction uint8

const (
	browseFormNoAction browseFormAction = iota
	browseFormSave
	browseFormDiscard
)

type browseForm struct {
	form             *huh.Form
	confirmation     *confirmationDialog
	values           *browseFormValues
	columns          []string
	original         []*string
	primary          []int
	table            string
	identifier       func(string) string
	width, height    int
	pendingG, saving bool
	confirmationSave bool
	scrollOffset     int
	keybindings      Keybindings
}

type browseFormValues struct {
	fields []string
	nulls  []bool
}

func (m *Model) openBrowseForm() tea.Cmd {
	row := m.browse.Cursor()
	if row < 0 || row >= len(m.browseResult.Rows) {
		m.Status = "select a row"
		return nil
	}
	form, err := newBrowseForm(m.browseResult.Columns, m.browseResult.Rows[row], m.structureColumns)
	if err != nil {
		m.Status = safeText(err.Error())
		return nil
	}
	m.browseForm = form
	m.formMode.buttonsFocused = false
	m.browseForm.keybindings = m.keybindings
	m.browseForm.table = m.SelectedTable
	if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
		m.browseForm.identifier = m.actionIdentifier
	}
	m.browseForm.setWidth(m.tableViewportWidth)
	return m.openForm(m.browseForm.form.Init(), m.browseForm.focus)
}

func newBrowseForm(columns []string, original []*string, info []sharedsql.ColumnInfo) (browseForm, error) {
	if len(columns) == 0 || len(columns) != len(original) {
		return browseForm{}, fmt.Errorf("selected row is unavailable")
	}
	primaryNames := make(map[string]bool, len(info))
	for _, column := range info {
		if column.PrimaryKey > 0 {
			primaryNames[strings.ToLower(column.Name)] = true
		}
	}
	form := browseForm{
		columns:     append([]string(nil), columns...),
		original:    append([]*string(nil), original...),
		values:      &browseFormValues{fields: make([]string, len(original)), nulls: make([]bool, len(original))},
		keybindings: DefaultKeybindings(),
	}
	for index, value := range original {
		if value == nil {
			form.values.nulls[index] = true
		} else {
			form.values.fields[index] = *value
		}
		if primaryNames[strings.ToLower(columns[index])] {
			form.primary = append(form.primary, index)
		}
	}
	if len(form.primary) == 0 {
		return browseForm{}, fmt.Errorf("cannot edit rows without a primary key")
	}
	form.rebuildForm()
	return form, nil
}

func (f browseForm) active() bool { return len(f.columns) > 0 }

func (f browseForm) confirming() bool { return f.confirmation != nil }

func (f *browseForm) Update(message tea.Msg, controller *formModeController) (tea.Cmd, browseFormAction) {
	if f.saving {
		return nil, browseFormNoAction
	}
	if f.confirmation != nil {
		completed, action := f.confirmation.Update(message, f.width, f.height)
		if !completed {
			return nil, browseFormNoAction
		}
		save := f.confirmationSave
		f.confirmation = nil
		controller.mode = formModeNormal
		if action != "confirm" {
			return nil, browseFormNoAction
		}
		if save {
			return nil, browseFormSave
		}
		return nil, browseFormDiscard
	}
	if route := controller.routeHuh(message, f.blur); route != formRouteParent {
		if route == formRouteHuh {
			return f.updateHuh(message, controller)
		}
		return nil, browseFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil, browseFormNoAction
	}
	if route, replay, cmd := controller.routeFormButtons(keyPress, f.keybindings, f.lastField); route != formButtonContinue {
		if route == formButtonReplay {
			keyPress = replay
		} else {
			return cmd, browseFormNoAction
		}
	}
	switch {
	case isInsertModeKey(keyPress), f.keybindings.Match(keyPress, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}):
		col := f.focusedColumn()
		if col >= 0 {
			f.values.nulls[col] = false
		}
		return controller.beginHuh(f.focus()), browseFormNoAction
	case f.keybindings.Match(keyPress, "browse_form.set_null", []scope{scopeView, scopeGlobal}):
		col := f.focusedColumn()
		if col >= 0 {
			f.values.nulls[col] = true
			f.values.fields[col] = ""
		}
		f.pendingG = false
		f.rebuildForm()
		return f.form.Init(), browseFormNoAction
	case f.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}):
		f.beginConfirmation(true)
		controller.beginConfirm()
		return nil, browseFormNoAction
	case f.keybindings.Match(keyPress, "form.discard", []scope{scopeForm, scopeView, scopeGlobal}):
		f.beginConfirmation(false)
		controller.beginConfirm()
		return nil, browseFormNoAction
	case f.keybindings.Match(keyPress, "form.field_next", []scope{scopeForm, scopeView, scopeGlobal}):
		f.pendingG = false
		if f.focusedColumn() >= len(f.columns)-1 {
			controller.focusButtons()
			f.blur()
			return nil, browseFormNoAction
		}
		return f.nextField(), browseFormNoAction
	case f.keybindings.Match(keyPress, "form.field_prev", []scope{scopeForm, scopeView, scopeGlobal}):
		f.pendingG = false
		return f.previousField(), browseFormNoAction
	case f.keybindings.Match(keyPress, "browse_form.field_top", []scope{scopeView, scopeGlobal}):
		if f.pendingG {
			f.pendingG = false
			return f.firstField(), browseFormNoAction
		}
		f.pendingG = true
		return nil, browseFormNoAction
	case f.keybindings.Match(keyPress, "browse_form.field_bottom", []scope{scopeView, scopeGlobal}):
		f.pendingG = false
		return f.lastField(), browseFormNoAction
	default:
		f.pendingG = false
	}
	return nil, browseFormNoAction
}

func (f *browseForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, browseFormAction) {
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if f.form.State == huh.StateCompleted {
		f.rebuildForm()
		return f.focus(), browseFormNoAction
	}
	f.scrollToColumn(f.focusedColumn())
	return command, browseFormNoAction
}

func (f *browseForm) beginConfirmation(save bool) {
	f.confirmationSave = save
	title := "Discard row changes?"
	if save {
		title = "Save row changes?"
		if statement, err := f.updateStatement(f.table); err == nil && statement != "" {
			f.confirmation = yesNoConfirmation(title, statement, "confirm")
			return
		}
	}
	f.confirmation = yesNoConfirmation(title, "", "confirm")
}

func (f *browseForm) rebuildForm() {
	fields := make([]huh.Field, 0, len(f.columns))
	for index, column := range f.columns {
		fields = append(fields,
			newEditableInput(huh.NewInput().Key(f.valueKey(index)).Title(column).Value(&f.values.fields[index]), &f.values.fields[index]),
		)
	}
	f.form = newForm(huh.NewGroup(fields...)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func (f browseForm) valueKey(index int) string { return fmt.Sprintf("value-%d", index) }

func (f browseForm) focusedColumn() int {
	if f.form == nil {
		return 0
	}
	key := f.form.GetFocusedField().GetKey()
	for index := range f.columns {
		if key == f.valueKey(index) {
			return index
		}
	}
	return 0
}

func (f *browseForm) nextField() tea.Cmd {
	if f.focusedColumn() == len(f.columns)-1 {
		return nil
	}
	f.scrollToColumn(f.focusedColumn() + 1)
	return f.form.NextField()
}

func (f *browseForm) previousField() tea.Cmd {
	if f.focusedColumn() == 0 {
		return nil
	}
	f.scrollToColumn(f.focusedColumn() - 1)
	return f.form.PrevField()
}

// focusColumn moves the field cursor to the column at index and scrolls it
// into view. The loop bounds guard against Huh navigation skipping fields.
func (f *browseForm) focusColumn(col int) tea.Cmd {
	col = min(max(col, 0), len(f.columns)-1)
	for range len(f.columns) {
		if f.focusedColumn() >= col {
			break
		}
		_ = f.form.NextField()
	}
	for range len(f.columns) {
		if f.focusedColumn() <= col {
			break
		}
		_ = f.form.PrevField()
	}
	f.scrollToColumn(col)
	return f.focus()
}

func (f *browseForm) firstField() tea.Cmd {
	for f.focusedColumn() > 0 {
		_ = f.form.PrevField()
	}
	f.scrollOffset = 0
	return f.focus()
}

func (f *browseForm) lastField() tea.Cmd {
	for f.focusedColumn() < len(f.columns)-1 {
		_ = f.form.NextField()
	}
	f.scrollToColumn(len(f.columns) - 1)
	return f.focus()
}

func (f *browseForm) scrollToColumn(col int) {
	f.scrollOffset = col * 2
}

func (f *browseForm) blur() {
	if f.form != nil {
		_ = f.form.GetFocusedField().Blur()
	}
}

func (f *browseForm) focus() tea.Cmd {
	if f.form == nil {
		return nil
	}
	return f.form.GetFocusedField().Focus()
}

func (f *browseForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f browseForm) View() string {
	if f.saving {
		return statusStyle.Render("saving row changes")
	}
	if f.form == nil {
		return ""
	}
	return f.form.View()
}
