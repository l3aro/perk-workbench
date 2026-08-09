package workbench

import (
	"database/sql"
	"errors"
	"fmt"
	"image"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
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
	// notificationTitle is the fixed title of every captured status.
	notificationTitle = "Notification"
)

// notificationEntry is one captured status message. id is the SQLite row ID
// when the entry was persisted for a connection scope, 0 otherwise.
type notificationEntry struct {
	id          int64
	createdAt   time.Time
	title       string
	description string
}

// notificationDismissMsg closes the visible popup when its generation still
// matches the model's current one.
type notificationDismissMsg struct {
	generation uint64
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

// notificationDB returns the model's persistent notification database,
// opened lazily on first use and reused for every save.
func (m *Model) notificationDB() *sql.DB {
	if m.notificationDatabase == nil && m.notificationPath != "" {
		if db, err := openNotificationStore(m.notificationPath); err == nil {
			m.notificationDatabase = db
		}
	}
	return m.notificationDatabase
}

// loadNotifications returns the retained entries for one connection scope,
// newest first. An empty scope never reads unscoped rows.
func loadNotifications(path, connectionID string) []notificationEntry {
	if connectionID == "" {
		return nil
	}
	db, err := openNotificationStore(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	return loadNotificationsDB(db, connectionID)
}

func loadNotificationsDB(db *sql.DB, connectionID string) []notificationEntry {
	if connectionID == "" {
		return nil
	}
	if pruneNotifications(db, time.Now(), connectionID) != nil {
		return nil
	}
	rows, err := db.Query(`SELECT id, created_at, title, description FROM notifications WHERE connection_id = ? ORDER BY created_at DESC, id DESC`, connectionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []notificationEntry
	for rows.Next() {
		var createdAt int64
		var entry notificationEntry
		if rows.Scan(&entry.id, &createdAt, &entry.title, &entry.description) != nil {
			continue
		}
		entry.createdAt = time.Unix(0, createdAt)
		entries = append(entries, entry)
	}
	return entries
}

// saveNotification persists one entry for a connection scope and returns the
// inserted row ID.
func saveNotification(path, connectionID string, entry notificationEntry) (int64, error) {
	db, err := openNotificationStore(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return saveNotificationDB(db, connectionID, entry)
}

// saveNotificationDB persists one entry through an already-open database:
// prune by retention, then insert.
func saveNotificationDB(db *sql.DB, connectionID string, entry notificationEntry) (int64, error) {
	// Never persist notifications without a profile scope; the caller keeps
	// the entry in memory only.
	if connectionID == "" {
		return 0, errors.New("notifications require a connection scope")
	}
	if err := pruneNotifications(db, time.Now(), connectionID); err != nil {
		return 0, err
	}
	result, err := db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description) VALUES (?, ?, ?, ?)`,
		connectionID, entry.createdAt.UnixNano(), entry.title, entry.description)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func openNotificationStore(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)"}).String())
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS notifications (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS notifications_connection_created ON notifications (connection_id, created_at DESC, id DESC)`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// pruneNotifications deletes expired entries for one connection scope.
func pruneNotifications(db *sql.DB, now time.Time, connectionID string) error {
	_, err := db.Exec(`DELETE FROM notifications WHERE created_at < ? AND connection_id = ?`,
		now.AddDate(0, 0, -notificationRetentionDays()).UnixNano(), connectionID)
	return err
}

// setStatus records a status transition, bumping the workbench-side revision
// so repeated writes of the same text still surface as notification events.
func (m *Model) setStatus(status string) {
	m.Status = status
	m.statusRevision++
}

// notificationDismissTick builds the command that closes the popup after
// the configured duration. It is a variable so tests can replace it with an
// immediate dismiss and avoid wall-clock waits.
var notificationDismissTick = func(generation uint64) tea.Cmd {
	return tea.Tick(notificationPopupDuration(), func(time.Time) tea.Msg {
		return notificationDismissMsg{generation: generation}
	})
}

// notify captures a status transition as the visible popup and, when a
// connection profile is active, persists it to history.
func (m *Model) notify(message string) tea.Cmd {
	entry := notificationEntry{
		createdAt:   time.Now(),
		title:       notificationTitle,
		description: safeText(message),
	}
	if m.connectionID != "" {
		if db := m.notificationDB(); db != nil {
			if id, err := saveNotificationDB(db, m.connectionID, entry); err == nil {
				entry.id = id
			}
		}
	}
	m.notificationEntries = append([]notificationEntry{entry}, m.notificationEntries...)
	m.notificationPopup = &entry
	m.notificationGeneration++
	generation := m.notificationGeneration
	return notificationDismissTick(generation)
}

// notificationPopupBounds returns the screen rectangle of the visible popup.
// The popup is a bordered card anchored to the top-right corner.
func (m Model) notificationPopupBounds() (image.Rectangle, bool) {
	if m.notificationPopup == nil {
		return image.Rectangle{}, false
	}
	width := min(50, m.width-4)
	if width < 4 || m.height < 4 {
		return image.Rectangle{}, false
	}
	lines := strings.Split(ansi.Wordwrap(m.notificationPopup.description, width-4, "\n"), "\n")
	cardW := width + 2
	cardH := len(lines) + 2
	if cardH > m.height-4 {
		cardH = m.height - 4
	}
	x := m.width - cardW - 1
	y := 1
	return image.Rect(x, y, x+cardW, y+cardH), true
}

// notificationHistoryPane identifies the active column of the split modal.
type notificationHistoryPane uint8

const (
	notificationHistoryListPane notificationHistoryPane = iota
	notificationHistoryDetailPane
)

// notificationListItem adapts a notificationEntry for the bubbles list.
type notificationListItem struct {
	entry notificationEntry
}

func (i notificationListItem) FilterValue() string {
	return i.entry.title + " " + i.entry.description
}

func (i notificationListItem) Title() string {
	return i.entry.createdAt.Format("2006-01-02 15:04:05")
}

func (i notificationListItem) Description() string {
	return i.entry.title
}

// notificationItemDelegate renders one notification list row: timestamp and
// title, primary when selected.
type notificationItemDelegate struct{}

func (notificationItemDelegate) Height() int                         { return 1 }
func (notificationItemDelegate) Spacing() int                        { return 0 }
func (notificationItemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (notificationItemDelegate) Render(writer io.Writer, model list.Model, index int, item list.Item) {
	entry, ok := item.(notificationListItem)
	if !ok {
		return
	}
	label := entry.Title() + "  " + entry.Description()
	if width := model.Width(); width > 0 {
		label = ansi.Truncate(label, width, "…")
	}
	color := colorMuted
	if index == model.Index() {
		color = colorPrimary
	}
	fmt.Fprint(writer, lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(label))
}

// notificationHistory is the split modal: a narrow filterable list on the
// left and a full detail viewport on the right.
type notificationHistory struct {
	entries       []notificationEntry
	filtered      []notificationEntry
	list          list.Model
	filter        textinput.Model
	detail        viewport.Model
	pane          notificationHistoryPane
	filterFocused bool
}

// newNotificationHistory builds the split modal. selectedID selects the
// entry with that SQLite row ID, falling back to the newest entry when 0 or
// absent.
func newNotificationHistory(entries []notificationEntry, selectedID int64, width, height int) *notificationHistory {
	h := &notificationHistory{
		entries:  entries,
		filtered: append([]notificationEntry{}, entries...),
		list:     newSchemaList(),
		filter:   newFilterInput(),
		pane:     notificationHistoryListPane,
	}
	h.list.SetDelegate(notificationItemDelegate{})
	h.filter.Placeholder = "filter notifications"
	h.setItems()
	selected := 0
	for index, entry := range entries {
		if entry.id == selectedID {
			selected = index
			break
		}
	}
	if len(h.filtered) > 0 && selected >= len(h.filtered) {
		selected = len(h.filtered) - 1
	}
	if len(h.filtered) > 0 {
		h.list.Select(selected)
		h.syncDetail()
	}
	h.resize(width, height)
	return h
}

func (h *notificationHistory) resize(width, height int) {
	if width < 40 || height < 8 {
		// Keep a usable viewport for rendering while the small-terminal
		// guard message is shown.
		h.detail = viewport.New(viewport.WithWidth(20), viewport.WithHeight(2))
		h.detail.SoftWrap = true
		h.detail.MouseWheelEnabled = true
		return
	}
	leftWidth := clamp(width/3, 24, 40)
	rightWidth := width - leftWidth - 3
	h.detail = viewport.New(viewport.WithWidth(rightWidth-4), viewport.WithHeight(height-6))
	h.detail.SoftWrap = true
	h.detail.MouseWheelEnabled = true
	h.list.SetSize(leftWidth-2, height-7)
	h.filter.SetWidth(leftWidth - 6)
	h.syncDetail()
}

// setItems refreshes the list rows from the filtered slice.
func (h *notificationHistory) setItems() {
	items := make([]list.Item, len(h.filtered))
	for index, entry := range h.filtered {
		items[index] = notificationListItem{entry: entry}
	}
	_ = h.list.SetItems(items)
}

// applyFilter re-filters by the filter input (case-insensitive substring
// match across title and description) and clamps the selection.
func (h *notificationHistory) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(h.filter.Value()))
	h.filtered = h.filtered[:0]
	for _, entry := range h.entries {
		if strings.Contains(strings.ToLower(entry.title+" "+entry.description), query) {
			h.filtered = append(h.filtered, entry)
		}
	}
	h.setItems()
	if len(h.filtered) > 0 && h.list.Index() >= len(h.filtered) {
		h.list.Select(len(h.filtered) - 1)
	}
	h.syncDetail()
}

// selected returns the filtered entry under the list cursor.
func (h *notificationHistory) selected() (notificationEntry, bool) {
	index := h.list.Index()
	if index < 0 || index >= len(h.filtered) {
		return notificationEntry{}, false
	}
	return h.filtered[index], true
}

// syncDetail renders the selected entry into the right detail viewport.
func (h *notificationHistory) syncDetail() {
	entry, ok := h.selected()
	if !ok {
		h.detail.SetContent("")
	} else {
		var b strings.Builder
		b.WriteString("Title:\n  ")
		b.WriteString(entry.title)
		b.WriteString("\n\nDescription:\n  ")
		b.WriteString(chrome.DetailValue(entry.description))
		b.WriteString("\n\nTime:\n  ")
		b.WriteString(entry.createdAt.Format("2006-01-02 15:04:05"))
		h.detail.SetContent(b.String())
	}
	// SetContent only stores lines; sizing and scroll range are computed by
	// setInitialValues, which runs on Init. Initialize after every content
	// change so programmatic scrolls (j/k) work without a tea lifecycle.
	h.detail.Init()
}

// handleKey routes one key press through the modal's pane-specific behavior.
func (h *notificationHistory) handleKey(msg tea.KeyPressMsg) bool {
	if h.filterFocused {
		switch msg.Key().Code {
		case tea.KeyEscape:
			h.filterFocused = false
			h.filter.Blur()
			return true
		}
		h.filter, _ = h.filter.Update(msg)
		h.applyFilter()
		return true
	}
	switch msg.Key().Code {
	case '/':
		h.filterFocused = true
		h.filter.Focus()
		return true
	case tea.KeyEscape:
		return false // caller closes the modal
	case tea.KeyLeft, 'h':
		h.pane = notificationHistoryListPane
		return true
	case tea.KeyRight, 'l':
		h.pane = notificationHistoryDetailPane
		return true
	case tea.KeyUp, 'k':
		if h.pane == notificationHistoryDetailPane {
			h.detail.ScrollUp(1)
		} else {
			h.list.CursorUp()
		}
		h.syncDetail()
		return true
	case tea.KeyDown, 'j':
		if h.pane == notificationHistoryDetailPane {
			h.detail.ScrollDown(1)
		} else {
			h.list.CursorDown()
		}
		h.syncDetail()
		return true
	}
	// Swallow everything else so no key reaches the panes underneath.
	return true
}
