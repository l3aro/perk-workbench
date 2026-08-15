package app

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
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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
	kind      string
}

type columnDeletedMsg struct {
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

type insertRowMsg struct {
	statement string
	startedAt time.Time
	err       error
}

func (m *Model) setResults(result sharedsql.Result) {
	m.queryLog.resultsNumericColumns = numericColumns(result.ColumnTypes)
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
	m.queryLog.resultsRaw = result.UntruncatedRows
	m.queryLog.results.SetRows(nil)
	m.queryLog.results.SetColumns(tableColumns(titles, rows))
	resizeResultsTable(&m.queryLog.results, m.layout.tableViewportWidth, max(m.layout.resultsHeight-4, 2))
	m.queryLog.results.SetRows(rows)
	m.layout.resultsColumn, m.layout.resultsOffset = 0, 0
	m.queryLog.results.Focus()
	m.overlay.formMode.Mode = formModeNormal
	m.queryLog.editor.text.Blur()
	rowLabel := "rows"
	if len(rows) == 1 {
		rowLabel = "row"
	}
	affectedLabel := "rows"
	if result.RowsAffected == 1 {
		affectedLabel = "row"
	}
	m.queryLog.resultsStatus = fmt.Sprintf("%d %s | %d %s affected | %s", len(rows), rowLabel, result.RowsAffected, affectedLabel, result.Duration)
	m.queryLog.resultsStatus += colsHint(m.queryLog.results.Columns(), m.layout.tableViewportWidth)
	if result.Truncated {
		m.queryLog.resultsStatus += " | truncated"
	}
	m.setStatus("")
}

func (m Model) loadTableInfo() tea.Cmd {
	tableName, service := m.SelectedTable, m.Database
	return func() tea.Msg {
		columns, err := service.TableInfo(m.appContext, tableName)
		return tableInfoMsg{table: tableName, columns: columns, err: err}
	}
}

func (m Model) loadBrowse() tea.Cmd {
	tableName, page, tag, service := m.SelectedTable, m.BrowsePage, m.browse.component.PageTag, m.Database
	settings := m.browse.component.Settings
	columns := make([]string, len(m.schema.component.Structure.Columns))
	for index, column := range m.schema.component.Structure.Columns {
		columns[index] = column.Name
	}
	startedAt := time.Now()
	return func() tea.Msg {
		sorts := make([]sharedsql.BrowseSort, len(settings.Sorts))
		for index, sort := range settings.Sorts {
			sorts[index] = sharedsql.BrowseSort{Column: sort.Column, Descending: sort.Desc}
		}
		filters := slices.Clone(settings.Filters)
		result, err := service.BrowseTable(m.appContext, tableName, sharedsql.BrowseOptions{
			Columns: columns, Filters: filters, Sorts: sorts,
			Offset: page * settings.PageSize(m.browse.component.PageSize), Limit: settings.PageSize(m.browse.component.PageSize),
		})
		return browseTableMsg{table: tableName, page: page, tag: tag, startedAt: startedAt, result: result, err: err}
	}
}

// openColumnForm opens the edit form for the selected structure column;
// without vim mode the form enters insert mode on the focused field.
func (m *Model) openColumnForm() tea.Cmd {
	component, cmd := m.schema.component.OpenColumnForm(m.schemaSnapshot(), m.workspaceLayout(), m.keybindings)
	m.schema.component = component
	if !component.Structure.ColumnForm.Active() {
		m.setStatus("select a column")
		return nil
	}
	m.overlay.formMode.ButtonsFocused = false
	return m.openForm(cmd, component.Structure.ColumnForm.Focus)
}

// openNewColumnForm opens the add-column form; without vim mode the form
// enters insert mode on the focused field.
func (m *Model) openNewColumnForm() tea.Cmd {
	component, cmd := m.schema.component.OpenNewColumnForm(m.schemaSnapshot(), m.workspaceLayout(), m.keybindings)
	m.schema.component = component
	m.overlay.formMode.ButtonsFocused = false
	return m.openForm(cmd, component.Structure.ColumnForm.Focus)
}

func (m Model) alterColumn() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return columnAlteredMsg{kind: "altered", err: fmt.Errorf("connection is read-only")} }
	}
	table, service := m.SelectedTable, m.Database
	change, err := m.schema.component.Structure.ColumnForm.Change()
	if err != nil {
		return func() tea.Msg { return columnAlteredMsg{kind: "altered", err: err} }
	}
	statement, startedAt := m.columnChangeStatement(table, change), time.Now()
	return func() tea.Msg {
		return columnAlteredMsg{statement: statement, startedAt: startedAt, err: service.AlterColumn(m.appContext, table, change), kind: "altered"}
	}
}

