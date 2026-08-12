package workbench

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (m *Model) openTableFilter() tea.Cmd {
	m.structure.tableFiltering = true
	m.structure.tableFilterTab = m.Tab
	m.structure.tableFilterInput = textinput.New()
	m.structure.tableFilterInput.Prompt = "Filter: "
	m.structure.tableFilterInput.SetValue(m.tableFilterValue(m.Tab))
	return m.structure.tableFilterInput.Focus()
}

func (m *Model) closeTableFilter() {
	m.structure.tableFiltering = false
	m.structure.tableFilterInput.Blur()
}

func (m *Model) updateTableFilter(message tea.KeyPressMsg) tea.Cmd {
	if code := message.Key().Code; code == tea.KeyEscape || code == tea.KeyEnter {
		m.closeTableFilter()
		return nil
	}
	var command tea.Cmd
	m.structure.tableFilterInput, command = m.structure.tableFilterInput.Update(message)
	m.setTableFilterValue(m.structure.tableFilterTab, m.structure.tableFilterInput.Value())
	m.applyTableFilter(m.structure.tableFilterTab)
	return command
}

func (m *Model) resetTableFilter() {
	m.setTableFilterValue(m.Tab, "")
	m.applyTableFilter(m.Tab)
}

func (m *Model) tableFilterValue(tab workspaceTab) string {
	switch tab {
	case tabStructure:
		return m.structure.structureFilter
	case tabIndexes:
		return m.structure.indexesFilter
	case tabForeignKeys:
		return m.structure.foreignKeysFilter
	default:
		return ""
	}
}

func (m *Model) setTableFilterValue(tab workspaceTab, value string) {
	switch tab {
	case tabStructure:
		m.structure.structureFilter = value
	case tabIndexes:
		m.structure.indexesFilter = value
	case tabForeignKeys:
		m.structure.foreignKeysFilter = value
	}
}

func (m *Model) applyTableFilter(tab workspaceTab) {
	var result *table.Model
	var source []table.Row
	switch tab {
	case tabStructure:
		result, source = &m.structure.table, m.structure.rows
	case tabIndexes:
		result, source = &m.structure.indexes, m.structure.indexRows
	case tabForeignKeys:
		result, source = &m.structure.foreignKeys, m.structure.foreignKeyRows
	default:
		return
	}
	query := strings.ToLower(strings.TrimSpace(m.tableFilterValue(tab)))
	if query == "" {
		result.SetRows(source)
		return
	}
	rows := make([]table.Row, 0, len(source))
	for _, row := range source {
		if strings.Contains(strings.ToLower(strings.Join(row, " ")), query) {
			rows = append(rows, row)
		}
	}
	result.SetRows(rows)
}

func (m Model) tableFilterStatus(tab workspaceTab) string {
	if m.structure.tableFiltering && m.structure.tableFilterTab == tab {
		return m.structure.tableFilterInput.View() + " | enter/esc done"
	}
	if query := m.tableFilterValue(tab); query != "" {
		return "/ filter | r reset | " + query
	}
	return "/ filter | r reset"
}

func (m Model) selectedColumn() *sharedsql.ColumnInfo {
	row := m.structure.table.Cursor()
	rows := m.structure.table.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.structure.columns {
		if m.structure.columns[index].Name == rows[row][0] {
			return &m.structure.columns[index]
		}
	}
	return nil
}

func (m Model) selectedIndex() *sharedsql.IndexInfo {
	row := m.structure.indexes.Cursor()
	rows := m.structure.indexes.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.structure.indexInfo {
		if m.structure.indexInfo[index].Name == rows[row][0] {
			return &m.structure.indexInfo[index]
		}
	}
	return nil
}

func (m Model) selectedForeignKey() *sharedsql.ForeignKeyInfo {
	row := m.structure.foreignKeys.Cursor()
	rows := m.structure.foreignKeys.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.structure.foreignKeyInfo {
		if m.structure.foreignKeyInfo[index].ID == rows[row][0] {
			return &m.structure.foreignKeyInfo[index]
		}
	}
	return nil
}
