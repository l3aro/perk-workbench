package workbench

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type querySucceededMsg struct {
	requestID uint64
	result    sharedsql.Result
}

type queryFailedMsg struct {
	requestID uint64
	err       error
}

type queryCanceledMsg struct{ requestID uint64 }

type tableInfoMsg struct {
	table   string
	columns []sharedsql.ColumnInfo
	err     error
}

type browseTableMsg struct {
	table  string
	page   int
	result sharedsql.Result
	err    error
}

type columnAlteredMsg struct{ err error }

type browseRowUpdatedMsg struct{ err error }

func (m Model) startQuery() (tea.Model, tea.Cmd) {
	query, ok := m.Workflow.StartQuery(m.appContext, m.editor.textarea.Value())
	if !ok {
		return m, nil
	}
	m.running, m.activeRequestID, m.cancelRequested = true, query.RequestID, false
	m.cancel = func() { m.Workflow.CancelQuery() }
	return m, func() tea.Msg {
		result, err := query.Service.Execute(query.Context, query.Statement)
		if err == nil {
			return querySucceededMsg{requestID: query.RequestID, result: result}
		}
		if errors.Is(err, context.Canceled) {
			return queryCanceledMsg{requestID: query.RequestID}
		}
		return queryFailedMsg{requestID: query.RequestID, err: err}
	}
}

func (m *Model) cancelQuery() {
	if m.cancel != nil {
		m.cancel()
	}
	m.Workflow.CancelQuery()
	m.cancelRequested = true
}

func (m Model) updateQuerySuccess(message querySucceededMsg) (tea.Model, tea.Cmd) {
	if !m.Workflow.MatchesQuery(message.requestID) {
		return m, nil
	}
	canceled, quit := m.Workflow.FinishQuery()
	m.running, m.cancel, m.cancelRequested, m.pendingQuit = false, nil, false, false
	if canceled {
		m.Status = "query canceled"
	} else {
		m.setResults(message.result)
	}
	if quit {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateQueryFailure(message queryFailedMsg) (tea.Model, tea.Cmd) {
	if !m.Workflow.MatchesQuery(message.requestID) {
		return m, nil
	}
	_, quit := m.Workflow.FinishQuery()
	m.running, m.cancel, m.cancelRequested, m.pendingQuit = false, nil, false, false
	m.Status = safeText(fmt.Sprintf("query failed: %v", message.err))
	if quit {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateQueryCanceled(message queryCanceledMsg) (tea.Model, tea.Cmd) {
	if !m.Workflow.MatchesQuery(message.requestID) {
		return m, nil
	}
	_, quit := m.Workflow.FinishQuery()
	m.running, m.cancel, m.cancelRequested, m.pendingQuit = false, nil, false, false
	m.Status = "query canceled"
	if quit {
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) setResults(result sharedsql.Result) {
	m.resultsNumericColumns = numericColumns(result.ColumnTypes)
	titles := make([]string, len(result.Columns))
	for index, column := range result.Columns {
		titles[index] = safeText(column)
	}
	rows := make([]table.Row, len(result.Rows))
	for rowIndex, row := range result.Rows {
		cells := make(table.Row, len(row))
		for cellIndex, cell := range row {
			if cell == nil {
				cells[cellIndex] = "NULL"
			} else {
				cells[cellIndex] = safeText(*cell)
			}
		}
		rows[rowIndex] = cells
	}
	m.results.SetRows(nil)
	m.results.SetColumns(tableColumns(titles, rows))
	resizeResultsTable(&m.results, m.tableViewportWidth, m.results.Height()+1)
	m.results.SetRows(rows)
	m.resultsOffset = 0
	m.results.Focus()
	m.editor.textarea.Blur()
	rowLabel := "rows"
	if len(rows) == 1 {
		rowLabel = "row"
	}
	affectedLabel := "rows"
	if result.RowsAffected == 1 {
		affectedLabel = "row"
	}
	m.Status = fmt.Sprintf("%d %s | %d %s affected | %s", len(rows), rowLabel, result.RowsAffected, affectedLabel, result.Duration)
	if result.Truncated {
		m.Status += " | truncated"
	}
}

func (m Model) loadTableInfo() tea.Cmd {
	tableName, service := m.SelectedTable, m.Database
	return func() tea.Msg {
		columns, err := service.TableInfo(m.appContext, tableName)
		return tableInfoMsg{table: tableName, columns: columns, err: err}
	}
}

func (m Model) loadBrowse() tea.Cmd {
	tableName, page, service := m.SelectedTable, m.BrowsePage, m.Database
	return func() tea.Msg {
		result, err := service.BrowseTable(m.appContext, tableName, page*browsePageSize, browsePageSize)
		return browseTableMsg{table: tableName, page: page, result: result, err: err}
	}
}

func (m Model) alterColumn() tea.Cmd {
	table, service := m.SelectedTable, m.Database
	change, err := m.columnForm.change()
	if err != nil {
		return func() tea.Msg { return columnAlteredMsg{err: err} }
	}
	return func() tea.Msg {
		return columnAlteredMsg{err: service.AlterColumn(m.appContext, table, change)}
	}
}

func (m Model) updateBrowseRow() tea.Cmd {
	statement, err := m.browseForm.updateStatement(m.SelectedTable)
	if err != nil {
		return func() tea.Msg { return browseRowUpdatedMsg{err: err} }
	}
	service := m.Database
	return func() tea.Msg {
		result, err := service.Execute(m.appContext, statement)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return browseRowUpdatedMsg{err: err}
	}
}

func (m Model) updateTableInfo(message tableInfoMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("loading structure: %v", message.err))
		}
		return m, nil
	}
	rows := make([]table.Row, len(message.columns))
	for index, column := range message.columns {
		defaultValue := "NULL"
		if column.DefaultValue != nil {
			defaultValue = safeText(*column.DefaultValue)
		}
		nullable := booleanValue(column.Nullable)
		rows[index] = table.Row{safeText(column.Name), indexIcons(column.Indexes), safeText(column.Type), nullable, defaultValue}
	}
	m.structure.SetColumns(tableColumns([]string{"Column", "Indexes", "Type", "Nullable", "Default"}, rows))
	resizeResultsTable(&m.structure, m.tableViewportWidth, m.structure.Height()+1)
	m.structure.SetRows(rows)
	m.structureColumns = message.columns
	m.structureOffset = 0
	return m, nil
}

func (m Model) updateColumnAltered(message columnAlteredMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.columnForm.saving = false
		m.Status = safeText(fmt.Sprintf("updating column: %v", message.err))
		return m, nil
	}
	m.columnForm = columnForm{}
	m.Status = "column updated"
	return m, tea.Batch(m.loadTableInfo(), m.loadBrowse())
}

func (m Model) updateBrowseRowUpdated(message browseRowUpdatedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.browseForm.saving = false
		m.Status = safeText(fmt.Sprintf("updating row: %v", message.err))
		return m, nil
	}
	m.browseForm = browseForm{}
	m.Status = "row updated"
	return m, m.loadBrowse()
}

