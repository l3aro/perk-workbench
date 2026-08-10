package workbench

import (
	"database/sql"
	"errors"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/log"
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
	// notificationLevelNone marks an entry that is not a logged event (a
	// status message). Positive level values are log.Level + 1, matching
	// the persisted column so older rows keep the neutral appearance.
	notificationLevelNone = 0
)

// notificationEntry is one captured status message. id is the SQLite row ID
// when the entry was persisted for a connection scope, 0 otherwise. level is
// notificationLevelNone for status messages, or log.Level + 1 for entries
// captured from the event log.
type notificationEntry struct {
	id          int64
	createdAt   time.Time
	title       string
	description string
	level       int
}

// storedLogLevel converts a log level to the persisted notification level.
func storedLogLevel(level log.Level) int { return int(level) + 1 }

// logLevelOf resolves the log level of a stored notification level, if any.
func logLevelOf(level int) (log.Level, bool) {
	if level <= notificationLevelNone || level > int(log.LevelError)+1 {
		return log.LevelInfo, false
	}
	return log.Level(level - 1), true
}

// notificationLevelColor returns the theme color for a stored level: the
// level's severity color for log entries, the neutral secondary otherwise.
func notificationLevelColor(level int) string {
	switch l, ok := logLevelOf(level); l {
	case log.LevelDebug:
		return colorMuted
	case log.LevelWarn:
		return colorWarn
	case log.LevelError:
		return colorDanger
	case log.LevelInfo:
		if ok {
			return colorPrimary
		}
		return colorSecondary
	default:
		return colorSecondary
	}
}

// notificationBorderColor returns the popup border color: the level's
// severity color for logged events, the neutral border otherwise.
func notificationBorderColor(level int) string {
	if _, ok := logLevelOf(level); ok {
		return notificationLevelColor(level)
	}
	return colorBorder
}

// logLevelIcon returns the icon glyph for a log level: Nerd Font icons when
// enabled (config.json "nerd_font"), geometric symbols otherwise.
func logLevelIcon(level log.Level) string {
	if appConfig.NerdFont == nil || *appConfig.NerdFont {
		switch level {
		case log.LevelDebug:
			return "\uf188" // nf-fa-bug
		case log.LevelWarn:
			return "\uf071" // nf-fa-exclamation-triangle
		case log.LevelError:
			return "\uf057" // nf-fa-times-circle
		default:
			return "\uf05a" // nf-fa-info-circle
		}
	}
	switch level {
	case log.LevelDebug:
		return "◌"
	case log.LevelWarn:
		return "⚠"
	case log.LevelError:
		return "✖"
	default:
		return "ℹ"
	}
}

// notificationIconIndent returns the horizontal space reserved on the
// title/body rows for the level symbol plus the gap after it, or 0 for
// plain status notifications (no symbol).
func notificationIconIndent(level int) int {
	if l, ok := logLevelOf(level); ok {
		return max(ansi.StringWidth(logLevelIcon(l)), 1) + 1
	}
	return 0
}

// logWakeupMsg wakes the idle program loop when a log entry arrives from an
// async command; the outer Update wrapper drains the queue into a popup.
type logWakeupMsg struct{}

// logProgramSender is the wakeup sink of the running program, an interface
// so tests can inject a recording stub.
type logProgramSender interface {
	Send(tea.Msg)
}

// logProgram is the attached program, if any. It receives a logWakeupMsg
// whenever an entry is enqueued outside an update handler so the idle loop
// wakes and drains the queue. Guarded by logNotificationMu.
var logProgram logProgramSender

// AttachLogProgram wires the running program into the log notification
// pipeline so entries logged by async commands surface as popups even when
// the UI is idle. Call once with the program returned by tea.NewProgram,
// before program.Run. Attaching nil detaches.
func AttachLogProgram(program *tea.Program) {
	logNotificationMu.Lock()
	logProgram = program
	logNotificationMu.Unlock()
}

