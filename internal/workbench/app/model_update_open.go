package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
)

// Both history stores share one SQLite database. Serialize their complete
// command-owned lifecycles so migrations and writes cannot race each other.
var historyPersistenceMu sync.Mutex

type notificationPersistedMsg struct {
	openTag      uint64
	connectionID string
	token        uint64
	id           int64
	err          error
}

type historyLoadedMsg struct {
	openTag             uint64
	connectionID        string
	queryEntries        []queryLogEntry
	notificationEntries []notification.Entry
	queryErr            error
	notificationErr     error
}

func (m Model) updateOpen(message databaseOpenedMsg) (tea.Model, tea.Cmd) {
	if message.openTag != m.openTag {
		// Superseded by a newer switch or a disconnect: drop the orphaned
		// connection (and any error) without touching the current session.
		if message.err == nil && message.service != nil {
			if err := message.service.Close(); err != nil {
				log.Error("close superseded database", err)
			}
		}
		return m, nil
	}
	if message.err != nil {
		// Redact credential material before it reaches the event log,
		// notification history, status line, or failure screen.
		redacted := redactCredentials(message.err.Error(), m.connectionSecrets(m.Target))
		log.Error("open database", errors.New(redacted))
		if message.reconnect {
			// The previous database is still connected: a failed switch
			// keeps the current session instead of dropping to failure.
			m.reconnectPending = false
			m.setStatus(safeText(fmt.Sprintf("database switch failed: %v", redacted)))
			return m, nil
		}
		if m.connection.component.Form.Focus == connectionFocusForm {
			m.State = stateConnection
			m.setStatus(safeText(fmt.Sprintf("database unavailable: %v", redacted)))
			m.overlay.formMode.Mode = formModeNormal
			return m, nil
		}
		m.Fail(safeText(fmt.Sprintf("database unavailable: %v", redacted)))
		return m, nil
	}
	m.reconnectPending = false
	previous := m.Database
	m.Opened(message.target, message.service, "")
	m.connectionTarget = message.requested
	m.connectionPlugin = message.pluginID
	if previous != nil {
		if err := previous.Close(); err != nil {
			log.Error("close previous database", err)
		}
	}
	m.databaseInfo = message.info
	m.queryLanguage = message.queryLanguage
	m.workspace.advertised = message.workspace
	m.resetWorkspaceView()
	m.queryLog.editor.setLanguage(message.queryLanguage)
	m.refreshBrowseBackend()
	m.chat.component.Executor = chatExecutor{service: message.service}
	m.chat.component.Target = message.target
	m.chat.component.ReadOnly = m.ReadOnly
	m.applyLayout(m.layout.width, m.layout.height)
	m.Focus = focusSchema
	m.queryLog.editor.text.Blur()
	m.blurTables()
	if !message.reconnect {
		if err := m.recordConnection(message.target); err != nil {
			m.connectionID = ""
			m.setStatus(safeText("saving connection profile: " + err.Error()))
		}
	}
	// A new scope starts empty while both histories load asynchronously. Any
	// entries appended after this reset remain available for merge on reply.
	m.queryLog.component.Reset()
	m.queryLog.component.SetPage(0)
	m.queryLog.transientStatements = nil
	m.notifications.component.Reset()

	name := filepath.Base(message.target)
	if configured := strings.TrimSpace(m.connection.component.Form.Values.Name); configured != "" {
		name = configured
	}
	m.setStatus(safeText("ready: " + name))
	// The ready transition surfaces as a Debug log notification (visible
	// only when log_level allows it), not as a plain status popup.
	m.notifications.skipStatusPopup = true
	log.Debug("ready: " + name)
	return m, tea.Batch(
		m.setSchemaObjects(message.objects),
		m.loadSchemaForeignKeysAll(),
		m.loadSchemaIndexesAll(),
		loadHistory(m.queryLog.path, m.notifications.path, m.connectionID, m.openTag,
			queryLogRetentionDays(), notificationRetentionDays(), queryLogLimit, notification.Limit),
	)
}

func persistNotification(entry notification.Entry, path, connectionID string, openTag, token uint64, retentionDays int) tea.Cmd {
	return func() tea.Msg {
		historyPersistenceMu.Lock()
		defer historyPersistenceMu.Unlock()

		store, err := notification.Open(path, retentionDays)
		var id int64
		if err == nil {
			id, err = store.Append(connectionID, entry, 0)
			closeErr := store.Close()
			if err == nil {
				err = closeErr
			}
		}
		return notificationPersistedMsg{
			openTag:      openTag,
			connectionID: connectionID,
			token:        token,
			id:           id,
			err:          err,
		}
	}
}

