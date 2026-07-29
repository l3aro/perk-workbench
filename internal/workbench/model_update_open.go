package workbench

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
)

func (m Model) updateOpen(message databaseOpenedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		log.Error("open database", message.err)
		if m.connection.focus == connectionFocusForm {
			m.State = stateConnection
			m.Status = safeText(fmt.Sprintf("database unavailable: %v", message.err))
			m.formMode.mode = formModeNormal
			return m, nil
		}
		m.Fail(safeText(fmt.Sprintf("database unavailable: %v", message.err)))
		return m, nil
	}
	m.Opened(message.target, message.service, "")
	m.databaseInfo = message.info
	m.layout(m.width, m.height)
	m.Focus = focusSchema
	m.editor.text.Blur()
	m.blurTables()
	m.recordConnection()
	name := filepath.Base(message.target)
	if configured := strings.TrimSpace(m.connection.values.name); configured != "" {
		name = configured
	}
	m.Status = safeText("ready: " + name)
	return m, m.setSchemaObjects(message.objects)
}
