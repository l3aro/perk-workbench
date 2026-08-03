package workbench

import (
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

const doubleClickTimeout = 500 * time.Millisecond

func (m Model) handleLeftClick(x, y int) (tea.Model, tea.Cmd) {
	if y == 0 {
		// Header row: the button pinned to the far right opens the command
		// palette. Callers only route here when no overlay is open, so this
		// matches the app.palette keybinding's guard.
		if x >= m.width-headerButtonWidth() {
			m.commandPalette = newCommandPalette(m)
			m.commandPalette.visible = true
		}
		return m, nil
	}
	if m.hasOverlay() {
		return m, nil
	}
	switch m.State {
	case stateReady:
		if m.compact {
			return m, nil
		}
		// Content starts at y=1 (after header). Schema on left, workspace+query log on right.
		contentY := y - 1
		if contentY < 0 {
			return m, nil
		}
		if x < m.schemaWidth {
			if m.Focus != focusSchema {
				m.Focus = focusSchema
				m.queryLogPendingG = false
				m.editor.text.Blur()
				m.blurTables()
			}
			return m.schemaClick(contentY)
		}
		if m.chat.visible && x >= m.schemaWidth+m.editorWidth {
			m.Focus = focusChat
			m.queryLogPendingG = false
			m.editor.text.Blur()
			m.blurTables()
			// A double-click press just entered insert mode; the trailing
			// release must not reset it to normal.
			if !m.chatKeepInsert {
				m.chat.chatMode = formModeNormal
			}
			m.chatKeepInsert = false
			return m, nil
		}
		if contentY < m.workspaceHeight {
			workspaceX := max(x-m.schemaWidth, 0)
			return m.handleWorkspaceClick(workspaceX, contentY)
		}
		return m.focusQueryLogClick(x, contentY-m.workspaceHeight)
	case stateConnection:
		if m.compact {
			return m, nil
		}
		if x < m.schemaWidth && m.connection.focus != connectionFocusRecent {
			m.connection.focus = connectionFocusRecent
			return m, nil
		}
		if x >= m.schemaWidth && m.connection.focus != connectionFocusForm {
			m.connection.focus = connectionFocusForm
		}
		return m, nil
	case statePicking:
		// Full-width picker: same list header layout (TitleBar 2 lines + StatusBar 2 lines).
		// Items start at contentY=5. Default delegate uses Height=2, Spacing=1 (3 lines per item).
		itemLine := y - 1 - 5
		if itemLine >= 0 {
			itemOnPage := itemLine / 3
			items := m.picker.VisibleItems()
			start, end := m.picker.Paginator.GetSliceBounds(len(items))
			if start+itemOnPage < end {
				m.picker.Select(start + itemOnPage)
				if item, ok := m.picker.SelectedItem().(pickerItem); ok {
					return m, selectPickerItem(item.raw)
				}
			}
		}
		return m, nil
	case stateFailure:
		m.RecoverToPicker("choose another database")
		return m, readDirectory(m.pickerDir)
	}
	return m, nil
}

func (m Model) handleWorkspaceClick(x, y int) (tea.Model, tea.Cmd) {
	if m.Focus != focusWorkspace {
		m.Focus = focusWorkspace
		m.queryLogPendingG = false
		m.focusActiveTable()
	}
	// The workspace pane has a NormalBorder (top border at contentY=0).
	// Tab row is inside the border at contentY=1.
	if y == 1 {
		tabNames := []workspaceTab{tabSQL, tabBrowse, tabStructure, tabIndexes, tabForeignKeys}
		tabWidths := []int{5, 8, 9, 9, 15}
		cx := 2 // pane left border (1) + left padding (1)
		for i, w := range tabWidths {
			if x >= cx && x < cx+w {
				if m.Tab != tabNames[i] {
					m.Tab = tabNames[i]
					m.focusActiveTable()
					return m, m.loadPendingBrowse()
				}
				return m, nil
			}
			cx += w
		}
	}
	return m, nil
}

// schemaItemAt maps a schema-pane Y coordinate to its item, using the same
// visible/filter/pagination mapping as the rendered sidebar, and selects it.
func (m *Model) schemaItemAt(contentY int) (schemaItem, bool) {
	// contentY = terminal Y - 1 (after header). Filtering adds one status
	// line above the visible list items.
	itemOffset := 4
	if m.schema.IsFiltered() || m.schema.SettingFilter() {
		itemOffset = 5
	}
	itemY := contentY - itemOffset
	if itemY < 0 {
		return schemaItem{}, false
	}
	items := m.schema.VisibleItems()
	if len(items) == 0 {
		return schemaItem{}, false
	}
	start, end := m.schema.Paginator.GetSliceBounds(len(items))
	if itemY >= end-start {
		return schemaItem{}, false
	}
	m.schema.Select(start + itemY)
	item, ok := m.schema.SelectedItem().(schemaItem)
	return item, ok
}

// schemaRowY returns the screen Y of the schema list item at the given
// index, clamped to the visible window.
func (m Model) schemaRowY(index int) int {
	itemOffset := 4
	if m.schema.IsFiltered() || m.schema.SettingFilter() {
		itemOffset = 5
	}
	items := m.schema.VisibleItems()
	start, end := m.schema.Paginator.GetSliceBounds(len(items))
	row := itemOffset + (index - start)
	return max(row, itemOffset)
}

// openSchemaItemMenu opens the schema context menu for the given item:
// Add table on a database root, Rename/Delete on a table row, nothing for
// views.
func (m *Model) openSchemaItemMenu(item schemaItem, x, y int) {
	switch {
	case item.root:
		m.contextMenu = &contextMenuModel{
			options:  []menuOption{{label: "Add table", action: "add_table", keys: "a"}},
			selected: 0,
			visible:  true,
			x:        x,
			y:        y,
			database: item.database,
		}
	case item.kind == "table":
		m.contextMenu = &contextMenuModel{
			options: []menuOption{
				{label: "Rename table", action: "rename_table", keys: "r"},
				{label: "Delete table", action: "delete_table", keys: "d"},
			},
			selected: 0,
			visible:  true,
			x:        x,
			y:        y,
			database: item.database,
			table:    item.table,
		}
	}
}

func (m Model) schemaClick(contentY int) (tea.Model, tea.Cmd) {
	item, ok := m.schemaItemAt(contentY)
	if !ok {
		return m, nil
	}
	if item.root {
		m.expandedDatabases[item.database] = !m.expandedDatabases[item.database]
		return m, m.rebuildSchemaTree()
	}
	return m, m.selectSchemaTable(item)
}

func (m Model) focusQueryLogClick(x, contentY int) (tea.Model, tea.Cmd) {
	if m.Focus != focusQueryLog {
		m.Focus = focusQueryLog
		m.queryLogPendingG = false
		m.editor.text.Blur()
		m.blurTables()
		m.queryLog.Focus()
		if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
			m.queryLog.SetCursor(0)
		}
	}
	rowY := contentY - 3
	if rowY < 0 || rowY >= m.queryLog.Height() {
		return m, nil
	}
	rows := m.queryLog.Rows()
	start := min(max(m.queryLog.Cursor()-m.queryLog.Height()+1, 0), max(len(rows)-m.queryLog.Height(), 0))
	if row := start + rowY; row < len(rows) {
		m.queryLog.SetCursor(row)
		cellX := x - m.schemaWidth - 1 + m.queryLogOffset
		for index, column := range m.queryLog.Columns() {
			cellWidth := column.Width + 2*spaceCompact
			if cellX < cellWidth {
				m.queryLogColumn = index
				break
			}
			cellX -= cellWidth
		}
	}
	return m, nil
}

