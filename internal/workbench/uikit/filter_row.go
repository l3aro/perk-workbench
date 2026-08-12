package uikit

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// FilterInputRow renders a filter input in a bordered box with a
// magnifying-glass suffix, sized to the given width. The border turns
// primary while the input is focused. The input is truncated because its
// placeholder view renders one cell wider than Width.
func FilterInputRow(input textinput.Model, width int) string {
	icon := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted)).Render("🔍")
	borderColor := ColorBorder
	if input.Focused() {
		borderColor = ColorPrimary
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(max(width-2, 0))
	// Box content area: width-2 (box) - 2 (borders) - 2 (padding) - 2 (icon).
	return box.Render(ansi.Truncate(input.View(), max(width-8, 0), "") + icon)
}
