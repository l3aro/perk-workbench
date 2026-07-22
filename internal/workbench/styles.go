package workbench

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

const (
	colorCanvas        = "#10151f"
	colorPanel         = "#17202e"
	colorStripe        = "#1c2838"
	colorInk           = "#e6edf3"
	colorMuted         = "#8b9bb4"
	colorAccent        = "#55d6be"
	colorBorder        = "#324155"
	spaceCompact       = 1
	sqlEditorRows      = 4
	queryLogPaneHeight = 11
	iconPrimaryKey     = "\uf084" // nf-fa-key
	iconUnique         = "\uee40" // nf-fa-fingerprint
	iconRegular        = "\uf0cb" // nf-fa-list_ol
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCanvas)).
			Background(lipgloss.Color(colorAccent)).
			Bold(true).
			Padding(0, spaceCompact)
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted)).
			Padding(0, spaceCompact)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted)).
			Padding(0, spaceCompact)
	focusStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorAccent)).
			Foreground(lipgloss.Color(colorInk)).
			Padding(0, spaceCompact)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorBorder)).
			Foreground(lipgloss.Color(colorInk)).
			Padding(0, spaceCompact)
	primaryIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))
	uniqueIndexStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	regularIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
)

func indexIcons(indexes []sharedsql.IndexKind) string {
	// ponytail: terminals do not expose font support, so labels are the fallback.
	icons := make([]string, 0, len(indexes))
	for _, index := range indexes {
		switch index {
		case sharedsql.IndexPrimaryKey:
			icons = append(icons, primaryIndexStyle.Render(iconPrimaryKey+"PK"))
		case sharedsql.IndexUnique:
			icons = append(icons, uniqueIndexStyle.Render(iconUnique+"UQ"))
		case sharedsql.IndexRegular:
			icons = append(icons, regularIndexStyle.Render(iconRegular+"IX"))
		}
	}
	return strings.Join(icons, " ")
}

func newList(title string, filtering bool) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color(colorInk))
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(lipgloss.Color(colorMuted))
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color(colorAccent))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color(colorAccent))
	model := list.New([]list.Item{}, delegate, 0, 0)
	model.Title = title
	model.SetFilteringEnabled(filtering)
	model.SetShowPagination(false)
	model.SetShowHelp(false)
	model.KeyMap.Quit.SetEnabled(false)
	model.KeyMap.ForceQuit.SetEnabled(false)
	model.Styles.Title = headerStyle
	model.Styles.NoItems = statusStyle
	return model
}

type schemaItemDelegate struct{}

func (schemaItemDelegate) Height() int                         { return 1 }
func (schemaItemDelegate) Spacing() int                        { return 0 }
func (schemaItemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (schemaItemDelegate) Render(writer io.Writer, model list.Model, index int, item list.Item) {
	schema, ok := item.(schemaItem)
	if !ok {
		return
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk)).PaddingLeft(2)
	if index == model.Index() {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).PaddingLeft(2)
		fmt.Fprint(writer, style.Render("> "+schema.Title()))
		return
	}
	fmt.Fprint(writer, style.Render(schema.Title()))
}

func newSchemaList() list.Model {
	model := newList("Tables", true)
	model.SetDelegate(schemaItemDelegate{})
	return model
}

func newResultsTable() table.Model {
	return table.New(
		table.WithColumns([]table.Column{{Title: "Results", Width: 1}}),
		table.WithFocused(true),
		table.WithWidth(1),
		table.WithHeight(2),
		table.WithStyles(table.Styles{
			Header:   headerStyle,
			Cell:     lipgloss.NewStyle().Padding(0, spaceCompact),
			Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorStripe)),
		}),
	)
}

func resizeResultsTable(resultTable *table.Model, width, height int) {
	tableWidth := max(width, tableContentWidth(resultTable.Columns()))
	resultTable.SetWidth(tableWidth)
	resultTable.SetHeight(height)
	resultTable.SetStyles(table.Styles{
		Header: headerStyle,
		Cell:   lipgloss.NewStyle().Padding(0, spaceCompact),
		Selected: lipgloss.NewStyle().
			Width(tableWidth).
			Foreground(lipgloss.Color(colorAccent)).
			Background(lipgloss.Color(colorStripe)),
	})
}

func tableContentWidth(columns []table.Column) int {
	width := 0
	for _, column := range columns {
		width += column.Width + 2*spaceCompact
	}
	return width
}

func tableColumns(titles []string, rows []table.Row) []table.Column {
	if len(titles) == 0 {
		titles = []string{"Results"}
	}

	columns := make([]table.Column, len(titles))
	for index, title := range titles {
		columns[index] = table.Column{Title: title, Width: max(ansi.StringWidth(title), 1)}
	}
	for _, row := range rows {
		for index, value := range row {
			if index < len(columns) {
				columns[index].Width = max(columns[index].Width, ansi.StringWidth(value))
			}
		}
	}
	return columns
}

