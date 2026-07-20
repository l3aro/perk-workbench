package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/go-sql-driver/mysql"
	dbmysql "github.com/l3aro/perk/internal/mysql"
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
	connectionFocusHost
	connectionFocusPort
	connectionFocusUsername
	connectionFocusPassword
	connectionFocusTest
	connectionFocusConnect
)

const connectionFocusFirstForm = connectionFocusDriver

type connectionForm struct {
	driver connectionDriver
	name   textinput.Model
	target textinput.Model
	host   textinput.Model
	port   textinput.Model
	user   textinput.Model
	pass   textinput.Model
	focus  int
}

type connectionTestMsg struct {
	err error
}

func (f *connectionForm) setFocus(index int) tea.Cmd {
	f.focus = index
	f.name.Blur()
	f.target.Blur()
	f.host.Blur()
	f.port.Blur()
	f.user.Blur()
	f.pass.Blur()
	switch index {
	case connectionFocusName:
		return f.name.Focus()
	case connectionFocusTarget:
		return f.target.Focus()
	case connectionFocusHost:
		return f.host.Focus()
	case connectionFocusPort:
		return f.port.Focus()
	case connectionFocusUsername:
		return f.user.Focus()
	case connectionFocusPassword:
		return f.pass.Focus()
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
		m.Status = "select a recent connection"
		return nil
	}
	m.connection.driver = connection.Driver
	m.connection.name.SetValue(connection.Name)
	m.connection.target.SetValue(connection.Target)
	m.Status = "editing " + safeText(connection.Name)
	return m.connection.setFocus(connectionFocusName)
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
	m.connection.driver = driverSQLite
	m.connection.name.SetValue("")
	m.connection.target.SetValue("")
	m.connection.host.SetValue("")
	m.connection.port.SetValue("3306")
	m.connection.user.SetValue("")
	m.connection.pass.SetValue("")
	m.connection.target.Placeholder = "path/to/database.db or :memory:"
	m.connection.target.Prompt = "Target: "
	m.Status = "new connection"
	return m.connection.setFocus(connectionFocusName)
}

func (f *connectionForm) update(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	switch f.focus {
	case connectionFocusName:
		f.name, command = f.name.Update(message)
	case connectionFocusTarget:
		f.target, command = f.target.Update(message)
	case connectionFocusHost:
		f.host, command = f.host.Update(message)
	case connectionFocusPort:
		f.port, command = f.port.Update(message)
	case connectionFocusUsername:
		f.user, command = f.user.Update(message)
	case connectionFocusPassword:
		f.pass, command = f.pass.Update(message)
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
	if f.driver == driverMySQL {
		config := mysql.NewConfig()
		config.User = strings.TrimSpace(f.user.Value())
		config.Passwd = f.pass.Value()
		config.Net = "tcp"
		config.Addr = net.JoinHostPort(strings.TrimSpace(f.host.Value()), strings.TrimSpace(f.port.Value()))
		config.DBName = strings.TrimSpace(f.target.Value())
		return config.FormatDSN()
	}
	return strings.TrimSpace(f.target.Value())
}

func (f connectionForm) focusOrder() []int {
	if f.driver == driverMySQL {
		return []int{connectionFocusDriver, connectionFocusName, connectionFocusHost, connectionFocusPort, connectionFocusUsername, connectionFocusPassword, connectionFocusTarget, connectionFocusTest, connectionFocusConnect}
	}
	return []int{connectionFocusDriver, connectionFocusName, connectionFocusTarget, connectionFocusTest, connectionFocusConnect}
}

func (f *connectionForm) shiftFocus(offset int) tea.Cmd {
	order := f.focusOrder()
	for index, focus := range order {
		if focus == f.focus {
			return f.setFocus(order[(index+offset+len(order))%len(order)])
		}
	}
	return f.setFocus(connectionFocusFirstForm)
}

func (f connectionForm) inputFocused() bool {
	switch f.focus {
	case connectionFocusName, connectionFocusTarget, connectionFocusHost, connectionFocusPort, connectionFocusUsername, connectionFocusPassword:
		return true
	}
	return false
}

func (f connectionForm) validateMySQL() error {
	if strings.TrimSpace(f.host.Value()) == "" {
		return errors.New("host is required")
	}
	port, err := strconv.Atoi(strings.TrimSpace(f.port.Value()))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
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
		if driver == driverMySQL {
			if err := m.connection.validateMySQL(); err != nil {
				return connectionTestMsg{err: err}
			}
		} else if target == "" {
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
		db, err := dbmysql.Open(ctx, target)
		if err != nil {
			return connectionTestMsg{err: err}
		}
		return connectionTestMsg{err: db.Close()}
	}
}

func (m Model) openConnection() (tea.Model, tea.Cmd) {
	if m.connection.driver == driverMySQL {
		if err := m.connection.validateMySQL(); err != nil {
			m.Status = safeText(err.Error())
			return m, nil
		}
	}
	if target := m.connection.targetValue(); target != "" {
		m.BeginOpening(target, "opening "+safeText(m.connection.connectionName()))
		if m.connection.driver == driverMySQL {
			return m, m.openTarget("mysql:" + target)
		}
		return m, m.openTarget(target)
	}
	m.Status = "target is required"
	return m, nil
}

func (m Model) updateConnection(message tea.Msg) (tea.Model, tea.Cmd) {
	if test, ok := message.(connectionTestMsg); ok {
		if test.err != nil {
			m.Status = "connection test failed"
		} else {
			m.Status = "connection test succeeded: " + safeText(m.connection.connectionName())
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
		return m, m.connection.shiftFocus(1)
	case "shift+tab":
		return m, m.connection.shiftFocus(-1)
	case "left", "right", "h", "l":
		if m.connection.focus == connectionFocusDriver {
			m.connection.driver = 1 - m.connection.driver
			if m.connection.driver == driverMySQL {
				m.connection.target.Prompt = "Database: "
				m.connection.target.Placeholder = "database"
				m.connection.port.SetValue("3306")
			} else {
				m.connection.target.Prompt = "Target: "
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
	fields := []string{
		driver,
		m.connection.name.View(),
	}
	if m.connection.driver == driverMySQL {
		fields = append(fields, m.connection.host.View(), m.connection.port.View(), m.connection.user.View(), m.connection.pass.View())
	}
	fields = append(fields, m.connection.target.View(), strings.Join([]string{testButton, connectButton}, " "), statusStyle.Render("Tab to a control | Enter activates it"))
	return strings.Join(fields, "\n")
}
