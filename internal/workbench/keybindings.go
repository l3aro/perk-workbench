package workbench

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// CommandID is a stable identifier for an application keyboard command.
type CommandID string

type scope int

const (
	scopeGlobal scope = iota
	scopeView         // pane/view scoped (structure tab, browse tab, query log, etc.)
	scopeForm         // form/modal scoped (edit forms, confirmation dialogs)
)

func (s scope) String() string {
	switch s {
	case scopeGlobal:
		return "global"
	case scopeView:
		return "view"
	case scopeForm:
		return "form"
	default:
		return "unknown"
	}
}

type commandDef struct {
	id    CommandID
	scope scope
	keys  []string // canonical keystroke strings (from KeyPressMsg.Keystroke())
	label string   // human-readable label for help/footer
}

// Keybindings is an immutable registry of application keyboard bindings.
type Keybindings struct {
	commands map[CommandID]commandDef
	index    map[scope]map[string][]CommandID // scope -> canonical stroke -> candidate commands
}

// DefaultKeybindings returns the built-in default bindings.
func DefaultKeybindings() Keybindings {
	return defaultBindings
}

// NewKeybindings creates a registry by merging overrides over defaults.
// An empty slice disables a command. Unknown command IDs, invalid keystrokes,
// and same-scope duplicates return an error.
func NewKeybindings(overrides map[string][]string) (Keybindings, error) {
	// Build a fresh copy of defaults from the defs list (no ref to defaultBindings).
	base, err := buildFromDefs(defaultDefs)
	if err != nil {
		return Keybindings{}, err
	}

	for rawID, rawKeys := range overrides {
		id := CommandID(rawID)
		def, ok := base.commands[id]
		if !ok {
			return Keybindings{}, fmt.Errorf("unknown command ID %q", id)
		}

		parsed := make([]string, 0, len(rawKeys))
		for _, raw := range rawKeys {
			canon, err2 := normalizeKeystroke(raw)
			if err2 != nil {
				return Keybindings{}, fmt.Errorf("command %q: invalid keystroke %q: %w", id, raw, err2)
			}
			parsed = append(parsed, canon)
		}
		def.keys = parsed
		base.commands[id] = def
	}

	// Build index from base.
	idx := map[scope]map[string][]CommandID{}
	for id, def := range base.commands {
		if idx[def.scope] == nil {
			idx[def.scope] = map[string][]CommandID{}
		}
		for _, stroke := range def.keys {
			idx[def.scope][stroke] = append(idx[def.scope][stroke], id)
		}
	}

	return Keybindings{commands: base.commands, index: idx}, nil
}

// buildFromDefs creates a Keybindings from a literal def list without referencing
// defaultBindings (breaks the init cycle).
func buildFromDefs(defs []commandDef) (Keybindings, error) {
	cmds := make(map[CommandID]commandDef, len(defs))
	for _, d := range defs {
		keys := make([]string, len(d.keys))
		for i, key := range d.keys {
			canonical, err := normalizeKeystroke(key)
			if err != nil {
				return Keybindings{}, fmt.Errorf("command %q: invalid keystroke %q: %w", d.id, key, err)
			}
			keys[i] = canonical
		}
		cmds[d.id] = commandDef{id: d.id, scope: d.scope, keys: keys, label: d.label}
	}

	idx := map[scope]map[string][]CommandID{}
	for id, def := range cmds {
		if idx[def.scope] == nil {
			idx[def.scope] = map[string][]CommandID{}
		}
		for _, stroke := range def.keys {
			idx[def.scope][stroke] = append(idx[def.scope][stroke], id)
		}
	}

	return Keybindings{commands: cmds, index: idx}, nil
}

// replaceCommand returns a copy with the given command's keys replaced.
// Used for testing. Returns the same instance if the command does not exist.
func (b Keybindings) replaceCommand(id CommandID, keys []string) Keybindings {
	if _, ok := b.commands[id]; !ok {
		return b
	}
	overrides := map[string][]string{string(id): keys}
	updated, err := NewKeybindings(overrides)
	if err != nil {
		panic(err)
	}
	return updated
}

// resolve finds the command for a canonical keystroke in the given scope
// priority order (first match wins). Returns ("", false) if unmatched.
// Note: this returns only the first candidate per scope when multiple
// commands share the same key (e.g. "d" in scopeView for delete actions).
// Real dispatch should use Match() with a specific command ID.
func (b Keybindings) resolve(stroke string, scopes []scope) (string, bool) {
	for _, s := range scopes {
		if b.index == nil {
			continue
		}
		candidates, ok := b.index[s][stroke]
		if !ok || len(candidates) == 0 {
			continue
		}
		return string(candidates[0]), true
	}
	return "", false
}

