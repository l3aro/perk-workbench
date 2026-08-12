package notification

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Update handles the notification messages in the root's precedence
// order: the popup dismiss timer, the release trailing a popup click, the
// popup click itself (which opens the history or detail overlay), and the
// open overlays, which swallow every input while visible. Messages the
// component does not own pass through unchanged; root routes them only
// when Consumes reports them.
func (m Model) Update(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher) (Model, uikit.Event, tea.Cmd) {
	switch msg := msg.(type) {
	case DismissMsg:
		if msg.Generation == m.Generation {
			m.Popup = nil
		}
		return m, nil, nil
	case tea.MouseReleaseMsg:
		if m.PopupSwallowRelease {
			m.PopupSwallowRelease = false
		}
		return m, nil, nil
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if bounds, ok := m.PopupBounds(layout); ok && msg.X >= bounds.Min.X && msg.X < bounds.Max.X && msg.Y >= bounds.Min.Y && msg.Y < bounds.Max.Y {
				m.PopupSwallowRelease = true
				// A persisted popup (nonzero row ID) only exists while its
				// connection scope is live, so it opens the scoped history
				// modal; a transient popup opens the single-entry detail.
				if m.Popup != nil && m.Popup.ID != 0 {
					m.History = NewHistory(m.Entries, m.Popup.ID, layout.Width, layout.Height)
				} else {
					m.Detail = m.Popup
				}
				return m, nil, nil
			}
		}
		if m.History != nil {
			if msg.Button == tea.MouseLeft {
				m.History.handleClick(msg.X, msg.Y)
			}
			return m, nil, nil
		}
		return m, nil, nil
	case tea.MouseWheelMsg:
		if m.History != nil {
			m.History.handleWheel(msg)
			return m, nil, nil
		}
		return m, nil, nil
	case tea.KeyPressMsg:
		if m.History != nil {
			// The filter's first Escape only blurs the filter and the
			// viewer's Escape closes the viewer; a further Escape closes
			// the modal.
			if msg.Key().Code == tea.KeyEscape && !m.History.filterFocused && m.History.viewer == nil {
				m.History = nil
				return m, nil, nil
			}
			if handled, event := m.History.handleKey(msg); handled {
				return m, event, nil
			}
			return m, nil, nil
		}
		if m.Detail != nil {
			if msg.Key().Code == tea.KeyEscape {
				m.Detail = nil
			}
			return m, nil, nil
		}
		return m, nil, nil
	}
	return m, nil, nil
}
