package workbench

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	_ "github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk/internal/sqlite"
)

type connectionDriver int

const (
	driverSQLite connectionDriver = iota
	driverMySQL
)

const (
	connectionFocusRecent = iota
	connectionFocusDriver
	connectionFocusName
	connectionFocusTarget
	connectionFocusTest
	connectionFocusConnect
	connectionFocusCount
)

const connectionFocusFirstForm = connectionFocusDriver

type connectionForm struct {
	driver connectionDriver
	name   textinput.Model
	target textinput.Model
	focus  int
}

type connectionTestMsg struct {
	err error
}

func (f *connectionForm) setFocus(index int) tea.Cmd {
	f.focus = index
	f.name.Blur()
	f.target.Blur()
	switch index {
	case connectionFocusName:
		return f.name.Focus()
	case connectionFocusTarget:
		return f.target.Focus()
	}
	return nil
}

func (m *Model) setRecentConnections(connections []recentConnection) {
	m.recentConnections = connections
	_ = m.recent.SetItems(recentListItems(connections))
	if m.recentPath != "" {
		_ = saveRecentConnections(m.recentPath, connections)
	}
}

func (m *Model) recordConnection() {
	connection := recentConnection{
		Driver: m.connection.driver,
		Name:   m.connection.connectionName(),
		Target: m.connection.targetValue(),
	}
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
		m.status = "select a recent connection"
		return nil
	}
	m.connection.driver = connection.Driver
	m.connection.name.SetValue(connection.Name)
	m.connection.target.SetValue(connection.Target)
	m.status = "editing " + safeText(connection.Name)
	return m.connection.setFocus(connectionFocusName)
}

func (m *Model) deleteSelectedRecentConnection() {
	connection, ok := m.selectedRecentConnection()
	if !ok {
		m.status = "select a recent connection"
		return
	}
	connections := make([]recentConnection, 0, len(m.recentConnections)-1)
	for _, existing := range m.recentConnections {
		if existing.Driver != connection.Driver || existing.Target != connection.Target {
			connections = append(connections, existing)
		}
	}
	m.setRecentConnections(connections)
	m.status = "deleted " + safeText(connection.Name)
}

func (m *Model) newConnection() tea.Cmd {
	m.connection.driver = driverSQLite
	m.connection.name.SetValue("")
	m.connection.target.SetValue("")
	m.connection.target.Placeholder = "path/to/database.db or :memory:"
	m.status = "new connection"
	return m.connection.setFocus(connectionFocusName)
}

func (f *connectionForm) update(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	switch f.focus {
	case connectionFocusName:
		f.name, command = f.name.Update(message)
	case connectionFocusTarget:
		f.target, command = f.target.Update(message)
	}
	return command
}

func (f connectionForm) driverName() string {
	if f.driver == driverMySQL {
		return "MySQL"
	}
	return "SQLite"
}

func (f connectionForm) targetValue() string {
	return strings.TrimSpace(f.target.Value())
}

func (f connectionForm) connectionName() string {
	if name := strings.TrimSpace(f.name.Value()); name != "" {
		return name
	}
	return f.driverName()
}

func (m Model) testConnection() tea.Cmd {
	driver, target := m.connection.driver, m.connection.targetValue()
	return func() tea.Msg {
		if target == "" {
			return connectionTestMsg{err: errors.New("target is required")}
		}
		ctx, cancel := context.WithTimeout(m.appContext, 5*time.Second)
		defer cancel()
		if driver == driverSQLite {
			service, err := sqlite.Open(ctx, target)
			if err != nil {
				return connectionTestMsg{err: err}
			}
			return connectionTestMsg{err: service.Close()}
		}
		db, err := sql.Open("mysql", target)
		if err != nil {
			return connectionTestMsg{err: err}
		}
		defer db.Close()
		return connectionTestMsg{err: db.PingContext(ctx)}
	}
}

func (m Model) openConnection() (tea.Model, tea.Cmd) {
	if m.connection.driver != driverSQLite {
		m.status = "MySQL connections can be tested; workspace support is SQLite-only"
		return m, nil
	}
	if target := m.connection.targetValue(); target != "" {
		m.target, m.state = target, stateOpening
		m.status = "opening " + safeText(m.connection.connectionName())
		return m, m.openTarget(target)
	}
	m.status = "target is required"
	return m, nil
}

func (m Model) updateConnection(message tea.Msg) (tea.Model, tea.Cmd) {
	if test, ok := message.(connectionTestMsg); ok {
		if test.err != nil {
			m.status = "connection test failed"
		} else {
			m.status = "connection test succeeded: " + safeText(m.connection.connectionName())
		}
		return m, nil
	}

	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, m.connection.update(message)
	}
	switch keyPress.String() {
	case "1":
		return m, m.connection.setFocus(connectionFocusRecent)
	case "2":
		return m, m.connection.setFocus(connectionFocusName)
	}
	if m.connection.focus == connectionFocusRecent {
		switch keyPress.String() {
		case "a":
			return m, m.newConnection()
		case "e", "enter":
			return m, m.editSelectedRecentConnection()
		case "d":
			m.deleteSelectedRecentConnection()
			return m, nil
		}
		var command tea.Cmd
		m.recent, command = m.recent.Update(message)
		return m, command
	}
	switch keyPress.String() {
	case "tab":
		return m, m.connection.setFocus(connectionFocusFirstForm + (m.connection.focus-connectionFocusFirstForm+1)%(connectionFocusCount-connectionFocusFirstForm))
	case "shift+tab":
		return m, m.connection.setFocus(connectionFocusFirstForm + (m.connection.focus-connectionFocusFirstForm+connectionFocusCount-connectionFocusFirstForm-1)%(connectionFocusCount-connectionFocusFirstForm))
	case "left", "right":
		if m.connection.focus == connectionFocusDriver {
			m.connection.driver = 1 - m.connection.driver
			if m.connection.driver == driverMySQL {
				m.connection.target.Placeholder = "user:password@tcp(host:3306)/database"
			} else {
				m.connection.target.Placeholder = "path/to/database.db or :memory:"
			}
			return m, nil
		}
	case "enter":
		switch m.connection.focus {
		case connectionFocusTest:
			return m, m.testConnection()
		case connectionFocusConnect:
			return m.openConnection()
		}
	case "ctrl+enter", "f5":
		return m.openConnection()
	}
	return m, m.connection.update(message)
}

func (m Model) connectionView() string {
	driver := fmt.Sprintf("Driver: <%s>", m.connection.driverName())
	if m.connection.focus == connectionFocusDriver {
		driver = headerStyle.Render(driver)
	}
	testButton := "[ Test ]"
	if m.connection.focus == connectionFocusTest {
		testButton = headerStyle.Render(testButton)
	}
	connectButton := "[ Connect ]"
	if m.connection.focus == connectionFocusConnect {
		connectButton = headerStyle.Render(connectButton)
	}
	return strings.Join([]string{
		driver,
		m.connection.name.View(),
		m.connection.target.View(),
		strings.Join([]string{testButton, connectButton}, " "),
		statusStyle.Render("Tab to a control | Enter activates it"),
	}, "\n")
}
