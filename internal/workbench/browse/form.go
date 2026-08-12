package browse

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// FormAction is the outcome of one form update the root acts on.
type FormAction uint8

const (
	FormNoAction FormAction = iota
	FormSave
	FormDiscard
)

// FormValues carries the per-column tri-state of a row form: the edited
// text, the NULL flag, and (insert forms only) the DEFAULT flag marking
// columns omitted from the INSERT so engine defaults or auto-increment
// apply.
type FormValues struct {
	Fields []string
	Nulls  []bool
	// Defaults marks insert-form fields left on DEFAULT: the column is
	// omitted from the INSERT so engine defaults or auto-increment apply.
	// Only meaningful while inserting; nil otherwise.
	Defaults []bool
}

// Form is the row edit/insert form state: the huh form, its values, the
// confirmation dialog, and the geometry. The root owns the confirmation
// overlay and the save/discard execution; the component owns the form
// construction, key routing, and preview rendering.
type Form struct {
	Form             *huh.Form
	Confirmation     *uikit.ConfirmationDialog
	Values           *FormValues
	Columns          []string
	Original         []*string
	Primary          []int
	Table            string
	Inserting        bool
	Width, Height    int
	PendingG         bool
	Saving           bool
	ConfirmationSave bool
	ScrollOffset     int
	Keybindings      uikit.KeyMatcher
}

// NewInsertForm builds an insert-row form over the given columns. Every
// field starts in the DEFAULT state (column omitted from the INSERT, so
// engine defaults or auto-increment apply). Typing moves a field to VALUE
// (the empty string included); the set_null and set_default bindings move
// it to NULL or back to DEFAULT. Unlike the edit form, no primary key is
// required.
func NewInsertForm(columns []string) (Form, error) {
	if len(columns) == 0 {
		return Form{}, fmt.Errorf("table has no columns")
	}
	values := &FormValues{
		Fields:   make([]string, len(columns)),
		Nulls:    make([]bool, len(columns)),
		Defaults: make([]bool, len(columns)),
	}
	for index := range values.Defaults {
		values.Defaults[index] = true
	}
	form := Form{
		Inserting:   true,
		Columns:     append([]string(nil), columns...),
		Values:      values,
		Keybindings: nil,
	}
	form.RebuildForm()
	return form, nil
}

// NewForm builds an edit-row form over the row's columns and original
// values. A row without a primary key cannot be edited.
func NewForm(columns []string, original []*string, info []sharedsql.ColumnInfo) (Form, error) {
	if len(columns) == 0 || len(columns) != len(original) {
		return Form{}, fmt.Errorf("selected row is unavailable")
	}
	primaryNames := make(map[string]bool, len(info))
	for _, column := range info {
		if column.PrimaryKey > 0 {
			primaryNames[strings.ToLower(column.Name)] = true
		}
	}
	form := Form{
		Columns:     append([]string(nil), columns...),
		Original:    append([]*string(nil), original...),
		Values:      &FormValues{Fields: make([]string, len(original)), Nulls: make([]bool, len(original))},
		Keybindings: nil,
	}
	for index, value := range original {
		if value == nil {
			form.Values.Nulls[index] = true
		} else {
			form.Values.Fields[index] = *value
		}
		if primaryNames[strings.ToLower(columns[index])] {
			form.Primary = append(form.Primary, index)
		}
	}
	if len(form.Primary) == 0 {
		return Form{}, fmt.Errorf("cannot edit rows without a primary key")
	}
	form.RebuildForm()
	return form, nil
}

// Active reports whether the form holds columns (the zero value is
// inactive).
func (f Form) Active() bool { return len(f.Columns) > 0 }

// Confirming reports whether the save/discard confirmation is open.
func (f Form) Confirming() bool { return f.Confirmation != nil }

