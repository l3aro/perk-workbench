package profile

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	loaded, _, _ := Load(path)
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

	loaded, _, _ := Load(path)
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
	loaded, _, _ := Load(path)
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

	loaded, migrated, _ := Load(path)
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
	loaded, migrated, _ := Load(path)
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
	if loaded, migrated, _ := Load(path); len(loaded) != 0 || migrated {
		t.Fatalf("missing file = %#v/%t, want none", loaded, migrated)
	}
	path = filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, migrated, _ := Load(path); len(loaded) != 0 || migrated {
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

	loaded, _, _ := Load(path)
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
	loaded, _, _ := Load(path)
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
	loaded, _, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Driver != "redis" || loaded[0].Target != "localhost" || loaded[0].Extras["db"] != "0" {
		t.Fatalf("loaded unknown-driver profile = %#v, want it preserved", loaded)
	}
}

// --- credential hardening -------------------------------------------------

func postgresProfile(name, pass string) Profile {
	return Profile{Driver: DriverPostgreSQL, Name: name, Target: "db", Host: "h", Port: "5432", User: "u", Pass: pass}
}

func TestSave_v2EnvelopeRoundtripAndAADSwaps(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	if err := Save(path, []Profile{postgresProfile("One", "pass-one"), postgresProfile("Two", "pass-two")}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"pass": "enc:v2:`) {
		t.Fatalf("stored pass is not a v2 envelope: %s", contents)
	}
	var saved []Profile
	if err := json.Unmarshal(contents, &saved); err != nil {
		t.Fatal(err)
	}
	for _, p := range saved {
		if !ValidID(p.ID) {
			t.Fatalf("ID-less input was persisted with invalid scope %q; every envelope must bind a UUIDv7", p.ID)
		}
	}
	loaded, _, _ := Load(path)
	if len(loaded) != 2 || loaded[0].Pass != "pass-one" || loaded[1].Pass != "pass-two" {
		t.Fatalf("loaded = %#v, want both passwords decrypted", loaded)
	}

	// Swap the ciphertexts between profiles: the AAD binds each envelope
	// to its scope ID plus field identity, so both swaps must fail closed.
	var stored []Profile
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	stored[0].Pass, stored[1].Pass = stored[1].Pass, stored[0].Pass
	swapped, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, swapped, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, secretFail := Load(path)
	if !secretFail {
		t.Fatal("AAD swap reported no secret failure")
	}
	for _, p := range loaded {
		if p.Pass == "pass-one" || p.Pass == "pass-two" {
			t.Fatalf("swapped profile surfaced a literal password %q", p.Pass)
		}
		if p.Undecryptable["pass"] != p.Pass {
			t.Fatalf("marker = %q, want the retained blob %q", p.Undecryptable["pass"], p.Pass)
		}
	}
	if err := Save(path, loaded); err == nil {
		t.Fatal("Save accepted profiles with retained undecryptable blobs")
	}
	// Save fails closed on the whole file while any profile still holds
	// a retained blob; re-entering both passwords clears the refusal.
	loaded[0].Pass = "re-entered"
	if err := Save(path, loaded); err == nil {
		t.Fatal("Save accepted a file with one profile still holding a retained blob")
	}
	loaded[1].Pass = "re-entered-two"
	if err := Save(path, loaded); err != nil {
		t.Fatalf("Save after re-entering both passwords: %v", err)
	}
	reloaded, _, secretFail := Load(path)
	if secretFail {
		t.Fatal("load after re-entry still reports a secret failure")
	}
	if reloaded[0].Pass != "re-entered" || reloaded[1].Pass != "re-entered-two" {
		t.Fatalf("reloaded = %#v, want the re-entered passwords", reloaded)
	}
}

