package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/clipboard"
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
// of a sensitive statement; the verbatim text is never retained after the
// decision point.
const redactedStatement = "[redacted]"

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
	return withStatementMetadata(entry, metadata)
}

// appendQueryLog records one executed statement: it fills the default
// message, applies the sensitive-entry policy — the verbatim statement is
// never retained, in memory or at rest, and the entry is never replayable
// — appends to the component (which caps and re-renders), and persists
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
	if entry.Sensitive {
		entry.Statement = redactedStatement
		entry.Replayable = false
	}
	m.queryLog.component.Append(entry)
	if store := m.queryLogStore(); store != nil {
		_ = store.Append(m.connectionID, entry, queryLogLimit)
	}
	m.refreshChatFailedContext()
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
