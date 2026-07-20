package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/go-sql-driver/mysql"
)

func TestConnectionForm_buildsMySQLDSNFromSeparateFields(t *testing.T) {
	form := newConnectionForm()
	form.driver = driverMySQL
	form.host.SetValue("2001:db8::1")
	form.port.SetValue("3307")
	form.user.SetValue("alice")
	form.pass.SetValue("secret")
	form.target.SetValue("app")

	dsn, err := mysql.ParseDSN(form.targetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.User != "alice" || dsn.Passwd != "secret" || dsn.Addr != "[2001:db8::1]:3307" || dsn.DBName != "app" {
		t.Fatalf("MySQL DSN = %#v, want separate field values", dsn)
	}
}

func TestConnectionForm_showsMySQLControls(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.setFocus(connectionFocusDriver)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	view := model.connectionView()
	for _, label := range []string{"Host:", "Port:", "Username:", "Password:", "Database:"} {
		if !strings.Contains(view, label) {
			t.Fatalf("MySQL connection view = %q, missing %q", view, label)
		}
	}

	for _, want := range []int{connectionFocusName, connectionFocusHost, connectionFocusPort, connectionFocusUsername, connectionFocusPassword, connectionFocusTarget, connectionFocusTest, connectionFocusConnect} {
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		model = updated.(Model)
		if model.connection.focus != want {
			t.Fatalf("connection focus = %d, want %d", model.connection.focus, want)
		}
	}
}

func TestConnectionForm_driverSwitchesWithDirectionalKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyLeft},
		{Code: tea.KeyRight},
		{Code: 'h', Text: "h"},
		{Code: 'l', Text: "l"},
	} {
		model := New("", Open(context.Background()))
		model.connection.setFocus(connectionFocusDriver)

		updated, _ := model.Update(key)
		model = updated.(Model)
		if model.connection.driver != driverMySQL {
			t.Fatalf("driver after %q = %d, want MySQL", key.String(), model.connection.driver)
		}
	}
}

func TestConnectionForm_allowsQInMySQLHost(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.driver = driverMySQL
	model.connection.setFocus(connectionFocusHost)

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)
	if model.connection.host.Value() != "q" {
		t.Fatalf("host = %q, want q", model.connection.host.Value())
	}
}

func TestConnectionForm_testsSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	message := model.testConnection()()
	updated, _ := model.Update(message)
	model = updated.(Model)

	if model.Status != "connection test succeeded: Scratch" {
		t.Fatalf("connection status = %q, want successful test", model.Status)
	}
}

func TestConnectionForm_opensSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	updated, command := model.openConnection()
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("model state = %v, want opening", model.State)
	}
	if command == nil {
		t.Fatal("open connection command = nil")
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.State != stateReady {
		t.Fatalf("model state = %v, want ready", model.State)
	}
	if model.Database == nil {
		t.Fatal("model service = nil, want opened service")
	}
	if model.Status != "ready: Scratch" {
		t.Fatalf("connection status = %q, want connection name", model.Status)
	}
	t.Cleanup(func() {
		if err := model.Database.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}

func TestConnectionForm_opensMySQLConnection(t *testing.T) {
	var openedTarget string
	model := New("", databaseOpener{
		ctx: context.Background(),
		command: func(target string) tea.Cmd {
			openedTarget = target
			return nil
		},
	})
	model.connection.driver = driverMySQL
	model.connection.host.SetValue("localhost")
	model.connection.port.SetValue("3306")
	model.connection.target.SetValue("app")

	updated, command := model.openConnection()
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("model state = %v, want opening", model.State)
	}
	if command != nil {
		t.Fatal("open command = non-nil, want nil from test opener")
	}
	if !strings.HasPrefix(openedTarget, "mysql:") {
		t.Fatalf("opened target = %q, want mysql DSN prefix", openedTarget)
	}
}

func TestConnectionForm_F5OpensSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.target.SetValue(":memory:")

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("model state = %v, want opening", model.State)
	}
	if command == nil {
		t.Fatal("open connection command = nil")
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.State != stateReady {
		t.Fatalf("model state = %v, want ready", model.State)
	}
	t.Cleanup(func() {
		if err := model.Database.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}

func TestConnectionForm_buttonsTestAndOpenSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if model.connection.focus != connectionFocusTarget {
		t.Fatalf("connection focus = %d, want target", model.connection.focus)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if model.connection.focus != connectionFocusTest {
		t.Fatalf("connection focus = %d, want test", model.connection.focus)
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.Status != "connection test succeeded: Scratch" {
		t.Fatalf("connection status = %q, want successful test", model.Status)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if model.connection.focus != connectionFocusConnect {
		t.Fatalf("connection focus = %d, want connect", model.connection.focus)
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("model state = %v, want opening", model.State)
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.State != stateReady {
		t.Fatalf("model state = %v, want ready", model.State)
	}
	t.Cleanup(func() {
		if err := model.Database.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}
