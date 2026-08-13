package browse

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Update handles the browse pane keys and table passthrough: the sort,
// reset, copy-cell, and pager keys emit events for the root's query
// lifecycle, and table navigation updates the component state directly.
// The root dispatcher keeps the form routing and the overlay keys (edit
// row/cell, insert, cell view, context menu) because they open root-owned
// overlays or need the form-mode controller; Backend is the root-built
// database adapter the component may consult for capability decisions.
func (m Model) Update(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher, backend Backend) (Model, Event, tea.Cmd) {
	keyPress, ok := msg.(tea.KeyPressMsg)
	if ok {
		if m.Objects != nil {
			return m.updateObjectList(keyPress, layout, keys)
		}
		switch {
		case keys.Match(keyPress, "browse.sort", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if m.CycleSort() {
				return m, DataChanged{}, nil
			}
			return m, nil, nil
		case keys.Match(keyPress, "browse.reset", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			m.ResetFilters()
			return m, DataChanged{}, nil
		case keys.Match(keyPress, "cell.yank", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if text, ok := m.SelectedCellText(); ok {
				return m, uikit.ClipboardRequested{Text: text}, nil
			}
			return m, nil, nil
		case keys.Match(keyPress, "browse.next_page", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if m.Loading {
				return m, nil, nil
			}
			m.PageTag++
			return m, PageRequested{Delta: 1}, nil
		case keys.Match(keyPress, "browse.prev_page", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if m.Loading {
				return m, nil, nil
			}
			m.PageTag++
			return m, PageRequested{Delta: -1}, nil
		}
		if uikit.MoveTableCell(&m.Table, &m.SelectedColumn, &m.Offset, layout.ViewportWidth, keyPress) {
			return m, nil, nil
		}
	}
	var command tea.Cmd
	m.Table, command = m.Table.Update(msg)
	return m, nil, command
}

// updateObjectList handles object-list pane keys: Enter opens the
// selected table/view through the root, the context-menu key asks for
// the object menu, and navigation moves the cursor (object rows have no
// columns to select). Table-row actions (sort, filter, paging, row CRUD,
// cell copy) have no meaning on an object list and stay inert.
func (m Model) updateObjectList(keyPress tea.KeyPressMsg, layout uikit.Layout, keys uikit.KeyMatcher) (Model, Event, tea.Cmd) {
	switch {
	case keyPress.Key().Code == tea.KeyEnter:
		// Only tables and views open a table workspace; collection rows
		// keep their menu actions but are not openable.
		if object, ok := m.SelectedObject(); ok && (object.Type == "table" || object.Type == "view") {
			return m, ObjectOpenRequested{Object: object}, nil
		}
		return m, nil, nil
	case keys.Match(keyPress, "browse.context_menu", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
		if _, ok := m.SelectedObject(); ok {
			return m, ObjectContextMenuRequested{}, nil
		}
		return m, nil, nil
	case keys.Match(keyPress, "cell.yank", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
		return m, nil, nil
	}
	if uikit.MoveTableRow(&m.Table, &m.Offset, layout.ViewportWidth, keyPress) {
		return m, nil, nil
	}
	var command tea.Cmd
	m.Table, command = m.Table.Update(keyPress)
	return m, nil, command
}

// SelectedCellText returns the raw value of the selected cell (falling
// back to the table's display value when raw data is unavailable), or
// false when the selection is out of range.
func (m Model) SelectedCellText() (string, bool) {
	row, col := m.Table.Cursor(), m.SelectedColumn
	if row < 0 || row >= len(m.Result.Rows) || col < 0 || col >= len(m.Result.Columns) {
		return "", false
	}
	display := ""
	if row < len(m.Table.Rows()) && col < len(m.Table.Rows()[row]) {
		display = m.Table.Rows()[row][col]
	}
	return m.RawCellValue(row, col, display), true
}

// RawCellValue returns the untruncated value for the given cell, falling
// back to the display value when raw data is unavailable.
func (m Model) RawCellValue(row, col int, displayValue string) string {
	source := m.Result.UntruncatedRows
	if row >= 0 && row < len(source) && col >= 0 && col < len(source[row]) {
		if cell := source[row][col]; cell != nil {
			return *cell
		}
		return "NULL"
	}
	return displayValue
}