func (m Model) addColumn() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return columnAlteredMsg{kind: "added", err: fmt.Errorf("connection is read-only")} }
	}
	table, service := m.SelectedTable, m.Database
	def, err := m.schema.component.Structure.ColumnForm.ColumnDef()
	if err != nil {
		return func() tea.Msg { return columnAlteredMsg{kind: "added", err: err} }
	}
	statement, startedAt := m.columnAddStatement(table, def), time.Now()
	return func() tea.Msg {
		return columnAlteredMsg{statement: statement, startedAt: startedAt, err: service.AddColumn(m.appContext, table, def), kind: "added"}
	}
}

func (m Model) deleteColumn() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return columnDeletedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	name := m.overlay.deletePendingName
	if name == "" {
		name = m.schema.component.Structure.ColumnForm.PreviousName
	}
	table, service := m.SelectedTable, m.Database
	statement, startedAt := m.dropColumnStatement(table, name), time.Now()
	return func() tea.Msg {
		return columnDeletedMsg{statement: statement, startedAt: startedAt, err: service.DropColumn(m.appContext, table, name)}
	}
}

// writeLogStatement prefers the backend's native replayable statement
// returned by a row/document write over the generic UI preview: the
// preview is display-only (never executable for non-SQL backends such as
// Redis), while external plugins may return the exact command they ran.
// Only a nonblank statement wins — whitespace-only output must not
// suppress the preview — and the original text is kept verbatim. The
// preview stays the fallback for compiled-in drivers and older plugins.
func writeLogStatement(preview string, result sharedsql.Result) string {
	if strings.TrimSpace(result.Statement) != "" {
		return result.Statement
	}
	return preview
}

func (m Model) updateBrowseRow() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return browseRowUpdatedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	writer := m.rowWriter()
	if writer == nil {
		return func() tea.Msg { return browseRowUpdatedMsg{err: m.rowWriteUnsupportedError()} }
	}
	key, err := m.browse.component.Form.KeyValues()
	if err != nil {
		return func() tea.Msg { return browseRowUpdatedMsg{err: err} }
	}
	values := m.browse.component.Form.RowValues()
	if len(values) == 0 {
		return func() tea.Msg { return browseRowUpdatedMsg{} }
	}
	preview := m.browse.component.Form.Preview()
	table, startedAt := m.SelectedTable, time.Now()
	return func() tea.Msg {
		result, err := writer.UpdateRow(m.appContext, table, key, values)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return browseRowUpdatedMsg{statement: writeLogStatement(preview, result), startedAt: startedAt, err: err}
	}
}

func (m Model) updateTableInfo(message tableInfoMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.err != nil {
		if message.err != nil {
			log.Error("loading structure", message.err)
			m.setStatus(safeText(fmt.Sprintf("loading structure: %v", message.err)))
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
	m.schema.component.Structure.Table.SetColumns(tableColumns([]string{"Column", "Indexes", "Type", "Attributes", "Nullable", "Default"}, rows))
	resizeResultsTable(&m.schema.component.Structure.Table, m.layout.tableViewportWidth, m.schema.component.Structure.Table.Height()+1)
	m.schema.component.Structure.Rows = rows
	m.schema.component.Structure.Columns = message.columns
	m.browse.component.Structure = message.columns
	m.schema.component.ApplyTableFilter(tabStructure)
	m.layout.structureOffset = 0
	return m, nil
}

func (m Model) updateColumnAltered(message columnAlteredMsg) (tea.Model, tea.Cmd) {
	action := "altered column"
	status := "column updated"
	if message.kind == "added" {
		action = "added column"
		status = "column added"
	}
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, action))
	}
	if message.err != nil {
		actionMsg := "updating column"
		if message.kind == "added" {
			actionMsg = "adding column"
		}
		m.schema.component.Structure.ColumnForm.Saving = false
		m.setStatus(safeText(fmt.Sprintf(actionMsg+": %v", message.err)))
		return m, nil
	}
	m.schema.component.Structure.ColumnForm = schema.ColumnForm{}
	m.setStatus(status)
	return m, tea.Batch(m.loadTableInfo(), m.loadBrowse(), m.loadSchemaForeignKeysAll(), m.loadSchemaIndexesAll())
}

