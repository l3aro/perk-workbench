package workbench

import (
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
		// Header row: the I/O button pinned to the right opens the quit
		// confirmation dialog, the palette button left of it (separated by a
		// fixed gap) opens the command palette; the margin cell between the
		// quit button and the right edge does nothing. Both buttons share
		// one width. Callers only route here when no overlay is open. The
		// dialog's actions are safe in every state (Disconnect just closes
		// the database if one is open), so unlike the Ctrl+Q keybinding it
		// opens regardless of form or running state.
		width := headerButtonWidth()
		quitX := m.layout.width - headerRightMargin - width
		if x >= quitX && x < quitX+width {
			return m.openQuitDialog(), nil
		}
		if x >= quitX-headerButtonGap-width && x < quitX-headerButtonGap {
			m.overlay.commandPalette = newCommandPalette(m)
			m.overlay.commandPalette.visible = true
		}
		return m, nil
	}
	if m.hasOverlay() {
		return m, nil
	}
	switch m.State {
	case stateReady:
		if m.layout.compact {
			// Single-pane layout: route by the focused pane, which renders
			// full-width with its title row at y=1 (contentY=0).
			contentY := y - 1
			if contentY < 1 {
				return m, nil
			}
			switch m.Focus {
			case focusSchema:
				return m.schemaClick(x, contentY)
			case focusWorkspace:
				return m.handleWorkspaceClick(x, contentY)
			case focusQueryLog:
				return m.focusQueryLogClick(x, contentY)
			case focusChat:
				if !m.chat.keepInsert {
					m.chat.chatMode = formModeNormal
				}
				m.chat.keepInsert = false
				return m, nil
			}
			return m, nil
		}
		// Content starts at y=1 (after header). Schema on left, workspace+query log on right.
		contentY := y - 1
		if contentY < 0 {
			return m, nil
		}
		if x < m.layout.schemaWidth {
			if m.Focus != focusSchema {
				m.Focus = focusSchema
				m.queryLog.component.ClearPendingG()
				m.queryLog.editor.text.Blur()
				m.blurTables()
			}
			return m.schemaClick(x, contentY)
		}
		if m.chat.visible && x >= m.layout.schemaWidth+m.layout.editorWidth {
			m.Focus = focusChat
			m.queryLog.component.ClearPendingG()
			m.queryLog.editor.text.Blur()
			m.blurTables()
			// A double-click press just entered insert mode; the trailing
			// release must not reset it to normal.
			if !m.chat.keepInsert {
				m.chat.chatMode = formModeNormal
			}
			m.chat.keepInsert = false
			return m, nil
		}
		if contentY < m.layout.workspaceHeight {
			workspaceX := max(x-m.layout.schemaWidth, 0)
			return m.handleWorkspaceClick(workspaceX, contentY)
		}
		// contentY is relative to the pane title row: the title sits at
		// queryLogTop, so subtract queryLogTop-1 to make it relative to 0.
		return m.focusQueryLogClick(x, contentY-m.queryLogTop()+1)
	case stateConnection:
		if m.layout.compact {
			return m, nil
		}
		if x < m.layout.schemaWidth && m.connection.form.focus != connectionFocusRecent {
			m.connection.form.focus = connectionFocusRecent
			return m, nil
		}
		if x >= m.layout.schemaWidth && m.connection.form.focus != connectionFocusForm {
			m.connection.form.focus = connectionFocusForm
			m.connection.recentFilter.Blur()
		}
		return m, nil
	case statePicking:
		// Full-width picker: same list header layout (TitleBar 2 lines + StatusBar 2 lines).
		// Items start at contentY=5. Default delegate uses Height=2, Spacing=1 (3 lines per item).
		itemLine := y - 1 - 5
		if itemLine >= 0 {
			itemOnPage := itemLine / 3
			items := m.connection.picker.VisibleItems()
			start, end := m.connection.picker.Paginator.GetSliceBounds(len(items))
			if start+itemOnPage < end {
				m.connection.picker.Select(start + itemOnPage)
				if item, ok := m.connection.picker.SelectedItem().(pickerItem); ok {
					return m, selectPickerItem(item.raw)
				}
			}
		}
		return m, nil
	case stateFailure:
		m.RecoverToPicker("choose another database")
		return m, readDirectory(m.connection.pickerDir)
	}
	return m, nil
}

