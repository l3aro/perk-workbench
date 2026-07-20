package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func (m Model) startQuery() (tea.Model, tea.Cmd) {
	statement := strings.TrimSpace(m.editor.textarea.Value())
	if statement == "" || m.running {
		return m, nil
	}
	m.requestID++
	m.activeRequestID = m.requestID
	m.running, m.cancelRequested = true, false
	m.queryContext, m.cancel = context.WithCancel(m.appContext)
	m.status = "running query"
	requestID, service, queryContext, cancel := m.activeRequestID, m.service, m.queryContext, m.cancel
	return m, func() tea.Msg {
		defer cancel()
		result, err := service.Execute(queryContext, statement)
		if err == nil {
			return querySucceededMsg{requestID: requestID, result: result}
		}
		if errors.Is(err, context.Canceled) {
			return queryCanceledMsg{requestID: requestID}
		}
		return queryFailedMsg{requestID: requestID, err: err}
	}
}

func (m *Model) cancelQuery() {
	if m.cancelRequested {
		return
	}
	m.cancelRequested = true
	m.status = "canceling query"
	m.cancel()
}

func (m Model) updateQuerySuccess(message querySucceededMsg) (tea.Model, tea.Cmd) {
	if !m.matchQuery(message.requestID) {
		return m, nil
	}
	canceled, quit := m.finishQuery()
	if canceled {
		m.status = "query canceled"
	} else {
		m.setResults(message.result)
	}
	if quit {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateQueryFailure(message queryFailedMsg) (tea.Model, tea.Cmd) {
	if !m.matchQuery(message.requestID) {
		return m, nil
	}
	_, quit := m.finishQuery()
	m.status = safeText(fmt.Sprintf("query failed: %v", message.err))
	if quit {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateQueryCanceled(message queryCanceledMsg) (tea.Model, tea.Cmd) {
	if !m.matchQuery(message.requestID) {
		return m, nil
	}
	_, quit := m.finishQuery()
	m.status = "query canceled"
	if quit {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) matchQuery(requestID uint64) bool {
	return m.running && m.activeRequestID == requestID
}

func (m *Model) finishQuery() (bool, bool) {
	canceled, quit := m.cancelRequested, m.pendingQuit
	m.running, m.cancelRequested, m.pendingQuit, m.queryContext, m.cancel = false, false, false, nil, nil
	return canceled, quit
}

func (m *Model) setResults(result sharedsql.Result) {
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
	m.results.SetColumns(tableColumns(m.results.Width(), titles))
	m.results.SetRows(rows)
	rowLabel := "rows"
	if len(rows) == 1 {
		rowLabel = "row"
	}
	affectedLabel := "rows"
	if result.RowsAffected == 1 {
		affectedLabel = "row"
	}
	m.status = fmt.Sprintf("%d %s | %d %s affected | %s", len(rows), rowLabel, result.RowsAffected, affectedLabel, result.Duration)
	if result.Truncated {
		m.status += " | truncated"
	}
}

func (m Model) loadTableInfo() tea.Cmd {
	tableName, service := m.selectedTable, m.service
	return func() tea.Msg {
		columns, err := service.TableInfo(m.appContext, tableName)
		return tableInfoMsg{table: tableName, columns: columns, err: err}
	}
}

func (m Model) loadBrowse() tea.Cmd {
	tableName, page, service := m.selectedTable, m.browsePage, m.service
	return func() tea.Msg {
		result, err := service.BrowseTable(m.appContext, tableName, page*browsePageSize, browsePageSize)
		return browseTableMsg{table: tableName, page: page, result: result, err: err}
	}
}

func (m Model) updateTableInfo(message tableInfoMsg) (tea.Model, tea.Cmd) {
	if message.table != m.selectedTable || message.err != nil {
		if message.err != nil {
			m.status = safeText(fmt.Sprintf("loading structure: %v", message.err))
		}
		return m, nil
	}
	rows := make([]table.Row, len(message.columns))
	for index, column := range message.columns {
		defaultValue := "NULL"
		if column.DefaultValue != nil {
			defaultValue = safeText(*column.DefaultValue)
		}
		nullable := "yes"
		if !column.Nullable {
			nullable = "no"
		}
		primaryKey := ""
		if column.PrimaryKey > 0 {
			primaryKey = fmt.Sprintf("%d", column.PrimaryKey)
		}
		rows[index] = table.Row{safeText(column.Name), safeText(column.Type), nullable, defaultValue, primaryKey}
	}
	m.structure.SetColumns(tableColumns(m.structure.Width(), []string{"Column", "Type", "Nullable", "Default", "PK"}))
	m.structure.SetRows(rows)
	return m, nil
}

func (m Model) updateBrowse(message browseTableMsg) (tea.Model, tea.Cmd) {
	if message.table != m.selectedTable || message.page != m.browsePage || message.err != nil {
		if message.err != nil {
			m.status = safeText(fmt.Sprintf("loading browse: %v", message.err))
		}
		return m, nil
	}
	m.setBrowse(message.result)
	m.status = fmt.Sprintf("%s | page %d | %d rows", safeText(message.table), message.page+1, len(message.result.Rows))
	return m, nil
}

func (m *Model) setBrowse(result sharedsql.Result) {
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
	m.browse.SetColumns(tableColumns(m.browse.Width(), titles))
	m.browse.SetRows(rows)
}
