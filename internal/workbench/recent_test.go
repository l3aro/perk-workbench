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
)

func TestRecentConnections_persistsSQLiteOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	connections := []recentConnection{
		{Driver: driverSQLite, Name: "Local", Target: "/tmp/local.db"},
		{Driver: driverMySQL, Name: "Remote", Target: "user:password@tcp(host:3306)/app"},
	}

	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving recent connections: %v", err)
	}

	loaded, _ := loadRecentConnections(path)
	if len(loaded) != 1 {
		t.Fatalf("loaded connections = %d, want 1", len(loaded))
	}
	if loaded[0].Driver != connections[0].Driver || loaded[0].Name != connections[0].Name || loaded[0].Target != connections[0].Target {
		t.Fatalf("loaded connection = %#v, want %#v", loaded[0], connections[0])
	}
}

func TestConnectionProfiles_persistUnnamedSQLiteTargets(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connection.values.driver, model.connection.values.name = driverSQLite, ""
	model.connection.values.target = "/tmp/alpha.db"
	model.recordConnection("")
	model.connection.values.target = "/tmp/beta.db"

	// When
	model.recordConnection("")
	loaded, _ := loadRecentConnections(model.recentPath)

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
	if model.recentPath == "" {
		t.Fatal("target startup did not initialize recent connection persistence")
	}
}

func TestConnectionProfiles_persistRemoteFieldsWithoutPassword(t *testing.T) {
	// Given
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "connections.json")
	model := New("", context.Background(), testOpen, false)
	model.connection.values.driver, model.connection.values.name = driverPostgreSQL, "Reporting"
	model.connection.values.target, model.connection.values.host = "analytics", "db.example.test"
	model.connection.values.port, model.connection.values.user, model.connection.values.pass = "5432", "analyst", "secret"
	model.recordConnection("")

	// When
	if err := saveRecentConnections(path, model.recentConnections); err != nil {
		t.Fatalf("saving connection profiles: %v", err)
	}
	loaded, _ := loadRecentConnections(path)

	// Then — verify password is encrypted (not plaintext) in the JSON file
	var stored []struct{ Pass string }
	contents, _ := os.ReadFile(path)
	json.Unmarshal(contents, &stored)
	if len(stored) != 1 || stored[0].Pass == "secret" {
		t.Fatalf("password stored in plaintext")
	}
	if !strings.HasPrefix(stored[0].Pass, encPrefix) {
		t.Fatalf("password not encrypted, prefix=%q", stored[0].Pass[:min(5, len(stored[0].Pass))])
	}
	if !reflect.DeepEqual(loaded, model.recentConnections) {
		t.Fatalf("loaded profiles = %#v, want %#v", loaded, model.recentConnections)
	}
}

func TestConnectionProfiles_encryptAndDecryptPassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "connections.json")
	password := "my-secret-password!"
	connections := []recentConnection{{
		Driver: driverPostgreSQL, Name: "Test",
		Target: "db", Host: "localhost", Port: "5432", User: "admin", Pass: password,
	}}

	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}

	var stored []struct{ Pass string }
	contents, _ := os.ReadFile(path)
	json.Unmarshal(contents, &stored)
	if len(stored) != 1 || stored[0].Pass == password {
		t.Fatalf("password stored in plaintext or missing")
	}
	if !strings.HasPrefix(stored[0].Pass, encPrefix) {
		t.Fatalf("password not encrypted, prefix=%q", stored[0].Pass[:min(5, len(stored[0].Pass))])
	}

	loaded, _ := loadRecentConnections(path)
	if len(loaded) != 1 || loaded[0].Pass != password {
		t.Fatalf("loaded password = %q, want %q", loaded[0].Pass, password)
	}
}

func TestConnectionProfiles_encryptDecryptRoundTrip_merged_2(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "connections.json")
	password := "my-secret-password!"
	connections := []recentConnection{{
		Driver: driverPostgreSQL, Name: "Test",
		Target: "db", Host: "localhost", Port: "5432", User: "admin", Pass: password,
	}}

	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}

	loaded, _ := loadRecentConnections(path)
	if len(loaded) != 1 || loaded[0].Pass != password {
		t.Fatalf("loaded password = %q, want %q", loaded[0].Pass, password)
	}
}

func TestConnectionProfiles_encryptDecryptRoundTrip_merged_3(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "connections.json")
	password := "my-secret-password!"
	connections := []recentConnection{{
		Driver: driverPostgreSQL, Name: "Test",
		Target: "db", Host: "localhost", Port: "5432", User: "admin", Pass: password,
	}}

	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}

	loaded, _ := loadRecentConnections(path)
	if len(loaded) != 1 || loaded[0].Pass != password {
		t.Fatalf("loaded password = %q, want %q", loaded[0].Pass, password)
	}
}

