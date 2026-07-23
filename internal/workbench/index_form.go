package workbench

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

const (
	indexKindNormal  = "Index"
	indexKindUnique  = "Unique"
	indexKindPrimary = "Primary key"
)

type indexFormAction uint8

const (
	indexFormNoAction indexFormAction = iota
	indexFormSave
	indexFormDiscard
	indexFormDelete
)

type indexForm struct {
	form, confirmation *huh.Form
	values             *indexFormValues
	previous           string
	width              int
	saving             bool
	confirmationSave   bool
	confirmationDelete bool
}

type indexFormValues struct {
	name, columns string
	kind          string
	confirmed     bool
}

func newIndexForm(index *sharedsql.IndexInfo) indexForm {
	form := indexForm{values: &indexFormValues{kind: indexKindNormal}}
	if index != nil {
		form.previous, form.values.name, form.values.columns = index.Name, index.Name, strings.Join(index.Columns, ", ")
		switch {
		case index.PrimaryKey:
			form.values.kind = indexKindPrimary
		case index.Unique:
			form.values.kind = indexKindUnique
		}
	}
	form.rebuildForm()
	return form
}

func (f indexForm) active() bool { return f.values != nil }

func (f indexForm) confirming() bool { return f.confirmation != nil }

func (f *indexForm) close() { *f = indexForm{} }

func (f *indexForm) Update(message tea.Msg, controller *formModeController) (tea.Cmd, indexFormAction) {
	if f.saving {
		return nil, indexFormNoAction
	}
	if route := controller.routeHuh(message, f.blur); route != formRouteParent {
		if route == formRouteConsumed && f.confirmation != nil && controller.mode == formModeNormal {
			f.confirmation = nil
		}
		if route == formRouteHuh {
			return f.updateHuh(message, controller)
		}
		return nil, indexFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil, indexFormNoAction
	}
	switch keyPress.String() {
	case "i", "enter":
		return controller.beginHuh(f.focus()), indexFormNoAction
	case "ctrl+enter", "f5":
		if _, err := f.change(); err != nil {
			f.showValidationError()
			return nil, indexFormNoAction
		}
		f.beginConfirmation(true, false)
		controller.beginConfirm()
		return f.confirmation.Init(), indexFormNoAction
	case "esc", "escape":
		f.beginConfirmation(false, false)
		controller.beginConfirm()
		return f.confirmation.Init(), indexFormNoAction
	case "d":
		if f.previous != "" {
			f.beginConfirmation(false, true)
			controller.beginConfirm()
			return f.confirmation.Init(), indexFormNoAction
		}
	case "j", "down":
		return f.form.NextField(), indexFormNoAction
	case "k", "up":
		return f.form.PrevField(), indexFormNoAction
	}
	return nil, indexFormNoAction
}

func (f *indexForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, indexFormAction) {
	if f.confirmation != nil {
		model, command := f.confirmation.Update(message)
		f.confirmation = model.(*huh.Form)
		if f.confirmation.State != huh.StateCompleted {
			return command, indexFormNoAction
		}
		confirmed := f.values.confirmed || f.confirmation.GetBool("confirm")
		f.confirmation = nil
		controller.mode = formModeNormal
		if !confirmed {
			return nil, indexFormNoAction
		}
		if f.confirmationDelete {
			return nil, indexFormDelete
		}
		if f.confirmationSave {
			return nil, indexFormSave
		}
		return nil, indexFormDiscard
	}
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if f.form.State == huh.StateCompleted {
		f.rebuildForm()
		return f.focus(), indexFormNoAction
	}
	return command, indexFormNoAction
}

func (f *indexForm) beginConfirmation(save, delete bool) {
	f.values.confirmed, f.confirmationSave, f.confirmationDelete = false, save, delete
	title := "Discard index changes?"
	if save {
		title = "Save index changes?"
	}
	if delete {
		title = "Delete index?"
	}
	f.confirmation = huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Key("confirm").Title(title).Affirmative("Yes").Negative("No").Value(&f.values.confirmed),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func (f *indexForm) rebuildForm() {
	f.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().Key("name").Title("Name*").Value(&f.values.name).Validate(f.validateName),
		huh.NewInput().Key("columns").Title("Columns*").Value(&f.values.columns).Validate(requiredIndexColumns),
		huh.NewSelect[string]().Key("kind").Title("Kind").Options(
			huh.NewOption(indexKindNormal, indexKindNormal),
			huh.NewOption(indexKindUnique, indexKindUnique),
			huh.NewOption(indexKindPrimary, indexKindPrimary),
		).Value(&f.values.kind),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func (f indexForm) validateName(value string) error {
	if f.values.kind == indexKindPrimary {
		return nil
	}
	return requiredIndexName(value)
}

func requiredIndexName(value string) error {
	if strings.TrimSpace(value) == "" {
		return sharedsql.ValidateIndexChange(sharedsql.IndexChange{Name: value, Columns: []string{"column"}})
	}
	return nil
}

func requiredIndexColumns(value string) error {
	columns := strings.Split(value, ",")
	return sharedsql.ValidateIndexChange(sharedsql.IndexChange{Name: "index", Columns: columns})
}

func (f *indexForm) showValidationError() {
	f.rebuildForm()
	if f.values.kind != indexKindPrimary && requiredIndexName(f.values.name) != nil {
		_ = f.form.GetFocusedField().Blur()
		return
	}
	_ = f.form.NextField()
	_ = f.form.GetFocusedField().Blur()
}

func (f *indexForm) blur() {
	if f.form != nil {
		_ = f.form.GetFocusedField().Blur()
	}
}

func (f *indexForm) focus() tea.Cmd {
	if f.form == nil {
		return nil
	}
	return f.form.GetFocusedField().Focus()
}

func (f indexForm) change() (sharedsql.IndexChange, error) {
	if f.values.kind != indexKindPrimary {
		if err := requiredIndexName(f.values.name); err != nil {
			return sharedsql.IndexChange{}, err
		}
	}
	if err := requiredIndexColumns(f.values.columns); err != nil {
		return sharedsql.IndexChange{}, err
	}
	columns := strings.Split(f.values.columns, ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	change := sharedsql.IndexChange{Name: strings.TrimSpace(f.values.name), Columns: columns}
	switch f.values.kind {
	case indexKindUnique:
		change.Unique = true
	case indexKindPrimary:
		change.Name, change.PrimaryKey = "PRIMARY", true
	}
	if err := sharedsql.ValidateIndexChange(change); err != nil {
		return sharedsql.IndexChange{}, err
	}
	return change, nil
}

func (f *indexForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
	if f.confirmation != nil {
		f.confirmation.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f indexForm) View() string {
	if f.saving {
		return statusStyle.Render("saving index changes")
	}
	if f.form == nil {
		return ""
	}
	return f.form.View()
}
