package workbench

import (
	"fmt"
	"strconv"
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
	inserting        bool
	width, height    int
	pendingG, saving bool
	confirmationSave bool
	scrollOffset     int
	keybindings      Keybindings
}

type browseFormValues struct {
	fields []string
	nulls  []bool
	// defaults marks insert-form fields left on DEFAULT: the column is
	// omitted from the INSERT so engine defaults or auto-increment apply.
	// Only meaningful while inserting; nil otherwise.
	defaults []bool
}

func (m *Model) openBrowseForm() tea.Cmd {
	if !m.writeCapabilities().RowWriter {
		// Document stores edit the whole document instead of a row.
		return m.openEditDocument()
	}
	row := m.browse.Cursor()
	if row < 0 || row >= len(m.browseResult.Rows) {
		m.setStatus("select a row")
		return nil
	}
	form, err := newBrowseForm(m.browseResult.Columns, m.browseResult.Rows[row], m.structureColumns)
	if err != nil {
		m.setStatus(safeText(err.Error()))
		return nil
	}
	m.browseForm = form
	m.formMode.buttonsFocused = false
	m.browseForm.keybindings = m.keybindings
	m.browseForm.table = m.SelectedTable
	m.browseForm.setWidth(m.tableViewportWidth)
	return m.openForm(m.browseForm.form.Init(), m.browseForm.focus)
}

// openInsertRowForm opens the insert-row form for the selected table. The
// form is built from the structure columns because an empty table has no
// browse rows to derive columns from. Document stores with an editable
// document capability get the document insert editor instead.
func (m *Model) openInsertRowForm() tea.Cmd {
	if !m.writeCapabilities().RowWriter {
		if m.documentCapability() != nil {
			return m.openInsertDocument()
		}
		m.setStatus(safeText(m.rowWriteUnsupportedError().Error()))
		return nil
	}
	columns := make([]string, 0, len(m.structureColumns))
	for _, info := range m.structureColumns {
		columns = append(columns, info.Name)
	}
	form, err := newInsertBrowseForm(columns)
	if err != nil {
		m.setStatus(safeText(err.Error()))
		return nil
	}
	m.browseForm = form
	m.formMode.buttonsFocused = false
	m.browseForm.keybindings = m.keybindings
	m.browseForm.table = m.SelectedTable
	m.browseForm.setWidth(m.tableViewportWidth)
	return m.openForm(m.browseForm.form.Init(), m.browseForm.focus)
}

// newInsertBrowseForm builds an insert-row form over the given columns.
// Every field starts in the DEFAULT state (column omitted from the INSERT,
// so engine defaults or auto-increment apply). Typing moves a field to
// VALUE (the empty string included); the browse_form.set_null and
// browse_form.set_default bindings move it to NULL or back to DEFAULT.
// Unlike the edit form, no primary key is required.
func newInsertBrowseForm(columns []string) (browseForm, error) {
	if len(columns) == 0 {
		return browseForm{}, fmt.Errorf("table has no columns")
	}
	values := &browseFormValues{
		fields:   make([]string, len(columns)),
		nulls:    make([]bool, len(columns)),
		defaults: make([]bool, len(columns)),
	}
	for index := range values.defaults {
		values.defaults[index] = true
	}
	form := browseForm{
		inserting:   true,
		columns:     append([]string(nil), columns...),
		values:      values,
		keybindings: DefaultKeybindings(),
	}
	form.rebuildForm()
	return form, nil
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
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.buttonsFocused {
		if route, replayed, cmd := controller.routeFormButtons(keyPress, f.keybindings, f.lastField); route != formButtonContinue {
			if route == formButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, browseFormNoAction
			}
		}
	}
	if !replay {
		if route := controller.routeHuh(message, f.blur); route != formRouteParent {
			if route == formRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, browseFormNoAction
		}
	}
	if !ok {
		return nil, browseFormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.keybindings.Match(keyPress, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}):
		col := f.focusedColumn()
		if col >= 0 {
			f.values.nulls[col] = false
			if f.inserting {
				// Entering edit mode means typing a value: leave DEFAULT.
				f.values.defaults[col] = false
			}
		}
		return controller.beginHuh(f.focus()), browseFormNoAction
	case f.keybindings.Match(keyPress, "browse_form.set_null", []scope{scopeView, scopeGlobal}):
		col := f.focusedColumn()
		if col >= 0 {
			f.values.nulls[col] = true
			f.values.fields[col] = ""
			if f.inserting {
				f.values.defaults[col] = false
			}
		}
		f.pendingG = false
		f.rebuildForm()
		return f.form.Init(), browseFormNoAction
	case f.inserting && f.keybindings.Match(keyPress, "browse_form.set_default", []scope{scopeView, scopeGlobal}):
		col := f.focusedColumn()
		if col >= 0 {
			f.values.defaults[col] = true
			f.values.nulls[col] = false
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
		if !f.hasChanges() {
			controller.mode = formModeNormal
			controller.buttonsFocused = false
			return nil, browseFormDiscard
		}
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
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.routeToBar(keyPress, f.focusedColumn() >= len(f.columns)-1, f.blur) {
		return nil, browseFormNoAction
	}
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if f.inserting {
		// Typing selects VALUE: content in the focused field leaves the
		// DEFAULT and NULL states so the typed text reaches the INSERT.
		// (vim-off forms open directly in insert mode, so keys never pass
		// through the state-switch branch above.)
		if col := f.focusedColumn(); col >= 0 && f.values.fields[col] != "" {
			f.values.defaults[col] = false
			f.values.nulls[col] = false
		}
	}
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
	if f.inserting && !save {
		title = "Discard row?"
	}
	if save {
		if f.inserting {
			title = "Insert row?"
		} else {
			title = "Save row changes?"
		}
		if preview := f.preview(); preview != "" {
			f.confirmation = yesNoConfirmation(title, preview, "confirm")
			return
		}
	}
	f.confirmation = yesNoConfirmation(title, "", "confirm")
}

