package workbench

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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
				if !m.chat.component.KeepInsert {
					m.chat.component.ChatMode = chat.ModeNormal
				}
				m.chat.component.KeepInsert = false
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
		if m.chat.component.Visible && x >= m.layout.schemaWidth+m.layout.editorWidth {
			m.Focus = focusChat
			m.queryLog.component.ClearPendingG()
			m.queryLog.editor.text.Blur()
			m.blurTables()
			// A double-click press just entered insert mode; the trailing
			// release must not reset it to normal.
			if !m.chat.component.KeepInsert {
				m.chat.component.ChatMode = chat.ModeNormal
			}
			m.chat.component.KeepInsert = false
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
		if x < m.layout.schemaWidth && m.connection.component.Form.Focus != connectionFocusRecent {
			m.connection.component.Form.Focus = connectionFocusRecent
			return m, nil
		}
		if x >= m.layout.schemaWidth && m.connection.component.Form.Focus != connectionFocusForm {
			m.connection.component.Form.Focus = connectionFocusForm
			m.connection.component.RecentFilter.Blur()
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
	items := m.connection.component.Recent.VisibleItems()
	start, end := m.connection.component.Recent.Paginator.GetSliceBounds(len(items))
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
	items := m.connection.component.Recent.VisibleItems()
	start, _ := m.connection.component.Recent.Paginator.GetSliceBounds(len(items))
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
		m.connection.component.RecentFilter.Focus()
		return m, nil
	}
	m.connection.component.RecentFilter.Blur()
	index, ok := m.recentItemOnPage(contentY)
	if !ok {
		return m, nil
	}
	m.connection.component.Recent.Select(index)
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
	m.connection.component.Recent.Select(index)
	m.connection.component.RecentFilter.Blur()
	m.connection.component.Form.Focus = connectionFocusRecent
	m.openRecentConnectionMenu(absX, absY+1)
	return m, nil
}

