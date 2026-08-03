package workbench

import (
	"context"
	"errors"
	"strings"
	"time"

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
		exec := query.Service.Execute
		if m.ReadOnly {
			exec = query.Service.ExecuteReadOnly
		}
		result, err := exec(query.Context, query.Statement)
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

func (m *Model) recallQueryHistory(direction int) bool {
	if len(m.queryHistory) == 0 {
		return false
	}
	if direction > 0 {
		if m.historyIndex == -1 {
			m.historyIndex = 0
		} else if m.historyIndex < len(m.queryHistory)-1 {
			m.historyIndex++
		} else {
			return false // already at the oldest entry; never wrap
		}
	} else {
		if m.historyIndex <= 0 {
			if m.historyIndex == -1 {
				return false // Down outside recall mode
			}
			m.historyIndex = -1
		} else {
			m.historyIndex--
		}
	}
	if m.historyIndex == -1 {
		m.editor.setValue("")
	} else {
		m.editor.setValue(m.queryHistory[m.historyIndex])
	}
	return true
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
	// Schema may have changed (DDL); recheck the editor value against it.
	m.editorValidity = sqlValidityPending
	return m, m.scheduleSQLValidation()
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

const sqlValidationDebounce = 250 * time.Millisecond

type sqlValidity uint8

const (
	sqlValidityPending sqlValidity = iota
	sqlValidityValid
	sqlValidityInvalid
)

// sqlValidationTickMsg fires after a quiet period following an editor change;
// the tag drops superseded ticks so only the latest edit is validated.
type sqlValidationTickMsg struct{ tag uint64 }

type sqlValidationMsg struct {
	statement string
	err       error
}

// scheduleSQLValidation debounces validation after the editor value changes.
func (m *Model) scheduleSQLValidation() tea.Cmd {
	m.sqlValidationTag++
	tag := m.sqlValidationTag
	return tea.Tick(sqlValidationDebounce, func(time.Time) tea.Msg {
		return sqlValidationTickMsg{tag: tag}
	})
}

// validateSQL prepares the current editor value against the open database.
func (m Model) validateSQL(statement string) tea.Cmd {
	ctx, cancel := context.WithTimeout(m.appContext, 2*time.Second)
	return func() tea.Msg {
		defer cancel()
		return sqlValidationMsg{statement: statement, err: m.Database.Validate(ctx, statement)}
	}
}

func (m Model) updateSQLValidationTick(message sqlValidationTickMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.sqlValidationTag || m.Database == nil {
		return m, nil
	}
	statement := strings.TrimSpace(m.editor.value)
	if statement == "" {
		m.editorValidity = sqlValidityPending
		return m, nil
	}
	return m, m.validateSQL(m.editor.value)
}

func (m Model) updateSQLValidation(message sqlValidationMsg) (tea.Model, tea.Cmd) {
	if m.editor.value != message.statement {
		return m, nil // stale result for an older revision
	}
	if message.err != nil {
		m.editorValidity = sqlValidityInvalid
	} else {
		m.editorValidity = sqlValidityValid
	}
	return m, nil
}
