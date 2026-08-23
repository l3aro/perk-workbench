package app

import (
	"encoding/json"
	"fmt"
)

// Keybinds holds manual keybinding overrides from the config.json
// "keybinds" object. Every command not listed here keeps its built-in
// default binding; an empty slice disables a command. Nothing is
// materialized to disk — users add overrides by hand.
type Keybinds map[string][]string

// UnmarshalJSON parses the "keybinds" value. Both formats work:
//
// Flat:   {"keybinds": {"app.quit": ["q"], "browse.next_page": ["n"]}}
// Nested: {"keybinds": {"app": {"quit": ["q"]}, "browse": {"next_page": ["n"]}}}
//
// Null values (flat entries or nested groups) are ignored.
func (k *Keybinds) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*k = nil
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	flat, err := flattenKeybindConfig(raw)
	if err != nil {
		return err
	}

	// Removed in favor of the persisted query-log pane.
	delete(flat, "query.save")
	delete(flat, "query.saved")

	// Removed: AI chat commands moved to chat slash commands.
	delete(flat, "chat.history")

	// Renamed: merged into cell.yank.
	delete(flat, "browse.yank_cell")

	*k = flat
	return nil
}

// flattenKeybindConfig converts a mixed flat/nested keybind map into the
// flat map[string][]string that NewKeybindings expects. Both formats work:
//
// Flat:   {"app.quit": ["q"], "browse.next_page": ["n"]}
// Nested: {"app": {"quit": ["q"]}, "browse": {"next_page": ["n"]}}
func flattenKeybindConfig(raw map[string]any) (map[string][]string, error) {
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
				if subVal == nil {
					continue
				}
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

// validateKeybinds checks that every override names a known command and
// only uses valid keystrokes. Called from LoadConfig so a bad entry fails
// startup with the offending config path.
func validateKeybinds(binds Keybinds) error {
	_, err := NewKeybindings(binds)
	return err
}