func tableViewportView(resultTable table.Model, offset, width int) string {
	return tableViewportViewWithAlignment(resultTable, nil, offset, width)
}

func tableViewportViewWithAlignment(resultTable table.Model, numericColumns []bool, offset, width int) string {
	offset = min(max(offset, 0), max(resultTable.Width()-width, 0))
	columns := resultTable.Columns()
	lines := []string{headerStyle.Padding(0, 0).Render(tableLine(columns, nil, numericColumns, offset, width))}
	rows, rowHeight := resultTable.Rows(), resultTable.Height()
	start := min(max(resultTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	for rowIndex := start; rowIndex < min(start+rowHeight, len(rows)); rowIndex++ {
		row := tableLine(columns, rows[rowIndex], numericColumns, offset, width)
		if rowIndex == resultTable.Cursor() {
			row = lipgloss.NewStyle().Width(width).Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorStripe)).Render(ansi.Strip(row))
		}
		lines = append(lines, row)
	}
	for range max(rowHeight-(len(lines)-1), 0) {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func tableLine(columns []table.Column, row table.Row, numericColumns []bool, offset, width int) string {
	cells := make([]string, len(columns))
	for index, column := range columns {
		value := column.Title
		if row != nil {
			value = ""
			if index < len(row) {
				value = row[index]
			}
		}
		style := lipgloss.NewStyle().Width(column.Width).MaxWidth(column.Width).Inline(true)
		if row != nil && index < len(numericColumns) && numericColumns[index] {
			style = style.Align(lipgloss.Right)
		}
		cell := style.Render(ansi.Truncate(value, column.Width, "…"))
		cells[index] = strings.Repeat(" ", spaceCompact) + cell + strings.Repeat(" ", spaceCompact)
	}
	return cropTableLine(strings.Join(cells, ""), offset, width)
}

func cropTableLine(line string, offset, width int) string {
	var visible strings.Builder
	position, end := 0, offset+width
	for len(line) > 0 && position < end {
		cluster, clusterWidth := ansi.FirstGraphemeCluster(line, ansi.WcWidth)
		nextPosition := position + clusterWidth
		if position >= offset && nextPosition <= end {
			visible.WriteString(cluster)
		}
		position = nextPosition
		line = line[len(cluster):]
	}
	return visible.String() + strings.Repeat(" ", max(width-ansi.StringWidth(visible.String()), 0))
}

func tableOffset(resultTable table.Model, offset, viewportWidth int) int {
	return min(max(offset, 0), max(resultTable.Width()-viewportWidth, 0))
}

func paneStyle(focused bool) lipgloss.Style {
	if focused {
		return focusStyle
	}
	return panelStyle
}

func compactPane(content string, width, height int) string {
	return paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height).Render(content)
}

func (m *Model) layout(width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)
	contentHeight := max(m.height-4, 0)
	m.compact = m.fullscreen || m.width < compactWidth || m.height < 24
	m.schemaWidth, m.editorWidth = m.width, m.width
	m.workspaceHeight, m.queryLogHeight = contentHeight, 0
	if m.compact {
		m.queryLogHeight = contentHeight
	}
	if !m.compact {
		m.schemaWidth = 30
		m.editorWidth = max(m.width-32, 0)
		m.queryLogHeight = min(queryLogPaneHeight, contentHeight)
		m.workspaceHeight = contentHeight - m.queryLogHeight
	}
	m.editorHeight = min(m.workspaceHeight, sqlEditorRows+2)
	m.resultsHeight = max(m.workspaceHeight-m.editorHeight, 0)
	m.schema.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.picker.SetSize(max(m.width-2, 0), max(contentHeight-2, 0))
	connectionWidth := m.width
	if !m.compact {
		connectionWidth = m.editorWidth
	}
	m.connection.name.SetWidth(max(connectionWidth-16, 1))
	m.connection.target.SetWidth(max(connectionWidth-16, 1))
	m.connection.host.SetWidth(max(connectionWidth-16, 1))
	m.connection.port.SetWidth(max(connectionWidth-16, 1))
	m.connection.user.SetWidth(max(connectionWidth-16, 1))
	m.connection.pass.SetWidth(max(connectionWidth-16, 1))
	m.recent.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.editor.textarea.SetWidth(max(m.editorWidth-4, 1))
	m.editor.textarea.SetHeight(max(m.editorHeight-2, 1))
	m.tableViewportWidth = max(m.editorWidth-4, 1)
	if m.compact {
		m.tableViewportWidth = max(m.editorWidth-6, 1)
	} else {
		m.tableViewportWidth = max(m.editorWidth-8, 1)
	}
	m.columnForm.setWidth(m.tableViewportWidth)
	m.browseForm.setWidth(m.tableViewportWidth)
	m.indexForm.setWidth(m.tableViewportWidth)
	m.foreignKeyForm.setWidth(m.tableViewportWidth)
	for _, resultTable := range []*table.Model{&m.results, &m.structure, &m.browse, &m.indexes, &m.foreignKeys, &m.queryLog} {
		columns := resultTable.Columns()
		titles := make([]string, len(columns))
		for index, column := range columns {
			titles[index] = column.Title
		}
		resultTable.SetColumns(tableColumns(titles, resultTable.Rows()))
	}
	resizeResultsTable(&m.results, m.tableViewportWidth, max(m.resultsHeight-4, 2))
	resizeResultsTable(&m.queryLog, m.tableViewportWidth, max(m.queryLogHeight-5, 2))
	resizeResultsTable(&m.structure, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	resizeResultsTable(&m.browse, m.tableViewportWidth, max(m.workspaceHeight-5, 2))
	resizeResultsTable(&m.indexes, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	resizeResultsTable(&m.foreignKeys, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	m.structureOffset = tableOffset(m.structure, m.structureOffset, m.tableViewportWidth)
	m.browseOffset = tableOffset(m.browse, m.browseOffset, m.tableViewportWidth)
	m.resultsOffset = tableOffset(m.results, m.resultsOffset, m.tableViewportWidth)
	m.indexesOffset = tableOffset(m.indexes, m.indexesOffset, m.tableViewportWidth)
	m.foreignKeysOffset = tableOffset(m.foreignKeys, m.foreignKeysOffset, m.tableViewportWidth)
	m.queryLogOffset = tableOffset(m.queryLog, m.queryLogOffset, m.tableViewportWidth)
}

func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	if m.height < 4 || m.width < 1 {
		view.SetContent(headerStyle.Render("BUBBLE WORKBENCH"))
		return view
	}
	content := m.contentView()
	view.SetContent(lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render("BUBBLE WORKBENCH"), content, footerStyle.Render(m.footer())))
	if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL {
		if cursor := m.editor.textarea.Cursor(); cursor != nil {
			cursor.Position.X += 2
			cursor.Position.Y += 4
			if !m.compact {
				cursor.Position.X += m.schemaWidth - 2
			}
			view.Cursor = cursor
		}
	}
	return view
}

func (m Model) contentView() string {
	switch m.State {
	case stateConnection:
		if m.compact {
			content := m.connectionPaneView(max(m.height-6, 0))
			if m.connection.focus == connectionFocusRecent {
				content = m.recent.View()
			}
			return compactPane(content, max(m.width-2, 0), max(m.height-4, 0))
		}
		left := paneStyle(m.connection.focus == connectionFocusRecent).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.recent.View())
		right := paneStyle(m.connection.focus != connectionFocusRecent).Width(max(m.editorWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.connectionPaneView(max(m.height-6, 0)))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	case statePicking:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(m.picker.View())
	case stateOpening:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(statusStyle.Render("opening database"))
	case stateFailure:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(statusStyle.Render(m.Status + "\npress enter to return to the picker"))
	}
	if m.compact {
		width, height := max(1, m.width-2), max(1, m.height-4)
		switch m.Focus {
		case focusSchema:
			return compactPane(m.schema.View(), width, height)
		case focusWorkspace:
			return compactPane(m.workspaceView(), width, height)
		case focusQueryLog:
			return compactPane(m.queryLogContentView(), width, height)
		}
	}
	left := paneStyle(m.Focus == focusSchema).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-2, 0)).Render(m.schema.View())
	right := lipgloss.JoinVertical(lipgloss.Left, m.rightView(), m.queryLogPaneView())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m Model) rightView() string {
	return paneStyle(m.Focus == focusWorkspace).Width(max(m.editorWidth-2, 0)).Height(max(m.workspaceHeight, 0)).Render(m.workspaceView())
}

