package workbench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dustin/go-humanize"
	"github.com/l3aro/perk-workbench/internal/chrome"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type tableInfoMsg struct {
	table   string
	columns []sharedsql.ColumnInfo
	err     error
}

type browseTableMsg struct {
	table     string
	page      int
	tag       uint64
	startedAt time.Time
	result    sharedsql.Result
	err       error
}

type columnAlteredMsg struct {
	statement string
	startedAt time.Time
	err       error
}

type browseRowUpdatedMsg struct {
	statement string
	startedAt time.Time
	err       error
}

type deleteRowMsg struct {
	statement string
	startedAt time.Time
	err       error
}

func (m *Model) setResults(result sharedsql.Result) {
	m.resultsNumericColumns = numericColumns(result.ColumnTypes)
	titles := make([]string, len(result.Columns))
	for index, column := range result.Columns {
		titles[index] = safeText(column)
	}
	rows := make([]table.Row, len(result.Rows))
	for rowIndex, row := range result.Rows {
		cells := make(table.Row, len(row))
		for cellIndex, cell := range row {
			if cell == nil {
				cells[cellIndex] = "NULL"
			} else {
				cells[cellIndex] = cellText(*cell)
			}
		}
		rows[rowIndex] = cells
	}
	m.results.SetRows(nil)
	m.results.SetColumns(tableColumns(titles, rows))
	resizeResultsTable(&m.results, m.tableViewportWidth, max(m.resultsHeight-4, 2))
	m.results.SetRows(rows)
	m.resultsColumn, m.resultsOffset = 0, 0
	m.results.Focus()
	m.formMode.mode = formModeNormal
	m.editor.text.Blur()
	rowLabel := "rows"
	if len(rows) == 1 {
		rowLabel = "row"
	}
	affectedLabel := "rows"
	if result.RowsAffected == 1 {
		affectedLabel = "row"
	}
	m.resultsStatus = fmt.Sprintf("%d %s | %d %s affected | %s", len(rows), rowLabel, result.RowsAffected, affectedLabel, result.Duration)
	m.resultsStatus += colsHint(m.results.Columns(), m.tableViewportWidth)
	if result.Truncated {
		m.resultsStatus += " | truncated"
	}
	m.Status = ""
}

func (m Model) loadTableInfo() tea.Cmd {
	tableName, service := m.SelectedTable, m.Database
	return func() tea.Msg {
		columns, err := service.TableInfo(m.appContext, tableName)
		return tableInfoMsg{table: tableName, columns: columns, err: err}
	}
}

func (m Model) loadBrowse() tea.Cmd {
	tableName, page, tag, service := m.SelectedTable, m.BrowsePage, m.browsePageTag, m.Database
	settings := m.browseSettings
	columns := make([]string, len(m.structureColumns))
	for index, column := range m.structureColumns {
		columns[index] = column.Name
	}
	startedAt := time.Now()
	return func() tea.Msg {
		sorts := make([]sharedsql.BrowseSort, len(settings.sorts))
		for index, sort := range settings.sorts {
			sorts[index] = sharedsql.BrowseSort{Column: sort.column, Descending: sort.desc}
		}
		filters := slices.Clone(settings.filters)
		result, err := service.BrowseTable(m.appContext, tableName, sharedsql.BrowseOptions{
			Columns: columns, Filters: filters, Sorts: sorts,
			Offset: page * settings.pageSize(), Limit: settings.pageSize(),
		})
		return browseTableMsg{table: tableName, page: page, tag: tag, startedAt: startedAt, result: result, err: err}
	}
}

func (m Model) alterColumn() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return columnAlteredMsg{err: fmt.Errorf("connection is read-only")} }
	}
	table, service := m.SelectedTable, m.Database
	change, err := m.columnForm.change()
	if err != nil {
		return func() tea.Msg { return columnAlteredMsg{err: err} }
	}
	statement, startedAt := m.columnChangeStatement(table, change), time.Now()
	return func() tea.Msg {
		return columnAlteredMsg{statement: statement, startedAt: startedAt, err: service.AlterColumn(m.appContext, table, change)}
	}
}

