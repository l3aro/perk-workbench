package querylog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Limit caps the in-memory entry list and the persisted rows per scope.
const Limit = 100

// Model is the query-log feature component: the table pane, paging state,
// the selected column/offset, the detail overlay, and the entry list.
// Root owns the persistence store and screen geometry; the component owns
// every interaction and renders its own pane and overlays.
type Model struct {
	Table    table.Model
	Entries  []Entry
	Detail   *Entry
	Page     int
	PageSize int
	PendingG bool
	Column   int
	Offset   int
	// ViewportWidth is the pane content width used by Render; View and
	// root layout refreshes update it.
	ViewportWidth int
}

// New builds an empty query-log component with the given page size.
func New(pageSize int) Model {
	m := Model{Table: uikit.NewResultsTable(), PageSize: pageSize, ViewportWidth: 80}
	m.Table.SetColumns(uikit.TableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, nil))
	m.Table.Blur()
	return m
}

// PageCount returns the number of pages for the current page size.
func (m Model) PageCount() int {
	return max((len(m.Entries)+m.PageSize-1)/m.PageSize, 1)
}

func (m Model) pageEntries() []Entry {
	start := m.Page * m.PageSize
	if start >= len(m.Entries) {
		return nil
	}
	return m.Entries[start:min(start+m.PageSize, len(m.Entries))]
}

// SelectedEntry returns the entry under the table cursor, if any.
func (m Model) SelectedEntry() (Entry, bool) {
	index := m.Page*m.PageSize + m.Table.Cursor()
	if index < 0 || index >= len(m.Entries) {
		return Entry{}, false
	}
	return m.Entries[index], true
}

// SetEntries replaces the entry list and re-renders the table.
func (m *Model) SetEntries(entries []Entry) {
	m.Entries = entries
	m.Render()
}

// Append prepends one entry, caps the list at Limit, and re-renders.
func (m *Model) Append(entry Entry) {
	m.Entries = append([]Entry{entry}, m.Entries...)
	if len(m.Entries) > Limit {
		m.Entries = m.Entries[:Limit]
	}
	m.Render()
}

// SetPage sets the page and clamps the cursor to its first row.
func (m *Model) SetPage(page int) {
	m.Page = page
	m.Table.SetCursor(0)
	m.Render()
}

// Reset clears the entry list, paging, selection, and detail overlay in
// place, keeping the sized table so the disconnect layout pass survives.
func (m *Model) Reset() {
	m.Entries = nil
	m.Detail = nil
	m.Page = 0
	m.PendingG = false
	m.Column, m.Offset = 0, 0
	m.Table.SetRows(nil)
}

// DetailOpen reports whether the detail overlay is visible.
func (m Model) DetailOpen() bool { return m.Detail != nil }

// CloseDetail closes the detail overlay.
func (m *Model) CloseDetail() { m.Detail = nil }

// OpenDetailAtCursor opens the detail overlay for the selected entry.
func (m *Model) OpenDetailAtCursor() {
	if entry, ok := m.SelectedEntry(); ok {
		m.Detail = &entry
	}
}

// SelectedCellText returns the raw text of the selected cell for copying.
func (m Model) SelectedCellText() (string, bool) {
	entry, ok := m.SelectedEntry()
	if !ok {
		return "", false
	}
	return queryLogCell(entry, m.Column), true
}

// SelectedStatement returns the selected entry's statement for EXPLAIN.
func (m Model) SelectedStatement() (string, bool) {
	entry, ok := m.SelectedEntry()
	if !ok {
		return "", false
	}
	return entry.Statement, true
}

// RevealColumn pans the viewport so the selected column is visible
// (root calls it from its layout pass).
func (m *Model) RevealColumn(viewportWidth int) {
	uikit.RevealTableColumn(m.Table, m.Column, &m.Offset, viewportWidth)
}

// AllEntries returns the current entry list (root reads it for chat
// context and tests; the component owns all writes).
func (m Model) AllEntries() []Entry { return m.Entries }

// Focus gives the pane table keyboard focus.
func (m *Model) Focus() { m.Table.Focus() }

// Blur removes keyboard focus from the pane table.
func (m *Model) Blur() { m.Table.Blur() }

// ClearPendingG drops the armed top-first gate.
func (m *Model) ClearPendingG() { m.PendingG = false }

// HasRows reports whether the pane table has any rows.
func (m Model) HasRows() bool { return len(m.Table.Rows()) > 0 }

// EnsureCursor places the cursor on row 0 when the table has rows and no
// cursor yet (focus entry).
func (m *Model) EnsureCursor() {
	if len(m.Table.Rows()) > 0 && m.Table.Cursor() < 0 {
		m.Table.SetCursor(0)
	}
}

// RefreshViewport records the pane content width and re-renders the
// table columns (root's layout width-change pass).
func (m *Model) RefreshViewport(width int) {
	m.ViewportWidth = max(width, 1)
	m.Render()
}

