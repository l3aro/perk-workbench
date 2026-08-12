package workbench

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// formButtonsBar renders the Save/Cancel button row shown under editable
// forms. When focused, only the chosen button is lit (focus color) and the
// other renders dimmed, so the primary style never reads as a second
// highlight. The row is hit-tested with formButtonAt.
func formButtonsBar(focused bool, choice int) string {
	save, cancel := formSaveButtonStyle, formCancelButtonStyle
	if focused {
		save, cancel = formCancelButtonStyle, formCancelButtonStyle
		if choice == 0 {
			save = formButtonFocusedStyle
		} else {
			cancel = formButtonFocusedStyle
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left,
		save.Render("Save"), " ", cancel.Render("Cancel"))
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
		return m.structure.columnForm.active()
	case tabBrowse:
		return m.browse.filterForm != nil || m.browse.documentEditor != nil || m.browse.form.active()
	case tabIndexes:
		return m.structure.indexForm.active()
	case tabForeignKeys:
		return m.structure.foreignKeyForm.active()
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
	if m.overlay.formMode.editing() || (m.browse.filterForm != nil && m.browse.filterForm.editing) {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
	}
	return m, formSaveKeyPress()
}
