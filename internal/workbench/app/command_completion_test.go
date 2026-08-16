package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// redisEditorModel returns a ready model whose query editor speaks the
// Redis language with the static command catalog, focused on the query
// tab with the editor in insert mode.
func redisEditorModel(t *testing.T) Model {
	t.Helper()
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus, model.Tab = focusWorkspace, tabQuery
	model.queryLanguage = testRedisLanguage
	model.queryLog.editor.setLanguage(testRedisLanguage)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	return updated.(Model)
}

// openCommandCompletion presses Ctrl+Space on a model whose query editor
// is in insert mode and returns the updated model.
func openCommandCompletion(model Model) Model {
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	return updated.(Model)
}

func TestCommandCompletion_filtersByTokenAtCursor(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("h")
	model.queryLog.editor.text.input.SetCursorColumn(1)

	model = openCommandCompletion(model)

	if !model.queryLog.editor.completion.visible() {
		t.Fatal("command completion should be visible after Ctrl+Space")
	}
	matches := model.queryLog.editor.completion.matches
	if len(matches) != 3 || matches[0].Label != "HGET" || matches[1].Label != "HGETALL" || matches[2].Label != "HSET" {
		t.Fatalf("matches for h = %+v, want [HGET HGETALL HSET]", matches)
	}
	if got := matches[0].Detail; got != "HGET key field" {
		t.Fatalf("HGET usage detail = %q, want the advertised usage", got)
	}
	if got := matches[0].Summary; got != "Get one field of the hash at key" {
		t.Fatalf("HGET summary = %q, want the advertised summary", got)
	}

	// Case-insensitive: the same catalog matches an uppercase token.
	model.queryLog.editor.setValue("HG")
	model.queryLog.editor.text.input.SetCursorColumn(2)
	model = openCommandCompletion(model)
	if !model.queryLog.editor.completion.visible() {
		t.Fatal("completion should be case-insensitive")
	}
	if got := model.queryLog.editor.completion.accept().Label; got != "HGET" {
		t.Fatalf("top match for HG = %q, want HGET", got)
	}
}

func TestCommandCompletion_exactMatchStillShowsHelp(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("HGETALL")
	model.queryLog.editor.text.input.SetCursorColumn(7)

	model = openCommandCompletion(model)

	// An exact command already in the buffer still opens its usage/help;
	// nothing is executed and the statement is untouched.
	if !model.queryLog.editor.completion.visible() {
		t.Fatal("exact command must still show its usage/help")
	}
	if got := model.queryLog.editor.completion.accept(); got.Label != "HGETALL" || got.Detail != "HGETALL key" {
		t.Fatalf("exact match = %+v, want HGETALL with usage", got)
	}
	if got := model.queryLog.editor.value; got != "HGETALL" {
		t.Fatalf("opening completion must not touch the statement, got %q", got)
	}
}

func TestCommandCompletion_acceptReplacesTokenAtStart(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("hg")
	model.queryLog.editor.text.input.SetCursorColumn(2)

	model = openCommandCompletion(model)
	model.queryLog.editor.completion.move(1) // HGETALL
	model.queryLog.editor.acceptVisibleCompletion()

	if got, want := model.queryLog.editor.value, "HGETALL "; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
	line, col := model.queryLog.editor.text.input.Line(), model.queryLog.editor.text.input.Column()
	if line != 0 || col != len("HGETALL ") {
		t.Fatalf("cursor at line %d col %d, want line 0 col %d", line, col, len("HGETALL "))
	}
}

func TestCommandCompletion_acceptReplacesTokenInMiddle(t *testing.T) {
	model := redisEditorModel(t)
	// Cursor between G and E of "hge" — inside the token, not at its end.
	model.queryLog.editor.setValue("hge")
	model.queryLog.editor.text.input.SetCursorColumn(1)

	model = openCommandCompletion(model)
	model.queryLog.editor.completion.move(1) // HGETALL
	model.queryLog.editor.acceptVisibleCompletion()

	if got, want := model.queryLog.editor.value, "HGETALL "; got != want {
		t.Fatalf("editor value = %q, want %q (whole token replaced)", got, want)
	}
	line, col := model.queryLog.editor.text.input.Line(), model.queryLog.editor.text.input.Column()
	if line != 0 || col != len("HGETALL ") {
		t.Fatalf("cursor at line %d col %d, want line 0 col %d", line, col, len("HGETALL "))
	}
}

