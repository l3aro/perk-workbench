package workbench

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

var (
	activeTheme                                                                                    = themeOcean
	colorCanvas, colorPanel, colorStripe                                                           string
	colorInk, colorMuted, colorPrimary                                                             string
	colorSecondary, colorDanger, colorFocused, colorSuccess                                        string
	colorBorder, colorModeNormal                                                                   string
	colorModeInsert                                                                                string
	headerStyle, headerButtonStyle, headerQuitButtonStyle, footerStyle, statusStyle, thinkingStyle lipgloss.Style
	focusStyle, panelStyle                                                                         lipgloss.Style
	connectionActionStyle                                                                          lipgloss.Style
	connectionActionSelectedStyle, connectionActionFocusedStyle                                    lipgloss.Style
	formSaveButtonStyle, formCancelButtonStyle, formButtonFocusedStyle                             lipgloss.Style
	primaryIndexStyle, uniqueIndexStyle                                                            lipgloss.Style
	regularIndexStyle                                                                              lipgloss.Style
	statusSuccessStyle, statusFailedStyle                                                          lipgloss.Style
	statusCanceledStyle, readOnlyStyle                                                             lipgloss.Style
	modeNormalStyle, modeInsertStyle                                                               lipgloss.Style
	selectedCellStyle, completionItemStyle, completionBoxStyle, completionDetailStyle              lipgloss.Style
	userMessageStyle, userMessageAccentStyle                                                       lipgloss.Style
)

func init() { setTheme(themeOcean) }

func setTheme(name appTheme) {
	activeTheme = name
	switch name {
	case themeDracula:
		colorCanvas, colorPanel, colorStripe = "#282a36", "#343746", "#44475a"
		colorInk, colorMuted, colorPrimary = "#f8f8f2", "#b1b2c7", "#bd93f9"
		colorSecondary, colorDanger, colorFocused = "#ff79c6", "#ff5555", "#50fa7b"
		colorSuccess = "#50fa7b"
		colorBorder, colorModeNormal, colorModeInsert = "#6272a4", "#8be9fd", "#50fa7b"
	case themeNord:
		colorCanvas, colorPanel, colorStripe = "#2e3440", "#3b4252", "#434c5e"
		colorInk, colorMuted, colorPrimary = "#eceff4", "#d8dee9", "#88c0d0"
		colorSecondary, colorDanger, colorFocused = "#ebcb8b", "#bf616a", "#a3be8c"
		colorSuccess = "#a3be8c"
		colorBorder, colorModeNormal, colorModeInsert = "#4c566a", "#81a1c1", "#a3be8c"
	case themeMonokai:
		colorCanvas, colorPanel, colorStripe = "#272822", "#2f302a", "#3e3d32"
		colorInk, colorMuted, colorPrimary = "#f8f8f2", "#75715e", "#a6e22e"
		colorSecondary, colorDanger, colorFocused = "#f92672", "#f92672", "#a6e22e"
		colorSuccess = "#a6e22e"
		colorBorder, colorModeNormal, colorModeInsert = "#49483e", "#66d9ef", "#fd971f"
	case themeCatppuccin:
		colorCanvas, colorPanel, colorStripe = "#1e1e2e", "#313244", "#45475a"
		colorInk, colorMuted, colorPrimary = "#cdd6f4", "#a6adc8", "#cba6f7"
		colorSecondary, colorDanger, colorFocused = "#f9e2af", "#f38ba8", "#a6e3a1"
		colorSuccess = "#a6e3a1"
		colorBorder, colorModeNormal, colorModeInsert = "#6c7086", "#89b4fa", "#a6e3a1"
	case themeSolarized:
		colorCanvas, colorPanel, colorStripe = "#002b36", "#073642", "#123f4a"
		colorInk, colorMuted, colorPrimary = "#839496", "#657b83", "#268bd2"
		colorSecondary, colorDanger, colorFocused = "#d33682", "#dc322f", "#859900"
		colorSuccess = "#859900"
		colorBorder, colorModeNormal, colorModeInsert = "#0e5553", "#268bd2", "#859900"
	default:
		colorCanvas, colorPanel, colorStripe = "#10151f", "#17202e", "#1c2838"
		colorInk, colorMuted, colorPrimary = "#e6edf3", "#8b9bb4", "#94e2d5"
		colorSecondary, colorDanger, colorFocused = "#89b4fa", "#f38ba8", "#f9e2af"
		colorSuccess = "#3fb950"
		colorBorder, colorModeNormal, colorModeInsert = "#324155", "#58a6ff", "#3fb950"
	}
	resetStyles()
}

