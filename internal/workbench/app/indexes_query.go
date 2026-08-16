package app

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
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
	change, err := m.schema.component.Structure.IndexForm.Change()
	if err != nil {
		return func() tea.Msg { return indexChangedMsg{err: err} }
	}
	if m.ReadOnly {
		return func() tea.Msg { return indexChangedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	table, service, previous := m.SelectedTable, m.Database, m.schema.component.Structure.IndexForm.Previous
	statement, startedAt := m.indexChangeStatement(table, previous, change), time.Now()
	return func() tea.Msg {
		if previous == "" {
			return indexChangedMsg{statement: statement, startedAt: startedAt, err: service.CreateIndex(m.appContext, table, change)}
		}
		return indexChangedMsg{statement: statement, startedAt: startedAt, err: service.ReplaceIndex(m.appContext, table, previous, change)}
	}
}
func (m Model) deleteIndex() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return indexDeletedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	name := m.overlay.deletePendingName
	if name == "" {
		name = m.schema.component.Structure.IndexForm.Previous
	}
	table, service := m.SelectedTable, m.Database
	statement, startedAt := m.dropIndexStatement(table, name), time.Now()
	return func() tea.Msg {
		return indexDeletedMsg{statement: statement, startedAt: startedAt, err: service.DropIndex(m.appContext, table, name)}
	}
}
func (m Model) updateIndexes(message indexesLoadedMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			log.Error("loading indexes", message.err)
			m.setStatus(safeText(pluginFailureStatus(message.err, fmt.Sprintf("loading indexes: %v", message.err))))
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
	m.schema.component.Structure.Indexes.SetColumns(tableColumns([]string{"Name", "Kind", "Columns"}, rows))
	resizeResultsTable(&m.schema.component.Structure.Indexes, m.layout.tableViewportWidth, m.schema.component.Structure.Indexes.Height()+1)
	m.schema.component.Structure.IndexRows = rows
	m.schema.component.Structure.IndexInfo = message.indexes
	m.schema.component.ApplyTableFilter(tabIndexes)
	m.layout.indexesOffset = 0
	return m, nil
}
func (m Model) updateIndexChanged(message indexChangedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, nil, message.startedAt, message.err, "updated index"))
	}
	if message.err != nil {
		m.schema.component.Structure.IndexForm.Saving = false
		m.setStatus(safeText(pluginFailureStatus(message.err, fmt.Sprintf("updating index: %v", message.err))))
		return m, nil
	}
	m.schema.component.Structure.IndexForm.Close()
	m.setStatus("index updated")
	return m, tea.Batch(m.loadIndexes(), m.loadTableInfo(), m.loadSchemaIndexesAll())
}
func (m Model) updateIndexDeleted(message indexDeletedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, nil, message.startedAt, message.err, "dropped index"))
	}
	if message.err != nil {
		m.schema.component.Structure.IndexForm.Saving = false
		m.setStatus(safeText(pluginFailureStatus(message.err, fmt.Sprintf("deleting index: %v", message.err))))
		return m, nil
	}
	m.schema.component.Structure.IndexForm.Close()
	m.setStatus("index deleted")
	return m, tea.Batch(m.loadIndexes(), m.loadTableInfo(), m.loadSchemaIndexesAll())
}
func (m *Model) openIndexForm(index *sharedsql.IndexInfo) tea.Cmd {
	component, cmd := m.schema.component.OpenIndexForm(index, m.workspaceLayout(), m.keybindings)
	m.schema.component = component
	m.overlay.formMode.ButtonsFocused = false
	return m.openForm(cmd, component.Structure.IndexForm.Focus)
}
