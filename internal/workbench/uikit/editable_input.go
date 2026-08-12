package uikit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// EditableInput is a huh text input bound to a value pointer, extended
// with an external-editor escape hatch: the root shell reads and writes
// the bound value while the user edits in $EDITOR. Shared by every form
// (connection, browse, structure, index, foreign key, table).
type EditableInput struct {
	*huh.Input
	value *string
}

// NewEditableInput wraps a huh input with the value pointer it edits.
func NewEditableInput(input *huh.Input, value *string) *EditableInput {
	return &EditableInput{Input: input, value: value}
}

// Update delegates to the embedded input, keeping the wrapper type.
func (i *EditableInput) Update(message tea.Msg) (huh.Model, tea.Cmd) {
	model, command := i.Input.Update(message)
	i.Input = model.(*huh.Input)
	return i, command
}

// ExternalEditorValue returns the bound value for the external editor.
func (i *EditableInput) ExternalEditorValue() string { return *i.value }

// SetExternalEditorValue replaces the bound value after an external edit.
func (i *EditableInput) SetExternalEditorValue(value string) {
	*i.value = value
	i.Input.Value(i.value)
}
