package chat

import (
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// View renders the chat pane body: the message viewport, the input, and
// the completion dropdown overlay. The root frames the pane and renders
// the mode badge.
func (cm Model) View(layout uikit.Layout) string {
	view := cm.Viewport.View()
	if dropdown := cm.CompletionOverlay(); dropdown != "" {
		lines := strings.Split(view, "\n")
		overlayLines := strings.Split(dropdown, "\n")
		start := len(lines) - len(overlayLines)
		if start < 0 {
			overlayLines = overlayLines[len(overlayLines)-len(lines):]
			start = 0
		}
		copy(lines[start:], overlayLines)
		view = strings.Join(lines, "\n")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		view,
		lipgloss.NewStyle().Padding(1, 0).Render(cm.Input.View()),
	)
}

// Draw renders nothing: the chat pane is a lipgloss pane, not a canvas
// overlay. The contract mirrors the other feature components.
func (cm Model) Draw(canvas uv.ScreenBuffer, layout uikit.Layout) {}

// Resize refits the input and viewport to the pane geometry.
func (cm *Model) Resize(layout uikit.Layout) {
	width := max(layout.Width, 1)
	height := max(layout.Height, 1)
	previousWidth := cm.Viewport.Width()
	cm.Input.SetWidth(width)
	cm.Input.SetHeight(1)
	cm.Viewport.SetWidth(width)
	cm.Viewport.SetHeight(max(height, 1))
	run := cm.ActiveRun()
	if previousWidth != width {
		run.resetRenderCache()
		run.resetStreamCache()
		run.CachedWidth = width
	}
	cm.initGlamour(width)
	cm.RefreshView()
}

// CompletionOverlay renders the slash-command dropdown above the input.
func (cm Model) CompletionOverlay() string {
	matches := cm.Completion.Matches
	if len(matches) == 0 {
		return ""
	}
	viewSize := 5
	selected := cm.Completion.Selected
	offset := selected - viewSize/2
	offset = min(offset, max(len(matches)-viewSize, 0))
	offset = max(offset, 0)
	visible := matches[offset : offset+min(viewSize, len(matches)-offset)]
	items := make([]string, 0, len(visible))
	for i, match := range visible {
		label := match.Label
		if offset+i == selected {
			label = "› " + label
		} else {
			label = "  " + label
		}
		item := uikit.CompletionItemStyle.Render(label)
		if match.Kind != "" {
			item += " " + uikit.CompletionDetailStyle.Render(match.Kind)
		}
		items = append(items, item)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(uikit.ColorPrimary)).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, items...))
}
