package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/clipboard"
)

const queryLogLimit = 100

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

type queryLogEntry struct {
	startedAt time.Time
	statement string
	duration  time.Duration
	message   string
	status    string // "success", "failed", "canceled"
}

func actionLogEntry(statement string, startedAt time.Time, err error, message string) queryLogEntry {
	entry := queryLogEntry{
		startedAt: startedAt,
		statement: statement,
		duration:  time.Since(startedAt),
		message:   message,
	}
	if err != nil {
		entry.status = "failed"
		entry.message = err.Error()
	}
	return entry
}

func (m *Model) appendQueryLog(entry queryLogEntry) {
	if entry.message == "" {
		switch entry.status {
		case "failed":
			entry.message = "failed"
		case "canceled":
			entry.message = "canceled"
		default:
			entry.message = "completed"
		}
	}
	m.queryLogEntries = append([]queryLogEntry{entry}, m.queryLogEntries...)
	if len(m.queryLogEntries) > queryLogLimit {
		m.queryLogEntries = m.queryLogEntries[:queryLogLimit]
	}
	rows := make([]table.Row, len(m.queryLogEntries))
	for index, item := range m.queryLogEntries {
		var statusStr string
		switch item.status {
		case "failed":
			statusStr = statusFailedStyle.Render(iconFailed)
		case "canceled":
			statusStr = statusCanceledStyle.Render(iconCanceled)
		default:
			statusStr = statusSuccessStyle.Render(iconSuccess)
		}
		rows[index] = table.Row{
			item.startedAt.Format("15:04:05"),
			statusStr,
			cellText(item.statement),
			item.duration.Round(time.Microsecond).String(),
			cellText(item.message),
		}
	}
	// Center status icons within column
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
	height := m.queryLog.Height()
	m.queryLog.SetRows(nil)
	m.queryLog.SetColumns(tableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, rows))
	resizeResultsTable(&m.queryLog, m.tableViewportWidth, max(height+1, 2))
	m.queryLog.SetRows(rows)
}

func queryLogCell(entry queryLogEntry, column int) string {
	switch column {
	case 0:
		return entry.startedAt.Format("15:04:05")
	case 1:
		return entry.status
	case 2:
		return entry.statement
	case 3:
		return entry.duration.Round(time.Microsecond).String()
	case 4:
		return entry.message
	default:
		return ""
	}
}

func (m Model) queryLogSummary() string {
	if len(m.queryLogEntries) == 0 {
		return ""
	}
	fastest, slowest := m.queryLogEntries[0].duration, m.queryLogEntries[0].duration
	for _, entry := range m.queryLogEntries[1:] {
		fastest = min(fastest, entry.duration)
		slowest = max(slowest, entry.duration)
	}
	return fmt.Sprintf("fastest %s | slowest %s", fastest.Round(time.Microsecond), slowest.Round(time.Microsecond))
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