func TestConnectionForm_recentConnectionActions(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.recentPath = filepath.Join(t.TempDir(), "connections.json")
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

func TestConnectionForm_editWithEmptyPassword(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.recentConnections = []recentConnection{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.values.pass = "previous-password"
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.values
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
	model.recentConnections = []recentConnection{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.values.pass = "previous-password"
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.values
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
	model.recentConnections = []recentConnection{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.values.pass = "previous-password"
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.values
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
	model.recentConnections = []recentConnection{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
		Pass:   "stored-pass",
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.values.pass = "previous-password"
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.values
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
	model.recentConnections = []recentConnection{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
		Pass:   "stored-pass",
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.values.pass = "previous-password"
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.values
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
	model.recentConnections = []recentConnection{{
		Driver: driverMySQL,
		Name:   "Production",
		Target: "app",
		Host:   "db.example.test",
		Port:   "3307",
		User:   "alice",
		Pass:   "stored-pass",
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.values.pass = "previous-password"
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	values := model.connection.values
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
			model := New("", context.Background(), testOpen, false)
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

func TestConnectionProfiles_generatesAndPreservesUUIDv7ID(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connection.values.name, model.connection.values.target = "Scratch", ":memory:"

	// When — a new profile is recorded
	model.recordConnection("")

	// Then — it carries a fresh UUIDv7 scope
	profile := model.recentConnections[0]
	if !validConnectionID(profile.ID) {
		t.Fatalf("new profile ID = %q, want a UUIDv7", profile.ID)
	}

	// Save/load preserves the ID.
	if err := saveRecentConnections(model.recentPath, model.recentConnections); err != nil {
		t.Fatalf("saving profiles: %v", err)
	}
	loaded, _ := loadRecentConnections(model.recentPath)
	if len(loaded) != 1 || loaded[0].ID != profile.ID {
		t.Fatalf("loaded profiles = %#v, want the saved ID preserved", loaded)
	}

	// Editing an existing profile carries its ID into the form and re-record
	// preserves it (simulating an edited-and-saved profile).
	model2 := New("", context.Background(), testOpen, false)
	model2.recentConnections, _ = loadRecentConnections(model2.recentPath)
	_ = model2.recent.SetItems(recentListItems(model2.recentConnections))
	command := model2.editSelectedRecentConnection()
	model2 = resolveConnectionCommand(model2, command)
	if model2.connection.values.id != profile.ID {
		t.Fatalf("form ID = %q, want selected profile ID %q", model2.connection.values.id, profile.ID)
	}
	model2.connection.values.name = "Renamed"
	model2.recordConnection("")
	if model2.recentConnections[0].ID != profile.ID {
		t.Fatalf("edited profile ID = %q, want preserved %q", model2.recentConnections[0].ID, profile.ID)
	}

	// A brand-new profile must mint a distinct ID, and the saved file keeps it.
	model2.connection.values.id = ""
	model2.connection.values.name, model2.connection.values.target = "Other", "/tmp/other.db"
	model2.recordConnection("")
	if model2.recentConnections[0].ID == profile.ID {
		t.Fatal("new profile reused the previous profile's ID")
	}
	if err := saveRecentConnections(model2.recentPath, model2.recentConnections); err != nil {
		t.Fatalf("saving profiles: %v", err)
	}
	persisted, _ := loadRecentConnections(model2.recentPath)
	if len(persisted) != 2 || persisted[0].ID != model2.recentConnections[0].ID {
		t.Fatalf("persisted profiles = %#v, want two with the new ID first", persisted)
	}
}

func TestConnectionProfiles_legacyJSONProfileReceivesPersistedID(t *testing.T) {
	// Given — a pre-scope connections.json without any id field
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := recentConnectionsPath()
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
	loaded, migrated := loadRecentConnections(path)
	if !migrated {
		t.Fatal("legacy profile load reported no migration")
	}
	if len(loaded) != 1 || !validConnectionID(loaded[0].ID) {
		t.Fatalf("migrated profiles = %#v, want one UUIDv7-scoped profile", loaded)
	}
	model := New("", context.Background(), testOpen, false)

	// Then — New persisted the assigned ID immediately
	persisted, _ := loadRecentConnections(model.recentPath)
	if len(persisted) != 1 || persisted[0].ID != model.recentConnections[0].ID {
		t.Fatalf("persisted profiles = %#v, want the migrated ID %q on disk", persisted, model.recentConnections[0].ID)
	}
}

func TestConnectionProfiles_duplicateIDsAreReassigned(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := recentConnectionsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[
		{"id":"not-a-uuid","driver":"sqlite","name":"Alpha","target":"/tmp/alpha.db"},
		{"id":"not-a-uuid","driver":"sqlite","name":"Beta","target":"/tmp/beta.db"},
		{"id":"not-a-uuid","driver":"sqlite","name":"Gamma","target":"/tmp/gamma.db"}
	]`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, migrated := loadRecentConnections(path)
	if !migrated {
		t.Fatal("invalid legacy IDs reported no migration")
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded profiles = %d, want 3", len(loaded))
	}
	seen := map[string]bool{}
	for _, profile := range loaded {
		if !validConnectionID(profile.ID) || seen[profile.ID] {
			t.Fatalf("profile %q has invalid or duplicate ID %q", profile.Name, profile.ID)
		}
		seen[profile.ID] = true
	}
}
