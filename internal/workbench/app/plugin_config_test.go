package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutableAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

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

func snapshotAppConfig(t *testing.T) {
	t.Helper()
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
}

func TestLoadConfig_pluginDescriptors(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	tests := []struct {
		name    string
		plugins string
		wantErr string
	}{
		{name: "multiple sources", plugins: `[{"builtin":"sqlite","path":"x"}]`, wantErr: "exactly one"},
		{name: "unknown builtin", plugins: `[{"builtin":"redis"}]`, wantErr: "not one of"},
		{name: "digest on builtin", plugins: `[{"builtin":"sqlite","sha256":"` + digest + `"}]`, wantErr: "only valid"},
		{name: "uppercase digest", plugins: `[{"path":"x","sha256":"` + strings.ToUpper(digest) + `"}]`, wantErr: "lowercase"},
		{name: "duplicate builtin", plugins: `[{"builtin":"sqlite"},{"builtin":"sqlite"}]`, wantErr: "duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfigFile(t, `{"plugins":`+test.plugins+`}`)
			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("LoadConfig error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadConfig_duplicateResolvedExternalPaths(t *testing.T) {
	dir := t.TempDir()
	real := writeExecutableAt(t, filepath.Join(dir, "plugin"))
	link := filepath.Join(dir, "plugin-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	data := []byte(`{"plugins":[{"path":"` + real + `"},{"path":"` + link + `"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "duplicate external plugin path") {
		t.Fatalf("LoadConfig error = %v, want duplicate resolved path", err)
	}
}

func TestLoadConfig_missingFileMaterializesBuiltins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []PluginConfig{{Builtin: "sqlite"}, {Builtin: "mysql"}, {Builtin: "postgres"}, {Builtin: "mongodb"}}
	if len(config.Plugins) != len(want) {
		t.Fatalf("plugins = %#v, want %#v", config.Plugins, want)
	}
	for i := range want {
		if config.Plugins[i] != want[i] {
			t.Fatalf("plugins = %#v, want %#v", config.Plugins, want)
		}
	}
	var raw map[string]json.RawMessage
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var persisted []PluginConfig
	if err := json.Unmarshal(raw["plugins"], &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 4 || persisted[0].Builtin != "sqlite" || persisted[3].Builtin != "mongodb" {
		t.Fatalf("persisted plugins = %#v, want exact built-in order", persisted)
	}
}

func TestLoadConfig_legacyPluginMigrationIsAtomicAndPreservesUnresolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	executable := writeExecutableAt(t, filepath.Join(dir, "legacy-plugin"))
	digest := strings.Repeat("cd", 32)
	original := `{"plugins":["./legacy-plugin","missing-plugin"],"plugin_trust":{` +
		`"` + executable + `":"` + digest + `"` + `},"disabled_official_plugins":["sqlite"],"future":1}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []PluginConfig{{Path: "./legacy-plugin", SHA256: digest}, {Path: "missing-plugin"}}
	if len(config.Plugins) != len(want) || config.Plugins[0] != want[0] || config.Plugins[1] != want[1] {
		t.Fatalf("plugins = %#v, want %#v", config.Plugins, want)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Contains(text, "plugin_trust") || strings.Contains(text, "disabled_official_plugins") {
		t.Fatalf("legacy keys remain after migration: %s", data)
	}
	if !strings.Contains(text, `"future": 1`) || !strings.Contains(text, `"sha256": "`+digest+`"`) {
		t.Fatalf("migration lost data or pin: %s", data)
	}
}

func TestLoadConfig_malformedLegacyTrustFailsWithoutRewrite(t *testing.T) {
	path := writeConfigFile(t, `{"plugins":["missing"],"plugin_trust":{"relative":"bad"}}`)
	before, _ := os.ReadFile(path)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig = nil, want malformed trust error")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("malformed trust changed the config")
	}
}

func TestSavePlugin_storesExternalDescriptor(t *testing.T) {
	snapshotAppConfig(t)
	dir := t.TempDir()
	path := writeConfigFile(t, `{"plugins":[{"builtin":"sqlite"}]}`)
	executable := writeExecutableAt(t, filepath.Join(dir, "plugin"))
	digest := strings.Repeat("ef", 32)
	changed, err := SavePlugin(path, executable, digest)
	if err != nil || !changed {
		t.Fatalf("SavePlugin = %t, %v, want changed", changed, err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Plugins) != 2 || config.Plugins[1] != (PluginConfig{Path: executable, SHA256: digest}) {
		t.Fatalf("plugins = %#v, want appended descriptor", config.Plugins)
	}
}

func TestRemovePlugin_builtin(t *testing.T) {
	snapshotAppConfig(t)
	path := writeConfigFile(t, `{"plugins":[{"builtin":"sqlite"},{"builtin":"mysql"}]}`)
	entry, canonical, changed, err := RemovePlugin(path, "mysql")
	if err != nil || !changed || entry != "mysql" || canonical != "" {
		t.Fatalf("RemovePlugin = %q/%q/%t/%v, want builtin removal", entry, canonical, changed, err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0].Builtin != "sqlite" {
		t.Fatalf("plugins = %#v, want only sqlite", config.Plugins)
	}
}

func TestReadPluginTrust_neverMaterializesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "perk-workbench", "config.json")
	if trust := ReadPluginTrust(path); trust != nil {
		t.Fatalf("ReadPluginTrust = %v, want nil", trust)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ReadPluginTrust created %s", path)
	}
}
