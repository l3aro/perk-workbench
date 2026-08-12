package workbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestConnectionProfiles_persistUnnamedSQLiteTargets(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connection.form.values.driver, model.connection.form.values.name = driverSQLite, ""
	model.connection.form.values.target = "/tmp/alpha.db"
	model.recordConnection("")
	model.connection.form.values.target = "/tmp/beta.db"

	// When
	model.recordConnection("")
	loaded, _ := profile.Load(model.connection.recentPath)

	// Then
	if len(loaded) != 2 {
		t.Fatalf("loaded SQLite profiles = %#v, want two distinct targets", loaded)
	}
	if loaded[0].Target != "/tmp/beta.db" || loaded[1].Target != "/tmp/alpha.db" {
		t.Fatalf("loaded SQLite targets = %#v, want beta then alpha", loaded)
	}
}

func TestNew_targetInitializesRecentConnectionPersistence(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// When
	model := New("/tmp/chinook.db", context.Background(), testOpen, false)

	// Then
	if model.connection.recentPath == "" {
		t.Fatal("target startup did not initialize recent connection persistence")
	}
}

func TestConnectionProfiles_persistRemoteFieldsWithoutPassword(t *testing.T) {
	// Given
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "connections.json")
	model := New("", context.Background(), testOpen, false)
	model.connection.form.values.driver, model.connection.form.values.name = driverPostgreSQL, "Reporting"
	model.connection.form.values.target, model.connection.form.values.host = "analytics", "db.example.test"
	model.connection.form.values.port, model.connection.form.values.user, model.connection.form.values.pass = "5432", "analyst", "secret"
	model.recordConnection("")

	// When
	if err := profile.Save(path, model.connection.recentConnections); err != nil {
		t.Fatalf("saving connection profiles: %v", err)
	}
	loaded, _ := profile.Load(path)

	// Then — verify password is encrypted (not plaintext) in the JSON file
	var stored []struct{ Pass string }
	contents, _ := os.ReadFile(path)
	json.Unmarshal(contents, &stored)
	if len(stored) != 1 || stored[0].Pass == "secret" {
		t.Fatalf("password stored in plaintext")
	}
	if !strings.HasPrefix(stored[0].Pass, "enc:") {
		t.Fatalf("password not encrypted, prefix=%q", stored[0].Pass[:min(5, len(stored[0].Pass))])
	}
	if !reflect.DeepEqual(loaded, model.connection.recentConnections) {
		t.Fatalf("loaded profiles = %#v, want %#v", loaded, model.connection.recentConnections)
	}
}

