package notification

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// testLayout is the 100x24 screen snapshot the component tests drive.
var testLayout = uikit.Layout{Width: 100, Height: 24}

// render draws the component's overlays and returns the raw canvas output.
func render(m Model) string {
	canvas := uv.NewScreenBuffer(testLayout.Width, testLayout.Height)
	screen.Clear(canvas)
	m.Draw(canvas, testLayout)
	return canvas.Render()
}

// openTestStore opens a scratch notification store in a temp directory.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "data.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// colOf returns the display column of sub's first occurrence in line, or -1.
func colOf(line, sub string) int {
	index := strings.Index(line, sub)
	if index < 0 {
		return -1
	}
	return ansi.StringWidth(line[:index])
}

func TestPopup_dismissGenerationGuards(t *testing.T) {
	m := New()
	updated, _ := m.Show(StatusEntry("first"), false, "", nil, time.Minute)
	m = updated
	if m.Popup == nil {
		t.Fatal("popup not shown after Show")
	}
	stale := m.Generation

	updated, _ = m.Show(StatusEntry("second"), false, "", nil, time.Minute)
	m = updated
	// A stale timer must not close the newer popup.
	m, _, _ = m.Update(DismissMsg{Generation: stale}, testLayout, nil)
	if m.Popup == nil || m.Popup.Description != "second" {
		t.Fatalf("stale dismiss closed the popup: %#v", m.Popup)
	}

	// A matching timer closes it.
	m, _, _ = m.Update(DismissMsg{Generation: m.Generation}, testLayout, nil)
	if m.Popup != nil {
		t.Fatal("matching dismiss did not close the popup")
	}
}

func TestPopup_rendersDescription(t *testing.T) {
	m, _ := New().Show(StatusEntry("ready: chinook"), false, "", nil, time.Minute)
	view := ansi.Strip(render(m))
	if !strings.Contains(view, "ready: chinook") {
		t.Fatalf("popup view = %q, want the notification text", view)
	}
}

func TestPopup_clickOpensHistoryWithSelectedEntry(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	m := New()
	updated, _ := m.Show(StatusEntry("first"), true, "conn-a", store, time.Minute)
	m = updated
	first := *m.Popup
	updated, _ = m.Show(StatusEntry("second"), true, "conn-a", store, time.Minute)
	m = updated
	if first.ID == 0 {
		t.Fatal("persisted notification has no row ID")
	}
	m.Popup = &first

	bounds, ok := m.PopupBounds(testLayout)
	if !ok {
		t.Fatal("no popup bounds")
	}
	m, _, _ = m.Update(tea.MouseClickMsg{X: bounds.Min.X + 1, Y: bounds.Min.Y + 1, Button: tea.MouseLeft}, testLayout, nil)
	if m.History == nil {
		t.Fatal("popup click did not open the notification history")
	}
	selected, ok := m.History.selected()
	if !ok || selected.ID != first.ID {
		t.Fatalf("selected entry = %#v, want the clicked entry %#v", selected, first)
	}
	if !m.PopupSwallowRelease {
		t.Fatal("popup click did not arm the release swallow")
	}
	// The trailing release is consumed.
	m, _, _ = m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft}, testLayout, nil)
	if m.PopupSwallowRelease {
		t.Fatal("release swallow not consumed")
	}
	// Escape closes the modal.
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testLayout, nil)
	if m.History != nil {
		t.Fatal("escape did not close the notification history")
	}
}

func TestPopup_clickWithoutScopeOpensDetailOnly(t *testing.T) {
	m, _ := New().Show(StatusEntry("database unavailable: boom"), false, "", nil, time.Minute)
	popup := *m.Popup
	m.Popup = &popup

	bounds, ok := m.PopupBounds(testLayout)
	if !ok {
		t.Fatal("no popup bounds")
	}
	m, _, _ = m.Update(tea.MouseClickMsg{X: bounds.Min.X + 1, Y: bounds.Min.Y + 1, Button: tea.MouseLeft}, testLayout, nil)
	if m.Detail == nil {
		t.Fatal("popup click without a scope did not open the detail overlay")
	}
	if m.History != nil {
		t.Fatal("popup click without a scope opened a list column")
	}
	view := ansi.Strip(render(m))
	if !strings.Contains(view, "database unavailable: boom") {
		t.Fatalf("detail view = %q, want the notification text", view)
	}
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testLayout, nil)
	if m.Detail != nil {
		t.Fatal("escape did not close the detail overlay")
	}
}

