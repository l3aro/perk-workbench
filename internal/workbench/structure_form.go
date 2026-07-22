package workbench

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type columnFormMode uint8

const (
	columnFormNormal columnFormMode = iota
	columnFormInsert
	columnFormSelectType
	columnFormConfirmSave
	columnFormConfirmDiscard
)

type columnFormAction uint8

const (
	columnFormNoAction columnFormAction = iota
	columnFormSave
	columnFormDiscard
)

const (
	columnFieldName = iota
	columnFieldType
	columnFieldParameterStart
)

type columnForm struct {
	mode         columnFormMode
	focus        int
	pendingG     bool
	confirmed    bool
	saving       bool
	previousName string
	originalType string
	typeChanged  bool
	primaryKey   int
	nullable     bool
	hadDefault   bool
	typeIndex    int
	typePicker   int
	typeOptions  []sharedsql.ColumnType
	parameters   []textinput.Model
	name, preset textinput.Model
}

func newColumnForm(column sharedsql.ColumnInfo, typeOptions []sharedsql.ColumnType) columnForm {
	name := textinput.New()
	name.Prompt = ""
	name.SetValue(column.Name)
	preset := textinput.New()
	preset.Prompt = ""
	if column.DefaultValue != nil {
		preset.SetValue(*column.DefaultValue)
	}
	form := columnForm{
		previousName: column.Name,
		originalType: column.Type,
		primaryKey:   column.PrimaryKey,
		nullable:     column.Nullable,
		hadDefault:   column.DefaultValue != nil,
		typeOptions:  typeOptions,
		name:         name,
		preset:       preset,
	}
	if index, values, ok := sharedsql.MatchColumnType(typeOptions, column.Type); ok {
		form.selectType(index, values)
		return form
	}
	if strings.TrimSpace(column.Type) != "" {
		form.typeOptions = append([]sharedsql.ColumnType{{Name: column.Type}}, typeOptions...)
	}
	form.selectType(0, nil)
	return form
}

func (m *Model) openColumnForm() {
	row := m.structure.Cursor()
	if row < 0 || row >= len(m.structureColumns) {
		m.Status = "select a column"
		return
	}
	m.columnForm = newColumnForm(m.structureColumns[row], sharedsql.ColumnTypes(m.databaseInfo))
}

func (f columnForm) active() bool { return f.previousName != "" }

func (f columnForm) confirming() bool {
	return f.mode == columnFormConfirmSave || f.mode == columnFormConfirmDiscard
}