// recentItemOnPage maps a profiles-pane content Y to the list index of the
// profile rendered there: the pane top border at contentY=0, the filter box
// (3 rows, when the pane is wide enough) and the list's status bar sit above
// the items, and every item spans 3 lines (Height 2 + Spacing 1).
func (m Model) recentItemOnPage(contentY int) (int, bool) {
	itemOffset := 4
	if m.schemaFilterShown() {
		itemOffset = 6
	}
	itemLine := contentY - itemOffset
	if itemLine < 0 {
		return 0, false
	}
	itemOnPage := itemLine / 3
	items := m.connection.recent.VisibleItems()
	start, end := m.connection.recent.Paginator.GetSliceBounds(len(items))
	if start+itemOnPage >= end {
		return 0, false
	}
	return start + itemOnPage, true
}

// recentRowY returns the screen Y of the recent list item at the given
// index, clamped to the visible window.
func (m Model) recentRowY(index int) int {
	itemOffset := 4
	if m.schemaFilterShown() {
		itemOffset = 6
	}
	items := m.connection.recent.VisibleItems()
	start, _ := m.connection.recent.Paginator.GetSliceBounds(len(items))
	return max(itemOffset+(index-start)*3, itemOffset)
}

// openRecentConnectionMenu opens the profile context menu: Edit loads the
// selected profile into the connection form, Delete asks for confirmation.
func (m *Model) openRecentConnectionMenu(x, y int) {
	m.overlay.contextMenu = &contextMenuModel{
		options: []menuOption{
			{label: "Edit", action: "edit_profile", keys: "e"},
			{label: "Delete", action: "delete_profile", keys: "d"},
		},
		selected: 0,
		visible:  true,
		x:        x,
		y:        y,
		title:    "Profile actions",
	}
}

// handleRecentClick maps a click on the recent-connections list to its item:
// a single click selects the profile, a double click loads it into the
// connection form (matching the Enter keybinding). Presses only — the
// trailing release routes through handleLeftClick, which only switches pane
// focus.
func (m Model) handleRecentClick(x, y int) (tea.Model, tea.Cmd) {
	contentY := y - 1
	// Pane top border at contentY=0. The filter box (3 rows, when the pane
	// is wide enough) and the list's status bar sit above the items.
	// Clicking the box focuses the input; any other click leaves filter
	// editing so navigation keys work again.
	if m.schemaFilterShown() && contentY >= 1 && contentY <= 3 {
		m.connection.recentFilter.Focus()
		return m, nil
	}
	m.connection.recentFilter.Blur()
	index, ok := m.recentItemOnPage(contentY)
	if !ok {
		return m, nil
	}
	m.connection.recent.Select(index)
	if !m.recordFormClick(x, y) {
		return m, nil
	}
	// Double-click: load the profile into the form, matching Enter.
	return m, m.editSelectedRecentConnection()
}

// handleRecentRightClick maps a right-click on the profiles list to its item
// and opens the profile context menu; the item is selected first so Edit and
// Delete act on the clicked profile, like the browse table.
func (m Model) handleRecentRightClick(absX, absY int) (tea.Model, tea.Cmd) {
	contentY := absY - 1
	if contentY < 0 {
		return m, nil
	}
	if m.schemaFilterShown() && contentY >= 1 && contentY <= 3 {
		// The filter box is not a profile row.
		return m, nil
	}
	index, ok := m.recentItemOnPage(contentY)
	if !ok {
		return m, nil
	}
	m.connection.recent.Select(index)
	m.connection.recentFilter.Blur()
	m.connection.form.focus = connectionFocusRecent
	m.openRecentConnectionMenu(absX, absY+1)
	return m, nil
}

