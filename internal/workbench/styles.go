package workbench

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
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
	colorModeNormal    = "#58a6ff"
	colorModeInsert    = "#3fb950"
	spaceCompact       = 1
	sqlEditorRows      = 4
	queryLogPaneHeight = 11
	iconPrimaryKey     = "\uf084" // nf-fa-key
	iconUnique         = "\uee40" // nf-fa-fingerprint
	iconRegular        = "\uf0cb" // nf-fa-list_ol
	iconSuccess        = "\uf00c" // nf-fa-check
	iconFailed         = "\uf00d" // nf-fa-times
	iconCanceled       = "\uf05e" // nf-fa-ban
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
	connectionActionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorInk)).
				Background(lipgloss.Color(colorStripe)).
				Padding(0, spaceCompact)
	connectionActionSelectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(colorCanvas)).
					Background(lipgloss.Color(colorAccent)).
					Bold(true).
					Padding(0, spaceCompact)
	primaryIndexStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))
	uniqueIndexStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	regularIndexStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	statusSuccessStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	statusFailedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	statusCanceledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	modeNormalStyle     = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color(colorModeNormal)).
				Bold(true).
				Padding(0, spaceCompact)
	modeInsertStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color(colorModeInsert)).
			Bold(true).
			Padding(0, spaceCompact)
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

func (m Model) modeBadge() string {
	if m.formMode.editing() {
		return modeInsertStyle.Render("INSERT")
	}
	return modeNormalStyle.Render("NORMAL")
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
	label := schema.Title()
	if schema.root {
		if schema.description == "collapsed" {
			label = "▸ " + label
		} else {
			label = "▾ " + label
		}
	} else if schema.table != "" {
		label = "  └ " + label
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk))
	if index == model.Index() {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))
		label = "> " + label
	}
	fmt.Fprint(writer, style.Render(label))
}

func newSchemaList() list.Model {
	model := newList("Databases", true)
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
	m.connection.setWidth(max(connectionWidth-4, 1))
	m.recent.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.editor.setSize(max(m.editorWidth-4, 1), max(m.editorHeight-2, 1))
	m.tableViewportWidth = max(m.editorWidth-4, 1)
	if m.compact {
		m.tableViewportWidth = max(m.editorWidth-6, 1)
	} else {
		m.tableViewportWidth = max(m.editorWidth-8, 1)
	}
	m.columnForm.setWidth(m.tableViewportWidth)
	m.columnForm.setHeight(m.formViewportHeight())
	m.browseForm.setWidth(m.tableViewportWidth)
	m.indexForm.setWidth(m.tableViewportWidth)
	m.foreignKeyForm.setWidth(m.tableViewportWidth)
	if m.explainPicker != nil {
		m.explainPicker.setWidth(m.tableViewportWidth)
	}
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
	view.KeyboardEnhancements.ReportEventTypes = true
	if m.height < 4 || m.width < 1 {
		view.SetContent(headerStyle.Render("BUBBLE WORKBENCH"))
		return view
	}
	content := m.contentView()
	fullContent := lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render("BUBBLE WORKBENCH"), content, footerStyle.Render(m.footer()))
	if m.queryLogDetail != nil {
		// Full-screen overlay for query log detail
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		m.drawQueryLogDetail(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.explainPicker != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() ||
		m.connection.confirmation != nil {
		// UV overlay path: render full UI, then overlay confirmation centered.
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if dialog := m.confirmContent(); dialog != "" {
			m.drawConfirmDialog(canvas, dialog)
		}
		view.SetContent(canvas.Render())
		return view
	}
	view.SetContent(fullContent)
	return view
}

func (m Model) hasConfirming() bool {
	return m.explainPicker != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() ||
		m.connection.confirmation != nil
}

func (m Model) confirmContent() string {
	var raw string
	switch {
	case m.explainPicker != nil:
		raw = m.explainPicker.form.View()
	case m.columnForm.confirming():
		raw = m.columnForm.confirmation.View()
	case m.browseForm.confirming():
		raw = m.browseForm.confirmation.View()
	case m.indexForm.confirming():
		raw = m.indexForm.confirmation.View()
	case m.foreignKeyForm.confirming():
		raw = m.foreignKeyForm.confirmation.View()
	case m.connection.confirmation != nil:
		raw = m.connection.confirmation.View()
	}
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for i, line := range strings.Split(raw, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " "))
	}
	return b.String()
}