func (f *columnForm) Update(message tea.Msg) (tea.Cmd, columnFormAction) {
	if f.saving {
		return nil, columnFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if f.mode == columnFormSelectType {
		if !ok {
			return nil, columnFormNoAction
		}
		switch keyPress.String() {
		case "esc", "escape":
			f.mode = columnFormNormal
		case "j", "down":
			f.typePicker = (f.typePicker + 1) % len(f.typeOptions)
		case "k", "up":
			f.typePicker = (f.typePicker + len(f.typeOptions) - 1) % len(f.typeOptions)
		case "enter":
			f.typeChanged = f.typeChanged || f.typePicker != f.typeIndex
			f.selectType(f.typePicker, nil)
			f.mode = columnFormNormal
		}
		return nil, columnFormNoAction
	}
	if !ok {
		if f.mode != columnFormInsert {
			return nil, columnFormNoAction
		}
		return f.updateInput(message), columnFormNoAction
	}
	if f.confirming() {
		if keyPress.Key().Code == tea.KeyEscape {
			f.mode, f.confirmed = columnFormNormal, false
			return nil, columnFormNoAction
		}
		switch keyPress.String() {
		case "tab", "shift+tab":
			f.confirmed = !f.confirmed
		case "enter":
			if !f.confirmed {
				f.mode = columnFormNormal
				return nil, columnFormNoAction
			}
			if f.mode == columnFormConfirmSave {
				return nil, columnFormSave
			}
			return nil, columnFormDiscard
		}
		return nil, columnFormNoAction
	}
	if f.mode == columnFormInsert {
		if keyPress.Key().Code == tea.KeyEscape {
			f.mode = columnFormNormal
			f.blurInputs()
			return nil, columnFormNoAction
		}
		return f.updateInput(message), columnFormNoAction
	}
	if keyPress.Key().Code == tea.KeyEscape {
		f.mode, f.confirmed = columnFormConfirmDiscard, false
		return nil, columnFormNoAction
	}
	switch keyPress.String() {
	case "esc", "escape":
		f.mode, f.confirmed = columnFormConfirmDiscard, false
	case "ctrl+enter", "f5":
		f.mode, f.confirmed = columnFormConfirmSave, false
	case "j", "down":
		f.focus = (f.focus + 1) % f.fieldCount()
	case "k", "up":
		f.focus = (f.focus + f.fieldCount() - 1) % f.fieldCount()
	case "g":
		if f.pendingG {
			f.focus, f.pendingG = columnFieldName, false
			return nil, columnFormNoAction
		}
		f.pendingG = true
		return nil, columnFormNoAction
	case "G":
		f.focus, f.pendingG = f.defaultField(), false
	case "i":
		f.pendingG = false
		if f.focus == columnFieldType {
			f.typePicker, f.mode = f.typeIndex, columnFormSelectType
			return nil, columnFormNoAction
		}
		if f.focus == f.nullableField() {
			f.nullable = !f.nullable
			return nil, columnFormNoAction
		}
		f.mode = columnFormInsert
		return f.focusInput(), columnFormNoAction
	case "enter":
		f.pendingG = false
		if f.focus == columnFieldType {
			f.typePicker, f.mode = f.typeIndex, columnFormSelectType
			return nil, columnFormNoAction
		}
		if f.focus == f.nullableField() {
			f.nullable = !f.nullable
		}
	default:
		f.pendingG = false
	}
	if keyPress.Key().Code == ' ' && f.focus == f.nullableField() {
		f.nullable = !f.nullable
	}
	return nil, columnFormNoAction
}

func (f *columnForm) focusInput() tea.Cmd {
	f.blurInputs()
	switch f.focus {
	case columnFieldName:
		return f.name.Focus()
	case f.defaultField():
		return f.preset.Focus()
	}
	if parameter := f.parameterIndex(); parameter >= 0 {
		f.parameters[parameter].SetValue("")
		return f.parameters[parameter].Focus()
	}
	return nil
}

func (f *columnForm) blurInputs() {
	f.name.Blur()
	f.preset.Blur()
	for index := range f.parameters {
		f.parameters[index].Blur()
	}
}

func (f *columnForm) updateInput(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	switch f.focus {
	case columnFieldName:
		f.name, command = f.name.Update(message)
	case f.defaultField():
		f.preset, command = f.preset.Update(message)
	}
	if parameter := f.parameterIndex(); parameter >= 0 {
		f.parameters[parameter], command = f.parameters[parameter].Update(message)
	}
	return command
}

func (f columnForm) change() (sharedsql.ColumnChange, error) {
	typeDeclaration, err := f.typeDeclaration()
	if err != nil {
		return sharedsql.ColumnChange{}, err
	}
	change := sharedsql.ColumnChange{PreviousName: f.previousName, Name: f.name.Value(), Type: typeDeclaration, Nullable: f.nullable}
	if value := strings.TrimSpace(f.preset.Value()); value != "" {
		change.DefaultValue = &value
	}
	return change, nil
}

func (f *columnForm) setWidth(width int) {
	inputs := []*textinput.Model{&f.name, &f.preset}
	for index := range f.parameters {
		inputs = append(inputs, &f.parameters[index])
	}
	for _, input := range inputs {
		input.SetWidth(max(width-formLabelWidth-len(formFieldGap)-1, 1))
	}
}
