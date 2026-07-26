package workbench

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type completionColumnsMsg struct {
	tag     uint64
	table   string
	columns []sharedsql.ColumnInfo
	err     error
}

func (m Model) loadCompletionColumns(table string, tag uint64) tea.Cmd {
	service := m.Database
	return func() tea.Msg {
		columns, err := service.TableInfo(m.appContext, table)
		return completionColumnsMsg{tag: tag, table: table, columns: columns, err: err}
	}
}

func (m *Model) startCompletion() tea.Cmd {
	prefix := sqlCompletionPrefix(m.editor.value)
	if table := m.completionTableFor(prefix); table != "" {
		if columns, ok := m.completionColumns[table]; ok {
			m.editor.showCompletionFor("", columns)
			return nil
		}
		m.completionRequestTag++
		m.completionTable = table
		return m.loadCompletionColumns(table, m.completionRequestTag)
	}
	m.editor.showCompletion(m.completionValues())
	return nil
}

func (m Model) completionValues() []string {
	values := append([]string(nil), sqlKeywords...)
	for _, object := range m.schemaObjects {
		if object.Type != "database" {
			values = append(values, m.completionObjectName(object))
		}
	}
	return values
}

func (m Model) completionObjectName(object sharedsql.SchemaObject) string {
	if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
		return object.Database + "." + object.Name
	}
	return object.Name
}

func (m Model) completionTableFor(prefix string) string {
	if !strings.HasSuffix(prefix, ".") {
		return ""
	}
	name := strings.TrimSuffix(prefix, ".")
	for _, object := range m.schemaObjects {
		if object.Type != "database" && (name == object.Name || name == m.completionObjectName(object)) {
			return m.schemaTable(schemaItem{database: object.Database, table: object.Name})
		}
	}
	return ""
}

func (m Model) updateCompletionColumns(message completionColumnsMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.completionRequestTag || message.table != m.completionTable {
		return m, nil
	}
	if message.err != nil {
		m.Status = safeText(fmt.Sprintf("loading completion: %v", message.err))
		return m, nil
	}
	columns := make([]string, len(message.columns))
	for index, column := range message.columns {
		columns[index] = column.Name
	}
	m.completionColumns[message.table] = columns
	m.editor.showCompletionFor("", columns)
	return m, nil
}
