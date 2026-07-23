package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"github.com/charmbracelet/x/ansi"
)

const queryLogLimit = 100

type queryLogEntry struct {
	startedAt time.Time
	statement string
	duration  time.Duration
	fetched   int
	status    string // "success", "failed", "canceled"
	errMsg    string
}

func (m *Model) appendQueryLog(entry queryLogEntry) {
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
		statement := safeText(item.statement)
		if len(statement) > 80 {
			statement = statement[:80] + "..."
		}
		fetchedStr := fmt.Sprintf("%d", item.fetched)
		if item.fetched == 0 && item.status == "failed" {
			fetchedStr = "-"
		}
		rows[index] = table.Row{
			item.startedAt.Format("15:04:05"),
			statusStr,
			statement,
			item.duration.Round(time.Microsecond).String(),
			fetchedStr,
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
	m.queryLog.SetColumns(tableColumns([]string{"Time", "Status", "Statement", "Duration", "Fetched"}, rows))
	resizeResultsTable(&m.queryLog, m.tableViewportWidth, max(height+1, 2))
	m.queryLog.SetRows(rows)
}

func (m Model) queryLogSummary() string {
	if len(m.queryLogEntries) == 0 {
		return ""
	}
	fastest, slowest := m.queryLogEntries[0].duration, m.queryLogEntries[0].duration
	leastFetch, mostFetch := m.queryLogEntries[0].fetched, m.queryLogEntries[0].fetched
	for _, entry := range m.queryLogEntries[1:] {
		fastest = min(fastest, entry.duration)
		slowest = max(slowest, entry.duration)
		leastFetch = min(leastFetch, entry.fetched)
		mostFetch = max(mostFetch, entry.fetched)
	}
	return fmt.Sprintf("fastest %s | slowest %s | most fetch %d | least fetch %d", fastest.Round(time.Microsecond), slowest.Round(time.Microsecond), mostFetch, leastFetch)
}