func (m Model) updateColumnDeleted(message columnDeletedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "dropped column"))
	}
	if message.err != nil {
		m.schema.component.Structure.ColumnForm.Saving = false
		m.setStatus(safeText(fmt.Sprintf("deleting column: %v", message.err)))
		return m, nil
	}
	m.schema.component.Structure.ColumnForm = schema.ColumnForm{}
	m.setStatus("column deleted")
	return m, tea.Batch(m.loadTableInfo(), m.loadBrowse(), m.loadSchemaForeignKeysAll(), m.loadSchemaIndexesAll())
}

func (m Model) updateBrowseRowUpdated(message browseRowUpdatedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "updated 1 row"))
	}
	if message.err != nil {
		m.browse.component.Form.Saving = false
		m.setStatus(safeText(fmt.Sprintf("updating row: %v", message.err)))
		return m, nil
	}
	m.browse.component.Form = browse.Form{}
	m.setStatus("row updated")
	return m, m.loadBrowse()
}

// insertBrowseRow executes the insert form's RowWriter insert. The form
// stays open on failure so the input survives a rejected insert
// (constraint, type mismatch); success closes it and reloads the browse
// page.
func (m Model) insertBrowseRow() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return insertRowMsg{err: fmt.Errorf("connection is read-only")} }
	}
	writer := m.rowWriter()
	if writer == nil {
		return func() tea.Msg { return insertRowMsg{err: m.rowWriteUnsupportedError()} }
	}
	values := m.browse.component.Form.RowValues()
	preview := m.browse.component.Form.Preview()
	table, startedAt := m.SelectedTable, time.Now()
	return func() tea.Msg {
		result, err := writer.InsertRow(m.appContext, table, values)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("inserted %d rows, want 1", result.RowsAffected)
		}
		return insertRowMsg{statement: writeLogStatement(preview, result), startedAt: startedAt, err: err}
	}
}

func (m Model) updateInsertRowMsg(message insertRowMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "inserted 1 row"))
	}
	if message.err != nil {
		m.browse.component.Form.Saving = false
		m.setStatus(safeText(fmt.Sprintf("inserting row: %v", message.err)))
		return m, nil
	}
	m.browse.component.Form = browse.Form{}
	m.setStatus("row inserted")
	return m, m.loadBrowse()
}

func (m Model) deleteRow() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return deleteRowMsg{err: fmt.Errorf("connection is read-only")} }
	}
	if len(m.schema.component.Structure.Columns) == 0 || m.browse.component.Table.Cursor() < 0 || m.browse.component.Table.Cursor() >= len(m.browse.component.Result.Rows) {
		return func() tea.Msg { return deleteRowMsg{err: fmt.Errorf("no row selected")} }
	}
	columns := m.browse.component.Result.Columns
	row := m.browse.component.Result.Rows[m.browse.component.Table.Cursor()]
	table, startedAt := m.SelectedTable, time.Now()
	capabilities := m.writeCapabilities()
	if capabilities.RowWriter {
		key, err := rowKeyValues(columns, row, browsePrimaryKeys(m.schema.component.Structure.Columns, columns))
		if err != nil {
			return func() tea.Msg { return deleteRowMsg{err: err} }
		}
		preview := browseDeletePreview(table, columns, row, browsePrimaryKeys(m.schema.component.Structure.Columns, columns))
		writer := m.rowWriter()
		return func() tea.Msg {
			result, err := writer.DeleteRow(m.appContext, table, key)
			if err == nil && result.RowsAffected != 1 {
				err = fmt.Errorf("deleted %d rows, want 1", result.RowsAffected)
			}
			return deleteRowMsg{statement: writeLogStatement(preview, result), startedAt: startedAt, err: err}
		}
	}
	if identity := m.browseDocumentIdentity(); identity != nil {
		writer := m.documentWriter()
		if writer == nil {
			return func() tea.Msg { return deleteRowMsg{err: m.rowWriteUnsupportedError()} }
		}
		preview := fmt.Sprintf("Table: %s\nKey:\n  _id = %s", table, string(identity.Data))
		return func() tea.Msg {
			result, err := writer.DeleteDocument(m.appContext, table, *identity)
			if err == nil && result.RowsAffected != 1 {
				err = fmt.Errorf("deleted %d rows, want 1", result.RowsAffected)
			}
			return deleteRowMsg{statement: writeLogStatement(preview, result), startedAt: startedAt, err: err}
		}
	}
	return func() tea.Msg { return deleteRowMsg{err: m.rowWriteUnsupportedError()} }
}