func TestLoad_v1EnvelopeMigratesToV2OnNextSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	key, err := loadOrGenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	// A pre-v2 envelope: AES-256-GCM without the scope/field AAD.
	raw, err := encrypt([]byte("legacy-secret"), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	v1 := encPrefix + base64.StdEncoding.EncodeToString(raw)
	dir := filepath.Join(t.TempDir(), "perk-workbench")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "connections.json")
	record := `[{"driver":"postgres","name":"Legacy","target":"db","host":"h","port":"5432","user":"u","pass":"` + v1 + `"}]`
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, _ := Load(path)
	if len(loaded) != 1 || loaded[0].Pass != "legacy-secret" {
		t.Fatalf("loaded = %#v, want the v1 record decrypted", loaded)
	}
	// The next successful save rewrites the envelope to v2.
	if err := Save(path, loaded); err != nil {
		t.Fatalf("saving migrated profiles: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"pass": "enc:v2:`) {
		t.Fatalf("migrated pass is not a v2 envelope: %s", contents)
	}
	reloaded, _, _ := Load(path)
	if len(reloaded) != 1 || reloaded[0].Pass != "legacy-secret" {
		t.Fatalf("reloaded = %#v, want the migrated password decrypted", reloaded)
	}
}

func TestLoad_tamperedOrWrongKeyCiphertextFailsClosed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	if err := Save(path, []Profile{postgresProfile("Prod", "tamper-me")}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored []Profile
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	// Flip one base64 character in the envelope body.
	body := stored[0].Pass[len(encV2Prefix):]
	last := body[len(body)-1]
	if last == 'A' {
		last = 'B'
	} else {
		last = 'A'
	}
	stored[0].Pass = encV2Prefix + body[:len(body)-1] + string(last)
	tampered, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, secretFail := Load(path)
	if !secretFail {
		t.Fatal("tampered ciphertext reported no secret failure")
	}
	if len(loaded) != 1 || loaded[0].Pass != stored[0].Pass {
		t.Fatalf("tampered profile = %#v, want the retained blob, never a literal", loaded)
	}
	if loaded[0].Pass == "tamper-me" || strings.Contains(loaded[0].Pass, "tamper-me") {
		t.Fatal("tampered ciphertext surfaced the plaintext")
	}
	if err := Save(path, loaded); err == nil {
		t.Fatal("Save accepted a retained undecryptable blob")
	}

	// A wrong key (replaced secret.key) must fail closed the same way.
	keyPath, err := secretKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	other := new([32]byte)
	if _, err := rand.Read(other[:]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, other[:], 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, secretFail = Load(path)
	if !secretFail {
		t.Fatal("wrong-key load reported no secret failure")
	}
	if len(loaded) != 1 || loaded[0].Pass == "tamper-me" || loaded[0].Undecryptable["pass"] == "" {
		t.Fatalf("wrong-key profile = %#v, want the retained blob marked", loaded)
	}
}

func TestLoadOrGenerateKey_concurrentFirstCreation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const workers = 8
	var wg sync.WaitGroup
	keys := make(chan *[32]byte, workers)
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, err := loadOrGenerateKey()
			errs <- err
			if err == nil {
				keys <- k
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(keys)
	for err := range errs {
		if err != nil {
			t.Fatalf("loadOrGenerateKey: %v", err)
		}
	}
	var first *[32]byte
	for k := range keys {
		if first == nil {
			first = k
		} else if *k != *first {
			t.Fatal("concurrent creators did not agree on one key")
		}
	}
	path, err := secretKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want a regular 0600 file", info.Mode())
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != secretKeyLen {
		t.Fatalf("key file = %d bytes, %v; want %d", len(data), err, secretKeyLen)
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("config dir mode = %#o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestLoadKey_refusesSymlinkLaxPermissionsAndBadLength(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := secretKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// A symlinked key is refused, never followed.
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink load error = %v, want a symlink refusal", err)
	}
	os.Remove(path)
	// Lax permission bits are refused.
	key := bytes.Repeat([]byte{1}, secretKeyLen)
	if err := os.WriteFile(path, key, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("lax-permission load error = %v, want a permission refusal", err)
	}
	os.Remove(path)
	// A wrong length is refused.
	if err := os.WriteFile(path, key[:secretKeyLen-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("bad-length load error = %v, want a length refusal", err)
	}
	os.Remove(path)
	// The valid form loads.
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(); err != nil {
		t.Fatalf("valid key did not load: %v", err)
	}
}

func TestSave_refusesSymlinkAndNonRegularTargets(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "perk-workbench")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "connections.json")
	profiles := []Profile{{Driver: DriverSQLite, Name: "Local", Target: "/tmp/local.db"}}

	// A symlinked profiles target is refused and never replaced.
	if err := os.Symlink(filepath.Join(t.TempDir(), "real.json"), path); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, profiles); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink save error = %v, want a symlink refusal", err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink target was replaced instead of refused")
	}
	os.Remove(path)

	// A non-regular (directory) target is refused.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, profiles); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("directory save error = %v, want a non-regular refusal", err)
	}
}

func TestLoad_refusesSymlinkedProfilesFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "perk-workbench")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(real, []byte(`[{"driver":"sqlite","name":"Real","target":"/tmp/x.db"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "connections.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if loaded, _, _ := Load(link); len(loaded) != 0 {
		t.Fatalf("symlinked profiles file loaded %#v, want an empty list", loaded)
	}
}

func TestSave_failedRenameLeavesOriginalIntact(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	profiles := []Profile{{Driver: DriverSQLite, Name: "Local", Target: "/tmp/local.db"}}
	if err := Save(path, profiles); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Inject a rename failure after the temp file is written and fsynced:
	// the original must stay byte-for-byte intact and the temp removed.
	original := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	defer func() { renameFile = original }()
	if err := Save(path, profiles); err == nil || !strings.Contains(err.Error(), "injected rename failure") {
		t.Fatalf("save error = %v, want the injected failure", err)
	}
	renameFile = original
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed save modified the original file")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "recent-") {
			t.Fatalf("failed save left a temp file %q behind", entry.Name())
		}
	}
}

