package workbench

import (
	"context"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

func TestPaletteThemeCommandsApplySharedPalette(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen, false)
	for _, test := range []struct {
		command CommandID
		theme   appTheme
		accent  string
		title   string
		confirm string
		focused string
	}{
		{"theme.dracula", themeDracula, "#bd93f9", "#ff79c6", "#ff5555", "#50fa7b"},
		{"theme.catppuccin", themeCatppuccin, "#cba6f7", "#f9e2af", "#f38ba8", "#a6e3a1"},
		{"theme.ocean", themeOcean, "#94e2d5", "#89b4fa", "#f38ba8", "#f9e2af"},
		{"theme.nord", themeNord, "#88c0d0", "#ebcb8b", "#bf616a", "#a3be8c"},
		{"theme.monokai", themeMonokai, "#a6e22e", "#f92672", "#f92672", "#a6e22e"},
		{"theme.solarized", themeSolarized, "#268bd2", "#d33682", "#dc322f", "#859900"},
	} {
		updated, _ := model.handlePaletteCommand(test.command)
		model = updated.(Model)
		if activeTheme != test.theme || colorAccent != test.accent || colorTitle != test.title || colorConfirm != test.confirm || colorFocused != test.focused {
			t.Fatalf("command %q applied theme %q with accent %q, title %q, confirm %q, focused %q; want %q with %q, %q, %q, %q", test.command, activeTheme, colorAccent, colorTitle, colorConfirm, colorFocused, test.theme, test.accent, test.title, test.confirm, test.focused)
		}
	}
}

func TestFormTheme_selectedOptionUsesFocusedForFocusedAndAccentForBlurred(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })
	setTheme(themeOcean)

	styles := formTheme(true)
	focused := styles.Focused.SelectedOption.Render("selected")
	blurred := styles.Blurred.SelectedOption.Render("selected")
	focusedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorFocused))
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))

	if want := focusedStyle.Render("selected"); focused != want {
		t.Fatalf("focused selected option = %q, want focused color %q", focused, want)
	}
	if want := accentStyle.Render("selected"); blurred != want {
		t.Fatalf("blurred selected option = %q, want accent color %q", blurred, want)
	}
	if got, want := styles.Focused.SelectSelector.Render("cursor"), focusedStyle.Render(">  cursor"); got != want {
		t.Fatalf("focused select cursor = %q, want focused color %q", got, want)
	}
	if got, want := styles.Focused.TextInput.Cursor.Render("cursor"), focusedStyle.Render("cursor"); got != want {
		t.Fatalf("focused text cursor = %q, want focused color %q", got, want)
	}
}

func TestFormTheme_confirmButtonUsesFocusedColorOnlyWhileFocused(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })
	setTheme(themeOcean)

	var value bool
	confirm := huh.NewConfirm().Title("Nullable").Value(&value)
	confirm.WithTheme(formTheme)
	confirm.WithWidth(40)
	focusedButton := lipgloss.NewStyle().
		Padding(0, 2).
		MarginRight(1).
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorFocused)).
		Render("No")
	accentButton := lipgloss.NewStyle().
		Padding(0, 2).
		MarginRight(1).
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorAccent)).
		Render("No")

	confirm.Focus()
	if view := confirm.View(); !strings.Contains(view, focusedButton) || strings.Contains(view, accentButton) {
		t.Fatalf("focused confirm view = %q, want focused button color only", view)
	}

	confirm.Blur()
	if view := confirm.View(); !strings.Contains(view, accentButton) || strings.Contains(view, focusedButton) {
		t.Fatalf("blurred confirm view = %q, want accent button color only", view)
	}
}
