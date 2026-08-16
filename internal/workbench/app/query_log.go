package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/clipboard"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
)

// queryLogLimit caps the persisted rows per scope; the component enforces
// the same cap on its in-memory list.
const queryLogLimit = querylog.Limit

// queryLogEntry is the query-log record type shared with the query-log
// component and its persistence store.
type queryLogEntry = querylog.Entry

// redactedStatement is the stable marker persisted and rendered in place
// of a sensitive statement; the verbatim text may live only in the
// bounded current-session transient cache that backs explicit copy.
const redactedStatement = "[redacted]"

// queryLogStatementColumn is the Statement column index in the query-log
// table (Time, Status, Statement, Duration, Message), mirroring the
// component's fixed column layout.
const queryLogStatementColumn = 2

// copyQueryLogStatement writes a statement through both OSC 52 and the
// native system clipboard, covering terminals and desktop clipboard
// clients. Root owns the clipboard path; components request it through
// the ClipboardRequested event.
func copyQueryLogStatement(statement string) tea.Cmd {
	return tea.Sequence(
		tea.SetClipboard(statement),
		func() tea.Msg {
			clipboard.WriteText(statement)
			return nil
		},
	)
}

// withStatementMetadata resolves optional statement metadata onto an
// entry. Omitted metadata keeps the legacy semantics: replayable, not
// sensitive, no language. A present metadata object is authoritative for
// language, replayable, and sensitive.
func withStatementMetadata(entry queryLogEntry, metadata *sharedsql.StatementMetadata) queryLogEntry {
	if metadata == nil {
		entry.Replayable = true
		return entry
	}
	entry.Language = metadata.Language
	entry.Replayable = metadata.Replayable
	entry.Sensitive = metadata.Sensitive
	return entry
}

func actionLogEntry(statement string, metadata *sharedsql.StatementMetadata, startedAt time.Time, err error, text string) queryLogEntry {
	entry := queryLogEntry{
		StartedAt: startedAt,
		Statement: statement,
		Duration:  time.Since(startedAt),
		Message:   text,
	}
	if err != nil {
		entry.Status = "failed"
		entry.Message = err.Error()
	}
	entry = withStatementMetadata(entry, metadata)
	if err != nil && entry.Sensitive {
		// A sensitive failure must not echo backend error text — Redis
		// errors include the offending command and its arguments — so
		// the default failed message fills in at append time. Advisory
		// guidance is backend text too (it can name keys of the
		// redacted statement) and is dropped with the message.
		entry.Message = ""
		entry.Hint = ""
		entry.SuggestedStatement = ""
	}
	return entry
}

// appendQueryLog records one executed statement: it fills the default
// message, applies the sensitive-entry policy — the verbatim statement is
// retained only in the session transient cache (never in the component,
// at rest, or in loaded entries), and the entry is never replayable —
// appends to the component (which caps and re-renders), and persists
// through the lazy store when a profile scope exists.
func (m *Model) appendQueryLog(entry queryLogEntry) {
	if entry.Message == "" {
		switch entry.Status {
		case "failed":
			entry.Message = "failed"
		case "canceled":
			entry.Message = "canceled"
		default:
			entry.Message = "completed"
		}
	}
	original := ""
	if entry.Sensitive {
		original = entry.Statement
		entry.Statement = redactedStatement
		entry.Replayable = false
		// The failure message must never echo backend error text either
		// (Redis errors include the offending command and its
		// arguments), and advisory guidance is backend text too — it can
		// name keys of the redacted statement — so both are dropped.
		if entry.Status == "failed" {
			entry.Message = "failed"
			entry.Hint = ""
			entry.SuggestedStatement = ""
		}
	}
	m.queryLog.component.Append(entry)
	// Keep the transient cache index-aligned with the component's entry
	// list under the same cap: the cache slot holds the verbatim original
	// for sensitive entries and stays empty for every other entry.
	m.queryLog.transientStatements = append([]string{original}, m.queryLog.transientStatements...)
	if len(m.queryLog.transientStatements) > len(m.queryLog.component.Entries) {
		m.queryLog.transientStatements = m.queryLog.transientStatements[:len(m.queryLog.component.Entries)]
	}
	if store := m.queryLogStore(); store != nil {
		_ = store.Append(m.connectionID, entry, queryLogLimit)
	}
	m.refreshChatFailedContext()
}