func TestHistory_modalFiltersAndNavigates(t *testing.T) {
	m := New()
	m.SetEntries([]Entry{
		{ID: 3, CreatedAt: time.Now(), Title: title, Description: "row updated"},
		{ID: 2, CreatedAt: time.Now(), Title: title, Description: "column deleted"},
		{ID: 1, CreatedAt: time.Now(), Title: title, Description: "ready: chinook"},
	})
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	if m.History == nil {
		t.Fatal("OpenHistory did not open the notification history")
	}
	selected, ok := m.History.selected()
	if !ok || selected.ID != 3 {
		t.Fatalf("initial selection = %#v, want newest entry id 3", selected)
	}

	// Filter narrows the list; unmatched filter leaves no selection.
	m.History.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.History.handleKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m.History.handleKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m.History.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if len(m.History.filtered) != 1 || m.History.filtered[0].ID != 2 {
		t.Fatalf("filtered entries = %#v, want only the deleted column entry", m.History.filtered)
	}

	// Esc blurs the filter, second Esc closes the modal.
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testLayout, nil)
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testLayout, nil)
	if m.History != nil {
		t.Fatal("escape did not close the notification history")
	}
}

func TestHistory_escapeExitsFilteringFirst(t *testing.T) {
	m := New()
	m.SetEntries([]Entry{
		{ID: 1, CreatedAt: time.Now(), Title: title, Description: "ready"},
	})
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	m.History.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.History.filterFocused {
		t.Fatal("filter not focused after /")
	}

	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testLayout, nil)
	if m.History == nil {
		t.Fatal("first escape closed the modal instead of exiting filtering")
	}
	if m.History.filterFocused {
		t.Fatal("filter still focused after first escape")
	}
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testLayout, nil)
	if m.History != nil {
		t.Fatal("second escape did not close the modal")
	}
}

func TestHistory_cellTravelAndViewer(t *testing.T) {
	m := New()
	long := strings.Repeat("word ", 600)
	m.SetEntries([]Entry{
		{ID: 2, CreatedAt: time.Now(), Title: title, Description: long},
		{ID: 1, CreatedAt: time.Now(), Title: title, Description: "short"},
	})
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	h := m.History

	if col := h.selectedCol; col != 0 {
		t.Fatalf("modal does not start on the first column, got %d", col)
	}
	// j moves the row cursor down.
	h.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if selected, _ := h.selected(); selected.ID != 1 {
		t.Fatalf("selection after j = %#v, want id 1", selected)
	}
	// Back up to the long entry.
	h.handleKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if selected, _ := h.selected(); selected.ID != 2 {
		t.Fatalf("selection after k = %#v, want id 2", selected)
	}
	// l travels to the next column, h back to the first.
	h.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if h.selectedCol != 1 {
		t.Fatalf("selectedCol after l = %d, want 1", h.selectedCol)
	}
	h.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if h.selectedCol != 0 {
		t.Fatalf("selectedCol after h = %d, want 0", h.selectedCol)
	}

	// v opens the viewer with the untruncated description; Escape closes it.
	h.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	h.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	h.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	h.handleKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if h.viewer == nil {
		t.Fatal("v did not open the viewer")
	}
	if got := h.viewer.Column; got != "Description" {
		t.Fatalf("viewer column = %q, want Description", got)
	}
	h.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if h.viewer != nil {
		t.Fatal("escape did not close the viewer")
	}
	// A key press over the open viewer scrolls it, not the table.
	h.handleKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	before := h.viewer.Viewport.YOffset()
	h.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := h.viewer.Viewport.YOffset(); got <= before {
		t.Fatalf("viewer scroll offset = %d, want it to grow past %d", got, before)
	}
	h.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if h.viewer != nil {
		t.Fatal("escape did not close the viewer")
	}
}