func TestConnectionForm_recentConnectionActions(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.recentPath = filepath.Join(t.TempDir(), "connections.json")
	model.setRecentConnections([]profile.Profile{
		{Driver: driverSQLite, Name: "Alpha", Target: "/tmp/alpha.db"},
		{Driver: driverSQLite, Name: "Beta", Target: "/tmp/beta.db"},
	})
	model.connection.form.setFocus(connectionFocusRecent)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if !model.connection.recentFilter.Focused() {
		t.Fatal("recent filter input should be focused")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.connection.form.focus != connectionFocusForm {
		t.Fatalf("connection focus = %d, want form", model.connection.form.focus)
	}
	if model.connection.form.values.name != "Alpha" || model.connection.form.values.target != "/tmp/alpha.db" {
		t.Fatalf("connection form = %q %q, want Alpha /tmp/alpha.db", model.connection.form.values.name, model.connection.form.values.target)
	}

	model.connection.form.setFocus(connectionFocusRecent)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.overlay.deleteConfirm == nil {
		t.Fatal("d did not open the delete confirmation")
	}
	if len(model.connection.recentConnections) != 2 {
		t.Fatalf("d deleted before confirmation: %#v", model.connection.recentConnections)
	}

	// Decline: nothing is deleted.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if model.overlay.deleteConfirm != nil || len(model.connection.recentConnections) != 2 {
		t.Fatalf("decline changed connections: %#v", model.connection.recentConnections)
	}

	// Confirm: Alpha is removed, Beta stays.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.overlay.deleteConfirm == nil {
		t.Fatal("d did not reopen the delete confirmation")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if len(model.connection.recentConnections) != 1 || model.connection.recentConnections[0].Name != "Beta" {
		t.Fatalf("recent connections = %#v, want only Beta", model.connection.recentConnections)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.connection.form.focus != connectionFocusForm || model.connection.form.values.name != "" || model.connection.form.values.target != "" {
		t.Fatalf("new connection form = focus %d, name %q, target %q", model.connection.form.focus, model.connection.form.values.name, model.connection.form.values.target)
	}
}

// TestConnectionForm_recentFilterInput guards the visible filter input that
// replaces the list's built-in filter: typing narrows the list live, and
// enter/escape exit editing while keeping the applied filter.
func TestConnectionForm_recentFilterInput(t *testing.T) {
	model := recentClickModel(t)
	model.connection.form.setFocus(connectionFocusRecent)

	// The filter row renders at the top of the profiles pane.
	if !strings.Contains(ansi.Strip(model.View().Content), "filter") {
		t.Fatal("profiles pane does not render the filter input row")
	}

	// Enter filter editing, then type: only beta matches.
	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if !model.connection.recentFilter.Focused() {
		t.Fatal("/ did not focus the recent filter input")
	}

	// Clipboard paste while editing lands in the input, not the list.
	updated, _ = model.Update(tea.PasteMsg{Content: "be"})
	model = updated.(Model)
	if got := model.connection.recentFilter.Value(); got != "be" {
		t.Fatalf("filter value after paste = %q, want be", got)
	}
	if !model.connection.recent.IsFiltered() {
		t.Fatal("paste did not filter the recent list")
	}
	visible := model.connection.recent.VisibleItems()
	if len(visible) != 1 || visible[0].(recentProfile).Profile.Name != "beta" {
		t.Fatalf("visible profiles = %#v, want only beta", visible)
	}

	// Typing extends the live filter.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	model = updated.(Model)
	if got := model.connection.recentFilter.Value(); got != "bet" {
		t.Fatalf("filter value after typing = %q, want bet", got)
	}
	if len(model.connection.recent.VisibleItems()) != 1 {
		t.Fatal("typing dropped the applied filter")
	}

	// Enter exits editing, keeping the filter and the typed query.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.connection.recentFilter.Focused() || model.connection.recentFilter.Value() != "bet" {
		t.Fatalf("after enter: focused=%t value=%q, want inactive/bet", model.connection.recentFilter.Focused(), model.connection.recentFilter.Value())
	}
	if !model.connection.recent.IsFiltered() || len(model.connection.recent.VisibleItems()) != 1 {
		t.Fatal("enter dropped the applied filter")
	}

	// Escape exits editing too; editing keys work again after that.
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.connection.recentFilter.Focused() {
		t.Fatal("escape left the recent filter input focused")
	}
	if got := model.connection.recentFilter.Value(); got != "bet" {
		t.Fatalf("filter value = %q, want preserved bet", got)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.connection.form.focus != connectionFocusForm {
		t.Fatalf("connection focus = %d, want form after filter exit", model.connection.form.focus)
	}

	// Clearing the input restores every profile.
	model.connection.form.setFocus(connectionFocusRecent)
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	for range "bet" {
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = updated.(Model)
	}
	if model.connection.recent.IsFiltered() || len(model.connection.recent.VisibleItems()) != 2 {
		t.Fatalf("after clearing: filtered=%t visible=%d, want all 2 profiles", model.connection.recent.IsFiltered(), len(model.connection.recent.VisibleItems()))
	}
}

// TestRecentClick_filterRowFocusesInput guards the click handling of the
// visible filter row: a click on the box focuses the input, a click on a
// profile leaves filter editing and selects the profile.
func TestRecentClick_filterRowFocusesInput(t *testing.T) {
	model := recentClickModel(t)

	// The filter box occupies screen rows 2-4 (pane top border at y=1).
	updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: 3, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.connection.recentFilter.Focused() {
		t.Fatal("click on the filter row did not focus the input")
	}

	itemY := renderedItemY(t, model, "beta")
	if itemY < 0 {
		t.Fatal("rendered profiles do not contain beta")
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: 2, Y: itemY, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.recentFilter.Focused() {
		t.Fatal("click on a profile left the filter input focused")
	}
	selected, ok := model.selectedRecentConnection()
	if !ok || selected.Name != "beta" {
		t.Fatalf("selected profile = %#v, want beta", selected)
	}
}

func TestConnectionForm_editWithEmptyPassword(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
	}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.values.pass = "previous-password"
	model.connection.form.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.form.values
	if values.driver != driverMySQL || values.name != "Production" || values.host != "db.example.test" || values.port != "3307" || values.user != "alice" || values.target != "app" {
		t.Fatalf("connection form = %#v, want selected profile fields", values)
	}
	if values.pass != "" {
		t.Fatalf("selected profile password = %q, want empty (no Pass in profile)", values.pass)
	}
}

func TestConnectionForm_editWithEmptyPassword_merged_2(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
	}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.values.pass = "previous-password"
	model.connection.form.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.form.values
	if values.driver != driverMySQL || values.name != "Production" || values.host != "db.example.test" || values.port != "3307" || values.user != "alice" || values.target != "app" {
		t.Fatalf("connection form = %#v, want selected profile fields", values)
	}
	if values.pass != "" {
		t.Fatalf("selected profile password = %q, want empty (no Pass in profile)", values.pass)
	}
}

