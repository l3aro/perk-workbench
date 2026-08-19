package app

import "strings"

// appearanceChoice is one selectable appearance mode in the appearance
// picker: follow the system, or an explicit light/dark scheme.
type appearanceChoice struct {
	label string
	value string
}

var appearanceChoices = []appearanceChoice{
	{label: "auto (follow system)", value: "auto"},
	{label: "light", value: string(schemeLight)},
	{label: "dark", value: string(schemeDark)},
}

// appearancePicker lets the user choose the appearance mode explicitly,
// including returning to system auto-following (the only way to re-enable
// auto once a toggle has turned it off).
type appearancePicker struct {
	original scheme
	selected int
}

func newAppearancePicker() *appearancePicker {
	p := &appearancePicker{selected: 0}
	switch {
	case appConfig.AutoTheme != nil && !*appConfig.AutoTheme:
		if schemeForAppearance(appConfig.Appearance) == schemeLight {
			p.selected = 1
		} else {
			p.selected = 2
		}
	default:
		p.selected = 0 // auto mode
	}
	return p
}

func (p *appearancePicker) value() string {
	return appearanceChoices[p.selected].value
}

func (p *appearancePicker) move(delta int) {
	p.selected = max(0, min(p.selected+delta, len(appearanceChoices)-1))
}

func (p *appearancePicker) content() string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Appearance "))
	content.WriteString("\n\n")
	for i, choice := range appearanceChoices {
		prefix := "  "
		label := choice.label
		if i == p.selected {
			prefix = "> "
			label = selectedItemStyle.Render(label)
		}
		content.WriteString(prefix + label + "\n")
	}
	content.WriteString("\n")
	content.WriteString(mutedStyle.Render(" auto follows the system theme at launch"))
	return content.String()
}
