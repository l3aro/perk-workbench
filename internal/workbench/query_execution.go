package workbench

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

type querySucceededMsg struct {
	requestID    uint64
	statement    string
	startedAt    time.Time
	result       sharedsql.Result
	reloadSchema bool
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
	return m.startQueryStatement(m.queryLog.editor.value, false)
}

// startQueryStatement runs a statement asynchronously; reloadSchema requests
// a sidebar refresh when the statement succeeds (table actions only).
func (m Model) startQueryStatement(statement string, reloadSchema bool) (tea.Model, tea.Cmd) {
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
			return querySucceededMsg{requestID: query.RequestID, statement: query.Statement, startedAt: startedAt, result: result, reloadSchema: reloadSchema}
		}
		if errors.Is(err, context.Canceled) {
			return queryCanceledMsg{requestID: query.RequestID, statement: query.Statement, startedAt: startedAt}
		}
		return queryFailedMsg{requestID: query.RequestID, statement: query.Statement, startedAt: startedAt, err: err}
	}
}

func (m *Model) recordQueryHistory(statement string) {
	m.queryLog.history = append([]string{statement}, m.queryLog.history...)
	if len(m.queryLog.history) > queryLogLimit {
		m.queryLog.history = m.queryLog.history[:queryLogLimit]
	}
	m.queryLog.historyIndex = -1
}

func (m *Model) recallQueryHistory(direction int) bool {
	if len(m.queryLog.history) == 0 {
		return false
	}
	if direction > 0 {
		if m.queryLog.historyIndex == -1 {
			m.queryLog.historyIndex = 0
		} else if m.queryLog.historyIndex < len(m.queryLog.history)-1 {
			m.queryLog.historyIndex++
		} else {
			return false // already at the oldest entry; never wrap
		}
	} else {
		if m.queryLog.historyIndex <= 0 {
			if m.queryLog.historyIndex == -1 {
				return false // Down outside recall mode
			}
			m.queryLog.historyIndex = -1
		} else {
			m.queryLog.historyIndex--
		}
	}
	if m.queryLog.historyIndex == -1 {
		m.queryLog.editor.setValue("")
	} else {
		m.queryLog.editor.setValue(m.queryLog.history[m.queryLog.historyIndex])
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
		m.appendQueryLog(queryLogEntry{StartedAt: message.startedAt, Statement: message.statement, Duration: time.Since(message.startedAt), Message: "canceled", Status: "canceled"})
	} else {
		m.setResults(message.result)
		if message.statement != "" && len(message.result.Rows) > 0 {
			m.queryLog.results.SetCursor(0)
		}
		m.appendQueryLog(queryLogEntry{StartedAt: message.startedAt, Statement: message.statement, Duration: message.result.Duration, Message: queryLogMessage(message.statement, message.result.RowsAffected, len(message.result.Rows)), Status: "success"})
	}
	if quit {
		return m, tea.Quit
	}
	// Schema may have changed (DDL); recheck the editor value against it.
	m.queryLog.editorValidity = sqlValidityPending
	if message.reloadSchema {
		// A table action succeeded: close its popup and refresh the sidebar.
		m.schema.component.Structure.TableForm = schema.TableForm{}
		m.schema.component.Structure.TableFormRunning = false
		return m, tea.Batch(m.scheduleSQLValidation(), m.loadSchema())
	}
	return m, m.scheduleSQLValidation()
}

func (m Model) updateQueryFailure(message queryFailedMsg) (tea.Model, tea.Cmd) {
	if !m.Workflow.MatchesQuery(message.requestID) {
		return m, nil
	}
	_, quit := m.Workflow.FinishQuery()
	m.appendQueryLog(queryLogEntry{StartedAt: message.startedAt, Statement: message.statement, Duration: time.Since(message.startedAt), Message: message.err.Error(), Status: "failed"})
	m.chat.component.LastFailedQuery = message.statement
	m.chat.component.LastFailedError = message.err.Error()
	if quit {
		return m, tea.Quit
	}
	if m.schema.component.Structure.TableFormRunning {
		// The table DDL was rejected: keep the popup open with its typed
		// name so the user can adjust or discard it.
		m.schema.component.Structure.TableFormRunning = false
		m.overlay.formMode.Mode = formModeNormal
		m.setStatus(safeText("table action failed: " + message.err.Error()))
	}
	return m, nil
}

func (m Model) updateQueryCanceled(message queryCanceledMsg) (tea.Model, tea.Cmd) {
	if !m.Workflow.MatchesQuery(message.requestID) {
		return m, nil
	}
	_, quit := m.Workflow.FinishQuery()
	m.appendQueryLog(queryLogEntry{StartedAt: message.startedAt, Statement: message.statement, Duration: time.Since(message.startedAt), Message: "canceled", Status: "canceled"})
	if quit {
		return m, tea.Quit
	}
	if m.schema.component.Structure.TableFormRunning {
		// The statement never ran; restore the popup.
		m.schema.component.Structure.TableFormRunning = false
		m.overlay.formMode.Mode = formModeNormal
	}
	return m, nil
}

// schemaLoadedMsg delivers a refreshed schema listing after a table action.
type schemaLoadedMsg struct {
	objects []sharedsql.SchemaObject
	err     error
}

// loadSchema refreshes the sidebar from the database after successful table
// DDL so the schema tree reflects the change.
func (m Model) loadSchema() tea.Cmd {
	return func() tea.Msg {
		objects, err := m.Database.ListSchema(m.appContext)
		return schemaLoadedMsg{objects: objects, err: err}
	}
}

func (m Model) updateSchemaLoaded(message schemaLoadedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		// Keep the previous sidebar; report the refresh failure.
		m.setStatus(safeText("refreshing schema: " + message.err.Error()))
		return m, nil
	}
	return m, m.setSchemaObjects(message.objects)
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
	m.queryLog.validationTag++
	tag := m.queryLog.validationTag
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
	if message.tag != m.queryLog.validationTag || m.Database == nil {
		return m, nil
	}
	statement := strings.TrimSpace(m.queryLog.editor.value)
	if statement == "" {
		m.queryLog.editorValidity = sqlValidityPending
		return m, nil
	}
	return m, m.validateSQL(m.queryLog.editor.value)
}

func (m Model) updateSQLValidation(message sqlValidationMsg) (tea.Model, tea.Cmd) {
	if m.queryLog.editor.value != message.statement {
		return m, nil // stale result for an older revision
	}
	if message.err != nil {
		m.queryLog.editorValidity = sqlValidityInvalid
	} else {
		m.queryLog.editorValidity = sqlValidityValid
	}
	return m, nil
}
