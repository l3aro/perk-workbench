package workbench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestNotificationHistory_paneNavigationAndScrolling(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	long := strings.Repeat("word ", 200)
	model.notificationEntries = []notificationEntry{
		{id: 2, createdAt: time.Now(), title: notificationTitle, description: long},
		{id: 1, createdAt: time.Now(), title: notificationTitle, description: "short"},
	}
	model.notificationHistory = newNotificationHistory(model.notificationEntries, 0, model.width, model.height)

	if model.notificationHistory.pane != notificationHistoryListPane {
		t.Fatal("modal does not start on the list pane")
	}
	// j moves the list selection down.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if selected, _ := model.notificationHistory.selected(); selected.id != 1 {
		t.Fatalf("list selection after j = %#v, want id 1", selected)
	}
	// Back up to the long entry so the detail pane has something to scroll.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if selected, _ := model.notificationHistory.selected(); selected.id != 2 {
		t.Fatalf("list selection after k = %#v, want id 2", selected)
	}
	// l activates the detail pane; j now scrolls the detail viewport.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if model.notificationHistory.pane != notificationHistoryDetailPane {
		t.Fatal("l did not activate the detail pane")
	}
	before := model.notificationHistory.detail.YOffset()
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := model.notificationHistory.detail.YOffset(); got <= before {
		t.Fatalf("detail scroll offset = %d, want it to grow past %d", got, before)
	}
	// h returns to the list pane.
	model.notificationHistory.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if model.notificationHistory.pane != notificationHistoryListPane {
		t.Fatal("h did not return to the list pane")
	}
}