func (m Model) updateBrowseRow() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return browseRowUpdatedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	statement, err := m.browseForm.updateStatement(m.SelectedTable)
	if err != nil {
		return func() tea.Msg { return browseRowUpdatedMsg{err: err} }
	}
	if statement == "" {
		return func() tea.Msg { return browseRowUpdatedMsg{} }
	}
	service, startedAt := m.Database, time.Now()
	return func() tea.Msg {
		result, err := service.Execute(m.appContext, statement)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return browseRowUpdatedMsg{statement: statement, startedAt: startedAt, err: err}
	}
}

func (m Model) updateTableInfo(message tableInfoMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("loading structure: %v", message.err))
		}
		return m, nil
	}
	rows := make([]table.Row, len(message.columns))
	for index, column := range message.columns {
		defaultValue := "NONE"
		if column.Nullable {
			defaultValue = "NULL"
		}
		if column.DefaultValue != nil {
			defaultValue = safeText(*column.DefaultValue)
		}
		nullable := chrome.BooleanValue(column.Nullable)
		rows[index] = table.Row{safeText(column.Name), indexIcons(column.Indexes), safeText(column.Type), safeText(column.Attributes), nullable, defaultValue}
	}
	// Center Nullable icons within column
	nullableColWidth := ansi.StringWidth("Nullable")
	for _, row := range rows {
		nullableColWidth = max(nullableColWidth, ansi.StringWidth(row[4]))
	}
	for _, row := range rows {
		contentWidth := ansi.StringWidth(row[4])
		if contentWidth < nullableColWidth {
			row[4] = strings.Repeat(" ", (nullableColWidth-contentWidth)/2) + row[4]
		}
	}
	m.structure.SetColumns(tableColumns([]string{"Column", "Indexes", "Type", "Attributes", "Nullable", "Default"}, rows))
	resizeResultsTable(&m.structure, m.tableViewportWidth, m.structure.Height()+1)
	m.structureRows = rows
	m.structureColumns = message.columns
	m.applyTableFilter(tabStructure)
	m.structureOffset = 0
	return m, nil
}

func (m Model) updateColumnAltered(message columnAlteredMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "altered column"))
	}
	if message.err != nil {
		m.columnForm.saving = false
		m.Status = safeText(fmt.Sprintf("updating column: %v", message.err))
		return m, nil
	}
	m.columnForm = columnForm{}
	m.Status = "column updated"
	return m, tea.Batch(m.loadTableInfo(), m.loadBrowse())
}

func (m Model) updateBrowseRowUpdated(message browseRowUpdatedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "updated 1 row"))
	}
	if message.err != nil {
		m.browseForm.saving = false
		m.Status = safeText(fmt.Sprintf("updating row: %v", message.err))
		return m, nil
	}
	m.browseForm = browseForm{}
	m.Status = "row updated"
	return m, m.loadBrowse()
}

func (m Model) deleteRow() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return deleteRowMsg{err: fmt.Errorf("connection is read-only")} }
	}
	if len(m.structureColumns) == 0 || m.browse.Cursor() < 0 || m.browse.Cursor() >= len(m.browseResult.Rows) {
		return func() tea.Msg { return deleteRowMsg{err: fmt.Errorf("no row selected")} }
	}
	columns := m.browseResult.Columns
	row := m.browseResult.Rows[m.browse.Cursor()]

	var primaryKeys []int
	for _, info := range m.structureColumns {
		if info.PrimaryKey > 0 {
			for ci, name := range columns {
				if strings.EqualFold(name, info.Name) {
					primaryKeys = append(primaryKeys, ci)
					break
				}
			}
		}
	}
	if len(primaryKeys) == 0 {
		return func() tea.Msg { return deleteRowMsg{err: fmt.Errorf("cannot delete: no primary key")} }
	}

	id := m.actionIdentifier
	statement := deleteRowStatement(m.SelectedTable, columns, row, primaryKeys, id)
	service, startedAt := m.Database, time.Now()
	return func() tea.Msg {
		result, err := service.Execute(m.appContext, statement)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("deleted %d rows, want 1", result.RowsAffected)
		}
		return deleteRowMsg{statement: statement, startedAt: startedAt, err: err}
	}
}

func (m Model) updateDeleteRowMsg(message deleteRowMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "deleted 1 row"))
	}
	if message.err != nil {
		m.Status = safeText(fmt.Sprintf("deleting row: %v", message.err))
		return m, nil
	}
	m.Status = "row deleted"
	return m, m.loadBrowse()
}