// Match checks whether a key press triggers the given command in the
// given scope priority order.
func (b Keybindings) Match(msg tea.KeyPressMsg, id CommandID, scopes []scope) bool {
	for _, stroke := range keyStrokes(msg) {
		for _, s := range scopes {
			candidates, ok := b.index[s][stroke]
			if !ok {
				continue
			}
			for _, candidate := range candidates {
				if candidate == id {
					return true
				}
			}
		}
	}
	return false
}

// ResolveAny finds any command matching a key press in the given scopes.
// Returns ("", false) if unmatched. Prefer Match for specific commands.
func (b Keybindings) ResolveAny(msg tea.KeyPressMsg, scopes []scope) (string, bool) {
	for _, stroke := range keyStrokes(msg) {
		for _, s := range scopes {
			candidates, ok := b.index[s][stroke]
			if !ok || len(candidates) == 0 {
				continue
			}
			return string(candidates[0]), true
		}
	}
	return "", false
}

func keyStrokes(msg tea.KeyPressMsg) []string {
	text, stroke := msg.String(), msg.Keystroke()
	if text == stroke {
		return []string{text}
	}
	return []string{text, stroke}
}

// canonical modifier order matching Bubble Tea v2 Keystroke() output.
var canonicalMods = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

// validNamedKeys are non-rune key names that Keystroke() emits.
var validNamedKeys = map[string]bool{
	"enter": true, "escape": true, "esc": true, "tab": true, "space": true,
	"backspace": true, "up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pgup": true, "pgdown": true,
	"insert": true, "delete": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true,
	"f6": true, "f7": true, "f8": true, "f9": true, "f10": true,
	"f11": true, "f12": true, "f13": true, "f14": true, "f15": true,
	"f16": true, "f17": true, "f18": true, "f19": true, "f20": true,
}

// normalizeKeystroke accepts a user-supplied key string and returns the
// canonical form matching tea.KeyPressMsg.Keystroke() output.
func normalizeKeystroke(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty keystroke")
	}

	parts := strings.Split(raw, "+")
	if len(parts) == 0 {
		return "", fmt.Errorf("empty keystroke")
	}

	// Last part is the key; everything before is modifiers.
	key := parts[len(parts)-1]
	keyLower := strings.ToLower(key)
	modStrs := make([]string, 0, len(parts)-1)
	for _, p := range parts[:len(parts)-1] {
		p = strings.ToLower(p)
		if p == "" {
			return "", fmt.Errorf("empty modifier in keystroke %q", raw)
		}
		modStrs = append(modStrs, p)
	}

	// Validate key.
	isRune := len([]rune(keyLower)) == 1
	if !isRune && !validNamedKeys[keyLower] {
		return "", fmt.Errorf("unknown key %q in keystroke %q", parts[len(parts)-1], raw)
	}
	if isRune && len(modStrs) > 0 {
		// No modifier order enforcement for single runes; just verify key is ascii-ish.
		r := []rune(key)[0]
		if r < 32 || r > 0x10FFFF {
			return "", fmt.Errorf("invalid rune %q in keystroke %q", key, raw)
		}
	}

	// Validate modifiers.
	for _, m := range modStrs {
		valid := false
		for _, cm := range canonicalMods {
			if m == cm {
				valid = true
				break
			}
		}
		if !valid {
			return "", fmt.Errorf("unknown modifier %q in keystroke %q", m, raw)
		}
	}

	// Check for duplicate modifiers.
	seen := map[string]bool{}
	for _, m := range modStrs {
		if seen[m] {
			return "", fmt.Errorf("duplicate modifier %q in keystroke %q", m, raw)
		}
		seen[m] = true
	}

	// Map "escape" / "ESCAPE" / "Escape" to "esc" to match Keystroke() output.
	if keyLower == "escape" {
		key = "esc"
	}

	// Sort modifiers into canonical order.
	sort.Slice(modStrs, func(i, j int) bool {
		ri, rj := 0, 0
		for k, cm := range canonicalMods {
			if modStrs[i] == cm {
				ri = k
			}
			if modStrs[j] == cm {
				rj = k
			}
		}
		return ri < rj
	})

	if len(modStrs) > 0 {
		return strings.Join(append(modStrs, key), "+"), nil
	}
	return key, nil
}
