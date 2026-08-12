package workbench

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/clipboard"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
)

// queryLogLimit caps the persisted rows per scope; the component enforces
// the same cap on its in-memory list.
const queryLogLimit = querylog.Limit

// queryLogEntry is the query-log record type shared with the query-log
// component and its persistence store.
type queryLogEntry = querylog.Entry

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

func actionLogEntry(statement string, startedAt time.Time, err error, message string) queryLogEntry {
	entry := queryLogEntry{
		StartedAt: startedAt,
		Statement: statement,
		Duration:  time.Since(startedAt),
		Message:   message,
	}
	if err != nil {
		entry.Status = "failed"
		entry.Message = err.Error()
	}
	return entry
}

// appendQueryLog records one executed statement: it fills the default
// message, appends to the component (which caps and re-renders), and
// persists through the lazy store when a profile scope exists.
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
	m.queryLog.component.Append(entry)
	if store := m.queryLogStore(); store != nil {
		_ = store.Append(m.connectionID, entry, queryLogLimit)
	}
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
