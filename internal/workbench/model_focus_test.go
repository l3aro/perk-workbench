package workbench

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestView_editor_cursor_accounts_for_rendered_layout(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		wantX, wantY  int
	}{
		{name: "wide", width: 100, height: 24, wantX: 36, wantY: 2},
		{name: "compact", width: 80, height: 24, wantX: 8, wantY: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.state = stateReady

			// When
			updated, _ := model.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			view := updated.(Model).View()

			// Then
			if view.Cursor == nil {
				t.Fatal("view cursor = nil, want editor cursor")
			}
			if got := view.Cursor.Position.X; got != test.wantX {
				t.Errorf("cursor X = %d, want %d", got, test.wantX)
			}
			if got := view.Cursor.Position.Y; got != test.wantY {
				t.Errorf("cursor Y = %d, want %d", got, test.wantY)
			}
		})
	}
}

func TestFocus_cycles_exclusively_and_routes_input_to_the_owner(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.state = stateReady
	model.editor.textarea.SetValue("select ")

	// When
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusEditor)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusResults)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.textarea.Value(); got != "select " {
		t.Fatalf("unfocused editor value = %q, want %q", got, "select ")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusSchema)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.textarea.Value(); got != "select " {
		t.Fatalf("unfocused editor value after schema key = %q, want %q", got, "select ")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusEditor)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusSchema)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusResults)
}

func TestFocus_editor_keeps_q_as_text_after_input_starts(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.state = stateReady
	model.editor.textarea.SetValue("select ")

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("editor q returned a root quit command")
	}
	if got := model.editor.textarea.Value(); got != "select q" {
		t.Fatalf("editor value = %q, want %q", got, "select q")
	}
}

func assertFocus(t *testing.T, model Model, want focus) {
	t.Helper()
	if model.focus != want {
		t.Fatalf("focus = %v, want %v", model.focus, want)
	}
	if got := model.editor.textarea.Focused(); got != (want == focusEditor) {
		t.Fatalf("editor focused = %t, want %t", got, want == focusEditor)
	}
	if got := model.results.Focused(); got != (want == focusResults) {
		t.Fatalf("results focused = %t, want %t", got, want == focusResults)
	}
}
