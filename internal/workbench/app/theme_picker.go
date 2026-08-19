package app

import "strings"

// themePicker lists the themes of the effective appearance (the active
// scheme's slot), so a selection always commits a theme whose scheme matches
// the scheme it will be stored under.
type themePicker struct {
	themes   []appTheme
	original appTheme
	selected int
}

func newThemePicker() *themePicker {
	themes := themesForScheme(runtimeScheme)
	picker := &themePicker{themes: themes, original: activeTheme, selected: 0}
	for i, theme := range themes {
		if theme == activeTheme {
			picker.selected = i
			break
		}
	}
	return picker
}

func (p *themePicker) theme() appTheme {
	return p.themes[p.selected]
}

func (p *themePicker) move(delta int) {
	p.selected = max(0, min(p.selected+delta, len(p.themes)-1))
}

func (p *themePicker) content() string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Theme — " + string(runtimeScheme) + " "))
	content.WriteString("\n\n")
	for i, theme := range p.themes {
		prefix := "  "
		label := string(theme)
		if i == p.selected {
			prefix = "> "
			label = selectedItemStyle.Render(label)
		}
		content.WriteString(prefix + label + "\n")
	}
	content.WriteString("\n")
	content.WriteString(mutedStyle.Render(" j/k or arrows preview | enter select | esc cancel"))
	return content.String()
}
