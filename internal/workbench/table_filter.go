package workbench

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (m *Model) openTableFilter() tea.Cmd {
	m.tableFiltering = true
	m.tableFilterTab = m.Tab
	m.tableFilterInput = textinput.New()
	m.tableFilterInput.Prompt = "Filter: "
	m.tableFilterInput.SetValue(m.tableFilterValue(m.Tab))
	return m.tableFilterInput.Focus()
}

func (m *Model) updateTableFilter(message tea.KeyPressMsg) tea.Cmd {
	if message.Key().Code == tea.KeyEscape {
		m.tableFiltering = false
		m.tableFilterInput.Blur()
		return nil
	}
	var command tea.Cmd
	m.tableFilterInput, command = m.tableFilterInput.Update(message)
	m.setTableFilterValue(m.tableFilterTab, m.tableFilterInput.Value())
	m.applyTableFilter(m.tableFilterTab)
	return command
}

func (m *Model) resetTableFilter() {
	m.setTableFilterValue(m.Tab, "")
	m.applyTableFilter(m.Tab)
}

func (m *Model) tableFilterValue(tab workspaceTab) string {
	switch tab {
	case tabStructure:
		return m.structureFilter
	case tabIndexes:
		return m.indexesFilter
	case tabForeignKeys:
		return m.foreignKeysFilter
	default:
		return ""
	}
}

func (m *Model) setTableFilterValue(tab workspaceTab, value string) {
	switch tab {
	case tabStructure:
		m.structureFilter = value
	case tabIndexes:
		m.indexesFilter = value
	case tabForeignKeys:
		m.foreignKeysFilter = value
	}
}

func (m *Model) applyTableFilter(tab workspaceTab) {
	var result *table.Model
	var source []table.Row
	switch tab {
	case tabStructure:
		result, source = &m.structure, m.structureRows
	case tabIndexes:
		result, source = &m.indexes, m.indexRows
	case tabForeignKeys:
		result, source = &m.foreignKeys, m.foreignKeyRows
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
	if m.tableFiltering && m.tableFilterTab == tab {
		return m.tableFilterInput.View() + " | esc done"
	}
	if query := m.tableFilterValue(tab); query != "" {
		return "/ filter | r reset | " + query
	}
	return "/ filter | r reset"
}

func (m Model) selectedColumn() *sharedsql.ColumnInfo {
	row := m.structure.Cursor()
	rows := m.structure.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.structureColumns {
		if m.structureColumns[index].Name == rows[row][0] {
			return &m.structureColumns[index]
		}
	}
	return nil
}

func (m Model) selectedIndex() *sharedsql.IndexInfo {
	row := m.indexes.Cursor()
	rows := m.indexes.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.indexInfo {
		if m.indexInfo[index].Name == rows[row][0] {
			return &m.indexInfo[index]
		}
	}
	return nil
}

func (m Model) selectedForeignKey() *sharedsql.ForeignKeyInfo {
	row := m.foreignKeys.Cursor()
	rows := m.foreignKeys.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.foreignKeyInfo {
		if m.foreignKeyInfo[index].ID == rows[row][0] {
			return &m.foreignKeyInfo[index]
		}
	}
	return nil
}