func TestSave_enforces0700DirAnd0600File(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "perk-workbench")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte(`[{"driver":"sqlite","name":"Old","target":"/tmp/old.db"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	profiles := []Profile{{Driver: DriverSQLite, Name: "Local", Target: "/tmp/local.db"}}
	if err := Save(path, profiles); err != nil {
		t.Fatalf("saving: %v", err)
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("config dir mode = %#o, want 0700", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("profiles file mode = %#o, want 0600", fileInfo.Mode().Perm())
	}
}

func TestSave_noPlaintextSecretBytesInFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	const password = "literal-pw-9x7"
	const token = "plugin-token-4q2"
	const targetURL = "redis://alice:target-pw-3m@db.example.test:6379/0"
	profiles := []Profile{
		{Driver: "redis", Name: "Cache", Target: targetURL, Extras: map[string]string{"auth_token": token}},
		postgresProfile("Prod", password),
	}
	if err := Save(path, profiles); err != nil {
		t.Fatalf("saving: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{password, token, "target-pw-3m", "alice:target-pw-3m@", "redis://alice"} {
		if bytes.Contains(contents, []byte(secret)) {
			t.Fatalf("file contains plaintext %q", secret)
		}
	}
	if !strings.Contains(string(contents), `"pass": "enc:v2:`) || !strings.Contains(string(contents), `"auth_token": "enc:v2:`) || !strings.Contains(string(contents), `"target": "enc:v2:`) {
		t.Fatalf("stored secrets are not v2 envelopes: %s", contents)
	}
	loaded, _, _ := Load(path)
	if len(loaded) != 2 || loaded[0].Extras["auth_token"] != token || loaded[0].Target != targetURL || loaded[1].Pass != password {
		t.Fatalf("loaded = %#v, want the secrets decrypted", loaded)
	}
}

// --- credential-bearing target protection ---------------------------------

func TestSave_encryptsCredentialBearingTargets(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	tests := []struct {
		name   string
		target string
	}{
		{"redis userinfo", "redis://alice:secret-pw@127.0.0.1:6379/0"},
		{"percent-encoded userinfo", "redis://alice:p%40ss%3Aw%2Frd@127.0.0.1:6379/0"},
		{"mongodb userinfo", "mongodb://alice:secret-pw@db.example.test:27017/app?authSource=admin"},
		{"postgres password query", "postgres://alice@db.example.test:5432/app?password=secret-pw&sslmode=disable"},
		{"mysql dsn", "mysql:alice:secret-pw@tcp(db.example.test:3306)/app"},
		{"label-prefixed url", "redis:redis://alice:secret-pw@127.0.0.1:6379/0"},
		{"postgres label-prefixed url", "postgres:postgres://alice:secret-pw@db.example.test:5432/app"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := []Profile{{Driver: "redis", Name: "Cache", Target: test.target}}
			if err := Save(path, profiles); err != nil {
				t.Fatalf("saving: %v", err)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(contents, []byte("secret-pw")) || bytes.Contains(contents, []byte("p%40ss")) {
				t.Fatalf("credential-bearing target persisted in plaintext: %s", contents)
			}
			if !strings.Contains(string(contents), `"target": "enc:v2:`) {
				t.Fatalf("target is not a v2 envelope: %s", contents)
			}
			loaded, _, secretFail := Load(path)
			if secretFail || len(loaded) != 1 || loaded[0].Target != test.target {
				t.Fatalf("loaded target = %q/%t, want the exact original %q", loaded[0].Target, secretFail, test.target)
			}
		})
	}
}

func TestSave_keepsNonCredentialTargetsReadable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	tests := []struct {
		name   string
		target string
	}{
		{"sqlite path", "/tmp/local.db"},
		{"database name", "0"},
		{"database name with label", "redis:2"},
		{"userinfo-less url", "postgres://alice@db.example.test:5432/app?sslmode=disable"},
		{"blank query password", "postgres://alice@db.example.test:5432/app?password="},
		{"localhost", "localhost"},
		{"file reference", "file:///tmp/secret-notes.txt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := []Profile{{Driver: DriverSQLite, Name: "Local", Target: test.target}}
			if err := Save(path, profiles); err != nil {
				t.Fatalf("saving: %v", err)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(contents), `"target": "`+test.target+`"`) {
				t.Fatalf("non-credential target %q was not persisted verbatim: %s", test.target, contents)
			}
			loaded, _, _ := Load(path)
			if len(loaded) != 1 || loaded[0].Target != test.target {
				t.Fatalf("loaded target = %q, want %q", loaded[0].Target, test.target)
			}
		})
	}
}

