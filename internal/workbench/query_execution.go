package workbench

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
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
	query, ok := m.Workflow.StartQuery(m.appContext, m.editor.textarea.Value())
	if !ok {
		return m, nil
	}
	startedAt := time.Now()
	m.running, m.activeRequestID, m.cancelRequested = true, query.RequestID, false
	m.cancel = func() { m.Workflow.CancelQuery() }
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
		m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: time.Since(message.startedAt)})
		m.Status = "query canceled"
	} else {
		m.setResults(message.result)
		m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: message.result.Duration, fetched: len(message.result.Rows)})
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
	m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: time.Since(message.startedAt)})
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
	m.appendQueryLog(queryLogEntry{startedAt: message.startedAt, statement: message.statement, duration: time.Since(message.startedAt)})
	m.Status = "query canceled"
	if quit {
		return m, tea.Quit
	}
	return m, nil
}
