package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (m Model) updateContextMenu(message tea.Msg) (tea.Model, tea.Cmd) {
	menu := m.overlay.contextMenu
	selectAction := func(action string) (tea.Model, tea.Cmd) {
		m.overlay.contextMenu = nil
		switch action {
		case "insert_row":
			return m, m.openInsertRowForm()
		case "copy_cell":
			return m, m.copyBrowseCell()
		case "edit_cell":
			return m, m.openCellEditor()
		case "edit_row":
			return m, m.openBrowseForm()
		case "delete_row":
			m.confirmBrowseRowDelete()
		case "rename_table":
			return m, m.openTableForm(menu.database, menu.table)
		case "add_table":
			return m, m.openTableForm(menu.database, "")
		case "create_database":
			return m, m.openDatabaseForm("")
		case "rename_database":
			return m, m.openDatabaseForm(menu.database)
		case "delete_database":
			m.confirmDatabaseDelete(menu.database)
		case "create_schema":
			return m, m.openSchemaForm("")
		case "rename_schema":
			return m, m.openSchemaForm(menu.schema)
		case "delete_schema":
			m.confirmSchemaDelete(menu.schema)
		case "connect_database":
			return m.reconnectDatabase(menu.database)
		case "delete_table":
			m.confirmTableDelete(menu.database, menu.table)
		case "edit_profile":
			return m, m.editSelectedRecentConnection()
		case "delete_profile":
			m.confirmDeleteRecentConnection()
		case "query_log_yank":
			text, ok := m.queryLog.component.SelectedCellText()
			if !ok {
				return m, nil
			}
			m.setStatus("copied to clipboard")
			return m, copyQueryLogStatement(text)
		case "query_log_explain":
			statement, ok := m.queryLog.component.SelectedStatement()
			if !ok {
				return m, nil
			}
			m.overlay.explainPicker = newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, statement, m.layout.tableViewportWidth)
			if m.overlay.explainPicker == nil {
				return m, nil
			}
			return m, m.overlay.explainPicker.form.Init()
		case "query_log_detail":
			m.queryLog.component.OpenDetailAtCursor()
		}
		return m, nil
	}
	selectShortcut := func(shortcut string) (tea.Model, tea.Cmd, bool) {
		for _, option := range menu.options {
			if option.keys == shortcut {
				model, command := selectAction(option.action)
				return model, command, true
			}
		}
		return m, nil, false
	}
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		switch msg.Keystroke() {
		case "esc":
			m.overlay.contextMenu = nil
		case "up", "k":
			menu.selected = max(menu.selected-1, 0)
		case "down", "j":
			menu.selected = min(menu.selected+1, max(len(menu.options)-1, 0))
		default:
			// Real terminals report shifted letters as shift+a; match the
			// same strokes as the keybinding index (text first).
			for _, stroke := range keyStrokes(msg) {
				if model, command, ok := selectShortcut(stroke); ok {
					return model, command
				}
			}
		case "enter":
			if menu.selected >= 0 && menu.selected < len(menu.options) {
				return selectAction(menu.options[menu.selected].action)
			}
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			m.overlay.contextMenu = nil
			return m, nil
		}
		relY := msg.Mouse().Y - menu.y - 1
		if relY >= 2 && relY < 2+len(menu.options) {
			return selectAction(menu.options[relY-2].action)
		}
		m.overlay.contextMenu = nil
	}
	return m, nil
}

// openObjectContextMenu opens the scope object-list context menu at the
// given screen position: Add/Edit/Delete table mapped to the existing
// root handlers, with the scope qualification from the object. The
// dispatch in updateContextMenu already routes these actions through the
// existing table form and delete confirmation; nothing here invents a
// new DDL path. An empty scope or a non-object product opens nothing.
func (m *Model) openObjectContextMenu(x, y int) {
	object, ok := m.browse.component.SelectedObject()
	if !ok {
		return
	}
	options, database := m.objectMenuOptions(object)
	if len(options) == 0 {
		return
	}
	m.overlay.contextMenu = &contextMenuModel{
		options:  options,
		selected: 0,
		visible:  true,
		x:        x,
		y:        y,
		database: database,
		table:    object.Name,
	}
}