func resetStyles() {
	headerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorSecondary)).
		Bold(true).
		Padding(0, spaceCompact)
	headerButtonStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorPrimary)).
		Bold(true).
		Padding(0, spaceCompact)
	headerQuitButtonStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorDanger)).
		Bold(true).
		Padding(0, spaceCompact)
	footerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted)).
		Padding(0, spaceCompact)
	statusStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted)).
		Padding(0, spaceCompact)
	thinkingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorPrimary)).
		Italic(true).
		Padding(0, spaceCompact)
	focusStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorPrimary)).
		Foreground(lipgloss.Color(colorInk)).
		Padding(0, spaceCompact)
	panelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Foreground(lipgloss.Color(colorInk)).
		Padding(0, spaceCompact)
	connectionActionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInk)).
		Background(lipgloss.Color(colorStripe)).
		Padding(0, spaceCompact)
	connectionActionSelectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorPrimary)).
		Bold(true).
		Padding(0, spaceCompact)
	connectionActionFocusedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorFocused)).
		Bold(true).
		Padding(0, spaceCompact)
	formSaveButtonStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorPrimary)).
		Bold(true).
		Padding(0, spaceCompact)
	formCancelButtonStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInk)).
		Background(lipgloss.Color(colorStripe)).
		Padding(0, spaceCompact)
	formButtonFocusedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorFocused)).
		Bold(true).
		Padding(0, spaceCompact)
	userMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInk)).
		Background(lipgloss.Color(colorPanel))
	userMessageAccentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorPrimary)).
		Background(lipgloss.Color(colorPanel))
	primaryIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))
	uniqueIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	regularIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	statusSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSuccess))
	statusFailedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorDanger))
	statusCanceledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	// Badges are foreground-only text so they never read as buttons, which
	// are the only solid pills in the UI.
	modeNormalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorModeNormal)).
		Bold(true)
	modeInsertStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorModeInsert)).
		Bold(true)
	readOnlyStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d29922")).
		Bold(true)
	selectedCellStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorPrimary)).
		Bold(true)
	completionItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted))
	completionBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorPrimary)).
		Padding(0, 1)
	completionDetailStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorBorder))
}

var formTheme = huh.ThemeFunc(func(bool) *huh.Styles {
	theme := huh.ThemeCharm(true)
	primary := lipgloss.Color(colorPrimary)
	focused := lipgloss.Color(colorFocused)
	secondary := lipgloss.Color(colorSecondary)
	ink := lipgloss.Color(colorInk)
	muted := lipgloss.Color(colorMuted)
	panel := lipgloss.Color(colorPanel)
	stripe := lipgloss.Color(colorStripe)
	canvas := lipgloss.Color(colorCanvas)

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

func newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(formTheme)
}

func indexIcons(indexes []sharedsql.IndexKind) string {
	// ponytail: terminals do not expose font support, so labels are the fallback.
	icons := make([]string, 0, len(indexes))
	for _, index := range indexes {
		switch index {
		case sharedsql.IndexPrimaryKey:
			icons = append(icons, primaryIndexStyle.Render(iconPrimaryKey+" PK"))
		case sharedsql.IndexUnique:
			icons = append(icons, uniqueIndexStyle.Render(iconUnique+" UQ"))
		case sharedsql.IndexRegular:
			icons = append(icons, regularIndexStyle.Render(iconRegular+" IX"))
		}
	}
	return strings.Join(icons, " | ")
}

// commitTheme applies a theme and persists the choice to config.json so it
// survives the next launch. Persistence is best-effort: a failure is shown
// in the status line without reverting the applied theme.
func (m *Model) commitTheme(name appTheme) {
	m.applyTheme(name)
	if m.configPath == "" {
		m.setStatus("theme: " + string(name))
		return
	}
	if err := SaveTheme(m.configPath, string(name)); err != nil {
		m.setStatus("theme: " + string(name) + " (not saved: " + err.Error() + ")")
		return
	}
	m.setStatus("theme: " + string(name))
}

func (m *Model) applyTheme(name appTheme) {
	setTheme(name)

	m.schema.SetDelegate(schemaItemDelegate{})
	m.picker.SetDelegate(newListDelegate())
	m.recent.SetDelegate(newListDelegate())
	applyListTheme(&m.schema)
	applyListTheme(&m.picker)
	applyListTheme(&m.recent)
	applyFormTheme(
		m.connection.form,
		m.columnForm.form,
		m.browseForm.form,
		m.indexForm.form,
		m.foreignKeyForm.form,
	)
	if m.cellEditor != nil {
		applyFormTheme(m.cellEditor.input)
	}
	if m.explainPicker != nil {
		applyFormTheme(m.explainPicker.form)
	}
	if m.width > 0 {
		m.layout(m.width, m.height)
	}
}

func applyFormTheme(forms ...*huh.Form) {
	for _, form := range forms {
		if form != nil {
			form.WithTheme(formTheme)
		}
	}
}
