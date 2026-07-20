package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bubble-workbench/internal/sqlite"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

type querySucceededMsg struct {
	requestID uint64
	result    sqlite.Result
}

type queryFailedMsg struct {
	requestID uint64
	err       error
}

type queryCanceledMsg struct{ requestID uint64 }

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

func (m *Model) setResults(result sqlite.Result) {
	columns := make([]table.Column, len(result.Columns))
	columnWidth := max((m.editorWidth-8)/max(len(columns), 1), 1)
	for index, column := range result.Columns {
		columns[index] = table.Column{Title: safeText(column), Width: columnWidth}
	}
	if len(columns) > 0 {
		m.results.SetColumns(columns)
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
