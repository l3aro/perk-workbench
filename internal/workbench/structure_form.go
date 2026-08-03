package workbench

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type columnFormAction uint8

const (
	columnFormNoAction columnFormAction = iota
	columnFormSave
	columnFormDiscard
	columnFormDelete
)

type columnForm struct {
	form                            *huh.Form
	confirmation                    *confirmationDialog
	values                          *columnFormValues
	previousName, originalType      string
	originalAttributes              string
	typeOptions                     []sharedsql.ColumnType
	validationError                 string
	width, height, scrollOffset     int
	formType                        string
	hadDefault, typeChanged, saving bool
	isNew                           bool
	confirmationSave                bool
	confirmationDelete              bool
	keybindings                     Keybindings
}

type columnFormValues struct {
	name, typeName, defaultValue, attributes string
	parameters                               []string
	nullable                                 bool
}

func newColumnForm(column sharedsql.ColumnInfo, typeOptions []sharedsql.ColumnType) columnForm {
	form := columnForm{
		previousName:       column.Name,
		originalType:       column.Type,
		hadDefault:         column.DefaultValue != nil,
		originalAttributes: column.Attributes,
		typeOptions:        typeOptions,
		values:             &columnFormValues{name: column.Name, nullable: column.Nullable, attributes: column.Attributes},
		keybindings:        DefaultKeybindings(),
	}
	if index, values, ok := sharedsql.MatchColumnType(typeOptions, column.Type); ok {
		form.selectType(index, values)
	} else {
		if strings.TrimSpace(column.Type) != "" {
			form.typeOptions = append([]sharedsql.ColumnType{{Name: column.Type}}, typeOptions...)
		}
		form.selectType(0, nil)
	}
	if column.DefaultValue != nil {
		form.values.defaultValue = *column.DefaultValue
	}
	form.rebuildForm()
	return form
}

func newEmptyColumnForm(typeOptions []sharedsql.ColumnType) columnForm {
	form := columnForm{
		values:      &columnFormValues{nullable: true},
		typeOptions: typeOptions,
		isNew:       true,
		keybindings: DefaultKeybindings(),
	}
	form.selectType(0, nil)
	form.rebuildForm()
	return form
}

func (m *Model) openColumnForm() tea.Cmd {
	column := m.selectedColumn()
	if column == nil {
		m.Status = "select a column"
		return nil
	}
	m.columnForm = newColumnForm(*column, sharedsql.ColumnTypes(m.databaseInfo))
	m.columnForm.keybindings = m.keybindings
	m.columnForm.setWidth(m.tableViewportWidth)
	m.columnForm.setHeight(m.formViewportHeight())
	return m.columnForm.form.Init()
}

func (m *Model) openNewColumnForm() tea.Cmd {
	m.columnForm = newEmptyColumnForm(sharedsql.ColumnTypes(m.databaseInfo))
	m.columnForm.keybindings = m.keybindings
	m.columnForm.setWidth(m.tableViewportWidth)
	m.columnForm.setHeight(m.formViewportHeight())
	return m.columnForm.form.Init()
}

func (f columnForm) active() bool { return f.form != nil }

func (f columnForm) confirming() bool { return f.confirmation != nil }

