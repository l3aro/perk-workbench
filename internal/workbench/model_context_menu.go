package workbench

import (
	tea "charm.land/bubbletea/v2"
)

func (m Model) updateContextMenu(message tea.Msg) (tea.Model, tea.Cmd) {
	menu := m.contextMenu
	selectAction := func(action string) (tea.Model, tea.Cmd) {
		m.contextMenu = nil
		switch action {
		case "copy_cell":
			return m, m.copyBrowseCell()
		case "edit_cell":
			return m, m.openCellEditor()
		case "edit_row":
			return m, m.openBrowseForm()
		case "delete_row":
			m.deleteConfirm = newConfirmationDialog("Delete this row?", "", []confirmationOption{
				{label: "Yes, delete", action: "delete"},
				{label: "Cancel", action: "cancel"},
			})
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
		case "query_log_yank":
			entry, ok := m.queryLogSelectedEntry()
			if !ok {
				return m, nil
			}
			m.Status = "copied to clipboard"
			return m, copyQueryLogStatement(queryLogCell(entry, m.queryLogColumn))
		case "query_log_explain":
			entry, ok := m.queryLogSelectedEntry()
			if !ok {
				return m, nil
			}
			m.explainPicker = newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, entry.statement, m.tableViewportWidth)
			if m.explainPicker == nil {
				return m, nil
			}
			return m, m.explainPicker.form.Init()
		case "query_log_detail":
			if entry, ok := m.queryLogSelectedEntry(); ok {
				m.queryLogDetail = &entry
			}
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
			m.contextMenu = nil
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
			m.contextMenu = nil
			return m, nil
		}
		relY := msg.Mouse().Y - menu.y - 1
		if relY >= 2 && relY < 2+len(menu.options) {
			return selectAction(menu.options[relY-2].action)
		}
		m.contextMenu = nil
	}
	return m, nil
}

// confirmTableDelete opens the Delete table? confirmation for the given
// target, shared by the schema.delete_table binding and the context menu.
// Acceptance drops the table and refreshes the sidebar; decline clears the
// retained target.
func (m *Model) confirmTableDelete(database, table string) {
	m.deletePending = "table"
	m.deletePendingDatabase = database
	m.deletePendingName = table
	m.deleteConfirm = yesNoConfirmation("Delete table?", "DROP TABLE "+m.actionIdentifier(m.qualifiedTableName(database, table)), "delete_table")
}

// confirmSchemaDelete opens the Delete schema? confirmation for the given
// schema. RESTRICT is fixed policy: a schema with contained objects is left
// untouched when the server rejects the drop.
func (m *Model) confirmSchemaDelete(schema string) {
	m.deletePending = "schema"
	m.deletePendingName = schema
	m.deleteConfirm = yesNoConfirmation("Delete schema?", "DROP SCHEMA "+m.quoteIdentifier(schema)+" RESTRICT", "delete")
}

// confirmDatabaseDelete opens the Delete database? confirmation for the
// given database.
func (m *Model) confirmDatabaseDelete(database string) {
	m.deletePending = "database"
	m.deletePendingDatabase = database
	m.deleteConfirm = yesNoConfirmation("Delete database?", "DROP DATABASE "+m.quoteIdentifier(database), "delete")
}

func (m *Model) copyBrowseCell() tea.Cmd {
	row, col := m.browse.Cursor(), m.browseColumn
	if row < 0 || row >= len(m.browseResult.Rows) || col < 0 || col >= len(m.browseResult.Columns) {
		return nil
	}
	value := ""
	if cell := m.browseResult.Rows[row][col]; cell != nil {
		value = *cell
	}
	m.Status = "copied to clipboard"
	return copyQueryLogStatement(value)
}

func (m *Model) copySQLCell() tea.Cmd {
	row, col := m.results.Cursor(), m.resultsColumn
	if row < 0 || row >= len(m.resultsRaw) || col < 0 || col >= len(m.resultsRaw[row]) {
		return nil
	}
	value := ""
	if cell := m.resultsRaw[row][col]; cell != nil {
		value = *cell
	}
	m.Status = "copied to clipboard"
	return copyQueryLogStatement(value)
}
