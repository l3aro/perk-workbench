package workbench

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type foreignKeyFormAction uint8

const (
	foreignKeyFormNoAction foreignKeyFormAction = iota
	foreignKeyFormSave
	foreignKeyFormDiscard
	foreignKeyFormDelete
)

type foreignKeyForm struct {
	form, confirmation                   *huh.Form
	values                               *foreignKeyFormValues
	previous                             string
	width                                int
	saving                               bool
	confirmationSave, confirmationDelete bool
	keybindings                          Keybindings
}

type foreignKeyFormValues struct {
	columns, referenceTable, referenceColumns string
	onDelete, onUpdate                        string
	confirmed                                 bool
}

func newForeignKeyForm(foreignKey *sharedsql.ForeignKeyInfo) foreignKeyForm {
	form := foreignKeyForm{values: &foreignKeyFormValues{onDelete: "NO ACTION", onUpdate: "NO ACTION"}, keybindings: DefaultKeybindings()}
	if foreignKey != nil {
		form.previous = foreignKey.ID
		form.values.columns = strings.Join(foreignKey.Columns, ", ")
		form.values.referenceTable = foreignKey.ReferenceTable
		form.values.referenceColumns = strings.Join(foreignKey.ReferenceColumns, ", ")
		form.values.onDelete = foreignKey.OnDelete
		form.values.onUpdate = foreignKey.OnUpdate
	}
	form.rebuildForm()
	return form
}

func (f foreignKeyForm) active() bool { return f.values != nil }

func (f foreignKeyForm) confirming() bool { return f.confirmation != nil }

func (f *foreignKeyForm) close() { *f = foreignKeyForm{} }

func (f *foreignKeyForm) Update(message tea.Msg, controller *formModeController) (tea.Cmd, foreignKeyFormAction) {
	if f.saving {
		return nil, foreignKeyFormNoAction
	}
	if route := controller.routeHuh(message, f.blur); route != formRouteParent {
		if route == formRouteConsumed && f.confirmation != nil && controller.mode == formModeNormal {
			f.confirmation = nil
		}
		if route == formRouteHuh {
			return f.updateHuh(message, controller)
		}
		return nil, foreignKeyFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil, foreignKeyFormNoAction
	}
	switch {
	case f.keybindings.Match(keyPress, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}):
		return controller.beginHuh(f.focus()), foreignKeyFormNoAction
	case f.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}):
		if _, err := f.change(); err != nil {
			f.showValidationError()
			return nil, foreignKeyFormNoAction
		}
		f.beginConfirmation(true, false)
		controller.beginConfirm()
		return f.confirmation.Init(), foreignKeyFormNoAction
	case f.keybindings.Match(keyPress, "form.discard", []scope{scopeForm, scopeView, scopeGlobal}):
		f.beginConfirmation(false, false)
		controller.beginConfirm()
		return f.confirmation.Init(), foreignKeyFormNoAction
	case f.keybindings.Match(keyPress, "form.delete", []scope{scopeForm, scopeView, scopeGlobal}):
		if f.previous != "" {
			f.beginConfirmation(false, true)
			controller.beginConfirm()
			return f.confirmation.Init(), foreignKeyFormNoAction
		}
	case f.keybindings.Match(keyPress, "form.field_next", []scope{scopeForm, scopeView, scopeGlobal}):
		return f.form.NextField(), foreignKeyFormNoAction
	case f.keybindings.Match(keyPress, "form.field_prev", []scope{scopeForm, scopeView, scopeGlobal}):
		return f.form.PrevField(), foreignKeyFormNoAction
	}
	return nil, foreignKeyFormNoAction
}

func (f *foreignKeyForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, foreignKeyFormAction) {
	if f.confirmation != nil {
		model, command := f.confirmation.Update(message)
		f.confirmation = model.(*huh.Form)
		if f.confirmation.State != huh.StateCompleted {
			return command, foreignKeyFormNoAction
		}
		confirmed := f.values.confirmed || f.confirmation.GetBool("confirm")
		f.confirmation = nil
		controller.mode = formModeNormal
		if !confirmed {
			return nil, foreignKeyFormNoAction
		}
		if f.confirmationDelete {
			return nil, foreignKeyFormDelete
		}
		if f.confirmationSave {
			return nil, foreignKeyFormSave
		}
		return nil, foreignKeyFormDiscard
	}
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if f.form.State == huh.StateCompleted {
		f.rebuildForm()
		return f.focus(), foreignKeyFormNoAction
	}
	return command, foreignKeyFormNoAction
}

