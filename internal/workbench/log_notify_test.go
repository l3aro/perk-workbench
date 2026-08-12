package workbench

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// loadNotificationHistory opens the shared data.db through the store and
// returns the retained entries for one connection scope.
func loadNotificationHistory(t *testing.T, path, connectionID string) []notificationEntry {
	t.Helper()
	store, err := notification.Open(path, notificationRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, err := store.Load(connectionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return notificationEntriesOf(entries)
}

// saveNotificationHistory persists one entry through the store.
func saveNotificationHistory(t *testing.T, path, connectionID string, entry notificationEntry) {
	t.Helper()
	store, err := notification.Open(path, notificationRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Append(connectionID, notification.Entry{
		ID:          entry.id,
		CreatedAt:   entry.createdAt,
		Title:       entry.title,
		Description: entry.description,
		Level:       entry.level,
	}, 0); err != nil {
		t.Fatal(err)
	}
}

func TestLogNotification_popupRendersLevelTitleIcon(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notifyLog(log.Entry{Time: time.Now(), Level: log.LevelWarn, Message: "slow query detected"})

	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("popup not shown after notifyLog")
	}
	if popup.level != storedLogLevel(log.LevelWarn) {
		t.Fatalf("popup level = %d, want %d", popup.level, storedLogLevel(log.LevelWarn))
	}
	if !strings.Contains(popup.title, "Warning") || !strings.Contains(popup.title, logLevelIcon(log.LevelWarn)) {
		t.Fatalf("popup title = %q, want icon and level title", popup.title)
	}

	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "Warning") || !strings.Contains(view, "slow query detected") {
		t.Fatalf("popup view = %q, want level title and description", view)
	}

	// The level symbol shares the title row; title text and description
	// start after it, aligned to the same column.
	bounds, ok := model.notificationPopupBounds()
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

// colOf returns the display column of sub's first occurrence in line, or -1.
func colOf(line, sub string) int {
	index := strings.Index(line, sub)
	if index < 0 {
		return -1
	}
	return ansi.StringWidth(line[:index])
}

func TestLogNotification_statusEntriesStayNeutral(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notify("row updated")

	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("popup not shown after notify")
	}
	if popup.level != notificationLevelNone {
		t.Fatalf("status popup level = %d, want %d", popup.level, notificationLevelNone)
	}
	if _, ok := logLevelOf(popup.level); ok {
		t.Fatal("status popup must not resolve to a log level")
	}
	if got := notificationLevelColor(popup.level); got != colorSecondary {
		t.Fatalf("status popup color = %q, want neutral %q", got, colorSecondary)
	}
}

func TestLogNotification_logCallsInsideUpdateDrainToPopup(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	drainLogNotifications() // clear anything queued by setup

	// The log package notifier enqueues exactly this during an update.
	enqueueLogNotification(log.Entry{Time: time.Now(), Level: log.LevelError, Message: "open database: boom"})

	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("queued log entry did not surface as a popup")
	}
	if popup.level != storedLogLevel(log.LevelError) {
		t.Fatalf("popup level = %d, want %d", popup.level, storedLogLevel(log.LevelError))
	}
	if !strings.Contains(popup.title, "Error") {
		t.Fatalf("popup title = %q, want the Error title", popup.title)
	}
	assertOnlyNotificationTick(t, cmd)
}

// openWithScratch drives the connection form's open flow to completion and
// returns the resulting model.
// openWithScratch drives the connection form's open flow to completion and
// returns the resulting model.
func openWithScratch(t *testing.T, model Model) Model {
	t.Helper()
	model.connection.form.values.name, model.connection.form.values.target = "Scratch", ":memory:"
	updated, command := model.openConnection()
	model = updated.(Model)
	if command == nil {
		t.Fatal("open connection command = nil")
	}
	updated, _ = model.Update(command())
	return updated.(Model)
}

