package workbench

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
		if m.connection.focus == connectionFocusForm {
			m.State = stateConnection
			m.setStatus(safeText(fmt.Sprintf("database unavailable: %v", message.err)))
			m.formMode.mode = formModeNormal
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
	m.layout(m.width, m.height)
	m.Focus = focusSchema
	m.editor.text.Blur()
	m.blurTables()
	if !message.reconnect {
		if err := m.recordConnection(message.target); err != nil {
			m.connectionID = ""
			m.setStatus(safeText("saving connection profile: " + err.Error()))
		}
	}
	m.queryLogEntries = loadQueryLog(m.queryLogPath, m.connectionID)
	m.queryLogPage = 0
	m.renderQueryLog()
	m.notificationEntries = loadNotifications(m.notificationPath, m.connectionID)
	name := filepath.Base(message.target)
	if configured := strings.TrimSpace(m.connection.values.name); configured != "" {
		name = configured
	}
	m.setStatus(safeText("ready: " + name))
	return m, m.setSchemaObjects(message.objects)
}
