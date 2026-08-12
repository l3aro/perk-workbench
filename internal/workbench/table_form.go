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

type tableFormObjectKind uint8

const (
	tableFormTable tableFormObjectKind = iota
	tableFormDatabase
	tableFormSchema
)

// tableForm is the create/rename popup for tables, databases, and schemas.
// Rename mode carries the original name so an unchanged save closes without
// any SQL. The typed name is kept verbatim for quoted SQL; it is only trimmed
// to test emptiness and equality.
type tableForm struct {
	form          *huh.Form
	confirmation  *confirmationDialog
	name          string
	nameValue     *string
	originalName  string
	database      string
	table         string // qualified old name for table renames
	objectKind    tableFormObjectKind
	width, height int
	keybindings   Keybindings
}

func newTableForm(database, table string) tableForm {
	name := table
	original := table
	// PostgreSQL sidebar tables carry schema.table; the popup edits the
	// bare name while the qualified name stays the ALTER target.
	if _, bare, found := strings.Cut(table, "."); found {
		name, original = bare, bare
	}
	form := tableForm{
		originalName: original,
		database:     database,
		table:        table,
		name:         name,
		nameValue:    &name,
		keybindings:  DefaultKeybindings(),
	}
	form.rebuildForm()
	return form
}

func newDatabaseForm(originalName string) tableForm {
	name := originalName
	form := tableForm{
		originalName: originalName,
		name:         originalName,
		nameValue:    &name,
		objectKind:   tableFormDatabase,
		keybindings:  DefaultKeybindings(),
	}
	form.rebuildForm()
	return form
}

func newSchemaForm(originalName string) tableForm {
	name := originalName
	form := tableForm{
		originalName: originalName,
		name:         originalName,
		nameValue:    &name,
		objectKind:   tableFormSchema,
		keybindings:  DefaultKeybindings(),
	}
	form.rebuildForm()
	return form
}

func (m Model) supportsCreateDatabase() bool {
	return m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL"
}

func (m Model) supportsSchemas() bool { return m.databaseInfo.Product == "PostgreSQL" }

func (f tableForm) active() bool     { return f.form != nil }
func (f tableForm) confirming() bool { return f.confirmation != nil }
func (m *Model) openTableForm(database, table string) tea.Cmd {
	return m.openPopup(newTableForm(database, table))
}

func (m *Model) openDatabaseForm(originalName string) tea.Cmd {
	if originalName != "" && m.databaseInfo.Product != "PostgreSQL" {
		return nil
	}
	return m.openPopup(newDatabaseForm(originalName))
}

func (m *Model) openSchemaForm(originalName string) tea.Cmd {
	if !m.supportsSchemas() {
		return nil
	}
	return m.openPopup(newSchemaForm(originalName))
}

