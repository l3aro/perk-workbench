package app

import (
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// editableInput is the shared huh text-input wrapper from the UI contract
// layer: a value-bound input with the external-editor escape hatch.
type editableInput = uikit.EditableInput

func newEditableInput(input *huh.Input, value *string) *editableInput {
	return uikit.NewEditableInput(input, value)
}
