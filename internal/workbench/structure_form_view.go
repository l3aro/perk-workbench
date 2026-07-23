package workbench

import (
	"strconv"
)

func (f columnForm) View() string {
	if f.saving {
		return statusStyle.Render("saving column changes")
	}
	if f.confirmation != nil {
		return f.confirmation.View()
	}
	if f.form == nil {
		return ""
	}
	return f.form.View()
}

func primaryKeyNote(position int) string { return strconv.Itoa(position) + " (read-only)" }