func (m Model) queryLogPaneView() string {
	return paneStyle(m.Focus == focusQueryLog).Width(max(m.editorWidth-2, 0)).Height(max(m.queryLogHeight, 0)).Render(m.queryLogContentView())
}

func (m Model) queryLogContentView() string {
	content := tableViewportView(m.queryLog, m.queryLogOffset, m.tableViewportWidth)
	if summary := m.queryLogSummary(); summary != "" {
		padding := max(m.queryLogHeight-1-lipgloss.Height(content)-1, 0)
		content += strings.Repeat("\n", padding+1) + paneStatus("", statusStyle.Render(summary), m.tableViewportWidth)
	}
	return content
}

func (m Model) workspaceView() string {
	tabs := []string{"Structure", "Browse", "SQL", "Indexes", "Foreign Keys"}
	for index := range tabs {
		if workspaceTab(index) == m.Tab {
			tabs[index] = headerStyle.Render(tabs[index])
		} else {
			tabs[index] = statusStyle.Render(tabs[index])
		}
	}
	var content string
	switch m.Tab {
	case tabStructure:
		content = m.structureView()
	case tabBrowse:
		content = m.browseView()
	case tabSQL:
		content = m.sqlPaneView()
	case tabIndexes:
		content = m.indexesView()
	case tabForeignKeys:
		content = m.foreignKeysView()
	}
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content)
}

