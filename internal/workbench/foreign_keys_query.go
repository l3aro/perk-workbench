package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type foreignKeysLoadedMsg struct {
	table       string
	foreignKeys []sharedsql.ForeignKeyInfo
	err         error
}
type referencingForeignKeysLoadedMsg struct {
	table       string
	foreignKeys []sharedsql.ReferencingForeignKeyInfo
	err         error
}
type foreignKeyChangedMsg struct {
	statement string
	startedAt time.Time
	err       error
}
type foreignKeyDeletedMsg struct {
	statement string
	startedAt time.Time
	err       error
}

func (m Model) loadForeignKeys() tea.Cmd {
	table, service := m.SelectedTable, m.Database
	return func() tea.Msg {
		foreignKeys, err := service.ListForeignKeys(m.appContext, table)
		return foreignKeysLoadedMsg{table: table, foreignKeys: foreignKeys, err: err}
	}
}

func (m Model) loadReferencingForeignKeys() tea.Cmd {
	table, service := m.SelectedTable, m.Database
	return func() tea.Msg {
		foreignKeys, err := service.ListReferencingForeignKeys(m.appContext, table)
		return referencingForeignKeysLoadedMsg{table: table, foreignKeys: foreignKeys, err: err}
	}
}

func (m Model) saveForeignKey() tea.Cmd {
	change, err := m.foreignKeyForm.change()
	if err != nil {
		return func() tea.Msg { return foreignKeyChangedMsg{err: err} }
	}
	table, service, previous := m.SelectedTable, m.Database, m.foreignKeyForm.previous
	statement, startedAt := m.foreignKeyChangeStatement(table, previous, change), time.Now()
	return func() tea.Msg {
		if previous == "" {
			return foreignKeyChangedMsg{statement: statement, startedAt: startedAt, err: service.CreateForeignKey(m.appContext, table, change)}
		}
		return foreignKeyChangedMsg{statement: statement, startedAt: startedAt, err: service.ReplaceForeignKey(m.appContext, table, previous, change)}
	}
}

func (m Model) deleteForeignKey() tea.Cmd {
	table, service, previous := m.SelectedTable, m.Database, m.foreignKeyForm.previous
	statement, startedAt := m.dropForeignKeyStatement(table, previous), time.Now()
	return func() tea.Msg {
		return foreignKeyDeletedMsg{statement: statement, startedAt: startedAt, err: service.DropForeignKey(m.appContext, table, previous)}
	}
}

func (m Model) updateForeignKeys(message foreignKeysLoadedMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("loading foreign keys: %v", message.err))
		}
		return m, nil
	}
	rows := make([]table.Row, len(message.foreignKeys))
	for index, foreignKey := range message.foreignKeys {
		rows[index] = table.Row{safeText(foreignKey.ID), safeText(strings.Join(foreignKey.Columns, ", ")), safeText(foreignKey.ReferenceTable), safeText(strings.Join(foreignKey.ReferenceColumns, ", ")), safeText(foreignKey.OnDelete), safeText(foreignKey.OnUpdate)}
	}
	m.foreignKeys.SetColumns(tableColumns([]string{"ID", "Columns", "Reference table", "Reference columns", "On delete", "On update"}, rows))
	resizeResultsTable(&m.foreignKeys, m.tableViewportWidth, m.foreignKeys.Height()+1)
	m.foreignKeys.SetRows(rows)
	m.foreignKeyInfo = message.foreignKeys
	m.foreignKeysOffset = 0
	return m, nil
}

func (m Model) updateReferencingForeignKeys(message referencingForeignKeysLoadedMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("loading referencing foreign keys: %v", message.err))
		}
		return m, nil
	}
	m.referencingForeignKeyInfo = message.foreignKeys
	return m, nil
}

func (m Model) updateForeignKeyChanged(message foreignKeyChangedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "updated foreign key"))
	}
	if message.err != nil {
		m.foreignKeyForm.saving = false
		m.Status = safeText(fmt.Sprintf("updating foreign key: %v", message.err))
		return m, nil
	}
	m.foreignKeyForm.close()
	m.Status = "foreign key updated"
	return m, tea.Batch(m.loadForeignKeys(), m.loadReferencingForeignKeys(), m.loadTableInfo())
}

func (m Model) updateForeignKeyDeleted(message foreignKeyDeletedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "dropped foreign key"))
	}
	if message.err != nil {
		m.foreignKeyForm.saving = false
		m.Status = safeText(fmt.Sprintf("deleting foreign key: %v", message.err))
		return m, nil
	}
	m.foreignKeyForm.close()
	m.Status = "foreign key deleted"
	return m, tea.Batch(m.loadForeignKeys(), m.loadReferencingForeignKeys(), m.loadTableInfo())
}

func (m *Model) openForeignKeyForm(foreignKey *sharedsql.ForeignKeyInfo) tea.Cmd {
	m.foreignKeyForm = newForeignKeyForm(foreignKey)
	m.foreignKeyForm.keybindings = m.keybindings
	m.foreignKeyForm.setWidth(m.tableViewportWidth)
	return m.foreignKeyForm.form.Init()
}