// TestLogNotification_readyStatusIsDebugLog pins the database-ready
// transition: it keeps the ready status text but surfaces as a Debug log
// notification instead of a plain status popup, so the default info level
// silences it while log_level "debug" opts it back in.
func TestLogNotification_readyStatusIsDebugLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() {
		appConfig = previous
		log.SetNotifier(nil)
		log.SetLevel(log.LevelInfo)
	})

	// Default level (info): the open keeps the ready status but the debug
	// entry is dropped, so nothing pops up and nothing reaches event.log.
	SetAppConfig(Config{})
	model := openWithScratch(t, New("", context.Background(), testOpen, false))
	if model.Status != "ready: Scratch" {
		t.Fatalf("status = %q, want the ready status kept", model.Status)
	}
	if popup := model.notifications.popup; popup != nil {
		t.Fatalf("default level surfaced a popup: %#v", popup)
	}
	if _, err := os.Stat(filepath.Join(dir, "perk-workbench", "event.log")); err == nil {
		t.Fatal("event.log created for a debug-only open at the default level")
	}

	// Debug level: the same open surfaces a Debug-typed popup and writes
	// the DEBUG line.
	SetAppConfig(Config{LogLevel: "debug"})
	model = openWithScratch(t, New("", context.Background(), testOpen, false))
	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("debug level did not surface the ready popup")
	}
	if popup.level != storedLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.level, storedLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.title, "Debug") || !strings.Contains(popup.description, "ready: Scratch") {
		t.Fatalf("popup = %#v, want the Debug ready popup", popup)
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DEBUG: ready: Scratch") {
		t.Fatalf("event.log = %q, want the DEBUG ready line", b)
	}
}

// TestLogNotification_editingStatusIsDebugLog pins the profile-edit
// transition: it keeps the status text but surfaces as a Debug log
// notification instead of a plain status popup, so the default info level
// silences it while log_level "debug" opts it back in.
func TestLogNotification_editingStatusIsDebugLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() {
		appConfig = previous
		log.SetNotifier(nil)
		log.SetLevel(log.LevelInfo)
	})

	// Default level (info): the edit keeps the status text but the debug
	// entry is dropped, so nothing pops up.
	SetAppConfig(Config{})
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{Name: "Scratch", Target: ":memory:"}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.setFocus(connectionFocusRecent)
	drainLogNotifications()
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.Status != "editing Scratch" {
		t.Fatalf("status = %q, want the editing status kept", model.Status)
	}
	if popup := model.notifications.popup; popup != nil {
		t.Fatalf("default level surfaced a popup: %#v", popup)
	}

	// Debug level: the same edit surfaces a Debug-typed popup and writes
	// the DEBUG line.
	SetAppConfig(Config{LogLevel: "debug"})
	model = New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{Name: "Scratch", Target: ":memory:"}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.setFocus(connectionFocusRecent)
	drainLogNotifications()
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("debug level did not surface the editing popup")
	}
	if popup.level != storedLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.level, storedLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.title, "Debug") || !strings.Contains(popup.description, "editing Scratch") {
		t.Fatalf("popup = %#v, want the Debug editing popup", popup)
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DEBUG: editing Scratch") {
		t.Fatalf("event.log = %q, want the DEBUG editing line", b)
	}
}

