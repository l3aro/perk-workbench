package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dustin/go-humanize"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type tableInfoMsg struct {
	table   string
	columns []sharedsql.ColumnInfo
	err     error
}

type browseTableMsg struct {
	table     string
	page      int
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
				cells[cellIndex] = safeText(*cell)
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
	tableName, page, service := m.SelectedTable, m.BrowsePage, m.Database
	startedAt := time.Now()
	return func() tea.Msg {
		result, err := service.BrowseTable(m.appContext, tableName, page*browsePageSize, browsePageSize)
		return browseTableMsg{table: tableName, page: page, startedAt: startedAt, result: result, err: err}
	}
}

func (m Model) alterColumn() tea.Cmd {
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
		nullable := booleanValue(column.Nullable)
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
	m.structure.SetRows(rows)
	m.structureColumns = message.columns
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

func (m Model) updateBrowse(message browseTableMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.page != m.BrowsePage {
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
	statement := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", quotedTable, browsePageSize, message.page*browsePageSize)
	m.appendQueryLog(queryLogEntry{
		startedAt: message.startedAt,
		statement: statement,
		duration:  duration,
		message:   queryLogMessage(statement, message.result.RowsAffected, len(message.result.Rows)),
	})
	start, end := message.page*browsePageSize+1, message.page*browsePageSize+len(message.result.Rows)
	if len(message.result.Rows) == 0 {
		start = 0
	}
	m.browseStatus = fmt.Sprintf("%s | %s-%s", safeText(message.table), humanize.Comma(int64(start)), humanize.Comma(int64(end)))
	m.Status = ""
	return m, nil
}

func (m *Model) setBrowse(result sharedsql.Result) {
	cursor := m.browse.Cursor()
	m.browseResult = result
	m.browseNumericColumns = numericColumns(result.ColumnTypes)
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
				cells[cellIndex] = safeText(*cell)
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
}

func numericColumns(types []string) []bool {
	columns := make([]bool, len(types))
	for index, typeName := range types {
		columns[index] = sharedsql.IsNumericColumnType(typeName)
	}
	return columns
}
