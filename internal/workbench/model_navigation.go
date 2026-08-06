package workbench

import (
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

func moveTableCell(resultTable *table.Model, selectedColumn, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	switch keyPress.Key().Code {
	case tea.KeyUp, 'k':
		resultTable.SetCursor(max(resultTable.Cursor()-1, 0))
		return true
	case tea.KeyDown, 'j':
		resultTable.SetCursor(min(resultTable.Cursor()+1, max(len(resultTable.Rows())-1, 0)))
		return true
	case tea.KeyLeft, 'h':
		columns := resultTable.Columns()
		if len(columns) > 0 {
			*selectedColumn = min(max(*selectedColumn, 0), len(columns)-1)
			*selectedColumn = max(*selectedColumn-1, 0)
			revealTableColumn(*resultTable, *selectedColumn, offset, viewportWidth)
		}
	case tea.KeyRight, 'l':
		columns := resultTable.Columns()
		if len(columns) > 0 {
			*selectedColumn = min(max(*selectedColumn, 0), len(columns)-1)
			*selectedColumn = min(*selectedColumn+1, len(columns)-1)
			revealTableColumn(*resultTable, *selectedColumn, offset, viewportWidth)
		}
	default:
		return false
	}
	return true
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
			if columnStart < *offset {
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
	m.browseSettings = browseSettings{}
	m.structureColumns = nil
	m.foreignKeyInfo = nil
	m.referencingForeignKeyInfo = nil
	m.relationshipDiagram = false
	m.browsePending = true
	m.focusActiveTable()
	return tea.Batch(m.loadTableInfo(), m.loadIndexes(), m.loadForeignKeys(), m.loadReferencingForeignKeys())
}

func (m *Model) toggleTab(forward bool) tea.Cmd {
	m.Workflow.ToggleTab(forward)
	m.focusActiveTable()
	return m.loadPendingBrowse()
}

func (m *Model) loadPendingBrowse() tea.Cmd {
	if !m.browsePending || m.Tab != tabBrowse {
		return nil
	}
	m.browsePending = false
	return m.loadBrowse()
}

func (m *Model) focusActiveTable() {
	m.editor.text.Blur()
	m.blurTables()
	switch m.Tab {
	case tabStructure:
		m.structure.Focus()
	case tabBrowse:
		m.browse.Focus()
	case tabSQL:
		if !m.vimMode {
			// No modal modes: the editor is the SQL tab's text target, so
			// typing works the moment the tab gains focus. The focus cmd is
			// dropped by design; Focused is set synchronously.
			m.formMode.beginInsert(m.editor)
			return
		}
		if len(m.results.Rows()) > 0 {
			m.results.Focus()
		}
	case tabIndexes:
		m.indexes.Focus()
	case tabForeignKeys:
		m.foreignKeys.Focus()
	}
}

func (m *Model) blurTables() {
	m.structure.Blur()
	m.browse.Blur()
	m.results.Blur()
	m.indexes.Blur()
	m.foreignKeys.Blur()
	m.queryLog.Blur()
	m.chat.input.Blur()
}

func (m *Model) cycleFocus(forward bool) {
	m.editor.text.Blur()
	m.blurTables()
	m.queryLogPendingG = false

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
		m.queryLog.Focus()
		if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
			m.queryLog.SetCursor(0)
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

func (m *Model) scrollActiveWorkspaceTable(step int) {
	switch m.Tab {
	case tabStructure:
		rows := m.structure.Rows()
		newCursor := clamp(m.structure.Cursor()+step, 0, max(len(rows)-1, 0))
		m.structure.SetCursor(newCursor)
	case tabBrowse:
		rows := m.browse.Rows()
		newCursor := clamp(m.browse.Cursor()+step, 0, max(len(rows)-1, 0))
		m.browse.SetCursor(newCursor)
	case tabSQL:
		rows := m.results.Rows()
		newCursor := clamp(m.results.Cursor()+step, 0, max(len(rows)-1, 0))
		m.results.SetCursor(newCursor)
	case tabIndexes:
		rows := m.indexes.Rows()
		newCursor := clamp(m.indexes.Cursor()+step, 0, max(len(rows)-1, 0))
		m.indexes.SetCursor(newCursor)
	case tabForeignKeys:
		rows := m.foreignKeys.Rows()
		newCursor := clamp(m.foreignKeys.Cursor()+step, 0, max(len(rows)-1, 0))
		m.foreignKeys.SetCursor(newCursor)
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
