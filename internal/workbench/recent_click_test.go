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
	model.connection.recentConnections = []recentConnection{
		{Driver: driverSQLite, Name: "alpha", Target: "/tmp/alpha.db"},
		{Driver: driverSQLite, Name: "beta", Target: "/tmp/beta.db"},
	}
	if err := model.connection.recent.SetItems(recentListItems(model.connection.recentConnections)); err != nil {
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
	selected, ok := model.connection.recent.SelectedItem().(recentConnection)
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
	if model.connection.form.focus != connectionFocusForm {
		t.Fatalf("connection focus = %v, want form after double click", model.connection.form.focus)
	}
	if model.connection.form.values.name != "beta" || model.connection.form.values.target != "/tmp/beta.db" {
		t.Fatalf("form = %q %q, want beta /tmp/beta.db", model.connection.form.values.name, model.connection.form.values.target)
	}
}

// TestConnectionForm_contextMenuEditAndDelete: comma opens the profile
// context menu with Edit/Delete on the selected profile; e loads it into the
// form, d opens the delete confirmation.
func TestConnectionForm_contextMenuEditAndDelete(t *testing.T) {
	model := recentClickModel(t)
	model.connection.form.setFocus(connectionFocusRecent)

	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	menu := model.overlay.contextMenu
	if menu == nil || !menu.visible {
		t.Fatal("comma did not open the profile context menu")
	}
	if got, want := len(menu.options), 2; got != want {
		t.Fatalf("context menu options = %d, want %d", got, want)
	}
	if menu.options[0].action != "edit_profile" || menu.options[0].keys != "e" ||
		menu.options[1].action != "delete_profile" || menu.options[1].keys != "d" {
		t.Fatalf("profile menu options = %+v, want Edit e / Delete d", menu.options)
	}

	// e edits the selected profile (alpha) into the connection form.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatal("context menu stayed open after e")
	}
	if model.connection.form.focus != connectionFocusForm || model.connection.form.values.name != "alpha" || model.connection.form.values.target != "/tmp/alpha.db" {
		t.Fatalf("edit did not load alpha: focus=%d values=%q %q", model.connection.form.focus, model.connection.form.values.name, model.connection.form.values.target)
	}

	// Comma again, then d closes the menu and opens the confirmation.
	model.connection.form.setFocus(connectionFocusRecent)
	updated, _ = model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	if model.overlay.contextMenu == nil {
		t.Fatal("comma did not reopen the profile context menu")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.overlay.contextMenu != nil || model.overlay.deleteConfirm == nil {
		t.Fatal("d did not close the menu and open the delete confirmation")
	}
	if len(model.connection.recentConnections) != 2 {
		t.Fatalf("d deleted before confirmation: %#v", model.connection.recentConnections)
	}

	// Esc closes the reopened menu without acting.
	updated, _ = model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatal("esc did not close the profile context menu")
	}
}

// TestRecentClick_rightClickOpensMenuOnProfile: a right-click on a rendered
// profile selects it and opens the context menu, so the menu acts on the
// clicked profile even when another one was selected.
func TestRecentClick_rightClickOpensMenuOnProfile(t *testing.T) {
	model := recentClickModel(t)
	model.connection.form.setFocus(connectionFocusRecent)

	itemY := renderedItemY(t, model, "beta")
	if itemY < 0 {
		t.Fatal("rendered profiles do not contain beta")
	}

	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: itemY, Button: tea.MouseRight})
	model = updated.(Model)
	if model.overlay.contextMenu == nil || !model.overlay.contextMenu.visible {
		t.Fatal("right-click did not open the profile context menu")
	}
	selected, ok := model.connection.recent.SelectedItem().(recentConnection)
	if !ok || selected.Name != "beta" {
		t.Fatalf("right-click selected %#v, want beta", selected)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.connection.form.focus != connectionFocusForm || model.connection.form.values.name != "beta" || model.connection.form.values.target != "/tmp/beta.db" {
		t.Fatalf("edit did not load beta: focus=%d values=%q %q", model.connection.form.focus, model.connection.form.values.name, model.connection.form.values.target)
	}
}

// TestRecentClick_rightClickFilterBoxIgnored: right-clicks on the filter
// input row do not open a profile menu.
func TestRecentClick_rightClickFilterBoxIgnored(t *testing.T) {
	model := recentClickModel(t)
	model.connection.form.setFocus(connectionFocusRecent)

	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: 3, Button: tea.MouseRight})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatal("right-click on the filter row opened a profile menu")
	}
}

// TestRecentClick_rightClickCompactFormFocusedIgnored: in the compact
// connection layout, right-clicks are ignored while the form pane is
// focused.
func TestRecentClick_rightClickCompactFormFocusedIgnored(t *testing.T) {
	model := resizeModel(recentClickModel(t), 60, 20)
	model.connection.form.setFocus(connectionFocusForm)

	updated, _ := model.Update(tea.MouseClickMsg{X: 30, Y: 10, Button: tea.MouseRight})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatal("right-click while the form is focused opened a profile menu")
	}

	// Focus the profiles pane: the same right-click opens the menu.
	model.connection.form.setFocus(connectionFocusRecent)
	updated, _ = model.Update(tea.MouseClickMsg{X: 30, Y: 10, Button: tea.MouseRight})
	model = updated.(Model)
	if model.overlay.contextMenu == nil {
		t.Fatal("right-click on the compact profiles pane did not open a menu")
	}
}
