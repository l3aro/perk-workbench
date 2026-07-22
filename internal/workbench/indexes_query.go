package workbench

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type indexesLoadedMsg struct {
	table   string
	indexes []sharedsql.IndexInfo
	err     error
}
type indexChangedMsg struct{ err error }
type indexDeletedMsg struct{ err error }

func (m Model) loadIndexes() tea.Cmd {
	table, service := m.SelectedTable, m.Database
	return func() tea.Msg {
		indexes, err := service.ListIndexes(m.appContext, table)
		return indexesLoadedMsg{table: table, indexes: indexes, err: err}
	}
}
func (m Model) saveIndex() tea.Cmd {
	change, err := m.indexForm.change()
	if err != nil {
		return func() tea.Msg { return indexChangedMsg{err: err} }
	}
	table, service, previous := m.SelectedTable, m.Database, m.indexForm.previous
	return func() tea.Msg {
		if previous == "" {
			return indexChangedMsg{err: service.CreateIndex(m.appContext, table, change)}
		}
		return indexChangedMsg{err: service.ReplaceIndex(m.appContext, table, previous, change)}
	}
}
func (m Model) deleteIndex() tea.Cmd {
	table, service, name := m.SelectedTable, m.Database, m.indexForm.previous
	return func() tea.Msg { return indexDeletedMsg{err: service.DropIndex(m.appContext, table, name)} }
}
func (m Model) updateIndexes(message indexesLoadedMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("loading indexes: %v", message.err))
		}
		return m, nil
	}
	rows := make([]table.Row, len(message.indexes))
	for i, index := range message.indexes {
		kind := "index"
		if index.PrimaryKey {
			kind = "primary key"
		} else if index.Unique {
			kind = "unique"
		}
		rows[i] = table.Row{safeText(index.Name), kind, safeText(strings.Join(index.Columns, ", "))}
	}
	m.indexes.SetColumns(tableColumns([]string{"Name", "Kind", "Columns"}, rows))
	resizeResultsTable(&m.indexes, m.tableViewportWidth, m.indexes.Height()+1)
	m.indexes.SetRows(rows)
	m.indexInfo = message.indexes
	m.indexesOffset = 0
	return m, nil
}
func (m Model) updateIndexChanged(message indexChangedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.indexForm.saving = false
		m.Status = safeText(fmt.Sprintf("updating index: %v", message.err))
		return m, nil
	}
	m.indexForm.close()
	m.Status = "index updated"
	return m, tea.Batch(m.loadIndexes(), m.loadTableInfo())
}
func (m Model) updateIndexDeleted(message indexDeletedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.indexForm.saving = false
		m.Status = safeText(fmt.Sprintf("deleting index: %v", message.err))
		return m, nil
	}
	m.indexForm.close()
	m.Status = "index deleted"
	return m, tea.Batch(m.loadIndexes(), m.loadTableInfo())
}
func (m *Model) openIndexForm(index *sharedsql.IndexInfo) { m.indexForm = newIndexForm(index) }
