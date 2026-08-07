package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
			// Single-pane layout: route by the focused pane, which renders
			// full-width with its title row at y=1 (contentY=0).
			contentY := y - 1
			if contentY < 1 {
				return m, nil
			}
			switch m.Focus {
			case focusSchema:
				return m.schemaClick(contentY)
			case focusWorkspace:
				return m.handleWorkspaceClick(x, contentY)
			case focusQueryLog:
				return m.focusQueryLogClick(x, contentY)
			case focusChat:
				if !m.chatKeepInsert {
					m.chat.chatMode = formModeNormal
				}
				m.chatKeepInsert = false
				return m, nil
			}
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
		// contentY is relative to the pane title row: the title sits at
		// queryLogTop, so subtract queryLogTop-1 to make it relative to 0.
		return m.focusQueryLogClick(x, contentY-m.queryLogTop()+1)
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

// handleRecentClick maps a click on the recent-connections list to its item:
// a single click selects the profile, a double click loads it into the
// connection form (matching the Enter keybinding). Presses only — the
// trailing release routes through handleLeftClick, which only switches pane
// focus.
func (m Model) handleRecentClick(x, y int) (tea.Model, tea.Cmd) {
	contentY := y - 1
	// Pane top border at contentY=0; the list renders its status bar (3
	// lines) above the items. Filtering inserts the filter prompt line.
	itemOffset := 4
	if m.recent.SettingFilter() {
		itemOffset = 5
	}
	itemLine := contentY - itemOffset
	if itemLine < 0 {
		return m, nil
	}
	itemOnPage := itemLine / 3 // DefaultDelegate: Height 2 + Spacing 1
	items := m.recent.VisibleItems()
	start, end := m.recent.Paginator.GetSliceBounds(len(items))
	if start+itemOnPage >= end {
		return m, nil
	}
	m.recent.Select(start + itemOnPage)
	if !m.recordFormClick(x, y) {
		return m, nil
	}
	// Double-click: load the profile into the form, matching Enter.
	return m, m.editSelectedRecentConnection()
}