func TestHistory_sortCyclesAndHeaderClick(t *testing.T) {
	m := New()
	base := time.Now()
	m.SetEntries([]Entry{
		{ID: 3, CreatedAt: base, Title: "zeta", Description: "third"},
		{ID: 2, CreatedAt: base.Add(-2 * time.Minute), Title: "alpha", Description: "second"},
		{ID: 1, CreatedAt: base.Add(-3 * time.Minute), Title: "mid", Description: "first"},
	})
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	h := m.History

	// s on the Time column cycles ascending, descending, then back to the
	// default entry order.
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if h.sortCol != 0 || h.sortDesc || h.filtered[0].ID != 1 {
		t.Fatalf("after first s: sort = col %d desc %t, first = %d, want Time ascending with id 1", h.sortCol, h.sortDesc, h.filtered[0].ID)
	}
	if title := h.table.Columns()[0].Title; title != "Time ▲" {
		t.Fatalf("sorted header = %q, want the ascending marker", title)
	}
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if h.sortCol != 0 || !h.sortDesc || h.filtered[0].ID != 3 {
		t.Fatalf("after second s: sort = col %d desc %t, first = %d, want Time descending with id 3", h.sortCol, h.sortDesc, h.filtered[0].ID)
	}
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if h.sortCol != -1 || h.filtered[0].ID != 3 {
		t.Fatalf("after third s: sort = col %d, first = %d, want the default order with id 3", h.sortCol, h.filtered[0].ID)
	}

	// A header click on the Title column sorts by title ascending; the
	// selected column follows the clicked column.
	columns := h.table.Columns()
	titleStart := 2 + (columns[0].Width + 2*uikit.SpaceCompact) + (columns[1].Width + 2*uikit.SpaceCompact)
	m, _, _ = m.Update(tea.MouseClickMsg{X: titleStart + 2, Y: 6, Button: tea.MouseLeft}, testLayout, nil)
	h = m.History
	if h.sortCol != 2 || h.sortDesc || h.selectedCol != 2 || h.filtered[0].ID != 2 {
		t.Fatalf("after header click: sort = col %d desc %t selected %d first %d, want Title ascending, selected col 2, first id 2", h.sortCol, h.sortDesc, h.selectedCol, h.filtered[0].ID)
	}
	// Clicking the same header again descends.
	m, _, _ = m.Update(tea.MouseClickMsg{X: titleStart + 2, Y: 6, Button: tea.MouseLeft}, testLayout, nil)
	h = m.History
	if h.sortCol != 2 || !h.sortDesc || h.filtered[0].ID != 3 {
		t.Fatalf("after second header click: sort = col %d desc %t, first = %d, want Title descending with id 3", h.sortCol, h.sortDesc, h.filtered[0].ID)
	}
	// A third s restores the default entry order, not the Title-desc
	// order: Title asc, desc, then newest-first (id 3, 2, 1).
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := make([]int64, len(h.filtered))
	for index, entry := range h.filtered {
		got[index] = entry.ID
	}
	if h.sortCol != -1 || len(got) != 3 || got[0] != 3 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("after third s on Title: sort = col %d, order = %v, want default newest-first 3 2 1", h.sortCol, got)
	}
}

