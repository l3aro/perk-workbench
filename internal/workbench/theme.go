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
		m.connection.form,
		m.columnForm.form,
		m.browseForm.form,
		m.indexForm.form,
		m.foreignKeyForm.form,
	)
	if m.cellEditor != nil {
		applyFormTheme(m.cellEditor.input)
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