// queryLogYankText resolves the text a query-log copy action puts on the
// clipboard. Statement cells copy the entry statement — for sensitive
// entries the verbatim session original from the transient cache, never
// the redacted marker; a loaded sensitive entry has no original and is
// rejected. Non-statement cells copy their displayed text as-is.
func (m Model) queryLogYankText(entry querylog.Entry) (string, bool) {
	if m.queryLog.component.Column != queryLogStatementColumn {
		return m.queryLog.component.SelectedCellText()
	}
	if entry.Sensitive {
		return m.transientStatement(entry)
	}
	return entry.Statement, true
}

// transientStatement returns the session-only verbatim statement of the
// sensitive entry under the table cursor, if appendQueryLog recorded one.
// The cache is index-aligned with the component's entry list, so the
// position association is collision-safe; loaded entries have no slot.
func (m Model) transientStatement(entry querylog.Entry) (string, bool) {
	index := m.queryLog.component.Page*m.queryLog.component.PageSize + m.queryLog.component.Table.Cursor()
	if index < 0 || index >= len(m.queryLog.transientStatements) {
		return "", false
	}
	original := m.queryLog.transientStatements[index]
	return original, original != ""
}

// rowWriteFailureStatus renders a row-write failure status. A dead
// plugin gets the actionable recovery path (the CTA leaks nothing); a
// sensitive write must not echo the backend error — Redis errors include
// the offending command and its arguments — so it reports a generic
// failure; anything else carries the raw error.
func rowWriteFailureStatus(verb string, metadata *sharedsql.StatementMetadata, err error) string {
	if plugin.IsTerminal(err) {
		return pluginStoppedCTA
	}
	if metadata != nil && metadata.Sensitive {
		return verb + ": failed"
	}
	return safeText(fmt.Sprintf("%s: %v", verb, err))
}

// refreshChatFailedContext mirrors the newest failed query into the chat
// component's assistant context, scanning the current log like the
// original context builder did.
func (m *Model) refreshChatFailedContext() {
	for _, entry := range m.queryLog.component.AllEntries() {
		if entry.Status == "failed" {
			m.chat.component.LastFailedQuery = entry.Statement
			m.chat.component.LastFailedError = entry.Message
			return
		}
	}
	m.chat.component.LastFailedQuery = ""
	m.chat.component.LastFailedError = ""
}

func queryLogMessage(statement string, rowsAffected int64, rows int) string {
	var operation string
	switch statementKeyword(statement) {
	case "INSERT", "REPLACE":
		operation = "inserted"
	case "UPDATE":
		operation = "updated"
	case "DELETE":
		operation = "deleted"
	case "SELECT", "WITH", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "PRAGMA":
		operation = "fetched"
	default:
		return "completed"
	}
	if operation == "fetched" {
		rowsAffected = int64(rows)
	}
	rowLabel := "rows"
	if rowsAffected == 1 {
		rowLabel = "row"
	}
	return fmt.Sprintf("%s %d %s", operation, rowsAffected, rowLabel)
}

func statementKeyword(statement string) string {
	for {
		statement = strings.TrimSpace(strings.TrimLeft(statement, "("))
		switch {
		case strings.HasPrefix(statement, "--"):
			if index := strings.IndexByte(statement, '\n'); index >= 0 {
				statement = statement[index+1:]
				continue
			}
			return ""
		case strings.HasPrefix(statement, "/*"):
			index := strings.Index(statement[2:], "*/")
			if index < 0 {
				return ""
			}
			statement = statement[index+4:]
			continue
		}
		break
	}
	if index := strings.IndexAny(statement, " \t\n\r("); index >= 0 {
		statement = statement[:index]
	}
	return strings.ToUpper(statement)
}
