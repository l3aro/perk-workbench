package workbench

import (
	"context"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/workbench/notification"
)

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
	entry := notification.StatusEntry("persisted")
	saveNotificationHistory(t, path, "conn-a", entry)

	model := New("", context.Background(), testOpen, false)
	// No connection is open yet, so persisted entries must not surface.
	if got := model.notifications.component.Entries; len(got) != 0 {
		t.Fatalf("notifications before a connection = %#v, want none", got)
	}
	// The scoped load (what updateOpen runs after recordConnection).
	model.connectionID = "conn-a"
	model.notifications.component.SetEntries(loadNotificationHistory(t, model.notifications.path, model.connectionID))
	if got := model.notifications.component.Entries; len(got) != 1 || got[0].Description != entry.Description {
		t.Fatalf("loaded notifications = %#v, want %#v", got, []notification.Entry{entry})
	}
}