func (m Model) handleMouseWheel(wheel tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.height < 4 || m.width < 1 {
		return m, nil
	}
	// Forward wheel events to the focused area's table.
	step := 0
	switch wheel.Button {
	case tea.MouseWheelDown:
		step = 1
	case tea.MouseWheelUp:
		step = -1
	default:
		return m, nil
	}

	if m.State != stateReady {
		return m, nil
	}

	switch m.Focus {
	case focusSchema:
		return m, nil
	case focusWorkspace:
		m.scrollActiveWorkspaceTable(step)
	case focusQueryLog:
		rows := m.queryLog.Rows()
		rowCount := len(rows)
		if rowCount == 0 {
			return m, nil
		}
		newCursor := clamp(m.queryLog.Cursor()+step, 0, rowCount-1)
		m.queryLog.SetCursor(newCursor)
	}
	return m, nil
}

// handleSchemaTableClick handles left-click on structure, indexes, or foreignKeys tables.
// Row-level selection; double-click opens the edit form for the row.
func (m Model) handleSchemaTableClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State != stateReady || m.Focus != focusWorkspace || m.contextMenu != nil || m.compact {
		return m, nil
	}

	var targetTable *table.Model
	switch m.Tab {
	case tabStructure:
		if m.columnForm.active() {
			return m, nil
		}
		targetTable = &m.structure
	case tabIndexes:
		if m.indexForm.active() {
			return m, nil
		}
		targetTable = &m.indexes
	case tabForeignKeys:
		if m.foreignKeyForm.active() || m.relationshipDiagram {
			return m, nil
		}
		targetTable = &m.foreignKeys
	default:
		return m, nil
	}

	rows := targetTable.Rows()
	if len(rows) == 0 {
		return m, nil
	}

	// Workspace X: skip schema pane (left) and pane left border (1).
	workspaceX := max(absX-m.schemaWidth, 0) - 1
	if workspaceX < 0 || workspaceX >= m.tableViewportWidth {
		return m, nil
	}

	contentY := absY - 1
	if contentY < 0 {
		return m, nil
	}

	// Workspace pane: contentY=0 border, contentY=1 tab row, contentY=2 blank, contentY=3+ = table view.
	tableLine := contentY - 3 // 0=header, 1..N=data rows
	if tableLine < 1 {
		return m, nil // Header or above.
	}

	rowHeight := targetTable.Height()
	start := min(max(targetTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + tableLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil
	}

	targetTable.SetCursor(dataRow)

	// Check for double-click at the same position on the same tab and row:
	// open the row's edit form, matching the enter/i keybinding behavior.
	now := time.Now()
	if !m.lastClickTime.IsZero() && now.Sub(m.lastClickTime) < doubleClickTimeout &&
		m.lastClickX == absX && m.lastClickY == absY &&
		m.lastClickTab == m.Tab && m.lastClickRow == dataRow {
		m.lastClickTime = time.Time{}
		switch m.Tab {
		case tabStructure:
			return m, m.openColumnForm()
		case tabIndexes:
			if index := m.selectedIndex(); index != nil {
				return m, m.openIndexForm(index)
			}
		case tabForeignKeys:
			if foreignKey := m.selectedForeignKey(); foreignKey != nil {
				return m, m.openForeignKeyForm(foreignKey)
			}
		}
		return m, nil
	}

	// Single click: select the row.
	m.lastClickTime = now
	m.lastClickX = absX
	m.lastClickY = absY
	m.lastClickTab = m.Tab
	m.lastClickRow = dataRow
	return m, nil
}