// Click selects the row and column under a pane click. paneX is the click
// x relative to the pane's content origin; contentY is relative to the
// pane title row (title at 0, table header at 1, data rows from 2).
func (m *Model) Click(paneX, contentY int) {
	rowY := contentY - 2
	if rowY < 0 || rowY >= m.Table.Height() {
		return
	}
	rows := m.Table.Rows()
	start := min(max(m.Table.Cursor()-m.Table.Height()+1, 0), max(len(rows)-m.Table.Height(), 0))
	if row := start + rowY; row < len(rows) {
		m.Table.SetCursor(row)
		cellX := paneX + m.Offset
		for index, column := range m.Table.Columns() {
			cellWidth := column.Width + 2*uikit.SpaceCompact
			if cellX < cellWidth {
				m.Column = index
				break
			}
			cellX -= cellWidth
		}
	}
}

// Select moves the cursor to row and selects column (test/API helper).
func (m *Model) Select(row, col int) {
	if row >= 0 && row < len(m.Table.Rows()) {
		m.Table.SetCursor(row)
	}
	if col >= 0 {
		m.Column = col
	}
}

// Wheel moves the cursor row (step) and selected column (hStep).
func (m *Model) Wheel(step, hStep int) {
	if hStep != 0 {
		uikit.MoveTableColumn(&m.Table, &m.Column, &m.Offset, m.ViewportWidth, hStep)
		return
	}
	rows := m.Table.Rows()
	rowCount := len(rows)
	if rowCount == 0 {
		return
	}
	m.Table.SetCursor(uikit.Clamp(m.Table.Cursor()+step, 0, rowCount-1))
}

// Render rebuilds the table columns and rows from the current page.
func (m *Model) Render() {
	if m.Page >= m.PageCount() {
		m.Page = m.PageCount() - 1
	}
	entries := m.pageEntries()
	rows := make([]table.Row, len(entries))
	for index, item := range entries {
		var statusStr string
		switch item.Status {
		case "failed":
			statusStr = uikit.StatusFailedStyle.Render(uikit.IconFailed)
		case "canceled":
			statusStr = uikit.StatusCanceledStyle.Render(uikit.IconCanceled)
		default:
			statusStr = uikit.StatusSuccessStyle.Render(uikit.IconSuccess)
		}
		rows[index] = table.Row{
			item.StartedAt.Format("15:04:05"),
			statusStr,
			uikit.CellText(item.Statement),
			item.Duration.Round(time.Microsecond).String(),
			uikit.CellText(item.Message),
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
	height := m.Table.Height()
	m.Table.SetRows(nil)
	columns := uikit.TableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, rows)
	columns[2].Width = min(columns[2].Width, 50)
	columns[4].Width = min(columns[4].Width, 50)
	m.Table.SetColumns(columns)
	uikit.ResizeResultsTable(&m.Table, m.ViewportWidth, max(height+1, 2))
	m.Table.SetRows(rows)
}

// Summary renders the paging summary line.
func (m Model) Summary() string {
	if len(m.Entries) == 0 {
		return ""
	}
	fastest, slowest := m.Entries[0].Duration, m.Entries[0].Duration
	for _, entry := range m.Entries[1:] {
		fastest = min(fastest, entry.Duration)
		slowest = max(slowest, entry.Duration)
	}
	return fmt.Sprintf("page %d/%d | fastest %s | slowest %s", m.Page+1, m.PageCount(), fastest.Round(time.Microsecond), slowest.Round(time.Microsecond))
}

// queryLogCell returns the raw value of one table cell.
func queryLogCell(entry Entry, column int) string {
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

// View renders the query-log pane body: the table viewport, the summary
// line, and the status row. It is pure: root refreshes the component's
// viewport width through SetViewportWidth when the layout changes.
func (m Model) View(layout uikit.Layout) string {
	content := uikit.TableViewportViewWithAlignment(m.Table, nil, m.Offset, layout.ViewportWidth, m.Column)
	summary := m.Summary() + uikit.ColsHint(m.Table.Columns(), layout.ViewportWidth)
	padding := max(layout.PaneHeight-1-lipgloss.Height(content)-1, 0)
	return content + strings.Repeat("\n", padding+1) +
		chrome.PaneStatus(uikit.StatusStyle.Render("n/p page"), uikit.StatusStyle.Render(summary), layout.ViewportWidth)
}

// Resize records the pane viewport width and fits the table height to
// the pane, mirroring the root layout equation (the header line is the
// one-row difference between the pane height and the table viewport).
// Root calls it from its layout pass.
func (m *Model) Resize(layout uikit.Layout) {
	m.ViewportWidth = max(layout.ViewportWidth, 1)
	uikit.ResizeResultsTable(&m.Table, m.ViewportWidth, max(layout.PaneHeight-5, 2))
}
