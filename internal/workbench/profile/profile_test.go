package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave_persistsSQLiteOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	connections := []Profile{
		{Driver: DriverSQLite, Name: "Local", Target: "/tmp/local.db"},
		{Driver: DriverMySQL, Name: "Remote", Target: "user:password@tcp(host:3306)/app"},
	}

	if err := Save(path, connections); err != nil {
		t.Fatalf("saving profiles: %v", err)
	}

	loaded, _ := Load(path)
	if len(loaded) != 1 {
		t.Fatalf("loaded profiles = %d, want 1 (MySQL without host/port/user is invalid)", len(loaded))
	}
	if loaded[0].Driver != connections[0].Driver || loaded[0].Name != connections[0].Name || loaded[0].Target != connections[0].Target {
		t.Fatalf("loaded profile = %#v, want %#v", loaded[0], connections[0])
	}
}

func TestSave_encryptsLiteralPasswordsAndLoadDecrypts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	password := "my-secret-password!"
	connections := []Profile{{
		Driver: DriverPostgreSQL, Name: "Test",
		Target: "db", Host: "localhost", Port: "5432", User: "admin", Pass: password,
	}}

	if err := Save(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}

	var stored []struct{ Pass string }
	contents, _ := os.ReadFile(path)
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Pass == password {
		t.Fatalf("password stored in plaintext or missing")
	}
	if !strings.HasPrefix(stored[0].Pass, "enc:") {
		t.Fatalf("password not encrypted, prefix=%q", stored[0].Pass[:min(5, len(stored[0].Pass))])
	}

	loaded, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Pass != password {
		t.Fatalf("loaded password = %q, want %q", loaded[0].Pass, password)
	}
}

func TestSave_keepsSecretReferencesVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	connections := []Profile{{
		Driver: DriverPostgreSQL, Name: "Ref", Target: "db",
		Host: "localhost", Port: "5432", User: "admin", Pass: "${DB_PASS}",
	}}
	if err := Save(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}
	loaded, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Pass != "${DB_PASS}" {
		t.Fatalf("loaded password = %q, want the reference verbatim", loaded[0].Pass)
	}
}

func TestLoad_legacyJSONProfileReceivesPersistedID(t *testing.T) {
	// A pre-scope connections.json without any id field.
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[{"driver":"sqlite","name":"Legacy","target":"/tmp/legacy.db"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, migrated := Load(path)
	if !migrated {
		t.Fatal("legacy profile load reported no migration")
	}
	if len(loaded) != 1 || !ValidID(loaded[0].ID) {
		t.Fatalf("migrated profiles = %#v, want one UUIDv7-scoped profile", loaded)
	}
}

func TestLoad_duplicateIDsAreReassigned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
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
	loaded, migrated := Load(path)
	if !migrated {
		t.Fatal("invalid legacy IDs reported no migration")
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded profiles = %d, want 3", len(loaded))
	}
	seen := map[string]bool{}
	for _, profile := range loaded {
		if !ValidID(profile.ID) || seen[profile.ID] {
			t.Fatalf("profile %q has invalid or duplicate ID %q", profile.Name, profile.ID)
		}
		seen[profile.ID] = true
	}
}

func TestLoad_unreadableOrCorruptFileYieldsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if loaded, migrated := Load(path); len(loaded) != 0 || migrated {
		t.Fatalf("missing file = %#v/%t, want none", loaded, migrated)
	}
	path = filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, migrated := Load(path); len(loaded) != 0 || migrated {
		t.Fatalf("corrupt file = %#v/%t, want none", loaded, migrated)
	}
}

func TestValidID_acceptsOnlyUUIDv7(t *testing.T) {
	if id, err := NewID(); err != nil || !ValidID(id) {
		t.Fatalf("NewID = %q/%v, want a valid UUIDv7", id, err)
	}
	for _, id := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if ValidID(id) {
			t.Fatalf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestSave_encryptsExtrasAndLoadDecrypts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	connections := []Profile{{
		Driver: DriverMySQL, Name: "Test",
		Target: "db", Host: "localhost", Port: "3306", User: "admin", Pass: "pw",
		Extras: map[string]string{"auth_token": "tok-secret"},
	}}
	if err := Save(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}

	var stored []struct {
		Pass   string
		Extras map[string]string
	}
	contents, _ := os.ReadFile(path)
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored profiles = %d, want 1", len(stored))
	}
	if got := stored[0].Extras["auth_token"]; got == "tok-secret" || !strings.HasPrefix(got, "enc:") {
		t.Fatalf("extras stored in plaintext, got %q", got)
	}

	loaded, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Extras["auth_token"] != "tok-secret" {
		t.Fatalf("loaded extras = %v, want the decrypted token", loaded[0].Extras)
	}
}

func TestSave_keepsExtrasSecretReferencesVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	connections := []Profile{{
		Driver: DriverMySQL, Name: "Ref", Target: "db",
		Host: "localhost", Port: "3306", User: "admin", Pass: "${DB_PASS}",
		Extras: map[string]string{"auth_token": "${AUTH_TOKEN}"},
	}}
	if err := Save(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}
	loaded, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Extras["auth_token"] != "${AUTH_TOKEN}" {
		t.Fatalf("loaded extras = %v, want the reference verbatim", loaded[0].Extras)
	}
}

func TestSave_keepsUnknownDriverProfiles(t *testing.T) {
	// A future registered driver's profile must survive persistence; its
	// form-level requirements are the connection layer's job.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	connections := []Profile{{
		Driver: "redis", Name: "Cache", Target: "localhost",
		Extras: map[string]string{"db": "0"},
	}}
	if err := Save(path, connections); err != nil {
		t.Fatalf("saving: %v", err)
	}
	contents, _ := os.ReadFile(path)
	if strings.Contains(string(contents), `"db": "0"`) {
		t.Fatal("extras stored in plaintext")
	}
	loaded, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Driver != "redis" || loaded[0].Target != "localhost" || loaded[0].Extras["db"] != "0" {
		t.Fatalf("loaded unknown-driver profile = %#v, want it preserved", loaded)
	}
}