func TestCommandCompletion_acceptPreservesMultilineStatement(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("GET user:1\nh\nPING")
	model.queryLog.editor.text.input.CursorUp() // line 1 (setValue leaves the cursor at the end)
	model.queryLog.editor.text.input.SetCursorColumn(1)

	model = openCommandCompletion(model)
	model.queryLog.editor.completion.move(1) // HGETALL
	model.queryLog.editor.acceptVisibleCompletion()

	if got, want := model.queryLog.editor.value, "GET user:1\nHGETALL \nPING"; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
	line, col := model.queryLog.editor.text.input.Line(), model.queryLog.editor.text.input.Column()
	if line != 1 || col != len("HGETALL ") {
		t.Fatalf("cursor at line %d col %d, want line 1 col %d", line, col, len("HGETALL "))
	}
}

func TestCommandCompletion_cancelChangesNothing(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("h")
	model.queryLog.editor.text.input.SetCursorColumn(1)
	model = openCommandCompletion(model)

	// Escape dismisses the overlay (and exits insert mode) without
	// touching the statement.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.queryLog.editor.completion.visible() {
		t.Fatal("completion still visible after Escape")
	}
	if got := model.queryLog.editor.value; got != "h" {
		t.Fatalf("editor value after cancel = %q, want unchanged", got)
	}

	// Enter with no matches is a no-op too.
	model = redisEditorModel(t)
	model.queryLog.editor.setValue("zzz")
	model.queryLog.editor.text.input.SetCursorColumn(3)
	model = openCommandCompletion(model)
	if model.queryLog.editor.completion.visible() {
		t.Fatal("no token should match any command")
	}
	model.queryLog.editor.acceptVisibleCompletion()
	if got := model.queryLog.editor.value; got != "zzz" {
		t.Fatalf("editor value after empty accept = %q, want unchanged", got)
	}
}

func TestCommandCompletion_refiltersByTokenWhileTyping(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("h")
	model.queryLog.editor.text.input.SetCursorColumn(1)
	model = openCommandCompletion(model)

	// Typing narrows the visible matches like the SQL overlay.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	if !model.queryLog.editor.completion.visible() {
		t.Fatal("completion should stay visible after typing")
	}
	if got := model.queryLog.editor.completion.accept().Label; got != "HGET" {
		t.Fatalf("top match after typing g = %q, want HGET", got)
	}
}

func TestCommandCompletion_noAssistanceFallback(t *testing.T) {
	// A non-SQL language without a command catalog leaves Ctrl+Space a
	// no-op: the overlay stays closed and the statement untouched.
	model := redisEditorModel(t)
	model.queryLanguage = sharedsqlQueryLanguage("KV")
	model.queryLog.editor.setLanguage(sharedsqlQueryLanguage("KV"))
	model.queryLog.editor.setValue("h")
	model.queryLog.editor.text.input.SetCursorColumn(1)

	model = openCommandCompletion(model)
	if model.queryLog.editor.completion.visible() {
		t.Fatal("completion opened for a language without commands")
	}
	if got := model.queryLog.editor.value; got != "h" {
		t.Fatalf("editor value = %q, want untouched", got)
	}

	// Outside insert mode Ctrl+Space does nothing even with a catalog.
	model = redisEditorModel(t)
	model.queryLog.editor.setValue("h")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // exit insert
	model = updated.(Model)
	model = openCommandCompletion(model)
	if model.queryLog.editor.completion.visible() {
		t.Fatal("completion opened outside insert mode")
	}
	if got := model.queryLog.editor.value; got != "h" {
		t.Fatalf("editor value = %q, want untouched", got)
	}
}

