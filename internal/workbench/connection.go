package workbench

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/log"
)

type connectionActionMsg struct{ action string }
type connectionValidationMsg struct{}

func sequenceConnectionAction(init tea.Cmd, action string) tea.Cmd {
	return tea.Sequence(init, func() tea.Msg { return connectionActionMsg{action: action} })
}

// applyRecentFilter pushes the visible filter input's value into the
// profiles list, which filters its items and reports the committed state
// the status bar mirrors.
func (m *Model) applyRecentFilter() {
	if query := strings.TrimSpace(m.recentFilter.Value()); query != "" {
		m.recent.SetFilterText(query)
		return
	}
	m.recent.ResetFilter()
}

func (m *Model) setRecentConnections(connections []recentConnection) {
	m.recentConnections = connections
	_ = m.recent.SetItems(recentListItems(connections))
	if m.recentPath != "" {
		_ = saveRecentConnections(m.recentPath, connections)
	}
}

// recordConnection saves the form's current credentials as a recent profile.
// openedTarget is the target that was actually opened (Connect after updateOpen,
// or the target a successful Test just verified); for SQLite it wins over the
// form value, so Connect records the resolved file while Test records the form
// target and never picks up a previously connected m.Target.
func (m *Model) recordConnection(openedTarget string) error {
	driver := m.connection.values.driver
	if driver == "" {
		driver = driverSQLite
	}
	target := strings.TrimSpace(m.connection.values.target)
	name := strings.TrimSpace(m.connection.values.name)
	if driver == driverSQLite {
		if opened := strings.TrimSpace(openedTarget); opened != "" {
			target = opened
		}
		if name == "" {
			name = filepath.Base(target)
		}
	} else if name == "" {
		name = m.connection.connectionName()
	}
	connection := recentConnection{
		Driver:   driver,
		Name:     name,
		Target:   target,
		Pass:     m.connection.values.pass,
		ReadOnly: m.ReadOnly,
	}
	if connection.Driver != driverSQLite {
		connection.Host = m.connection.hostValue()
		connection.Port = m.connection.portValue()
		connection.User = strings.TrimSpace(m.connection.values.user)
		switch connection.Driver {
		case driverMySQL:
			connection.MySQLTLS = m.connection.values.mysqlTLS
		case driverPostgreSQL:
			connection.PostgreSQLTLS = m.connection.values.postgresTLS
		}
	}
	// Reuse or generate the opaque profile identity. Editing a profile keeps
	// its ID; a new profile that matches an existing one reuses that ID so its
	// scoped chat/query history survives; otherwise mint a UUIDv7. Never
	// write a profile without a valid scope.
	id := strings.TrimSpace(m.connection.values.id)
	if !validConnectionID(id) {
		for _, existing := range m.recentConnections {
			if sameRecentConnection(existing, connection) && validConnectionID(existing.ID) {
				id = existing.ID
				break
			}
		}
	}
	if !validConnectionID(id) {
		generated, err := newConnectionID()
		if err != nil {
			return err
		}
		id = generated
	}
	connection.ID = id
	connections := make([]recentConnection, 0, min(len(m.recentConnections)+1, maxRecentConnections))
	connections = append(connections, connection)
	for _, existing := range m.recentConnections {
		if sameRecentConnection(existing, connection) {
			continue
		}
		if len(connections) == maxRecentConnections {
			break
		}
		connections = append(connections, existing)
	}
	m.setRecentConnections(connections)
	m.connectionID = connection.ID
	return nil
}

func sameRecentConnection(left, right recentConnection) bool {
	if left.Driver != right.Driver {
		return false
	}
	if left.Driver == driverSQLite {
		return left.Target == right.Target
	}
	return left.Name == right.Name
}

func (m *Model) selectedRecentConnection() (recentConnection, bool) {
	connection, ok := m.recent.SelectedItem().(recentConnection)
	return connection, ok
}