func TestConnectionForm_editWithEmptyPassword_merged_3(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
	}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.values.pass = "previous-password"
	model.connection.form.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.form.values
	if values.driver != driverMySQL || values.name != "Production" || values.host != "db.example.test" || values.port != "3307" || values.user != "alice" || values.target != "app" {
		t.Fatalf("connection form = %#v, want selected profile fields", values)
	}
	if values.pass != "" {
		t.Fatalf("selected profile password = %q, want empty (no Pass in profile)", values.pass)
	}
}

func TestConnectionForm_editPopulatesStoredPassword(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
		Pass:   "stored-pass",
	}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.values.pass = "previous-password"
	model.connection.form.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.form.values
	if values.driver != driverMySQL || values.name != "Production" || values.host != "db.example.test" || values.port != "3307" || values.user != "alice" || values.target != "app" {
		t.Fatalf("connection form = %#v, want selected profile fields", values)
	}
	if values.pass != "stored-pass" {
		t.Fatalf("selected profile password = %q, want stored-pass", values.pass)
	}
}

func TestConnectionForm_editPopulatesStoredPassword_merged_2(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
		Pass:   "stored-pass",
	}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.values.pass = "previous-password"
	model.connection.form.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.form.values
	if values.driver != driverMySQL || values.name != "Production" || values.host != "db.example.test" || values.port != "3307" || values.user != "alice" || values.target != "app" {
		t.Fatalf("connection form = %#v, want selected profile fields", values)
	}
	if values.pass != "stored-pass" {
		t.Fatalf("selected profile password = %q, want stored-pass", values.pass)
	}
}

func TestConnectionForm_editPopulatesStoredPassword_merged_3(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.recentConnections = []profile.Profile{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
		Pass:   "stored-pass",
	}}
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
	model.connection.form.values.pass = "previous-password"
	model.connection.form.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.form.values
	if values.driver != driverMySQL || values.name != "Production" || values.host != "db.example.test" || values.port != "3307" || values.user != "alice" || values.target != "app" {
		t.Fatalf("connection form = %#v, want selected profile fields", values)
	}
	if values.pass != "stored-pass" {
		t.Fatalf("selected profile password = %q, want stored-pass", values.pass)
	}
}

