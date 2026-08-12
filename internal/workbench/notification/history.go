package notification

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// columnTitles are the modal table's columns in display order; entryRow
// and the sort column index both rely on this order.
var columnTitles = []string{"Time", "Level", "Title", "Description"}

// history is a full-width modal table of retained notifications. It
// mirrors the Browse pane's table: cell travel with h/j/k/l and the arrow
// keys, y copies the selected cell, / filters, s (or a header click)
// sorts, n/p page, and v opens the cell in the viewer overlay.
type history struct {
	entries       []Entry // all retained entries, newest first
	filtered      []Entry // entries after filter + sort
	page          int     // current page
	pageSize      int     // rows per page, derived from the modal height
	table         table.Model
	pageEntries   []Entry // entries behind the table rows
	width, height int     // modal size, for click hit-testing
	selectedCol   int     // cell column under the cursor
	offset        int     // horizontal scroll offset
	filter        textinput.Model
	filterFocused bool
	sortCol       int // -1 = default (newest first) order
	sortDesc      bool
	viewer        *uikit.CellViewer // view-cell overlay, nil when closed
}

// NewHistory builds the modal. selectedID selects the entry with that
// SQLite row ID, falling back to the newest entry when 0 or absent.
func NewHistory(entries []Entry, selectedID int64, width, height int) *history {
	h := &history{
		entries:  append([]Entry{}, entries...),
		filtered: append([]Entry{}, entries...),
		filter:   uikit.NewFilterInput(),
		sortCol:  -1,
	}
	h.filter.Placeholder = "filter notifications"
	h.table = table.New(table.WithStyles(table.Styles{
		Header:   uikit.HeaderStyle,
		Cell:     lipgloss.NewStyle().Padding(0, uikit.SpaceCompact),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(uikit.ColorPrimary)).Background(lipgloss.Color(uikit.ColorStripe)),
	}))
	h.resize(width, height)
	for index, entry := range h.filtered {
		if entry.ID == selectedID {
			h.page = index / h.pageSize
			h.table.SetCursor(index % h.pageSize)
			break
		}
	}
	h.syncPage()
	return h
}

// resize updates the modal geometry: page size and table follow the
// height, and the page is clamped back into range.
func (h *history) resize(width, height int) {
	h.width, h.height = width, height
	h.pageSize = max(height-12, 1)
	h.page = uikit.Clamp(h.page, 0, max(h.pageCount()-1, 0))
	// The input's View is one cell wider than its Width, so leave one cell
	// of slack against the filter box's content width.
	h.filter.SetWidth(max(h.viewportWidth()-7, 1))
	if h.viewer != nil {
		h.viewer.Resize(max(width-8, 1), max(height-10, 1))
	}
	h.syncPage()
}

// pageCount returns the number of pages the filtered entries span.
func (h *history) pageCount() int {
	return (len(h.filtered) + h.pageSize - 1) / h.pageSize
}

// viewportWidth returns the modal's inner content width, the width the
// table and the pager row are laid out to.
func (h *history) viewportWidth() int {
	return max(h.width-6, 1)
}

// syncPage rebuilds the current page rows and column widths (sort markers
// included) from filtered, preserving the selected cell.
func (h *history) syncPage() {
	row, col := h.table.Cursor(), h.selectedCol
	h.page = uikit.Clamp(h.page, 0, max(h.pageCount()-1, 0))
	start := h.page * h.pageSize
	end := min(start+h.pageSize, len(h.filtered))
	h.pageEntries = append([]Entry{}, h.filtered[start:end]...)
	rows := make([]table.Row, len(h.pageEntries))
	for index, entry := range h.pageEntries {
		rows[index] = h.entryRow(entry)
	}
	h.table.SetCursor(uikit.Clamp(row, 0, max(len(rows)-1, 0)))
	h.selectedCol = uikit.Clamp(col, 0, len(columnTitles)-1)
	titles := append([]string{}, columnTitles...)
	if h.sortCol >= 0 && h.sortCol < len(titles) {
		if h.sortDesc {
			titles[h.sortCol] += " ▼"
		} else {
			titles[h.sortCol] += " ▲"
		}
	}
	// Columns first: bubbles renders rows against the current columns, so
	// SetRows after a column change would index out of range otherwise.
	h.table.SetColumns(uikit.TableColumns(titles, rows))
	h.table.SetRows(rows)
	h.table.SetCursor(uikit.Clamp(row, 0, max(len(rows)-1, 0)))
	h.table.SetWidth(max(h.viewportWidth(), uikit.TableContentWidth(h.table.Columns())))
	h.table.SetHeight(h.pageSize)
}

