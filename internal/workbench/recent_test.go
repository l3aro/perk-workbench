package workbench

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
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
	if model.connection.focus != connectionFocusForm {
		t.Fatalf("connection focus = %d, want form", model.connection.focus)
	}
	if model.connection.values.name != "Alpha" || model.connection.values.target != "/tmp/alpha.db" {
		t.Fatalf("connection form = %q %q, want Alpha /tmp/alpha.db", model.connection.values.name, model.connection.values.target)
	}

	model.connection.setFocus(connectionFocusRecent)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if len(model.recentConnections) != 1 || model.recentConnections[0].Name != "Beta" {
		t.Fatalf("recent connections = %#v, want only Beta", model.recentConnections)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.connection.focus != connectionFocusForm || model.connection.values.name != "" || model.connection.values.target != "" {
		t.Fatalf("new connection form = focus %d, name %q, target %q", model.connection.focus, model.connection.values.name, model.connection.values.target)
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
	if model.connection.focus != connectionFocusForm {
		t.Fatalf("connection focus = %d, want form", model.connection.focus)
	}
}

func TestConnectionForm_recentAddAndEditInitializeUsableHuhForms(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "add", key: tea.KeyPressMsg{Code: 'a', Text: "a"}},
		{name: "edit", key: tea.KeyPressMsg{Code: 'e', Text: "e"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.recentConnections = []recentConnection{{Driver: driverSQLite, Name: "Alpha", Target: ":memory:"}}
			_ = model.recent.SetItems(recentListItems(model.recentConnections))
			model.connection.setFocus(connectionFocusRecent)

			// When
			updated, command := model.Update(test.key)
			model = updated.(Model)
			if command == nil {
				t.Fatal("recent route returned no Huh initialization command")
			}
			message := command()
			if _, ok := message.(tea.BatchMsg); ok {
				t.Fatal("recent route returned redundant focus batching instead of the Huh init sequence")
			}
			model = resolveConnectionMessage(model, message, 16)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
			model = updated.(Model)
			updated, command = model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
			model = updated.(Model)
			model = resolveConnectionCommand(model, command)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			model = updated.(Model)

			// Then
			if !strings.Contains(model.connection.View(), "Target*") {
				t.Fatalf("connection form after %s = %q, want rendered Target* control", test.name, model.connection.View())
			}
			if !strings.Contains(model.connection.values.name, "x") {
				t.Fatalf("connection name after %s = %q, want Huh input to accept text", test.name, model.connection.values.name)
			}
		})
	}
}

func resolveConnectionCommand(model Model, command tea.Cmd) Model {
	if command == nil {
		return model
	}
	return resolveConnectionMessage(model, command(), 16)
}

func resolveConnectionMessage(model Model, message tea.Msg, remaining int) Model {
	if remaining == 0 {
		return model
	}
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, next := range batch {
			model = resolveConnectionCommand(model, next)
		}
		return model
	}
	value := reflect.ValueOf(message)
	commandType := reflect.TypeFor[tea.Cmd]()
	if value.Kind() == reflect.Slice && value.Type().Elem() == commandType {
		for index := range value.Len() {
			model = resolveConnectionCommand(model, value.Index(index).Interface().(tea.Cmd))
		}
		return model
	}
	updated, command := model.Update(message)
	if command == nil {
		return updated.(Model)
	}
	return resolveConnectionMessage(updated.(Model), command(), remaining-1)
}
