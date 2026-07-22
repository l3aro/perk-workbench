package workbench

import (
	"strconv"
	"strings"
)

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
	fields := []string{f.field(columnFieldName, f.name.View()), f.field(columnFieldType, "Type: "+f.typeValue())}
	for index, parameter := range f.parameters {
		fields = append(fields, f.field(columnFieldParameterStart+index, parameter.View()))
	}
	nullable := "Nullable: " + booleanValue(f.nullable)
	fields = append(fields, f.field(f.nullableField(), nullable), f.field(f.defaultField(), f.preset.View()), "PK: "+primaryKeyText(f.primaryKey))
	help := "j/k fields | gg/G first/last | i edit/select | space toggle nullable | F5 save | Esc discard"
	if f.mode == columnFormInsert {
		help = "insert mode | Esc normal mode"
	}
	return strings.Join(fields, "\n") + "\n" + statusStyle.Render(help)
}

func (f columnForm) field(index int, value string) string {
	if f.focus == index && f.mode == columnFormNormal {
		return headerStyle.Render(value)
	}
	return value
}

func primaryKeyText(position int) string {
	if position == 0 {
		return ""
	}
	return strconv.Itoa(position) + " (read-only)"
}
