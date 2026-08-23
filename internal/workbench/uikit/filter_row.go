package uikit

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type filterInputRowKey struct {
	width   int
	focused bool
}

type filterInputRowStyle struct {
	box  lipgloss.Style
	icon string
}

var filterInputRowCache = make(map[filterInputRowKey]filterInputRowStyle)

func clearFilterInputRowCache() {
	filterInputRowCache = make(map[filterInputRowKey]filterInputRowStyle)
}

func filterInputRowStyles(width int, focused bool) filterInputRowStyle {
	key := filterInputRowKey{width: width, focused: focused}
	if style, ok := filterInputRowCache[key]; ok {
		return style
	}
	borderColor := ColorBorder
	if focused {
		borderColor = ColorPrimary
	}
	style := filterInputRowStyle{
		icon: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted)).Render("🔍"),
		box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(borderColor)).
			Padding(0, 1).
			Width(max(width-2, 0)),
	}
	filterInputRowCache[key] = style
	return style
}

// FilterInputRow renders a filter input in a bordered box with a
// magnifying-glass suffix, sized to the given width. The border turns
// primary while the input is focused. The input is truncated because its
// placeholder view renders one cell wider than Width.
func FilterInputRow(input textinput.Model, width int) string {
	style := filterInputRowStyles(width, input.Focused())
	return style.box.Render(ansi.Truncate(input.View(), max(width-8, 0), "") + style.icon)
}