func (f *columnForm) Update(message tea.Msg, controller *formModeController) (tea.Cmd, columnFormAction) {
	if f.saving {
		return nil, columnFormNoAction
	}
	if f.confirmation != nil {
		completed, action := f.confirmation.Update(message, f.width, f.height)
		if !completed {
			return nil, columnFormNoAction
		}
		f.confirmation = nil
		controller.mode = formModeNormal
		if action != "confirm" {
			return nil, columnFormNoAction
		}
		if f.confirmationSave {
			return nil, columnFormSave
		}
		if f.confirmationDelete {
			return nil, columnFormDelete
		}
		return nil, columnFormDiscard
	}
	if route := controller.routeHuh(message, f.blur); route != formRouteParent {
		if route == formRouteHuh {
			return f.updateHuh(message, controller)
		}
		return nil, columnFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil, columnFormNoAction
	}
	switch {
	case isInsertModeKey(keyPress), f.keybindings.Match(keyPress, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}):
		return controller.beginHuh(f.focus()), columnFormNoAction
	case f.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}):
		if f.isNew {
			if _, err := f.columnDef(); err != nil {
				f.validationError = err.Error()
				return nil, columnFormNoAction
			}
		} else if _, err := f.change(); err != nil {
			f.validationError = err.Error()
			return nil, columnFormNoAction
		}
		f.beginConfirmation(true, false)
		controller.beginConfirm()
		return nil, columnFormNoAction
	case f.keybindings.Match(keyPress, "form.discard", []scope{scopeForm, scopeView, scopeGlobal}):
		f.beginConfirmation(false, false)
		controller.beginConfirm()
		return nil, columnFormNoAction
	case f.keybindings.Match(keyPress, "form.delete", []scope{scopeForm, scopeView, scopeGlobal}):
		if f.previousName != "" {
			f.beginConfirmation(false, true)
			controller.beginConfirm()
		}
		return nil, columnFormNoAction
	case f.keybindings.Match(keyPress, "form.field_next", []scope{scopeForm, scopeView, scopeGlobal}):
		return f.nextField(), columnFormNoAction
	case f.keybindings.Match(keyPress, "form.field_prev", []scope{scopeForm, scopeView, scopeGlobal}):
		return f.previousField(), columnFormNoAction
	}
	return nil, columnFormNoAction
}

func (f *columnForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, columnFormAction) {
	focused := f.focusedField()
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "enter" && f.values.typeName != f.formType {
		f.typeChanged = true
		f.selectType(f.typeIndex(), nil)
		f.rebuildForm()
		f.scrollToField(focused)
		return f.focusField(focused), columnFormNoAction
	}
	if f.form.State == huh.StateCompleted {
		f.rebuildForm()
		f.scrollToField(focused)
		return f.focusField(focused), columnFormNoAction
	}
	f.scrollToField(f.focusedField())
	return command, columnFormNoAction
}

func (f *columnForm) beginConfirmation(save, delete bool) {
	f.confirmationSave = save
	f.confirmationDelete = delete
	title := "Discard column changes?"
	if save && f.isNew {
		title = "Add column?"
	} else if save {
		title = "Save column changes?"
	} else if delete {
		title = "Delete column?"
	}
	f.confirmation = yesNoConfirmation(title, "", "confirm")
}

func (f *columnForm) blur() {
	if f.form != nil {
		_ = f.form.GetFocusedField().Blur()
	}
}

func (f *columnForm) focus() tea.Cmd {
	if f.form == nil {
		return nil
	}
	return f.form.GetFocusedField().Focus()
}

func (f *columnForm) nextField() tea.Cmd {
	field := f.focusedField()
	if field >= f.fieldCount()-1 {
		return nil
	}
	_ = f.form.GetFocusedField().Blur()
	f.scrollToField(field + 1)
	return f.form.NextField()
}

func (f *columnForm) previousField() tea.Cmd {
	field := f.focusedField()
	if field == 0 {
		return nil
	}
	_ = f.form.GetFocusedField().Blur()
	f.scrollToField(field - 1)
	return f.form.PrevField()
}

func (f columnForm) fieldCount() int { return len(f.values.parameters) + 5 }

func (f columnForm) focusedField() int {
	if f.form == nil {
		return 0
	}
	key := f.form.GetFocusedField().GetKey()
	switch {
	case key == "name":
		return 0
	case key == "type":
		return 1
	case strings.HasPrefix(key, "parameter-"):
		index, err := strconv.Atoi(strings.TrimPrefix(key, "parameter-"))
		if err == nil {
			return min(index+2, f.fieldCount()-1)
		}
	case key == "nullable":
		return len(f.values.parameters) + 2
	case key == "default":
		return len(f.values.parameters) + 3
	case key == "attributes":
		return len(f.values.parameters) + 4
	}
	return 0
}

