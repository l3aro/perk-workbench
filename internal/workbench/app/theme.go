package app

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// scheme is the light/dark appearance that picks the active theme slot.
type scheme string

const (
	schemeDark  scheme = "dark"
	schemeLight scheme = "light"
)

// themeScheme reports the appearance family a theme name belongs to. Every
// theme carries exactly one scheme, so a theme slot and the palette it
// renders can never disagree about the effective appearance.
func themeScheme(name appTheme) scheme {
	switch name {
	case themeLightOcean, themeLightNord, themeLightMonokai, themeLightDracula, themeLightCatppuccin, themeLightSolarized:
		return schemeLight
	default:
		return schemeDark
	}
}

// darkThemes lists the dark-scheme themes in picker order.
func darkThemes() []appTheme {
	return []appTheme{themeOcean, themeNord, themeMonokai, themeDracula, themeCatppuccin, themeSolarized}
}

// lightThemes lists the light-scheme themes in picker order.
func lightThemes() []appTheme {
	return []appTheme{themeLightOcean, themeLightNord, themeLightMonokai, themeLightDracula, themeLightCatppuccin, themeLightSolarized}
}

// allThemes lists every theme, dark first then light.
func allThemes() []appTheme {
	return append(darkThemes(), lightThemes()...)
}

// themesForScheme returns the theme list shown for an appearance.
func themesForScheme(s scheme) []appTheme {
	if s == schemeLight {
		return lightThemes()
	}
	return darkThemes()
}

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

// runtimeScheme is the effective appearance this session (system-derived or
// explicit). runtimeTheme is the active theme resolved from the matching
// config slot.
var (
	runtimeScheme scheme
	runtimeTheme  appTheme
	// detectedScheme holds the system light/dark result captured at startup
	// for auto-following. Empty until detection runs.
	detectedScheme scheme
)

// applyAppearanceConfig resolves the effective appearance and active theme
// from config and applies it to the UI. Called by SetAppConfig.
func applyAppearanceConfig(config Config) {
	runtimeScheme = resolveScheme(config)
	runtimeTheme = themeForScheme(runtimeScheme, config)
	setTheme(runtimeTheme)
}

// resolveScheme determines the effective appearance: when auto-following is
// enabled (the default) it prefers system detection, falling back to the
// persisted appearance and then dark; otherwise it uses the persisted
// appearance directly.
func resolveScheme(config Config) scheme {
	if config.AutoTheme != nil && !*config.AutoTheme {
		return schemeForAppearance(config.Appearance)
	}
	if detectedScheme != "" {
		return detectedScheme
	}
	return schemeForAppearance(config.Appearance)
}

// schemeForAppearance maps an appearance string to a scheme, defaulting to
// dark for empty or unknown values (validation has already rejected
// non-empty unknown values).
func schemeForAppearance(value string) scheme {
	if value == string(schemeLight) {
		return schemeLight
	}
	return schemeDark
}

// themeForScheme picks the configured slot theme for an appearance, falling
// back to that scheme's default when the slot is empty or holds a theme name
// whose scheme does not match (a hand-edited config): the slot is repaired,
// never accepted as-is.
func themeForScheme(s scheme, config Config) appTheme {
	if s == schemeLight {
		name := appTheme(config.LightTheme)
		if validTheme(string(name)) && themeScheme(name) == schemeLight {
			return name
		}
		return themeLightOcean
	}
	name := appTheme(config.DarkTheme)
	if validTheme(string(name)) && themeScheme(name) == schemeDark {
		return name
	}
	return themeOcean
}

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
	thinkingStyle = uikit.ThinkingStyle
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
	userMessageStyle = uikit.UserMessageStyle
	userMessageAccentStyle = uikit.UserMessageAccentStyle
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
	completionItemStyle = uikit.CompletionItemStyle
	completionBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorPrimary)).
		Padding(0, 1)
	completionDetailStyle = uikit.CompletionDetailStyle
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
	runtimeTheme = name
	runtimeScheme = themeScheme(name)
	if m.configPath == "" {
		m.setStatus("theme: " + string(name))
		return
	}
	if err := SaveTheme(m.configPath, name); err != nil {
		m.setStatus("theme: " + string(name) + " (not saved: " + err.Error() + ")")
		return
	}
	m.setStatus("theme: " + string(name))
}

func (m *Model) applyTheme(name appTheme) {
	setTheme(name)

	m.schema.component.RefreshTheme()
	m.connection.picker.SetDelegate(newListDelegate())
	m.connection.component.Recent.SetDelegate(newListDelegate())
	applyListTheme(&m.connection.picker)
	applyListTheme(&m.connection.component.Recent)
	applyFormTheme(
		m.connection.component.Form.Huh,
		m.browse.component.Form.Form,
	)
	if m.browse.component.CellEditor != nil {
		applyFormTheme(m.browse.component.CellEditor.Input)
	}
	if m.overlay.explainPicker != nil {
		applyFormTheme(m.overlay.explainPicker.form)
	}
	if m.layout.width > 0 {
		m.applyLayout(m.layout.width, m.layout.height)
	}
}

// toggleAppearance flips the effective appearance between light and dark,
// applying the theme of the other scheme's slot. The flip is durable (persists
// the new appearance) only while auto-following is off; while auto_theme is
// enabled it is a session-only override that the next launch's system
// detection wins back, so it never rewrites the persisted appearance.
// Persistence is best-effort: a failure is shown in the status line without
// reverting the flip.
func (m *Model) toggleAppearance() {
	other := schemeLight
	if runtimeScheme == schemeLight {
		other = schemeDark
	}
	m.setAppearance(other)
}

// setAppearance applies an appearance to the UI and persists it when the
// choice is durable (auto-following is off).
func (m *Model) setAppearance(s scheme) {
	theme := themeForScheme(s, appConfig)
	m.applyTheme(theme)
	runtimeScheme = s
	runtimeTheme = theme
	if m.configPath == "" {
		m.setStatus("appearance: " + string(s))
		return
	}
	if appConfig.AutoTheme == nil || *appConfig.AutoTheme {
		// Auto-following: a mid-session flip is a temporary override; the
		// next launch resolves system appearance again. Do not persist it.
		m.setStatus("appearance: " + string(s) + " (system until restart)")
		return
	}
	if err := SaveAppearance(m.configPath, string(s)); err != nil {
		m.setStatus("appearance: " + string(s) + " (not saved: " + err.Error() + ")")
		return
	}
	m.setStatus("appearance: " + string(s))
}

func applyFormTheme(forms ...*huh.Form) {
	for _, form := range forms {
		if form != nil {
			form.WithTheme(formTheme)
		}
	}
}
