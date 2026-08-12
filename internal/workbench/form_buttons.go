package workbench

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// The Save/Cancel button bar is a shared UI contract: the rendered row and
// its click hit-test live in uikit so the browse cell editor and the root
// form footer draw identically.
var formButtonsBar = uikit.FormButtonsBar

var formButtonAt = uikit.FormButtonAt

// formTabActive reports whether the current workspace tab shows an editable
// form, which owns the bottom button bar.
func (m Model) formTabActive() bool {
	switch m.Tab {
	case tabStructure:
		return m.schema.component.Structure.ColumnForm.Active()
	case tabBrowse:
		return m.browse.component.FilterForm != nil || m.browse.component.DocumentEditor != nil || m.browse.component.Form.Active()
	case tabIndexes:
		return m.schema.component.Structure.IndexForm.Active()
	case tabForeignKeys:
		return m.schema.component.Structure.ForeignKeyForm.Active()
	}
	return false
}

// The button commands synthesize the exact default key presses the buttons
// stand for, so a click routes through the same Update path as the keyboard.
// ponytail: literal default keys only; user-remapped bindings (form.save,
// form.discard, connection.switch_to_list) are not forwarded — resolve the
// configured stroke if that ever matters.
func formSaveKeyPress() tea.Cmd {
	return func() tea.Msg { return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl, Text: "s"} }
}

func formEscapeKeyPress() tea.Cmd {
	return func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEscape} }
}

// formSaveCommand returns the Save-button command. While a field is being
// edited (insert mode), the Escape that exits the editor is applied
// synchronously — exactly what the keyboard does — so the returned form.save
// key always lands on the normal-mode save path instead of being swallowed
// by the field editor. Mouse-entered filter edits set only
// browseFilterForm.editing, so it is checked alongside the controller mode.
func (m Model) formSaveCommand() (Model, tea.Cmd) {
	if m.overlay.formMode.Editing() || (m.browse.component.FilterForm != nil && m.browse.component.FilterForm.Editing) {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
	}
	return m, formSaveKeyPress()
}