// entryRow renders one entry as a table row: time, level, title,
// description. Copy and view use the raw entry, not these display cells.
func (h *history) entryRow(entry Entry) table.Row {
	return table.Row{
		entry.CreatedAt.Format("2006-01-02 15:04:05"),
		h.levelText(entry),
		entry.Title,
		entry.Description,
	}
}

// levelText returns the display text of an entry's level column: the
// severity title for logged events, empty for plain status messages.
func (h *history) levelText(entry Entry) string {
	if level, ok := logLevelOf(entry.Level); ok {
		return level.Title()
	}
	return ""
}

// applyFilter re-filters by the filter input (case-insensitive substring
// match across time, level, title, and description), re-sorts, and resets
// to the first page.
func (h *history) applyFilter() {
	h.refilter()
	h.page = 0
	h.table.SetCursor(0)
	h.syncPage()
}

// refilter rebuilds filtered from entries under the current filter query
// (case-insensitive substring match across time, level, title, and
// description), then applies the active sort. Called whenever the filter
// or the sort state changes, so removing a sort restores the default
// newest-first order instead of keeping the last sorted order.
func (h *history) refilter() {
	query := strings.ToLower(strings.TrimSpace(h.filter.Value()))
	h.filtered = h.filtered[:0]
	for _, entry := range h.entries {
		if strings.Contains(strings.ToLower(h.searchText(entry)), query) {
			h.filtered = append(h.filtered, entry)
		}
	}
	h.sortFiltered()
}

// searchText joins every searchable field of one entry.
func (h *history) searchText(entry Entry) string {
	return entry.CreatedAt.Format("2006-01-02 15:04:05") + " " + h.levelText(entry) + " " + entry.Title + " " + entry.Description
}

// sortFiltered applies the current sort to filtered. The default order
// (sortCol < 0) is the entry order: newest first.
func (h *history) sortFiltered() {
	if h.sortCol < 0 || h.sortCol >= len(columnTitles) || len(h.filtered) < 2 {
		return
	}
	col, desc := h.sortCol, h.sortDesc
	slices.SortStableFunc(h.filtered, func(a, b Entry) int {
		var cmp int
		switch col {
		case 0:
			cmp = a.CreatedAt.Compare(b.CreatedAt)
		case 1:
			cmp = strings.Compare(strings.ToLower(h.levelText(a)), strings.ToLower(h.levelText(b)))
		case 2:
			cmp = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		default:
			cmp = strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
		}
		if desc {
			return -cmp
		}
		return cmp
	})
}

// cycleSort advances the sort on the selected column like the Browse
// pane's s key: ascending, descending, then back to the default order.
// The selected entry stays under the cursor.
func (h *history) cycleSort() {
	var anchor Entry
	anchored := false
	if row := h.table.Cursor(); row >= 0 && row < len(h.pageEntries) {
		anchor, anchored = h.pageEntries[row], true
	}
	if h.selectedCol == h.sortCol {
		if !h.sortDesc {
			h.sortDesc = true
		} else {
			h.sortCol, h.sortDesc = -1, false
		}
	} else {
		h.sortCol, h.sortDesc = h.selectedCol, false
	}
	h.refilter()
	h.page = 0
	h.table.SetCursor(0)
	if anchored {
		for index, entry := range h.filtered {
			if entry.ID == anchor.ID && entry.CreatedAt.Equal(anchor.CreatedAt) {
				h.page = index / h.pageSize
				h.table.SetCursor(index % h.pageSize)
				break
			}
		}
	}
	h.syncPage()
}