func TestHistory_paginationAndButtons(t *testing.T) {
	m := New()
	entries := make([]Entry, 25)
	for i := range entries {
		entries[i] = Entry{ID: int64(25 - i), CreatedAt: time.Now(), Title: title, Description: "entry"}
	}
	m.SetEntries(entries)
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	h := m.History

	if h.pageSize != 12 {
		t.Fatalf("page size = %d, want 12 at height 24", h.pageSize)
	}
	if got := h.statusText(); got != "1-12 of 25 | page 1/3" {
		t.Fatalf("status = %q, want the first page summary", got)
	}
	if pager := h.pager(); pager.PrevEnabled || !pager.NextEnabled {
		t.Fatalf("pager = prev %t next %t, want only Next on page 0", pager.PrevEnabled, pager.NextEnabled)
	}

	// n pages forward, p back, both keeping the cursor row.
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if h.page != 1 || h.statusText() != "13-24 of 25 | page 2/3" {
		t.Fatalf("after n: page = %d, status = %q", h.page, h.statusText())
	}
	h.handleKey(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if h.page != 0 {
		t.Fatalf("after p: page = %d, want 0", h.page)
	}

	// The Next button pages forward; Prev pages back.
	pager := h.pager()
	h.handleClick(pager.NextStart+1, h.height-4)
	if h.page != 1 {
		t.Fatalf("after Next click: page = %d, want 1", h.page)
	}
	pager = h.pager()
	h.handleClick(pager.PrevStart+1, h.height-4)
	if h.page != 0 {
		t.Fatalf("after Prev click: page = %d, want 0", h.page)
	}

	// The last page shows the remainder and disables Next.
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if h.page != 2 || h.statusText() != "25-25 of 25 | page 3/3" {
		t.Fatalf("on last page: page = %d, status = %q", h.page, h.statusText())
	}
	if pager := h.pager(); !pager.PrevEnabled || pager.NextEnabled {
		t.Fatalf("pager = prev %t next %t, want only Prev on the last page", pager.PrevEnabled, pager.NextEnabled)
	}
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if h.page != 2 {
		t.Fatalf("n on the last page moved to page %d", h.page)
	}
}

func TestHistory_copyCellRequestsClipboard(t *testing.T) {
	m := New()
	m.SetEntries([]Entry{
		{ID: 1, CreatedAt: time.Now(), Title: title, Description: "copy me"},
	})
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	h := m.History

	// Travel to the Description column and copy the raw cell value.
	for range 3 {
		h.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	}
	handled, event := h.handleKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !handled {
		t.Fatal("y was not handled")
	}
	request, ok := event.(uikit.ClipboardRequested)
	if !ok || request.Text != "copy me" {
		t.Fatalf("copy event = %#v, want the raw Description cell", event)
	}

	// Time and Level cells copy too.
	h.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	h.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if handled, event := h.handleKey(tea.KeyPressMsg{Code: 'y', Text: "y"}); !handled {
		t.Fatal("y on the Level cell was not handled")
	} else if _, ok := event.(uikit.ClipboardRequested); !ok {
		t.Fatalf("Level copy event = %#v, want a clipboard request", event)
	}
}

func TestHistory_filterSearchesAllColumns(t *testing.T) {
	m := New()
	m.SetEntries([]Entry{
		{ID: 3, CreatedAt: time.Now(), Title: title, Description: "row updated", Level: StoredLogLevel(log.LevelError)},
		{ID: 2, CreatedAt: time.Now(), Title: title, Description: "column deleted"},
		{ID: 1, CreatedAt: time.Now(), Title: title, Description: "ready: chinook"},
	})
	m.OpenHistory(0, testLayout.Width, testLayout.Height)
	h := m.History

	h.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	h.handleKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if len(h.filtered) != 1 || h.filtered[0].ID != 3 {
		t.Fatalf("filtered = %#v, want only the error entry", h.filtered)
	}
	// The level column is searchable: "error" matches the same entry.
	h.filter.SetValue("")
	h.applyFilter()
	h.handleKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	h.handleKey(tea.KeyPressMsg{Code: 'o', Text: "o"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if len(h.filtered) != 1 || h.filtered[0].ID != 3 {
		t.Fatalf("level search filtered = %#v, want only the error entry", h.filtered)
	}
}

func TestPopup_logEntryRendersLevelTitleIcon(t *testing.T) {
	m, _ := New().Show(LogEntry(log.Entry{Time: time.Now(), Level: log.LevelWarn, Message: "slow query detected"}), false, "", nil, time.Minute)

	popup := m.Popup
	if popup == nil {
		t.Fatal("popup not shown after Show")
	}
	if popup.Level != StoredLogLevel(log.LevelWarn) {
		t.Fatalf("popup level = %d, want %d", popup.Level, StoredLogLevel(log.LevelWarn))
	}
	if !strings.Contains(popup.Title, "Warning") || !strings.Contains(popup.Title, logLevelIcon(log.LevelWarn)) {
		t.Fatalf("popup title = %q, want icon and level title", popup.Title)
	}

	view := ansi.Strip(render(m))
	if !strings.Contains(view, "Warning") || !strings.Contains(view, "slow query detected") {
		t.Fatalf("popup view = %q, want level title and description", view)
	}

	// The level symbol shares the title row; title text and description
	// start after it, aligned to the same column.
	bounds, ok := m.PopupBounds(testLayout)
	if !ok {
		t.Fatal("no popup bounds")
	}
	lines := strings.Split(view, "\n")
	if bounds.Min.Y+1 >= len(lines) {
		t.Fatalf("popup title row %d outside rendered view", bounds.Min.Y+1)
	}
	titleRow := lines[bounds.Min.Y+1]
	icon := logLevelIcon(log.LevelWarn)
	iconCol := colOf(titleRow, icon)
	if iconCol != bounds.Min.X+1 {
		t.Fatalf("level symbol at column %d, want %d (title row start)", iconCol, bounds.Min.X+1)
	}
	bodyX := bounds.Min.X + 1 + max(ansi.StringWidth(icon), 1) + 1
	if got := colOf(titleRow, "Warning"); got != bodyX {
		t.Fatalf("title text at column %d, want %d (after symbol + gap)", got, bodyX)
	}
	found := false
	for _, line := range lines[bounds.Min.Y+2:] {
		if !strings.Contains(line, "slow query detected") {
			continue
		}
		found = true
		if got := colOf(line, "slow query detected"); got != bodyX {
			t.Fatalf("description at column %d, want %d (aligned with title)", got, bodyX)
		}
		break
	}
	if !found {
		t.Fatalf("description row missing from rendered view")
	}
}

func TestPopup_statusEntriesStayNeutral(t *testing.T) {
	m, _ := New().Show(StatusEntry("row updated"), false, "", nil, time.Minute)

	popup := m.Popup
	if popup == nil {
		t.Fatal("popup not shown after Show")
	}
	if popup.Level != levelNone {
		t.Fatalf("status popup level = %d, want %d", popup.Level, levelNone)
	}
	if _, ok := logLevelOf(popup.Level); ok {
		t.Fatal("status popup must not resolve to a log level")
	}
	if got := levelColor(popup.Level); got != uikit.ColorSecondary {
		t.Fatalf("status popup color = %q, want neutral %q", got, uikit.ColorSecondary)
	}
}

func TestBorderColor_matchesLevel(t *testing.T) {
	for _, tc := range []struct {
		level log.Level
		want  string
	}{
		{log.LevelDebug, uikit.ColorMuted},
		{log.LevelInfo, uikit.ColorPrimary},
		{log.LevelWarn, uikit.ColorWarn},
		{log.LevelError, uikit.ColorDanger},
	} {
		if got := borderColor(StoredLogLevel(tc.level)); got != tc.want {
			t.Fatalf("border color for %s = %q, want %q", tc.level, got, tc.want)
		}
	}
	if got := borderColor(levelNone); got != uikit.ColorBorder {
		t.Fatalf("border color for status = %q, want %q", got, uikit.ColorBorder)
	}
}

var rgbPattern = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)

// lastRGB returns the last 38;2 RGB foreground emitted on a raw line.
func lastRGB(line string) string {
	matches := rgbPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	return last[1] + ";" + last[2] + ";" + last[3]
}

// rgbOf converts "#rrggbb" to the "r;g;b" SGR payload.
func rgbOf(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return fmt.Sprintf("%d;%d;%d", r, g, b)
}

func TestPopup_borderMatchesLevelColor(t *testing.T) {
	m, _ := New().Show(LogEntry(log.Entry{Time: time.Now(), Level: log.LevelWarn, Message: "slow query detected"}), false, "", nil, time.Minute)

	bounds, ok := m.PopupBounds(testLayout)
	if !ok {
		t.Fatal("no popup bounds")
	}
	line := strings.Split(render(m), "\n")[bounds.Min.Y]
	if got, want := lastRGB(line), rgbOf(uikit.ColorWarn); got != want {
		t.Fatalf("popup top border color = %s, want %s (line %q)", got, want, line)
	}
}

func TestPopup_statusBorderStaysNeutral(t *testing.T) {
	m, _ := New().Show(StatusEntry("row updated"), false, "", nil, time.Minute)

	bounds, ok := m.PopupBounds(testLayout)
	if !ok {
		t.Fatal("no popup bounds")
	}
	line := strings.Split(render(m), "\n")[bounds.Min.Y]
	if got, want := lastRGB(line), rgbOf(uikit.ColorBorder); got != want {
		t.Fatalf("status popup top border color = %s, want %s (line %q)", got, want, line)
	}
}

func TestLogEntry_levelSurvivesPersistence(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	entry := LogEntry(log.Entry{Time: time.Now(), Level: log.LevelError, Message: "boom"})
	if _, err := store.Append("conn-a", entry, 0); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("conn-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Level != entry.Level {
		t.Fatalf("notifications = %#v, want the entry with level %d", got, entry.Level)
	}
}

// recordingProgram stubs the attached tea.Program for wakeup assertions.
// Send may run on the notifier's goroutine, so the recorded messages are
// mutex-guarded.
type recordingProgram struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (r *recordingProgram) Send(msg tea.Msg) {
	r.mu.Lock()
	r.msgs = append(r.msgs, msg)
	r.mu.Unlock()
}

func (r *recordingProgram) sent() []tea.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tea.Msg{}, r.msgs...)
}