// browsePrimaryKeys maps primary-key structure columns to their browse
// column indices.
func browsePrimaryKeys(structure []sharedsql.ColumnInfo, columns []string) []int {
	var primaryKeys []int
	for _, info := range structure {
		if info.PrimaryKey > 0 {
			for ci, name := range columns {
				if strings.EqualFold(name, info.Name) {
					primaryKeys = append(primaryKeys, ci)
					break
				}
			}
		}
	}
	return primaryKeys
}

// rowKeyValues converts browse row values at the primary-key indices to
// the RowValue key list; NULL cells stay NULL.
func rowKeyValues(columns []string, row []*string, primaryKeys []int) ([]sharedsql.RowValue, error) {
	if len(primaryKeys) == 0 {
		return nil, fmt.Errorf("cannot delete: no primary key")
	}
	key := make([]sharedsql.RowValue, 0, len(primaryKeys))
	for _, pk := range primaryKeys {
		value := sharedsql.Value{Kind: sharedsql.ValueString}
		if row[pk] == nil {
			value.Kind = sharedsql.ValueNull
		} else {
			value.String = *row[pk]
		}
		key = append(key, sharedsql.RowValue{Name: columns[pk], Value: value})
	}
	return key, nil
}

// browseDeletePreview renders the structured delete preview for the
// query-log entry: Table and the primary-key Key.
func browseDeletePreview(table string, columns []string, row []*string, primaryKeys []int) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", table)
	builder.WriteString("\nKey:")
	for _, pk := range primaryKeys {
		value := sharedsql.Value{Kind: sharedsql.ValueString}
		if row[pk] == nil {
			value.Kind = sharedsql.ValueNull
		} else {
			value.String = *row[pk]
		}
		fmt.Fprintf(&builder, "\n  %s = %s", columns[pk], browse.RowValuePreview(value))
	}
	return builder.String()
}

func (m Model) updateDeleteRowMsg(message deleteRowMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, "deleted 1 row"))
	}
	if message.err != nil {
		m.setStatus(safeText(fmt.Sprintf("deleting row: %v", message.err)))
		return m, nil
	}
	m.setStatus("row deleted")
	return m, m.loadBrowse()
}

func (m Model) browseFilterStatement(filter sharedsql.BrowseFilter) string {
	column := m.actionIdentifier(filter.Column)
	switch filter.Operator {
	case sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull:
		return column + " " + string(filter.Operator)
	default:
		return column + " " + string(filter.Operator) + " " + quoteLogString(filter.Value)
	}
}

// quoteLogString quotes a value for the display-only browse log statement.
// The browse query itself runs through BrowseTable with structured
// filters, so this text never executes.
func quoteLogString(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func (m Model) updateBrowse(message browseTableMsg) (tea.Model, tea.Cmd) {
	if message.table != m.SelectedTable || message.page != m.BrowsePage || message.tag != m.browse.component.PageTag {
		return m, nil
	}
	quotedTable := m.actionIdentifier(message.table)
	statement := fmt.Sprintf("SELECT * FROM %s", quotedTable)
	if len(m.browse.component.Settings.Filters) > 0 {
		filters := make([]string, len(m.browse.component.Settings.Filters))
		for index, filter := range m.browse.component.Settings.Filters {
			filters[index] = m.browseFilterStatement(filter)
		}
		statement += " WHERE " + strings.Join(filters, " AND ")
	}
	if len(m.browse.component.Settings.Sorts) > 0 {
		orders := make([]string, len(m.browse.component.Settings.Sorts))
		for index, sort := range m.browse.component.Settings.Sorts {
			orders[index] = m.actionIdentifier(sort.Column)
			if sort.Desc {
				orders[index] += " DESC"
			}
		}
		statement += " ORDER BY " + strings.Join(orders, ", ")
	}
	pageSize := m.browse.component.Settings.PageSize(m.browse.component.PageSize)
	statement += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, message.page*pageSize)

	m.browse.component.Loading = false
	m.browse.component.Page = message.page
	if message.err != nil {
		duration := time.Duration(0)
		if !message.startedAt.IsZero() {
			duration = time.Since(message.startedAt)
		}
		m.appendQueryLog(queryLogEntry{
			StartedAt: message.startedAt,
			Statement: statement,
			Duration:  duration,
			Message:   message.err.Error(),
			Status:    "failed",
		})
		m.setStatus(safeText(fmt.Sprintf("loading browse: %v", message.err)))
		return m, nil
	}
	// Set the status before setBrowse so its split-aware table sizing
	// (browseStatusSplit) sees the summary it will render. The position
	// mirrors the cursor setBrowse will leave, so it never exceeds the
	// loaded rows.
	rows := len(message.result.Rows)
	start, end := message.page*pageSize+1, message.page*pageSize+rows
	if rows == 0 {
		start = 0
	}
	m.browse.component.Status = browseStatusText(message.table, start, end, browsePosition(m.browse.component.Table.Cursor(), rows), rows, message.page)
	m.setBrowse(message.result)
	duration := message.result.Duration
	if !message.startedAt.IsZero() {
		duration = time.Since(message.startedAt)
	}
	m.appendQueryLog(queryLogEntry{
		StartedAt: message.startedAt,
		Statement: statement,
		Duration:  duration,
		Message:   queryLogMessage(statement, message.result.RowsAffected, len(message.result.Rows)),
	})
	m.setStatus("")
	return m, nil
}

