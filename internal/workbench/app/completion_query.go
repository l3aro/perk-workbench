package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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

// startCompletion triggers context-aware completion suggestions. Only
// SQL offers relational table/column completion; other query languages
// leave the editor without completion.
func (m *Model) startCompletion() tea.Cmd {
	if !m.isSQLLanguage() {
		return nil
	}
	row := m.queryLog.editor.text.input.Line()
	col := m.queryLog.editor.text.input.Column()
	analysis := sharedsql.AnalyzeSQL(m.queryLog.editor.value, row, col)

	switch analysis.Context {
	case sharedsql.CtxQualified:
		return m.qualifiedCompletion(analysis)
	case sharedsql.CtxTable:
		m.queryLog.editor.showCompletion(m.tableContextItems(analysis))
		return nil
	case sharedsql.CtxExpression:
		m.queryLog.editor.showCompletion(m.expressionContextItems(analysis))
		return nil
	default:
		m.queryLog.editor.showCompletion(m.genericItems())
		return nil
	}
}

// qualifiedCompletion handles schema. or table. prefix.
func (m *Model) qualifiedCompletion(analysis sharedsql.SQLAnalysis) tea.Cmd {
	qualifier := analysis.Qualifier

	// Check if the qualifier is an alias for a table.
	if resolved, ok := analysis.Aliases[strings.ToLower(qualifier)]; ok {
		qualifier = resolved
	}

	// If qualifier matches a known table name (from schemaObjects), show its columns.
	if resolved, columns := m.columnsForTableName(qualifier); resolved {
		m.queryLog.editor.showCompletionFor(analysis.Prefix, columns)
		return nil
	}

	// If qualifier matches a schema name, show objects in that schema.
	if objects := m.objectsForSchema(qualifier); len(objects) > 0 {
		// Also include keywords as fallback.
		items := make([]CompletionItem, 0, len(objects)+len(sharedsql.Keywords))
		items = append(items, objects...)
		for _, kw := range sharedsql.Keywords {
			items = append(items, keywordItem(kw))
		}
		m.queryLog.editor.showCompletionFor(analysis.Prefix, items)
		return nil
	}

	// If qualifier hasn't been resolved, try loading columns from the database.
	if table := m.schemaTableForName(qualifier); table != "" {
		if columns, ok := m.queryLog.completionColumns[table]; ok {
			items := make([]CompletionItem, len(columns))
			for i, c := range columns {
				items[i] = completionItemForColumn(c, "", qualifier)
			}
			m.queryLog.editor.showCompletionFor(analysis.Prefix, items)
			return nil
		}
		m.queryLog.completionRequestTag++
		m.queryLog.completionTable = table
		return m.loadCompletionColumns(table, m.queryLog.completionRequestTag)
	}

	// Fallback: generic items.
	m.queryLog.editor.showCompletion(m.genericItems())
	return nil
}

// columnsForTableName checks if qualifier matches a known table and returns its cached columns.
func (m Model) columnsForTableName(qualifier string) (bool, []CompletionItem) {
	for _, object := range m.schema.component.Objects {
		if object.Type == "database" {
			continue
		}
		if !strings.EqualFold(object.Name, qualifier) {
			continue
		}
		table := m.schemaTable(schema.Item{Database: object.Database, Table: object.Name})
		if columns, ok := m.queryLog.completionColumns[table]; ok {
			items := make([]CompletionItem, len(columns))
			for i, c := range columns {
				items[i] = completionItemForColumn(c, "", object.Name)
			}
			return true, items
		}
	}
	return false, nil
}

// schemaTableForName finds the full schema.table key for a given table name.
func (m Model) schemaTableForName(name string) string {
	for _, object := range m.schema.component.Objects {
		if object.Type == "database" {
			continue
		}
		if strings.EqualFold(object.Name, name) || strings.EqualFold(m.completionObjectName(object), name) {
			return m.schemaTable(schema.Item{Database: object.Database, Table: object.Name})
		}
	}
	return ""
}

// objectsForSchema returns completion items for objects in a given schema.
func (m Model) objectsForSchema(schema string) []CompletionItem {
	var items []CompletionItem
	for _, object := range m.schema.component.Objects {
		if object.Type == "database" {
			continue
		}
		if strings.EqualFold(object.Database, schema) {
			items = append(items, completionItemForObject(object))
		}
	}
	return items
}

