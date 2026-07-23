package workbench

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type connectionActionMsg struct{ action string }
type connectionValidationMsg struct{}

func sequenceConnectionAction(init tea.Cmd, action string) tea.Cmd {
	return tea.Sequence(init, func() tea.Msg { return connectionActionMsg{action: action} })
}

func (m *Model) setRecentConnections(connections []recentConnection) {
	m.recentConnections = connections
	_ = m.recent.SetItems(recentListItems(connections))
	if m.recentPath != "" {
		_ = saveRecentConnections(m.recentPath, connections)
	}
}

func (m *Model) recordConnection() {
	if m.connection.values.driver != driverSQLite {
		return
	}
	connection := recentConnection{Driver: driverSQLite, Name: m.connection.connectionName(), Target: m.connection.targetValue()}
	connections := make([]recentConnection, 0, min(len(m.recentConnections)+1, maxRecentConnections))
	connections = append(connections, connection)
	for _, existing := range m.recentConnections {
		if existing.Driver != connection.Driver || existing.Target != connection.Target {
			connections = append(connections, existing)
		}
		if len(connections) == maxRecentConnections {
			break
		}
	}
	m.setRecentConnections(connections)
}

func (m *Model) selectedRecentConnection() (recentConnection, bool) {
	connection, ok := m.recent.SelectedItem().(recentConnection)
	return connection, ok
}

func (m *Model) editSelectedRecentConnection() tea.Cmd {
	connection, ok := m.selectedRecentConnection()
	if !ok {
		m.Status = "select a recent connection"
		return nil
	}
	m.connection.values.driver, m.connection.values.name, m.connection.values.target = connection.Driver, connection.Name, connection.Target
	command := m.connection.rebuildForm()
	m.connection.focus = connectionFocusForm
	m.Status = "editing " + safeText(connection.Name)
	return command
}

func (m *Model) deleteSelectedRecentConnection() {
	connection, ok := m.selectedRecentConnection()
	if !ok {
		m.Status = "select a recent connection"
		return
	}
	connections := make([]recentConnection, 0, len(m.recentConnections)-1)
	for _, existing := range m.recentConnections {
		if existing.Driver != connection.Driver || existing.Target != connection.Target {
			connections = append(connections, existing)
		}
	}
	m.setRecentConnections(connections)
	m.Status = "deleted " + safeText(connection.Name)
}

func (m *Model) newConnection() tea.Cmd {
	m.connection.values = &connectionFormValues{port: "3306", action: connectionActionTest}
	command := m.connection.rebuildForm()
	m.connection.focus = connectionFocusForm
	m.formMode.mode = formModeNormal
	m.Status = "new connection"
	return command
}

func (m Model) testConnection() tea.Cmd {
	target := m.connectionTarget()
	return func() tea.Msg {
		if err := m.connection.validate(); err != nil {
			return connectionTestMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(m.appContext, 5*time.Second)
		defer cancel()
		opened, err := m.openDatabase(ctx, target)
		if err != nil {
			return connectionTestMsg{err: err}
		}
		return connectionTestMsg{err: opened.Service.Close()}
	}
}

func (m Model) connectionTarget() string {
	target := m.connection.targetValue()
	switch m.connection.values.driver {
	case driverMySQL:
		return "mysql:" + target
	case driverPostgreSQL:
		return "postgres:" + target
	default:
		return target
	}
}

func (m Model) openConnection() (tea.Model, tea.Cmd) {
	if err := m.connection.validate(); err != nil {
		m.Status = safeText(err.Error())
		return m, nil
	}
	target := m.connectionTarget()
	m.BeginOpening(target, "opening "+safeText(m.connection.connectionName()))
	return m, m.openTarget(target)
}

func (m Model) connectionActionFocused() bool {
	return m.connection.form != nil && m.connection.form.GetFocusedField().GetKey() == "action"
}

func (m Model) updateConnection(message tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := message.(connectionValidationMsg); ok {
		m.connection.focusValidationError()
		return m, nil
	}
	if action, ok := message.(connectionActionMsg); ok {
		switch action.action {
		case connectionActionTest:
			return m, m.testConnection()
		case connectionActionConnect:
			return m.openConnection()
		}
	}
	if test, ok := message.(connectionTestMsg); ok {
		if test.err != nil {
			m.Status = "connection test failed"
		} else {
			m.Status = "connection test succeeded: " + safeText(m.connection.connectionName())
		}
		return m, nil
	}
	if m.connection.focus == connectionFocusRecent {
		keyPress, ok := message.(tea.KeyPressMsg)
		if ok {
			switch keyPress.String() {
			case "2":
				m.connection.focus = connectionFocusForm
				return m, nil
			case "a":
				return m, m.newConnection()
			case "e", "enter":
				return m, m.editSelectedRecentConnection()
			case "d":
				m.deleteSelectedRecentConnection()
				return m, nil
			}
		}
		var command tea.Cmd
		m.recent, command = m.recent.Update(message)
		return m, command
	}
	keyPress, isKeyPress := message.(tea.KeyPressMsg)
	if isKeyPress && m.connection.confirmation == nil && m.connectionActionFocused() &&
		keyPress.Key().Code == tea.KeyEnter && keyPress.Key().Mod == 0 {
		m.formMode.mode = formModeNormal
		m.connection.blur()
		if m.connection.values.action == connectionActionTest {
			return m, m.testConnection()
		}
		return m.openConnection()
	}
	if isKeyPress && m.connection.confirmation == nil && m.executeKey(keyPress) {
		if err := m.connection.validate(); err != nil {
			m.Status = safeText(err.Error())
			return m, m.connection.showValidationError()
		}
		m.formMode.beginConfirm()
		return m, m.connection.beginConfirmation()
	}
	if route := m.formMode.routeHuh(message, m.connection.blur); route != formRouteParent {
		if route == formRouteConsumed && m.connection.confirmation != nil && m.formMode.mode == formModeNormal {
			m.connection.confirmation = nil
		}
		if route != formRouteHuh {
			return m, nil
		}
		command, action := m.connection.updateHuh(message, m.formMode)
		switch action {
		case connectionActionTest:
			if command != nil {
				return m, sequenceConnectionAction(command, action)
			}
			return m, m.testConnection()
		case connectionActionConnect:
			if m.connection.confirmation != nil {
				return m, command
			}
			if command != nil {
				return m, sequenceConnectionAction(command, action)
			}
			return m.openConnection()
		}
		return m, command
	}
	if !isKeyPress {
		return m, nil
	}
	switch keyPress.Key().Code {
	case tea.KeyLeft, 'h', tea.KeyRight, 'l':
		if m.connection.form != nil && m.connection.form.GetFocusedField().GetKey() == "action" {
			model, cmd := m.connection.form.Update(message)
			m.connection.form = model.(*huh.Form)
			return m, cmd
		}
	}
	switch keyPress.String() {
	case "1":
		m.connection.setFocus(connectionFocusRecent)
		return m, nil
	case "i", "enter":
		return m, m.formMode.beginHuh(m.connection.focusForm())
	case "j", "down":
		return m, m.connection.form.NextField()
	case "k", "up":
		return m, m.connection.form.PrevField()
	}
	return m, nil
}

func (m Model) connectionPaneView(height int) string {
	content := m.connection.View()
	mode := "NORMAL"
	if m.formMode.editing() {
		mode = "INSERT"
	}
	return content + strings.Repeat("\n", max(height-strings.Count(content, "\n")-1, 1)) + headerStyle.Render(mode)
}
