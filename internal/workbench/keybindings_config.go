package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadKeybindings reads a JSON config file and returns Keybindings.
// If the file does not exist, it writes the default config file and
// returns the defaults.
func LoadKeybindings(path string) (Keybindings, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := writeDefaultConfig(path); err != nil {
				return Keybindings{}, fmt.Errorf("writing default keybindings config %q: %w", path, err)
			}
			return DefaultKeybindings(), nil
		}
		return Keybindings{}, fmt.Errorf("reading keybindings config %q: %w", path, err)
	}
	if len(contents) == 0 {
		return DefaultKeybindings(), nil
	}

	var raw map[string]any
	if err := json.Unmarshal(contents, &raw); err != nil {
		return Keybindings{}, fmt.Errorf("parsing keybindings config %q: %w", path, err)
	}

	flat, err := flattenConfig(raw)
	if err != nil {
		return Keybindings{}, fmt.Errorf("keybindings config %q: %w", path, err)
	}

	// Removed in favor of the persisted query-log pane.
	delete(flat, "query.save")
	delete(flat, "query.saved")

	b, err := NewKeybindings(flat)
	if err != nil {
		return Keybindings{}, fmt.Errorf("keybindings config %q: %w", path, err)
	}
	return b, nil
}

// flattenConfig converts a mixed flat/nested config map into the flat
// map[string][]string that NewKeybindings expects. Both formats work:
//
// Flat:   {"app.quit": ["q"], "browse.next_page": ["n"]}
// Nested: {"app": {"quit": ["q"]}, "browse": {"next_page": ["n"]}}
func flattenConfig(raw map[string]any) (map[string][]string, error) {
	flat := make(map[string][]string, len(raw))
	for key, val := range raw {
		switch v := val.(type) {
		case []any:
			// Flat format: "command_id": ["key1", "key2"]
			strs, err := anyToStrings(v)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}
			flat[key] = strs
		case map[string]any:
			// Nested format: "group": {"sub": ["key1"]}
			for subKey, subVal := range v {
				subArr, ok := subVal.([]any)
				if !ok {
					return nil, fmt.Errorf("key %q: expected array of strings, got %T", key+"."+subKey, subVal)
				}
				strs, err := anyToStrings(subArr)
				if err != nil {
					return nil, fmt.Errorf("key %q: %w", key+"."+subKey, err)
				}
				flat[key+"."+subKey] = strs
			}
		default:
			return nil, fmt.Errorf("key %q: expected array or object, got %T", key, val)
		}
	}
	return flat, nil
}

func anyToStrings(arr []any) ([]string, error) {
	strs := make([]string, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T at index %d", v, i)
		}
		strs[i] = s
	}
	return strs, nil
}

// MustLoadKeybindings is like LoadKeybindings but panics on error.
// Suitable for cmd/perk startup.
func MustLoadKeybindings(path string) Keybindings {
	b, err := LoadKeybindings(path)
	if err != nil {
		panic(fmt.Sprintf("keybindings: %v", err))
	}
	return b
}

// KeybindingsPath returns the default config file path in the user's
// XDG config directory.
func KeybindingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "perk-workbench", "keybindings.json")
}

// writeDefaultConfig writes all default command bindings as a nested JSON
// config file, grouped by command prefix (e.g. "query" → {"execute": ...}).
func writeDefaultConfig(path string) error {
	groups := make(map[string]map[string][]string)
	for _, d := range defaultDefs {
		id := string(d.id)
		dot := strings.IndexByte(id, '.')
		if dot < 0 {
			if groups[""] == nil {
				groups[""] = make(map[string][]string)
			}
			groups[""][id] = d.keys
			continue
		}
		prefix, suffix := id[:dot], id[dot+1:]
		if groups[prefix] == nil {
			groups[prefix] = make(map[string][]string)
		}
		groups[prefix][suffix] = d.keys
	}
	// json.MarshalIndent sorts object keys alphabetically, so the output
	// is deterministic.
	data, err := json.MarshalIndent(groups, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// keybindingsPath returns the default config file path (same as KeybindingsPath).
func keybindingsPath() string {
	return KeybindingsPath()
}

func stringsSuffix(s, suffix string) bool {
	return strings.HasSuffix(s, suffix)
}
