package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKeybindings_defaults_include_all_commands(t *testing.T) {
	b := DefaultKeybindings()
	// Every registered command must have at least one key in its primary scope.
	for id, cmd := range b.commands {
		if len(cmd.keys) == 0 {
			t.Errorf("command %q has zero default keys", id)
		}
	}
}

func TestKeybindings_defaults_resolve_registered_commands(t *testing.T) {
	b := DefaultKeybindings()
	// Test only queries with unique keys within their scope.
	tests := []struct {
		name    string
		stroke  string
		scopes  []scope
		want    string
		wantHit bool
	}{
		{name: "app.quit via ctrl+c", stroke: "ctrl+c", scopes: []scope{scopeGlobal}, want: "app.quit", wantHit: true},
		{name: "query.execute via f5", stroke: "f5", scopes: []scope{scopeGlobal}, want: "query.execute", wantHit: true},
		{name: "focus.schema via 1", stroke: "1", scopes: []scope{scopeGlobal}, want: "focus.schema", wantHit: true},
		{name: "editor.external via ctrl+e", stroke: "ctrl+e", scopes: []scope{scopeGlobal}, want: "editor.external", wantHit: true},
		{name: "focus.cycle_forward via tab", stroke: "tab", scopes: []scope{scopeGlobal}, want: "focus.cycle_forward", wantHit: true},
		{name: "focus.toggle_fullscreen via f", stroke: "f", scopes: []scope{scopeGlobal}, want: "focus.toggle_fullscreen", wantHit: true},
		{name: "form.save via ctrl+s", stroke: "ctrl+s", scopes: []scope{scopeForm}, want: "form.save", wantHit: true},
		{name: "form.discard via esc", stroke: "esc", scopes: []scope{scopeForm}, want: "form.discard", wantHit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hit := b.resolve(tt.stroke, tt.scopes)
			if got != tt.want || hit != tt.wantHit {
				t.Errorf("resolve(%q, %v) = (%q, %t), want (%q, %t)", tt.stroke, tt.scopes, got, hit, tt.want, tt.wantHit)
			}
		})
	}
}

func TestKeybindings_replaceCommand_replaces_defaults(t *testing.T) {
	b := DefaultKeybindings()
	b = b.replaceCommand("app.quit", []string{"x"})

	// "q" should no longer resolve to app.quit in any scope.
	got, hit := b.resolve("q", []scope{scopeGlobal})
	if hit {
		t.Errorf("resolve(q) unexpectedly hit command %q", got)
	}

	// "x" should now resolve to app.quit.
	got, hit = b.resolve("x", []scope{scopeGlobal})
	if !hit || got != "app.quit" {
		t.Errorf("resolve(x) = (%q, %t), want (app.quit, true)", got, hit)
	}
}

func TestKeybindings_disableCommand_removes_all_bindings(t *testing.T) {
	b := DefaultKeybindings()
	b = b.replaceCommand("app.quit", []string{})

	got, hit := b.resolve("q", []scope{scopeGlobal})
	if hit {
		t.Errorf("resolve(q) unexpectedly hit command %q after disable", got)
	}
	got, hit = b.resolve("ctrl+c", []scope{scopeGlobal})
	if hit {
		t.Errorf("resolve(ctrl+c) unexpectedly hit command %q after disable", got)
	}
}

func TestKeybindings_unknown_command_returns_error(t *testing.T) {
	_, err := NewKeybindings(map[string][]string{"does.not.exist": {"x"}})
	if err == nil {
		t.Fatal("expected error for unknown command ID, got nil")
	}
}

func TestKeybindings_invalid_keystroke_returns_error(t *testing.T) {
	_, err := NewKeybindings(map[string][]string{"app.quit": {"++invalid", "ctrl++", "+x"}})
	if err == nil {
		t.Fatal("expected error for invalid keystroke, got nil")
	}
}

func TestKeybindings_duplicate_key_same_scope_allowed_different_contexts(t *testing.T) {
	// Commands in the same scope may share keys because they're active
	// in mutually exclusive states (e.g. "d" for delete in different tabs,
	// "y" for yank in query log vs detail overlay).
	b := DefaultKeybindings()
	// Both exist in scopeView without panicking.
	// Just verify they were registered without panic.
	if _, hit := b.resolve("d", []scope{scopeView}); !hit {
		t.Fatal("expected key 'd' to resolve in scope view")
	}
}

func TestKeybindings_cross_scope_precedence(t *testing.T) {
	b := DefaultKeybindings()

	// In form scope, "enter" should resolve form.edit (not a global/view command).
	got, hit := b.resolve("enter", []scope{scopeForm, scopeView, scopeGlobal})
	if !hit {
		t.Fatal("resolve(enter, [form,view,global]) missed")
	}
	// form.edit is the form-scope command bound to enter.
	if got != "form.edit" && got != "browse.edit" && got != "structure.edit" {
		t.Errorf("resolve(enter, [form,view,global]) = %q, expected a form.edit variant", got)
	}

	// In view scope without form, "enter" should resolve a view command.
	got, hit = b.resolve("enter", []scope{scopeView})
	if !hit {
		t.Fatal("resolve(enter, [view]) missed")
	}
}

func TestKeybindings_keystroke_matches(t *testing.T) {
	b := DefaultKeybindings()

	// The Keystroke method is how Bubble Tea v2 represents keys at runtime.
	// Verify our bindings match against real KeyPressMsg values.
	tests := []struct {
		name    string
		msg     tea.KeyPressMsg
		wantID  string
		wantHit bool
	}{
		{name: "q does not quit", msg: tea.KeyPressMsg{Code: 'q'}, wantID: "", wantHit: false},
		{name: "query.execute via f5", msg: tea.KeyPressMsg{Code: tea.KeyF5}, wantID: "query.execute", wantHit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stroke := tt.msg.Keystroke()
			got, hit := b.resolve(stroke, []scope{scopeGlobal})
			if got != tt.wantID || hit != tt.wantHit {
				t.Errorf("resolve(%q) = (%q, %t), want (%q, %t)", stroke, got, hit, tt.wantID, tt.wantHit)
			}
		})
	}
}

func TestKeybindings_shiftedTabKeysMatchRuntimeEvents(t *testing.T) {
	bindings := DefaultKeybindings()
	tests := []struct {
		key tea.KeyPressMsg
		id  CommandID
	}{
		{key: tea.KeyPressMsg{Code: 'h', Text: "H", Mod: tea.ModShift}, id: "workspace.tab_prev"},
		{key: tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift}, id: "workspace.tab_next"},
	}

	for _, test := range tests {
		if !bindings.Match(test.key, test.id, []scope{scopeView}) {
			t.Errorf("%s did not match %q", test.key.Keystroke(), test.id)
		}
	}
}

func TestKeybindings_uppercaseOverrideMatchesShiftedRuntimeEvent(t *testing.T) {
	bindings, err := NewKeybindings(map[string][]string{"workspace.tab_next": {"L"}})
	if err != nil {
		t.Fatalf("creating keybindings: %v", err)
	}

	key := tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift}
	if !bindings.Match(key, "workspace.tab_next", []scope{scopeView}) {
		t.Errorf("%s did not match uppercase L override", key.Keystroke())
	}
}

func TestKeybindings_displayKey_first_alias(t *testing.T) {
	b := DefaultKeybindings()
	key := b.DisplayKey("query.execute")
	if key == "" {
		t.Fatal("DisplayKey(query.execute) returned empty string")
	}
}
