package workbench

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type columnFormAction uint8

const (
	columnFormNoAction columnFormAction = iota
	columnFormSave
	columnFormDiscard
)

type columnForm struct {
	form, confirmation              *huh.Form
	values                          *columnFormValues
	previousName, originalType      string
	typeOptions                     []sharedsql.ColumnType
	validationError                 string
	width, primaryKey               int
	formType                        string
	hadDefault, typeChanged, saving bool
	confirmationSave                bool
}

type columnFormValues struct {
	name, typeName, defaultValue string
	parameters                   []string
	nullable, confirmed          bool
}

func newColumnForm(column sharedsql.ColumnInfo, typeOptions []sharedsql.ColumnType) columnForm {
	form := columnForm{
		previousName: column.Name,
		originalType: column.Type,
		hadDefault:   column.DefaultValue != nil,
		primaryKey:   column.PrimaryKey,
		typeOptions:  typeOptions,
		values:       &columnFormValues{name: column.Name, nullable: column.Nullable},
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

func (m *Model) openColumnForm() tea.Cmd {
	row := m.structure.Cursor()
	if row < 0 || row >= len(m.structureColumns) {
		m.Status = "select a column"
		return nil
	}
	m.columnForm = newColumnForm(m.structureColumns[row], sharedsql.ColumnTypes(m.databaseInfo))
	m.columnForm.setWidth(m.tableViewportWidth)
	return m.columnForm.form.Init()
}

func (f columnForm) active() bool { return f.previousName != "" }

func (f columnForm) confirming() bool { return f.confirmation != nil }

func (f *columnForm) Update(message tea.Msg, controller *formModeController) (tea.Cmd, columnFormAction) {
	if f.saving {
		return nil, columnFormNoAction
	}
	if route := controller.routeHuh(message, f.blur); route != formRouteParent {
		if route == formRouteConsumed && f.confirmation != nil && controller.mode == formModeNormal {
			f.confirmation = nil
		}
		if route == formRouteHuh {
			return f.updateHuh(message, controller)
		}
		return nil, columnFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil, columnFormNoAction
	}
	switch keyPress.String() {
	case "i", "enter":
		return controller.beginHuh(f.focus()), columnFormNoAction
	case "ctrl+enter", "ctrl+s", "f5":
		if _, err := f.change(); err != nil {
			f.validationError = err.Error()
			return nil, columnFormNoAction
		}
		f.beginConfirmation(true)
		controller.beginConfirm()
		return f.confirmation.Init(), columnFormNoAction
	case "esc", "escape":
		f.beginConfirmation(false)
		controller.beginConfirm()
		return f.confirmation.Init(), columnFormNoAction
	}
	return nil, columnFormNoAction
}

func (f *columnForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, columnFormAction) {
	if f.confirmation != nil {
		model, command := f.confirmation.Update(message)
		f.confirmation = model.(*huh.Form)
		if f.confirmation.State != huh.StateCompleted {
			return command, columnFormNoAction
		}
		confirmation, save := f.values.confirmed || f.confirmation.GetBool("confirm"), f.confirmationSave
		f.confirmation = nil
		controller.mode = formModeNormal
		if !confirmation {
			return nil, columnFormNoAction
		}
		if save {
			return nil, columnFormSave
		}
		return nil, columnFormDiscard
	}
	if _, ok := message.(tea.KeyPressMsg); ok {
		f.validationError = ""
	}
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "enter" && f.values.typeName != f.formType {
		f.typeChanged = true
		f.selectType(f.typeIndex(), nil)
		f.rebuildForm()
		return f.focus(), columnFormNoAction
	}
	if f.form.State == huh.StateCompleted {
		f.rebuildForm()
		return f.focus(), columnFormNoAction
	}
	return command, columnFormNoAction
}

func (f *columnForm) beginConfirmation(save bool) {
	f.values.confirmed, f.confirmationSave = false, save
	title := "Discard column changes?"
	if save {
		title = "Save column changes?"
	}
	f.confirmation = huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Key("confirm").Title(title).Affirmative("Yes").Negative("No").Value(&f.values.confirmed),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
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

func (f columnForm) change() (sharedsql.ColumnChange, error) {
	typeDeclaration, err := f.typeDeclaration()
	if err != nil {
		return sharedsql.ColumnChange{}, err
	}
	change := sharedsql.ColumnChange{PreviousName: f.previousName, Name: f.values.name, Type: typeDeclaration, Nullable: f.values.nullable}
	if value := strings.TrimSpace(f.values.defaultValue); value != "" {
		change.DefaultValue = &value
	}
	if err := sharedsql.ValidateColumnChange(change); err != nil {
		return sharedsql.ColumnChange{}, err
	}
	return change, nil
}

func (f *columnForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
	if f.confirmation != nil {
		f.confirmation.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f *columnForm) rebuildForm() {
	f.formType = f.values.typeName
	fields := []huh.Field{
		huh.NewInput().Key("name").Title("Name*").Value(&f.values.name).Validate(requiredColumnName),
		huh.NewSelect[string]().Key("type").Title("Type*").Options(f.typeChoices()...).Value(&f.values.typeName).Validate(f.validateType),
	}
	for index, parameter := range f.typeOptions[f.typeIndex()].Parameters {
		index, parameter := index, parameter
		fields = append(fields, huh.NewInput().Key("parameter").Title(parameter.Name).Value(&f.values.parameters[index]).Validate(f.validateParameter(index)))
	}
	fields = append(fields,
		huh.NewConfirm().Key("nullable").Title("Nullable").Affirmative("Yes").Negative("No").Value(&f.values.nullable),
		huh.NewInput().Key("default").Title("Default").Value(&f.values.defaultValue),
	)
	if f.primaryKey > 0 {
		fields = append(fields, huh.NewNote().Title("Primary key").Description(primaryKeyNote(f.primaryKey)))
	}
	f.form = huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func requiredColumnName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("column name is required")
	}
	return nil
}