func (m Model) browseFilterStatement(filter sharedsql.BrowseFilter) string {
	column := m.actionIdentifier(filter.Column)
	switch filter.Operator {
	case sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull:
		return column + " " + string(filter.Operator)
	default:
		return column + " " + string(filter.Operator) + " " + quoteBrowseValue(filter.Value)
	}
}

func (m Model) updateBrowse(message browseTableMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.page != m.BrowsePage || message.tag != m.browsePageTag {
		return m, nil
	}
	m.browseLoading = false
	if message.err != nil {
		m.Status = safeText(fmt.Sprintf("loading browse: %v", message.err))
		return m, nil
	}
	m.setBrowse(message.result)
	duration := message.result.Duration
	if !message.startedAt.IsZero() {
		duration = time.Since(message.startedAt)
	}
	quotedTable := m.actionIdentifier(message.table)
	statement := fmt.Sprintf("SELECT * FROM %s", quotedTable)
	if len(m.browseSettings.filters) > 0 {
		filters := make([]string, len(m.browseSettings.filters))
		for index, filter := range m.browseSettings.filters {
			filters[index] = m.browseFilterStatement(filter)
		}
		statement += " WHERE " + strings.Join(filters, " AND ")
	}
	if len(m.browseSettings.sorts) > 0 {
		orders := make([]string, len(m.browseSettings.sorts))
		for index, sort := range m.browseSettings.sorts {
			orders[index] = m.actionIdentifier(sort.column)
			if sort.desc {
				orders[index] += " DESC"
			}
		}
		statement += " ORDER BY " + strings.Join(orders, ", ")
	}
	pageSize := m.browseSettings.pageSize()
	statement += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, message.page*pageSize)
	m.appendQueryLog(queryLogEntry{
		startedAt: message.startedAt,
		statement: statement,
		duration:  duration,
		message:   queryLogMessage(statement, message.result.RowsAffected, len(message.result.Rows)),
	})
	start, end := message.page*pageSize+1, message.page*pageSize+len(message.result.Rows)
	if len(message.result.Rows) == 0 {
		start = 0
	}
	m.browseStatus = fmt.Sprintf("%s | %s-%s", safeText(message.table), humanize.Comma(int64(start)), humanize.Comma(int64(end)))
	m.Status = ""
	return m, nil
}

func (m *Model) setBrowse(result sharedsql.Result) {
	cursor := m.browse.Cursor()
	selectedColumn := ""
	if m.browseColumn >= 0 && m.browseColumn < len(m.browseResult.Columns) {
		selectedColumn = m.browseResult.Columns[m.browseColumn]
	}
	m.browseResult = result
	m.browseNumericColumns = numericColumns(result.ColumnTypes)
	titles := make([]string, len(result.Columns))
	for index, column := range result.Columns {
		title := safeText(column)
		for _, sort := range m.browseSettings.sorts {
			if column != sort.column {
				continue
			}
			if sort.desc {
				title = "⌄ " + title
			} else {
				title = "⌃ " + title
			}
			break
		}
		titles[index] = title
	}
	rows := make([]table.Row, len(result.Rows))
	for rowIndex, row := range result.Rows {
		cells := make(table.Row, len(row))
		for cellIndex, cell := range row {
			if cell == nil {
				cells[cellIndex] = "NULL"
			} else {
				cells[cellIndex] = cellText(*cell)
			}
		}
		rows[rowIndex] = cells
	}
	m.browse.SetRows(nil)
	m.browse.SetColumns(tableColumns(titles, rows))
	resizeResultsTable(&m.browse, m.tableViewportWidth, max(m.workspaceHeight-5, 2))
	m.browse.SetRows(rows)
	if cursor >= 0 && len(rows) > 0 {
		m.browse.SetCursor(min(cursor, len(rows)-1))
	}
	m.browseColumn, m.browseOffset = 0, 0
	for index, column := range result.Columns {
		if column == selectedColumn {
			m.browseColumn = index
			revealTableColumn(m.browse, index, &m.browseOffset, m.tableViewportWidth)
			break
		}
	}
}

func numericColumns(types []string) []bool {
	columns := make([]bool, len(types))
	for index, typeName := range types {
		columns[index] = sharedsql.IsNumericColumnType(typeName)
	}
	return columns
}
