package workbench

import (
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

// expandSchemaLevel expands the selected node when collapsed (with the
// accordion animation) or moves the cursor to its first child when already
// expanded. Leaves are a no-op.
func (m Model) expandSchemaLevel() (tea.Model, tea.Cmd) {
	item, ok := m.schema.list.SelectedItem().(schemaItem)
	if !ok {
		return m, nil
	}
	switch {
	case item.root:
		if m.schema.expandedDatabases[item.database] {
			return m.schemaSelectFirstChild(item)
		}
		return m, treeToggleCmd(m.toggleDatabase(item.database), m.rebuildSchemaTree())
	case item.kind == "schema":
		if m.schema.expandedSchemas[m.schemaExpansionKey(item.database, item.schema)] {
			return m.schemaSelectFirstChild(item)
		}
		return m, treeToggleCmd(m.toggleSchema(item.database, item.schema), m.rebuildSchemaTree())
	default:
		return m, nil // table/view leaf
	}
}

// collapseSchemaLevel collapses the selected node when expanded (with the
// accordion animation) or moves the cursor up to its parent row: the schema
// for a PostgreSQL table, the database root for anything else. Roots that
// are already collapsed are a no-op.
func (m Model) collapseSchemaLevel() (tea.Model, tea.Cmd) {
	item, ok := m.schema.list.SelectedItem().(schemaItem)
	if !ok {
		return m, nil
	}
	switch {
	case item.root:
		if !m.schema.expandedDatabases[item.database] {
			return m, nil
		}
		return m, treeToggleCmd(m.toggleDatabase(item.database), m.rebuildSchemaTree())
	case item.kind == "schema":
		key := m.schemaExpansionKey(item.database, item.schema)
		if m.schema.expandedSchemas[key] {
			return m, treeToggleCmd(m.toggleSchema(item.database, item.schema), m.rebuildSchemaTree())
		}
		return m.schemaSelectParent(item)
	default:
		return m.schemaSelectParent(item)
	}
}

// schemaSelectFirstChild moves the cursor to the first visible child row of
// an expanded node: the next row when it belongs to the node's subtree.
func (m Model) schemaSelectFirstChild(item schemaItem) (tea.Model, tea.Cmd) {
	items := m.schema.list.Items()
	index := m.schema.list.Index()
	if index+1 >= len(items) {
		return m, nil
	}
	next, ok := items[index+1].(schemaItem)
	if !ok || next.database != item.database {
		return m, nil
	}
	if item.kind == "schema" && next.schema != item.schema {
		return m, nil
	}
	m.schema.list.Select(index + 1)
	return m, nil
}

// schemaSelectParent moves the cursor to the parent row of the selected
// item: the schema row for a PostgreSQL table, the database root otherwise.
func (m Model) schemaSelectParent(item schemaItem) (tea.Model, tea.Cmd) {
	items := m.schema.list.Items()
	for index := m.schema.list.Index() - 1; index >= 0; index-- {
		parent, ok := items[index].(schemaItem)
		if !ok || parent.database != item.database {
			continue
		}
		if m.databaseInfo.Product == "PostgreSQL" && (item.kind == "table" || item.kind == "view") {
			if parent.kind == "schema" && parent.schema == item.schema {
				m.schema.list.Select(index)
				return m, nil
			}
			continue
		}
		if parent.root {
			m.schema.list.Select(index)
			return m, nil
		}
	}
	return m, nil
}

func moveTableCell(resultTable *table.Model, selectedColumn, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	switch keyPress.Key().Code {
	case tea.KeyUp, 'k':
		resultTable.SetCursor(max(resultTable.Cursor()-1, 0))
		return true
	case tea.KeyDown, 'j':
		resultTable.SetCursor(min(resultTable.Cursor()+1, max(len(resultTable.Rows())-1, 0)))
		return true
	case tea.KeyLeft, 'h':
		moveTableColumn(resultTable, selectedColumn, offset, viewportWidth, -1)
	case tea.KeyRight, 'l':
		moveTableColumn(resultTable, selectedColumn, offset, viewportWidth, 1)
	default:
		return false
	}
	return true
}

// moveTableColumn moves the selected column one step in dir (-1 left, +1
// right), clamped to the column range, and reveals it in the viewport.
func moveTableColumn(resultTable *table.Model, selectedColumn, offset *int, viewportWidth, dir int) {
	columns := resultTable.Columns()
	if len(columns) == 0 {
		return
	}
	*selectedColumn = clamp(*selectedColumn, 0, len(columns)-1)
	*selectedColumn = clamp(*selectedColumn+dir, 0, len(columns)-1)
	revealTableColumn(*resultTable, *selectedColumn, offset, viewportWidth)
}

func moveTableRow(resultTable *table.Model, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	switch keyPress.Key().Code {
	case tea.KeyUp, 'k':
		resultTable.SetCursor(max(resultTable.Cursor()-1, 0))
	case tea.KeyDown, 'j':
		resultTable.SetCursor(min(resultTable.Cursor()+1, max(len(resultTable.Rows())-1, 0)))
	case tea.KeyLeft, 'h':
		*offset = tableOffset(*resultTable, *offset-max(viewportWidth/2, 1), viewportWidth)
	case tea.KeyRight, 'l':
		*offset = tableOffset(*resultTable, *offset+max(viewportWidth/2, 1), viewportWidth)
	default:
		return false
	}
	return true
}

func revealTableColumn(resultTable table.Model, selectedColumn int, offset *int, viewportWidth int) {
	columns := resultTable.Columns()
	if len(columns) == 0 {
		*offset = 0
		return
	}

	selectedColumn = min(max(selectedColumn, 0), len(columns)-1)
	columnStart := 0
	for index, column := range columns {
		columnEnd := columnStart + column.Width + 2*spaceCompact
		if index == selectedColumn {
			if columnEnd-columnStart >= viewportWidth {
				// The column is wider than the viewport, so it cannot fit:
				// align its start so the view opens at the cell's head
				// instead of pinning the viewport to its tail.
				*offset = columnStart
			} else if columnStart < *offset {
				*offset = columnStart
			} else if columnEnd > *offset+viewportWidth {
				*offset = columnEnd - viewportWidth
			}
			*offset = tableOffset(resultTable, *offset, viewportWidth)
			return
		}
		columnStart = columnEnd
	}
}

func (m *Model) selectSchemaTable(item schemaItem) tea.Cmd {
	m.SelectTable(m.schemaTable(item))
	// The landing tab is configurable; SelectTable defaults to the
	// Structure (columns) tab.
	m.Tab = tableOpenTargetTab()
	m.browse.settings = browseSettings{}
	m.structure.columns = nil
	m.structure.foreignKeyInfo = nil
	m.structure.referencingForeignKeyInfo = nil
	m.structure.relationshipDiagram = false
	m.browse.pending = true
	m.focusActiveTable()
	return tea.Batch(m.rebuildSchemaTree(), m.loadTableInfo(), m.loadIndexes(), m.loadForeignKeys(), m.loadReferencingForeignKeys(), m.loadPendingBrowse())
}

func (m *Model) toggleTab(forward bool) tea.Cmd {
	m.Workflow.ToggleTab(forward)
	m.focusActiveTable()
	return m.loadPendingBrowse()
}

func (m *Model) loadPendingBrowse() tea.Cmd {
	if !m.browse.pending || m.Tab != tabBrowse {
		return nil
	}
	m.browse.pending = false
	return m.loadBrowse()
}

func (m *Model) focusActiveTable() {
	m.queryLog.editor.text.Blur()
	m.blurTables()
	switch m.Tab {
	case tabStructure:
		m.structure.table.Focus()
	case tabBrowse:
		m.browse.table.Focus()
	case tabSQL:
		if !m.vimMode {
			// No modal modes: the editor is the SQL tab's text target, so
			// typing works the moment the tab gains focus. The focus cmd is
			// dropped by design; Focused is set synchronously.
			m.overlay.formMode.beginInsert(m.queryLog.editor)
			return
		}
		if len(m.queryLog.results.Rows()) > 0 {
			m.queryLog.results.Focus()
		}
	case tabIndexes:
		m.structure.indexes.Focus()
	case tabForeignKeys:
		m.structure.foreignKeys.Focus()
	}
}

func (m *Model) blurTables() {
	m.structure.table.Blur()
	m.browse.table.Blur()
	m.queryLog.results.Blur()
	m.structure.indexes.Blur()
	m.structure.foreignKeys.Blur()
	m.queryLog.table.Blur()
	m.chat.input.Blur()
}

func (m *Model) cycleFocus(forward bool) {
	m.queryLog.editor.text.Blur()
	m.blurTables()
	m.queryLog.pendingG = false

	focusCount := focus(3)
	if m.chat.visible {
		focusCount++
	}
	if forward {
		m.Focus = (m.Focus + 1) % focusCount
	} else {
		m.Focus = (m.Focus + focusCount - 1) % focusCount
	}

	switch m.Focus {
	case focusSchema:
	case focusWorkspace:
		m.focusActiveTable()
	case focusQueryLog:
		m.queryLog.table.Focus()
		if len(m.queryLog.table.Rows()) > 0 && m.queryLog.table.Cursor() < 0 {
			m.queryLog.table.SetCursor(0)
		}
	case focusChat:
		m.chat.chatMode = formModeNormal
		if !m.vimMode {
			// Focus is set synchronously; the cursor cmd is dropped.
			m.chat.chatMode = formModeInsert
			m.chat.input.Focus()
		}
	}
}

// mouseHorizontalStep is the number of columns a horizontal wheel tick
// pans on row-based tables, matching the bubbles viewport's default
// horizontal step.
const mouseHorizontalStep = 6

// scrollActiveWorkspaceTableHorizontal moves the active tab's table
// horizontally on a wheel tick: cell-based tables (browse, results) travel
// the selected column like the h/l keys, while row-based tables (structure,
// indexes, foreign keys), which have no column selection, pan the viewport.
func (m *Model) scrollActiveWorkspaceTableHorizontal(step int) {
	switch m.Tab {
	case tabBrowse:
		moveTableColumn(&m.browse.table, &m.layout.browseColumn, &m.layout.browseOffset, m.layout.tableViewportWidth, step)
		m.refreshBrowseStatus()
		return
	case tabSQL:
		moveTableColumn(&m.queryLog.results, &m.layout.resultsColumn, &m.layout.resultsOffset, m.layout.tableViewportWidth, step)
		return
	}
	var resultTable *table.Model
	var offset *int
	switch m.Tab {
	case tabStructure:
		resultTable, offset = &m.structure.table, &m.layout.structureOffset
	case tabIndexes:
		resultTable, offset = &m.structure.indexes, &m.layout.indexesOffset
	case tabForeignKeys:
		resultTable, offset = &m.structure.foreignKeys, &m.layout.foreignKeysOffset
	default:
		return
	}
	*offset = tableOffset(*resultTable, *offset+step*mouseHorizontalStep, m.layout.tableViewportWidth)
}

func (m *Model) scrollActiveWorkspaceTable(step int) {
	switch m.Tab {
	case tabStructure:
		rows := m.structure.table.Rows()
		newCursor := clamp(m.structure.table.Cursor()+step, 0, max(len(rows)-1, 0))
		m.structure.table.SetCursor(newCursor)
	case tabBrowse:
		rows := m.browse.table.Rows()
		newCursor := clamp(m.browse.table.Cursor()+step, 0, max(len(rows)-1, 0))
		m.browse.table.SetCursor(newCursor)
		m.refreshBrowseStatus()
	case tabSQL:
		rows := m.queryLog.results.Rows()
		newCursor := clamp(m.queryLog.results.Cursor()+step, 0, max(len(rows)-1, 0))
		m.queryLog.results.SetCursor(newCursor)
	case tabIndexes:
		rows := m.structure.indexes.Rows()
		newCursor := clamp(m.structure.indexes.Cursor()+step, 0, max(len(rows)-1, 0))
		m.structure.indexes.SetCursor(newCursor)
	case tabForeignKeys:
		rows := m.structure.foreignKeys.Rows()
		newCursor := clamp(m.structure.foreignKeys.Cursor()+step, 0, max(len(rows)-1, 0))
		m.structure.foreignKeys.SetCursor(newCursor)
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
