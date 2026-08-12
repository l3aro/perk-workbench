package workbench

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

var (
	activeTheme                                                                                    = themeOcean
	colorCanvas, colorPanel, colorStripe                                                           string
	colorInk, colorMuted, colorPrimary                                                             string
	colorSecondary, colorDanger, colorFocused, colorSuccess                                        string
	colorBorder, colorModeNormal                                                                   string
	colorModeInsert                                                                                string
	colorWarn                                                                                      string
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
	// The palette values live in the shared UI layer so feature components
	// and the root derive their styles from one source; the root snapshots
	// them into its own style registry below.
	uikit.SetTheme(string(name))
	colorCanvas, colorPanel, colorStripe = uikit.ColorCanvas, uikit.ColorPanel, uikit.ColorStripe
	colorInk, colorMuted, colorPrimary = uikit.ColorInk, uikit.ColorMuted, uikit.ColorPrimary
	colorSecondary, colorDanger, colorFocused, colorSuccess = uikit.ColorSecondary, uikit.ColorDanger, uikit.ColorFocused, uikit.ColorSuccess
	colorBorder, colorModeNormal, colorModeInsert = uikit.ColorBorder, uikit.ColorModeNormal, uikit.ColorModeInsert
	colorWarn = uikit.ColorWarn
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
	formSaveButtonStyle = uikit.ButtonSaveStyle
	formCancelButtonStyle = uikit.ButtonCancelStyle
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

var formTheme = uikit.FormTheme

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

	m.schema.list.SetDelegate(schemaItemDelegate{})
	m.connection.picker.SetDelegate(newListDelegate())
	m.connection.component.Recent.SetDelegate(newListDelegate())
	applyListTheme(&m.schema.list)
	applyListTheme(&m.connection.picker)
	applyListTheme(&m.connection.component.Recent)
	applyFormTheme(
		m.connection.component.Form.Huh,
		m.structure.columnForm.form,
		m.browse.form.form,
		m.structure.indexForm.form,
		m.structure.foreignKeyForm.form,
	)
	if m.browse.cellEditor != nil {
		applyFormTheme(m.browse.cellEditor.input)
	}
	if m.overlay.explainPicker != nil {
		applyFormTheme(m.overlay.explainPicker.form)
	}
	if m.layout.width > 0 {
		m.applyLayout(m.layout.width, m.layout.height)
	}
}

func applyFormTheme(forms ...*huh.Form) {
	for _, form := range forms {
		if form != nil {
			form.WithTheme(formTheme)
		}
	}
}