// objectMenuOptions returns the context-menu actions for a scope-listed
// object: Add/Edit/Delete table for every product, mirroring the schema
// sidebar's table-row menu. database carries the create qualifier: the
// object's database for MySQL/MongoDB, its schema for PostgreSQL.
func (m Model) objectMenuOptions(object sharedsql.SchemaObject) ([]menuOption, string) {
	options := []menuOption{
		{label: "Add new table", action: "add_table", keys: "a"},
		{label: "Edit table", action: "rename_table", keys: "e"},
		{label: "Delete table", action: "delete_table", keys: "d"},
	}
	database := object.Database
	if m.databaseInfo.Product == "PostgreSQL" {
		if schema, _, found := strings.Cut(object.Name, "."); found {
			database = schema
		}
	}
	return options, database
}

// confirmTableDelete opens the Delete table? confirmation for the given
// target, shared by the schema.delete_table binding and the context menu.
// Acceptance drops the table and refreshes the sidebar; decline clears the
// retained target.
func (m *Model) confirmTableDelete(database, table string) {
	m.overlay.deletePending = "table"
	m.overlay.deletePendingDatabase = database
	m.overlay.deletePendingName = table
	m.overlay.deleteConfirm = yesNoConfirmation("Delete table?", "DROP TABLE "+m.actionIdentifier(m.qualifiedTableName(database, table)), "delete_table")
}

// confirmSchemaDelete opens the Delete schema? confirmation for the given
// schema. RESTRICT is fixed policy: a schema with contained objects is left
// untouched when the server rejects the drop.
func (m *Model) confirmSchemaDelete(schema string) {
	m.overlay.deletePending = "schema"
	m.overlay.deletePendingName = schema
	m.overlay.deleteConfirm = yesNoConfirmation("Delete schema?", "DROP SCHEMA "+m.quoteIdentifier(schema)+" RESTRICT", "delete")
}

// confirmDatabaseDelete opens the Delete database? confirmation for the
// given database.
func (m *Model) confirmDatabaseDelete(database string) {
	m.overlay.deletePending = "database"
	m.overlay.deletePendingDatabase = database
	m.overlay.deleteConfirm = yesNoConfirmation("Delete database?", "DROP DATABASE "+m.quoteIdentifier(database), "delete")
}

// browseObjectAction runs one object-list table action (add/edit/delete)
// on the selected object, mirroring the object context menu's dispatch so
// the direct a/e/d keys and the "," menu share one path.
func (m *Model) browseObjectAction(action string) tea.Cmd {
	object, ok := m.browse.component.SelectedObject()
	if !ok {
		return nil
	}
	_, database := m.objectMenuOptions(object)
	switch action {
	case "add_table":
		return m.openTableForm(database, "")
	case "rename_table":
		return m.openTableForm(database, object.Name)
	case "delete_table":
		m.confirmTableDelete(database, object.Name)
	}
	return nil
}

// confirmBrowseRowDelete opens the Delete this row? confirmation for the
// selected browse row, shared by the browse context menu and the direct
// delete_row binding. Acceptance runs the row delete flow (deleteRow).
func (m *Model) confirmBrowseRowDelete() {
	m.overlay.deleteConfirm = newConfirmationDialog("Delete this row?", "", []confirmationOption{
		{Label: "Yes, delete", Action: "delete"},
		{Label: "Cancel", Action: "cancel"},
	})
}

func (m *Model) copyBrowseCell() tea.Cmd {
	row, col := m.browse.component.Table.Cursor(), m.browse.component.SelectedColumn
	if row < 0 || row >= len(m.browse.component.Result.Rows) || col < 0 || col >= len(m.browse.component.Result.Columns) {
		return nil
	}
	display := ""
	if row < len(m.browse.component.Table.Rows()) && col < len(m.browse.component.Table.Rows()[row]) {
		display = m.browse.component.Table.Rows()[row][col]
	}
	value := m.rawCellValue("browse", row, col, display)
	m.setStatus("copied to clipboard")
	return copyQueryLogStatement(value)
}

func (m *Model) copySQLCell() tea.Cmd {
	row, col := m.queryLog.results.Cursor(), m.layout.resultsColumn
	if row < 0 || row >= len(m.queryLog.resultsRaw) || col < 0 || col >= len(m.queryLog.resultsRaw[row]) {
		return nil
	}
	value := ""
	if cell := m.queryLog.resultsRaw[row][col]; cell != nil {
		value = *cell
	}
	m.setStatus("copied to clipboard")
	return copyQueryLogStatement(value)
}
