package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
)

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
		log.Error("open database", message.err)
		if message.reconnect {
			// The previous database is still connected: a failed switch
			// keeps the current session instead of dropping to failure.
			m.reconnectPending = false
			m.setStatus(safeText(fmt.Sprintf("database switch failed: %v", message.err)))
			return m, nil
		}
		if m.connection.component.Form.Focus == connectionFocusForm {
			m.State = stateConnection
			m.setStatus(safeText(fmt.Sprintf("database unavailable: %v", message.err)))
			m.overlay.formMode.Mode = formModeNormal
			return m, nil
		}
		m.Fail(safeText(fmt.Sprintf("database unavailable: %v", message.err)))
		return m, nil
	}
	m.reconnectPending = false
	previous := m.Database
	m.Opened(message.target, message.service, "")
	if previous != nil {
		if err := previous.Close(); err != nil {
			log.Error("close previous database", err)
		}
	}
	m.databaseInfo = message.info
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
	if store := m.queryLogStore(); store != nil {
		m.queryLog.component.SetEntries(loadQueryLogEntries(store, m.connectionID))
	}
	m.queryLog.component.SetPage(0)
	if store := m.notificationStore(); store != nil {
		if entries, err := store.Load(m.connectionID, 0); err == nil {
			m.notifications.component.SetEntries(entries)
		}
	}
	name := filepath.Base(message.target)
	if configured := strings.TrimSpace(m.connection.component.Form.Values.Name); configured != "" {
		name = configured
	}
	m.setStatus(safeText("ready: " + name))
	// The ready transition surfaces as a Debug log notification (visible
	// only when log_level allows it), not as a plain status popup.
	m.notifications.skipStatusPopup = true
	log.Debug("ready: " + name)
	return m, m.setSchemaObjects(message.objects)
}