func (f *columnForm) focusField(field int) tea.Cmd {
	field = min(max(field, 0), f.fieldCount()-1)
	for range f.fieldCount() {
		if f.focusedField() >= field {
			break
		}
		_ = f.form.NextField()
	}
	for range f.fieldCount() {
		if f.focusedField() <= field {
			break
		}
		_ = f.form.PrevField()
	}
	return f.focus()
}

// fieldTitles lists the rendered titles of every column form field in render
// order; parameter titles come from the selected type.
func (f columnForm) fieldTitles() []string {
	titles := []string{"Name*", "Type*"}
	for _, parameter := range f.typeOptions[f.typeIndex()].Parameters {
		titles = append(titles, parameter.Name)
	}
	return append(titles, "Nullable", "Default", "Attributes")
}

func (f *columnForm) scrollToField(field int) {
	f.scrollOffset = max(field*2, 0)
}

func (f columnForm) change() (sharedsql.ColumnChange, error) {
	typeDeclaration, err := f.typeDeclaration()
	if err != nil {
		return sharedsql.ColumnChange{}, err
	}
	change := sharedsql.ColumnChange{PreviousName: f.previousName, Name: f.values.name, Type: typeDeclaration, Nullable: f.values.nullable}
	if value := strings.TrimSpace(f.values.defaultValue); value != "" {
		change.DefaultValue = &value
	}
	if value := strings.TrimSpace(f.values.attributes); value != f.originalAttributes {
		change.Attributes = &value
	}
	if err := sharedsql.ValidateColumnChange(change); err != nil {
		return sharedsql.ColumnChange{}, err
	}
	return change, nil
}

func (f columnForm) columnDef() (sharedsql.ColumnDef, error) {
	typeDeclaration, err := f.typeDeclaration()
	if err != nil {
		return sharedsql.ColumnDef{}, err
	}
	def := sharedsql.ColumnDef{Name: f.values.name, Type: typeDeclaration, Nullable: f.values.nullable}
	if value := strings.TrimSpace(f.values.defaultValue); value != "" {
		def.DefaultValue = &value
	}
	if value := strings.TrimSpace(f.values.attributes); value != "" {
		def.Attributes = &value
	}
	if err := sharedsql.ValidateColumnDef(def); err != nil {
		return sharedsql.ColumnDef{}, err
	}
	return def, nil
}

func (f *columnForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f *columnForm) setHeight(height int) {
	f.height = max(height, 1)
	if f.form != nil {
		f.form.WithHeight(f.height)
	}
}

func (f *columnForm) rebuildForm() {
	f.formType = f.values.typeName
	fields := []huh.Field{
		newEditableInput(huh.NewInput().Key("name").Title("Name*").Value(&f.values.name).Validate(requiredColumnName), &f.values.name),
		huh.NewSelect[string]().Key("type").Title("Type*").Options(f.typeChoices()...).Value(&f.values.typeName).Validate(f.validateType),
	}
	for index, parameter := range f.typeOptions[f.typeIndex()].Parameters {
		index, parameter := index, parameter
		fields = append(fields, newEditableInput(huh.NewInput().Key("parameter-"+strconv.Itoa(index)).Title(parameter.Name).Value(&f.values.parameters[index]).Validate(f.validateParameter(index)), &f.values.parameters[index]))
	}
	fields = append(fields,
		huh.NewConfirm().Key("nullable").Title("Nullable").Affirmative("Yes").Negative("No").Value(&f.values.nullable),
		newEditableInput(huh.NewInput().Key("default").Title("Default").Value(&f.values.defaultValue), &f.values.defaultValue),
		newEditableInput(huh.NewInput().Key("attributes").Title("Attributes").Value(&f.values.attributes), &f.values.attributes),
	)
	f.form = newForm(huh.NewGroup(fields...)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1)).WithHeight(max(f.height, 1))
}

func requiredColumnName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("column name is required")
	}
	return nil
}