func (m Model) handleWorkspaceClick(x, y int) (tea.Model, tea.Cmd) {
	if m.Focus != focusWorkspace {
		m.Focus = focusWorkspace
		m.queryLogPendingG = false
		m.focusActiveTable()
	}
	// The workspace pane has a RoundedBorder (top border at contentY=0).
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

// schemaStatusText mirrors the list's statusView text (filter chip, item
// count, filtered count) so the offset can predict how the pane body wraps
// it.
func (m Model) schemaStatusText() string {
	name := "items"
	if n := len(m.schema.VisibleItems()); n == 1 {
		name = "item"
	}
	var status string
	if m.schema.SettingFilter() && len(m.schema.VisibleItems()) == 0 {
		status = "Nothing matched"
	} else {
		status = fmt.Sprintf("%d %s", len(m.schema.VisibleItems()), name)
		if !m.schema.SettingFilter() && m.schema.IsFiltered() {
			filter := ansi.Truncate(strings.TrimSpace(m.schema.FilterValue()), 10, "…")
			status = fmt.Sprintf("“%s” %s", filter, status)
		}
	}
	if filtered := len(m.schema.Items()) - len(m.schema.VisibleItems()); filtered > 0 {
		status += fmt.Sprintf(" • %d filtered", filtered)
	}
	return status
}

// statusWrapLines counts the lines the status text occupies when the pane
// body wraps it at the given inner width, mirroring lipgloss's word wrap
// (plus the 2 leading cells of the list's StatusBar style). The first word
// sits directly after the padding — no joining space before it.
func statusWrapLines(status string, inner int) int {
	if inner < 1 {
		return 1
	}
	words := strings.Fields(status)
	if len(words) == 0 {
		return 1
	}
	lines := 1
	width := 2 + ansi.StringWidth(words[0])
	for _, word := range words[1:] {
		w := ansi.StringWidth(word)
		if w > inner { // a single word longer than the line hard-wraps
			lines += w / inner
			width = w % inner
			continue
		}
		if width+1+w > inner {
			lines++
			width = w
		} else {
			width += 1 + w
		}
	}
	return lines
}

// schemaItemOffset returns the content Y of the first schema item line (the
// pane border plus the list's title/status sections). The wrapped status
// line can take several rows on narrow panes; the open filter input adds
// one more line.
func (m Model) schemaItemOffset() int {
	offset := 3 + statusWrapLines(m.schemaStatusText(), m.schemaWidth-6)
	if m.schema.SettingFilter() {
		offset++
	}
	return offset
}

// openBlankServerMenu opens the blank-sidebar context menu: creating a
// database is valid on any server product and needs no selection.
func (m *Model) openBlankServerMenu(x, y int) {
	m.contextMenu = &contextMenuModel{
		options:  []menuOption{{label: "Add new database", action: "create_database", keys: "A"}},
		selected: 0,
		visible:  true,
		x:        x,
		y:        y,
	}
}

// schemaItemAt maps a schema-pane Y coordinate to its item, using the same
// visible/filter/pagination mapping as the rendered sidebar, and selects it.
func (m *Model) schemaItemAt(contentY int) (schemaItem, bool) {
	// contentY = terminal Y - 1 (after header).
	itemOffset := m.schemaItemOffset()
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
	itemOffset := m.schemaItemOffset()
	items := m.schema.VisibleItems()
	start, _ := m.schema.Paginator.GetSliceBounds(len(items))
	row := itemOffset + (index - start)
	return max(row, itemOffset)
}

// schemaAddTarget returns the qualifier for a new table next to the
// selected item: the database for SQLite/MySQL, the schema for PostgreSQL
// (whose sidebar groups tables under database roots).
func (m Model) schemaAddTarget(item schemaItem) (string, bool) {
	if m.databaseInfo.Product == "PostgreSQL" {
		switch item.kind {
		case "schema":
			return item.schema, true
		case "table":
			schema, _, found := strings.Cut(item.table, ".")
			return schema, found
		}
		return "", false
	}
	if item.root || item.kind == "table" {
		return item.database, true
	}
	return "", false
}

// openSchemaItemMenu opens the schema context menu for the given item:
// each tree level offers its sibling, child, edit, and delete operations.
// SQLite keeps the table-only menu; views expose no menu.
func (m *Model) openSchemaItemMenu(item schemaItem, x, y int) {
	switch {
	case item.kind == "schema":
		// database carries the schema-qualified Add table target (the
		// table form uses it as the PostgreSQL schema); schema carries
		// the same value for the schema-level actions.
		m.contextMenu = &contextMenuModel{
			options: []menuOption{
				{label: "Add new schema", action: "create_schema", keys: "A"},
				{label: "Add new table", action: "add_table", keys: "a"},
				{label: "Edit schema", action: "rename_schema", keys: "r"},
				{label: "Delete schema", action: "delete_schema", keys: "d"},
			},
			selected: 0,
			visible:  true,
			x:        x,
			y:        y,
			database: item.schema,
			schema:   item.schema,
		}
	case item.root:
		switch {
		case m.databaseInfo.Product == "PostgreSQL":
			// The connected database cannot be renamed or dropped in
			// place; a root that is not the connected database offers
			// Connect to switch to it and full database operations.
			options := []menuOption{
				{label: "Add new database", action: "create_database", keys: "A"},
				{label: "Add new schema", action: "create_schema", keys: "a"},
			}
			if !m.databaseRootConnected(item.database) {
				options = []menuOption{
					{label: "Connect", action: "connect_database", keys: "enter"},
					{label: "Add new database", action: "create_database", keys: "A"},
					{label: "Edit database", action: "rename_database", keys: "r"},
					{label: "Delete database", action: "delete_database", keys: "d"},
				}
			}
			m.contextMenu = &contextMenuModel{
				options:  options,
				selected: 0,
				visible:  true,
				x:        x,
				y:        y,
				database: item.database,
			}
		case m.supportsCreateDatabase():
			// MySQL treats database and schema as one level: sibling
			// database actions plus the table child. Database rename
			// has no safe DDL, so Edit database is not offered.
			m.contextMenu = &contextMenuModel{
				options: []menuOption{
					{label: "Add new database", action: "create_database", keys: "A"},
					{label: "Add new table", action: "add_table", keys: "a"},
					{label: "Delete database", action: "delete_database", keys: "d"},
				},
				selected: 0,
				visible:  true,
				x:        x,
				y:        y,
				database: item.database,
			}
		default:
			m.contextMenu = &contextMenuModel{
				options:  []menuOption{{label: "Add table", action: "add_table", keys: "a"}},
				selected: 0,
				visible:  true,
				x:        x,
				y:        y,
				database: item.database,
			}
		}
	case item.kind == "table":
		// Server products add the same-level Add new table; SQLite keeps
		// its table-only menu.
		options := []menuOption{
			{label: "Rename table", action: "rename_table", keys: "r"},
			{label: "Delete table", action: "delete_table", keys: "d"},
		}
		menuDatabase := item.database
		if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
			options = []menuOption{
				{label: "Add new table", action: "add_table", keys: "a"},
				{label: "Edit table", action: "rename_table", keys: "r"},
				{label: "Delete table", action: "delete_table", keys: "d"},
			}
			if target, ok := m.schemaAddTarget(item); ok {
				// PostgreSQL creates tables inside the table's schema,
				// so the Add new table target is the schema.
				menuDatabase = target
			}
		}
		m.contextMenu = &contextMenuModel{
			options:  options,
			selected: 0,
			visible:  true,
			x:        x,
			y:        y,
			database: menuDatabase,
			table:    item.table,
		}
	}
}