func TestConnectionForm_paneKeysKeepTabInTheForm(t *testing.T) {
	model := New("", context.Background(), testOpen, false)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)
	if model.connection.form.focus != connectionFocusRecent {
		t.Fatalf("connection focus = %d, want recent", model.connection.form.focus)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	model = updated.(Model)
	if model.connection.form.focus != connectionFocusForm {
		t.Fatalf("connection focus = %d, want form", model.connection.form.focus)
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
			model := New("", context.Background(), testOpen, false)
			model.connection.recentConnections = []profile.Profile{{Driver: driverSQLite, Name: "Alpha", Target: ":memory:"}}
			_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
			model.connection.form.setFocus(connectionFocusRecent)

			// When
			updated, command := model.Update(test.key)
			model = updated.(Model)
			if command == nil {
				t.Fatal("recent route returned no Huh initialization command")
			}
			message := command()
			// The open-form action also shows a notification popup, so the
			// returned command may batch the form init with the popup tick;
			// resolveConnectionMessage flattens batches recursively.
			model = resolveConnectionMessage(model, message, 16)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
			model = updated.(Model)
			updated, command = model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
			model = updated.(Model)
			model = resolveConnectionCommand(model, command)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			model = updated.(Model)

			// Then
			if !strings.Contains(model.connection.form.View(), "Target*") {
				t.Fatalf("connection form after %s = %q, want rendered Target* control", test.name, model.connection.form.View())
			}
			if !strings.Contains(model.connection.form.values.name, "x") {
				t.Fatalf("connection name after %s = %q, want Huh input to accept text", test.name, model.connection.form.values.name)
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

func TestConnectionProfiles_generatesAndPreservesUUIDv7ID(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connection.form.values.name, model.connection.form.values.target = "Scratch", ":memory:"

	// When — a new profile is recorded
	model.recordConnection("")

	// Then — it carries a fresh UUIDv7 scope
	prof := model.connection.recentConnections[0]
	if !profile.ValidID(prof.ID) {
		t.Fatalf("new profile ID = %q, want a UUIDv7", prof.ID)
	}

	// Save/load preserves the ID.
	if err := profile.Save(model.connection.recentPath, model.connection.recentConnections); err != nil {
		t.Fatalf("saving profiles: %v", err)
	}
	loaded, _ := profile.Load(model.connection.recentPath)
	if len(loaded) != 1 || loaded[0].ID != prof.ID {
		t.Fatalf("loaded profiles = %#v, want the saved ID preserved", loaded)
	}

	// Editing an existing profile carries its ID into the form and re-record
	// preserves it (simulating an edited-and-saved profile).
	model2 := New("", context.Background(), testOpen, false)
	model2.connection.recentConnections, _ = profile.Load(model2.connection.recentPath)
	_ = model2.connection.recent.SetItems(recentListItems(model2.connection.recentConnections))
	command := model2.editSelectedRecentConnection()
	model2 = resolveConnectionCommand(model2, command)
	if model2.connection.form.values.id != prof.ID {
		t.Fatalf("form ID = %q, want selected profile ID %q", model2.connection.form.values.id, prof.ID)
	}
	model2.connection.form.values.name = "Renamed"
	model2.recordConnection("")
	if model2.connection.recentConnections[0].ID != prof.ID {
		t.Fatalf("edited profile ID = %q, want preserved %q", model2.connection.recentConnections[0].ID, prof.ID)
	}

	// A brand-new profile must mint a distinct ID, and the saved file keeps it.
	model2.connection.form.values.id = ""
	model2.connection.form.values.name, model2.connection.form.values.target = "Other", "/tmp/other.db"
	model2.recordConnection("")
	if model2.connection.recentConnections[0].ID == prof.ID {
		t.Fatal("new profile reused the previous profile's ID")
	}
	if err := profile.Save(model2.connection.recentPath, model2.connection.recentConnections); err != nil {
		t.Fatalf("saving profiles: %v", err)
	}
	persisted, _ := profile.Load(model2.connection.recentPath)
	if len(persisted) != 2 || persisted[0].ID != model2.connection.recentConnections[0].ID {
		t.Fatalf("persisted profiles = %#v, want two with the new ID first", persisted)
	}
}

func TestConnectionProfiles_legacyJSONProfileReceivesPersistedID(t *testing.T) {
	// Given — a pre-scope connections.json without any id field
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := profile.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[{"driver":"sqlite","name":"Legacy","target":"/tmp/legacy.db"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	// When — loaded and rebuilt through New
	loaded, migrated := profile.Load(path)
	if !migrated {
		t.Fatal("legacy profile load reported no migration")
	}
	if len(loaded) != 1 || !profile.ValidID(loaded[0].ID) {
		t.Fatalf("migrated profiles = %#v, want one UUIDv7-scoped profile", loaded)
	}
	model := New("", context.Background(), testOpen, false)

	// Then — New persisted the assigned ID immediately
	persisted, _ := profile.Load(model.connection.recentPath)
	if len(persisted) != 1 || persisted[0].ID != model.connection.recentConnections[0].ID {
		t.Fatalf("persisted profiles = %#v, want the migrated ID %q on disk", persisted, model.connection.recentConnections[0].ID)
	}
}
