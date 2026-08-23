package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_keybinds_missing_uses_defaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "config.json")
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig on missing file: %v", err)
	}
	if len(config.Keybinds) != 0 {
		t.Fatalf("missing keybinds loaded overrides: %#v", config.Keybinds)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings without overrides: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "Ctrl+C" {
		t.Fatalf("DisplayKey(app.quit) = %q, want built-in Ctrl+C", got)
	}
}

func TestLoadConfig_keybinds_null_uses_defaults(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": null}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig with null keybinds: %v", err)
	}
	if len(config.Keybinds) != 0 {
		t.Fatalf("null keybinds loaded overrides: %#v", config.Keybinds)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings with null keybinds: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "Ctrl+C" {
		t.Fatalf("DisplayKey(app.quit) = %q, want built-in Ctrl+C", got)
	}
}

func TestLoadConfig_keybinds_flat_overrides(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {"app.quit": ["x"], "focus.schema": ["f1"]}}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "x" {
		t.Fatalf("app.quit key = %q, want x", got)
	}
	if got := b.DisplayKey("focus.schema"); got != "F1" {
		t.Fatalf("focus.schema key = %q, want F1", got)
	}
	// Non-overridden commands keep their defaults.
	if got := b.DisplayKey("query.cancel"); got != "Esc" {
		t.Fatalf("query.cancel key = %q, want default Esc", got)
	}
}

func TestLoadConfig_keybinds_nested_format(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {
		"query": {"execute": ["f9"], "cancel": ["q"]},
		"app": {"quit": ["ctrl+q", "ctrl+x"]}
	}}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig with nested keybinds: %v", err)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings with nested keybinds: %v", err)
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

func TestLoadConfig_keybinds_disable_command(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {"app.quit": []}}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "" {
		t.Fatalf("disabled app.quit should have no display key, got %q", got)
	}
}

func TestLoadConfig_keybinds_unknown_command(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {"does.not.exist": ["x"]}}`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "keybinds") {
		t.Fatalf("error %v does not mention keybinds", err)
	}
}

func TestLoadConfig_keybinds_invalid_keystroke(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {"app.quit": ["++bad"]}}`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for invalid keystroke, got nil")
	}
}

func TestLoadConfig_keybinds_invalid_value_type(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {"app.quit": 5}}`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for non-array keybind value, got nil")
	}
}

func TestLoadConfig_keybinds_null_entry_ignored(t *testing.T) {
	// Regression: nested null values (e.g. from a palette-only command
	// written by an older version of the placeholder file) must not fail.
	path := writeKeybindsConfig(t, `{"keybinds": {"ai": {"yolo_writes.toggle": null}, "app": {"quit": ["x"]}}}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig with null nested entry: %v", err)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings with null nested entry: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "x" {
		t.Fatalf("app.quit key = %q, want x", got)
	}
}

func TestLoadConfig_keybinds_ignoresRemovedCommands(t *testing.T) {
	path := writeKeybindsConfig(t, `{"keybinds": {"query": {"save": ["ctrl+k"], "saved": ["ctrl+o"]}, "chat": {"history": ["ctrl+h"]}, "browse": {"yank_cell": ["y"]}, "app": {"quit": ["x"]}}}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig with removed commands: %v", err)
	}
	b, err := NewKeybindings(config.Keybinds)
	if err != nil {
		t.Fatalf("NewKeybindings with removed commands: %v", err)
	}
	if got := b.DisplayKey("app.quit"); got != "x" {
		t.Fatalf("app.quit key = %q, want x", got)
	}
}

// writeKeybindsConfig writes a minimal config.json containing the given raw
// JSON merged alongside a plugins list (so plugin migration leaves it alone)
// and returns its path.
func writeKeybindsConfig(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "perk-workbench", "config.json")
	// Splice the raw object next to a plugins descriptor: raw must start
	// with "{" so its first key follows the injected one.
	if !strings.HasPrefix(raw, "{") {
		t.Fatalf("writeKeybindsConfig: raw %q must be a JSON object", raw)
	}
	contents := `{"plugins": [{"builtin": "sqlite"}], ` + raw[1:]
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
