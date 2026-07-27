package workbench

func (f columnForm) View() string {
	if f.saving {
		return statusStyle.Render("saving column changes")
	}
	if f.form == nil {
		return ""
	}
	return f.form.View()
}
