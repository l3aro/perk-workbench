package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeExecutableAt writes an executable file and returns its path.
func writeExecutableAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeConfigFile writes config.json under an isolated XDG_CONFIG_HOME
// and returns the config file path.
func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// snapshotAppConfig preserves the package-level appConfig for the rest
// of the suite: SavePlugin/RemovePlugin keep it in sync, so tests that
// persist must restore it (the suite's TestMain sets a global
// NerdFont=false that later tests rely on).
func snapshotAppConfig(t *testing.T) {
	t.Helper()
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
}

func TestLoadConfig_pluginTrustValidation(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	tests := []struct {
		name    string
		trust   string
		wantErr string
	}{
		{name: "valid trust", trust: `{"/usr/bin/perk-redis": "` + digest + `"}`},
		{name: "uppercase digest accepted", trust: `{"/usr/bin/perk-redis": "` + strings.ToUpper(digest) + `"}`},
		{name: "relative key rejected", trust: `{"perk-redis": "` + digest + `"}`, wantErr: "must be an absolute path"},
		{name: "blank key rejected", trust: `{"": "` + digest + `"}`, wantErr: "must not be blank"},
		{name: "short digest rejected", trust: `{"/usr/bin/perk-redis": "abc"}`, wantErr: "64 hexadecimal characters"},
		{name: "non-hex digest rejected", trust: `{"/usr/bin/perk-redis": "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"}`, wantErr: "64 hexadecimal characters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfigFile(t, `{"plugins": ["/usr/bin/perk-redis"], "plugin_trust": `+test.trust+`}`)
			config, err := LoadConfig(path)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("LoadConfig error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig = %v, want nil error", err)
			}
			if config.PluginTrust["/usr/bin/perk-redis"] == "" {
				t.Fatalf("PluginTrust = %v, want the digest", config.PluginTrust)
			}
		})
	}
}

func TestSavePlugin_appendsAndPinsAtomically(t *testing.T) {
	snapshotAppConfig(t)
	digest := strings.Repeat("cd", 32)
	path := writeConfigFile(t, `{"dark_theme": "nord", "unknown_future_key": {"nested": 1}}`)
	executable := writeExecutableAt(t, filepath.Join(t.TempDir(), "perk-redis"))

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := SavePlugin(path, executable, digest)
	if err != nil || !changed {
		t.Fatalf("SavePlugin = %t, %v, want changed", changed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil error", err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0] != executable {
		t.Fatalf("Plugins = %v, want the canonical executable path", config.Plugins)
	}
	if config.PluginTrust[executable] != digest {
		t.Fatalf("PluginTrust = %v, want the pinned digest", config.PluginTrust)
	}

	// Every unrelated key survives (semantically; MarshalIndent
	// re-indents nested values, exactly like the existing save paths).
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rawBefore, rawAfter map[string]json.RawMessage
	if err := json.Unmarshal(before, &rawBefore); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &rawAfter); err != nil {
		t.Fatal(err)
	}
	var valueBefore, valueAfter any
	for key := range rawBefore {
		if key == "plugins" || key == "plugin_trust" {
			continue
		}
		if err := json.Unmarshal(rawBefore[key], &valueBefore); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rawAfter[key], &valueAfter); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(valueAfter, valueBefore) {
			t.Fatalf("key %q changed: %s -> %s", key, rawBefore[key], rawAfter[key])
		}
	}

	// Re-pinning the same digest is idempotent: no write, changed=false.
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = SavePlugin(path, executable, digest)
	if err != nil || changed {
		t.Fatalf("idempotent SavePlugin = %t, %v, want changed=false", changed, err)
	}
	after, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("idempotent SavePlugin rewrote the file")
	}
}