func (m *Model) openPopup(form tableForm) tea.Cmd {
	m.structure.tableForm = form
	m.structure.tableForm.keybindings = m.keybindings
	m.structure.tableForm.setWidth(m.layout.tableViewportWidth)
	m.structure.tableForm.setHeight(m.formViewportHeight())
	m.structure.tableForm.form.Init()
	return m.overlay.formMode.beginHuh(m.structure.tableForm.focus())
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
	// The Save/Cancel bar is a real focus target in both modes: route its
	// keys first so insert mode (vim off) never needs Escape to reach it.
	keyPress, ok := message.(tea.KeyPressMsg)
	replay := false
	if ok && controller.buttonsFocused {
		if route, replayed, cmd := controller.routeFormButtons(keyPress, f.keybindings, func() tea.Cmd { return f.focus() }); route != formButtonContinue {
			if route == formButtonReplay {
				keyPress, replay = replayed, true
			} else {
				return cmd, tableFormNoAction
			}
		}
	}
	if !replay {
		if route := controller.routeHuh(message, f.blur); route != formRouteParent {
			if route == formRouteHuh {
				return f.updateHuh(message, controller)
			}
			return nil, tableFormNoAction
		}
	}
	if !ok {
		return nil, tableFormNoAction
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
	// The name field is the form's only field, so Tab always lands on the bar.
	if keyPress, ok := message.(tea.KeyPressMsg); ok && controller.routeToBar(keyPress, true, f.blur) {
		return nil, tableFormNoAction
	}
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
	_, required, createTitle, editTitle := f.labels()
	if err := required(f.name); err != nil {
		if f.nameValue != nil {
			*f.nameValue = f.name
		}
		model, command := f.form.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		f.form = model.(*huh.Form)
		return command, tableFormNoAction
	}
	if f.originalName != "" && f.name == f.originalName {
		f.close()
		return nil, tableFormClose
	}
	title := createTitle
	if f.originalName != "" {
		title = editTitle
	}
	f.confirmation = yesNoConfirmation(title, "", "confirm")
	controller.beginConfirm()
	return nil, tableFormSave
}

// statement returns the DDL for the pending create or rename, quoting
// identifiers with the active product's rules and keeping the typed name
// verbatim. MySQL qualifies names with the selected database so the ALTER
// targets the right schema; PostgreSQL table creates carry the target schema.
func (f tableForm) statement(m Model) string {
	switch f.objectKind {
	case tableFormDatabase:
		if f.originalName == "" {
			return "CREATE DATABASE " + m.quoteIdentifier(f.name)
		}
		if m.databaseInfo.Product == "PostgreSQL" {
			return "ALTER DATABASE " + m.quoteIdentifier(f.originalName) + " RENAME TO " + m.quoteIdentifier(f.name)
		}
		return ""
	case tableFormSchema:
		if m.databaseInfo.Product != "PostgreSQL" {
			return ""
		}
		if f.originalName == "" {
			return "CREATE SCHEMA " + m.quoteIdentifier(f.name)
		}
		return "ALTER SCHEMA " + m.quoteIdentifier(f.originalName) + " RENAME TO " + m.quoteIdentifier(f.name)
	}
	if f.originalName != "" {
		oldName := f.table
		if m.databaseInfo.Product == "MySQL" {
			oldName = m.qualifiedTableName(f.database, f.originalName)
		}
		newName := f.name
		if m.databaseInfo.Product == "MySQL" {
			newName = m.qualifiedTableName(f.database, f.name)
		}
		return "ALTER TABLE " + m.actionIdentifier(oldName) + " RENAME TO " + m.actionIdentifier(newName)
	}
	createName := f.name
	switch m.databaseInfo.Product {
	case "MySQL":
		createName = m.qualifiedTableName(f.database, f.name)
	case "PostgreSQL":
		// f.database carries the target schema from the sidebar item.
		createName = f.database + "." + f.name
	}
	return "CREATE TABLE " + m.actionIdentifier(createName) + " (id INTEGER PRIMARY KEY)"
}

// qualifiedTableName returns name for SQLite and PostgreSQL (whose sidebar
// tables already carry schema.table) and database.name for MySQL.
func (m Model) qualifiedTableName(database, name string) string {
	if m.databaseInfo.Product == "MySQL" {
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
	title, required, _, _ := f.labels()
	f.form = newForm(huh.NewGroup(
		newEditableInput(huh.NewInput().Key("name").Title(title).Value(f.nameValue).Validate(required), f.nameValue),
	)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
}

func (f tableForm) labels() (string, func(string) error, string, string) {
	switch f.objectKind {
	case tableFormDatabase:
		return "Database name", requiredDatabaseName, "Create database?", "Edit database?"
	case tableFormSchema:
		return "Schema name", requiredSchemaName, "Create schema?", "Edit schema?"
	default:
		return "Table name", requiredTableName, "Create table?", "Edit table?"
	}
}

func requiredTableName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("table name is required")
	}
	return nil
}

func requiredDatabaseName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("database name is required")
	}
	return nil
}

func requiredSchemaName(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("schema name is required")
	}
	return nil
}