// Update routes one message through the form: the confirmation, the
// Save/Cancel bar, the modal modes, and the field navigation. The
// controller is the session's shared form-mode controller; the root keeps
// it current for the mode badge and the button bar.
func (f *Form) Update(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, FormAction) {
	if f.Saving {
		return nil, FormNoAction
	}
	if f.Confirmation != nil {
		completed, action := f.Confirmation.Update(message, f.Width, f.Height)
		if !completed {
			return nil, FormNoAction
		}
		save := f.ConfirmationSave
		f.Confirmation = nil
		controller.Mode = uikit.FormModeNormal
		if action != "confirm" {
			return nil, FormNoAction
		}
		if save {
			return nil, FormSave
		}
		return nil, FormDiscard
	}
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.ButtonsFocused {
		if route, replayed, cmd := controller.RouteFormButtons(keyPress, f.Keybindings, f.LastField); route != uikit.FormButtonContinue {
			if route == uikit.FormButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, FormNoAction
			}
		}
	}
	if !replay {
		if route := controller.RouteHuh(message, f.Blur); route != uikit.FormRouteParent {
			if route == uikit.FormRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, FormNoAction
		}
	}
	if !ok {
		return nil, FormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.Keybindings.Match(keyPress, "form.edit", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		col := f.FocusedColumn()
		if col >= 0 {
			f.Values.Nulls[col] = false
			if f.Inserting {
				// Entering edit mode means typing a value: leave DEFAULT.
				f.Values.Defaults[col] = false
			}
		}
		return controller.BeginHuh(f.Focus()), FormNoAction
	case f.Keybindings.Match(keyPress, "browse_form.set_null", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
		col := f.FocusedColumn()
		if col >= 0 {
			f.Values.Nulls[col] = true
			f.Values.Fields[col] = ""
			if f.Inserting {
				f.Values.Defaults[col] = false
			}
		}
		f.PendingG = false
		f.RebuildForm()
		return f.Form.Init(), FormNoAction
	case f.Inserting && f.Keybindings.Match(keyPress, "browse_form.set_default", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
		col := f.FocusedColumn()
		if col >= 0 {
			f.Values.Defaults[col] = true
			f.Values.Nulls[col] = false
			f.Values.Fields[col] = ""
		}
		f.PendingG = false
		f.RebuildForm()
		return f.Form.Init(), FormNoAction
	case f.Keybindings.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		f.BeginConfirmation(true)
		controller.BeginConfirm()
		return nil, FormNoAction
	case f.Keybindings.Match(keyPress, "form.discard", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if !f.HasChanges() {
			controller.Mode = uikit.FormModeNormal
			controller.ButtonsFocused = false
			return nil, FormDiscard
		}
		f.BeginConfirmation(false)
		controller.BeginConfirm()
		return nil, FormNoAction
	case f.Keybindings.Match(keyPress, "form.field_next", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		f.PendingG = false
		if f.FocusedColumn() >= len(f.Columns)-1 {
			controller.FocusButtons()
			f.Blur()
			return nil, FormNoAction
		}
		return f.NextField(), FormNoAction
	case f.Keybindings.Match(keyPress, "form.field_prev", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		f.PendingG = false
		return f.PreviousField(), FormNoAction
	case f.Keybindings.Match(keyPress, "browse_form.field_top", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
		if f.PendingG {
			f.PendingG = false
			return f.FirstField(), FormNoAction
		}
		f.PendingG = true
		return nil, FormNoAction
	case f.Keybindings.Match(keyPress, "browse_form.field_bottom", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
		f.PendingG = false
		return f.LastField(), FormNoAction
	default:
		f.PendingG = false
	}
	return nil, FormNoAction
}

func (f *Form) updateHuh(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, FormAction) {
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.RouteToBar(keyPress, f.FocusedColumn() >= len(f.Columns)-1, f.Blur) {
		return nil, FormNoAction
	}
	model, command := f.Form.Update(message)
	f.Form = model.(*huh.Form)
	if f.Inserting {
		// Typing selects VALUE: content in the focused field leaves the
		// DEFAULT and NULL states so the typed text reaches the INSERT.
		// (vim-off forms open directly in insert mode, so keys never pass
		// through the state-switch branch above.)
		if col := f.FocusedColumn(); col >= 0 && f.Values.Fields[col] != "" {
			f.Values.Defaults[col] = false
			f.Values.Nulls[col] = false
		}
	}
	if f.Form.State == huh.StateCompleted {
		f.RebuildForm()
		return f.Focus(), FormNoAction
	}
	f.ScrollToColumn(f.FocusedColumn())
	return command, FormNoAction
}

// BeginConfirmation opens the save or discard confirmation carrying the
// structured write preview.
func (f *Form) BeginConfirmation(save bool) {
	f.ConfirmationSave = save
	title := "Discard row changes?"
	if f.Inserting && !save {
		title = "Discard row?"
	}
	if save {
		if f.Inserting {
			title = "Insert row?"
		} else {
			title = "Save row changes?"
		}
		if preview := f.Preview(); preview != "" {
			f.Confirmation = uikit.YesNoConfirmation(title, preview, "confirm")
			return
		}
	}
	f.Confirmation = uikit.YesNoConfirmation(title, "", "confirm")
}

// Preview renders the structured write preview shown in confirmations and
// query-log entries: Table, optional Key, and Values (insert) or Changes
// (update). DEFAULT, NULL, and Go-quoted scalars keep the tri-state
// distinct without duplicating any driver SQL in the UI.
func (f Form) Preview() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", f.Table)
	if !f.Inserting {
		if key, err := f.KeyValues(); err == nil {
			builder.WriteString("\nKey:")
			for _, row := range key {
				fmt.Fprintf(&builder, "\n  %s = %s", row.Name, RowValuePreview(row.Value))
			}
		}
	}
	values := f.RowValues()
	label := "Values:"
	if !f.Inserting {
		label = "Changes:"
	}
	if len(values) > 0 {
		builder.WriteString("\n" + label)
		for _, row := range values {
			fmt.Fprintf(&builder, "\n  %s = %s", row.Name, RowValuePreview(row.Value))
		}
	}
	return builder.String()
}

// RowValues converts the form state to the insert/update RowValue list:
// insert includes every non-DEFAULT field, update every dirty field.
// Caller ordering is preserved for the driver's parameter list.
func (f Form) RowValues() []sharedsql.RowValue {
	values := make([]sharedsql.RowValue, 0, len(f.Columns))
	for index, column := range f.Columns {
		if !f.IsDirty(index) {
			continue
		}
		values = append(values, sharedsql.RowValue{Name: column, Value: f.rowValue(index)})
	}
	return values
}

// KeyValues converts the primary-key columns to the RowValue key list
// using the original values, matching the old WHERE clause.
func (f Form) KeyValues() ([]sharedsql.RowValue, error) {
	if len(f.Primary) == 0 {
		return nil, fmt.Errorf("selected row cannot be updated")
	}
	key := make([]sharedsql.RowValue, 0, len(f.Primary))
	for _, index := range f.Primary {
		value := sharedsql.Value{Kind: sharedsql.ValueString}
		if f.Original[index] == nil {
			value.Kind = sharedsql.ValueNull
		} else {
			value.String = *f.Original[index]
		}
		key = append(key, sharedsql.RowValue{Name: f.Columns[index], Value: value})
	}
	return key, nil
}

// rowValue converts one column's tri-state to its tagged value: DEFAULT
// (insert only), NULL, or the typed String.
func (f Form) rowValue(index int) sharedsql.Value {
	if f.Values.Nulls[index] {
		return sharedsql.Value{Kind: sharedsql.ValueNull}
	}
	if f.Inserting && f.Values.Defaults[index] {
		return sharedsql.Value{Kind: sharedsql.ValueDefault}
	}
	return sharedsql.Value{Kind: sharedsql.ValueString, String: f.Values.Fields[index]}
}

// HasChanges reports whether any column differs from its original value.
func (f Form) HasChanges() bool {
	for index := range f.Columns {
		if f.IsDirty(index) {
			return true
		}
	}
	return false
}

// IsDirty reports whether one column differs from its original state.
func (f Form) IsDirty(index int) bool {
	if f.Inserting {
		return !f.Values.Defaults[index]
	}
	if f.Original[index] == nil {
		return !f.Values.Nulls[index] // was NULL, now has a value → dirty
	}
	if f.Values.Nulls[index] {
		return true // had a value, now NULL → dirty
	}
	return f.Values.Fields[index] != *f.Original[index] // value changed
}

// RebuildForm rebuilds the huh form from the current columns.
func (f *Form) RebuildForm() {
	fields := make([]huh.Field, 0, len(f.Columns))
	for index, column := range f.Columns {
		fields = append(fields,
			uikit.NewEditableInput(huh.NewInput().Key(f.ValueKey(index)).Title(column).Value(&f.Values.Fields[index]), &f.Values.Fields[index]),
		)
	}
	f.Form = uikit.NewForm(huh.NewGroup(fields...)).WithShowHelp(f.Width >= 40).WithWidth(max(f.Width, 1))
}

// ValueKey returns the huh key of the field for the column at index.
func (f Form) ValueKey(index int) string { return fmt.Sprintf("value-%d", index) }

// FocusedColumn returns the column index of the focused field, or 0 when
// the form has no focus.
func (f Form) FocusedColumn() int {
	if f.Form == nil {
		return 0
	}
	key := f.Form.GetFocusedField().GetKey()
	for index := range f.Columns {
		if key == f.ValueKey(index) {
			return index
		}
	}
	return 0
}

// NextField advances the field cursor.
func (f *Form) NextField() tea.Cmd {
	if f.FocusedColumn() == len(f.Columns)-1 {
		return nil
	}
	f.ScrollToColumn(f.FocusedColumn() + 1)
	return f.Form.NextField()
}

// PreviousField retreats the field cursor.
func (f *Form) PreviousField() tea.Cmd {
	if f.FocusedColumn() == 0 {
		return nil
	}
	f.ScrollToColumn(f.FocusedColumn() - 1)
	return f.Form.PrevField()
}

// FocusColumn moves the field cursor to the column at index and scrolls it
// into view. The loop bounds guard against Huh navigation skipping fields.
func (f *Form) FocusColumn(col int) tea.Cmd {
	col = min(max(col, 0), len(f.Columns)-1)
	for range len(f.Columns) {
		if f.FocusedColumn() >= col {
			break
		}
		_ = f.Form.NextField()
	}
	for range len(f.Columns) {
		if f.FocusedColumn() <= col {
			break
		}
		_ = f.Form.PrevField()
	}
	f.ScrollToColumn(col)
	return f.Focus()
}

// FirstField jumps to the first column.
func (f *Form) FirstField() tea.Cmd {
	for f.FocusedColumn() > 0 {
		_ = f.Form.PrevField()
	}
	f.ScrollOffset = 0
	return f.Focus()
}

// LastField jumps to the last column.
func (f *Form) LastField() tea.Cmd {
	for f.FocusedColumn() < len(f.Columns)-1 {
		_ = f.Form.NextField()
	}
	f.ScrollToColumn(len(f.Columns) - 1)
	return f.Focus()
}

// ScrollToColumn advances the viewport offset so the column is visible.
func (f *Form) ScrollToColumn(col int) {
	f.ScrollOffset = col * 2
}

// Blur blurs the focused field.
func (f *Form) Blur() {
	if f.Form != nil {
		_ = f.Form.GetFocusedField().Blur()
	}
}

// Focus focuses the focused field.
func (f *Form) Focus() tea.Cmd {
	if f.Form == nil {
		return nil
	}
	return f.Form.GetFocusedField().Focus()
}

// SetWidth refits the form to the given content width.
func (f *Form) SetWidth(width int) {
	f.Width = max(width, 1)
	if f.Form != nil {
		f.Form.WithWidth(f.Width).WithShowHelp(f.Width >= 40)
	}
}

// View renders the form body.
func (f Form) View() string {
	if f.Saving {
		if f.Inserting {
			return uikit.StatusStyle.Render("inserting row")
		}
		return uikit.StatusStyle.Render("saving row changes")
	}
	if f.Form == nil {
		return ""
	}
	return f.Form.View()
}

// isInsertModeKey reports whether the key press enters insert mode.
func isInsertModeKey(keyPress tea.KeyPressMsg) bool {
	return keyPress.Key().Code == 'i'
}