func TestIdleAsyncLogWakesProgram(t *testing.T) {
	queueMu.Lock()
	previous := logProgram
	logProgram = nil
	queueMu.Unlock()
	t.Cleanup(func() {
		queueMu.Lock()
		logProgram = previous
		queueMu.Unlock()
		DrainLogEntries()
	})

	program := &recordingProgram{}
	queueMu.Lock()
	logProgram = program
	queueMu.Unlock()

	DrainLogEntries()

	// An async command logs while the UI is idle: no Update is running.
	EnqueueLogEntry(log.Entry{Time: time.Now(), Level: log.LevelWarn, Message: "async warning"})

	// The enqueue must wake the program loop without any user input.
	deadline := time.After(2 * time.Second)
	for len(program.sent()) == 0 {
		select {
		case <-deadline:
			t.Fatal("enqueued log did not wake the program")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, ok := program.sent()[0].(LogWakeupMsg); !ok {
		t.Fatalf("wakeup message = %T, want LogWakeupMsg", program.sent()[0])
	}

	// The woken loop drains the queue into a popup.
	entries := DrainLogEntries()
	if len(entries) != 1 || entries[0].Message != "async warning" {
		t.Fatalf("drained entries = %#v, want the async warning", entries)
	}
}

func TestUpdate_passesThroughUnrelatedMessages(t *testing.T) {
	m := New()
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Code: 'x', Text: "x"},
		tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft},
		tea.MouseWheelMsg{Button: tea.MouseWheelDown},
		tea.WindowSizeMsg{Width: 80, Height: 24},
	} {
		if m.Consumes(msg, testLayout) {
			t.Fatalf("idle component consumed %T", msg)
		}
		updated, event, cmd := m.Update(msg, testLayout, nil)
		if event != nil || cmd != nil {
			t.Fatalf("idle component Update(%T) returned event %#v cmd %v", msg, event, cmd != nil)
		}
		m = updated
	}
	if m.Popup != nil || m.History != nil || m.Detail != nil {
		t.Fatalf("idle component changed state: %#v", m)
	}
}
