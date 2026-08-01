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
			if model, command, ok := selectShortcut(msg.Keystroke()); ok {
				return model, command
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
