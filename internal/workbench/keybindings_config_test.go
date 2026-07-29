package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeybindings_missing_file_returns_defaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "keybindings.json")
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings on missing file: %v", err)
	}
	// Should have populated commands (not zero-value).
	if b.DisplayKey("app.quit") == "" {
		t.Fatal("missing file loaded empty keybindings")
	}
	// Should have written the default config file.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("LoadKeybindings did not create the config file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated config: %v", err)
	}
	var config map[string]map[string][]string
	if err := json.Unmarshal(contents, &config); err != nil {
		t.Fatalf("parsing generated config: %v", err)
	}
	if _, ok := config["query_log"]["cursor_down"]; ok {
		t.Fatal("generated config includes fixed grid navigation")
	}
	if got := config["form"]["edit"]; len(got) != 1 || got[0] != "enter" {
		t.Fatalf("generated config form.edit = %#v, want [enter]", got)
	}
}

func TestLoadKeybindings_second_call_success(t *testing.T) {
	// Regression: first call writes the default config, second call reads it back.
	// The writer skips palette-only commands (keys: nil), so the file should never
	// contain null; this test verifies the full round-trip.
	path := filepath.Join(t.TempDir(), "nonexistent", "keybindings.json")
	if _, err := LoadKeybindings(path); err != nil {
		t.Fatalf("first call (create): %v", err)
	}

	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("second call (read back): %v", err)
	}
	if b.DisplayKey("app.quit") == "" {
		t.Fatal("second call loaded empty keybindings")
	}
}

func TestLoadKeybindings_null_nested_value_ignored(t *testing.T) {
	// Regression: existing config files with null values (e.g. from a palette-only
	// command written by an older version) must not cause errors.
	config := `{"ai":{"yolo_writes.toggle":null},"app":{"quit":["x"]}}`
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings with null nested value: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "x" {
		t.Fatalf("app.quit key = %q, want x", got)
	}
}

func TestLoadKeybindings_empty_config_returns_defaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings on empty config: %v", err)
	}
	if b.DisplayKey("app.quit") != "Ctrl+C" {
		t.Fatalf("empty config should use defaults, got DisplayKey(app.quit) = %q", b.DisplayKey("app.quit"))
	}
}

func TestLoadKeybindings_valid_overrides(t *testing.T) {
	config := `{"app.quit": ["x"], "focus.schema": ["f1"]}`
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings: %v", err)
	}
	if b.DisplayKey("app.quit") != "x" {
		t.Fatalf("app.quit key = %q, want x", b.DisplayKey("app.quit"))
	}
	if b.DisplayKey("focus.schema") != "F1" {
		t.Fatalf("focus.schema key = %q, want F1", b.DisplayKey("focus.schema"))
	}
}

func TestLoadKeybindings_disable_command(t *testing.T) {
	config := `{"app.quit": []}`
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings: %v", err)
	}
	if b.DisplayKey("app.quit") != "" {
		t.Fatalf("disabled app.quit should have no display key, got %q", b.DisplayKey("app.quit"))
	}
}

func TestLoadKeybindings_unknown_command(t *testing.T) {
	config := `{"does.not.exist": ["x"]}`
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadKeybindings(path)
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
}

func TestLoadKeybindings_ignoresRemovedSavedQueryBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"query":{"save":["ctrl+k"],"saved":["ctrl+o"]},"app.quit":["x"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "x" {
		t.Fatalf("app.quit key = %q, want x", got)
	}
}

func TestLoadKeybindings_invalid_json(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte("{invalid}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadKeybindings(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadKeybindings_invalid_keystroke(t *testing.T) {
	config := `{"app.quit": ["++bad"]}`
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadKeybindings(path)
	if err == nil {
		t.Fatal("expected error for invalid keystroke, got nil")
	}
}

func TestLoadKeybindings_nested_format(t *testing.T) {
	config := `{
		"query": {"execute": ["f9"], "cancel": ["q"]},
		"app": {"quit": ["ctrl+q", "ctrl+x"]}
	}`
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := LoadKeybindings(path)
	if err != nil {
		t.Fatalf("LoadKeybindings with nested config: %v", err)
	}
	if b.DisplayKey("query.execute") != "F9" {
		t.Fatalf("query.execute key = %q, want F9", b.DisplayKey("query.execute"))
	}
	if b.DisplayKey("app.quit") != "Ctrl+Q" {
		t.Fatalf("app.quit key = %q, want Ctrl+Q", b.DisplayKey("app.quit"))
	}
	if b.DisplayKey("query.cancel") != "q" {
		t.Fatalf("query.cancel key = %q, want q", b.DisplayKey("query.cancel"))
	}
}

func TestKeybindingsPath_in_xdg_config(t *testing.T) {
	path := keybindingsPath()
	if !filepath.IsAbs(path) {
		t.Fatalf("keybindingsPath() = %q, want absolute path", path)
	}
	if !stringsSuffix(path, "perk-workbench/keybindings.json") {
		t.Fatalf("keybindingsPath() = %q, want .../perk-workbench/keybindings.json", path)
	}
}
