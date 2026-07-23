package workbench

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"
)

func (f *columnForm) selectType(index int, values []string) {
	if index < 0 || index >= len(f.typeOptions) {
		return
	}
	f.values.typeName = f.typeOptions[index].Name
	parameters := f.typeOptions[index].Parameters
	f.values.parameters = make([]string, len(parameters))
	for parameterIndex, parameter := range parameters {
		f.values.parameters[parameterIndex] = parameter.Default
		if parameterIndex < len(values) {
			f.values.parameters[parameterIndex] = strings.TrimSpace(values[parameterIndex])
		}
	}
}

func (f columnForm) typeIndex() int {
	for index, option := range f.typeOptions {
		if option.Name == f.values.typeName {
			return index
		}
	}
	return 0
}

func (f columnForm) typeChoices() []huh.Option[string] {
	choices := make([]huh.Option[string], len(f.typeOptions))
	for index, option := range f.typeOptions {
		choices[index] = huh.NewOption(option.Name, option.Name)
	}
	return choices
}

func (f columnForm) validateType(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("column type is required")
	}
	for _, option := range f.typeOptions {
		if option.Name == value {
			return nil
		}
	}
	return fmt.Errorf("column type is invalid")
}

func (f columnForm) validateParameter(index int) func(string) error {
	return func(value string) error {
		values := append([]string(nil), f.values.parameters...)
		values[index] = value
		_, err := f.typeOptions[f.typeIndex()].Declaration(values)
		return err
	}
}

func (f columnForm) typeDeclaration() (string, error) {
	if !f.typeChanged && strings.TrimSpace(f.originalType) != "" {
		if _, err := f.typeOptions[f.typeIndex()].Declaration(f.values.parameters); err != nil {
			return "", err
		}
		return f.originalType, nil
	}
	if err := f.validateType(f.values.typeName); err != nil {
		return "", err
	}
	return f.typeOptions[f.typeIndex()].Declaration(f.values.parameters)
}
