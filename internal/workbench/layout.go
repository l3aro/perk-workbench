package workbench

import "charm.land/bubbles/v2/table"

// formViewportHeight computes the viewport height for forms in the workspace.
// Editable forms reserve rows for actions, their separating gap, and the mode hint.
func (m Model) formViewportHeight() int {
	if m.layout.compact {
		return max(m.layout.height-11, 1)
	}
	return max(m.layout.workspaceHeight-7, 1)
}

func (m *Model) applyLayout(width, height int) {
	previousViewportWidth := m.layout.tableViewportWidth
	m.layout.width, m.layout.height = max(width, 0), max(height, 0)
	contentHeight := max(m.layout.height-4, 0)
	minimumWidth := compactWidth
	if m.State == stateReady && m.chat.visible {
		minimumWidth = compactWidth + chatPaneWidth
	}
	m.layout.compact = m.layout.fullscreen || m.layout.width < minimumWidth || m.layout.height < 24
	m.layout.schemaWidth, m.layout.editorWidth, m.layout.chatWidth = m.layout.width, m.layout.width, m.layout.width
	m.layout.workspaceHeight, m.layout.queryLogHeight = contentHeight, 0
	if m.layout.compact {
		m.layout.queryLogHeight = contentHeight
	}
	if !m.layout.compact {
		m.layout.schemaWidth = 44
		if m.State == stateReady && m.chat.visible {
			m.layout.chatWidth = chatPaneWidth
			m.layout.editorWidth = max(m.layout.width-m.layout.schemaWidth-m.layout.chatWidth-4, 1)
		} else {
			m.layout.chatWidth = 0
			m.layout.editorWidth = max(m.layout.width-m.layout.schemaWidth-2, 1)
		}
		m.layout.queryLogHeight = min(queryLogPaneHeight, contentHeight)
		m.layout.workspaceHeight = contentHeight - m.layout.queryLogHeight
	}
	m.layout.editorHeight = min(m.layout.workspaceHeight, sqlEditorRows+2)
	m.layout.resultsHeight = max(m.layout.workspaceHeight-m.layout.editorHeight, 0)
	schemaListHeight := max(contentHeight-2, 0)
	if m.schemaFilterShown() {
		// The filter box takes 3 rows; the list must yield two of its own.
		schemaListHeight = max(schemaListHeight-2, 0)
	}
	// The pane body content is two cells narrower than schemaWidth (border
	// + padding each side); sizing the list wider wraps every full-width
	// row onto a second line inside the bordered pane.
	m.schema.list.SetSize(max(m.layout.schemaWidth-6, 0), schemaListHeight)
	m.schema.filter.SetWidth(max(m.layout.schemaWidth-6, 0))
	m.connection.recentFilter.SetWidth(max(m.layout.schemaWidth-6, 0))
	m.connection.picker.SetSize(max(m.layout.width-2, 0), max(contentHeight-2, 0))
	connectionWidth := m.layout.width
	if !m.layout.compact {
		connectionWidth = max(m.layout.width-m.layout.schemaWidth, 1)
	}
	m.connection.form.setWidth(max(connectionWidth-4, 1))
	m.connection.form.setHeight(max(m.layout.height-8, 1))
	// The profiles list spans the pane body exactly; the filter box (3
	// rows, when the pane is wide enough) and the bottom hint line are
	// reserved above and below it (see recentPaneView).
	recentListHeight := max(contentHeight-3, 0)
	if m.schemaFilterShown() {
		recentListHeight = max(recentListHeight-3, 0)
	}
	m.connection.recent.SetSize(max(m.layout.schemaWidth-6, 0), recentListHeight)
	m.queryLog.editor.setSize(max(m.layout.editorWidth-8, 1), max(m.layout.editorHeight-4, 1))
	m.resizeChat()
	m.layout.tableViewportWidth = max(m.layout.editorWidth-4, 1)
	if m.layout.compact {
		m.layout.tableViewportWidth = max(m.layout.editorWidth-6, 1)
	} else {
		m.layout.tableViewportWidth = max(m.layout.editorWidth-8, 1)
	}
	m.structure.columnForm.setWidth(m.layout.tableViewportWidth)
	m.structure.columnForm.setHeight(m.formViewportHeight())
	m.structure.tableForm.setWidth(m.layout.tableViewportWidth)
	m.structure.tableForm.setHeight(m.formViewportHeight())
	m.browse.form.setWidth(m.layout.tableViewportWidth)
	if m.browse.filterForm != nil {
		m.browse.filterForm.setSize(m.layout.tableViewportWidth, m.formViewportHeight())
	}
	m.structure.indexForm.setWidth(m.layout.tableViewportWidth)
	m.structure.foreignKeyForm.setWidth(m.layout.tableViewportWidth)
	if m.overlay.explainPicker != nil {
		m.overlay.explainPicker.setWidth(m.layout.tableViewportWidth)
	}
	if m.layout.tableViewportWidth != previousViewportWidth {
		for _, resultTable := range []*table.Model{&m.queryLog.results, &m.structure.table, &m.browse.table, &m.structure.indexes, &m.structure.foreignKeys} {
			columns := resultTable.Columns()
			titles := make([]string, len(columns))
			for index, column := range columns {
				titles[index] = column.Title
			}
			resultTable.SetColumns(tableColumns(titles, resultTable.Rows()))
		}
		m.queryLog.component.RefreshViewport(m.layout.tableViewportWidth)
	}
	resizeResultsTable(&m.queryLog.results, m.layout.tableViewportWidth, max(m.layout.resultsHeight-5, 2))
	m.queryLog.component.Resize(queryLogLayout(*m))
	// The tab tables yield one row to the blank line that separates their
	// status line from the mode/tab-hint footer.
	resizeResultsTable(&m.structure.table, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-6, 2))
	// The browse table yields the footer rows below its data rows
	// (browseFooterRows: the status line, the footer gap, and the pager
	// button row, plus pane chrome), keeping the pane exactly full.
	resizeResultsTable(&m.browse.table, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-m.browseFooterRows(), 2))
	resizeResultsTable(&m.structure.indexes, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-6, 2))
	resizeResultsTable(&m.structure.foreignKeys, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-6, 2))
	m.layout.structureOffset = tableOffset(m.structure.table, m.layout.structureOffset, m.layout.tableViewportWidth)
	revealTableColumn(m.browse.table, m.layout.browseColumn, &m.layout.browseOffset, m.layout.tableViewportWidth)
	revealTableColumn(m.queryLog.results, m.layout.resultsColumn, &m.layout.resultsOffset, m.layout.tableViewportWidth)
	m.layout.indexesOffset = tableOffset(m.structure.indexes, m.layout.indexesOffset, m.layout.tableViewportWidth)
	m.layout.foreignKeysOffset = tableOffset(m.structure.foreignKeys, m.layout.foreignKeysOffset, m.layout.tableViewportWidth)
	m.queryLog.component.RevealColumn(m.layout.tableViewportWidth)
}
