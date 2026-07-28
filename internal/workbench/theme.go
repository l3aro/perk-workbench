package workbench

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

var (
	activeTheme                                                                       = themeOcean
	colorCanvas, colorPanel, colorStripe                                              string
	colorInk, colorMuted, colorAccent                                                 string
	colorBorder, colorModeNormal                                                      string
	colorModeInsert                                                                   string
	headerStyle, footerStyle, statusStyle, thinkingStyle                              lipgloss.Style
	focusStyle, panelStyle                                                            lipgloss.Style
	connectionActionStyle                                                             lipgloss.Style
	connectionActionSelectedStyle                                                     lipgloss.Style
	primaryIndexStyle, uniqueIndexStyle                                               lipgloss.Style
	regularIndexStyle                                                                 lipgloss.Style
	statusSuccessStyle, statusFailedStyle                                             lipgloss.Style
	statusCanceledStyle, readOnlyStyle                                                lipgloss.Style
	modeNormalStyle, modeInsertStyle                                                  lipgloss.Style
	selectedCellStyle, completionItemStyle, completionBoxStyle, completionDetailStyle lipgloss.Style
	userMessageStyle, userMessageAccentStyle                                          lipgloss.Style
)

func init() { setTheme(themeOcean) }

func setTheme(name appTheme) {
	activeTheme = name
	switch name {
	case themeDracula:
		colorCanvas, colorPanel, colorStripe = "#282a36", "#343746", "#44475a"
		colorInk, colorMuted, colorAccent = "#f8f8f2", "#b1b2c7", "#bd93f9"
		colorBorder, colorModeNormal, colorModeInsert = "#6272a4", "#8be9fd", "#50fa7b"
	case themeNord:
		colorCanvas, colorPanel, colorStripe = "#2e3440", "#3b4252", "#434c5e"
		colorInk, colorMuted, colorAccent = "#eceff4", "#d8dee9", "#88c0d0"
		colorBorder, colorModeNormal, colorModeInsert = "#4c566a", "#81a1c1", "#a3be8c"
	case themeMonokai:
		colorCanvas, colorPanel, colorStripe = "#272822", "#2f302a", "#3e3d32"
		colorInk, colorMuted, colorAccent = "#f8f8f2", "#75715e", "#a6e22e"
		colorBorder, colorModeNormal, colorModeInsert = "#49483e", "#66d9ef", "#fd971f"
	case themeCatppuccin:
		colorCanvas, colorPanel, colorStripe = "#1e1e2e", "#313244", "#45475a"
		colorInk, colorMuted, colorAccent = "#cdd6f4", "#a6adc8", "#cba6f7"
		colorBorder, colorModeNormal, colorModeInsert = "#6c7086", "#89b4fa", "#a6e3a1"
	case themeSolarized:
		colorCanvas, colorPanel, colorStripe = "#002b36", "#073642", "#123f4a"
		colorInk, colorMuted, colorAccent = "#839496", "#657b83", "#268bd2"
		colorBorder, colorModeNormal, colorModeInsert = "#0e5553", "#268bd2", "#859900"
	default:
		colorCanvas, colorPanel, colorStripe = "#10151f", "#17202e", "#1c2838"
		colorInk, colorMuted, colorAccent = "#e6edf3", "#8b9bb4", "#55d6be"
		colorBorder, colorModeNormal, colorModeInsert = "#324155", "#58a6ff", "#3fb950"
	}
	resetStyles()
}

func resetStyles() {
	headerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorAccent)).
		Bold(true).
		Padding(0, spaceCompact)
	footerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted)).
		Padding(0, spaceCompact)
	statusStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted)).
		Padding(0, spaceCompact)
	thinkingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorAccent)).
		Italic(true).
		Padding(0, spaceCompact)
	focusStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Foreground(lipgloss.Color(colorInk)).
		Padding(0, spaceCompact)
	panelStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Foreground(lipgloss.Color(colorInk)).
		Padding(0, spaceCompact)
	connectionActionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInk)).
		Background(lipgloss.Color(colorStripe)).
		Padding(0, spaceCompact)
	connectionActionSelectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorAccent)).
		Bold(true).
		Padding(0, spaceCompact)
	userMessageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInk)).
		Background(lipgloss.Color(colorPanel))
	userMessageAccentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorAccent)).
		Background(lipgloss.Color(colorPanel))
	primaryIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))
	uniqueIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	regularIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	statusSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	statusFailedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	statusCanceledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	modeNormalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color(colorModeNormal)).
		Bold(true).
		Padding(0, spaceCompact)
	modeInsertStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color(colorModeInsert)).
		Bold(true).
		Padding(0, spaceCompact)
	readOnlyStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#d29922")).
		Bold(true).
		Padding(0, spaceCompact)
	selectedCellStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorAccent)).
		Bold(true)
	completionItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted))
	completionBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Padding(0, 1)
	completionDetailStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorBorder))
}

var formTheme = huh.ThemeFunc(func(bool) *huh.Styles {
	theme := huh.ThemeCharm(true)
	accent := lipgloss.Color(colorAccent)
	ink := lipgloss.Color(colorInk)
	muted := lipgloss.Color(colorMuted)
	panel := lipgloss.Color(colorPanel)
	stripe := lipgloss.Color(colorStripe)
	canvas := lipgloss.Color(colorCanvas)

	theme.Focused.Base = theme.Focused.Base.BorderForeground(accent)
	theme.Focused.Card = theme.Focused.Base.Background(panel)
	theme.Focused.Title = theme.Focused.Title.Foreground(accent)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(accent)
	theme.Focused.Description = theme.Focused.Description.Foreground(muted)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(accent)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(accent)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(accent)
	theme.Focused.Option = theme.Focused.Option.Foreground(ink)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(accent)
	theme.Focused.UnselectedOption = theme.Focused.UnselectedOption.Foreground(ink)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(canvas).Background(accent)
	theme.Focused.BlurredButton = theme.Focused.BlurredButton.Foreground(ink).Background(stripe)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(accent)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(muted)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(accent)
	theme.Focused.TextInput.Text = theme.Focused.TextInput.Text.Foreground(ink)
	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base.Background(panel)
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
			icons = append(icons, primaryIndexStyle.Render(iconPrimaryKey+"PK"))
		case sharedsql.IndexUnique:
			icons = append(icons, uniqueIndexStyle.Render(iconUnique+"UQ"))
		case sharedsql.IndexRegular:
			icons = append(icons, regularIndexStyle.Render(iconRegular+"IX"))
		}
	}
	return strings.Join(icons, " ")
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