// tableContextItems returns items appropriate after FROM/JOIN/INTO.
func (m Model) tableContextItems(analysis sharedsql.SQLAnalysis) []CompletionItem {
	// Schemas, tables, views, CTEs (from aliases), and keywords.
	var items []CompletionItem

	// Add schema names.
	seenSchema := make(map[string]bool)
	for _, object := range m.schema.component.Objects {
		if object.Database != "" && !seenSchema[object.Database] {
			seenSchema[object.Database] = true
			items = append(items, CompletionItem{
				Label: object.Database, InsertText: object.Database,
				Kind: KindSchema, Detail: "schema",
			})
		}
	}

	// Add tables and views.
	for _, object := range m.schema.component.Objects {
		if object.Type != "database" {
			items = append(items, completionItemForObject(object))
		}
	}

	// Add CTE-like aliases found in the query.
	for alias := range analysis.Aliases {
		items = append(items, CompletionItem{
			Label: alias, InsertText: alias,
			Kind: KindTable, Detail: "CTE",
		})
	}

	// Add keywords.
	for _, kw := range sharedsql.Keywords {
		items = append(items, keywordItem(kw))
	}

	return items
}

// expressionContextItems returns items appropriate in SELECT/WHERE/ON.
func (m Model) expressionContextItems(analysis sharedsql.SQLAnalysis) []CompletionItem {
	var items []CompletionItem

	// 1. Result columns from the last query.
	for _, col := range m.queryLog.results.Columns() {
		items = append(items, completionItemForColumn(col.Title, "", "result"))
	}

	// 1b. Built-in functions.
	items = append(items, m.builtinFunctionItems()...)

	// 2. Columns from referenced tables.
	for _, table := range analysis.ReferencedTables {
		for _, object := range m.schema.component.Objects {
			if !strings.EqualFold(object.Name, table) && !strings.EqualFold(m.completionObjectName(object), table) {
				continue
			}
			tableKey := m.schemaTable(schema.Item{Database: object.Database, Table: object.Name})
			if columns, ok := m.queryLog.completionColumns[tableKey]; ok {
				for _, col := range columns {
					items = append(items, completionItemForColumn(col, "", object.Name))
				}
			}
		}
	}

	// 3. Buffer words (identifiers already in the query).
	for _, word := range sharedsql.ExtractBufferWordsTokens(analysis.Words) {
		items = append(items, bufferWordItem(word))
	}

	// 4. Keywords.
	for _, kw := range sharedsql.Keywords {
		items = append(items, keywordItem(kw))
	}

	// 5. All schema objects (tables/views can appear in expressions too).
	for _, object := range m.schema.component.Objects {
		if object.Type != "database" {
			items = append(items, completionItemForObject(object))
		}
	}

	return items
}

// builtinFunctionItems returns CompletionItems for built-in functions.
func (m Model) builtinFunctionItems() []CompletionItem {
	functions, ok := BuiltinFunctions[m.databaseInfo.Product]
	if !ok {
		return nil
	}
	items := make([]CompletionItem, len(functions))
	for i, fn := range functions {
		items[i] = CompletionItem{
			Label: fn, InsertText: fn + "()",
			Kind: KindFunction, Detail: "built-in",
		}
	}
	return items
}

// genericItems returns a broad set of candidates.
func (m Model) genericItems() []CompletionItem {
	all := m.tableContextItems(sharedsql.SQLAnalysis{})
	return all
}

// ---- legacy helpers kept for compatibility ----

func (m Model) completionObjectName(object sharedsql.SchemaObject) string {
	if m.databaseInfo.Product == "MySQL" || m.databaseInfo.Product == "PostgreSQL" {
		return object.Database + "." + object.Name
	}
	return object.Name
}

func (m Model) updateCompletionColumns(message completionColumnsMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.queryLog.completionRequestTag || message.table != m.queryLog.completionTable || !m.isSQLLanguage() {
		return m, nil
	}
	if message.err != nil {
		m.setStatus(safeText(pluginFailureStatus(message.err, fmt.Sprintf("loading completion: %v", message.err))))
		return m, nil
	}
	columnNames := make([]string, len(message.columns))
	items := make([]CompletionItem, len(message.columns))
	for index, column := range message.columns {
		columnNames[index] = column.Name
		// Find the table name from the table key.
		tableName := message.table
		if parts := strings.SplitN(message.table, ".", 2); len(parts) == 2 {
			tableName = parts[1]
		}
		items[index] = completionItemForColumn(column.Name, column.Type, tableName)
	}
	m.queryLog.completionColumns[message.table] = columnNames
	m.queryLog.editor.showCompletionFor("", items)
	return m, nil
}