func (f *foreignKeyForm) beginConfirmation(save, delete bool) {
	f.values.confirmed, f.confirmationSave, f.confirmationDelete = false, save, delete
	title := "Discard foreign-key changes?"
	if save {
		title = "Save foreign-key changes?"
	}
	if delete {
		title = "Delete foreign key?"
	}
	f.confirmation = newForm(huh.NewGroup(
		huh.NewConfirm().Key("confirm").Title(title).Affirmative("Yes").Negative("No").Value(&f.values.confirmed),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func (f *foreignKeyForm) rebuildForm() {
	actions := []huh.Option[string]{
		huh.NewOption("NO ACTION", "NO ACTION"),
		huh.NewOption("RESTRICT", "RESTRICT"),
		huh.NewOption("SET NULL", "SET NULL"),
		huh.NewOption("SET DEFAULT", "SET DEFAULT"),
		huh.NewOption("CASCADE", "CASCADE"),
	}
	f.form = newForm(huh.NewGroup(
		newEditableInput(huh.NewInput().Key("columns").Title("Columns*").Value(&f.values.columns).Validate(requiredForeignKeyColumns), &f.values.columns),
		newEditableInput(huh.NewInput().Key("reference-table").Title("Reference table*").Value(&f.values.referenceTable).Validate(requiredReferenceTable), &f.values.referenceTable),
		newEditableInput(huh.NewInput().Key("reference-columns").Title("Reference columns*").Value(&f.values.referenceColumns).Validate(f.validateReferenceColumns), &f.values.referenceColumns),
		huh.NewSelect[string]().Key("on-delete").Title("On delete").Options(actions...).Value(&f.values.onDelete),
		huh.NewSelect[string]().Key("on-update").Title("On update").Options(actions...).Value(&f.values.onUpdate),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func requiredForeignKeyColumns(value string) error {
	columns := splitForeignKeyColumns(value)
	referenceColumns := make([]string, len(columns))
	for index := range referenceColumns {
		referenceColumns[index] = fmt.Sprintf("reference-%d", index)
	}
	return sharedsql.ValidateForeignKeyChange(sharedsql.ForeignKeyChange{
		Columns: columns, ReferenceTable: "reference", ReferenceColumns: referenceColumns,
		OnDelete: "NO ACTION", OnUpdate: "NO ACTION",
	})
}

func requiredReferenceTable(value string) error {
	return sharedsql.ValidateForeignKeyChange(sharedsql.ForeignKeyChange{
		Columns: []string{"column"}, ReferenceTable: value, ReferenceColumns: []string{"reference"},
		OnDelete: "NO ACTION", OnUpdate: "NO ACTION",
	})
}

func requiredReferenceColumns(value string) error {
	referenceColumns := splitForeignKeyColumns(value)
	columns := make([]string, len(referenceColumns))
	for index := range columns {
		columns[index] = fmt.Sprintf("column-%d", index)
	}
	return sharedsql.ValidateForeignKeyChange(sharedsql.ForeignKeyChange{
		Columns: columns, ReferenceTable: "reference", ReferenceColumns: referenceColumns,
		OnDelete: "NO ACTION", OnUpdate: "NO ACTION",
	})
}

func (f foreignKeyForm) validateReferenceColumns(value string) error {
	if err := requiredReferenceColumns(value); err != nil {
		return err
	}
	_, err := f.change()
	return err
}

func (f *foreignKeyForm) showValidationError() {
	f.rebuildForm()
	if requiredForeignKeyColumns(f.values.columns) != nil {
		_ = f.form.GetFocusedField().Blur()
		return
	}
	_ = f.form.NextField()
	if requiredReferenceTable(f.values.referenceTable) != nil {
		_ = f.form.GetFocusedField().Blur()
		return
	}
	_ = f.form.NextField()
	_ = f.form.GetFocusedField().Blur()
}

func (f *foreignKeyForm) blur() {
	if f.form != nil {
		_ = f.form.GetFocusedField().Blur()
	}
}

func (f *foreignKeyForm) focus() tea.Cmd {
	if f.form == nil {
		return nil
	}
	return f.form.GetFocusedField().Focus()
}

func (f foreignKeyForm) change() (sharedsql.ForeignKeyChange, error) {
	change := sharedsql.ForeignKeyChange{
		Columns:          splitForeignKeyColumns(f.values.columns),
		ReferenceTable:   strings.TrimSpace(f.values.referenceTable),
		ReferenceColumns: splitForeignKeyColumns(f.values.referenceColumns),
		OnDelete:         strings.ToUpper(strings.TrimSpace(f.values.onDelete)),
		OnUpdate:         strings.ToUpper(strings.TrimSpace(f.values.onUpdate)),
	}
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return sharedsql.ForeignKeyChange{}, err
	}
	return change, nil
}

func splitForeignKeyColumns(value string) []string {
	columns := strings.Split(value, ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	return columns
}

func (f *foreignKeyForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
	if f.confirmation != nil {
		f.confirmation.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f foreignKeyForm) View() string {
	if f.saving {
		return statusStyle.Render("saving foreign-key changes")
	}
	if f.form == nil {
		return ""
	}
	return f.form.View()
}
