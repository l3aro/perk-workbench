package connection

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Update handles the recent-profiles pane messages: filter editing (every
// message goes to the input so clipboard paste works; enter/escape exit
// editing, keeping the applied filter) and the list passthrough. The
// pane's action keys (add/edit/delete/menu/switch) stay in the root
// dispatcher because they open root-owned overlays or the form-mode
// controller; the component emits events for the database flows.
func (m Model) Update(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher) (Model, Event, tea.Cmd) {
	if m.Form.Focus != FocusRecent {
		return m, nil, nil
	}
	if m.RecentFilter.Focused() {
		if keyPress, ok := msg.(tea.KeyPressMsg); ok {
			switch keyPress.Code {
			case tea.KeyEscape, tea.KeyEnter:
				m.BlurFilter()
				return m, nil, nil
			}
		}
		before := m.RecentFilter.Value()
		var filterCommand tea.Cmd
		m.RecentFilter, filterCommand = m.RecentFilter.Update(msg)
		if m.RecentFilter.Value() != before {
			m.ApplyFilter()
		}
		return m, nil, filterCommand
	}
	var command tea.Cmd
	m.Recent, command = m.Recent.Update(msg)
	// The list's own keymap can clear the filter (esc in list navigation);
	// keep the visible input in sync.
	m.SyncFilter()
	return m, nil, command
}

// Draw renders nothing: the connection screen has no canvas overlays.
// The contract mirrors the other feature components; root calls it at its
// overlay draw slots.
func (m Model) Draw(canvas uv.ScreenBuffer, layout uikit.Layout) {}
