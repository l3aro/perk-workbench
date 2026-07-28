package workbench

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func titledPane(title, content string, style lipgloss.Style) string {
	border := style.GetBorderStyle()
	borderStyle := lipgloss.NewStyle().
		Foreground(style.GetBorderTopForeground()).
		Background(style.GetBorderTopBackground())
	width := style.GetWidth()
	labelWidth := max(width-lipgloss.Width(border.TopLeft)-lipgloss.Width(border.TopRight), 0)
	label := ""
	if labelWidth >= 3 {
		label = " " + ansi.Truncate(title, labelWidth-2, "") + " "
	}
	top := borderStyle.Render(border.TopLeft) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk)).Bold(true).Render(label) +
		borderStyle.Render(strings.Repeat(border.Top, max(width-lipgloss.Width(border.TopLeft)-lipgloss.Width(label)-lipgloss.Width(border.TopRight), 0))) +
		borderStyle.Render(border.TopRight)
	bodyStyle := style.Copy().BorderTop(false)
	if height := style.GetHeight(); height > 0 {
		bodyStyle = bodyStyle.Height(height - 1)
	}
	return top + "\n" + bodyStyle.Render(content)
}