func (m Model) updateBrowse(message browseTableMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.page != m.BrowsePage || message.err != nil {
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("loading browse: %v", message.err))
		}
		return m, nil
	}
	m.setBrowse(message.result)
	m.Status = fmt.Sprintf("%s | page %d | %d rows", safeText(message.table), message.page+1, len(message.result.Rows))
	return m, nil
}

func (m *Model) setBrowse(result sharedsql.Result) {
	cursor := m.browse.Cursor()
	m.browseResult = result
	m.browseNumericColumns = numericColumns(result.ColumnTypes)
	titles := make([]string, len(result.Columns))
	for index, column := range result.Columns {
		titles[index] = safeText(column)
	}
	rows := make([]table.Row, len(result.Rows))
	for rowIndex, row := range result.Rows {
		cells := make(table.Row, len(row))
		for cellIndex, cell := range row {
			if cell == nil {
				cells[cellIndex] = "NULL"
			} else {
				cells[cellIndex] = safeText(*cell)
			}
		}
		rows[rowIndex] = cells
	}
	m.browse.SetRows(nil)
	m.browse.SetColumns(tableColumns(titles, rows))
	resizeResultsTable(&m.browse, m.tableViewportWidth, m.browse.Height()+1)
	m.browse.SetRows(rows)
	if cursor >= 0 && len(rows) > 0 {
		m.browse.SetCursor(min(cursor, len(rows)-1))
	}
	m.browseOffset = 0
}

func numericColumns(types []string) []bool {
	columns := make([]bool, len(types))
	for index, typeName := range types {
		columns[index] = sharedsql.IsNumericColumnType(typeName)
	}
	return columns
}
