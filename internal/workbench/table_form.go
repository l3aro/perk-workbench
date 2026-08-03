package workbench

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type tableFormAction uint8

const (
	tableFormNoAction tableFormAction = iota
	tableFormClose
	tableFormSave
	tableFormExecute
)

// tableForm is the create/rename table popup: a single name input plus a
// confirmation for the generated DDL. Rename mode carries the original name
// so an unchanged save closes without any SQL. The typed name is kept
// verbatim for quoted SQL; it is only trimmed to test emptiness and equality.
type tableForm struct {
	form          *huh.Form
	confirmation  *confirmationDialog
	name          string
	nameValue     *string
	originalName  string
	database      string
	table         string
	width, height int
	keybindings   Keybindings
}

func newTableForm(database, table string) tableForm {
	name := table
	form := tableForm{
		originalName: table,
		database:     database,
		table:        table,
		name:         table,
		nameValue:    &name,
		keybindings:  DefaultKeybindings(),
	}
	form.rebuildForm()
	return form
}

func (f tableForm) active() bool     { return f.form != nil }
func (f tableForm) confirming() bool { return f.confirmation != nil }
func (m *Model) openTableForm(database, table string) tea.Cmd {
	m.tableForm = newTableForm(database, table)
	m.tableForm.keybindings = m.keybindings
	m.tableForm.setWidth(m.tableViewportWidth)
	m.tableForm.setHeight(m.formViewportHeight())
	m.tableForm.form.Init()
	return m.formMode.beginHuh(m.tableForm.focus())
}

func (f *tableForm) Update(message tea.Msg, controller *formModeController) (tea.Cmd, tableFormAction) {
	if f.confirmation != nil {
		completed, action := f.confirmation.Update(message, f.width, f.height)
		if !completed {
			return nil, tableFormNoAction
		}
		f.confirmation = nil
		controller.mode = formModeNormal
		if action != "confirm" {
			return nil, tableFormNoAction
		}
		return nil, tableFormExecute
	}
	if keyPress, ok := message.(tea.KeyPressMsg); ok &&
		(keyPress.Key().Code == tea.KeyEnter || f.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal})) &&
		!controller.buttonsFocused {
		return f.save(controller)
	}
	if route := controller.routeHuh(message, f.blur); route != formRouteParent {
		if route == formRouteHuh {
			return f.updateHuh(message, controller)
		}
		return nil, tableFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil, tableFormNoAction
	}
	if route, replay, cmd := controller.routeFormButtons(keyPress, f.keybindings, func() tea.Cmd { return f.focus() }); route != formButtonContinue {
		if route == formButtonReplay {
			keyPress = replay
		} else {
			return cmd, tableFormNoAction
		}
	}
	switch {
	case isInsertModeKey(keyPress), f.keybindings.Match(keyPress, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}):
		return controller.beginHuh(f.focus()), tableFormNoAction
	case f.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}):
		return f.save(controller)
	case f.keybindings.Match(keyPress, "form.discard", []scope{scopeForm, scopeView, scopeGlobal}):
		f.close()
		return nil, tableFormClose
	}
	return nil, tableFormNoAction
}

func (f *tableForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, tableFormAction) {
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if input, ok := f.form.GetFocusedField().(*editableInput); ok {
		f.name = *input.value
	}
	if f.form.State == huh.StateCompleted {
		f.rebuildForm()
		return f.focus(), tableFormNoAction
	}
	return command, tableFormNoAction
}

func (f *tableForm) save(controller *formModeController) (tea.Cmd, tableFormAction) {
	if err := requiredTableName(f.name); err != nil {
		if f.nameValue != nil {
			*f.nameValue = f.name
		}
		model, command := f.form.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		f.form = model.(*huh.Form)
		return command, tableFormNoAction
	}
	if f.table != "" && f.name == f.originalName {
		f.close()
		return nil, tableFormClose
	}
	title := "Create table?"
	if f.table != "" {
		title = "Rename table?"
	}
	f.confirmation = yesNoConfirmation(title, "", "confirm")
	controller.beginConfirm()
	return nil, tableFormSave
}

// statement returns the DDL for the pending create or rename, quoting
// identifiers with the active product's rules and keeping the typed name
// verbatim. MySQL/PostgreSQL qualify the old name with the selected database
// so the ALTER targets the right schema; the new name stays bare for
// PostgreSQL, matching its RENAME TO semantics.
func (f tableForm) statement(m Model) string {
	if f.table != "" {
		oldName := f.table
		if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
			oldName = m.qualifiedTableName(f.database, f.table)
		}
		newName := f.name
		if m.databaseInfo.Product == "MySQL" {
			newName = m.qualifiedTableName(f.database, f.name)
		}
		return "ALTER TABLE " + m.actionIdentifier(oldName) + " RENAME TO " + m.actionIdentifier(newName)
	}
	return "CREATE TABLE " + m.actionIdentifier(m.qualifiedTableName(f.database, f.name)) + " (id INTEGER PRIMARY KEY)"
}

// qualifiedTableName returns name for SQLite and database.name for
// MySQL/PostgreSQL, preserving the selected schema.
func (m Model) qualifiedTableName(database, name string) string {
	if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
		return database + "." + name
	}
	return name
}

func (f tableForm) View() string {
	if f.form == nil {
		return ""
	}
	return f.form.View()
}

func (f *tableForm) close() { f.form = nil }

func (f *tableForm) focus() tea.Cmd {
	if f.form == nil {
		return nil
	}
	return f.form.GetFocusedField().Focus()
}

func (f *tableForm) blur() {
	if f.form != nil {
		_ = f.form.GetFocusedField().Blur()
	}
}

func (f *tableForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f *tableForm) setHeight(height int) {
	f.height = max(height, 1)
}

func (f *tableForm) rebuildForm() {
	if f.nameValue == nil {
		name := f.name
		f.nameValue = &name
	} else {
		*f.nameValue = f.name
	}
	f.form = newForm(huh.NewGroup(
		newEditableInput(huh.NewInput().Key("name").Title("Table name").Value(f.nameValue).Validate(requiredTableName), f.nameValue),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func requiredTableName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("table name is required")
	}
	return nil
}