// browseStatusText formats the browse page summary: the table name, the
// record range shown on the page, the selected row's position within the
// page, and the page number.
func browseStatusText(table string, start, end, position, rows, page int) string {
	return fmt.Sprintf("%s | %s-%s | %s/%s | page %s",
		safeText(table),
		humanize.Comma(int64(start)),
		humanize.Comma(int64(end)),
		humanize.Comma(int64(position)),
		humanize.Comma(int64(rows)),
		humanize.Comma(int64(page+1)))
}

// browsePosition returns the 1-based row position of the browse cursor
// within the loaded page, or 0 when the page has no rows. The cursor is
// clamped so a fresh table (cursor -1) reports position 1 on a nonempty
// page, matching the row setBrowse will select.
func browsePosition(cursor, rows int) int {
	if rows <= 0 {
		return 0
	}
	return clamp(cursor, 0, rows-1) + 1
}

// refreshBrowseStatus recomputes the browse status from the current
// cursor, page, and loaded rows. Every browse cursor move must call it so
// the "index/total | page N" position stays accurate.
func (m *Model) refreshBrowseStatus() {
	rows := len(m.browse.component.Result.Rows)
	pageSize := m.browse.component.Settings.PageSize(m.browse.component.PageSize)
	start, end := m.BrowsePage*pageSize+1, m.BrowsePage*pageSize+rows
	if rows == 0 {
		start = 0
	}
	status := browseStatusText(m.SelectedTable, start, end, browsePosition(m.browse.component.Table.Cursor(), rows), rows, m.BrowsePage)
	if status == m.browse.component.Status {
		return
	}
	m.browse.component.Status = status
	// The split decision depends on the status width; if it flipped, keep
	// the table height consistent with the rendered footer.
	footerRows := m.browseFooterRows()
	if m.browse.component.Table.Height() != max(m.layout.workspaceHeight-footerRows, 2) {
		resizeResultsTable(&m.browse.component.Table, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-footerRows, 2))
	}
}

func (m *Model) setBrowse(result sharedsql.Result) {
	cursor := m.browse.component.Table.Cursor()
	selectedColumn := ""
	if m.browse.component.SelectedColumn >= 0 && m.browse.component.SelectedColumn < len(m.browse.component.Result.Columns) {
		selectedColumn = m.browse.component.Result.Columns[m.browse.component.SelectedColumn]
	}
	m.browse.component.Result = result
	m.browse.component.NumericColumns = numericColumns(result.ColumnTypes)
	titles := make([]string, len(result.Columns))
	for index, column := range result.Columns {
		title := safeText(column)
		for _, sort := range m.browse.component.Settings.Sorts {
			if column != sort.Column {
				continue
			}
			if sort.Desc {
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
	m.browse.component.Table.SetRows(nil)
	m.browse.component.Table.SetColumns(tableColumns(titles, rows))
	// Must mirror layout()'s sizing: the browse table yields the footer
	// rows below its data rows (browseFooterRows).
	resizeResultsTable(&m.browse.component.Table, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-m.browseFooterRows(), 2))
	m.browse.component.Table.SetRows(rows)
	if cursor >= 0 && len(rows) > 0 {
		m.browse.component.Table.SetCursor(min(cursor, len(rows)-1))
	}
	m.browse.component.SelectedColumn, m.browse.component.Offset = 0, 0
	for index, column := range result.Columns {
		if column == selectedColumn {
			m.browse.component.SelectedColumn = index
			revealTableColumn(m.browse.component.Table, index, &m.browse.component.Offset, m.layout.tableViewportWidth)
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