// TestLogNotification_openingStatusIsDebugLog pins the connection-form
// opening transition: it keeps the opening status text but surfaces as a
// Debug log notification instead of a plain status popup, so the default
// info level silences it while log_level "debug" opts it back in.
func TestLogNotification_openingStatusIsDebugLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() {
		appConfig = previous
		log.SetNotifier(nil)
		log.SetLevel(log.LevelInfo)
	})

	// Default level (info): the form keeps the opening status text but the
	// debug entry is dropped, so nothing pops up and nothing reaches
	// event.log.
	SetAppConfig(Config{})
	model := New("", context.Background(), testOpen, false)
	model.connection.form.values.name, model.connection.form.values.target = "Scratch", ":memory:"
	updated, _ := model.Update(connectionActionMsg{action: connectionActionConnect})
	model = updated.(Model)
	if model.Status != "opening Scratch" {
		t.Fatalf("status = %q, want the opening status kept", model.Status)
	}
	if popup := model.notifications.popup; popup != nil {
		t.Fatalf("default level surfaced a popup: %#v", popup)
	}
	if _, err := os.Stat(filepath.Join(dir, "perk-workbench", "event.log")); err == nil {
		t.Fatal("event.log created for a debug-only open at the default level")
	}

	// Debug level: the same open surfaces a Debug-typed popup and writes
	// the DEBUG line. The opening entry is transient: completing the open
	// persists the ready entry under the new profile scope, never the
	// opening one.
	SetAppConfig(Config{LogLevel: "debug"})
	model = New("", context.Background(), testOpen, false)
	model.connection.form.values.name, model.connection.form.values.target = "Scratch", ":memory:"
	updated, _ = model.Update(connectionActionMsg{action: connectionActionConnect})
	model = updated.(Model)
	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("debug level did not surface the opening popup")
	}
	if popup.level != storedLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.level, storedLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.title, "Debug") || !strings.Contains(popup.description, "opening Scratch") {
		t.Fatalf("popup = %#v, want the Debug opening popup", popup)
	}
	// The returned command is a batch with the dismiss tick; drive the
	// open target directly so the open completes deterministically.
	updated, _ = model.Update(model.openTarget(":memory:")())
	model = updated.(Model)
	history := loadNotificationHistory(t, filepath.Join(dir, "perk-workbench", "data.db"), model.connectionID)
	if len(history) == 0 {
		t.Fatal("history has no retained entries after the open")
	}
	for _, entry := range history {
		if strings.Contains(entry.description, "opening") {
			t.Fatalf("history retained the transient opening entry: %#v", entry)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DEBUG: opening Scratch") {
		t.Fatalf("event.log = %q, want the DEBUG opening line", b)
	}
}

// TestLogNotification_openingPickerStatusIsDebugLog pins the picker's
// opening transition: same Debug log treatment as the connection form, so
// the default info level silences it while log_level "debug" opts it back
// in.
func TestLogNotification_openingPickerStatusIsDebugLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() {
		appConfig = previous
		log.SetNotifier(nil)
		log.SetLevel(log.LevelInfo)
	})

	// Default level (info): the picker keeps the opening status text but
	// the debug entry is dropped, so nothing pops up.
	SetAppConfig(Config{})
	model := New("", context.Background(), testOpen, false)
	updated, _ := model.Update(pickerSelectionMsg{target: ":memory:"})
	model = updated.(Model)
	if model.Status != "opening database" {
		t.Fatalf("status = %q, want the opening status kept", model.Status)
	}
	if popup := model.notifications.popup; popup != nil {
		t.Fatalf("default level surfaced a popup: %#v", popup)
	}

	// Debug level: the same selection surfaces a Debug-typed popup and
	// writes the DEBUG line. The opening entry is transient: completing
	// the open persists the ready entry under the new profile scope,
	// never the opening one.
	SetAppConfig(Config{LogLevel: "debug"})
	model = New("", context.Background(), testOpen, false)
	updated, _ = model.Update(pickerSelectionMsg{target: ":memory:"})
	model = updated.(Model)
	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("debug level did not surface the opening popup")
	}
	if popup.level != storedLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.level, storedLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.title, "Debug") || !strings.Contains(popup.description, "opening database") {
		t.Fatalf("popup = %#v, want the Debug opening popup", popup)
	}
	// The returned command is a batch with the dismiss tick; drive the
	// open target directly so the open completes deterministically.
	updated, _ = model.Update(model.openTarget(":memory:")())
	model = updated.(Model)
	history := loadNotificationHistory(t, filepath.Join(dir, "perk-workbench", "data.db"), model.connectionID)
	if len(history) == 0 {
		t.Fatal("history has no retained entries after the open")
	}
	for _, entry := range history {
		if strings.Contains(entry.description, "opening") {
			t.Fatalf("history retained the transient opening entry: %#v", entry)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DEBUG: opening database") {
		t.Fatalf("event.log = %q, want the DEBUG opening line", b)
	}
}

