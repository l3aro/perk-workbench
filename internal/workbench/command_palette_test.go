package workbench

import (
	"context"
	"strings"
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

func TestCommandPalette_filterNoDuplicates(t *testing.T) {
	// Regression: filtered and items must not share a backing array.
	// When they do, each applyFilter call corrupts p.items while ranging,
	// producing duplicate entries as the query grows.
	items := []commandPaletteItem{
		{id: "app.quit", label: "quit with confirm"},
		{id: "workspace.tab_next", label: "next tab"},
		{id: "theme.select", label: "theme"},
		{id: "focus.schema", label: "schema"},
	}
	palette := &commandPalette{
		items:    items,
		filtered: append([]commandPaletteItem{}, items...),
	}

	// Type "th" one character at a time, checking no duplicates after each.
	for _, char := range []rune{'t', 'h'} {
		palette.query = append(palette.query, char)
		palette.applyFilter()
		seen := map[string]bool{}
		for _, item := range palette.filtered {
			if seen[item.label] {
				t.Fatalf("after typing %q: duplicate label %q in filtered list",
					string(palette.query), item.label)
			}
			seen[item.label] = true
			if !strings.Contains(strings.ToLower(item.label), strings.ToLower(string(palette.query))) {
				t.Fatalf("after typing %q: item %q does not match query",
					string(palette.query), item.label)
			}
		}
	}

	// After "th": "quit with confirm" (contains "th" in "with"), "theme" — exactly 2 items.
	if len(palette.filtered) != 2 {
		t.Fatalf("after typing \"th\": got %d items, want 2", len(palette.filtered))
	}
}

func TestModelCommandPalette_opensThemePicker(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen, false)
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

	model := New("", context.Background(), testOpen, false)
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

	model := New("", context.Background(), testOpen, false)
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