func (m Model) sqlPaneView() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.editor.textarea.View(),
		tableViewportViewWithAlignment(m.results, m.resultsNumericColumns, m.resultsOffset, m.tableViewportWidth),
	)
	mode := "NORMAL"
	if m.editor.insert {
		mode = "INSERT"
	}
	return content + "\n" + paneStatus(headerStyle.Render(mode), m.resultsStatus, m.tableViewportWidth)
}

func (m Model) structureView() string {
	if m.columnForm.active() {
		return m.columnForm.View()
	}
	return tableViewportView(m.structure, m.structureOffset, m.tableViewportWidth)
}

func (m Model) browseView() string {
	if m.browseForm.active() {
		return m.browseForm.View()
	}
	return tableViewportViewWithAlignment(m.browse, m.browseNumericColumns, m.browseOffset, m.tableViewportWidth) + "\n" + paneStatus("", m.browseStatus, m.tableViewportWidth)
}

func paneStatus(left, right string, width int) string {
	return left + lipgloss.NewStyle().Width(max(width-lipgloss.Width(left), 0)).Align(lipgloss.Right).Render(right)
}

func (m Model) indexesView() string {
	if m.indexForm.active() {
		return m.indexForm.View()
	}
	return tableViewportView(m.indexes, m.indexesOffset, m.tableViewportWidth)
}

func (m Model) foreignKeysView() string {
	if m.foreignKeyForm.active() {
		return m.foreignKeyForm.View()
	}
	return tableViewportView(m.foreignKeys, m.foreignKeysOffset, m.tableViewportWidth)
}

func (m Model) footer() string {
	if m.State == stateConnection {
		return safeText(m.Status + " | 1 recent | 2 form | tab controls | a add | e edit | d delete | / filter | q quit")
	}
	if m.State == stateReady {
		parts := []string{}
		if m.Status != "" {
			parts = append(parts, m.Status)
		}
		if m.databaseInfo.Product != "" && m.databaseInfo.Version != "" {
			parts = append(parts, m.databaseInfo.Product+" "+m.databaseInfo.Version)
		}
		parts = append(parts, "1 tables", "2 tabs", "3 history", "f fullscreen", "tab switch view", "q quit")
		return safeText(strings.Join(parts, " | "))
	}
	return safeText(m.Status + " | q quit")
}

func readDirectory(dir string) tea.Cmd {
	return func() tea.Msg {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return directoryReadMsg{err: err}
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return directoryReadMsg{dir: absolute, err: err}
		}
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return directoryReadMsg{dir: resolved, err: err}
		}
		items := []pickerItem{{raw: ":memory:", title: "In-memory database", description: "temporary SQLite database"}}
		if parent := filepath.Dir(resolved); parent != resolved {
			items = append(items, pickerItem{raw: parent, title: "..", description: "parent directory"})
		}
		for _, entry := range entries {
			name := entry.Name()
			target, err := filepath.EvalSymlinks(filepath.Join(resolved, name))
			if err != nil {
				continue
			}
			info, err := os.Stat(target)
			if err != nil {
				continue
			}
			kind := "directory"
			if !info.IsDir() {
				if !info.Mode().IsRegular() || !databaseSuffix(name) {
					continue
				}
				kind = "database"
			}
			items = append(items, pickerItem{raw: target, title: safeText(name), description: kind})
		}
		return directoryReadMsg{dir: resolved, items: items}
	}
}

func selectPickerItem(raw string) tea.Cmd {
	return func() tea.Msg {
		if raw == ":memory:" {
			return pickerSelectionMsg{target: raw}
		}
		resolved, err := filepath.EvalSymlinks(raw)
		if err != nil {
			return pickerSelectionMsg{err: err}
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return pickerSelectionMsg{err: err}
		}
		return pickerSelectionMsg{target: resolved, dir: info.IsDir()}
	}
}

func resolveTarget(target string) (string, error) {
	if target == ":memory:" {
		return target, nil
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("database target is not a regular file")
	}
	return resolved, nil
}

func databaseSuffix(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite") || strings.HasSuffix(name, ".sqlite3")
}

func safeText(input string) string { return sharedsql.SanitizeDisplay(input) }