// selected returns the filtered entry under the table cursor.
func (h *history) selected() (Entry, bool) {
	row := h.table.Cursor()
	if row < 0 || row >= len(h.pageEntries) {
		return Entry{}, false
	}
	return h.pageEntries[row], true
}

// cellValue returns the raw value of one table cell: the formatted time,
// level title, notification title, or the full description.
func (h *history) cellValue(row, col int) string {
	if row < 0 || row >= len(h.pageEntries) {
		return ""
	}
	entry := h.pageEntries[row]
	switch col {
	case 0:
		return entry.CreatedAt.Format("2006-01-02 15:04:05")
	case 1:
		return h.levelText(entry)
	case 2:
		return entry.Title
	default:
		return entry.Description
	}
}

// copyCell returns the raw value of the selected cell for the root to copy.
func (h *history) copyCell() (string, bool) {
	row := h.table.Cursor()
	if row < 0 || row >= len(h.pageEntries) || h.selectedCol < 0 || h.selectedCol >= len(columnTitles) {
		return "", false
	}
	return h.cellValue(row, h.selectedCol), true
}

// openViewer opens the selected cell in the viewer overlay, showing the
// untruncated value with wrap toggling.
func (h *history) openViewer() {
	row := h.table.Cursor()
	if row < 0 || row >= len(h.pageEntries) || h.selectedCol < 0 || h.selectedCol >= len(columnTitles) {
		return
	}
	col := h.selectedCol
	h.viewer = uikit.NewCellViewer(columnTitles[col], h.cellValue(row, col), max(h.width-8, 1), max(h.height-10, 1))
}

// nextPage advances to the next page, keeping the cursor row.
func (h *history) nextPage() {
	if h.page >= h.pageCount()-1 {
		return
	}
	h.page++
	h.syncPage()
}

// prevPage steps back a page, keeping the cursor row.
func (h *history) prevPage() {
	if h.page <= 0 {
		return
	}
	h.page--
	h.syncPage()
}

// statusText renders the modal's row-range summary: "1-12 of 25 | page
// 1/3", like the browse status line.
func (h *history) statusText() string {
	total := len(h.filtered)
	if total == 0 {
		return "No notifications"
	}
	start := h.page*h.pageSize + 1
	end := min(start+h.pageSize-1, total)
	return fmt.Sprintf("%d-%d of %d | page %d/%d", start, end, total, h.page+1, h.pageCount())
}

// pager describes the modal's Prev/Next button row: Prev and Next pinned
// to the row's ends around the status text, sharing the browse pane's
// button styling and placement. The rendered line and the click hit-test
// both read this one source of truth.
func (h *history) pager() uikit.Pager {
	pager := uikit.NewPager(h.page > 0, h.page < h.pageCount()-1)
	status := ansi.Truncate(h.statusText(), max(h.viewportWidth()-2-ansi.StringWidth(pager.Prev)-ansi.StringWidth(pager.Next)-2, 1), "…")
	gap := max(h.viewportWidth()-2-ansi.StringWidth(status)-ansi.StringWidth(pager.Prev)-ansi.StringWidth(pager.Next), 0)
	pager.PrevStart = 3 + ansi.StringWidth(status) + gap
	pager.NextStart = pager.PrevStart + ansi.StringWidth(pager.Prev)
	pager.Line = uikit.StatusStyle.Render(status + strings.Repeat(" ", gap) + pager.Prev + pager.Next)
	return pager
}