func loadHistory(queryPath, notificationPath, connectionID string, openTag uint64, queryRetentionDays, notificationRetentionDays, queryLimit, notificationLimit int) tea.Cmd {
	return func() tea.Msg {
		historyPersistenceMu.Lock()
		defer historyPersistenceMu.Unlock()

		var (
			queryEntries        []queryLogEntry
			notificationEntries []notification.Entry
			queryErr            error
			notificationErr     error
		)
		queryStore, err := querylog.Open(queryPath, queryRetentionDays)
		if err != nil {
			queryErr = err
		} else {
			queryEntries, queryErr = queryStore.Load(connectionID, queryLimit)
			closeErr := queryStore.Close()
			if queryErr == nil {
				queryErr = closeErr
			}
		}
		notificationStore, err := notification.Open(notificationPath, notificationRetentionDays)
		if err != nil {
			notificationErr = err
		} else {
			notificationEntries, notificationErr = notificationStore.Load(connectionID, notificationLimit)
			closeErr := notificationStore.Close()
			if notificationErr == nil {
				notificationErr = closeErr
			}
		}
		return historyLoadedMsg{
			openTag:             openTag,
			connectionID:        connectionID,
			queryEntries:        queryEntries,
			notificationEntries: notificationEntries,
			queryErr:            queryErr,
			notificationErr:     notificationErr,
		}
	}
}

func queryHistoryKey(entry queryLogEntry) string {
	return fmt.Sprintf("%d\x00%s\x00%d\x00%s\x00%s\x00%s\x00%t\x00%t",
		entry.StartedAt.UnixNano(), entry.Statement, entry.Duration,
		entry.Message, entry.Status, entry.Language, entry.Replayable, entry.Sensitive)
}

func mergeQueryHistory(current, loaded []queryLogEntry) []queryLogEntry {
	merged := make([]queryLogEntry, 0, min(len(current)+len(loaded), queryLogLimit))
	seen := make(map[string]struct{}, len(current)+len(loaded))
	appendUnique := func(entries []queryLogEntry) {
		for _, entry := range entries {
			key := queryHistoryKey(entry)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, entry)
		}
	}
	appendUnique(current)
	appendUnique(loaded)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].StartedAt.After(merged[j].StartedAt)
	})
	if len(merged) > queryLogLimit {
		merged = merged[:queryLogLimit]
	}
	return merged
}

func sameNotificationEntry(a, b notification.Entry) bool {
	if a.ID != 0 && b.ID != 0 {
		return a.ID == b.ID
	}
	return a.CreatedAt.Equal(b.CreatedAt) &&
		a.Title == b.Title &&
		a.Description == b.Description &&
		a.Level == b.Level
}

func mergeNotificationHistory(current, loaded []notification.Entry) []notification.Entry {
	merged := make([]notification.Entry, 0, min(len(current)+len(loaded), notification.Limit))
	appendUnique := func(entries []notification.Entry) {
		for _, entry := range entries {
			duplicate := false
			for _, existing := range merged {
				if sameNotificationEntry(existing, entry) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				merged = append(merged, entry)
			}
		}
	}
	appendUnique(current)
	appendUnique(loaded)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].CreatedAt.Equal(merged[j].CreatedAt) {
			return merged[i].ID > merged[j].ID
		}
		return merged[i].CreatedAt.After(merged[j].CreatedAt)
	})
	if len(merged) > notification.Limit {
		merged = merged[:notification.Limit]
	}
	return merged
}

func (m Model) updateHistoryLoaded(message historyLoadedMsg) (tea.Model, tea.Cmd) {
	if message.openTag != m.openTag || message.connectionID != m.connectionID {
		return m, nil
	}
	currentQuery := m.queryLog.component.Entries
	currentNotifications := m.notifications.component.Entries

	if message.queryErr == nil {
		mergedQuery := mergeQueryHistory(currentQuery, message.queryEntries)
		transient := make(map[string]string, len(currentQuery))
		for index, entry := range currentQuery {
			if index < len(m.queryLog.transientStatements) && m.queryLog.transientStatements[index] != "" {
				transient[queryHistoryKey(entry)] = m.queryLog.transientStatements[index]
			}
		}
		m.queryLog.component.SetEntries(mergedQuery)
		m.queryLog.component.SetPage(0)
		m.queryLog.transientStatements = make([]string, len(mergedQuery))
		for index, entry := range mergedQuery {
			m.queryLog.transientStatements[index] = transient[queryHistoryKey(entry)]
		}
	}
	if message.notificationErr == nil {
		mergedNotifications := mergeNotificationHistory(currentNotifications, message.notificationEntries)
		m.notifications.component.SetEntries(mergedNotifications)
	}
	return m, nil
}