func (m Model) handleWorkspaceClick(x, y int) (tea.Model, tea.Cmd) {
	m.schema.component.Filter.Blur()
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

// schemaClick maps a schema-pane click to its item. A double-click on a
// PostgreSQL root that is not the connected database reconnects to it
// (matching the recent list's double-click-to-load); any other root or
// schema click toggles the subtree, and a table click selects it. The
// component owns the click mapping; the root supplies the double-click
// detector and applies the returned events.
func (m Model) schemaClick(x, contentY int) (tea.Model, tea.Cmd) {
	component, event, cmd := m.schema.component.HandleSchemaClick(x, contentY, m.schemaLayout(), m.schemaSnapshot(), m.recordFormClick)
	m.schema.component = component
	return m.applySchemaEvent(event, cmd)
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
		if m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Huh != nil {
			if step > 0 {
				return m, m.connection.component.Form.Huh.NextField()
			}
			return m, m.connection.component.Form.Huh.PrevField()
		}
		return m, nil
	}
	switch {
	case m.schema.component.Structure.ColumnForm.Active():
		if step > 0 {
			return m, m.schema.component.Structure.ColumnForm.NextField()
		}
		return m, m.schema.component.Structure.ColumnForm.PreviousField()
	case m.browse.component.FilterForm != nil:
		m.browse.component.FilterForm.ScrollOffset = clamp(m.browse.component.FilterForm.ScrollOffset+step, 0, len(m.browse.component.FilterForm.Fields)+1)
	case m.browse.component.DocumentEditor != nil:
		m.browse.component.DocumentEditor.ScrollOffset = formScrollOffset(m.browse.component.DocumentEditor.View(), m.browse.component.DocumentEditor.ScrollOffset, step, m.formViewportHeight())
	case m.browse.component.Form.Active():
		m.browse.component.Form.ScrollOffset = formScrollOffset(m.browse.component.Form.View(), m.browse.component.Form.ScrollOffset, step, m.formViewportHeight())
	case m.schema.component.Structure.IndexForm.Active():
		m.schema.component.Structure.IndexForm.ScrollOffset = formScrollOffset(m.schema.component.Structure.IndexForm.View(), m.schema.component.Structure.IndexForm.ScrollOffset, step, m.formViewportHeight())
	case m.schema.component.Structure.ForeignKeyForm.Active():
		m.schema.component.Structure.ForeignKeyForm.ScrollOffset = formScrollOffset(m.schema.component.Structure.ForeignKeyForm.View(), m.schema.component.Structure.ForeignKeyForm.ScrollOffset, step, m.formViewportHeight())
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
// handleSchemaTableClick handles left-click on structure, indexes, or
// foreignKeys tables. Row-level selection; double-click opens the edit
// form for the row. The component owns the click mapping and the
// double-click detection; the root keeps the form-mode bookkeeping and the
// form openers.
func (m Model) handleSchemaTableClick(absX, absY int) (tea.Model, tea.Cmd) {
	if m.State != stateReady || m.Focus != focusWorkspace || m.overlay.contextMenu != nil {
		return m, nil
	}
	clock := schema.ClickClock{Time: m.layout.lastClickTime, X: m.layout.lastClickX, Y: m.layout.lastClickY, Tab: m.layout.lastClickTab, Row: m.layout.lastClickRow}
	component, event, cmd, clock, hit := m.schema.component.HandleTableClick(absX, absY, m.workspaceLayout(), m.Tab, m.vimMode, m.layout.schemaWidth, m.layout.compact, clock, m.schemaSnapshot())
	m.schema.component = component
	m.layout.lastClickTime, m.layout.lastClickX, m.layout.lastClickY, m.layout.lastClickTab, m.layout.lastClickRow = clock.Time, clock.X, clock.Y, clock.Tab, clock.Row
	if hit && !m.vimMode {
		// Non-vim mode: the clicked table owns focus, so leave any text
		// editing.
		m.overlay.formMode.Mode = formModeNormal
		m.queryLog.editor.text.Blur()
	}
	if _, ok := event.(schema.TableRowActivated); ok {
		switch m.Tab {
		case tabStructure:
			return m, m.openColumnForm()
		case tabIndexes:
			if index := m.schema.component.SelectedIndex(); index != nil {
				return m, m.openIndexForm(index)
			}
		case tabForeignKeys:
			if foreignKey := m.schema.component.SelectedForeignKey(); foreignKey != nil {
				return m, m.openForeignKeyForm(foreignKey)
			}
		}
		return m, nil
	}
	return m, cmd
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
	pagerRow := m.browse.component.Table.Height() + 6
	if m.browseStatusSplit() {
		pagerRow++
	}
	if m.Tab == tabBrowse && contentY == pagerRow && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
		pager := m.browsePager()
		browseX := absX - 1
		if !m.layout.compact {
			browseX = max(absX-m.layout.schemaWidth, 0) - 1
		}
		if pager.PrevEnabled && browseX >= pager.PrevStart && browseX < pager.PrevStart+ansi.StringWidth(pager.Prev) {
			return m.pagerBrowseCommand(-1)
		}
		if pager.NextEnabled && browseX >= pager.NextStart && browseX < pager.NextStart+ansi.StringWidth(pager.Next) {
			return m.pagerBrowseCommand(1)
		}
	}

	// Determine which table tab we're on and which table to target.
	switch m.Tab {
	case tabBrowse:
		if m.browse.component.Form.Active() || m.browse.component.FilterForm != nil || len(m.browse.component.Table.Rows()) == 0 {
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
		targetTable = &m.browse.component.Table
		targetCol = &m.browse.component.SelectedColumn
		targetOffset = &m.browse.component.Offset
		rows = m.browse.component.Table.Rows()
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
		m.browse.component.SelectedColumn = col
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
		m.overlay.formMode.Mode = formModeNormal
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
		if m.layout.compact && m.connection.component.Form.Focus != connectionFocusRecent {
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
		component, menu, ok := m.schema.component.HandleSchemaRightClick(absX, absY, m.schemaLayout(), m.schemaSnapshot())
		m.schema.component = component
		if ok {
			m.openSchemaComponentMenu(menu)
		}
		return m, nil
	}
	if m.layout.compact && m.Focus != focusWorkspace {
		return m, nil
	}

	// Only show context menu on browse table (tabBrowse) when form isn't active.
	if !(m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active()) {
		return m, nil
	}
	rows := m.browse.component.Table.Rows()
	if len(rows) == 0 {
		return m, nil
	}

	// Map to row.
	browseLine := contentY - 3
	if browseLine < 1 {
		return m, nil
	}
	rowHeight := m.browse.component.Table.Height()
	start := min(max(m.browse.component.Table.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + browseLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil
	}

	// Select the row and build context menu.
	m.browse.component.Table.SetCursor(dataRow)
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
