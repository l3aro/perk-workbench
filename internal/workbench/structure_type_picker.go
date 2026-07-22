package workbench

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
)

func (f *columnForm) selectType(index int, values []string) {
	f.typeIndex, f.typePicker = index, index
	parameters := f.typeOptions[index].Parameters
	f.parameters = make([]textinput.Model, len(parameters))
	for parameterIndex, parameter := range parameters {
		input := textinput.New()
		input.Prompt = ""
		input.SetValue(parameter.Default)
		if parameterIndex < len(values) {
			input.SetValue(strings.TrimSpace(values[parameterIndex]))
		}
		f.parameters[parameterIndex] = input
	}
}

func (f columnForm) fieldCount() int { return f.defaultField() + 1 }

func (f columnForm) nullableField() int { return columnFieldParameterStart + len(f.parameters) }

func (f columnForm) defaultField() int { return f.nullableField() + 1 }

func (f columnForm) parameterIndex() int {
	index := f.focus - columnFieldParameterStart
	if index < 0 || index >= len(f.parameters) {
		return -1
	}
	return index
}

func (f columnForm) typeDeclaration() (string, error) {
	if !f.typeChanged && strings.TrimSpace(f.originalType) != "" {
		return f.originalType, nil
	}
	values := make([]string, len(f.parameters))
	for index, parameter := range f.parameters {
		values[index] = parameter.Value()
	}
	return f.typeOptions[f.typeIndex].Declaration(values)
}

func (f columnForm) typeValue() string {
	declaration, err := f.typeDeclaration()
	if err != nil {
		return f.typeOptions[f.typeIndex].Name
	}
	return declaration
}

func (f columnForm) typePickerView() string {
	const visibleOptions = 7
	start := min(max(f.typePicker-visibleOptions/2, 0), max(len(f.typeOptions)-visibleOptions, 0))
	end := min(start+visibleOptions, len(f.typeOptions))
	options := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		option := f.typeOptions[index].Name
		if index == f.typePicker {
			option = headerStyle.Render(option)
		}
		options = append(options, option)
	}
	return headerStyle.Render("Select type") + "\n" + strings.Join(options, "\n") + "\n" + statusStyle.Render("j/k choose | Enter select | Esc return")
}