func (m Model) handleWorkspaceClick(x, y int) (tea.Model, tea.Cmd) {
	m.schema.filter.Blur()
	if m.Focus != focusWorkspace {
		m.Focus = focusWorkspace
		m.queryLog.component.ClearPendingG()
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

// schemaItemOffset returns the content Y of the first schema item line: the
// pane title row plus the 3-row filter box (when shown). The list's status
// bar is hidden, so nothing else precedes the items.
func (m Model) schemaItemOffset() int {
	if m.schemaFilterShown() {
		return 4
	}
	return 1
}

// openBlankServerMenu opens the blank-sidebar context menu: creating a
// database is valid on any server product and needs no selection.
func (m *Model) openBlankServerMenu(x, y int) {
	m.overlay.contextMenu = &contextMenuModel{
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
	items := m.schema.list.VisibleItems()
	if len(items) == 0 {
		return schemaItem{}, false
	}
	start, end := m.schema.list.Paginator.GetSliceBounds(len(items))
	if itemY >= end-start {
		return schemaItem{}, false
	}
	m.schema.list.Select(start + itemY)
	item, ok := m.schema.list.SelectedItem().(schemaItem)
	return item, ok
}

// schemaRowY returns the screen Y of the schema list item at the given
// index, clamped to the visible window.
func (m Model) schemaRowY(index int) int {
	itemOffset := m.schemaItemOffset()
	items := m.schema.list.VisibleItems()
	start, _ := m.schema.list.Paginator.GetSliceBounds(len(items))
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
		m.overlay.contextMenu = &contextMenuModel{
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
			m.overlay.contextMenu = &contextMenuModel{
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
			m.overlay.contextMenu = &contextMenuModel{
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
			m.overlay.contextMenu = &contextMenuModel{
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
		m.overlay.contextMenu = &contextMenuModel{
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

// schemaClick maps a schema-pane click to its item. A double-click on a
// PostgreSQL root that is not the connected database reconnects to it
// (matching the recent list's double-click-to-load); any other root or
// schema click toggles the subtree, and a table click selects it.
func (m Model) schemaClick(x, contentY int) (tea.Model, tea.Cmd) {
	// The filter box is the first body rows; clicking it focuses the
	// input for typing.
	if m.schemaFilterShown() && contentY >= 1 && contentY <= 3 {
		m.schema.filter.Focus()
		return m, nil
	}
	item, ok := m.schemaItemAt(contentY)
	if !ok {
		m.schema.filter.Blur()
		return m, nil
	}
	// Item clicks leave filter editing so navigation keys work again.
	m.schema.filter.Blur()
	if item.kind == "schema" {
		return m, treeToggleCmd(m.toggleSchema(item.database, item.schema), m.rebuildSchemaTree())
	}
	if item.root {
		if m.recordFormClick(x, contentY+1) && m.databaseInfo.Product == "PostgreSQL" && !m.databaseRootConnected(item.database) {
			return m.reconnectDatabase(item.database)
		}
		return m, treeToggleCmd(m.toggleDatabase(item.database), m.rebuildSchemaTree())
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
	if m.layout.compact {
		return 1
	}
	workspaceViewHeight := lipgloss.Height(m.workspaceView())
	return 3 + max(m.layout.workspaceHeight-1, workspaceViewHeight)
}

func (m Model) focusQueryLogClick(x, contentY int) (tea.Model, tea.Cmd) {
	// contentY is relative to the pane title row: title at 0, the table
	// header line at 1, and data rows from 2.
	if m.Focus != focusQueryLog {
		m.Focus = focusQueryLog
		m.queryLog.component.ClearPendingG()
		m.queryLog.editor.text.Blur()
		m.blurTables()
		m.queryLog.component.Table.Focus()
		if len(m.queryLog.component.Table.Rows()) > 0 && m.queryLog.component.Table.Cursor() < 0 {
			m.queryLog.component.Table.SetCursor(0)
		}
	}
	rowY := contentY - 2
	if rowY < 0 || rowY >= m.queryLog.component.Table.Height() {
		return m, nil
	}
	rows := m.queryLog.component.Table.Rows()
	start := min(max(m.queryLog.component.Table.Cursor()-m.queryLog.component.Table.Height()+1, 0), max(len(rows)-m.queryLog.component.Table.Height(), 0))
	if row := start + rowY; row < len(rows) {
		m.queryLog.component.Table.SetCursor(row)
		cellX := x - m.workspaceLeft() - 1 + m.queryLog.component.Offset
		for index, column := range m.queryLog.component.Table.Columns() {
			cellWidth := column.Width + 2*spaceCompact
			if cellX < cellWidth {
				m.queryLog.component.Column = index
				break
			}
			cellX -= cellWidth
		}
	}
	return m, nil
}

func (m Model) handleMouseWheel(wheel tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.layout.height < 4 || m.layout.width < 1 {
		return m, nil
	}
	// Forward wheel events to the focused area's table. Plain vertical
	// wheel moves rows; horizontal trackpad scroll (WheelLeft/Right) and
	// shift+vertical wheel travel columns on cell-based tables and pan
	// row-based ones.
	step := 0
	hStep := 0
	switch wheel.Button {
	case tea.MouseWheelDown:
		if wheel.Mod.Contains(tea.ModShift) {
			hStep = 1
		} else {
			step = 1
		}
	case tea.MouseWheelUp:
		if wheel.Mod.Contains(tea.ModShift) {
			hStep = -1
		} else {
			step = -1
		}
	case tea.MouseWheelLeft:
		hStep = -1
	case tea.MouseWheelRight:
		hStep = 1
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
		if hStep != 0 {
			m.scrollActiveWorkspaceTableHorizontal(hStep)
		} else {
			m.scrollActiveWorkspaceTable(step)
		}
	case focusQueryLog:
		if hStep != 0 {
			moveTableColumn(&m.queryLog.component.Table, &m.queryLog.component.Column, &m.queryLog.component.Offset, m.layout.tableViewportWidth, hStep)
			return m, nil
		}
		rows := m.queryLog.component.Table.Rows()
		rowCount := len(rows)
		if rowCount == 0 {
			return m, nil
		}
		newCursor := clamp(m.queryLog.component.Table.Cursor()+step, 0, rowCount-1)
		m.queryLog.component.Table.SetCursor(newCursor)
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
		if m.connection.form.focus == connectionFocusForm && m.connection.form.form != nil {
			if step > 0 {
				return m, m.connection.form.form.NextField()
			}
			return m, m.connection.form.form.PrevField()
		}
		return m, nil
	}
	switch {
	case m.structure.columnForm.active():
		if step > 0 {
			return m, m.structure.columnForm.nextField()
		}
		return m, m.structure.columnForm.previousField()
	case m.browse.filterForm != nil:
		m.browse.filterForm.scrollOffset = clamp(m.browse.filterForm.scrollOffset+step, 0, len(m.browse.filterForm.fields)+1)
	case m.browse.documentEditor != nil:
		m.browse.documentEditor.scrollOffset = formScrollOffset(m.browse.documentEditor.View(), m.browse.documentEditor.scrollOffset, step, m.formViewportHeight())
	case m.browse.form.active():
		m.browse.form.scrollOffset = formScrollOffset(m.browse.form.View(), m.browse.form.scrollOffset, step, m.formViewportHeight())
	case m.structure.indexForm.active():
		m.structure.indexForm.scrollOffset = formScrollOffset(m.structure.indexForm.View(), m.structure.indexForm.scrollOffset, step, m.formViewportHeight())
	case m.structure.foreignKeyForm.active():
		m.structure.foreignKeyForm.scrollOffset = formScrollOffset(m.structure.foreignKeyForm.View(), m.structure.foreignKeyForm.scrollOffset, step, m.formViewportHeight())
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
	if m.State != stateReady || m.Focus != focusWorkspace || m.overlay.contextMenu != nil {
		return m, nil
	}

	var targetTable *table.Model
	switch m.Tab {
	case tabStructure:
		if m.structure.columnForm.active() {
			return m, nil
		}
		targetTable = &m.structure.table
	case tabIndexes:
		if m.structure.indexForm.active() {
			return m, nil
		}
		targetTable = &m.structure.indexes
	case tabForeignKeys:
		if m.structure.foreignKeyForm.active() || m.structure.relationshipDiagram {
			return m, nil
		}
		targetTable = &m.structure.foreignKeys
	default:
		return m, nil
	}

	rows := targetTable.Rows()
	if len(rows) == 0 {
		return m, nil
	}

	// Workspace X: skip schema pane (left) and pane left border (1).
	workspaceX := absX - 1 // Skip pane left border.
	if !m.layout.compact {
		workspaceX = max(absX-m.layout.schemaWidth, 0) - 1
	}
	if workspaceX < 0 || workspaceX >= m.layout.tableViewportWidth {
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
		m.overlay.formMode.mode = formModeNormal
		m.queryLog.editor.text.Blur()
		targetTable.Focus()
	}

	// Check for double-click at the same position on the same tab and row:
	// open the row's edit form, matching the enter/i keybinding behavior.
	now := time.Now()
	if !m.layout.lastClickTime.IsZero() && now.Sub(m.layout.lastClickTime) < doubleClickTimeout &&
		m.layout.lastClickX == absX && m.layout.lastClickY == absY &&
		m.layout.lastClickTab == m.Tab && m.layout.lastClickRow == dataRow {
		m.layout.lastClickTime = time.Time{}
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
	m.layout.lastClickTime = now
	m.layout.lastClickX = absX
	m.layout.lastClickY = absY
	m.layout.lastClickTab = m.Tab
	m.layout.lastClickRow = dataRow
	return m, nil
}

// handleBrowseClick handles left-click on the browse or results table.
// A click on the browse header sorts by that column (like the s
// keybinding); a click on a data row selects the cell and detects
// double-click for inline editing.
func (m Model) handleBrowseClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State != stateReady || m.Focus != focusWorkspace || m.overlay.contextMenu != nil {
		return m, nil
	}

	contentY := absY - 1
	if contentY < 0 {
		return m, nil
	}

	// The button row under the browse status line hosts the Prev/Next
	// pager buttons. The browse view starts at contentY=3 (table header),
	// so the status line sits at Height()+4, the gap at Height()+5, and
	// the button row at Height()+6 — one row lower while the status line
	// splits onto two rows on a narrow viewport. This runs before the
	// table's rows-empty guard: on an empty page (e.g. the last page after
	// deletions) Prev is still enabled and must page back. Disabled
	// buttons share the row but ignore clicks.
	pagerRow := m.browse.table.Height() + 6
	if m.browseStatusSplit() {
		pagerRow++
	}
	if m.Tab == tabBrowse && contentY == pagerRow && !m.browse.form.active() && m.browse.filterForm == nil {
		pager := m.browsePager()
		browseX := absX - 1
		if !m.layout.compact {
			browseX = max(absX-m.layout.schemaWidth, 0) - 1
		}
		if pager.prevEnabled && browseX >= pager.prevStart && browseX < pager.prevStart+ansi.StringWidth(pager.prev) {
			return m.pagerBrowseCommand(-1)
		}
		if pager.nextEnabled && browseX >= pager.nextStart && browseX < pager.nextStart+ansi.StringWidth(pager.next) {
			return m.pagerBrowseCommand(1)
		}
	}

	// Determine which table tab we're on and which table to target.
	switch m.Tab {
	case tabBrowse:
		if m.browse.form.active() || m.browse.filterForm != nil || len(m.browse.table.Rows()) == 0 {
			return m, nil
		}
	case tabSQL:
		if len(m.queryLog.results.Rows()) == 0 {
			return m, nil
		}
	default:
		return m, nil
	}

	// The workspace pane has a 1-char border on each side.
	// Inside the pane: contentY=0 is border top, contentY=1 = tab row, contentY=2 = blank, contentY=3+ = browseView.
	browseLine := contentY - 3 // 0=header, 1..N=data rows

	var targetTable *table.Model
	var targetCol *int
	var targetOffset *int
	var rows []table.Row
	switch m.Tab {
	case tabBrowse:
		targetTable = &m.browse.table
		targetCol = &m.layout.browseColumn
		targetOffset = &m.layout.browseOffset
		rows = m.browse.table.Rows()
	case tabSQL:
		targetTable = &m.queryLog.results
		targetCol = &m.layout.resultsColumn
		targetOffset = &m.layout.resultsOffset
		rows = m.queryLog.results.Rows()
	}

	browseX := absX - 1 // Skip pane left border.
	if !m.layout.compact {
		browseX = max(absX-m.layout.schemaWidth, 0) - 1
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

	if browseLine == 0 {
		// Header click: sort by the clicked column, cycling exactly like
		// the s keybinding. Only the browse tab sorts; the results table
		// has no sort behavior.
		if m.Tab != tabBrowse {
			return m, nil
		}
		m.layout.browseColumn = col
		return m, m.cycleBrowseSort()
	}
	if browseLine < 1 {
		return m, nil // Clicked above the table header.
	}

	rowHeight := targetTable.Height()
	start := min(max(targetTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + browseLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil
	}

	// Non-vim mode: the clicked table owns focus, so leave any text editing.
	if !m.vimMode {
		m.overlay.formMode.mode = formModeNormal
		m.queryLog.editor.text.Blur()
		targetTable.Focus()
	}

	// Check for double-click at the same position.
	now := time.Now()
	if !m.layout.lastClickTime.IsZero() && now.Sub(m.layout.lastClickTime) < doubleClickTimeout &&
		m.layout.lastClickX == absX && m.layout.lastClickY == absY {
		// Double-click: open inline cell editor.
		targetTable.SetCursor(dataRow)
		*targetCol = col
		revealTableColumn(*targetTable, *targetCol, targetOffset, m.layout.tableViewportWidth)
		m.layout.lastClickTime = time.Time{}
		if m.Tab == tabBrowse {
			return m, m.openCellEditor()
		}
		return m, nil
	}

	// Single click: select the cell.
	m.layout.lastClickTime = now
	m.layout.lastClickX = absX
	m.layout.lastClickY = absY
	targetTable.SetCursor(dataRow)
	*targetCol = col
	revealTableColumn(*targetTable, *targetCol, targetOffset, m.layout.tableViewportWidth)
	return m, nil
}

func (m Model) handleRightClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State == stateConnection {
		// Profiles pane: the left pane in the wide layout, the focused
		// pane in compact. Right-clicking a profile opens its menu.
		if !m.layout.compact && absX >= m.layout.schemaWidth {
			return m, nil
		}
		if m.layout.compact && m.connection.form.focus != connectionFocusRecent {
			return m, nil
		}
		return m.handleRecentRightClick(absX, absY)
	}
	if m.State != stateReady {
		return m, nil
	}
	contentY := absY - 1
	if contentY < 0 {
		return m, nil
	}

	inSchema := !m.layout.compact && absX < m.layout.schemaWidth
	if m.layout.compact {
		inSchema = m.Focus == focusSchema
	}
	if inSchema {
		if m.schemaFilterShown() && contentY >= 1 && contentY <= 3 {
			// The filter box is not blank sidebar space.
			return m, nil
		}
		item, ok := m.schemaItemAt(contentY)
		if !ok {
			// Blank sidebar space on server products offers creating a
			// database; the keyboard path shares this helper.
			if m.supportsCreateDatabase() {
				m.openBlankServerMenu(absX, absY+1)
				return m, nil
			}
			item, ok = m.schema.list.SelectedItem().(schemaItem)
			if !ok || item.database == "" {
				return m, nil
			}
			item = schemaItem{database: item.database, root: true}
		}
		m.openSchemaItemMenu(item, absX, absY+1)
		return m, nil
	}
	if m.layout.compact && m.Focus != focusWorkspace {
		return m, nil
	}

	// Only show context menu on browse table (tabBrowse) when form isn't active.
	if !(m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active()) {
		return m, nil
	}
	rows := m.browse.table.Rows()
	if len(rows) == 0 {
		return m, nil
	}

	// Map to row.
	browseLine := contentY - 3
	if browseLine < 1 {
		return m, nil
	}
	rowHeight := m.browse.table.Height()
	start := min(max(m.browse.table.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + browseLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil
	}

	// Select the row and build context menu.
	m.browse.table.SetCursor(dataRow)
	m.refreshBrowseStatus()

	m.overlay.contextMenu = &contextMenuModel{
		options:  m.browseRowMenuOptions(),
		selected: 0,
		visible:  true,
		x:        absX,
		y:        absY + 1,
	}

	return m, nil
}
