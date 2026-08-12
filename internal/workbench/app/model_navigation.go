package app

import (
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
)

// expandSchemaLevel expands the selected node when collapsed (with the
// accordion animation) or moves the cursor to its first child when already
// expanded. Leaves are a no-op.
func (m Model) expandSchemaLevel() (tea.Model, tea.Cmd) {
	component, cmd := m.schema.component.SchemaExpand(m.schemaSnapshot())
	m.schema.component = component
	return m, cmd
}

// collapseSchemaLevel collapses the selected node when expanded (with the
// accordion animation) or moves the cursor up to its parent row: the schema
// for a PostgreSQL table, the database root for anything else. Roots that
// are already collapsed are a no-op.
func (m Model) collapseSchemaLevel() (tea.Model, tea.Cmd) {
	component, cmd := m.schema.component.SchemaCollapse(m.schemaSnapshot())
	m.schema.component = component
	return m, cmd
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

func (m *Model) toggleTab(forward bool) tea.Cmd {
	m.Workflow.ToggleTab(forward)
	m.focusActiveTable()
	return m.loadPendingBrowse()
}

func (m *Model) loadPendingBrowse() tea.Cmd {
	if !m.browse.component.Pending || m.Tab != tabBrowse {
		return nil
	}
	m.browse.component.Pending = false
	return m.loadBrowse()
}

func (m *Model) focusActiveTable() {
	m.queryLog.editor.text.Blur()
	m.blurTables()
	switch m.Tab {
	case tabStructure:
		m.schema.component.Structure.Table.Focus()
	case tabBrowse:
		m.browse.component.Table.Focus()
	case tabSQL:
		if !m.vimMode {
			// No modal modes: the editor is the SQL tab's text target, so
			// typing works the moment the tab gains focus. The focus cmd is
			// dropped by design; Focused is set synchronously.
			beginInsert(m.overlay.formMode, m.queryLog.editor)
			return
		}
		if len(m.queryLog.results.Rows()) > 0 {
			m.queryLog.results.Focus()
		}
	case tabIndexes:
		m.schema.component.Structure.Indexes.Focus()
	case tabForeignKeys:
		m.schema.component.Structure.ForeignKeys.Focus()
	}
}

func (m *Model) blurTables() {
	m.schema.component.Structure.Table.Blur()
	m.browse.component.Table.Blur()
	m.queryLog.results.Blur()
	m.schema.component.Structure.Indexes.Blur()
	m.schema.component.Structure.ForeignKeys.Blur()
	m.queryLog.component.Blur()
	m.chat.component.Input.Blur()
}

func (m *Model) cycleFocus(forward bool) {
	m.queryLog.editor.text.Blur()
	m.blurTables()
	m.queryLog.component.ClearPendingG()

	focusCount := focus(3)
	if m.chat.component.Visible {
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
		m.queryLog.component.Focus()
		m.queryLog.component.EnsureCursor()
	case focusChat:
		m.chat.component.ChatMode = chat.ModeNormal
		if !m.vimMode {
			// Focus is set synchronously; the cursor cmd is dropped.
			m.chat.component.ChatMode = chat.ModeInsert
			m.chat.component.Input.Focus()
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
		moveTableColumn(&m.browse.component.Table, &m.browse.component.SelectedColumn, &m.browse.component.Offset, m.layout.tableViewportWidth, step)
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
		resultTable, offset = &m.schema.component.Structure.Table, &m.layout.structureOffset
	case tabIndexes:
		resultTable, offset = &m.schema.component.Structure.Indexes, &m.layout.indexesOffset
	case tabForeignKeys:
		resultTable, offset = &m.schema.component.Structure.ForeignKeys, &m.layout.foreignKeysOffset
	default:
		return
	}
	*offset = tableOffset(*resultTable, *offset+step*mouseHorizontalStep, m.layout.tableViewportWidth)
}

func (m *Model) scrollActiveWorkspaceTable(step int) {
	switch m.Tab {
	case tabStructure:
		rows := m.schema.component.Structure.Table.Rows()
		newCursor := clamp(m.schema.component.Structure.Table.Cursor()+step, 0, max(len(rows)-1, 0))
		m.schema.component.Structure.Table.SetCursor(newCursor)
	case tabBrowse:
		rows := m.browse.component.Table.Rows()
		newCursor := clamp(m.browse.component.Table.Cursor()+step, 0, max(len(rows)-1, 0))
		m.browse.component.Table.SetCursor(newCursor)
		m.refreshBrowseStatus()
	case tabSQL:
		rows := m.queryLog.results.Rows()
		newCursor := clamp(m.queryLog.results.Cursor()+step, 0, max(len(rows)-1, 0))
		m.queryLog.results.SetCursor(newCursor)
	case tabIndexes:
		rows := m.schema.component.Structure.Indexes.Rows()
		newCursor := clamp(m.schema.component.Structure.Indexes.Cursor()+step, 0, max(len(rows)-1, 0))
		m.schema.component.Structure.Indexes.SetCursor(newCursor)
	case tabForeignKeys:
		rows := m.schema.component.Structure.ForeignKeys.Rows()
		newCursor := clamp(m.schema.component.Structure.ForeignKeys.Cursor()+step, 0, max(len(rows)-1, 0))
		m.schema.component.Structure.ForeignKeys.SetCursor(newCursor)
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
