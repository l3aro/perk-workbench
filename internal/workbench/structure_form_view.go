package workbench

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	formLabelWidth = 13
	formFieldGap   = " "
)

var formLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))

func formLabel(text string, width int) string {
	if width <= 0 {
		return formLabelStyle.Render(text)
	}
	return formLabelStyle.Width(width).Render(ansi.Truncate(text, width, ""))
}

func focusedFormLabel(text string, width int) string {
	if width <= 0 {
		return headerStyle.Padding(0, 0).Render(text)
	}
	return headerStyle.Padding(0, 0).Width(width).Render(ansi.Truncate(text, width, ""))
}

type formRow struct {
	label    string
	value    string
	focusKey int // -1 if the row has no focusable field
}

func (f columnForm) View() string {
	if f.saving {
		return statusStyle.Render("saving column changes")
	}
	if f.confirming() {
		action := "Save column changes?"
		if f.mode == columnFormConfirmDiscard {
			action = "Discard column changes?"
		}
		choices := "[" + booleanValue(false) + "] " + booleanValue(true)
		if f.confirmed {
			choices = booleanValue(false) + " [" + booleanValue(true) + "]"
		}
		return headerStyle.Render(action) + "\n" + choices + "\n" + statusStyle.Render("Tab selects true | Enter confirms | Esc returns to the form")
	}
	if f.mode == columnFormSelectType {
		return f.typePickerView()
	}
	rows := []formRow{
		{label: "Name", value: f.name.View(), focusKey: columnFieldName},
		{label: "Type", value: f.typeValue(), focusKey: columnFieldType},
	}
	for index, parameter := range f.parameters {
		rows = append(rows, formRow{label: "", value: parameter.View(), focusKey: columnFieldParameterStart + index})
	}
	rows = append(rows,
		formRow{label: "Nullable", value: booleanValue(f.nullable), focusKey: f.nullableField()},
		formRow{label: "Default", value: f.defaultDisplay(), focusKey: f.defaultField()},
		formRow{label: "PK", value: primaryKeyText(f.primaryKey), focusKey: -1},
	)
	lines := make([]string, len(rows))
	for index, row := range rows {
		label := formLabel(row.label, formLabelWidth)
		if row.focusKey >= 0 && f.focus == row.focusKey && f.mode == columnFormNormal {
			label = focusedFormLabel(row.label, formLabelWidth)
		}
		lines[index] = label + formFieldGap + row.value
	}
	help := "j/k fields | gg/G first/last | i edit/select | space toggle nullable | F5 save | Esc discard"
	if f.mode == columnFormInsert {
		help = "insert mode | Esc normal mode"
	}
	return strings.Join(lines, "\n") + "\n" + statusStyle.Render(help)
}

// defaultDisplay makes the empty-default state legible.
func (f columnForm) defaultDisplay() string {
	value := strings.TrimSpace(f.preset.Value())
	switch {
	case value != "":
		return f.preset.View()
	case f.hadDefault:
		return formLabelStyle.Render("(cleared)")
	default:
		return formLabelStyle.Render("(none)")
	}
}

func primaryKeyText(position int) string {
	if position == 0 {
		return ""
	}
	return strconv.Itoa(position) + " (read-only)"
}
