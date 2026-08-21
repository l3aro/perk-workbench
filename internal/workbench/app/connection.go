package app

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/log"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Connection-feature aliases: the component owns the form, values, and
// profiles; root code names the shared constants and types through these.
type connectionDriver = connection.Driver
type mysqlTLSMode = connection.MySQLTLS
type postgresTLSMode = connection.PostgreSQLTLS
type connectionFormValues = connection.FormValues

const (
	driverSQLite     = connection.DriverSQLite
	driverMySQL      = connection.DriverMySQL
	driverPostgreSQL = connection.DriverPostgreSQL

	mysqlTLSVerify     = connection.MySQLTLSVerify
	mysqlTLSSkipVerify = connection.MySQLTLSSkipVerify
	mysqlTLSDisabled   = connection.MySQLTLSDisabled

	postgresTLSVerifyFull = connection.PostgreSQLTLSVerifyFull
	postgresTLSEncrypt    = connection.PostgreSQLTLSEncrypt
	postgresTLSDisabled   = connection.PostgreSQLTLSDisabled

	connectionFocusRecent = connection.FocusRecent
	connectionFocusForm   = connection.FocusForm

	connectionActionTest    = connection.ActionTest
	connectionActionConnect = connection.ActionConnect
)

type connectionActionMsg struct{ action string }

type connectionTestMsg struct {
	pluginID string
	err      error
	target   string // target that was tested; persisted when the test succeeds
}

func sequenceConnectionAction(init tea.Cmd, action string) tea.Cmd {
	return tea.Sequence(init, func() tea.Msg { return connectionActionMsg{action: action} })
}

// setRecentConnections replaces the persisted profile list through the
// component and saves best-effort.
func (m *Model) setRecentConnections(connections []profile.Profile) {
	m.connection.component.SetProfiles(connections)
	m.connection.component.Save()
}

// recordConnection saves the form's current credentials as a recent
// profile and assigns its identity to the connection scope. openedTarget
// is the target that was actually opened (Connect after updateOpen, or
// the target a successful Test just verified).
func (m *Model) recordConnection(openedTarget string) error {
	saved, err := m.connection.component.Record(openedTarget, m.ReadOnly)
	if err != nil {
		return err
	}
	m.connectionID = saved.ID
	return nil
}

func (m *Model) selectedRecentConnection() (profile.Profile, bool) {
	return m.connection.component.Selected()
}

// loadRecentConnectionValues copies a saved profile into the connection
// form's values, normalizing empty TLS modes to the disabled defaults.
func (m *Model) loadRecentConnectionValues(saved profile.Profile) {
	m.connection.component.LoadValues(saved)
	if saved.Plugin != "" {
		m.connectionPlugin = saved.Plugin
	}
}

func (m *Model) editSelectedRecentConnection() tea.Cmd {
	saved, ok := m.selectedRecentConnection()
	if !ok {
		m.setStatus("select a connection profile")
		return nil
	}
	m.loadRecentConnectionValues(saved)
	command := m.connection.component.Form.Rebuild()
	m.connection.component.Form.Focus = connectionFocusForm
	m.setStatus("editing " + safeText(saved.Name))
	// The editing transition surfaces as a Debug log notification, not a
	// plain status popup.
	m.notifications.skipStatusPopup = true
	log.Debug("editing " + safeText(saved.Name))
	return m.openForm(command, m.connection.component.Form.FocusForm)
}

// confirmDeleteRecentConnection opens the Delete connection? confirmation
// for the selected profile; the actual removal runs on confirm.
func (m *Model) confirmDeleteRecentConnection() {
	saved, ok := m.selectedRecentConnection()
	if !ok {
		m.setStatus("select a connection profile")
		return
	}
	m.overlay.deletePending = "connection"
	m.overlay.deletePendingConnection = &saved
	m.overlay.deleteConfirm = yesNoConfirmation("Delete connection?", safeText(saved.Name), "delete")
}

