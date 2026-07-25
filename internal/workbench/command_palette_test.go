package workbench

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCommandPalette_navigationAndSelection(t *testing.T) {
	palette := commandPalette{
		filtered: []commandPaletteItem{
			{id: "first"},
			{id: "second"},
		},
		visible: true,
	}

	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 1},
		{key: tea.KeyPressMsg{Code: tea.KeyUp}, want: 0},
		{key: tea.KeyPressMsg{Code: tea.KeyDown}, want: 1},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 0},
	} {
		_, _, consumed := palette.handleKey(test.key)
		if !consumed {
			t.Fatal("navigation key was not consumed")
		}
		if palette.cursor != test.want {
			t.Fatalf("cursor = %d, want %d", palette.cursor, test.want)
		}
	}

	_, _, _ = palette.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	selected, closed, consumed := palette.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !consumed || closed {
		t.Fatal("enter was not handled as a palette selection")
	}
	if selected.id != "second" {
		t.Fatalf("selected command = %q, want %q", selected.id, "second")
	}
	if palette.visible {
		t.Fatal("palette remained visible after selection")
	}
}

func TestModelCommandPalette_opensThemePicker(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl, Text: "p"})
	model = updated.(Model)
	if !model.commandPalette.visible {
		t.Fatal("ctrl+p did not open the palette")
	}

	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 1},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 0},
	} {
		updated, _ = model.Update(test.key)
		model = updated.(Model)
		if model.commandPalette.cursor != test.want {
			t.Fatalf("cursor = %d, want %d", model.commandPalette.cursor, test.want)
		}
	}

	for _, character := range "theme" {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.commandPalette.visible || model.themePicker == nil {
		t.Fatal("theme selection did not open the theme picker")
	}
	if activeTheme != original {
		t.Fatalf("theme = %q, want %q", activeTheme, original)
	}
}

func TestThemePicker_previewsAndCancelsTheme(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen)
	model.themePicker = newThemePicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if activeTheme != themeNord {
		t.Fatalf("previewed theme = %q, want %q", activeTheme, themeNord)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.themePicker != nil {
		t.Fatal("theme picker remained visible after cancel")
	}
	if activeTheme != original {
		t.Fatalf("restored theme = %q, want %q", activeTheme, original)
	}
}

func TestThemePicker_commitsPreviewedTheme(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen)
	model.themePicker = newThemePicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.themePicker != nil {
		t.Fatal("theme picker remained visible after selection")
	}
	if activeTheme != themeNord {
		t.Fatalf("committed theme = %q, want %q", activeTheme, themeNord)
	}
}
