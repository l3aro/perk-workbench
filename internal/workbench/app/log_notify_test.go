package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// loadNotificationHistory opens the shared data.db through the store and
// returns the retained entries for one connection scope.
func loadNotificationHistory(t *testing.T, path, connectionID string) []notification.Entry {
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
	return entries
}

// saveNotificationHistory persists one entry through the store.
func saveNotificationHistory(t *testing.T, path, connectionID string, entry notification.Entry) {
	t.Helper()
	store, err := notification.Open(path, notificationRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Append(connectionID, entry, 0); err != nil {
		t.Fatal(err)
	}
}

func TestLogNotification_logCallsInsideUpdateDrainToPopup(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	notification.DrainLogEntries() // clear anything queued by setup

	// The log package notifier enqueues exactly this during an update.
	notification.EnqueueLogEntry(log.Entry{Time: time.Now(), Level: log.LevelError, Message: "open database: boom"})

	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	popup := model.notifications.component.Popup
	if popup == nil {
		t.Fatal("queued log entry did not surface as a popup")
	}
	if popup.Level != notification.StoredLogLevel(log.LevelError) {
		t.Fatalf("popup level = %d, want %d", popup.Level, notification.StoredLogLevel(log.LevelError))
	}
	if !strings.Contains(popup.Title, "Error") {
		t.Fatalf("popup title = %q, want the Error title", popup.Title)
	}
	model = driveCommand(model, cmd)
}

// openWithScratch drives the connection form's open flow to completion and
// returns the resulting model.
func openWithScratch(t *testing.T, model Model) Model {
	t.Helper()
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Scratch", ":memory:"
	updated, command := model.openConnection()
	model = updated.(Model)
	if command == nil {
		t.Fatal("open connection command = nil")
	}
	for _, message := range executeCommandAll(command) {
		updated, _ := model.Update(message)
		model = updated.(Model)
	}
	return model
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
	if popup := model.notifications.component.Popup; popup != nil {
		t.Fatalf("default level surfaced a popup: %#v", popup)
	}
	if _, err := os.Stat(filepath.Join(dir, "perk-workbench", "event.log")); err == nil {
		t.Fatal("event.log created for a debug-only open at the default level")
	}

	// Debug level: the same open surfaces a Debug-typed popup and writes
	// the DEBUG line.
	SetAppConfig(Config{LogLevel: "debug"})
	model = openWithScratch(t, New("", context.Background(), testOpen, false))
	popup := model.notifications.component.Popup
	if popup == nil {
		t.Fatal("debug level did not surface the ready popup")
	}
	if popup.Level != notification.StoredLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.Level, notification.StoredLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.Title, "Debug") || !strings.Contains(popup.Description, "ready: Scratch") {
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
	model.connection.component.Profiles = []profile.Profile{{Name: "Scratch", Target: ":memory:"}}
	_ = model.connection.component.Recent.SetItems(connection.RecentListItems(model.connection.component.Profiles))
	model.connection.component.Form.SetFocus(connectionFocusRecent)
	notification.DrainLogEntries()
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.Status != "editing Scratch" {
		t.Fatalf("status = %q, want the editing status kept", model.Status)
	}
	if popup := model.notifications.component.Popup; popup != nil {
		t.Fatalf("default level surfaced a popup: %#v", popup)
	}

	// Debug level: the same edit surfaces a Debug-typed popup and writes
	// the DEBUG line.
	SetAppConfig(Config{LogLevel: "debug"})
	model = New("", context.Background(), testOpen, false)
	model.connection.component.Profiles = []profile.Profile{{Name: "Scratch", Target: ":memory:"}}
	_ = model.connection.component.Recent.SetItems(connection.RecentListItems(model.connection.component.Profiles))
	model.connection.component.Form.SetFocus(connectionFocusRecent)
	notification.DrainLogEntries()
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	popup := model.notifications.component.Popup
	if popup == nil {
		t.Fatal("debug level did not surface the editing popup")
	}
	if popup.Level != notification.StoredLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.Level, notification.StoredLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.Title, "Debug") || !strings.Contains(popup.Description, "editing Scratch") {
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
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Scratch", ":memory:"
	updated, _ := model.Update(connectionActionMsg{action: connectionActionConnect})
	model = updated.(Model)
	if model.Status != "opening Scratch" {
		t.Fatalf("status = %q, want the opening status kept", model.Status)
	}
	if popup := model.notifications.component.Popup; popup != nil {
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
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Scratch", ":memory:"
	updated, _ = model.Update(connectionActionMsg{action: connectionActionConnect})
	model = updated.(Model)
	popup := model.notifications.component.Popup
	if popup == nil {
		t.Fatal("debug level did not surface the opening popup")
	}
	if popup.Level != notification.StoredLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.Level, notification.StoredLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.Title, "Debug") || !strings.Contains(popup.Description, "opening Scratch") {
		t.Fatalf("popup = %#v, want the Debug opening popup", popup)
	}
	// The returned command is a batch with the dismiss tick; drive the
	// open target directly so the open and its asynchronous history writes
	// complete deterministically.
	updated, command := model.Update(model.openTarget(":memory:")())
	model = updated.(Model)
	model = driveCommand(model, command)
	history := loadNotificationHistory(t, filepath.Join(dir, "perk-workbench", "data.db"), model.connectionID)
	if len(history) == 0 {
		t.Fatal("history has no retained entries after the open")
	}
	for _, entry := range history {
		if strings.Contains(entry.Description, "opening") {
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
	if popup := model.notifications.component.Popup; popup != nil {
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
	popup := model.notifications.component.Popup
	if popup == nil {
		t.Fatal("debug level did not surface the opening popup")
	}
	if popup.Level != notification.StoredLogLevel(log.LevelDebug) {
		t.Fatalf("popup level = %d, want %d", popup.Level, notification.StoredLogLevel(log.LevelDebug))
	}
	if !strings.Contains(popup.Title, "Debug") || !strings.Contains(popup.Description, "opening database") {
		t.Fatalf("popup = %#v, want the Debug opening popup", popup)
	}
	// The returned command is a batch with the dismiss tick; drive the
	// open target directly so the open and its asynchronous history writes
	// complete deterministically.
	updated, command := model.Update(model.openTarget(":memory:")())
	model = updated.(Model)
	model = driveCommand(model, command)
	history := loadNotificationHistory(t, filepath.Join(dir, "perk-workbench", "data.db"), model.connectionID)
	if len(history) == 0 {
		t.Fatal("history has no retained entries after the open")
	}
	for _, entry := range history {
		if strings.Contains(entry.Description, "opening") {
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
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Second", ":memory:"

	// The Debug opening popup shows while the scope is still the live
	// connection's.
	updated, _ := model.Update(connectionActionMsg{action: connectionActionConnect})
	model = updated.(Model)
	popup := model.notifications.component.Popup
	if popup == nil || !strings.Contains(popup.Title, "Debug") || !strings.Contains(popup.Description, "opening Second") {
		t.Fatalf("popup = %#v, want the Debug opening Second popup", popup)
	}
	if got := loadNotificationHistory(t, historyPath, "live-scope"); len(got) != 0 {
		t.Fatalf("opening entry bound to the live scope: %#v", got)
	}

	// Completing the open assigns the new scope and persists only its
	// ready entry. Drive every command returned by the update before
	// inspecting either scope's on-disk history.
	updated, command := model.Update(model.openTarget(":memory:")())
	model = updated.(Model)
	model = driveCommand(model, command)
	if model.connectionID == "live-scope" {
		t.Fatal("open did not assign a new profile scope")
	}
	for _, entries := range [][]notification.Entry{
		loadNotificationHistory(t, historyPath, "live-scope"),
		loadNotificationHistory(t, historyPath, model.connectionID),
	} {
		for _, entry := range entries {
			if strings.Contains(entry.Description, "opening") {
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
	notification.DrainLogEntries() // clear anything queued by setup

	log.Error("open database", errors.New("boom"))

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	popup := model.notifications.component.Popup
	if popup == nil {
		t.Fatal("log.Error did not surface as a popup")
	}
	if !strings.Contains(popup.Title, "Error") || popup.Level != notification.StoredLogLevel(log.LevelError) {
		t.Fatalf("popup = %#v, want the Error popup", popup)
	}
	if !strings.Contains(popup.Description, "open database: boom") {
		t.Fatalf("popup description = %q, want the log message", popup.Description)
	}
	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ERROR: open database: boom") {
		t.Fatalf("event.log = %q, want the ERROR line", b)
	}
}
