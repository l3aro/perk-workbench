package workbench

import "charm.land/bubbles/v2/table"

// formViewportHeight computes the viewport height for forms in the workspace.
func (m Model) formViewportHeight() int {
	if m.compact {
		return max(m.height-9, 1)
	}
	return max(m.workspaceHeight-5, 1)
}

func (m *Model) layout(width, height int) {
	previousViewportWidth := m.tableViewportWidth
	m.width, m.height = max(width, 0), max(height, 0)
	contentHeight := max(m.height-4, 0)
	minimumWidth := compactWidth
	if m.State == stateReady && m.chat.visible {
		minimumWidth = compactWidth + chatPaneWidth
	}
	m.compact = m.fullscreen || m.width < minimumWidth || m.height < 24
	m.schemaWidth, m.editorWidth, m.chatWidth = m.width, m.width, m.width
	m.workspaceHeight, m.queryLogHeight = contentHeight, 0
	if m.compact {
		m.queryLogHeight = contentHeight
	}
	if !m.compact {
		m.schemaWidth = 30
		if m.State == stateReady && m.chat.visible {
			m.chatWidth = chatPaneWidth
			m.editorWidth = max(m.width-m.schemaWidth-m.chatWidth-4, 1)
		} else {
			m.chatWidth = 0
			m.editorWidth = max(m.width-m.schemaWidth-2, 1)
		}
		m.queryLogHeight = min(queryLogPaneHeight, contentHeight)
		m.workspaceHeight = contentHeight - m.queryLogHeight
	}
	m.editorHeight = min(m.workspaceHeight, sqlEditorRows+2)
	m.resultsHeight = max(m.workspaceHeight-m.editorHeight, 0)
	m.schema.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.picker.SetSize(max(m.width-2, 0), max(contentHeight-2, 0))
	connectionWidth := m.width
	if !m.compact {
		connectionWidth = m.editorWidth
	}
	m.connection.setWidth(max(connectionWidth-4, 1))
	m.recent.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.editor.setSize(max(m.editorWidth-4, 1), max(m.editorHeight-2, 1))
	m.resizeChat()
	m.tableViewportWidth = max(m.editorWidth-4, 1)
	if m.compact {
		m.tableViewportWidth = max(m.editorWidth-6, 1)
	} else {
		m.tableViewportWidth = max(m.editorWidth-8, 1)
	}
	m.columnForm.setWidth(m.tableViewportWidth)
	m.columnForm.setHeight(m.formViewportHeight())
	m.browseForm.setWidth(m.tableViewportWidth)
	m.indexForm.setWidth(m.tableViewportWidth)
	m.foreignKeyForm.setWidth(m.tableViewportWidth)
	if m.explainPicker != nil {
		m.explainPicker.setWidth(m.tableViewportWidth)
	}
	if m.tableViewportWidth != previousViewportWidth {
		for _, resultTable := range []*table.Model{&m.results, &m.structure, &m.browse, &m.indexes, &m.foreignKeys, &m.queryLog} {
			columns := resultTable.Columns()
			titles := make([]string, len(columns))
			for index, column := range columns {
				titles[index] = column.Title
			}
			resultTable.SetColumns(tableColumns(titles, resultTable.Rows()))
		}
	}
	resizeResultsTable(&m.results, m.tableViewportWidth, max(m.resultsHeight-4, 2))
	resizeResultsTable(&m.queryLog, m.tableViewportWidth, max(m.queryLogHeight-5, 2))
	resizeResultsTable(&m.structure, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	resizeResultsTable(&m.browse, m.tableViewportWidth, max(m.workspaceHeight-5, 2))
	resizeResultsTable(&m.indexes, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	resizeResultsTable(&m.foreignKeys, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	revealTableColumn(m.structure, m.structureColumn, &m.structureOffset, m.tableViewportWidth)
	revealTableColumn(m.browse, m.browseColumn, &m.browseOffset, m.tableViewportWidth)
	revealTableColumn(m.results, m.resultsColumn, &m.resultsOffset, m.tableViewportWidth)
	revealTableColumn(m.indexes, m.indexesColumn, &m.indexesOffset, m.tableViewportWidth)
	revealTableColumn(m.foreignKeys, m.foreignKeysColumn, &m.foreignKeysOffset, m.tableViewportWidth)
	revealTableColumn(m.queryLog, m.queryLogColumn, &m.queryLogOffset, m.tableViewportWidth)
}
