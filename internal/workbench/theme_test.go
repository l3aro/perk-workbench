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
		danger  string
		focused string
		success string
	}{
		{"theme.dracula", themeDracula, "#bd93f9", "#ff79c6", "#ff5555", "#50fa7b", "#50fa7b"},
		{"theme.catppuccin", themeCatppuccin, "#cba6f7", "#f9e2af", "#f38ba8", "#a6e3a1", "#a6e3a1"},
		{"theme.ocean", themeOcean, "#94e2d5", "#89b4fa", "#f38ba8", "#f9e2af", "#3fb950"},
		{"theme.nord", themeNord, "#88c0d0", "#ebcb8b", "#bf616a", "#a3be8c", "#a3be8c"},
		{"theme.monokai", themeMonokai, "#a6e22e", "#f92672", "#f92672", "#a6e22e", "#a6e22e"},
		{"theme.solarized", themeSolarized, "#268bd2", "#d33682", "#dc322f", "#859900", "#859900"},
	} {
		updated, _ := model.handlePaletteCommand(test.command)
		model = updated.(Model)
		if activeTheme != test.theme || colorPrimary != test.accent || colorSecondary != test.title || colorDanger != test.danger || colorFocused != test.focused || colorSuccess != test.success {
			t.Fatalf("command %q applied theme %q with primary %q, secondary %q, danger %q, focused %q, success %q; want %q with %q, %q, %q, %q, %q", test.command, activeTheme, colorPrimary, colorSecondary, colorDanger, colorFocused, colorSuccess, test.theme, test.accent, test.title, test.danger, test.focused, test.success)
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
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary))

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
		Background(lipgloss.Color(colorPrimary)).
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
