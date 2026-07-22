package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
)

const queryLogLimit = 100

type queryLogEntry struct {
	startedAt time.Time
	statement string
	duration  time.Duration
	fetched   int
}

func (m *Model) appendQueryLog(entry queryLogEntry) {
	m.queryLogEntries = append([]queryLogEntry{entry}, m.queryLogEntries...)
	if len(m.queryLogEntries) > queryLogLimit {
		m.queryLogEntries = m.queryLogEntries[:queryLogLimit]
	}
	rows := make([]table.Row, len(m.queryLogEntries))
	for index, item := range m.queryLogEntries {
		rows[index] = table.Row{
			item.startedAt.Format("15:04:05"),
			safeText(strings.Join(strings.Fields(item.statement), " ")),
			item.duration.Round(time.Microsecond).String(),
			fmt.Sprintf("%d", item.fetched),
		}
	}
	height := m.queryLog.Height()
	m.queryLog.SetRows(nil)
	m.queryLog.SetColumns(tableColumns([]string{"Time", "Query", "Duration", "Fetch"}, rows))
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
