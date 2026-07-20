package workbench

import (
	"context"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRecentConnections_persistsSQLiteOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk", "recent.json")
	connections := []recentConnection{
		{Driver: driverSQLite, Name: "Local", Target: "/tmp/local.db"},
		{Driver: driverMySQL, Name: "Remote", Target: "user:password@tcp(host:3306)/app"},
	}

	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving recent connections: %v", err)
	}

	loaded := loadRecentConnections(path)
	if len(loaded) != 1 {
		t.Fatalf("loaded connections = %d, want 1", len(loaded))
	}
	if loaded[0] != connections[0] {
		t.Fatalf("loaded connection = %#v, want %#v", loaded[0], connections[0])
	}
}

func TestConnectionForm_recentConnectionActions(t *testing.T) {
	model := New("", Open(context.Background()))
	model.recentPath = filepath.Join(t.TempDir(), "recent.json")
	model.setRecentConnections([]recentConnection{
		{Driver: driverSQLite, Name: "Alpha", Target: "/tmp/alpha.db"},
		{Driver: driverSQLite, Name: "Beta", Target: "/tmp/beta.db"},
	})
	model.connection.setFocus(connectionFocusRecent)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if !model.recent.SettingFilter() {
		t.Fatal("recent list should enter filter mode")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.connection.focus != connectionFocusName {
		t.Fatalf("connection focus = %d, want name", model.connection.focus)
	}
	if model.connection.name.Value() != "Alpha" || model.connection.target.Value() != "/tmp/alpha.db" {
		t.Fatalf("connection form = %q %q, want Alpha /tmp/alpha.db", model.connection.name.Value(), model.connection.target.Value())
	}

	model.connection.setFocus(connectionFocusRecent)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if len(model.recentConnections) != 1 || model.recentConnections[0].Name != "Beta" {
		t.Fatalf("recent connections = %#v, want only Beta", model.recentConnections)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.connection.focus != connectionFocusName || model.connection.name.Value() != "" || model.connection.target.Value() != "" {
		t.Fatalf("new connection form = focus %d, name %q, target %q", model.connection.focus, model.connection.name.Value(), model.connection.target.Value())
	}
}

func TestConnectionForm_paneKeysKeepTabInTheForm(t *testing.T) {
	model := New("", Open(context.Background()))

	updated, _ := model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)
	if model.connection.focus != connectionFocusRecent {
		t.Fatalf("connection focus = %d, want recent", model.connection.focus)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	model = updated.(Model)
	if model.connection.focus != connectionFocusName {
		t.Fatalf("connection focus = %d, want name", model.connection.focus)
	}

	for range 4 {
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		model = updated.(Model)
	}
	if model.connection.focus != connectionFocusDriver {
		t.Fatalf("connection focus = %d, want driver", model.connection.focus)
	}
}
