package workbench

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// formButtonsBar renders the Save/Cancel button row shown under editable
// forms. The row is hit-tested with formButtonAt.
func formButtonsBar() string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		formSaveButtonStyle.Render("Save"), " ", formCancelButtonStyle.Render("Cancel"))
}

// formButtonAt returns the button under a click at relX within the buttons
// bar: "save", "cancel", or "" when the click misses both buttons.
func formButtonAt(relX int) string {
	saveWidth := ansi.StringWidth(formSaveButtonStyle.Render("Save"))
	if relX >= 0 && relX < saveWidth {
		return "save"
	}
	gap := saveWidth + 1 // one space between the buttons
	if relX >= gap && relX < gap+ansi.StringWidth(formCancelButtonStyle.Render("Cancel")) {
		return "cancel"
	}
	return ""
}

// formTabActive reports whether the current workspace tab shows an editable
// form, which owns the bottom button bar.
func (m Model) formTabActive() bool {
	switch m.Tab {
	case tabStructure:
		return m.columnForm.active()
	case tabBrowse:
		return m.browseFilterForm != nil || m.browseForm.active()
	case tabIndexes:
		return m.indexForm.active()
	case tabForeignKeys:
		return m.foreignKeyForm.active()
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

// formListKeyPress synthesizes connection.switch_to_list ("1"): the Cancel
// action of the connection form, where Escape is a no-op.
func formListKeyPress() tea.Cmd {
	return func() tea.Msg { return tea.KeyPressMsg{Code: '1', Text: "1"} }
}

// formSaveCommand returns the Save-button command. While a field is being
// edited (insert mode), the Escape that exits the editor is applied
// synchronously — exactly what the keyboard does — so the returned form.save
// key always lands on the normal-mode save path instead of being swallowed
// by the field editor. Mouse-entered filter edits set only
// browseFilterForm.editing, so it is checked alongside the controller mode.
func (m Model) formSaveCommand() (Model, tea.Cmd) {
	if m.formMode.editing() || (m.browseFilterForm != nil && m.browseFilterForm.editing) {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
	}
	return m, formSaveKeyPress()
}

// connectionCancelCommand returns the Cancel-button command of the
// connection form. While a field is being edited, the Escape that exits the
// editor is applied synchronously — so the following switch_to_list key
// ("1") reaches the profiles list instead of being typed into the focused
// field.
func (m Model) connectionCancelCommand() (Model, tea.Cmd) {
	if m.formMode.editing() {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
	}
	return m, formListKeyPress()
}
