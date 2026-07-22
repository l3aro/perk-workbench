package workbench

import (
	"context"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func TestModel_ctrlEEditsFocusedText(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Model)
		value func(Model) string
	}{
		{
			name: "connection field",
			setup: func(model *Model) {
				model.connection.setFocus(connectionFocusName)
				model.connection.name.SetValue("before")
			},
			value: func(model Model) string { return model.connection.name.Value() },
		},
		{
			name: "picker filter",
			setup: func(model *Model) {
				model.State = statePicking
				model.picker.SetFilterText("before")
				model.picker.SetFilterState(list.Filtering)
			},
			value: func(model Model) string { return model.picker.FilterValue() },
		},
		{
			name: "schema filter",
			setup: func(model *Model) {
				model.State, model.Focus = stateReady, focusSchema
				model.schema.SetFilterText("before")
				model.schema.SetFilterState(list.Filtering)
			},
			value: func(model Model) string { return model.schema.FilterValue() },
		},
		{
			name: "SQL editor",
			setup: func(model *Model) {
				model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
				model.editor.textarea.Focus()
				model.editor.textarea.SetValue("before")
			},
			value: func(model Model) string { return model.editor.textarea.Value() },
		},
		{
			name: "column form",
			setup: func(model *Model) {
				input := textinput.New()
				input.Focus()
				input.SetValue("before")
				model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabStructure
				model.columnForm = columnForm{mode: columnFormInsert, name: input}
			},
			value: func(model Model) string { return model.columnForm.name.Value() },
		},
		{
			name: "browse form",
			setup: func(model *Model) {
				input := textinput.New()
				input.Focus()
				input.SetValue("before")
				model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabBrowse
				model.browseForm = browseForm{mode: browseFormInsert, inputs: []textinput.Model{input}}
			},
			value: func(model Model) string { return model.browseForm.inputs[0].Value() },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			t.Setenv("EDITOR", "true")
			model := New("", Open(context.Background()))
			test.setup(&model)

			// When
			updated, command := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			model = updated.(Model)
			updated, _ = model.Update(externalEditorFinishedMsg{value: "after"})
			model = updated.(Model)

			// Then
			if command == nil {
				t.Fatal("editor command = nil")
			}
			if got := test.value(model); got != "after" {
				t.Fatalf("value = %q, want edited value", got)
			}
		})
	}
}