// deleteRecentConnection removes the given profile from the recent list.
func (m *Model) deleteRecentConnection(connection profile.Profile) {
	m.connection.component.Delete(connection)
	m.setStatus("deleted " + safeText(connection.Name))
}
func (m *Model) newConnection() tea.Cmd {
	values := &connectionFormValues{
		MySQLTLS: mysqlTLSDisabled, PostgreSQLTLS: postgresTLSDisabled, Action: connectionActionTest,
	}
	plugins := database.FormPlugins()
	if sqlite, ok := database.ByPlugin("sqlite"); ok && sqlite.Form != nil {
		values.Plugin, values.Driver = sqlite.PluginID, connectionDriver(sqlite.Driver)
	} else if len(plugins) > 0 {
		values.Plugin, values.Driver = plugins[0].PluginID, connectionDriver(plugins[0].Driver)
	}
	m.connection.component.Form.Values = values
	command := m.connection.component.Form.Rebuild()
	m.connection.component.Form.Focus = connectionFocusForm
	m.overlay.formMode.Mode = formModeNormal
	m.setStatus("new connection")
	return m.openForm(command, m.connection.component.Form.FocusForm)
}
func (m Model) testConnection() tea.Cmd {
	pluginID := m.connection.component.Form.Values.Plugin
	target := m.connection.component.ConnectionTarget()
	return func() tea.Msg {
		if err := m.connection.component.Form.Validate(); err != nil {
			return connectionTestMsg{pluginID: pluginID, err: err, target: target}
		}
		ctx, cancel := context.WithTimeout(m.appContext, 5*time.Second)
		defer cancel()
		opened, err := m.openDatabase(ctx, pluginID, target)
		if err != nil {
			return connectionTestMsg{pluginID: pluginID, err: err, target: target}
		}
		return connectionTestMsg{pluginID: pluginID, err: opened.Service.Close(), target: target}
	}
}
func (m Model) openConnection() (tea.Model, tea.Cmd) {
	if err := m.connection.component.Form.Validate(); err != nil {
		m.setStatus(safeText(err.Error()))
		return m, nil
	}
	m.ReadOnly = m.connection.component.Form.Values.ReadOnly
	pluginID := m.connection.component.Form.Values.Plugin
	target := m.connection.component.Form.ConnectionTarget()
	m.connectionPlugin = pluginID
	m.BeginOpening(target, "opening "+safeText(m.connection.component.Form.ConnectionName()))
	m.notifications.skipStatusPopup = true
	m.notifications.skipNotificationPersist = true
	log.Debug("opening " + safeText(m.connection.component.Form.ConnectionName()))
	return m, m.openTarget(target)
}

func (m Model) connectionActionFocused() bool {
	return m.connection.component.Form.Huh != nil && m.connection.component.Form.Huh.GetFocusedField().GetKey() == "action"
}