func TestCommandCompletion_mouseSelection(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("h")
	model.queryLog.editor.text.input.SetCursorColumn(1)
	model = openCommandCompletion(model)

	// Locate the HGETALL row in the rendered overlay and click it; the
	// press selects and accepts, like Enter on the keyboard. The view
	// line index is the screen y; the click handler converts it to
	// content coordinates itself.
	view := ansi.Strip(model.View().Content)
	overlayRow := -1
	for i, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "HGETALL key") {
			overlayRow = i
			break
		}
	}
	if overlayRow < 0 {
		t.Fatalf("rendered view has no HGETALL overlay row:\n%s", view)
	}
	x := model.layout.schemaWidth + 2 // just inside the overlay box
	updated, _ := model.Update(tea.MouseClickMsg{X: x, Y: overlayRow, Button: tea.MouseLeft})
	model = updated.(Model)

	if got, want := model.queryLog.editor.value, "HGETALL "; got != want {
		t.Fatalf("editor value after mouse accept = %q, want %q", got, want)
	}
	line, col := model.queryLog.editor.text.input.Line(), model.queryLog.editor.text.input.Column()
	if line != 0 || col != len("HGETALL ") {
		t.Fatalf("cursor at line %d col %d, want line 0 col %d", line, col, len("HGETALL "))
	}

	// The trailing release must not re-accept.
	updated, _ = model.Update(tea.MouseReleaseMsg{X: x, Y: overlayRow, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.queryLog.editor.value; got != "HGETALL " {
		t.Fatalf("editor value after release = %q, want unchanged", got)
	}
}

func TestCommandCompletion_mouseClickOutsideOverlayFallsThrough(t *testing.T) {
	model := redisEditorModel(t)
	model.queryLog.editor.setValue("h")
	model.queryLog.editor.text.input.SetCursorColumn(1)
	model = openCommandCompletion(model)

	// A click far below the overlay (the results pane) changes nothing.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + 2, Y: 20, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.queryLog.editor.value; got != "h" {
		t.Fatalf("editor value after outside click = %q, want untouched", got)
	}
}

func TestCommandCompletion_footerCue(t *testing.T) {
	// The query pane status line advertises the completion key when the
	// active language has a command catalog.
	model := redisEditorModel(t)
	view := ansi.Strip(model.queryPaneView())
	if !strings.Contains(view, "^space complete") {
		t.Fatalf("query pane = %q, want the ^space complete cue", view)
	}

	// The legacy SQL language carries no cue.
	model = resizeModel(readyModel(t), 100, 24)
	model.Focus, model.Tab = focusWorkspace, tabQuery
	view = ansi.Strip(model.queryPaneView())
	if strings.Contains(view, "complete") {
		t.Fatalf("SQL query pane = %q, want no completion cue", view)
	}
}

func TestKeybindings_queryCompleteDefaultAndOverride(t *testing.T) {
	bindings := DefaultKeybindings()
	if got := bindings.DisplayKey("query.complete"); got != "Ctrl+Space" {
		t.Fatalf("query.complete default key = %q, want Ctrl+Space", got)
	}
	press := tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}
	if !bindings.Match(press, "query.complete", []scope{scopeEditor, scopeForm, scopeView, scopeGlobal}) {
		t.Fatal("query.complete must match Ctrl+Space in the editor scope")
	}

	rebound, err := NewKeybindings(map[string][]string{"query.complete": {"ctrl+k"}})
	if err != nil {
		t.Fatalf("rebinding query.complete: %v", err)
	}
	if got := rebound.DisplayKey("query.complete"); got != "Ctrl+K" {
		t.Fatalf("rebound query.complete key = %q, want Ctrl+K", got)
	}
	if rebound.Match(press, "query.complete", []scope{scopeEditor}) {
		t.Fatal("rebound query.complete must not match Ctrl+Space")
	}
	if !rebound.Match(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, "query.complete", []scope{scopeEditor}) {
		t.Fatal("rebound query.complete must match Ctrl+K")
	}

	disabled, err := NewKeybindings(map[string][]string{"query.complete": {}})
	if err != nil {
		t.Fatalf("disabling query.complete: %v", err)
	}
	if got := disabled.DisplayKey("query.complete"); got != "" {
		t.Fatalf("disabled query.complete key = %q, want none", got)
	}
}

// sharedsqlQueryLanguage builds a minimal non-SQL advertisement without
// a command catalog for the fallback tests.
func sharedsqlQueryLanguage(name string) sharedsql.QueryLanguage {
	return sharedsql.QueryLanguage{
		Name: name, EditorLabel: "Command", Placeholder: "Enter a statement…",
	}
}