func (m Model) drawConfirmDialog(canvas uv.ScreenBuffer, dialog string) {
	dialogStr := uv.NewStyledString(dialog)
	dialogW := min(dialogStr.UnicodeWidth(), canvas.Bounds().Dx()-6)
	dialogH := min(dialogStr.Height(), canvas.Bounds().Dy()-6)
	if dialogW <= 0 || dialogH <= 0 {
		return
	}
	bounds := canvas.Bounds()
	borderW := dialogW + 2
	borderH := dialogH + 2
	x := max(0, (bounds.Dx()-borderW)/2)
	y := max(0, (bounds.Dy()-borderH)/2)

	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: parseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(x, y, x+borderW, y+borderH))

	borderStyle := uv.Style{Fg: parseHex(colorBorder)}
	canvas.SetCell(x, y, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(x, y+borderH-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y+borderH-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})
	for cx := x + 1; cx < x+borderW-1; cx++ {
		canvas.SetCell(cx, y, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, y+borderH-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for cy := y + 1; cy < y+borderH-1; cy++ {
		canvas.SetCell(x, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(x+borderW-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}

	dialogStr.Draw(canvas, image.Rect(x+1, y+1, x+1+dialogW, y+1+dialogH))
}
func (m Model) drawQueryLogDetail(canvas uv.ScreenBuffer) {
	d := m.queryLogDetail
	if d == nil {
		return
	}
	var statusStr, iconStr string
	switch d.status {
	case "failed":
		statusStr = "Failed"
		iconStr = statusFailedStyle.Render(iconFailed)
	case "canceled":
		statusStr = "Canceled"
		iconStr = statusCanceledStyle.Render(iconCanceled)
	default:
		statusStr = "Success"
		iconStr = statusSuccessStyle.Render(iconSuccess)
	}

	innerW := m.width - 4

	var b strings.Builder
	b.WriteString(headerStyle.Render("  \uf0ca Query Log Detail  "))
	b.WriteString("\n\n")
	b.WriteString("  Time:     ")
	b.WriteString(d.startedAt.Format("2006-01-02 15:04:05"))
	b.WriteString("\n")
	b.WriteString("  Status:   ")
	b.WriteString(iconStr)
	b.WriteString(" ")
	b.WriteString(statusStr)
	b.WriteString("\n")
	b.WriteString("  Duration: ")
	b.WriteString(d.duration.Round(time.Microsecond).String())
	b.WriteString("\n")
	b.WriteString("  Statement:\n    ")
	b.WriteString(ansi.Wordwrap(safeText(d.statement), innerW-4, "\n    "))
	b.WriteString("\n")
	b.WriteString("  Message:  ")
	b.WriteString(ansi.Wordwrap(safeText(d.message), innerW-14, " "))
	b.WriteString("\n\n  enter/esc to close")

	// Fill background
	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: parseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(1, 1, m.width-1, m.height-1))

	// Border
	borderStyle := uv.Style{Fg: parseHex(colorBorder)}
	for x := 1; x < m.width-1; x++ {
		canvas.SetCell(x, 0, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(x, m.height-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for y := 1; y < m.height-1; y++ {
		canvas.SetCell(0, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(m.width-1, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(0, 0, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(m.width-1, 0, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(0, m.height-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(m.width-1, m.height-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})

	uv.NewStyledString(b.String()).Draw(canvas, image.Rect(1, 1, m.width-1, m.height-1))
}

func parseHex(s string) color.Color {
	var r, g, b uint8
	fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return color.RGBA{R: r, G: g, B: b, A: 255}
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
	summary := m.queryLogSummary()
	padding := max(m.queryLogHeight-1-lipgloss.Height(content)-1, 0)
	return content + strings.Repeat("\n", padding+1) +
		paneStatus(statusStyle.Render("y copy query | enter detail | e explain"), statusStyle.Render(summary), m.tableViewportWidth)
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
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, m.modeBadge()+" "+statusStyle.Render("tab view"))
}

func (m Model) sqlPaneView() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.editor.text.View(),
		tableViewportViewWithAlignment(m.results, m.resultsNumericColumns, m.resultsOffset, m.tableViewportWidth),
	)
	return content + "\n" + paneStatus("", m.resultsStatus, m.tableViewportWidth)
}

func (m Model) structureView() string {
	if m.columnForm.active() {
		return m.formViewport(m.columnForm.View(), m.columnForm.scrollOffset)
	}
	return tableViewportView(m.structure, m.structureOffset, m.tableViewportWidth)
}

func (m Model) browseView() string {
	if m.browseForm.active() {
		return m.formViewport(m.browseForm.View(), m.browseForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.browse, m.browseNumericColumns, m.browseOffset, m.tableViewportWidth) + "\n" + paneStatus("", m.browseStatus, m.tableViewportWidth)
}

func (m Model) formViewportHeight() int {
	if m.compact {
		return max(m.height-9, 1)
	}
	return max(m.workspaceHeight-5, 1)
}

func (m Model) formViewport(view string, offset int) string {
	height := m.formViewportHeight()
	lines := strings.Split(view, "\n")
	if len(lines) <= height {
		return view
	}
	offset = min(max(offset, 0), len(lines)-height)
	return strings.Join(lines[offset:offset+height], "\n")
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
	if m.relationshipDiagram {
		return m.relationshipView()
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
		parts = append(parts, "1 tables", "2 tabs", "3 history", "f fullscreen")
		parts = append(parts, "q quit")
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

func databaseSuffix(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite") || strings.HasSuffix(name, ".sqlite3")
}

func safeText(input string) string { return sharedsql.SanitizeDisplay(input) }
