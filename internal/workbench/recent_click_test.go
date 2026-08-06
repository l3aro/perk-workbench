package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// recentClickModel builds a connection screen with two SQLite profiles.
func recentClickModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.recentConnections = []recentConnection{
		{Driver: driverSQLite, Name: "alpha", Target: "/tmp/alpha.db"},
		{Driver: driverSQLite, Name: "beta", Target: "/tmp/beta.db"},
	}
	if err := model.recent.SetItems(recentListItems(model.recentConnections)); err != nil {
		t.Fatal(err)
	}
	return resizeModel(model, 100, 24)
}

// renderedItemY returns the content line of the first rendered line that
// contains the given profile name, or -1.
func renderedItemY(t *testing.T, model Model, name string) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	for y, line := range lines {
		if strings.Contains(line, name) {
			return y
		}
	}
	return -1
}

// TestRecentClick_selectsRenderedProfile: a single click on a recent
// connection selects it (the list's keyboard cursor moves to it).
func TestRecentClick_selectsRenderedProfile(t *testing.T) {
	model := recentClickModel(t)

	itemY := renderedItemY(t, model, "beta")
	if itemY < 0 {
		t.Fatal("rendered profiles do not contain beta")
	}

	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: itemY, Button: tea.MouseLeft})
	model = updated.(Model)
	selected, ok := model.recent.SelectedItem().(recentConnection)
	if !ok || selected.Name != "beta" {
		t.Fatalf("selected profile = %#v, want beta", selected)
	}
}

// TestRecentClick_doubleClickLoadsProfileIntoForm: a double-click loads the
// profile into the connection form, matching the Enter keybinding.
func TestRecentClick_doubleClickLoadsProfileIntoForm(t *testing.T) {
	model := recentClickModel(t)

	itemY := renderedItemY(t, model, "beta")
	if itemY < 0 {
		t.Fatal("rendered profiles do not contain beta")
	}

	for range 2 {
		updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: itemY, Button: tea.MouseLeft})
		model = updated.(Model)
	}
	if model.connection.focus != connectionFocusForm {
		t.Fatalf("connection focus = %v, want form after double click", model.connection.focus)
	}
	if model.connection.values.name != "beta" || model.connection.values.target != "/tmp/beta.db" {
		t.Fatalf("form = %q %q, want beta /tmp/beta.db", model.connection.values.name, model.connection.values.target)
	}
}
