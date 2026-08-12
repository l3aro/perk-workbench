package querylog

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Update handles query-log messages: the detail overlay consumes
// everything while open; otherwise the pane's keybindings, table
// passthrough, and pending-G logic run. The context-menu key and the
// pane focus switch stay in root (they need screen geometry); root routes
// them before calling Update. Events ask root for side effects the
// component cannot perform itself.
func (m Model) Update(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher) (Model, uikit.Event, tea.Cmd) {
	// The detail overlay replaces normal content and consumes all input.
	if m.Detail != nil {
		if keyPress, ok := msg.(tea.KeyPressMsg); ok {
			switch {
			case keys.Match(keyPress, "detail.explain", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				// Root builds the picker; it closes the detail only when
				// the picker is supported (matches the old behavior).
				return m, uikit.ExplainRequested{Statement: m.Detail.Statement}, nil
			case keys.Match(keyPress, "detail.close", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Detail = nil
				return m, nil, nil
			}
		}
		return m, nil, nil
	}

	if keyPress, ok := msg.(tea.KeyPressMsg); ok {
		if !keys.Match(keyPress, "query_log.top_first", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}) {
			if m.PendingG {
				m.PendingG = false
				return m, nil, nil
			}
			m.PendingG = false
		}
		if keys.Match(keyPress, "query_log.next_page", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}) {
			if m.Page+1 < m.PageCount() {
				m.Page++
				m.Table.SetCursor(0)
				m.Render()
			}
			return m, nil, nil
		}
		if keys.Match(keyPress, "query_log.prev_page", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}) {
			if m.Page > 0 {
				m.Page--
				m.Table.SetCursor(0)
				m.Render()
			}
			return m, nil, nil
		}
		if uikit.MoveTableCell(&m.Table, &m.Column, &m.Offset, layout.ViewportWidth, keyPress) {
			return m, nil, nil
		}
		rows := m.Table.Rows()
		if len(rows) == 0 {
			return m, nil, nil
		}
		switch {
		case keys.Match(keyPress, "query_log.yank", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			entry, ok := m.SelectedEntry()
			if !ok {
				return m, nil, nil
			}
			return m, uikit.ClipboardRequested{Text: queryLogCell(entry, m.Column)}, nil
		case keys.Match(keyPress, "query_log.explain", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			entry, ok := m.SelectedEntry()
			if !ok {
				return m, nil, nil
			}
			return m, uikit.ExplainRequested{Statement: entry.Statement}, nil
		case keys.Match(keyPress, "query_log.top_first", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if m.PendingG {
				m.Table.SetCursor(0)
				m.Column, m.Offset = 0, 0
				m.PendingG = false
			} else {
				m.PendingG = true
			}
			return m, nil, nil
		case keys.Match(keyPress, "query_log.top_last", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			m.Table.SetCursor(len(rows) - 1)
			return m, nil, nil
		case keys.Match(keyPress, "query_log.detail", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if entry, ok := m.SelectedEntry(); ok {
				m.Detail = &entry
			}
			return m, nil, nil
		}
	}
	var cmd tea.Cmd
	m.Table, cmd = m.Table.Update(msg)
	return m, nil, cmd
}