func (m Model) schemaClick(contentY int) (tea.Model, tea.Cmd) {
	item, ok := m.schemaItemAt(contentY)
	if !ok {
		return m, nil
	}
	if item.kind == "schema" {
		key := m.schemaExpansionKey(item.database, item.schema)
		m.expandedSchemas[key] = !m.expandedSchemas[key]
		return m, m.rebuildSchemaTree()
	}
	if item.root {
		m.expandedDatabases[item.database] = !m.expandedDatabases[item.database]
		return m, m.rebuildSchemaTree()
	}
	return m, m.selectSchemaTable(item)
}

// queryLogTop returns the screen Y of the query log pane's title row:
// y=1 in the compact single-pane layout; below the rendered workspace pane
// (title + body + border) in the wide layout. The workspace view is
// re-measured because its rendered height can exceed workspaceHeight when
// content overflows; the inner render is cheap now (cached editor lexing,
// cached cell styles, linear segment crop).
func (m Model) queryLogTop() int {
	if m.compact {
		return 1
	}
	workspaceViewHeight := lipgloss.Height(m.workspaceView())
	return 3 + max(m.workspaceHeight-1, workspaceViewHeight)
}

func (m Model) focusQueryLogClick(x, contentY int) (tea.Model, tea.Cmd) {
	// contentY is relative to the pane title row: title at 0, the table
	// header line at 1, and data rows from 2.
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
	rowY := contentY - 2
	if rowY < 0 || rowY >= m.queryLog.Height() {
		return m, nil
	}
	rows := m.queryLog.Rows()
	start := min(max(m.queryLog.Cursor()-m.queryLog.Height()+1, 0), max(len(rows)-m.queryLog.Height(), 0))
	if row := start + rowY; row < len(rows) {
		m.queryLog.SetCursor(row)
		cellX := x - m.workspaceLeft() - 1 + m.queryLogOffset
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

// scrollForm scrolls the active form on mouse wheel. Forms whose view is
// clipped by huh's group viewport (column, connection) move field focus —
// huh pins its viewport to the focused field — while viewport-sliced forms
// (browse, filter, indexes, foreign keys) advance their scroll offset
// without moving focus.
func (m Model) scrollForm(wheel tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	step := 0
	switch wheel.Button {
	case tea.MouseWheelDown:
		step = 1
	case tea.MouseWheelUp:
		step = -1
	default:
		return m, nil
	}
	if m.State == stateConnection {
		if m.connection.focus == connectionFocusForm && m.connection.form != nil {
			if step > 0 {
				return m, m.connection.form.NextField()
			}
			return m, m.connection.form.PrevField()
		}
		return m, nil
	}
	switch {
	case m.columnForm.active():
		if step > 0 {
			return m, m.columnForm.nextField()
		}
		return m, m.columnForm.previousField()
	case m.browseFilterForm != nil:
		m.browseFilterForm.scrollOffset = clamp(m.browseFilterForm.scrollOffset+step, 0, len(m.browseFilterForm.fields)+1)
	case m.browseForm.active():
		m.browseForm.scrollOffset = formScrollOffset(m.browseForm.View(), m.browseForm.scrollOffset, step, m.formViewportHeight())
	case m.indexForm.active():
		m.indexForm.scrollOffset = formScrollOffset(m.indexForm.View(), m.indexForm.scrollOffset, step, m.formViewportHeight())
	case m.foreignKeyForm.active():
		m.foreignKeyForm.scrollOffset = formScrollOffset(m.foreignKeyForm.View(), m.foreignKeyForm.scrollOffset, step, m.formViewportHeight())
	}
	return m, nil
}

// formScrollOffset advances a form view offset by step, clamped to the last
// line that keeps the window full; the offset is unchanged when the view
// fits the viewport.
func formScrollOffset(view string, offset, step, height int) int {
	lines := len(strings.Split(view, "\n"))
	if lines <= height {
		return offset
	}
	return clamp(offset+step, 0, lines-height)
}

// handleSchemaTableClick handles left-click on structure, indexes, or foreignKeys tables.
// Row-level selection; double-click opens the edit form for the row.
func (m Model) handleSchemaTableClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State != stateReady || m.Focus != focusWorkspace || m.contextMenu != nil {
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
	workspaceX := absX - 1 // Skip pane left border.
	if !m.compact {
		workspaceX = max(absX-m.schemaWidth, 0) - 1
	}
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

	// Non-vim mode: the clicked table owns focus, so leave any text editing.
	if !m.vimMode {
		m.formMode.mode = formModeNormal
		m.editor.text.Blur()
		targetTable.Focus()
	}

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

	browseX := absX - 1 // Skip pane left border.
	if !m.compact {
		browseX = max(absX-m.schemaWidth, 0) - 1
	}
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

	// Non-vim mode: the clicked table owns focus, so leave any text editing.
	if !m.vimMode {
		m.formMode.mode = formModeNormal
		m.editor.text.Blur()
		targetTable.Focus()
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
	if contentY < 0 {
		return m, nil
	}

	inSchema := !m.compact && absX < m.schemaWidth
	if m.compact {
		inSchema = m.Focus == focusSchema
	}
	if inSchema {
		item, ok := m.schemaItemAt(contentY)
		if !ok {
			// Blank sidebar space on server products offers creating a
			// database; the keyboard path shares this helper.
			if m.supportsCreateDatabase() {
				m.openBlankServerMenu(absX, absY+1)
				return m, nil
			}
			item, ok = m.schema.SelectedItem().(schemaItem)
			if !ok || item.database == "" {
				return m, nil
			}
			item = schemaItem{database: item.database, root: true}
		}
		m.openSchemaItemMenu(item, absX, absY+1)
		return m, nil
	}
	if m.compact && m.Focus != focusWorkspace {
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
