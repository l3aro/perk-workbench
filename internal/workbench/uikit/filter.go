package uikit

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// NewFilterInput returns a persistent pane filter input. The list's
// built-in filter is driven externally via SetFilterText so the input can
// stay visible with its own placeholder and icon. Shared by the schema
// sidebar, the profiles pane, and the notification history modal.
func NewFilterInput() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "filter"
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted))
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted))
	input.SetStyles(styles)
	input.CharLimit = 64
	return input
}
