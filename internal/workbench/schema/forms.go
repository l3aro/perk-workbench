package schema

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// isInsertModeKey reports whether the key press enters insert mode.
func isInsertModeKey(keyPress tea.KeyPressMsg) bool {
	return keyPress.Key().Code == 'i'
}

// noopKeys is the placeholder matcher used before the root installs the
// session keybindings via SetKeys; it matches nothing, so a freshly
// constructed form routes every key as an unbound press.
type noopKeys struct{}

func (noopKeys) Match(tea.KeyPressMsg, uikit.CommandID, []uikit.Scope) bool { return false }

// ColumnFormAction is the outcome of one column form update the root acts
// on: saving/altering the column, discarding, or deleting it.
type ColumnFormAction uint8

const (
	ColumnFormNoAction ColumnFormAction = iota
	ColumnFormSave
	ColumnFormDiscard
	ColumnFormDelete
)

// ColumnFormValues is the editable column state of the structure form.
type ColumnFormValues struct {
	Name, TypeName, DefaultValue, Attributes string
	Parameters                               []string
	Nullable                                 bool
}

// ColumnForm is the structure tab's add/edit column form: the huh form, its
// values, the save/discard confirmation, the type options, and the
// geometry. The root owns the confirmation overlay and the DDL execution;
// the component owns the form construction, key routing, and validation.
type ColumnForm struct {
	Form                            *huh.Form
	Confirmation                    *uikit.ConfirmationDialog
	Values                          *ColumnFormValues
	baseline                        ColumnFormValues
	PreviousName, OriginalType      string
	OriginalAttributes              string
	TypeOptions                     []sharedsql.ColumnType
	ValidationError                 string
	Width, Height, ScrollOffset     int
	formType                        string
	HadDefault, TypeChanged, Saving bool
	IsNew                           bool
	ConfirmationSave                bool
	ConfirmationDelete              bool
	keys                            uikit.KeyMatcher
}

func NewColumnForm(column sharedsql.ColumnInfo, typeOptions []sharedsql.ColumnType) ColumnForm {
	form := ColumnForm{
		PreviousName:       column.Name,
		OriginalType:       column.Type,
		HadDefault:         column.DefaultValue != nil,
		OriginalAttributes: column.Attributes,
		TypeOptions:        typeOptions,
		Values:             &ColumnFormValues{Name: column.Name, Nullable: column.Nullable, Attributes: column.Attributes},
	}
	if index, values, ok := sharedsql.MatchColumnType(typeOptions, column.Type); ok {
		form.selectType(index, values)
	} else {
		if strings.TrimSpace(column.Type) != "" {
			form.TypeOptions = append([]sharedsql.ColumnType{{Name: column.Type}}, typeOptions...)
		}
		form.selectType(0, nil)
	}
	if column.DefaultValue != nil {
		form.Values.DefaultValue = *column.DefaultValue
	}
	form.baseline = *form.Values
	form.baseline.Parameters = slices.Clone(form.Values.Parameters)
	form.rebuildForm()
	return form
}

func NewEmptyColumnForm(typeOptions []sharedsql.ColumnType) ColumnForm {
	form := ColumnForm{
		Values:      &ColumnFormValues{Nullable: true},
		TypeOptions: typeOptions,
		IsNew:       true,
	}
	form.selectType(0, nil)
	form.baseline = *form.Values
	form.baseline.Parameters = slices.Clone(form.Values.Parameters)
	form.rebuildForm()
	return form
}

// Active reports whether the form holds fields.
func (f ColumnForm) Active() bool { return f.Form != nil }

// Confirming reports whether the save/discard confirmation is open.
func (f ColumnForm) Confirming() bool { return f.Confirmation != nil }

// SetKeys installs the session keybindings for the form's bindings.
func (f *ColumnForm) SetKeys(keys uikit.KeyMatcher) { f.keys = keys }

