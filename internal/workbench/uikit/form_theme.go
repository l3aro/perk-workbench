package uikit

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// FormTheme is the shared huh theme derived from the active palette.
// Every form in the workbench (connection, browse, structure, index,
// foreign key, table) renders through it.
var FormTheme = huh.ThemeFunc(func(bool) *huh.Styles {
	theme := huh.ThemeCharm(true)
	primary := lipgloss.Color(ColorPrimary)
	focused := lipgloss.Color(ColorFocused)
	secondary := lipgloss.Color(ColorSecondary)
	ink := lipgloss.Color(ColorInk)
	muted := lipgloss.Color(ColorMuted)
	panel := lipgloss.Color(ColorPanel)
	stripe := lipgloss.Color(ColorStripe)
	canvas := lipgloss.Color(ColorCanvas)

	theme.Focused.Base = theme.Focused.Base.BorderForeground(primary)
	theme.Focused.Card = theme.Focused.Base.Background(panel)
	theme.Focused.Title = theme.Focused.Title.Foreground(secondary)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(secondary)
	theme.Focused.Description = theme.Focused.Description.Foreground(muted)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(focused)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(focused)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(focused)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(focused)
	theme.Focused.Option = theme.Focused.Option.Foreground(ink)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(focused)
	theme.Focused.UnselectedOption = theme.Focused.UnselectedOption.Foreground(ink)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(canvas).Background(focused)
	theme.Focused.BlurredButton = theme.Focused.BlurredButton.Foreground(ink).Background(stripe)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(focused)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(muted)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(focused)
	theme.Focused.TextInput.Text = theme.Focused.TextInput.Text.Foreground(ink)
	theme.Blurred = theme.Focused
	theme.Blurred.SelectedOption = theme.Blurred.SelectedOption.Foreground(primary)
	theme.Blurred.FocusedButton = theme.Blurred.FocusedButton.Foreground(canvas).Background(primary)
	theme.Blurred.MultiSelectSelector = theme.Blurred.MultiSelectSelector.Foreground(primary)
	theme.Blurred.TextInput.Cursor = theme.Blurred.TextInput.Cursor.Foreground(primary)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(primary)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()
	theme.Group.Title = theme.Focused.Title
	theme.Group.Description = theme.Focused.Description
	return theme
})

// NewForm builds a huh form with the shared workbench theme.
func NewForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(FormTheme)
}
