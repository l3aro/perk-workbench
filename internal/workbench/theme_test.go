package workbench

import (
	"context"
	"testing"
)

func TestPaletteThemeCommandsApplySharedPalette(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen, false)
	for _, test := range []struct {
		command CommandID
		theme   appTheme
		accent  string
	}{
		{"theme.dracula", themeDracula, "#bd93f9"},
		{"theme.catppuccin", themeCatppuccin, "#cba6f7"},
		{"theme.ocean", themeOcean, "#55d6be"},
		{"theme.nord", themeNord, "#88c0d0"},
		{"theme.monokai", themeMonokai, "#a6e22e"},
		{"theme.solarized", themeSolarized, "#268bd2"},
	} {
		updated, _ := model.handlePaletteCommand(test.command)
		model = updated.(Model)
		if activeTheme != test.theme || colorAccent != test.accent {
			t.Fatalf("command %q applied theme %q with accent %q, want %q with %q", test.command, activeTheme, colorAccent, test.theme, test.accent)
		}
	}
}