// preview renders the structured write preview shown in confirmations and
// query-log entries: Table, optional Key, and Values (insert) or Changes
// (update). DEFAULT, NULL, and Go-quoted scalars keep the tri-state
// distinct without duplicating any driver SQL in the UI.
func (f browseForm) preview() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", f.table)
	if !f.inserting {
		if key, err := f.keyValues(); err == nil {
			builder.WriteString("\nKey:")
			for _, row := range key {
				fmt.Fprintf(&builder, "\n  %s = %s", row.Name, rowValuePreview(row.Value))
			}
		}
	}
	values := f.rowValues()
	label := "Values:"
	if !f.inserting {
		label = "Changes:"
	}
	if len(values) > 0 {
		builder.WriteString("\n" + label)
		for _, row := range values {
			fmt.Fprintf(&builder, "\n  %s = %s", row.Name, rowValuePreview(row.Value))
		}
	}
	return builder.String()
}

// rowValues converts the form state to the insert/update RowValue list:
// insert includes every non-DEFAULT field, update every dirty field.
// Caller ordering is preserved for the driver's parameter list.
func (f browseForm) rowValues() []sharedsql.RowValue {
	values := make([]sharedsql.RowValue, 0, len(f.columns))
	for index, column := range f.columns {
		if !f.isDirty(index) {
			continue
		}
		values = append(values, sharedsql.RowValue{Name: column, Value: f.rowValue(index)})
	}
	return values
}

// keyValues converts the primary-key columns to the RowValue key list using
// the original values, matching the old WHERE clause.
func (f browseForm) keyValues() ([]sharedsql.RowValue, error) {
	if len(f.primary) == 0 {
		return nil, fmt.Errorf("selected row cannot be updated")
	}
	key := make([]sharedsql.RowValue, 0, len(f.primary))
	for _, index := range f.primary {
		value := sharedsql.Value{Kind: sharedsql.ValueString}
		if f.original[index] == nil {
			value.Kind = sharedsql.ValueNull
		} else {
			value.String = *f.original[index]
		}
		key = append(key, sharedsql.RowValue{Name: f.columns[index], Value: value})
	}
	return key, nil
}

// rowValue converts one column's tri-state to its tagged value: DEFAULT
// (insert only), NULL, or the typed String.
func (f browseForm) rowValue(index int) sharedsql.Value {
	if f.values.nulls[index] {
		return sharedsql.Value{Kind: sharedsql.ValueNull}
	}
	if f.inserting && f.values.defaults[index] {
		return sharedsql.Value{Kind: sharedsql.ValueDefault}
	}
	return sharedsql.Value{Kind: sharedsql.ValueString, String: f.values.fields[index]}
}

// rowValuePreview renders one tagged value for confirmation and query-log
// text: DEFAULT, NULL, or a Go-quoted scalar.
func rowValuePreview(value sharedsql.Value) string {
	switch value.Kind {
	case sharedsql.ValueDefault:
		return "DEFAULT"
	case sharedsql.ValueNull:
		return "NULL"
	default:
		return strconv.Quote(value.String)
	}
}

func (f browseForm) hasChanges() bool {
	for index := range f.columns {
		if f.isDirty(index) {
			return true
		}
	}
	return false
}

func (f browseForm) isDirty(index int) bool {
	if f.inserting {
		return !f.values.defaults[index]
	}
	if f.original[index] == nil {
		return !f.values.nulls[index] // was NULL, now has a value → dirty
	}
	if f.values.nulls[index] {
		return true // had a value, now NULL → dirty
	}
	return f.values.fields[index] != *f.original[index] // value changed
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
		if f.inserting {
			return statusStyle.Render("inserting row")
		}
		return statusStyle.Render("saving row changes")
	}
	if f.form == nil {
		return ""
	}
	return f.form.View()
}
