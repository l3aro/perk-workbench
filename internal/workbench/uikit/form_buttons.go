package uikit

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// FormButtonsBar renders the Save/Cancel button row shown under editable
// forms. When focused, only the chosen button is lit (focus color) and the
// other renders dimmed, so the primary style never reads as a second
// highlight. The row is hit-tested with FormButtonAt.
func FormButtonsBar(focused bool, choice int) string {
	save, cancel := ButtonSaveStyle, ButtonCancelStyle
	if focused {
		save, cancel = ButtonCancelStyle, ButtonCancelStyle
		if choice == 0 {
			save = ActionFocusedStyle
		} else {
			cancel = ActionFocusedStyle
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left,
		save.Render("Save"), " ", cancel.Render("Cancel"))
}

// FormButtonAt returns the button under a click at relX within the buttons
// bar: "save", "cancel", or "" when the click misses both buttons.
func FormButtonAt(relX int) string {
	saveWidth := ansi.StringWidth(ButtonSaveStyle.Render("Save"))
	if relX >= 0 && relX < saveWidth {
		return "save"
	}
	gap := saveWidth + 1 // one space between the buttons
	if relX >= gap && relX < gap+ansi.StringWidth(ButtonCancelStyle.Render("Cancel")) {
		return "cancel"
	}
	return ""
}
