package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type indexesLoadedMsg struct {
	table   string
	indexes []sharedsql.IndexInfo
	err     error
}
type indexChangedMsg struct {
	statement string
	startedAt time.Time
	err       error
}
type indexDeletedMsg struct {
	statement string
	startedAt time.Time
	err       error
}

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
	statement, startedAt := m.indexChangeStatement(table, previous, change), time.Now()
	return func() tea.Msg {
		if previous == "" {
			return indexChangedMsg{statement: statement, startedAt: startedAt, err: service.CreateIndex(m.appContext, table, change)}
		}
		return indexChangedMsg{statement: statement, startedAt: startedAt, err: service.ReplaceIndex(m.appContext, table, previous, change)}
	}
}
func (m Model) deleteIndex() tea.Cmd {
	table, service, name := m.SelectedTable, m.Database, m.indexForm.previous
	statement, startedAt := m.dropIndexStatement(table, name), time.Now()
	return func() tea.Msg {
		return indexDeletedMsg{statement: statement, startedAt: startedAt, err: service.DropIndex(m.appContext, table, name)}
	}
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
	m.indexRows = rows
	m.indexInfo = message.indexes
	m.applyTableFilter(tabIndexes)
	m.indexesOffset = 0
	return m, nil
}
func (m Model) updateIndexChanged(message indexChangedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "updated index"))
	}
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
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "dropped index"))
	}
	if message.err != nil {
		m.indexForm.saving = false
		m.Status = safeText(fmt.Sprintf("deleting index: %v", message.err))
		return m, nil
	}
	m.indexForm.close()
	m.Status = "index deleted"
	return m, tea.Batch(m.loadIndexes(), m.loadTableInfo())
}
func (m *Model) openIndexForm(index *sharedsql.IndexInfo) tea.Cmd {
	m.indexForm = newIndexForm(index)
	m.indexForm.keybindings = m.keybindings
	m.indexForm.setWidth(m.tableViewportWidth)
	return m.indexForm.form.Init()
}