// handleBrowseClick handles left-click on the browse or results table.
// It selects the cell and detects double-click for inline editing.
func (m Model) handleBrowseClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State != stateReady || m.Focus != focusWorkspace || m.contextMenu != nil {
		return m, nil
	}
	// Determine which table tab we're on and which table to target.
	switch m.Tab {
	case tabBrowse:
		if m.browseForm.active() || m.browseFilterForm != nil || len(m.browse.Rows()) == 0 {
			return m, nil
		}
	case tabSQL:
		if len(m.results.Rows()) == 0 {
			return m, nil
		}
	default:
		return m, nil
	}

	contentY := absY - 1
	if contentY < 0 {
		return m, nil
	}

	// The workspace pane has a 1-char border on each side.
	// Inside the pane: contentY=0 is border top, contentY=1 = tab row, contentY=2 = blank, contentY=3+ = browseView.
	browseLine := contentY - 3 // 0=header, 1..N=data rows
	if browseLine < 1 {
		return m, nil // Clicked on header or above data rows.
	}

	var targetTable *table.Model
	var targetCol *int
	var targetOffset *int
	var rows []table.Row
	switch m.Tab {
	case tabBrowse:
		targetTable = &m.browse
		targetCol = &m.browseColumn
		targetOffset = &m.browseOffset
		rows = m.browse.Rows()
	case tabSQL:
		targetTable = &m.results
		targetCol = &m.resultsColumn
		targetOffset = &m.resultsOffset
		rows = m.results.Rows()
	}

	rowHeight := targetTable.Height()
	start := min(max(targetTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + browseLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil
	}

	workspaceX := max(absX-m.schemaWidth, 0)
	if m.compact {
		return m, nil
	}

	browseX := workspaceX - 1 // Skip pane left border.
	if browseX < 0 {
		return m, nil
	}
	clickColOffset := browseX + *targetOffset
	if clickColOffset < 0 {
		return m, nil
	}

	col := 0
	colStart := 0
	columns := targetTable.Columns()
	for ci, colInfo := range columns {
		colEnd := colStart + colInfo.Width + 2*spaceCompact
		if clickColOffset >= colStart && clickColOffset < colEnd {
			col = ci
			break
		}
		colStart = colEnd
	}
	if col >= len(columns) {
		return m, nil
	}

	// Check for double-click at the same position.
	now := time.Now()
	if !m.lastClickTime.IsZero() && now.Sub(m.lastClickTime) < doubleClickTimeout &&
		m.lastClickX == absX && m.lastClickY == absY {
		// Double-click: open inline cell editor.
		targetTable.SetCursor(dataRow)
		*targetCol = col
		revealTableColumn(*targetTable, *targetCol, targetOffset, m.tableViewportWidth)
		m.lastClickTime = time.Time{}
		if m.Tab == tabBrowse {
			return m, m.openCellEditor()
		}
		return m, nil
	}

	// Single click: select the cell.
	m.lastClickTime = now
	m.lastClickX = absX
	m.lastClickY = absY
	targetTable.SetCursor(dataRow)
	*targetCol = col
	revealTableColumn(*targetTable, *targetCol, targetOffset, m.tableViewportWidth)
	return m, nil
}