// TestLogNotification_openingDoesNotBindPreviousScope pins the transient
// rule: the opening notice logs before the new connection profile exists,
// so it must never persist under a scope that is still live. (Today's UI
// clears the scope when leaving a session, but any future connect path
// that skips that reset would otherwise mis-scope the entry.)
func TestLogNotification_openingDoesNotBindPreviousScope(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() {
		appConfig = previous
		log.SetNotifier(nil)
		log.SetLevel(log.LevelInfo)
	})
	SetAppConfig(Config{LogLevel: "debug"})
	historyPath := filepath.Join(dir, "perk-workbench", "data.db")

	model := New("", context.Background(), testOpen, false)
	// A live connection's scope is still assigned while the next open is
	// in flight.
	model.connectionID = "live-scope"
	model.connection.form.values.name, model.connection.form.values.target = "Second", ":memory:"

	// The Debug opening popup shows while the scope is still the live
	// connection's.
	updated, _ := model.Update(connectionActionMsg{action: connectionActionConnect})
	model = updated.(Model)
	popup := model.notifications.popup
	if popup == nil || !strings.Contains(popup.title, "Debug") || !strings.Contains(popup.description, "opening Second") {
		t.Fatalf("popup = %#v, want the Debug opening Second popup", popup)
	}
	if got := loadNotificationHistory(t, historyPath, "live-scope"); len(got) != 0 {
		t.Fatalf("opening entry bound to the live scope: %#v", got)
	}

	// Completing the open assigns the new scope and persists only its
	// ready entry.
	updated, _ = model.Update(model.openTarget(":memory:")())
	model = updated.(Model)
	if model.connectionID == "live-scope" {
		t.Fatal("open did not assign a new profile scope")
	}
	for _, entries := range [][]notificationEntry{
		loadNotificationHistory(t, historyPath, "live-scope"),
		loadNotificationHistory(t, historyPath, model.connectionID),
	} {
		for _, entry := range entries {
			if strings.Contains(entry.description, "opening") {
				t.Fatalf("opening entry retained in history: %#v", entry)
			}
		}
	}
	if got := loadNotificationHistory(t, historyPath, model.connectionID); len(got) == 0 {
		t.Fatal("new scope has no retained entries after the open")
	}
}

func TestLogNotification_endToEndFileAndPopup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Cleanup(func() { log.SetNotifier(nil) })

	model := resizeModel(readyModel(t), 100, 24)
	drainLogNotifications() // clear anything queued by setup

	log.Error("open database", errors.New("boom"))

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("log.Error did not surface as a popup")
	}
	if !strings.Contains(popup.title, "Error") || popup.level != storedLogLevel(log.LevelError) {
		t.Fatalf("popup = %#v, want the Error popup", popup)
	}
	if !strings.Contains(popup.description, "open database: boom") {
		t.Fatalf("popup description = %q, want the log message", popup.description)
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ERROR: open database: boom") {
		t.Fatalf("event.log = %q, want the ERROR line", b)
	}
}

func TestNotifications_persistLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	entry := notificationEntry{createdAt: time.Now(), title: logLevelIcon(log.LevelError) + " Error", description: "boom", level: storedLogLevel(log.LevelError)}
	saveNotificationHistory(t, path, "conn-a", entry)

	got := loadNotificationHistory(t, path, "conn-a")
	if len(got) != 1 || got[0].level != entry.level {
		t.Fatalf("notifications = %#v, want the entry with level %d", got, entry.level)
	}
}

func TestNotifications_migrateOldSchemaAddsLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	// Build a pre-level database exactly as the old schema defined it.
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE notifications (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description) VALUES (?, ?, ?, ?)`,
		"conn-a", time.Now().UnixNano(), "Legacy", "pre-level row"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	// Opening through the store migrates the schema.
	store, err := notification.Open(path, notificationRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	loaded, err := store.Load("conn-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	legacy := notificationEntriesOf(loaded)
	if len(legacy) != 1 || legacy[0].level != notificationLevelNone {
		t.Fatalf("legacy notifications = %#v, want one neutral-level entry", legacy)
	}
	if _, ok := logLevelOf(legacy[0].level); ok {
		t.Fatalf("legacy entry resolved to a log level: %#v", legacy[0])
	}

	// New entries keep their severity through the migrated schema.
	entry := notificationEntry{createdAt: time.Now(), title: "ℹ Info", description: "fresh", level: storedLogLevel(log.LevelInfo)}
	if _, err = store.Append("conn-a", notification.Entry{ID: entry.id, CreatedAt: entry.createdAt, Title: entry.title, Description: entry.description, Level: entry.level}, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load("conn-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	got := notificationEntriesOf(loaded)
	if len(got) != 2 || got[0].level != entry.level {
		t.Fatalf("notifications = %#v, want the fresh entry with level %d", got, entry.level)
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

func TestLogNotification_idleAsyncLogWakesProgram(t *testing.T) {
	previous := logProgram
	t.Cleanup(func() { logProgram = previous })

	program := &recordingProgram{}
	logProgram = program

	model := resizeModel(readyModel(t), 100, 24)
	drainLogNotifications()

	// An async command logs while the UI is idle: no Update is running.
	enqueueLogNotification(log.Entry{Time: time.Now(), Level: log.LevelWarn, Message: "async warning"})

	// The enqueue must wake the program loop without any user input.
	deadline := time.After(2 * time.Second)
	for len(program.sent()) == 0 {
		select {
		case <-deadline:
			t.Fatal("enqueued log did not wake the program")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, ok := program.sent()[0].(logWakeupMsg); !ok {
		t.Fatalf("wakeup message = %T, want logWakeupMsg", program.sent()[0])
	}

	// The woken loop drains the queue into a popup.
	updated, _ := model.Update(logWakeupMsg{})
	model = updated.(Model)
	popup := model.notifications.popup
	if popup == nil {
		t.Fatal("async log did not surface as a popup")
	}
	if !strings.Contains(popup.title, "Warning") || popup.level != storedLogLevel(log.LevelWarn) {
		t.Fatalf("popup = %#v, want the Warning popup", popup)
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

func TestNotificationBorderColor_matchesLevel(t *testing.T) {
	for _, tc := range []struct {
		level log.Level
		want  string
	}{
		{log.LevelDebug, colorMuted},
		{log.LevelInfo, colorPrimary},
		{log.LevelWarn, colorWarn},
		{log.LevelError, colorDanger},
	} {
		if got := notificationBorderColor(storedLogLevel(tc.level)); got != tc.want {
			t.Fatalf("border color for %s = %q, want %q", tc.level, got, tc.want)
		}
	}
	if got := notificationBorderColor(notificationLevelNone); got != colorBorder {
		t.Fatalf("border color for status = %q, want %q", got, colorBorder)
	}
}

func TestNotificationPopup_borderMatchesLevelColor(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notifyLog(log.Entry{Time: time.Now(), Level: log.LevelWarn, Message: "slow query detected"})

	bounds, ok := model.notificationPopupBounds()
	if !ok {
		t.Fatal("no popup bounds")
	}
	line := strings.Split(model.View().Content, "\n")[bounds.Min.Y]
	if got, want := lastRGB(line), rgbOf(colorWarn); got != want {
		t.Fatalf("popup top border color = %s, want %s (line %q)", got, want, line)
	}
}

func TestNotificationPopup_statusBorderStaysNeutral(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.notify("row updated")

	bounds, ok := model.notificationPopupBounds()
	if !ok {
		t.Fatal("no popup bounds")
	}
	line := strings.Split(model.View().Content, "\n")[bounds.Min.Y]
	if got, want := lastRGB(line), rgbOf(colorBorder); got != want {
		t.Fatalf("status popup top border color = %s, want %s (line %q)", got, want, line)
	}
}