// Logged events enqueued by the log package notifier between Updates.
// Draining happens in Model.Update so every log call made inside an update
// handler surfaces as a popup in the same message cycle.
var (
	logNotificationMu    sync.Mutex
	logNotificationQueue []log.Entry
)

// enqueueLogNotification is the log package notifier: it queues entries for
// the next Update drain and wakes the attached program so an entry from an
// async command surfaces even when the UI is idle. Safe for concurrent
// callers (async Bubble Tea commands). The wakeup is sent on its own
// goroutine: the program's message channel is unbuffered, so sending from
// inside an Update handler (where every current log call happens) would
// deadlock on the loop waiting for that same Update to return.
func enqueueLogNotification(entry log.Entry) {
	logNotificationMu.Lock()
	logNotificationQueue = append(logNotificationQueue, entry)
	program := logProgram
	logNotificationMu.Unlock()
	if program != nil {
		go program.Send(logWakeupMsg{})
	}
}

// drainLogNotifications returns and clears the queued log entries.
func drainLogNotifications() []log.Entry {
	logNotificationMu.Lock()
	defer logNotificationMu.Unlock()
	entries := logNotificationQueue
	logNotificationQueue = nil
	return entries
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
	rows, err := db.Query(`SELECT id, created_at, title, description, level FROM notifications WHERE connection_id = ? ORDER BY created_at DESC, id DESC`, connectionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []notificationEntry
	for rows.Next() {
		var createdAt int64
		var entry notificationEntry
		if rows.Scan(&entry.id, &createdAt, &entry.title, &entry.description, &entry.level) != nil {
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
	result, err := db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description, level) VALUES (?, ?, ?, ?, ?)`,
		connectionID, entry.createdAt.UnixNano(), entry.title, entry.description, entry.level)
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
		description TEXT NOT NULL,
		level INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, err
	}
	// Older databases predate the level column; add it so history keeps the
	// severity of logged events. Pre-existing rows stay neutral (0).
	rows, err := db.Query(`PRAGMA table_info(notifications)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	hasLevel := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil && name == "level" {
			hasLevel = true
		}
	}
	rows.Close()
	if !hasLevel {
		if _, err = db.Exec(`ALTER TABLE notifications ADD COLUMN level INTEGER NOT NULL DEFAULT 0`); err != nil {
			db.Close()
			return nil, err
		}
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
	return m.showNotification(notificationEntry{
		createdAt:   time.Now(),
		title:       notificationTitle,
		description: safeText(message),
	})
}

// notifyLog captures a logged event as the visible popup and, when a
// connection profile is active, persists it to history. The popup carries
// the level's icon, title, and severity color.
func (m *Model) notifyLog(entry log.Entry) tea.Cmd {
	return m.showNotification(notificationEntry{
		createdAt:   entry.Time,
		title:       logLevelIcon(entry.Level) + " " + entry.Level.Title(),
		description: safeText(entry.Message),
		level:       storedLogLevel(entry.Level),
	})
}

// showNotification surfaces one entry as the visible popup and, when a
// connection profile is active, persists it to history.
func (m *Model) showNotification(entry notificationEntry) tea.Cmd {
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
	lines := strings.Split(ansi.Wordwrap(m.notificationPopup.description, max(width-4-notificationIconIndent(m.notificationPopup.level), 1), "\n"), "\n")
	cardW := width + 2
	cardH := len(lines) + 3 // title row + description + top/bottom border
	if cardH > m.height-4 {
		cardH = m.height - 4
	}
	x := m.width - cardW - 1
	y := 1
	return image.Rect(x, y, x+cardW, y+cardH), true
}

// notificationColumnTitles are the modal table's columns in display
// order; entryRow and the sort column index both rely on this order.
var notificationColumnTitles = []string{"Time", "Level", "Title", "Description"}

// notificationHistory is a full-width modal table of retained
// notifications. It mirrors the Browse pane's table: cell travel with
// h/j/k/l and the arrow keys, y copies the selected cell, / filters, s
// (or a header click) sorts, n/p page, and v opens the cell in the
// viewer overlay.
type notificationHistory struct {
	entries       []notificationEntry // all retained entries, newest first
	filtered      []notificationEntry // entries after filter + sort
	page          int                 // current page
	pageSize      int                 // rows per page, derived from the modal height
	table         table.Model         // rows of the current page
	pageEntries   []notificationEntry // entries behind the table rows
	width, height int                 // modal size, for click hit-testing
	selectedCol   int                 // cell column under the cursor
	offset        int                 // horizontal scroll offset
	filter        textinput.Model
	filterFocused bool
	sortCol       int // -1 = default (newest first) order
	sortDesc      bool
	viewer        *cellViewer // view-cell overlay, nil when closed
}

// newNotificationHistory builds the modal. selectedID selects the entry
// with that SQLite row ID, falling back to the newest entry when 0 or
// absent.
func newNotificationHistory(entries []notificationEntry, selectedID int64, width, height int) *notificationHistory {
	h := &notificationHistory{
		entries:  append([]notificationEntry{}, entries...),
		filtered: append([]notificationEntry{}, entries...),
		filter:   newFilterInput(),
		sortCol:  -1,
	}
	h.filter.Placeholder = "filter notifications"
	h.table = table.New(table.WithStyles(table.Styles{
		Header:   headerStyle,
		Cell:     lipgloss.NewStyle().Padding(0, spaceCompact),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary)).Background(lipgloss.Color(colorStripe)),
	}))
	h.resize(width, height)
	for index, entry := range h.filtered {
		if entry.id == selectedID {
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
func (h *notificationHistory) resize(width, height int) {
	h.width, h.height = width, height
	h.pageSize = max(height-12, 1)
	h.page = clamp(h.page, 0, max(h.pageCount()-1, 0))
	// The input's View is one cell wider than its Width, so leave one cell
	// of slack against the filter box's content width.
	h.filter.SetWidth(max(h.viewportWidth()-7, 1))
	if h.viewer != nil {
		h.viewer.resize(max(width-8, 1), max(height-10, 1))
	}
	h.syncPage()
}

// pageCount returns the number of pages the filtered entries span.
func (h *notificationHistory) pageCount() int {
	return (len(h.filtered) + h.pageSize - 1) / h.pageSize
}

// viewportWidth returns the modal's inner content width, the width the
// table and the pager row are laid out to.
func (h *notificationHistory) viewportWidth() int {
	return max(h.width-6, 1)
}

// syncPage rebuilds the current page rows and column widths (sort markers
// included) from filtered, preserving the selected cell.
func (h *notificationHistory) syncPage() {
	row, col := h.table.Cursor(), h.selectedCol
	h.page = clamp(h.page, 0, max(h.pageCount()-1, 0))
	start := h.page * h.pageSize
	end := min(start+h.pageSize, len(h.filtered))
	h.pageEntries = append([]notificationEntry{}, h.filtered[start:end]...)
	rows := make([]table.Row, len(h.pageEntries))
	for index, entry := range h.pageEntries {
		rows[index] = h.entryRow(entry)
	}
	h.table.SetCursor(clamp(row, 0, max(len(rows)-1, 0)))
	h.selectedCol = clamp(col, 0, len(notificationColumnTitles)-1)
	titles := append([]string{}, notificationColumnTitles...)
	if h.sortCol >= 0 && h.sortCol < len(titles) {
		if h.sortDesc {
			titles[h.sortCol] += " ▼"
		} else {
			titles[h.sortCol] += " ▲"
		}
	}
	// Columns first: bubbles renders rows against the current columns, so
	// SetRows after a column change would index out of range otherwise.
	h.table.SetColumns(tableColumns(titles, rows))
	h.table.SetRows(rows)
	h.table.SetCursor(clamp(row, 0, max(len(rows)-1, 0)))
	h.table.SetWidth(max(h.viewportWidth(), tableContentWidth(h.table.Columns())))
	h.table.SetHeight(h.pageSize)
}

// entryRow renders one entry as a table row: time, level, title,
// description. Copy and view use the raw entry, not these display cells.
func (h *notificationHistory) entryRow(entry notificationEntry) table.Row {
	return table.Row{
		entry.createdAt.Format("2006-01-02 15:04:05"),
		h.levelText(entry),
		entry.title,
		entry.description,
	}
}

// levelText returns the display text of an entry's level column: the
// severity title for logged events, empty for plain status messages.
func (h *notificationHistory) levelText(entry notificationEntry) string {
	if level, ok := logLevelOf(entry.level); ok {
		return level.Title()
	}
	return ""
}

// applyFilter re-filters by the filter input (case-insensitive substring
// match across time, level, title, and description), re-sorts, and resets
// to the first page.
func (h *notificationHistory) applyFilter() {
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
func (h *notificationHistory) refilter() {
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
func (h *notificationHistory) searchText(entry notificationEntry) string {
	return entry.createdAt.Format("2006-01-02 15:04:05") + " " + h.levelText(entry) + " " + entry.title + " " + entry.description
}

// sortFiltered applies the current sort to filtered. The default order
// (sortCol < 0) is the entry order: newest first.
func (h *notificationHistory) sortFiltered() {
	if h.sortCol < 0 || h.sortCol >= len(notificationColumnTitles) || len(h.filtered) < 2 {
		return
	}
	col, desc := h.sortCol, h.sortDesc
	slices.SortStableFunc(h.filtered, func(a, b notificationEntry) int {
		var cmp int
		switch col {
		case 0:
			cmp = a.createdAt.Compare(b.createdAt)
		case 1:
			cmp = strings.Compare(strings.ToLower(h.levelText(a)), strings.ToLower(h.levelText(b)))
		case 2:
			cmp = strings.Compare(strings.ToLower(a.title), strings.ToLower(b.title))
		default:
			cmp = strings.Compare(strings.ToLower(a.description), strings.ToLower(b.description))
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
func (h *notificationHistory) cycleSort() {
	var anchor notificationEntry
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
			if entry.id == anchor.id && entry.createdAt.Equal(anchor.createdAt) {
				h.page = index / h.pageSize
				h.table.SetCursor(index % h.pageSize)
				break
			}
		}
	}
	h.syncPage()
}

// selected returns the filtered entry under the table cursor.
func (h *notificationHistory) selected() (notificationEntry, bool) {
	row := h.table.Cursor()
	if row < 0 || row >= len(h.pageEntries) {
		return notificationEntry{}, false
	}
	return h.pageEntries[row], true
}

// cellValue returns the raw value of one table cell: the formatted time,
// level title, notification title, or the full description.
func (h *notificationHistory) cellValue(row, col int) string {
	if row < 0 || row >= len(h.pageEntries) {
		return ""
	}
	entry := h.pageEntries[row]
	switch col {
	case 0:
		return entry.createdAt.Format("2006-01-02 15:04:05")
	case 1:
		return h.levelText(entry)
	case 2:
		return entry.title
	default:
		return entry.description
	}
}

// copyCell returns a command copying the selected cell's raw value.
func (h *notificationHistory) copyCell() tea.Cmd {
	row := h.table.Cursor()
	if row < 0 || row >= len(h.pageEntries) || h.selectedCol < 0 || h.selectedCol >= len(notificationColumnTitles) {
		return nil
	}
	return copyQueryLogStatement(h.cellValue(row, h.selectedCol))
}

// openViewer opens the selected cell in the viewer overlay, showing the
// untruncated value with wrap toggling.
func (h *notificationHistory) openViewer() {
	row := h.table.Cursor()
	if row < 0 || row >= len(h.pageEntries) || h.selectedCol < 0 || h.selectedCol >= len(notificationColumnTitles) {
		return
	}
	col := h.selectedCol
	h.viewer = newCellViewer(notificationColumnTitles[col], h.cellValue(row, col), max(h.width-8, 1), max(h.height-10, 1))
}

// nextPage advances to the next page, keeping the cursor row.
func (h *notificationHistory) nextPage() {
	if h.page >= h.pageCount()-1 {
		return
	}
	h.page++
	h.syncPage()
}

// prevPage steps back a page, keeping the cursor row.
func (h *notificationHistory) prevPage() {
	if h.page <= 0 {
		return
	}
	h.page--
	h.syncPage()
}

// statusText renders the modal's row-range summary: "1-12 of 25 | page
// 1/3", like the browse status line.
func (h *notificationHistory) statusText() string {
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
func (h *notificationHistory) pager() browsePager {
	pager := browsePager{
		prev:        formCancelButtonStyle.Render(browsePrevLabel),
		next:        formCancelButtonStyle.Render(browseNextLabel),
		prevEnabled: h.page > 0,
		nextEnabled: h.page < h.pageCount()-1,
	}
	if pager.prevEnabled {
		pager.prev = formSaveButtonStyle.Render(browsePrevLabel)
	}
	if pager.nextEnabled {
		pager.next = formSaveButtonStyle.Render(browseNextLabel)
	}
	status := ansi.Truncate(h.statusText(), max(h.viewportWidth()-2-ansi.StringWidth(pager.prev)-ansi.StringWidth(pager.next)-2, 1), "…")
	gap := max(h.viewportWidth()-2-ansi.StringWidth(status)-ansi.StringWidth(pager.prev)-ansi.StringWidth(pager.next), 0)
	pager.prevStart = 3 + ansi.StringWidth(status) + gap
	pager.nextStart = pager.prevStart + ansi.StringWidth(pager.prev)
	pager.line = statusStyle.Render(status + strings.Repeat(" ", gap) + pager.prev + pager.next)
	return pager
}

// handleClick routes a left click inside the modal: the table header
// cycles the sort, the pager buttons page, a data row selects the cell.
func (h *notificationHistory) handleClick(x, y int) {
	if h.viewer != nil || h.filterFocused || x < 1 || x >= h.width-1 || y < 1 || y >= h.height-1 {
		return
	}
	if y == h.height-4 {
		pager := h.pager()
		if pager.prevEnabled && x >= pager.prevStart && x < pager.prevStart+ansi.StringWidth(pager.prev) {
			h.prevPage()
			return
		}
		if pager.nextEnabled && x >= pager.nextStart && x < pager.nextStart+ansi.StringWidth(pager.next) {
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
				revealTableColumn(h.table, h.selectedCol, &h.offset, h.viewportWidth())
			}
		}
	}
}

// columnAt returns the table column under an absolute click x, or -1 when
// the click misses every column.
func (h *notificationHistory) columnAt(x int) int {
	clickOffset := x - 2 + h.offset
	if clickOffset < 0 {
		return -1
	}
	start := 0
	for index, column := range h.table.Columns() {
		end := start + column.Width + 2*spaceCompact
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
func (h *notificationHistory) handleWheel(msg tea.MouseWheelMsg) {
	if h.viewer != nil {
		h.viewer.update(msg)
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
		h.table.SetCursor(clamp(h.table.Cursor()+step, 0, max(len(rows)-1, 0)))
	}
	if hStep != 0 {
		moveTableColumn(&h.table, &h.selectedCol, &h.offset, h.viewportWidth(), hStep)
	}
}

// handleKey routes one key press through the modal. It returns false only
// when the press should close the modal (Escape outside the filter and
// the viewer); every other press is swallowed so no key reaches the panes
// underneath.
func (h *notificationHistory) handleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if h.viewer != nil {
		if msg.Key().Code == tea.KeyEscape {
			h.viewer = nil
			return true, nil
		}
		h.viewer.update(msg)
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
		return true, h.copyCell()
	case 'n', tea.KeyPgDown:
		h.nextPage()
		return true, nil
	case 'p', tea.KeyPgUp:
		h.prevPage()
		return true, nil
	}
	if moveTableCell(&h.table, &h.selectedCol, &h.offset, h.viewportWidth(), msg) {
		return true, nil
	}
	// Swallow everything else so no key reaches the panes underneath.
	return true, nil
}