// loadRecentConnectionValues copies a saved profile into the connection
// form's values, normalizing empty TLS modes to the disabled defaults.
func (m *Model) loadRecentConnectionValues(connection recentConnection) {
	m.connection.values.driver, m.connection.values.name, m.connection.values.target = connection.Driver, connection.Name, connection.Target
	m.connection.values.id = connection.ID
	m.connection.values.host, m.connection.values.port, m.connection.values.user = connection.Host, connection.Port, connection.User
	m.connection.values.mysqlTLS = connection.MySQLTLS
	if m.connection.values.mysqlTLS == "" {
		m.connection.values.mysqlTLS = mysqlTLSDisabled
	}
	m.connection.values.postgresTLS = connection.PostgreSQLTLS
	if m.connection.values.postgresTLS == "" {
		m.connection.values.postgresTLS = postgresTLSDisabled
	}
	m.connection.values.readOnly = connection.ReadOnly
	m.connection.values.pass = connection.Pass
}

func (m *Model) editSelectedRecentConnection() tea.Cmd {
	connection, ok := m.selectedRecentConnection()
	if !ok {
		m.setStatus("select a connection profile")
		return nil
	}
	m.loadRecentConnectionValues(connection)
	command := m.connection.rebuildForm()
	m.connection.focus = connectionFocusForm
	m.setStatus("editing " + safeText(connection.Name))
	return m.openForm(command, m.connection.focusForm)
}

// confirmDeleteRecentConnection opens the Delete connection? confirmation for
// the selected profile; the actual removal runs on confirm.
func (m *Model) confirmDeleteRecentConnection() {
	connection, ok := m.selectedRecentConnection()
	if !ok {
		m.setStatus("select a connection profile")
		return
	}
	m.deletePending = "connection"
	m.deletePendingConnection = &connection
	m.deleteConfirm = yesNoConfirmation("Delete connection?", safeText(connection.Name), "delete")
}

// deleteRecentConnection removes the given profile from the recent list.
func (m *Model) deleteRecentConnection(connection recentConnection) {
	connections := make([]recentConnection, 0, len(m.recentConnections)-1)
	for _, existing := range m.recentConnections {
		if sameRecentConnection(existing, connection) {
			continue
		}
		connections = append(connections, existing)
	}
	m.setRecentConnections(connections)
	m.setStatus("deleted " + safeText(connection.Name))
}

