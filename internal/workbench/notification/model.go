package notification

import (
	"image"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

const (
	// title is the fixed title of every captured status.
	title = "Notification"
	// levelNone marks an entry that is not a logged event (a status
	// message). Positive level values are log.Level + 1, matching the
	// persisted column so older rows keep the neutral appearance.
	levelNone = 0
)

// StoredLogLevel converts a log level to the persisted notification level.
func StoredLogLevel(level log.Level) int { return int(level) + 1 }

// logLevelOf resolves the log level of a stored notification level, if any.
func logLevelOf(level int) (log.Level, bool) {
	if level <= levelNone || level > int(log.LevelError)+1 {
		return log.LevelInfo, false
	}
	return log.Level(level - 1), true
}

// levelColor returns the theme color for a stored level: the level's
// severity color for log entries, the neutral secondary otherwise.
func levelColor(level int) string {
	switch l, ok := logLevelOf(level); l {
	case log.LevelDebug:
		return uikit.ColorMuted
	case log.LevelWarn:
		return uikit.ColorWarn
	case log.LevelError:
		return uikit.ColorDanger
	case log.LevelInfo:
		if ok {
			return uikit.ColorPrimary
		}
		return uikit.ColorSecondary
	default:
		return uikit.ColorSecondary
	}
}

// borderColor returns the popup border color: the level's severity color
// for logged events, the neutral border otherwise.
func borderColor(level int) string {
	if _, ok := logLevelOf(level); ok {
		return levelColor(level)
	}
	return uikit.ColorBorder
}

// logLevelIcon returns the icon glyph for a log level: Nerd Font icons
// when enabled, geometric symbols otherwise. The nerd-font preference is
// the resolved app config, injected by the root shell.
func logLevelIcon(level log.Level) string {
	if nerdFont {
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

// nerdFont is the resolved config.json "nerd_font" preference. Root injects
// it once per app config so icon choice stays a single source of truth.
var nerdFont = true

// SetNerdFont records the resolved nerd-font preference for log icons.
func SetNerdFont(enabled bool) { nerdFont = enabled }

// iconIndent returns the horizontal space reserved on the title/body rows
// for the level symbol plus the gap after it, or 0 for plain status
// notifications (no symbol).
func iconIndent(level int) int {
	if l, ok := logLevelOf(level); ok {
		return max(ansi.StringWidth(logLevelIcon(l)), 1) + 1
	}
	return 0
}

// StatusEntry builds the captured-status entry for one status transition.
func StatusEntry(text string) Entry {
	return Entry{
		CreatedAt:   time.Now(),
		Title:       title,
		Description: text,
	}
}

// LogEntry builds the captured entry for one logged event, carrying the
// level's icon, title, and severity.
func LogEntry(entry log.Entry) Entry {
	return Entry{
		CreatedAt:   entry.Time,
		Title:       logLevelIcon(entry.Level) + " " + entry.Level.Title(),
		Description: uikit.SafeText(entry.Message),
		Level:       StoredLogLevel(entry.Level),
	}
}

// LogWakeupMsg wakes the idle program loop when a log entry arrives from an
// async command; the outer Update wrapper drains the queue into a popup.
type LogWakeupMsg struct{}

// logProgramSender is the wakeup sink of the running program, an interface
// so tests can inject a recording stub.
type logProgramSender interface {
	Send(tea.Msg)
}

// logProgram is the attached program, if any. It receives a LogWakeupMsg
// whenever an entry is enqueued outside an update handler so the idle loop
// wakes and drains the queue. Guarded by queueMu.
var logProgram logProgramSender

// AttachLogProgram wires the running program into the log notification
// pipeline so entries logged by async commands surface as popups even when
// the UI is idle. Call once with the program returned by tea.NewProgram,
// before program.Run. Attaching nil detaches.
func AttachLogProgram(program *tea.Program) {
	queueMu.Lock()
	logProgram = program
	queueMu.Unlock()
}

// Logged events enqueued by the log package notifier between Updates.
// Draining happens in the root Update wrapper so every log call made inside
// an update handler surfaces as a popup in the same message cycle.
var (
	queueMu sync.Mutex
	queue   []log.Entry
)

// EnqueueLogEntry is the log package notifier: it queues entries for the
// next Update drain and wakes the attached program so an entry from an
// async command surfaces even when the UI is idle. Safe for concurrent
// callers (async Bubble Tea commands). The wakeup is sent on its own
// goroutine: the program's message channel is unbuffered, so sending from
// inside an Update handler (where every current log call happens) would
// deadlock on the loop waiting for that same Update to return.
func EnqueueLogEntry(entry log.Entry) {
	queueMu.Lock()
	queue = append(queue, entry)
	program := logProgram
	queueMu.Unlock()
	if program != nil {
		go program.Send(LogWakeupMsg{})
	}
}

// DrainLogEntries returns and clears the queued log entries.
func DrainLogEntries() []log.Entry {
	queueMu.Lock()
	defer queueMu.Unlock()
	entries := queue
	queue = nil
	return entries
}

// DismissMsg closes the visible popup when its generation still matches
// the model's current one.
type DismissMsg struct {
	Generation uint64
}

// DismissTick builds the command that closes the popup after the given
// duration. It is a variable so tests can replace it with an immediate
// dismiss and avoid wall-clock waits.
var DismissTick = func(generation uint64, duration time.Duration) tea.Cmd {
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return DismissMsg{Generation: generation}
	})
}

// Model is the notification feature component: the captured entries, the
// visible popup, the history/detail overlays, the popup click/release
// swallow state, and the dismiss generation counter. Root owns the
// persistence store, the connection scope, and screen geometry; the
// component owns every interaction and renders its own overlays.
type Model struct {
	Entries             []Entry
	Popup               *Entry
	Detail              *Entry
	History             *history
	Generation          uint64
	PopupSwallowRelease bool
}

// New builds an empty notification component.
func New() Model { return Model{} }

// Show surfaces one entry as the visible popup and, when persist is set and
// a connection scope with a store is available, saves it to history first.
// The returned command closes the popup after the configured duration.
func (m Model) Show(entry Entry, persist bool, scope string, store *Store, duration time.Duration) (Model, tea.Cmd) {
	if persist && scope != "" && store != nil {
		if id, err := store.Append(scope, Entry{
			ID:          entry.ID,
			CreatedAt:   entry.CreatedAt,
			Title:       entry.Title,
			Description: entry.Description,
			Level:       entry.Level,
		}, 0); err == nil {
			entry.ID = id
		}
	}
	m.Entries = append([]Entry{entry}, m.Entries...)
	m.Popup = &entry
	m.Generation++
	return m, DismissTick(m.Generation, duration)
}

// SetEntries replaces the captured entry list (the scoped history load).
func (m *Model) SetEntries(entries []Entry) { m.Entries = entries }

// OpenHistory opens the history modal, selecting the entry with the given
// SQLite row ID (0 or absent falls back to the newest entry).
func (m *Model) OpenHistory(selectedID int64, width, height int) {
	m.History = NewHistory(m.Entries, selectedID, width, height)
}

// ResizeHistory refits an open history modal to a new window size.
func (m *Model) ResizeHistory(width, height int) {
	if m.History != nil {
		m.History.resize(width, height)
	}
}

// Reset clears the captured entries and every overlay in place.
func (m *Model) Reset() {
	m.Entries = nil
	m.Popup = nil
	m.Detail = nil
	m.History = nil
}

// PopupOpen reports whether the popup is visible.
func (m Model) PopupOpen() bool { return m.Popup != nil }

// HistoryOpen reports whether the history modal is open.
func (m Model) HistoryOpen() bool { return m.History != nil }

// DetailOpen reports whether the single-entry detail overlay is open.
func (m Model) DetailOpen() bool { return m.Detail != nil }

// PopupBounds returns the screen rectangle of the visible popup. The popup
// is a bordered card anchored to the top-right corner.
func (m Model) PopupBounds(layout uikit.Layout) (image.Rectangle, bool) {
	return popupBounds(m.Popup, layout)
}

// Consumes reports whether msg belongs to the notification overlays: the
// popup dismiss timer, the popup click and its trailing release, or an open
// history/detail modal (which swallows every input while open). Root routes
// the message into Update only when this returns true, so an idle
// notification state never intercepts messages meant for the panes.
func (m Model) Consumes(msg tea.Msg, layout uikit.Layout) bool {
	switch msg := msg.(type) {
	case DismissMsg:
		return true
	case tea.MouseReleaseMsg:
		return m.PopupSwallowRelease || m.History != nil || m.Detail != nil
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if bounds, ok := m.PopupBounds(layout); ok && msg.X >= bounds.Min.X && msg.X < bounds.Max.X && msg.Y >= bounds.Min.Y && msg.Y < bounds.Max.Y {
				return true
			}
		}
		return m.History != nil || m.Detail != nil
	case tea.MouseWheelMsg:
		return m.History != nil || m.Detail != nil
	case tea.KeyPressMsg:
		return m.History != nil || m.Detail != nil
	}
	return false
}