func TestLoad_legacyPlaintextCredentialTargetMigrates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "perk-workbench")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "connections.json")
	const target = "redis://alice:secret-pw@127.0.0.1:6379/0"
	record := `[{"driver":"redis","name":"Cache","target":"` + target + `"}]`
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	// Legacy plaintext credential-bearing targets load for compatibility.
	loaded, migrated, secretFail := Load(path)
	if secretFail || len(loaded) != 1 || loaded[0].Target != target {
		t.Fatalf("loaded = %#v/%t, want the legacy target verbatim", loaded, secretFail)
	}
	if !migrated {
		t.Fatal("legacy credential target reported no migration")
	}
	// The next successful save migrates it to the encrypted form.
	if err := Save(path, loaded); err != nil {
		t.Fatalf("saving migrated profile: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"target": "enc:v2:`) || bytes.Contains(contents, []byte("secret-pw")) {
		t.Fatalf("migrated target is not an encrypted envelope: %s", contents)
	}
	reloaded, _, secretFail := Load(path)
	if secretFail || len(reloaded) != 1 || reloaded[0].Target != target {
		t.Fatalf("reloaded = %#v/%t, want the exact target back", reloaded, secretFail)
	}
}

func TestLoad_tamperedEncryptedTargetFailsClosed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	const target = "redis://alice:secret-pw@127.0.0.1:6379/0"
	if err := Save(path, []Profile{{Driver: "redis", Name: "Cache", Target: target}}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte(`"target": "enc:v2:`)
	envelope := bytes.Index(contents, marker)
	if envelope < 0 {
		t.Fatalf("no encrypted target in %s", contents)
	}
	bodyStart := envelope + len(marker)
	bodyEnd := bytes.IndexByte(contents[bodyStart:], '"')
	if bodyEnd < 0 {
		t.Fatal("unterminated envelope")
	}
	tampered := append([]byte{}, contents...)
	first := tampered[bodyStart]
	if first == 'A' {
		first = 'B'
	} else {
		first = 'A'
	}
	tampered[bodyStart] = first
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, secretFail := Load(path)
	if !secretFail {
		t.Fatal("tampered target reported no secret failure")
	}
	if len(loaded) != 1 || loaded[0].Target == target || strings.Contains(loaded[0].Target, "secret-pw") {
		t.Fatalf("tampered profile target = %q, want the retained blob, never a literal", loaded[0].Target)
	}
	if loaded[0].Undecryptable["target"] != loaded[0].Target {
		t.Fatalf("marker = %q, want the retained blob %q", loaded[0].Undecryptable["target"], loaded[0].Target)
	}
	if err := Save(path, loaded); err == nil {
		t.Fatal("Save accepted a profile with a retained undecryptable target")
	}
	// Re-entering the target clears the refusal and saves normally.
	loaded[0].Target = "redis://127.0.0.1:6379/1"
	if err := Save(path, loaded); err != nil {
		t.Fatalf("Save after re-entering the target: %v", err)
	}
	reloaded, _, secretFail := Load(path)
	if secretFail || len(reloaded) != 1 || reloaded[0].Target != "redis://127.0.0.1:6379/1" {
		t.Fatalf("reloaded = %#v/%t, want the re-entered target", reloaded, secretFail)
	}
}

func TestLoad_targetPassAADSwapFailsClosed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "perk-workbench", "connections.json")
	const target = "redis://alice:secret-pw@127.0.0.1:6379/0"
	const password = "other-secret"
	if err := Save(path, []Profile{{
		Driver: "redis", Name: "Cache", Target: target,
		Extras: map[string]string{"auth_token": password},
	}}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Swap the two envelopes: the AAD binds each to its field identity,
	// so both decrypts must fail closed.
	var stored []Profile
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || len(stored[0].Extras) != 1 {
		t.Fatalf("stored = %#v, want one profile with one extra", stored)
	}
	stored[0].Target, stored[0].Extras["auth_token"] = stored[0].Extras["auth_token"], stored[0].Target
	swapped, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, swapped, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, secretFail := Load(path)
	if !secretFail {
		t.Fatal("field-swapped envelopes reported no secret failure")
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %#v, want one profile retained", loaded)
	}
	for _, forbidden := range []string{target, password} {
		if loaded[0].Target == forbidden || loaded[0].Extras["auth_token"] == forbidden {
			t.Fatalf("swapped profile surfaced a literal %q", forbidden)
		}
	}
	if loaded[0].Undecryptable["target"] == "" || loaded[0].Undecryptable["auth_token"] == "" {
		t.Fatalf("swapped profile markers = %#v, want both fields marked", loaded[0].Undecryptable)
	}
}