func (m Model) updateConnection(message tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := message.(connection.ValidationMsg); ok {
		m.connection.component.Form.FocusValidationError()
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
			// Redact credential material before it reaches the event
			// log and the notification pipeline.
			redacted := redactCredentials(test.err.Error(), m.connectionSecrets(test.target))
			log.Error("connection test", errors.New(redacted))
			m.setStatus(safeText(pluginFailureStatus(test.err, "connection test failed")))
			return m, nil
		}
		// A successful test stands in for the removed Save button: the
		// form's current credentials become the saved profile.
		m.connectionPlugin = test.pluginID
		if err := m.recordConnection(test.target); err != nil {
			m.setStatus(safeText("saving connection profile: " + err.Error()))
			return m, nil
		}
		m.setStatus("connection test succeeded: " + safeText(m.connection.component.Form.ConnectionName()))
		return m, nil
	}
	if m.connection.component.Form.Focus == connectionFocusRecent {
		// The pane's action keys only apply outside the filter input;
		// while filtering, every key goes to the input.
		if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.connection.component.RecentFilter.Focused() {
			switch {
			case m.keybindings.Match(keyPress, "connection.switch_to_form", []scope{scopeView, scopeGlobal}):
				m.connection.component.Form.Focus = connectionFocusForm
				return m, nil
			case m.keybindings.Match(keyPress, "connection.filter", []scope{scopeView, scopeGlobal}):
				return m, m.connection.component.FocusFilter()
			case m.keybindings.Match(keyPress, "connection.add", []scope{scopeView, scopeGlobal}):
				return m, m.newConnection()
			case m.keybindings.Match(keyPress, "connection.edit", []scope{scopeView, scopeGlobal}):
				return m, m.editSelectedRecentConnection()
			case m.keybindings.Match(keyPress, "connection.delete", []scope{scopeView, scopeGlobal}):
				m.confirmDeleteRecentConnection()
				return m, nil
			case m.keybindings.Match(keyPress, "connection.context_menu", []scope{scopeView, scopeGlobal}):
				if _, ok := m.selectedRecentConnection(); ok {
					m.openRecentConnectionMenu(m.layout.schemaWidth/2, m.recentRowY(m.connection.component.Recent.Index())+1)
				}
				return m, nil
			}
		}
		// The component owns the recent pane's filter editing and list
		// passthrough.
		model, _, command := m.connection.component.Update(message, uikit.Layout{Width: m.layout.schemaWidth}, m.keybindings)
		m.connection.component = model
		return m, command
	}
	if m.connection.component.Form.Confirmation != nil {
		completed, action := m.connection.component.Form.Confirmation.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		m.connection.component.Form.Confirmation = nil
		m.overlay.formMode.Mode = formModeNormal
		if action == connectionActionConnect {
			return m.openConnection()
		}
		return m, nil
	}
	keyPress, isKeyPress := message.(tea.KeyPressMsg)
	if isKeyPress && m.connection.component.Form.Confirmation == nil && m.connectionActionFocused() &&
		m.keybindings.Match(keyPress, "connection.action_enter", []scope{scopeView, scopeGlobal}) {
		m.overlay.formMode.Mode = formModeNormal
		m.connection.component.Form.Blur()
		if m.connection.component.Form.Values.Action == connectionActionTest {
			return m, m.testConnection()
		}
		return m.openConnection()
	}
	if isKeyPress && m.connection.component.Form.Confirmation == nil && m.keybindings.Match(keyPress, "connection.execute", []scope{scopeView, scopeGlobal}) {
		if err := m.connection.component.Form.Validate(); err != nil {
			m.setStatus(safeText(err.Error()))
			return m, m.connection.component.Form.ShowValidationError()
		}
		m.overlay.formMode.BeginConfirm()
		m.connection.component.Form.BeginConfirmation()
		return m, nil
	}
	if route := m.overlay.formMode.RouteHuh(message, m.connection.component.Form.Blur); route != formRouteParent {
		if route != formRouteHuh {
			return m, nil
		}
		command, event := m.connection.component.Form.UpdateHuh(message)
		switch event.(type) {
		case connection.TestRequested:
			if command != nil {
				return m, sequenceConnectionAction(command, connectionActionTest)
			}
			return m, m.testConnection()
		case connection.OpenRequested:
			if command != nil {
				return m, sequenceConnectionAction(command, connectionActionConnect)
			}
			return m.openConnection()
		case nil:
		}
		return m, command
	}
	if !isKeyPress {
		return m, nil
	}
	switch keyPress.Key().Code {
	case tea.KeyLeft, 'h', tea.KeyRight, 'l':
		if m.connection.component.Form.Huh != nil && m.connection.component.Form.Huh.GetFocusedField().GetKey() == "action" {
			model, cmd := m.connection.component.Form.Huh.Update(message)
			m.connection.component.Form.Huh = model.(*huh.Form)
			return m, cmd
		}
	}
	switch {
	case m.keybindings.Match(keyPress, "connection.switch_to_list", []scope{scopeView, scopeGlobal}):
		m.connection.component.Form.SetFocus(connectionFocusRecent)
		return m, nil
	case isInsertModeKey(keyPress), m.keybindings.Match(keyPress, "connection.edit_field", []scope{scopeView, scopeGlobal}):
		return m, m.overlay.formMode.BeginHuh(m.connection.component.Form.FocusForm())
	case m.keybindings.Match(keyPress, "connection.field_next", []scope{scopeView, scopeGlobal}):
		return m, m.connection.component.Form.Huh.NextField()
	case m.keybindings.Match(keyPress, "connection.field_prev", []scope{scopeView, scopeGlobal}):
		return m, m.connection.component.Form.Huh.PrevField()
	}
	return m, nil
}

// connectionActionWidth and connectionActionRender are the action-button
// presentation helpers, shared by the form (component) and the click
// hit-test.
func connectionActionWidth() int { return connection.ActionWidth() }

func connectionActionRender(style lipgloss.Style, label string) string {
	return connection.ActionRender(style, label)
}

func (m Model) connectionPaneView(height int) string {
	return m.connection.component.FormPaneView(height, m.modeBadge())
}

// recentPaneView renders the profiles pane through the component. The
// pane is fixed content: it always shows the profile list, never the
// form, regardless of which pane currently holds focus.
func (m Model) recentPaneView() string {
	return m.connection.component.RecentPaneView(m.recentFilterRow(), m.layout.schemaWidth)
}
