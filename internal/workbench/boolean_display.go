package workbench

import "charm.land/lipgloss/v2"

const (
	iconBooleanTrue  = "\uf00c" // nf-fa-check
	iconBooleanFalse = "\uf00d" // nf-fa-times
)

var (
	trueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	falseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
)

func booleanValue(value bool) string {
	if value {
		return trueStyle.Render(iconBooleanTrue)
	}
	return falseStyle.Render(iconBooleanFalse)
}