func TestSavePlugin_replacesEntryResolvingToSameExecutable(t *testing.T) {
	snapshotAppConfig(t)
	digest := strings.Repeat("ef", 32)
	configDir := t.TempDir()
	real := writeExecutableAt(t, filepath.Join(configDir, "real-plugin"))
	link := filepath.Join(configDir, "linked-plugin")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(path, []byte(`{"plugins": ["./real-plugin"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pinning via the symlink: the entry that resolves to the same
	// canonical path is replaced, not duplicated.
	changed, err := SavePlugin(path, real, digest)
	if err != nil || !changed {
		t.Fatalf("SavePlugin = %t, %v, want changed", changed, err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0] != real {
		t.Fatalf("Plugins = %v, want the entry replaced by %q", config.Plugins, real)
	}
	if config.PluginTrust[real] != digest {
		t.Fatalf("PluginTrust = %v, want the pin", config.PluginTrust)
	}
}

func TestSavePlugin_failureLeavesOriginalIntact(t *testing.T) {
	snapshotAppConfig(t)
	digest := strings.Repeat("01", 32)
	path := writeConfigFile(t, `{"plugins": ["/broken`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	executable := writeExecutableAt(t, filepath.Join(t.TempDir(), "perk-redis"))

	if _, err := SavePlugin(path, executable, digest); err == nil {
		t.Fatal("SavePlugin on a malformed config = nil error, want an error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed SavePlugin modified the config file")
	}
}

func TestRemovePlugin_byExactEntry(t *testing.T) {
	snapshotAppConfig(t)
	dir := t.TempDir()
	executable := writeExecutableAt(t, filepath.Join(dir, "perk-redis"))
	digest := strings.Repeat("23", 32)
	path := writeConfigFile(t, `{"dark_theme": "nord", "plugins": ["`+executable+`"], "plugin_trust": {"`+executable+`": "`+digest+`"}}`)

	entry, canonical, changed, err := RemovePlugin(path, executable)
	if err != nil || !changed {
		t.Fatalf("RemovePlugin = %q/%q/%t, %v, want removed", entry, canonical, changed, err)
	}
	if entry != executable || canonical != executable {
		t.Fatalf("RemovePlugin returned %q/%q, want the exact entry %q", entry, canonical, executable)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if len(config.Plugins) != 0 {
		t.Fatalf("Plugins = %v, want empty after removal", config.Plugins)
	}
	if len(config.PluginTrust) != 0 {
		t.Fatalf("PluginTrust = %v, want the trust record dropped", config.PluginTrust)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "plugin_trust") {
		t.Fatalf("config still carries plugin_trust: %s", raw)
	}
	if !strings.Contains(string(raw), `"dark_theme": "nord"`) {
		t.Fatalf("config lost the unrelated dark_theme key: %s", raw)
	}
}

func TestRemovePlugin_byCanonicalPathMatch(t *testing.T) {
	snapshotAppConfig(t)
	dir := t.TempDir()
	executable := writeExecutableAt(t, filepath.Join(dir, "perk-redis"))
	path := writeConfigFile(t, `{"plugins": ["`+executable+`"]}`)

	entry, _, changed, err := RemovePlugin(path, filepath.Join(dir, "perk-redis"))
	if err != nil || !changed || entry != executable {
		t.Fatalf("RemovePlugin = %q/%t, %v, want the canonical-path match removed", entry, changed, err)
	}
}

func TestRemovePlugin_ambiguousFailsWithoutChanges(t *testing.T) {
	snapshotAppConfig(t)
	dir := t.TempDir()
	real := writeExecutableAt(t, filepath.Join(dir, "real-plugin"))
	link := filepath.Join(dir, "linked-plugin")
	alias := filepath.Join(dir, "alias-plugin")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	path := writeConfigFile(t, `{"plugins": ["`+real+`", "`+link+`"]}`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// alias is not a configured entry, but it resolves to the same
	// canonical path as two entries: the match is ambiguous and nothing
	// may be removed.
	if _, _, _, err := RemovePlugin(path, alias); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("RemovePlugin error = %v, want an ambiguous-match failure", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("ambiguous RemovePlugin modified the config")
	}
}

func TestRemovePlugin_notConfigured(t *testing.T) {
	snapshotAppConfig(t)
	path := writeConfigFile(t, `{"plugins": []}`)
	if _, _, _, err := RemovePlugin(path, filepath.Join(t.TempDir(), "nope")); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("RemovePlugin error = %v, want not configured", err)
	}
}

func TestRemovePlugin_keepsSharedTrustRecord(t *testing.T) {
	snapshotAppConfig(t)
	dir := t.TempDir()
	real := writeExecutableAt(t, filepath.Join(dir, "real-plugin"))
	link := filepath.Join(dir, "linked-plugin")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("45", 32)
	path := writeConfigFile(t, `{"plugins": ["`+real+`", "`+link+`"], "plugin_trust": {"`+real+`": "`+digest+`"}}`)

	// Removing the link entry must keep the pin: the real entry still
	// resolves to the same canonical path.
	if _, _, _, err := RemovePlugin(path, link); err != nil {
		t.Fatalf("RemovePlugin = %v", err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0] != real {
		t.Fatalf("Plugins = %v, want only the real entry", config.Plugins)
	}
	if config.PluginTrust[real] != digest {
		t.Fatalf("PluginTrust = %v, want the shared pin retained", config.PluginTrust)
	}
}

func TestReadPluginTrust_neverMaterializesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "config.json")

	if trust := ReadPluginTrust(path); trust != nil {
		t.Fatalf("ReadPluginTrust = %v, want nil for a missing config", trust)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ReadPluginTrust created %s, want no file", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if trust := ReadPluginTrust(path); trust != nil {
		t.Fatalf("ReadPluginTrust = %v, want nil for a malformed config", trust)
	}
}
