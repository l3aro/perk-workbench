package workbench

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestConnectionForm_testsSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	message := model.testConnection()()
	updated, _ := model.Update(message)
	model = updated.(Model)

	if model.status != "connection test succeeded: Scratch" {
		t.Fatalf("connection status = %q, want successful test", model.status)
	}
}

func TestConnectionForm_opensSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	updated, command := model.openConnection()
	model = updated.(Model)
	if model.state != stateOpening {
		t.Fatalf("model state = %v, want opening", model.state)
	}
	if command == nil {
		t.Fatal("open connection command = nil")
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.state != stateReady {
		t.Fatalf("model state = %v, want ready", model.state)
	}
	if model.service == nil {
		t.Fatal("model service = nil, want opened service")
	}
	if model.status != "ready: Scratch" {
		t.Fatalf("connection status = %q, want connection name", model.status)
	}
	t.Cleanup(func() {
		if err := model.service.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}

func TestConnectionForm_F5OpensSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.target.SetValue(":memory:")

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	if model.state != stateOpening {
		t.Fatalf("model state = %v, want opening", model.state)
	}
	if command == nil {
		t.Fatal("open connection command = nil")
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.state != stateReady {
		t.Fatalf("model state = %v, want ready", model.state)
	}
	t.Cleanup(func() {
		if err := model.service.Close(); err != nil {
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
	if model.status != "connection test succeeded: Scratch" {
		t.Fatalf("connection status = %q, want successful test", model.status)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if model.connection.focus != connectionFocusConnect {
		t.Fatalf("connection focus = %d, want connect", model.connection.focus)
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.state != stateOpening {
		t.Fatalf("model state = %v, want opening", model.state)
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.state != stateReady {
		t.Fatalf("model state = %v, want ready", model.state)
	}
	t.Cleanup(func() {
		if err := model.service.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}
