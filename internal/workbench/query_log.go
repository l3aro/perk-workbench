package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/clipboard"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
)

const queryLogLimit = 100

// queryLogEntry is the query-log record type shared with the persistence
// store.
type queryLogEntry = querylog.Entry

// copyQueryLogStatement writes a statement through both OSC 52 and the native
// system clipboard, covering terminals and desktop clipboard clients.
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
	m.queryLog.entries = append([]queryLogEntry{entry}, m.queryLog.entries...)
	if len(m.queryLog.entries) > queryLogLimit {
		m.queryLog.entries = m.queryLog.entries[:queryLogLimit]
	}
	if store := m.queryLogStore(); store != nil {
		_ = store.Append(m.connectionID, entry, queryLogLimit)
	}
	m.renderQueryLog()
}

func (m Model) queryLogPageCount() int {
	return max((len(m.queryLog.entries)+m.queryLog.pageSize-1)/m.queryLog.pageSize, 1)
}

func (m Model) queryLogPageEntries() []queryLogEntry {
	start := m.queryLog.page * m.queryLog.pageSize
	if start >= len(m.queryLog.entries) {
		return nil
	}
	return m.queryLog.entries[start:min(start+m.queryLog.pageSize, len(m.queryLog.entries))]
}

func (m Model) queryLogSelectedEntry() (queryLogEntry, bool) {
	index := m.queryLog.page*m.queryLog.pageSize + m.queryLog.table.Cursor()
	if index < 0 || index >= len(m.queryLog.entries) {
		return queryLogEntry{}, false
	}
	return m.queryLog.entries[index], true
}

func (m *Model) renderQueryLog() {
	if m.queryLog.page >= m.queryLogPageCount() {
		m.queryLog.page = m.queryLogPageCount() - 1
	}
	entries := m.queryLogPageEntries()
	rows := make([]table.Row, len(entries))
	for index, item := range entries {
		var statusStr string
		switch item.Status {
		case "failed":
			statusStr = statusFailedStyle.Render(iconFailed)
		case "canceled":
			statusStr = statusCanceledStyle.Render(iconCanceled)
		default:
			statusStr = statusSuccessStyle.Render(iconSuccess)
		}
		rows[index] = table.Row{
			item.StartedAt.Format("15:04:05"),
			statusStr,
			cellText(item.Statement),
			item.Duration.Round(time.Microsecond).String(),
			cellText(item.Message),
		}
	}
	statusColWidth := ansi.StringWidth("Status")
	for _, row := range rows {
		statusColWidth = max(statusColWidth, ansi.StringWidth(row[1]))
	}
	for _, row := range rows {
		contentWidth := ansi.StringWidth(row[1])
		if contentWidth < statusColWidth {
			row[1] = strings.Repeat(" ", (statusColWidth-contentWidth)/2) + row[1]
		}
	}
	height := m.queryLog.table.Height()
	m.queryLog.table.SetRows(nil)
	columns := tableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, rows)
	columns[2].Width = min(columns[2].Width, 50)
	columns[4].Width = min(columns[4].Width, 50)
	m.queryLog.table.SetColumns(columns)
	resizeResultsTable(&m.queryLog.table, m.layout.tableViewportWidth, max(height+1, 2))
	m.queryLog.table.SetRows(rows)
}

func queryLogCell(entry queryLogEntry, column int) string {
	switch column {
	case 0:
		return entry.StartedAt.Format("15:04:05")
	case 1:
		return entry.Status
	case 2:
		return entry.Statement
	case 3:
		return entry.Duration.Round(time.Microsecond).String()
	case 4:
		return entry.Message
	default:
		return ""
	}
}

func (m Model) queryLogSummary() string {
	if len(m.queryLog.entries) == 0 {
		return ""
	}
	fastest, slowest := m.queryLog.entries[0].Duration, m.queryLog.entries[0].Duration
	for _, entry := range m.queryLog.entries[1:] {
		fastest = min(fastest, entry.Duration)
		slowest = max(slowest, entry.Duration)
	}
	return fmt.Sprintf("page %d/%d | fastest %s | slowest %s", m.queryLog.page+1, m.queryLogPageCount(), fastest.Round(time.Microsecond), slowest.Round(time.Microsecond))
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