// handleClick routes a left click inside the modal: the table header
// cycles the sort, the pager buttons page, a data row selects the cell.
func (h *history) handleClick(x, y int) {
	if h.viewer != nil || h.filterFocused || x < 1 || x >= h.width-1 || y < 1 || y >= h.height-1 {
		return
	}
	if y == h.height-4 {
		pager := h.pager()
		if pager.PrevEnabled && x >= pager.PrevStart && x < pager.PrevStart+ansi.StringWidth(pager.Prev) {
			h.prevPage()
			return
		}
		if pager.NextEnabled && x >= pager.NextStart && x < pager.NextStart+ansi.StringWidth(pager.Next) {
			h.nextPage()
			return
		}
		return
	}
	if y == 6 {
		if col := h.columnAt(x); col >= 0 {
			h.selectedCol = col
			h.cycleSort()
		}
		return
	}
	if y >= 7 && y < 7+h.pageSize {
		row := y - 7
		if row >= 0 && row < len(h.table.Rows()) {
			h.table.SetCursor(row)
			if col := h.columnAt(x); col >= 0 {
				h.selectedCol = col
				uikit.RevealTableColumn(h.table, h.selectedCol, &h.offset, h.viewportWidth())
			}
		}
	}
}

// columnAt returns the table column under an absolute click x, or -1 when
// the click misses every column.
func (h *history) columnAt(x int) int {
	clickOffset := x - 2 + h.offset
	if clickOffset < 0 {
		return -1
	}
	start := 0
	for index, column := range h.table.Columns() {
		end := start + column.Width + 2*uikit.SpaceCompact
		if clickOffset >= start && clickOffset < end {
			return index
		}
		start = end
	}
	return -1
}

// handleWheel routes wheel events: vertical ticks move the cursor row,
// horizontal (or shift+vertical) ticks travel the selected column, and
// ticks over an open viewer scroll it.
func (h *history) handleWheel(msg tea.MouseWheelMsg) {
	if h.viewer != nil {
		h.viewer.Update(msg)
		return
	}
	step, hStep := 0, 0
	switch msg.Button {
	case tea.MouseWheelDown:
		if msg.Mod.Contains(tea.ModShift) {
			hStep = 1
		} else {
			step = 1
		}
	case tea.MouseWheelUp:
		if msg.Mod.Contains(tea.ModShift) {
			hStep = -1
		} else {
			step = -1
		}
	case tea.MouseWheelLeft:
		hStep = -1
	case tea.MouseWheelRight:
		hStep = 1
	default:
		return
	}
	if step != 0 {
		rows := h.table.Rows()
		h.table.SetCursor(uikit.Clamp(h.table.Cursor()+step, 0, max(len(rows)-1, 0)))
	}
	if hStep != 0 {
		uikit.MoveTableColumn(&h.table, &h.selectedCol, &h.offset, h.viewportWidth(), hStep)
	}
}

// handleKey routes one key press through the modal. It returns false only
// when the press should close the modal (Escape outside the filter and
// the viewer); every other press is swallowed so no key reaches the panes
// underneath. The copy key returns the cell's raw value as a
// ClipboardRequested event for the root to act on.
func (h *history) handleKey(msg tea.KeyPressMsg) (bool, uikit.Event) {
	if h.viewer != nil {
		if msg.Key().Code == tea.KeyEscape {
			h.viewer = nil
			return true, nil
		}
		h.viewer.Update(msg)
		return true, nil
	}
	if h.filterFocused {
		switch msg.Key().Code {
		case tea.KeyEscape:
			h.filterFocused = false
			h.filter.Blur()
			return true, nil
		}
		h.filter, _ = h.filter.Update(msg)
		h.applyFilter()
		return true, nil
	}
	switch msg.Key().Code {
	case '/':
		h.filterFocused = true
		h.filter.Focus()
		return true, nil
	case tea.KeyEscape:
		return false, nil // caller closes the modal
	case 's':
		h.cycleSort()
		return true, nil
	case 'v':
		h.openViewer()
		return true, nil
	case 'y':
		if text, ok := h.copyCell(); ok {
			return true, uikit.ClipboardRequested{Text: text}
		}
		return true, nil
	case 'n', tea.KeyPgDown:
		h.nextPage()
		return true, nil
	case 'p', tea.KeyPgUp:
		h.prevPage()
		return true, nil
	}
	if uikit.MoveTableCell(&h.table, &h.selectedCol, &h.offset, h.viewportWidth(), msg) {
		return true, nil
	}
	// Swallow everything else so no key reaches the panes underneath.
	return true, nil
}