func (m Model) handleRightClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State != stateReady {
		return m, nil
	}
	contentY := absY - 1
	if contentY < 0 || m.compact {
		return m, nil
	}

	if absX < m.schemaWidth {
		item, ok := m.schemaItemAt(contentY)
		if !ok {
			// Blank sidebar space: offer Add table in the current
			// selection's database; without a selection, no menu.
			item, ok = m.schema.SelectedItem().(schemaItem)
			if !ok || item.database == "" {
				return m, nil
			}
			item = schemaItem{database: item.database, root: true}
		}
		m.openSchemaItemMenu(item, absX, absY+1)
		return m, nil
	}

	// Only show context menu on browse table (tabBrowse) when form isn't active.
	if !(m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active()) {
		return m, nil
	}
	rows := m.browse.Rows()
	if len(rows) == 0 {
		return m, nil
	}

	// Map to row.
	browseLine := contentY - 3
	if browseLine < 1 {
		return m, nil
	}
	rowHeight := m.browse.Height()
	start := min(max(m.browse.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + browseLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil
	}

	// Select the row and build context menu.
	m.browse.SetCursor(dataRow)

	m.contextMenu = &contextMenuModel{
		options: []menuOption{
			{label: "Edit cell", action: "edit_cell"},
			{label: "Edit row", action: "edit_row"},
			{label: "Delete row", action: "delete_row"},
		},
		selected: 0,
		visible:  true,
		x:        absX,
		y:        absY + 1,
	}

	return m, nil
}
