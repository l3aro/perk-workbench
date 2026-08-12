package app

import (
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

const (
	// defaultNotificationRetentionDays is how long notification history is
	// kept when config.json leaves notification_retention_days unset.
	defaultNotificationRetentionDays = 30
	// defaultNotificationTimeoutSeconds is how long a popup stays visible
	// when config.json leaves notification_timeout_seconds unset.
	defaultNotificationTimeoutSeconds = 10
	// maxNotificationTimeoutSeconds bounds the popup lifetime so a
	// misconfiguration can never pin a popup to the screen for days.
	maxNotificationTimeoutSeconds = 86_400
)

// AttachLogProgram wires the running program into the log notification
// pipeline so entries logged by async commands surface as popups even when
// the UI is idle. Call once with the program returned by tea.NewProgram,
// before program.Run. Attaching nil detaches.
func AttachLogProgram(program *tea.Program) {
	notification.AttachLogProgram(program)
}

// notificationPopupDuration resolves the configured popup lifetime.
func notificationPopupDuration() time.Duration {
	seconds := defaultNotificationTimeoutSeconds
	if appConfig.NotificationTimeoutSeconds > 0 {
		seconds = appConfig.NotificationTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

// notificationRetentionDays resolves the configured history window.
func notificationRetentionDays() int {
	if appConfig.NotificationRetentionDays > 0 {
		return appConfig.NotificationRetentionDays
	}
	return defaultNotificationRetentionDays
}

// notificationPath returns the shared app-state SQLite file.
func notificationPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", "data.db"), nil
}

// notificationStore returns the model's persistent notification store,
// opened lazily on first use and reused for every save.
func (m *Model) notificationStore() *notification.Store {
	if m.notifications.store == nil && m.notifications.path != "" {
		if store, err := notification.Open(m.notifications.path, notificationRetentionDays()); err == nil {
			m.notifications.store = store
		}
	}
	return m.notifications.store
}

// setStatus records a status transition, bumping the workbench-side revision
// so repeated writes of the same text still surface as notification events.
func (m *Model) setStatus(status string) {
	m.Status = status
	m.notifications.statusRevision++
}

// notify captures a status transition as the visible popup and, when a
// connection profile is active, persists it to history.
func (m *Model) notify(message string) tea.Cmd {
	return m.show(notification.StatusEntry(uikit.SafeText(message)), true)
}

// notifyLog captures a logged event as the visible popup and, when a
// connection profile is active, persists it to history. The popup carries
// the level's icon, title, and severity color.
func (m *Model) notifyLog(entry log.Entry) tea.Cmd {
	return m.show(notification.LogEntry(entry), true)
}

// notifyLogTransient captures a logged event as the visible popup without
// persisting it to history. Transitions that log before a connection
// profile exists (the opening notice) use it, so the entry is never bound
// to the wrong scope.
func (m *Model) notifyLogTransient(entry log.Entry) tea.Cmd {
	return m.show(notification.LogEntry(entry), false)
}

// show surfaces one entry through the notification component: it persists
// when a connection profile is active and persist is set, then makes the
// entry the visible popup. The store is fetched lazily only when the entry
// could be scoped, so an unscoped status never opens the history database.
func (m *Model) show(entry notification.Entry, persist bool) tea.Cmd {
	var store *notification.Store
	if persist && m.connectionID != "" {
		store = m.notificationStore()
	}
	updated, cmd := m.notifications.component.Show(entry, persist, m.connectionID, store, notificationPopupDuration())
	m.notifications.component = updated
	return cmd
}

// notificationLayout builds the layout snapshot root hands to the
// notification component; the overlays only need the full screen size.
func notificationLayout(m Model) uikit.Layout {
	return uikit.Layout{
		Width:         m.layout.width,
		Height:        m.layout.height,
		ViewportWidth: m.layout.tableViewportWidth,
		PaneHeight:    m.layout.queryLogHeight,
	}
}

// applyNotificationEvent applies one notification event: the only side
// effect the component cannot perform itself is the clipboard write.
func (m Model) applyNotificationEvent(event uikit.Event, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch e := event.(type) {
	case nil:
		return m, cmd
	case uikit.ClipboardRequested:
		if cmd == nil {
			return m, copyQueryLogStatement(e.Text)
		}
		return m, tea.Batch(cmd, copyQueryLogStatement(e.Text))
	}
	return m, cmd
}
