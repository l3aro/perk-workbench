package workbench

import "charm.land/huh/v2"

func (m *Model) applyTheme(name appTheme) {
	setTheme(name)

	m.schema.SetDelegate(schemaItemDelegate{})
	m.picker.SetDelegate(newListDelegate())
	m.recent.SetDelegate(newListDelegate())
	applyListTheme(&m.schema)
	applyListTheme(&m.picker)
	applyListTheme(&m.recent)
	applyFormTheme(
		m.connection.form, m.connection.confirmation,
		m.columnForm.form, m.columnForm.confirmation,
		m.browseForm.form, m.browseForm.confirmation,
		m.indexForm.form, m.indexForm.confirmation,
		m.foreignKeyForm.form, m.foreignKeyForm.confirmation,
		m.quitDialog,
	)
	if m.cellEditor != nil {
		applyFormTheme(m.cellEditor.input, m.cellEditor.confirm)
	}
	if m.queryConfirmation != nil {
		applyFormTheme(m.queryConfirmation.form)
	}
	if m.explainPicker != nil {
		applyFormTheme(m.explainPicker.form)
	}
	if m.savedQueryPicker != nil {
		applyFormTheme(m.savedQueryPicker.form)
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
