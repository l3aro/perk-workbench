package workbench

import "strings"

var themeChoices = []appTheme{themeOcean, themeDracula, themeCatppuccin}

type themePicker struct {
	original appTheme
	selected int
}

func newThemePicker() *themePicker {
	picker := &themePicker{original: activeTheme}
	for i, theme := range themeChoices {
		if theme == activeTheme {
			picker.selected = i
			break
		}
	}
	return picker
}

func (p *themePicker) theme() appTheme {
	return themeChoices[p.selected]
}

func (p *themePicker) move(delta int) {
	p.selected = max(0, min(p.selected+delta, len(themeChoices)-1))
}

func (p *themePicker) content() string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Theme "))
	content.WriteString("\n\n")
	for i, theme := range themeChoices {
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
