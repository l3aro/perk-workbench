package workbench

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type editableInput struct {
	*huh.Input
	value *string
}

func newEditableInput(input *huh.Input, value *string) *editableInput {
	return &editableInput{Input: input, value: value}
}
func (i *editableInput) Update(message tea.Msg) (huh.Model, tea.Cmd) {
	model, command := i.Input.Update(message)
	i.Input = model.(*huh.Input)
	return i, command
}

func (i *editableInput) externalEditorValue() string { return *i.value }

func (i *editableInput) setExternalEditorValue(value string) {
	*i.value = value
	i.Input.Value(i.value)
}
