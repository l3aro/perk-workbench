package workbench

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/log"
)

func TestNotifications_persistInSQLiteAndExpire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{NotificationRetentionDays: 1})

	entry := notificationEntry{createdAt: time.Now(), title: notificationTitle, description: "ready"}
	if _, err := saveNotification(path, "conn-a", entry); err != nil {
		t.Fatal(err)
	}
	db, err := openNotificationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description) VALUES (?, ?, ?, ?)`,
		"conn-a", time.Now().AddDate(0, 0, -2).UnixNano(), notificationTitle, "expired"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	got := loadNotifications(path, "conn-a")
	if len(got) != 1 || got[0].description != entry.description {
		t.Fatalf("notifications = %#v, want only the fresh entry", got)
	}
	if got[0].id == 0 {
		t.Fatal("persisted notification has no SQLite row ID")
	}
}

func TestNotifications_scopeEntriesByConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	aEntry := notificationEntry{createdAt: time.Now(), title: notificationTitle, description: "for A"}
	bEntry := notificationEntry{createdAt: time.Now(), title: notificationTitle, description: "for B"}
	if _, err := saveNotification(path, "conn-a", aEntry); err != nil {
		t.Fatal(err)
	}
	if _, err := saveNotification(path, "conn-b", bEntry); err != nil {
		t.Fatal(err)
	}
	if _, err := saveNotification(path, "conn-a", notificationEntry{createdAt: time.Now(), title: notificationTitle, description: "for A again"}); err != nil {
		t.Fatal(err)
	}

	gotA := loadNotifications(path, "conn-a")
	if len(gotA) != 2 || gotA[0].description != "for A again" || gotA[1].description != "for A" {
		t.Fatalf("scope conn-a notifications = %#v, want the two A entries newest first", gotA)
	}
	gotB := loadNotifications(path, "conn-b")
	if len(gotB) != 1 || gotB[0].description != "for B" {
		t.Fatalf("scope conn-b notifications = %#v, want only B's entry", gotB)
	}
	if got := loadNotifications(path, ""); len(got) != 0 {
		t.Fatalf("empty scope notifications = %#v, want none", got)
	}
}

func TestNotificationRetentionAndTimeout_defaults(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{})

	if got, want := notificationRetentionDays(), 30; got != want {
		t.Fatalf("retention days = %d, want %d", got, want)
	}
	if got, want := notificationPopupDuration(), 10*time.Second; got != want {
		t.Fatalf("popup duration = %v, want %v", got, want)
	}

	SetAppConfig(Config{NotificationRetentionDays: 45, NotificationTimeoutSeconds: 20})
	if got, want := notificationRetentionDays(), 45; got != want {
		t.Fatalf("retention days = %d, want %d", got, want)
	}
	if got, want := notificationPopupDuration(), 20*time.Second; got != want {
		t.Fatalf("popup duration = %v, want %v", got, want)
	}
}

func TestNew_loadsPersistedNotifications(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path, err := notificationPath()
	if err != nil {
		t.Fatal(err)
	}
	entry := notificationEntry{createdAt: time.Now(), title: notificationTitle, description: "persisted"}
	if _, err := saveNotification(path, "conn-a", entry); err != nil {
		t.Fatal(err)
	}

	model := New("", context.Background(), testOpen, false)
	// No connection is open yet, so persisted entries must not surface.
	if got := model.notificationEntries; len(got) != 0 {
		t.Fatalf("notifications before a connection = %#v, want none", got)
	}
	// The scoped load (what updateOpen runs after recordConnection).
	model.connectionID = "conn-a"
	model.notificationEntries = loadNotifications(model.notificationPath, model.connectionID)
	if got := model.notificationEntries; len(got) != 1 || got[0].description != entry.description {
		t.Fatalf("loaded notifications = %#v, want %#v", got, []notificationEntry{entry})
	}
}

func TestNotificationPopup_dismissGenerationGuards(t *testing.T) {
	model := readyModel(t)
	model.notify("first")
	if model.notificationPopup == nil {
		t.Fatal("popup not shown after notify")
	}
	stale := model.notificationGeneration

	model.notify("second")
	// A stale timer must not close the newer popup.
	updated, _ := model.Update(notificationDismissMsg{generation: stale})
	model = updated.(Model)
	if model.notificationPopup == nil || model.notificationPopup.description != "second" {
		t.Fatalf("stale dismiss closed the popup: %#v", model.notificationPopup)
	}

	// A matching timer closes it.
	updated, _ = model.Update(notificationDismissMsg{generation: model.notificationGeneration})
	model = updated.(Model)
	if model.notificationPopup != nil {
		t.Fatal("matching dismiss did not close the popup")
	}
}

func TestNotificationPopup_rendersAndFooterOmitsStatus(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notify("ready: chinook")
	model = resizeModel(model, 100, 24)

	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "ready: chinook") {
		t.Fatalf("popup view = %q, want the notification text", view)
	}
	if strings.Contains(model.footer(), "ready: chinook") {
		t.Fatalf("footer = %q, want status removed from the footer", model.footer())
	}
}

func TestNotificationPopup_clickOpensHistoryWithSelectedEntry(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notificationPath = filepath.Join(t.TempDir(), "data.db")
	model.connectionID = "conn-a"
	model.notify("first")
	first := *model.notificationPopup
	model.notify("second")
	if first.id == 0 {
		t.Fatal("persisted notification has no row ID")
	}
	model.notificationPopup = &first

	bounds, ok := model.notificationPopupBounds()
	if !ok {
		t.Fatal("no popup bounds")
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: bounds.Min.X + 1, Y: bounds.Min.Y + 1, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.notificationHistory == nil {
		t.Fatal("popup click did not open the notification history")
	}
	selected, ok := model.notificationHistory.selected()
	if !ok || selected.id != first.id {
		t.Fatalf("selected entry = %#v, want the clicked entry %#v", selected, first)
	}
	if !model.notificationPopupSwallowRelease {
		t.Fatal("popup click did not arm the release swallow")
	}
	// The trailing release is consumed.
	updated, _ = model.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft})
	model = updated.(Model)
	if model.notificationPopupSwallowRelease {
		t.Fatal("release swallow not consumed")
	}
	// Escape closes the modal.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.notificationHistory != nil {
		t.Fatal("escape did not close the notification history")
	}
}

func TestNotificationPopup_clickWithoutScopeOpensDetailOnly(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.connectionID = ""
	model.notify("database unavailable: boom")
	popup := *model.notificationPopup
	model.notificationPopup = &popup

	bounds, ok := model.notificationPopupBounds()
	if !ok {
		t.Fatal("no popup bounds")
	}
	updated, _ := model.Update(tea.MouseClickMsg{X: bounds.Min.X + 1, Y: bounds.Min.Y + 1, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.notificationDetail == nil {
		t.Fatal("popup click without a scope did not open the detail overlay")
	}
	if model.notificationHistory != nil {
		t.Fatal("popup click without a scope opened a list column")
	}
	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "database unavailable: boom") {
		t.Fatalf("detail view = %q, want the notification text", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.notificationDetail != nil {
		t.Fatal("escape did not close the detail overlay")
	}
}

func TestNotificationHistory_modalFiltersAndNavigates(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notificationEntries = []notificationEntry{
		{id: 3, createdAt: time.Now(), title: notificationTitle, description: "row updated"},
		{id: 2, createdAt: time.Now(), title: notificationTitle, description: "column deleted"},
		{id: 1, createdAt: time.Now(), title: notificationTitle, description: "ready: chinook"},
	}

	updated, _ := model.handlePaletteCommand("notifications.show")
	model = updated.(Model)
	if model.notificationHistory == nil {
		t.Fatal("palette command did not open the notification history")
	}
	selected, ok := model.notificationHistory.selected()
	if !ok || selected.id != 3 {
		t.Fatalf("initial selection = %#v, want newest entry id 3", selected)
	}

	// Filter narrows the list; unmatched filter leaves no selection.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if len(model.notificationHistory.filtered) != 1 || model.notificationHistory.filtered[0].id != 2 {
		t.Fatalf("filtered entries = %#v, want only the deleted column entry", model.notificationHistory.filtered)
	}

	// Esc blurs the filter, second Esc closes the modal.
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.notificationHistory != nil {
		t.Fatal("escape did not close the notification history")
	}
}

func TestNotificationHistory_escapeExitsFilteringFirst(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notificationEntries = []notificationEntry{
		{id: 1, createdAt: time.Now(), title: notificationTitle, description: "ready"},
	}
	model.notificationHistory = newNotificationHistory(model.notificationEntries, 0, model.width, model.height)
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !model.notificationHistory.filterFocused {
		t.Fatal("filter not focused after /")
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.notificationHistory == nil {
		t.Fatal("first escape closed the modal instead of exiting filtering")
	}
	if model.notificationHistory.filterFocused {
		t.Fatal("filter still focused after first escape")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.notificationHistory != nil {
		t.Fatal("second escape did not close the modal")
	}
}

func TestNotificationHistory_cellTravelAndViewer(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	long := strings.Repeat("word ", 600)
	model.notificationEntries = []notificationEntry{
		{id: 2, createdAt: time.Now(), title: notificationTitle, description: long},
		{id: 1, createdAt: time.Now(), title: notificationTitle, description: "short"},
	}
	model.notificationHistory = newNotificationHistory(model.notificationEntries, 0, model.width, model.height)

	if col := model.notificationHistory.selectedCol; col != 0 {
		t.Fatalf("modal does not start on the first column, got %d", col)
	}
	// j moves the row cursor down.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if selected, _ := model.notificationHistory.selected(); selected.id != 1 {
		t.Fatalf("selection after j = %#v, want id 1", selected)
	}
	// Back up to the long entry.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if selected, _ := model.notificationHistory.selected(); selected.id != 2 {
		t.Fatalf("selection after k = %#v, want id 2", selected)
	}
	// l travels to the next column, h back to the first.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if model.notificationHistory.selectedCol != 1 {
		t.Fatalf("selectedCol after l = %d, want 1", model.notificationHistory.selectedCol)
	}
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if model.notificationHistory.selectedCol != 0 {
		t.Fatalf("selectedCol after h = %d, want 0", model.notificationHistory.selectedCol)
	}

	// v opens the viewer with the untruncated description; Escape closes it.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if model.notificationHistory.viewer == nil {
		t.Fatal("v did not open the viewer")
	}
	if got := model.notificationHistory.viewer.column; got != "Description" {
		t.Fatalf("viewer column = %q, want Description", got)
	}
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if model.notificationHistory.viewer != nil {
		t.Fatal("escape did not close the viewer")
	}
	// A key press over the open viewer scrolls it, not the table.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	before := model.notificationHistory.viewer.viewport.YOffset()
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := model.notificationHistory.viewer.viewport.YOffset(); got <= before {
		t.Fatalf("viewer scroll offset = %d, want it to grow past %d", got, before)
	}
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if model.notificationHistory.viewer != nil {
		t.Fatal("escape did not close the viewer")
	}
}

func TestNotificationHistory_sortCyclesAndHeaderClick(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	base := time.Now()
	model.notificationEntries = []notificationEntry{
		{id: 3, createdAt: base, title: "zeta", description: "third"},
		{id: 2, createdAt: base.Add(-2 * time.Minute), title: "alpha", description: "second"},
		{id: 1, createdAt: base.Add(-3 * time.Minute), title: "mid", description: "first"},
	}
	model.notificationHistory = newNotificationHistory(model.notificationEntries, 0, model.width, model.height)
	h := model.notificationHistory

	// s on the Time column cycles ascending, descending, then back to the
	// default entry order.
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if h.sortCol != 0 || h.sortDesc || h.filtered[0].id != 1 {
		t.Fatalf("after first s: sort = col %d desc %t, first = %d, want Time ascending with id 1", h.sortCol, h.sortDesc, h.filtered[0].id)
	}
	if title := h.table.Columns()[0].Title; title != "Time ▲" {
		t.Fatalf("sorted header = %q, want the ascending marker", title)
	}
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if h.sortCol != 0 || !h.sortDesc || h.filtered[0].id != 3 {
		t.Fatalf("after second s: sort = col %d desc %t, first = %d, want Time descending with id 3", h.sortCol, h.sortDesc, h.filtered[0].id)
	}
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if h.sortCol != -1 || h.filtered[0].id != 3 {
		t.Fatalf("after third s: sort = col %d, first = %d, want the default order with id 3", h.sortCol, h.filtered[0].id)
	}

	// A header click on the Title column sorts by title ascending; the
	// selected column follows the clicked column.
	columns := h.table.Columns()
	titleStart := 2 + (columns[0].Width + 2*spaceCompact) + (columns[1].Width + 2*spaceCompact)
	updated, _ := model.Update(tea.MouseClickMsg{X: titleStart + 2, Y: 6, Button: tea.MouseLeft})
	model = updated.(Model)
	h = model.notificationHistory
	if h.sortCol != 2 || h.sortDesc || h.selectedCol != 2 || h.filtered[0].id != 2 {
		t.Fatalf("after header click: sort = col %d desc %t selected %d first %d, want Title ascending, selected col 2, first id 2", h.sortCol, h.sortDesc, h.selectedCol, h.filtered[0].id)
	}
	// Clicking the same header again descends.
	updated, _ = model.Update(tea.MouseClickMsg{X: titleStart + 2, Y: 6, Button: tea.MouseLeft})
	model = updated.(Model)
	h = model.notificationHistory
	if h.sortCol != 2 || !h.sortDesc || h.filtered[0].id != 3 {
		t.Fatalf("after second header click: sort = col %d desc %t, first = %d, want Title descending with id 3", h.sortCol, h.sortDesc, h.filtered[0].id)
	}
	// A third s restores the default entry order, not the Title-desc
	// order: Title asc, desc, then newest-first (id 3, 2, 1).
	h.handleKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := make([]int64, len(h.filtered))
	for index, entry := range h.filtered {
		got[index] = entry.id
	}
	if h.sortCol != -1 || !slices.Equal(got, []int64{3, 2, 1}) {
		t.Fatalf("after third s on Title: sort = col %d, order = %v, want default newest-first 3 2 1", h.sortCol, got)
	}
}

func TestNotificationHistory_paginationAndButtons(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	entries := make([]notificationEntry, 25)
	for i := range entries {
		entries[i] = notificationEntry{id: int64(25 - i), createdAt: time.Now(), title: notificationTitle, description: "entry"}
	}
	model.notificationEntries = entries
	h := newNotificationHistory(model.notificationEntries, 0, model.width, model.height)
	model.notificationHistory = h

	if h.pageSize != 12 {
		t.Fatalf("page size = %d, want 12 at height 24", h.pageSize)
	}
	if got := h.statusText(); got != "1-12 of 25 | page 1/3" {
		t.Fatalf("status = %q, want the first page summary", got)
	}
	if pager := h.pager(); pager.prevEnabled || !pager.nextEnabled {
		t.Fatalf("pager = prev %t next %t, want only Next on page 0", pager.prevEnabled, pager.nextEnabled)
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
	h.handleClick(pager.nextStart+1, h.height-4)
	if h.page != 1 {
		t.Fatalf("after Next click: page = %d, want 1", h.page)
	}
	pager = h.pager()
	h.handleClick(pager.prevStart+1, h.height-4)
	if h.page != 0 {
		t.Fatalf("after Prev click: page = %d, want 0", h.page)
	}

	// The last page shows the remainder and disables Next.
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if h.page != 2 || h.statusText() != "25-25 of 25 | page 3/3" {
		t.Fatalf("on last page: page = %d, status = %q", h.page, h.statusText())
	}
	if pager := h.pager(); !pager.prevEnabled || pager.nextEnabled {
		t.Fatalf("pager = prev %t next %t, want only Prev on the last page", pager.prevEnabled, pager.nextEnabled)
	}
	h.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if h.page != 2 {
		t.Fatalf("n on the last page moved to page %d", h.page)
	}
}

func TestNotificationHistory_copyCell(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notificationEntries = []notificationEntry{
		{id: 1, createdAt: time.Now(), title: notificationTitle, description: "copy me"},
	}
	h := newNotificationHistory(model.notificationEntries, 0, model.width, model.height)

	// Travel to the Description column and copy the raw cell value.
	for range 3 {
		h.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	}
	handled, cmd := h.handleKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !handled {
		t.Fatal("y was not handled")
	}
	if cmd == nil {
		t.Fatal("y returned no copy command")
	}
	// The command chain must run cleanly (OSC 52 message + native no-op).
	for _, message := range executeCommandAll(cmd) {
		_ = message
	}

	// Time and Level cells copy too.
	h.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	h.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if _, cmd := h.handleKey(tea.KeyPressMsg{Code: 'y', Text: "y"}); cmd == nil {
		t.Fatal("y on the Level cell returned no copy command")
	}
}

func TestNotificationHistory_filterSearchesAllColumns(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notificationEntries = []notificationEntry{
		{id: 3, createdAt: time.Now(), title: notificationTitle, description: "row updated", level: storedLogLevel(log.LevelError)},
		{id: 2, createdAt: time.Now(), title: notificationTitle, description: "column deleted"},
		{id: 1, createdAt: time.Now(), title: notificationTitle, description: "ready: chinook"},
	}
	h := newNotificationHistory(model.notificationEntries, 0, model.width, model.height)

	h.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	h.handleKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	h.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if len(h.filtered) != 1 || h.filtered[0].id != 3 {
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
	if len(h.filtered) != 1 || h.filtered[0].id != 3 {
		t.Fatalf("level search filtered = %#v, want only the error entry", h.filtered)
	}
}
