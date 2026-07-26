package workbench

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type querySucceededMsg struct {
	requestID uint64
	statement string
	startedAt time.Time
	result    sharedsql.Result
}

type queryFailedMsg struct {
	requestID uint64
	statement string
	startedAt time.Time
	err       error
}

type queryCanceledMsg struct {
	requestID uint64
	statement string
	startedAt time.Time
}

func (m Model) startQuery() (tea.Model, tea.Cmd) {
	return m.startQueryStatement(m.editor.value)
}

func (m Model) startQueryStatement(statement string) (tea.Model, tea.Cmd) {
	query, ok := m.Workflow.StartQuery(m.appContext, statement)
	if !ok {
		return m, nil
	}
	m.recordQueryHistory(query.Statement)
	startedAt := time.Now()
	return m, func() tea.Msg {
		result, err := query.Service.Execute(query.Context, query.Statement)
		if err == nil {
			return querySucceededMsg{requestID: query.RequestID, statement: query.Statement, startedAt: startedAt, result: result}
		}
		if errors.Is(err, context.Canceled) {
			return queryCanceledMsg{requestID: query.RequestID, statement: query.Statement, startedAt: startedAt}
		}
		return queryFailedMsg{requestID: query.RequestID, statement: query.Statement, startedAt: startedAt, err: err}
	}
}

func (m *Model) recordQueryHistory(statement string) {
	m.queryHistory = append([]string{statement}, m.queryHistory...)
	if len(m.queryHistory) > queryLogLimit {
		m.queryHistory = m.queryHistory[:queryLogLimit]
	}
	m.historyIndex = -1
}

func (m *Model) recallQueryHistory() bool {
	if len(m.queryHistory) == 0 {
		return false
	}
	m.historyIndex = (m.historyIndex + 1) % len(m.queryHistory)
	m.editor.setValue(m.queryHistory[m.historyIndex])
	return true
}

func (m *Model) saveQuery() (bool, error) {
	statement := m.editor.value
	if strings.TrimSpace(statement) == "" {
		return false, nil
	}
	if utf8.RuneCountInString(statement) > maxSavedQueryRunes {
		return false, nil
	}
	for _, saved := range m.savedQueries {
		if saved == statement {
			return false, nil
		}
	}
	queries := append(append([]string(nil), m.savedQueries...), statement)
	if len(queries) > maxSavedQueries {
		queries = queries[len(queries)-maxSavedQueries:]
	}
	if err := saveSavedQueries(m.savedQueriesPath, queries); err != nil {
		return false, err
	}
	m.savedQueries = queries
	return true, nil
}

func (m *Model) cancelQuery() {
	m.Workflow.CancelQuery()
}

func (m Model) updateQuerySuccess(message querySucceededMsg) (tea.Model, tea.Cmd) {
	if !m.Workflow.MatchesQuery(message.requestID) {
		return m, nil
	}
	canceled, quit := m.Workflow.FinishQuery()
	if canceled {
		m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: time.Since(message.startedAt), message: "canceled", status: "canceled"})
	} else {
		m.setResults(message.result)
		if message.statement != "" && len(message.result.Rows) > 0 {
			m.results.SetCursor(0)
		}
		m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: message.result.Duration, message: queryLogMessage(message.statement, message.result.RowsAffected, len(message.result.Rows)), status: "success"})
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
	m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: time.Since(message.startedAt), message: message.err.Error(), status: "failed"})
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
	m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: time.Since(message.startedAt), message: "canceled", status: "canceled"})
	if quit {
		return m, tea.Quit
	}
	return m, nil
}
