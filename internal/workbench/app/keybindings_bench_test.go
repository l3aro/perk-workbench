package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

var (
	benchmarkAppKeybindingsMatchSink   bool
	benchmarkAppKeybindingsResolveSink string
	benchmarkAppKeybindingsHitSink     bool
)

func BenchmarkKeybindings(b *testing.B) {
	bindings := DefaultKeybindings()
	key := tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift}
	scopes := []scope{scopeEditor, scopeForm, scopeView, scopeGlobal}
	command := CommandID("workspace.tab_next")

	b.ReportAllocs()
	for b.Loop() {
		benchmarkAppKeybindingsMatchSink = bindings.Match(key, command, scopes)
		benchmarkAppKeybindingsResolveSink, benchmarkAppKeybindingsHitSink = bindings.ResolveAny(key, scopes)
	}
}