// Update routes one message through the column form: the confirmation, the
// Save/Cancel bar, the modal modes, and the field navigation. The
// controller is the session's shared form-mode controller; the root keeps
// it current for the mode badge and the button bar.
func (f *ColumnForm) Update(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, ColumnFormAction) {
	if f.Saving {
		return nil, ColumnFormNoAction
	}
	if f.Confirmation != nil {
		completed, action := f.Confirmation.Update(message, f.Width, f.Height)
		if !completed {
			return nil, ColumnFormNoAction
		}
		f.Confirmation = nil
		controller.Mode = uikit.FormModeNormal
		if action != "confirm" {
			return nil, ColumnFormNoAction
		}
		if f.ConfirmationSave {
			return nil, ColumnFormSave
		}
		if f.ConfirmationDelete {
			return nil, ColumnFormDelete
		}
		return nil, ColumnFormDiscard
	}
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	// A replayed activation key skips mode routing so Enter on Cancel
	// discards instead of being eaten as insert-mode Escape.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.ButtonsFocused {
		if route, replayed, cmd := controller.RouteFormButtons(keyPress, f.keys, func() tea.Cmd { return f.FocusField(f.FieldCount() - 1) }); route != uikit.FormButtonContinue {
			if route == uikit.FormButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, ColumnFormNoAction
			}
		}
	}
	if !replay {
		if route := controller.RouteHuh(message, f.blur); route != uikit.FormRouteParent {
			if route == uikit.FormRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, ColumnFormNoAction
		}
	}
	if !ok {
		return nil, ColumnFormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.keys.Match(keyPress, "form.edit", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		return controller.BeginHuh(f.Focus()), ColumnFormNoAction
	case f.keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.IsNew {
			if _, err := f.ColumnDef(); err != nil {
				f.ValidationError = err.Error()
				return nil, ColumnFormNoAction
			}
		} else if _, err := f.Change(); err != nil {
			f.ValidationError = err.Error()
			return nil, ColumnFormNoAction
		}
		f.beginConfirmation(true, false)
		controller.BeginConfirm()
		return nil, ColumnFormNoAction
	case f.keys.Match(keyPress, "form.discard", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if !f.HasChanges() {
			controller.Mode = uikit.FormModeNormal
			controller.ButtonsFocused = false
			return nil, ColumnFormDiscard
		}
		f.beginConfirmation(false, false)
		controller.BeginConfirm()
		return nil, ColumnFormNoAction
	case f.keys.Match(keyPress, "form.delete", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.PreviousName != "" {
			f.beginConfirmation(false, true)
			controller.BeginConfirm()
		}
		return nil, ColumnFormNoAction
	case f.keys.Match(keyPress, "form.field_next", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.focusedField() >= f.FieldCount()-1 {
			controller.FocusButtons()
			f.blur()
			return nil, ColumnFormNoAction
		}
		return f.NextField(), ColumnFormNoAction
	case f.keys.Match(keyPress, "form.field_prev", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		return f.PreviousField(), ColumnFormNoAction
	}
	return nil, ColumnFormNoAction
}

func (f *ColumnForm) updateHuh(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, ColumnFormAction) {
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.RouteToBar(keyPress, f.focusedField() >= f.FieldCount()-1, f.blur) {
		return nil, ColumnFormNoAction
	}
	focused := f.focusedField()
	model, command := f.Form.Update(message)
	f.Form = model.(*huh.Form)
	if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "enter" && f.Values.TypeName != f.formType {
		f.TypeChanged = true
		f.SelectType(f.typeIndex(), nil)
		f.rebuildForm()
		f.ScrollToField(focused)
		return f.FocusField(focused), ColumnFormNoAction
	}
	if f.Form.State == huh.StateCompleted {
		f.rebuildForm()
		f.ScrollToField(focused)
		return f.FocusField(focused), ColumnFormNoAction
	}
	f.ScrollToField(f.focusedField())
	return command, ColumnFormNoAction
}

func (f *ColumnForm) beginConfirmation(save, delete bool) {
	f.ConfirmationSave = save
	f.ConfirmationDelete = delete
	title := "Discard column changes?"
	if save && f.IsNew {
		title = "Add column?"
	} else if save {
		title = "Save column changes?"
	} else if delete {
		title = "Delete column?"
	}
	f.Confirmation = uikit.YesNoConfirmation(title, "", "confirm")
}

func (f *ColumnForm) blur() {
	if f.Form != nil {
		_ = f.Form.GetFocusedField().Blur()
	}
}

// Focus focuses the focused field.
func (f *ColumnForm) Focus() tea.Cmd {
	if f.Form == nil {
		return nil
	}
	return f.Form.GetFocusedField().Focus()
}

// NextField advances the field cursor.
func (f *ColumnForm) NextField() tea.Cmd {
	field := f.focusedField()
	if field >= f.FieldCount()-1 {
		return nil
	}
	_ = f.Form.GetFocusedField().Blur()
	f.ScrollToField(field + 1)
	return f.Form.NextField()
}

// PreviousField retreats the field cursor.
func (f *ColumnForm) PreviousField() tea.Cmd {
	field := f.focusedField()
	if field == 0 {
		return nil
	}
	_ = f.Form.GetFocusedField().Blur()
	f.ScrollToField(field - 1)
	return f.Form.PrevField()
}

func (f ColumnForm) fieldCount() int { return len(f.Values.Parameters) + 5 }

// FieldCount returns the number of form fields.
func (f ColumnForm) FieldCount() int { return f.fieldCount() }

func (f ColumnForm) focusedField() int {
	if f.Form == nil {
		return 0
	}
	key := f.Form.GetFocusedField().GetKey()
	switch {
	case key == "name":
		return 0
	case key == "type":
		return 1
	case strings.HasPrefix(key, "parameter-"):
		index, err := strconv.Atoi(strings.TrimPrefix(key, "parameter-"))
		if err == nil {
			return min(index+2, f.FieldCount()-1)
		}
	case key == "nullable":
		return len(f.Values.Parameters) + 2
	case key == "default":
		return len(f.Values.Parameters) + 3
	case key == "attributes":
		return len(f.Values.Parameters) + 4
	}
	return 0
}

// FocusedField returns the index of the focused field.
func (f ColumnForm) FocusedField() int { return f.focusedField() }

// FocusField moves the field cursor to the field at index. The loop bounds
// guard against Huh navigation skipping fields.
func (f *ColumnForm) FocusField(field int) tea.Cmd {
	field = min(max(field, 0), f.FieldCount()-1)
	for range f.FieldCount() {
		if f.focusedField() >= field {
			break
		}
		_ = f.Form.NextField()
	}
	for range f.FieldCount() {
		if f.focusedField() <= field {
			break
		}
		_ = f.Form.PrevField()
	}
	return f.Focus()
}

// FieldTitles lists the rendered titles of every column form field in
// render order; parameter titles come from the selected type.
func (f ColumnForm) FieldTitles() []string {
	titles := []string{"Name*", "Type*"}
	for _, parameter := range f.TypeOptions[f.typeIndex()].Parameters {
		titles = append(titles, parameter.Name)
	}
	return append(titles, "Nullable", "Default", "Attributes")
}

// ScrollToField moves the viewport offset so the field at index is visible.
func (f *ColumnForm) ScrollToField(field int) {
	f.ScrollOffset = max(field*2, 0)
}

func (f *ColumnForm) scrollToField(field int) {
	f.ScrollOffset = max(field*2, 0)
}

// HasChanges reports whether the form differs from the values it was opened
// with, in ways a save would persist. Type parameters count only when the
// type was re-selected or the column had no recorded type, mirroring
// typeDeclaration.
func (f ColumnForm) HasChanges() bool {
	baseline := f.baseline
	if f.Values.Name != baseline.Name || f.Values.Nullable != baseline.Nullable {
		return true
	}
	// The save path drops the default when the field is emptied, so clearing
	// a non-empty default must count as a change; raw equality keeps a
	// pristine form clean even when the default is whitespace or empty
	// (DEFAULT '' is indistinguishable from a cleared field, so it never
	// counts as changed).
	if f.Values.DefaultValue != baseline.DefaultValue {
		return true
	}
	if strings.TrimSpace(f.Values.Attributes) != strings.TrimSpace(baseline.Attributes) {
		return true
	}
	if f.TypeChanged || strings.TrimSpace(f.OriginalType) == "" {
		return f.Values.TypeName != baseline.TypeName || !slices.Equal(f.Values.Parameters, baseline.Parameters)
	}
	return false
}

// Change builds the persisted column change, validating it.
func (f ColumnForm) Change() (sharedsql.ColumnChange, error) {
	typeDeclaration, err := f.typeDeclaration()
	if err != nil {
		return sharedsql.ColumnChange{}, err
	}
	change := sharedsql.ColumnChange{PreviousName: f.PreviousName, Name: f.Values.Name, Type: typeDeclaration, Nullable: f.Values.Nullable}
	if value := strings.TrimSpace(f.Values.DefaultValue); value != "" {
		change.DefaultValue = &value
	}
	if value := strings.TrimSpace(f.Values.Attributes); value != f.OriginalAttributes {
		change.Attributes = &value
	}
	if err := sharedsql.ValidateColumnChange(change); err != nil {
		return sharedsql.ColumnChange{}, err
	}
	return change, nil
}

// ColumnDef builds the new-column definition, validating it.
func (f ColumnForm) ColumnDef() (sharedsql.ColumnDef, error) {
	typeDeclaration, err := f.typeDeclaration()
	if err != nil {
		return sharedsql.ColumnDef{}, err
	}
	def := sharedsql.ColumnDef{Name: f.Values.Name, Type: typeDeclaration, Nullable: f.Values.Nullable}
	if value := strings.TrimSpace(f.Values.DefaultValue); value != "" {
		def.DefaultValue = &value
	}
	if value := strings.TrimSpace(f.Values.Attributes); value != "" {
		def.Attributes = &value
	}
	if err := sharedsql.ValidateColumnDef(def); err != nil {
		return sharedsql.ColumnDef{}, err
	}
	return def, nil
}

// SetWidth refits the form to the given content width.
func (f *ColumnForm) SetWidth(width int) {
	f.Width = max(width, 1)
	if f.Form != nil {
		f.Form.WithWidth(f.Width).WithShowHelp(f.Width >= 40)
	}
}

// SetHeight caps the form to the given viewport height.
func (f *ColumnForm) SetHeight(height int) {
	f.Height = max(height, 1)
	if f.Form != nil {
		f.Form.WithHeight(f.Height)
	}
}

// RebuildForm rebuilds the huh form from the current values.
func (f *ColumnForm) RebuildForm() { f.rebuildForm() }

func (f *ColumnForm) rebuildForm() {
	f.formType = f.Values.TypeName
	fields := []huh.Field{
		uikit.NewEditableInput(huh.NewInput().Key("name").Title("Name*").Value(&f.Values.Name).Validate(requiredColumnName), &f.Values.Name),
		huh.NewSelect[string]().Key("type").Title("Type*").Options(f.TypeChoices()...).Value(&f.Values.TypeName).Validate(f.validateType),
	}
	for index, parameter := range f.TypeOptions[f.typeIndex()].Parameters {
		index, parameter := index, parameter
		fields = append(fields, uikit.NewEditableInput(huh.NewInput().Key("parameter-"+strconv.Itoa(index)).Title(parameter.Name).Value(&f.Values.Parameters[index]).Validate(f.validateParameter(index)), &f.Values.Parameters[index]))
	}
	fields = append(fields,
		huh.NewConfirm().Key("nullable").Title("Nullable").Affirmative("Yes").Negative("No").Value(&f.Values.Nullable),
		uikit.NewEditableInput(huh.NewInput().Key("default").Title("Default").Value(&f.Values.DefaultValue), &f.Values.DefaultValue),
		f.attributesField(),
	)
	f.Form = uikit.NewForm(huh.NewGroup(fields...)).WithShowHelp(f.Width >= 40).WithWidth(max(f.Width, 1))
	if f.Height > 0 {
		// The pane height is unknown while the form is being constructed;
		// capping then would freeze every field at that tiny height (huh's
		// group only shrinks fields, never grows them). Apply the height
		// once the real pane size is known, like the connection form does.
		f.Form.WithHeight(f.Height)
	}
}

// attributesField returns the attributes form field: a select of the
// driver/type-specific attribute options when the selected type declares
// any, otherwise a free-text input for database-specific attributes.
func (f ColumnForm) attributesField() huh.Field {
	options := f.TypeOptions[f.typeIndex()].Attributes
	if len(options) == 0 {
		return uikit.NewEditableInput(huh.NewInput().Key("attributes").Title("Attributes").Value(&f.Values.Attributes), &f.Values.Attributes)
	}
	choices := make([]huh.Option[string], 0, len(options)+2)
	choices = append(choices, huh.NewOption("None", ""))
	seen := false
	for _, option := range options {
		if option == f.Values.Attributes {
			seen = true
		}
		choices = append(choices, huh.NewOption(option, option))
	}
	if !seen && strings.TrimSpace(f.Values.Attributes) != "" {
		choices = append(choices, huh.NewOption(f.Values.Attributes, f.Values.Attributes))
	}
	return huh.NewSelect[string]().Key("attributes").Title("Attributes").Options(choices...).Value(&f.Values.Attributes)
}

func requiredColumnName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("column name is required")
	}
	return nil
}

// SelectType selects the type option at index and seeds its parameters.
func (f *ColumnForm) SelectType(index int, values []string) {
	f.selectType(index, values)
}

func (f *ColumnForm) selectType(index int, values []string) {
	if index < 0 || index >= len(f.TypeOptions) {
		return
	}
	// Drop an attribute picked from the previous type's option set (e.g.
	// AUTO_INCREMENT) when the new type no longer offers it, instead of
	// emitting invalid DDL like "TEXT AUTO_INCREMENT". Free-text attributes
	// (COMMENT 'x') and values seeded from the database survive.
	attribute := f.Values.Attributes
	if attribute != "" && slices.Contains(f.TypeOptions[f.typeIndex()].Attributes, attribute) && !slices.Contains(f.TypeOptions[index].Attributes, attribute) {
		f.Values.Attributes = ""
	}
	f.Values.TypeName = f.TypeOptions[index].Name
	parameters := f.TypeOptions[index].Parameters
	f.Values.Parameters = make([]string, len(parameters))
	for parameterIndex, parameter := range parameters {
		f.Values.Parameters[parameterIndex] = parameter.Default
		if parameterIndex < len(values) {
			f.Values.Parameters[parameterIndex] = strings.TrimSpace(values[parameterIndex])
		}
	}
}

func (f ColumnForm) typeIndex() int {
	for index, option := range f.TypeOptions {
		if option.Name == f.Values.TypeName {
			return index
		}
	}
	return 0
}

// TypeChoices renders the selectable type options.
func (f ColumnForm) TypeChoices() []huh.Option[string] { return f.typeChoices() }

func (f ColumnForm) typeChoices() []huh.Option[string] {
	choices := make([]huh.Option[string], len(f.TypeOptions))
	for index, option := range f.TypeOptions {
		label := option.Label
		if label == "" {
			label = option.Name
		}
		choices[index] = huh.NewOption(label, option.Name)
	}
	return choices
}

func (f ColumnForm) validateType(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("column type is required")
	}
	for _, option := range f.TypeOptions {
		if option.Name == value {
			return nil
		}
	}
	return fmt.Errorf("column type is invalid")
}

func (f ColumnForm) validateParameter(index int) func(string) error {
	return func(value string) error {
		values := append([]string(nil), f.Values.Parameters...)
		values[index] = value
		_, err := f.TypeOptions[f.typeIndex()].Declaration(values)
		return err
	}
}

func (f ColumnForm) typeDeclaration() (string, error) {
	if !f.TypeChanged && strings.TrimSpace(f.OriginalType) != "" {
		if _, err := f.TypeOptions[f.typeIndex()].Declaration(f.Values.Parameters); err != nil {
			return "", err
		}
		return f.OriginalType, nil
	}
	if err := f.validateType(f.Values.TypeName); err != nil {
		return "", err
	}
	return f.TypeOptions[f.typeIndex()].Declaration(f.Values.Parameters)
}

// View renders the column form body.
func (f ColumnForm) View() string {
	if f.Saving {
		return uikit.StatusStyle.Render("saving column changes")
	}
	if f.Form == nil {
		return ""
	}
	return f.Form.View()
}

// IndexFormAction is the outcome of one index form update the root acts on.
type IndexFormAction uint8

const (
	IndexFormNoAction IndexFormAction = iota
	IndexFormSave
	IndexFormDiscard
	IndexFormDelete
)

const (
	IndexKindNormal  = "Index"
	IndexKindUnique  = "Unique"
	IndexKindPrimary = "Primary key"
)

// IndexFormValues is the editable index state of the index form.
type IndexFormValues struct {
	Name, Columns string
	Kind          string
}

// IndexForm is the indexes tab's create/edit index form: the huh form, its
// values, the save/discard confirmation, and the geometry. The root owns
// the confirmation overlay and the DDL execution.
type IndexForm struct {
	Form               *huh.Form
	Confirmation       *uikit.ConfirmationDialog
	Values             *IndexFormValues
	baseline           IndexFormValues
	Previous           string
	Width, Height      int
	ScrollOffset       int
	Saving             bool
	ConfirmationSave   bool
	ConfirmationDelete bool
	keys               uikit.KeyMatcher
}

func NewIndexForm(index *sharedsql.IndexInfo) IndexForm {
	form := IndexForm{Values: &IndexFormValues{Kind: IndexKindNormal}, keys: noopKeys{}}
	if index != nil {
		form.Previous, form.Values.Name, form.Values.Columns = index.Name, index.Name, strings.Join(index.Columns, ", ")
		switch {
		case index.PrimaryKey:
			form.Values.Kind = IndexKindPrimary
		case index.Unique:
			form.Values.Kind = IndexKindUnique
		}
	}
	form.baseline = *form.Values
	form.rebuildForm()
	return form
}

// Active reports whether the form holds fields.
func (f IndexForm) Active() bool { return f.Values != nil }

// Confirming reports whether the save/discard confirmation is open.
func (f IndexForm) Confirming() bool { return f.Confirmation != nil }

// Close drops the form.
func (f *IndexForm) Close() { *f = IndexForm{} }

// SetKeys installs the session keybindings for the form's bindings.
func (f *IndexForm) SetKeys(keys uikit.KeyMatcher) { f.keys = keys }

// Update routes one message through the index form.
func (f *IndexForm) Update(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, IndexFormAction) {
	if f.Saving {
		return nil, IndexFormNoAction
	}
	if f.Confirmation != nil {
		completed, action := f.Confirmation.Update(message, f.Width, f.Height)
		if !completed {
			return nil, IndexFormNoAction
		}
		f.Confirmation = nil
		controller.Mode = uikit.FormModeNormal
		if action != "confirm" {
			return nil, IndexFormNoAction
		}
		if f.ConfirmationDelete {
			return nil, IndexFormDelete
		}
		if f.ConfirmationSave {
			return nil, IndexFormSave
		}
		return nil, IndexFormDiscard
	}
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.ButtonsFocused {
		if route, replayed, cmd := controller.RouteFormButtons(keyPress, f.keys, func() tea.Cmd { return f.FocusField(2) }); route != uikit.FormButtonContinue {
			if route == uikit.FormButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, IndexFormNoAction
			}
		}
	}
	if !replay {
		if route := controller.RouteHuh(message, f.blur); route != uikit.FormRouteParent {
			if route == uikit.FormRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, IndexFormNoAction
		}
	}
	if !ok {
		return nil, IndexFormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.keys.Match(keyPress, "form.edit", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		return controller.BeginHuh(f.Focus()), IndexFormNoAction
	case f.keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if _, err := f.Change(); err != nil {
			f.showValidationError()
			return nil, IndexFormNoAction
		}
		f.beginConfirmation(true, false)
		controller.BeginConfirm()
		return nil, IndexFormNoAction
	case f.keys.Match(keyPress, "form.discard", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if !f.HasChanges() {
			controller.Mode = uikit.FormModeNormal
			controller.ButtonsFocused = false
			return nil, IndexFormDiscard
		}
		f.beginConfirmation(false, false)
		controller.BeginConfirm()
		return nil, IndexFormNoAction
	case f.keys.Match(keyPress, "form.delete", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.Previous != "" {
			f.beginConfirmation(false, true)
			controller.BeginConfirm()
			return nil, IndexFormNoAction
		}
	case f.keys.Match(keyPress, "form.field_next", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.focusedField() >= 2 {
			controller.FocusButtons()
			f.blur()
			return nil, IndexFormNoAction
		}
		f.ScrollToField(f.focusedField() + 1)
		return f.Form.NextField(), IndexFormNoAction
	case f.keys.Match(keyPress, "form.field_prev", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		f.ScrollToField(f.focusedField() - 1)
		return f.Form.PrevField(), IndexFormNoAction
	}
	return nil, IndexFormNoAction
}

func (f *IndexForm) updateHuh(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, IndexFormAction) {
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.RouteToBar(keyPress, f.focusedField() >= 2, f.blur) {
		return nil, IndexFormNoAction
	}
	focused := f.focusedField()
	model, command := f.Form.Update(message)
	f.Form = model.(*huh.Form)
	if f.Form.State == huh.StateCompleted {
		f.rebuildForm()
		f.ScrollToField(focused)
		return f.Focus(), IndexFormNoAction
	}
	f.ScrollToField(f.focusedField())
	return command, IndexFormNoAction
}

func (f *IndexForm) beginConfirmation(save, delete bool) {
	f.ConfirmationSave, f.ConfirmationDelete = save, delete
	title := "Discard index changes?"
	if save {
		title = "Save index changes?"
	}
	if delete {
		title = "Delete index?"
	}
	f.Confirmation = uikit.YesNoConfirmation(title, "", "confirm")
}

func (f *IndexForm) rebuildForm() {
	f.Form = uikit.NewForm(huh.NewGroup(
		uikit.NewEditableInput(huh.NewInput().Key("name").Title("Name*").Value(&f.Values.Name).Validate(f.validateName), &f.Values.Name),
		uikit.NewEditableInput(huh.NewInput().Key("columns").Title("Columns*").Value(&f.Values.Columns).Validate(requiredIndexColumns), &f.Values.Columns),
		huh.NewSelect[string]().Key("kind").Title("Kind").Options(
			huh.NewOption(IndexKindNormal, IndexKindNormal),
			huh.NewOption(IndexKindUnique, IndexKindUnique),
			huh.NewOption(IndexKindPrimary, IndexKindPrimary),
		).Value(&f.Values.Kind),
	)).WithShowHelp(f.Width >= 40).WithWidth(max(f.Width, 1))
}

func (f IndexForm) validateName(value string) error {
	if f.Values.Kind == IndexKindPrimary {
		return nil
	}
	return requiredIndexName(value)
}

func requiredIndexName(value string) error {
	if strings.TrimSpace(value) == "" {
		return sharedsql.ValidateIndexChange(sharedsql.IndexChange{Name: value, Columns: []string{"column"}})
	}
	return nil
}

func requiredIndexColumns(value string) error {
	columns := strings.Split(value, ",")
	return sharedsql.ValidateIndexChange(sharedsql.IndexChange{Name: "index", Columns: columns})
}

func (f *IndexForm) showValidationError() {
	f.rebuildForm()
	if f.Values.Kind != IndexKindPrimary && requiredIndexName(f.Values.Name) != nil {
		_ = f.Form.GetFocusedField().Blur()
		return
	}
	_ = f.Form.NextField()
	_ = f.Form.GetFocusedField().Blur()
}

func (f IndexForm) focusedField() int {
	if f.Form == nil {
		return 0
	}
	switch f.Form.GetFocusedField().GetKey() {
	case "name":
		return 0
	case "columns":
		return 1
	default:
		return 2
	}
}

// FocusField moves the field cursor to the field at index. The loop bounds
// guard against Huh navigation skipping fields.
func (f *IndexForm) FocusField(field int) tea.Cmd {
	field = min(max(field, 0), 2)
	for range 3 {
		if f.focusedField() >= field {
			break
		}
		_ = f.Form.NextField()
	}
	for range 3 {
		if f.focusedField() <= field {
			break
		}
		_ = f.Form.PrevField()
	}
	f.ScrollToField(field)
	return f.Focus()
}

func (f *IndexForm) blur() {
	if f.Form != nil {
		_ = f.Form.GetFocusedField().Blur()
	}
}

// Focus focuses the focused field.
func (f *IndexForm) Focus() tea.Cmd {
	if f.Form == nil {
		return nil
	}
	return f.Form.GetFocusedField().Focus()
}

// FieldTitles lists the rendered titles of every index form field in render
// order.
func (f IndexForm) FieldTitles() []string {
	return []string{"Name*", "Columns*", "Kind"}
}

// ScrollToField keeps the rendered block of the field at index visible by
// moving the viewport offset to its title line.
func (f *IndexForm) ScrollToField(field int) {
	if offset, ok := scrollToFieldTitle(f.Form.View(), f.FieldTitles(), field); ok {
		f.ScrollOffset = offset
	}
}

func (f *IndexForm) scrollToField(field int) {
	f.ScrollToField(field)
}

// HasChanges reports whether the form differs from the values it was opened
// with.
func (f IndexForm) HasChanges() bool {
	if f.Values == nil {
		return false
	}
	return *f.Values != f.baseline
}

// Change builds the persisted index change, validating it.
func (f IndexForm) Change() (sharedsql.IndexChange, error) {
	if f.Values.Kind != IndexKindPrimary {
		if err := requiredIndexName(f.Values.Name); err != nil {
			return sharedsql.IndexChange{}, err
		}
	}
	if err := requiredIndexColumns(f.Values.Columns); err != nil {
		return sharedsql.IndexChange{}, err
	}
	columns := strings.Split(f.Values.Columns, ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	change := sharedsql.IndexChange{Name: strings.TrimSpace(f.Values.Name), Columns: columns}
	switch f.Values.Kind {
	case IndexKindUnique:
		change.Unique = true
	case IndexKindPrimary:
		change.Name, change.PrimaryKey = "PRIMARY", true
	}
	if err := sharedsql.ValidateIndexChange(change); err != nil {
		return sharedsql.IndexChange{}, err
	}
	return change, nil
}

// SetWidth refits the form to the given content width.
func (f *IndexForm) SetWidth(width int) {
	f.Width = max(width, 1)
	if f.Form != nil {
		f.Form.WithWidth(f.Width).WithShowHelp(f.Width >= 40)
	}
}

// View renders the index form body.
func (f IndexForm) View() string {
	if f.Saving {
		return uikit.StatusStyle.Render("saving index changes")
	}
	if f.Form == nil {
		return ""
	}
	return f.Form.View()
}

// ForeignKeyFormAction is the outcome of one foreign-key form update the
// root acts on.
type ForeignKeyFormAction uint8

const (
	ForeignKeyFormNoAction ForeignKeyFormAction = iota
	ForeignKeyFormSave
	ForeignKeyFormDiscard
	ForeignKeyFormDelete
)

// ForeignKeyFormValues is the editable foreign-key state of the form.
type ForeignKeyFormValues struct {
	Columns, ReferenceTable, ReferenceColumns string
	OnDelete, OnUpdate                        string
}

// ForeignKeyForm is the foreign-keys tab's create/edit form: the huh form,
// its values, the save/discard confirmation, and the geometry. The root
// owns the confirmation overlay and the DDL execution.
type ForeignKeyForm struct {
	Form                                 *huh.Form
	Confirmation                         *uikit.ConfirmationDialog
	Values                               *ForeignKeyFormValues
	baseline                             ForeignKeyFormValues
	Previous                             string
	Width, Height                        int
	ScrollOffset                         int
	Saving                               bool
	ConfirmationSave, ConfirmationDelete bool
	keys                                 uikit.KeyMatcher
}

func NewForeignKeyForm(foreignKey *sharedsql.ForeignKeyInfo) ForeignKeyForm {
	form := ForeignKeyForm{Values: &ForeignKeyFormValues{OnDelete: "NO ACTION", OnUpdate: "NO ACTION"}, keys: noopKeys{}}
	if foreignKey != nil {
		form.Previous = foreignKey.ID
		form.Values.Columns = strings.Join(foreignKey.Columns, ", ")
		form.Values.ReferenceTable = foreignKey.ReferenceTable
		form.Values.ReferenceColumns = strings.Join(foreignKey.ReferenceColumns, ", ")
		form.Values.OnDelete = foreignKey.OnDelete
		form.Values.OnUpdate = foreignKey.OnUpdate
	}
	form.baseline = *form.Values
	form.rebuildForm()
	return form
}

// Active reports whether the form holds fields.
func (f ForeignKeyForm) Active() bool { return f.Values != nil }

// Confirming reports whether the save/discard confirmation is open.
func (f ForeignKeyForm) Confirming() bool { return f.Confirmation != nil }

// Close drops the form.
func (f *ForeignKeyForm) Close() { *f = ForeignKeyForm{} }

// SetKeys installs the session keybindings for the form's bindings.
func (f *ForeignKeyForm) SetKeys(keys uikit.KeyMatcher) { f.keys = keys }

// Update routes one message through the foreign-key form.
func (f *ForeignKeyForm) Update(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, ForeignKeyFormAction) {
	if f.Saving {
		return nil, ForeignKeyFormNoAction
	}
	if f.Confirmation != nil {
		completed, action := f.Confirmation.Update(message, f.Width, f.Height)
		if !completed {
			return nil, ForeignKeyFormNoAction
		}
		f.Confirmation = nil
		controller.Mode = uikit.FormModeNormal
		if action != "confirm" {
			return nil, ForeignKeyFormNoAction
		}
		if f.ConfirmationDelete {
			return nil, ForeignKeyFormDelete
		}
		if f.ConfirmationSave {
			return nil, ForeignKeyFormSave
		}
		return nil, ForeignKeyFormDiscard
	}
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.ButtonsFocused {
		if route, replayed, cmd := controller.RouteFormButtons(keyPress, f.keys, func() tea.Cmd { return f.FocusField(4) }); route != uikit.FormButtonContinue {
			if route == uikit.FormButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, ForeignKeyFormNoAction
			}
		}
	}
	if !replay {
		if route := controller.RouteHuh(message, f.blur); route != uikit.FormRouteParent {
			if route == uikit.FormRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, ForeignKeyFormNoAction
		}
	}
	if !ok {
		return nil, ForeignKeyFormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.keys.Match(keyPress, "form.edit", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		return controller.BeginHuh(f.Focus()), ForeignKeyFormNoAction
	case f.keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if _, err := f.Change(); err != nil {
			f.showValidationError()
			return nil, ForeignKeyFormNoAction
		}
		f.beginConfirmation(true, false)
		controller.BeginConfirm()
		return nil, ForeignKeyFormNoAction
	case f.keys.Match(keyPress, "form.discard", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if !f.HasChanges() {
			controller.Mode = uikit.FormModeNormal
			controller.ButtonsFocused = false
			return nil, ForeignKeyFormDiscard
		}
		f.beginConfirmation(false, false)
		controller.BeginConfirm()
		return nil, ForeignKeyFormNoAction
	case f.keys.Match(keyPress, "form.delete", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.Previous != "" {
			f.beginConfirmation(false, true)
			controller.BeginConfirm()
			return nil, ForeignKeyFormNoAction
		}
	case f.keys.Match(keyPress, "form.field_next", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		if f.focusedField() >= 4 {
			controller.FocusButtons()
			f.blur()
			return nil, ForeignKeyFormNoAction
		}
		f.ScrollToField(f.focusedField() + 1)
		return f.Form.NextField(), ForeignKeyFormNoAction
	case f.keys.Match(keyPress, "form.field_prev", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		f.ScrollToField(f.focusedField() - 1)
		return f.Form.PrevField(), ForeignKeyFormNoAction
	}
	return nil, ForeignKeyFormNoAction
}

func (f *ForeignKeyForm) updateHuh(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, ForeignKeyFormAction) {
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.RouteToBar(keyPress, f.focusedField() >= 4, f.blur) {
		return nil, ForeignKeyFormNoAction
	}
	focused := f.focusedField()
	model, command := f.Form.Update(message)
	f.Form = model.(*huh.Form)
	if f.Form.State == huh.StateCompleted {
		f.rebuildForm()
		f.ScrollToField(focused)
		return f.Focus(), ForeignKeyFormNoAction
	}
	f.ScrollToField(f.focusedField())
	return command, ForeignKeyFormNoAction
}

func (f *ForeignKeyForm) beginConfirmation(save, delete bool) {
	f.ConfirmationSave, f.ConfirmationDelete = save, delete
	title := "Discard foreign-key changes?"
	if save {
		title = "Save foreign-key changes?"
	}
	if delete {
		title = "Delete foreign key?"
	}
	f.Confirmation = uikit.YesNoConfirmation(title, "", "confirm")
}

func (f *ForeignKeyForm) rebuildForm() {
	actions := []huh.Option[string]{
		huh.NewOption("NO ACTION", "NO ACTION"),
		huh.NewOption("RESTRICT", "RESTRICT"),
		huh.NewOption("SET NULL", "SET NULL"),
		huh.NewOption("SET DEFAULT", "SET DEFAULT"),
		huh.NewOption("CASCADE", "CASCADE"),
	}
	f.Form = uikit.NewForm(huh.NewGroup(
		uikit.NewEditableInput(huh.NewInput().Key("columns").Title("Columns*").Value(&f.Values.Columns).Validate(requiredForeignKeyColumns), &f.Values.Columns),
		uikit.NewEditableInput(huh.NewInput().Key("reference-table").Title("Reference table*").Value(&f.Values.ReferenceTable).Validate(requiredReferenceTable), &f.Values.ReferenceTable),
		uikit.NewEditableInput(huh.NewInput().Key("reference-columns").Title("Reference columns*").Value(&f.Values.ReferenceColumns).Validate(f.validateReferenceColumns), &f.Values.ReferenceColumns),
		huh.NewSelect[string]().Key("on-delete").Title("On delete").Options(actions...).Value(&f.Values.OnDelete),
		huh.NewSelect[string]().Key("on-update").Title("On update").Options(actions...).Value(&f.Values.OnUpdate),
	)).WithShowHelp(f.Width >= 40).WithWidth(max(f.Width, 1))
}

func requiredForeignKeyColumns(value string) error {
	columns := splitForeignKeyColumns(value)
	referenceColumns := make([]string, len(columns))
	for index := range referenceColumns {
		referenceColumns[index] = fmt.Sprintf("reference-%d", index)
	}
	return sharedsql.ValidateForeignKeyChange(sharedsql.ForeignKeyChange{
		Columns: columns, ReferenceTable: "reference", ReferenceColumns: referenceColumns,
		OnDelete: "NO ACTION", OnUpdate: "NO ACTION",
	})
}

func requiredReferenceTable(value string) error {
	return sharedsql.ValidateForeignKeyChange(sharedsql.ForeignKeyChange{
		Columns: []string{"column"}, ReferenceTable: value, ReferenceColumns: []string{"reference"},
		OnDelete: "NO ACTION", OnUpdate: "NO ACTION",
	})
}

func requiredReferenceColumns(value string) error {
	referenceColumns := splitForeignKeyColumns(value)
	columns := make([]string, len(referenceColumns))
	for index := range columns {
		columns[index] = fmt.Sprintf("column-%d", index)
	}
	return sharedsql.ValidateForeignKeyChange(sharedsql.ForeignKeyChange{
		Columns: columns, ReferenceTable: "reference", ReferenceColumns: referenceColumns,
		OnDelete: "NO ACTION", OnUpdate: "NO ACTION",
	})
}

func (f ForeignKeyForm) validateReferenceColumns(value string) error {
	if err := requiredReferenceColumns(value); err != nil {
		return err
	}
	_, err := f.Change()
	return err
}

func (f *ForeignKeyForm) showValidationError() {
	f.rebuildForm()
	if requiredForeignKeyColumns(f.Values.Columns) != nil {
		_ = f.Form.GetFocusedField().Blur()
		return
	}
	_ = f.Form.NextField()
	if requiredReferenceTable(f.Values.ReferenceTable) != nil {
		_ = f.Form.GetFocusedField().Blur()
		return
	}
	_ = f.Form.NextField()
	_ = f.Form.GetFocusedField().Blur()
}

func (f ForeignKeyForm) focusedField() int {
	if f.Form == nil {
		return 0
	}
	switch f.Form.GetFocusedField().GetKey() {
	case "columns":
		return 0
	case "reference-table":
		return 1
	case "reference-columns":
		return 2
	case "on-delete":
		return 3
	default:
		return 4
	}
}

// FocusField moves the field cursor to the field at index. The loop bounds
// guard against Huh navigation skipping fields.
func (f *ForeignKeyForm) FocusField(field int) tea.Cmd {
	field = min(max(field, 0), 4)
	for range 5 {
		if f.focusedField() >= field {
			break
		}
		_ = f.Form.NextField()
	}
	for range 5 {
		if f.focusedField() <= field {
			break
		}
		_ = f.Form.PrevField()
	}
	f.ScrollToField(field)
	return f.Focus()
}

// FieldTitles lists the rendered titles of every foreign-key form field in
// render order.
func (f ForeignKeyForm) FieldTitles() []string {
	return []string{"Columns*", "Reference table*", "Reference columns*", "On delete", "On update"}
}

// ScrollToField keeps the rendered block of the field at index visible by
// moving the viewport offset to its title line.
func (f *ForeignKeyForm) ScrollToField(field int) {
	if offset, ok := scrollToFieldTitle(f.Form.View(), f.FieldTitles(), field); ok {
		f.ScrollOffset = offset
	}
}

func (f *ForeignKeyForm) scrollToField(field int) {
	f.ScrollToField(field)
}

func (f *ForeignKeyForm) blur() {
	if f.Form != nil {
		_ = f.Form.GetFocusedField().Blur()
	}
}

// Focus focuses the focused field.
func (f *ForeignKeyForm) Focus() tea.Cmd {
	if f.Form == nil {
		return nil
	}
	return f.Form.GetFocusedField().Focus()
}

// HasChanges reports whether the form differs from the values it was opened
// with.
func (f ForeignKeyForm) HasChanges() bool {
	if f.Values == nil {
		return false
	}
	return *f.Values != f.baseline
}

// Change builds the persisted foreign-key change, validating it.
func (f ForeignKeyForm) Change() (sharedsql.ForeignKeyChange, error) {
	change := sharedsql.ForeignKeyChange{
		Columns:          splitForeignKeyColumns(f.Values.Columns),
		ReferenceTable:   strings.TrimSpace(f.Values.ReferenceTable),
		ReferenceColumns: splitForeignKeyColumns(f.Values.ReferenceColumns),
		OnDelete:         strings.ToUpper(strings.TrimSpace(f.Values.OnDelete)),
		OnUpdate:         strings.ToUpper(strings.TrimSpace(f.Values.OnUpdate)),
	}
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return sharedsql.ForeignKeyChange{}, err
	}
	return change, nil
}

func splitForeignKeyColumns(value string) []string {
	columns := strings.Split(value, ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	return columns
}

// SetWidth refits the form to the given content width.
func (f *ForeignKeyForm) SetWidth(width int) {
	f.Width = max(width, 1)
	if f.Form != nil {
		f.Form.WithWidth(f.Width).WithShowHelp(f.Width >= 40)
	}
}

// View renders the foreign-key form body.
func (f ForeignKeyForm) View() string {
	if f.Saving {
		return uikit.StatusStyle.Render("saving foreign-key changes")
	}
	if f.Form == nil {
		return ""
	}
	return f.Form.View()
}

// TableFormAction is the outcome of one table popup update the root acts
// on: closing it, opening the save confirmation, or executing the DDL.
type TableFormAction uint8

const (
	TableFormNoAction TableFormAction = iota
	TableFormClose
	TableFormSave
	TableFormExecute
)

// TableFormObjectKind selects which object the table popup creates or
// renames.
type TableFormObjectKind uint8

const (
	TableFormTable TableFormObjectKind = iota
	TableFormDatabase
	TableFormSchema
)

// TableForm is the create/rename popup for tables, databases, and schemas.
// Rename mode carries the original name so an unchanged save closes without
// any SQL. The typed name is kept verbatim for quoted SQL; it is only
// trimmed to test emptiness and equality. The root builds the DDL statement
// from the exported fields and runs it through the query lifecycle.
type TableForm struct {
	Form          *huh.Form
	Confirmation  *uikit.ConfirmationDialog
	Name          string
	nameValue     *string
	OriginalName  string
	Database      string
	Table         string // qualified old name for table renames
	ObjectKind    TableFormObjectKind
	Width, Height int
	keys          uikit.KeyMatcher
}

func NewTableForm(database, table string) TableForm {
	name := table
	original := table
	// PostgreSQL sidebar tables carry schema.table; the popup edits the
	// bare name while the qualified name stays the ALTER target.
	if _, bare, found := strings.Cut(table, "."); found {
		name, original = bare, bare
	}
	form := TableForm{
		OriginalName: original,
		Database:     database,
		Table:        table,
		Name:         name,
		keys:         noopKeys{},
		nameValue:    &name,
	}
	form.rebuildForm()
	return form
}

func NewDatabaseForm(originalName string) TableForm {
	name := originalName
	form := TableForm{
		OriginalName: originalName,
		Name:         originalName,
		keys:         noopKeys{},
		nameValue:    &name,
		ObjectKind:   TableFormDatabase,
	}
	form.rebuildForm()
	return form
}

func NewSchemaForm(originalName string) TableForm {
	name := originalName
	form := TableForm{
		OriginalName: originalName,
		Name:         originalName,
		keys:         noopKeys{},
		nameValue:    &name,
		ObjectKind:   TableFormSchema,
	}
	form.rebuildForm()
	return form
}

// Active reports whether the popup is open.
func (f TableForm) Active() bool { return f.Form != nil }

// Confirming reports whether the save confirmation is open.
func (f TableForm) Confirming() bool { return f.Confirmation != nil }

// SetKeys installs the session keybindings for the popup's bindings.
func (f *TableForm) SetKeys(keys uikit.KeyMatcher) { f.keys = keys }

// Update routes one message through the table popup. Save/Discard flow
// through the confirmation like the other forms; the root executes the
// confirmed DDL.
func (f *TableForm) Update(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, TableFormAction) {
	if f.Confirmation != nil {
		completed, action := f.Confirmation.Update(message, f.Width, f.Height)
		if !completed {
			return nil, TableFormNoAction
		}
		f.Confirmation = nil
		controller.Mode = uikit.FormModeNormal
		if action != "confirm" {
			return nil, TableFormNoAction
		}
		return nil, TableFormExecute
	}
	if keyPress, ok := message.(tea.KeyPressMsg); ok &&
		(keyPress.Key().Code == tea.KeyEnter || f.keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal})) &&
		!controller.ButtonsFocused {
		return f.save(controller)
	}
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.ButtonsFocused {
		if route, replayed, cmd := controller.RouteFormButtons(keyPress, f.keys, func() tea.Cmd { return f.Focus() }); route != uikit.FormButtonContinue {
			if route == uikit.FormButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, TableFormNoAction
			}
		}
	}
	if !replay {
		if route := controller.RouteHuh(message, f.blur); route != uikit.FormRouteParent {
			if route == uikit.FormRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, TableFormNoAction
		}
	}
	if !ok {
		return nil, TableFormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.keys.Match(keyPress, "form.edit", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		return controller.BeginHuh(f.Focus()), TableFormNoAction
	case f.keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		return f.save(controller)
	case f.keys.Match(keyPress, "form.discard", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}):
		f.Close()
		return nil, TableFormClose
	}
	return nil, TableFormNoAction
}

func (f *TableForm) updateHuh(message tea.Msg, controller *uikit.FormModeController) (tea.Cmd, TableFormAction) {
	// The name field is the form's only field, so Tab always lands on the bar.
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.RouteToBar(keyPress, true, f.blur) {
		return nil, TableFormNoAction
	}
	model, command := f.Form.Update(message)
	f.Form = model.(*huh.Form)
	if input, ok := f.Form.GetFocusedField().(*uikit.EditableInput); ok {
		f.Name = input.ExternalEditorValue()
	}
	if f.Form.State == huh.StateCompleted {
		f.rebuildForm()
		return f.Focus(), TableFormNoAction
	}
	return command, TableFormNoAction
}

func (f *TableForm) save(controller *uikit.FormModeController) (tea.Cmd, TableFormAction) {
	_, required, createTitle, editTitle := f.labels()
	if err := required(f.Name); err != nil {
		if f.nameValue != nil {
			*f.nameValue = f.Name
		}
		model, command := f.Form.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		f.Form = model.(*huh.Form)
		return command, TableFormNoAction
	}
	if f.OriginalName != "" && f.Name == f.OriginalName {
		f.Close()
		return nil, TableFormClose
	}
	title := createTitle
	if f.OriginalName != "" {
		title = editTitle
	}
	f.Confirmation = uikit.YesNoConfirmation(title, "", "confirm")
	controller.BeginConfirm()
	return nil, TableFormSave
}

// View renders the table popup body.
func (f TableForm) View() string {
	if f.Form == nil {
		return ""
	}
	return f.Form.View()
}

// Close drops the popup.
func (f *TableForm) Close() { f.Form = nil }

// Focus focuses the focused field.
func (f *TableForm) Focus() tea.Cmd {
	if f.Form == nil {
		return nil
	}
	return f.Form.GetFocusedField().Focus()
}

func (f *TableForm) blur() {
	if f.Form != nil {
		_ = f.Form.GetFocusedField().Blur()
	}
}

// SetWidth refits the popup to the given content width.
func (f *TableForm) SetWidth(width int) {
	f.Width = max(width, 1)
	if f.Form != nil {
		f.Form.WithWidth(f.Width).WithShowHelp(f.Width >= 40)
	}
}

// SetHeight caps the popup to the given viewport height.
func (f *TableForm) SetHeight(height int) {
	f.Height = max(height, 1)
}

func (f *TableForm) rebuildForm() {
	if f.nameValue == nil {
		name := f.Name
		f.nameValue = &name
	} else {
		*f.nameValue = f.Name
	}
	title, required, _, _ := f.labels()
	f.Form = uikit.NewForm(huh.NewGroup(
		uikit.NewEditableInput(huh.NewInput().Key("name").Title(title).Value(f.nameValue).Validate(required), f.nameValue),
	)).WithShowHelp(f.Width >= 40).WithWidth(max(f.Width, 1))
}

func (f TableForm) labels() (string, func(string) error, string, string) {
	switch f.ObjectKind {
	case TableFormDatabase:
		return "Database name", requiredDatabaseName, "Create database?", "Edit database?"
	case TableFormSchema:
		return "Schema name", requiredSchemaName, "Create schema?", "Edit schema?"
	default:
		return "Table name", requiredTableName, "Create table?", "Edit table?"
	}
}

func requiredTableName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("table name is required")
	}
	return nil
}

func requiredDatabaseName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("database name is required")
	}
	return nil
}

func requiredSchemaName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("schema name is required")
	}
	return nil
}

// scrollToFieldTitle returns the view line of the rendered title of the
// field at index (titles in render order), or (0, false) when the layout
// cannot be determined.
func scrollToFieldTitle(view string, titles []string, field int) (int, bool) {
	if field < 0 || field >= len(titles) {
		return 0, false
	}
	line := 0
	for _, content := range strings.Split(view, "\n") {
		if strings.Contains(content, titles[field]) {
			return line, true
		}
		line++
	}
	return 0, false
}