func (m *Model) newConnection() tea.Cmd {
	m.connection.values = &connectionFormValues{mysqlTLS: mysqlTLSDisabled, postgresTLS: postgresTLSDisabled, action: connectionActionTest}
	command := m.connection.rebuildForm()
	m.connection.focus = connectionFocusForm
	m.formMode.mode = formModeNormal
	m.setStatus("new connection")
	return m.openForm(command, m.connection.focusForm)
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
		return connectionTestMsg{err: opened.Service.Close(), target: target}
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
		m.setStatus(safeText(err.Error()))
		return m, nil
	}
	m.ReadOnly = m.connection.values.readOnly
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
			log.Error("connection test", test.err)
			m.setStatus("connection test failed")
			return m, nil
		}
		// A successful test stands in for the removed Save button: the form's
		// current credentials become the saved profile.
		if err := m.recordConnection(test.target); err != nil {
			m.setStatus(safeText("saving connection profile: " + err.Error()))
			return m, nil
		}
		m.setStatus("connection test succeeded: " + safeText(m.connection.connectionName()))
		return m, nil
	}
	if m.connection.focus == connectionFocusRecent {
		keyPress, ok := message.(tea.KeyPressMsg)
		if m.recentFilter.Focused() {
			// Filter editing: every message goes to the input (so clipboard
			// paste works too); enter/escape exit editing, keeping the
			// applied filter.
			if ok {
				switch keyPress.Code {
				case tea.KeyEscape, tea.KeyEnter:
					m.recentFilter.Blur()
					return m, nil
				}
			}
			before := m.recentFilter.Value()
			var filterCommand tea.Cmd
			m.recentFilter, filterCommand = m.recentFilter.Update(message)
			if m.recentFilter.Value() != before {
				m.applyRecentFilter()
			}
			return m, filterCommand
		}
		if ok {
			switch {
			case m.keybindings.Match(keyPress, "connection.switch_to_form", []scope{scopeView, scopeGlobal}):
				m.connection.focus = connectionFocusForm
				return m, nil
			case m.keybindings.Match(keyPress, "connection.filter", []scope{scopeView, scopeGlobal}):
				m.recentFilter.Focus()
				return m, nil
			case m.keybindings.Match(keyPress, "connection.add", []scope{scopeView, scopeGlobal}):
				return m, m.newConnection()
			case m.keybindings.Match(keyPress, "connection.edit", []scope{scopeView, scopeGlobal}):
				return m, m.editSelectedRecentConnection()
			case m.keybindings.Match(keyPress, "connection.delete", []scope{scopeView, scopeGlobal}):
				m.confirmDeleteRecentConnection()
				return m, nil
			case m.keybindings.Match(keyPress, "connection.context_menu", []scope{scopeView, scopeGlobal}):
				if _, ok := m.selectedRecentConnection(); ok {
					m.openRecentConnectionMenu(m.schemaWidth/2, m.recentRowY(m.recent.Index())+1)
				}
				return m, nil
			}
		}
		var command tea.Cmd
		m.recent, command = m.recent.Update(message)
		// The list's own keymap can clear the filter (esc in list
		// navigation); keep the visible input in sync.
		if !m.recent.IsFiltered() && m.recentFilter.Value() != "" {
			m.recentFilter.SetValue("")
		}
		return m, command
	}
	if m.connection.confirmation != nil {
		completed, action := m.connection.confirmation.Update(message, m.width, m.height)
		if !completed {
			return m, nil
		}
		m.connection.confirmation = nil
		m.formMode.mode = formModeNormal
		if action == connectionActionConnect {
			return m.openConnection()
		}
		return m, nil
	}
	keyPress, isKeyPress := message.(tea.KeyPressMsg)
	if isKeyPress && m.connection.confirmation == nil && m.connectionActionFocused() &&
		m.keybindings.Match(keyPress, "connection.action_enter", []scope{scopeView, scopeGlobal}) {
		m.formMode.mode = formModeNormal
		m.connection.blur()
		if m.connection.values.action == connectionActionTest {
			return m, m.testConnection()
		}
		return m.openConnection()
	}
	if isKeyPress && m.connection.confirmation == nil && m.keybindings.Match(keyPress, "connection.execute", []scope{scopeView, scopeGlobal}) {
		if err := m.connection.validate(); err != nil {
			m.setStatus(safeText(err.Error()))
			return m, m.connection.showValidationError()
		}
		m.formMode.beginConfirm()
		return m, m.connection.beginConfirmation()
	}
	if route := m.formMode.routeHuh(message, m.connection.blur); route != formRouteParent {
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
	switch {
	case m.keybindings.Match(keyPress, "connection.switch_to_list", []scope{scopeView, scopeGlobal}):
		m.connection.setFocus(connectionFocusRecent)
		return m, nil
	case isInsertModeKey(keyPress), m.keybindings.Match(keyPress, "connection.edit_field", []scope{scopeView, scopeGlobal}):
		return m, m.formMode.beginHuh(m.connection.focusForm())
	case m.keybindings.Match(keyPress, "connection.field_next", []scope{scopeView, scopeGlobal}):
		return m, m.connection.form.NextField()
	case m.keybindings.Match(keyPress, "connection.field_prev", []scope{scopeView, scopeGlobal}):
		return m, m.connection.form.PrevField()
	}
	return m, nil
}

func (m Model) connectionPaneView(height int) string {
	content := m.connection.View()
	footer := m.modeBadge()
	return content + strings.Repeat("\n", max(height-strings.Count(content, "\n")-1, 1)) + footer
}

// recentPaneView renders the profiles list with its pane-local action hints;
// the layout reserves rows for the filter box (3) and the hint line (1).
func (m Model) recentPaneView() string {
	body := m.recent.View()
	if row := m.recentFilterRow(); row != "" {
		body = row + "\n" + body
	}
	return body + "\n" + chrome.PaneStatus("a add | e edit | d delete | / filter", "", max(m.schemaWidth-6, 0))
}
