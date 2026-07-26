package workbench

import (
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

const (
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

type appTheme string

const (
	themeOcean      appTheme = "ocean"
	themeDracula    appTheme = "dracula"
	themeCatppuccin appTheme = "catppuccin"
	themeNord       appTheme = "nord"
	themeMonokai    appTheme = "monokai"
	themeSolarized  appTheme = "solarized"
)

var (
	activeTheme                                                                       = themeOcean
	colorCanvas, colorPanel, colorStripe                                              string
	colorInk, colorMuted, colorAccent                                                 string
	colorBorder, colorModeNormal                                                      string
	colorModeInsert                                                                   string
	headerStyle, footerStyle, statusStyle                                             lipgloss.Style
	focusStyle, panelStyle                                                            lipgloss.Style
	connectionActionStyle                                                             lipgloss.Style
	connectionActionSelectedStyle                                                     lipgloss.Style
	primaryIndexStyle, uniqueIndexStyle                                               lipgloss.Style
	regularIndexStyle                                                                 lipgloss.Style
	statusSuccessStyle, statusFailedStyle                                             lipgloss.Style
	statusCanceledStyle                                                               lipgloss.Style
	modeNormalStyle, modeInsertStyle                                                  lipgloss.Style
	selectedCellStyle, completionItemStyle, completionBoxStyle, completionDetailStyle lipgloss.Style
)

func init() { setTheme(themeOcean) }

func setTheme(name appTheme) {
	activeTheme = name
	switch name {
	case themeDracula:
		colorCanvas, colorPanel, colorStripe = "#282a36", "#343746", "#44475a"
		colorInk, colorMuted, colorAccent = "#f8f8f2", "#b1b2c7", "#bd93f9"
		colorBorder, colorModeNormal, colorModeInsert = "#6272a4", "#8be9fd", "#50fa7b"
	case themeNord:
		colorCanvas, colorPanel, colorStripe = "#2e3440", "#3b4252", "#434c5e"
		colorInk, colorMuted, colorAccent = "#eceff4", "#d8dee9", "#88c0d0"
		colorBorder, colorModeNormal, colorModeInsert = "#4c566a", "#81a1c1", "#a3be8c"
	case themeMonokai:
		colorCanvas, colorPanel, colorStripe = "#272822", "#2f302a", "#3e3d32"
		colorInk, colorMuted, colorAccent = "#f8f8f2", "#75715e", "#a6e22e"
		colorBorder, colorModeNormal, colorModeInsert = "#49483e", "#66d9ef", "#fd971f"
	case themeCatppuccin:
		colorCanvas, colorPanel, colorStripe = "#1e1e2e", "#313244", "#45475a"
		colorInk, colorMuted, colorAccent = "#cdd6f4", "#a6adc8", "#cba6f7"
		colorBorder, colorModeNormal, colorModeInsert = "#6c7086", "#89b4fa", "#a6e3a1"
	case themeSolarized:
		colorCanvas, colorPanel, colorStripe = "#002b36", "#073642", "#123f4a"
		colorInk, colorMuted, colorAccent = "#839496", "#657b83", "#268bd2"
		colorBorder, colorModeNormal, colorModeInsert = "#0e5553", "#268bd2", "#859900"
	default:
		colorCanvas, colorPanel, colorStripe = "#10151f", "#17202e", "#1c2838"
		colorInk, colorMuted, colorAccent = "#e6edf3", "#8b9bb4", "#55d6be"
		colorBorder, colorModeNormal, colorModeInsert = "#324155", "#58a6ff", "#3fb950"
	}
	resetStyles()
}

func resetStyles() {
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
	primaryIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))
	uniqueIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e3b341"))
	regularIndexStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	statusSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	statusFailedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	statusCanceledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	modeNormalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color(colorModeNormal)).
		Bold(true).
		Padding(0, spaceCompact)
	modeInsertStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color(colorModeInsert)).
		Bold(true).
		Padding(0, spaceCompact)
	selectedCellStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorCanvas)).
		Background(lipgloss.Color(colorAccent)).
		Bold(true)
	completionItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted))
	completionBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Padding(0, 1)
	completionDetailStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorBorder))
}

var formTheme = huh.ThemeFunc(func(bool) *huh.Styles {
	theme := huh.ThemeCharm(true)
	accent := lipgloss.Color(colorAccent)
	ink := lipgloss.Color(colorInk)
	muted := lipgloss.Color(colorMuted)
	panel := lipgloss.Color(colorPanel)
	stripe := lipgloss.Color(colorStripe)
	canvas := lipgloss.Color(colorCanvas)

	theme.Focused.Base = theme.Focused.Base.BorderForeground(accent)
	theme.Focused.Card = theme.Focused.Base.Background(panel)
	theme.Focused.Title = theme.Focused.Title.Foreground(accent)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(accent)
	theme.Focused.Description = theme.Focused.Description.Foreground(muted)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(accent)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(accent)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(accent)
	theme.Focused.Option = theme.Focused.Option.Foreground(ink)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(accent)
	theme.Focused.UnselectedOption = theme.Focused.UnselectedOption.Foreground(ink)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(canvas).Background(accent)
	theme.Focused.BlurredButton = theme.Focused.BlurredButton.Foreground(ink).Background(stripe)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(accent)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(muted)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(accent)
	theme.Focused.TextInput.Text = theme.Focused.TextInput.Text.Foreground(ink)
	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base.Background(panel)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()
	theme.Group.Title = theme.Focused.Title
	theme.Group.Description = theme.Focused.Description
	return theme
})

func newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(formTheme)
}

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
	delegate := newListDelegate()
	model := list.New([]list.Item{}, delegate, 0, 0)
	applyListTheme(&model)
	model.Title = title
	model.SetFilteringEnabled(filtering)
	model.SetShowPagination(false)
	model.SetShowHelp(false)
	model.DisableQuitKeybindings()
	return model
}

func newListDelegate() list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color(colorInk))
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(lipgloss.Color(colorMuted))
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color(colorAccent))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color(colorAccent))
	return delegate
}

func applyListTheme(model *list.Model) {
	model.Styles.Title = headerStyle
	model.Styles.NoItems = statusStyle
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
	return tableViewportViewWithAlignment(resultTable, nil, offset, width, -1)
}

func tableViewportViewWithAlignment(resultTable table.Model, numericColumns []bool, offset, width, selectedColumn int) string {
	offset = min(max(offset, 0), max(resultTable.Width()-width, 0))
	columns := resultTable.Columns()
	lines := []string{headerStyle.Padding(0, 0).Render(tableLineWithSelection(columns, nil, numericColumns, offset, width, -1, false))}
	rows, rowHeight := resultTable.Rows(), resultTable.Height()
	start := min(max(resultTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	for rowIndex := start; rowIndex < min(start+rowHeight, len(rows)); rowIndex++ {
		selectedRow := rowIndex == resultTable.Cursor()
		row := tableLineWithSelection(columns, rows[rowIndex], numericColumns, offset, width, selectedColumn, selectedRow)
		if selectedRow {
			row = lipgloss.NewStyle().Width(width).Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorStripe)).Render(row)
		}
		lines = append(lines, row)
	}
	for range max(rowHeight-(len(lines)-1), 0) {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func tableLine(columns []table.Column, row table.Row, numericColumns []bool, offset, width int) string {
	return tableLineWithSelection(columns, row, numericColumns, offset, width, -1, false)
}

func tableLineWithSelection(columns []table.Column, row table.Row, numericColumns []bool, offset, width, selectedColumn int, selectedRow bool) string {
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
		if selectedRow {
			value = ansi.Strip(value)
		}
		cell := strings.Repeat(" ", spaceCompact) + style.Render(ansi.Truncate(value, column.Width, "…")) + strings.Repeat(" ", spaceCompact)
		if selectedRow && index == selectedColumn {
			cell = selectedCellStyle.Render(cell)
		}
		cells[index] = cell
	}
	return cropTableLine(strings.Join(cells, ""), offset, width)
}

func cropTableLine(line string, offset, width int) string {
	var visible strings.Builder
	var buf strings.Builder
	total := offset + width
	for len(line) > 0 {
		cluster, _ := ansi.FirstGraphemeCluster(line, ansi.WcWidth)
		start := ansi.StringWidth(buf.String())
		buf.WriteString(cluster)
		if ansi.StringWidth(buf.String()) > total {
			break
		}
		if start >= offset {
			visible.WriteString(cluster)
		}
		line = line[len(cluster):]
	}
	return visible.String() + strings.Repeat(" ", max(width-ansi.StringWidth(visible.String()), 0))
}

func tableOffset(resultTable table.Model, offset, viewportWidth int) int {
	return min(max(offset, 0), max(resultTable.Width()-viewportWidth, 0))
}

func colsHint(columns []table.Column, viewportWidth int) string {
	if len(columns) <= 1 {
		return ""
	}
	totalWidth := 0
	for _, c := range columns {
		totalWidth += c.Width + 2*spaceCompact
	}
	if totalWidth <= viewportWidth {
		return ""
	}
	return fmt.Sprintf(" | %d cols", len(columns))
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
	previousViewportWidth := m.tableViewportWidth
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
	if m.tableViewportWidth != previousViewportWidth {
		for _, resultTable := range []*table.Model{&m.results, &m.structure, &m.browse, &m.indexes, &m.foreignKeys, &m.queryLog} {
			columns := resultTable.Columns()
			titles := make([]string, len(columns))
			for index, column := range columns {
				titles[index] = column.Title
			}
			resultTable.SetColumns(tableColumns(titles, resultTable.Rows()))
		}
	}
	resizeResultsTable(&m.results, m.tableViewportWidth, max(m.resultsHeight-4, 2))
	resizeResultsTable(&m.queryLog, m.tableViewportWidth, max(m.queryLogHeight-5, 2))
	resizeResultsTable(&m.structure, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	resizeResultsTable(&m.browse, m.tableViewportWidth, max(m.workspaceHeight-5, 2))
	resizeResultsTable(&m.indexes, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	resizeResultsTable(&m.foreignKeys, m.tableViewportWidth, max(m.workspaceHeight-4, 2))
	revealTableColumn(m.structure, m.structureColumn, &m.structureOffset, m.tableViewportWidth)
	revealTableColumn(m.browse, m.browseColumn, &m.browseOffset, m.tableViewportWidth)
	revealTableColumn(m.results, m.resultsColumn, &m.resultsOffset, m.tableViewportWidth)
	revealTableColumn(m.indexes, m.indexesColumn, &m.indexesOffset, m.tableViewportWidth)
	revealTableColumn(m.foreignKeys, m.foreignKeysColumn, &m.foreignKeysOffset, m.tableViewportWidth)
	revealTableColumn(m.queryLog, m.queryLogColumn, &m.queryLogOffset, m.tableViewportWidth)
}

func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	view.KeyboardEnhancements.ReportEventTypes = true
	view.MouseMode = tea.MouseModeCellMotion
	if m.height < 4 || m.width < 1 {
		view.SetContent(headerStyle.Render("PERK WORKBENCH"))
		return view
	}
	content := m.contentView()
	fullContent := lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render("PERK WORKBENCH"), content, footerStyle.Render(m.footer()))
	if m.commandPalette.visible || m.themePicker != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.themePicker != nil {
			m.drawConfirmDialog(canvas, m.themePicker.content())
		} else {
			m.commandPalette.paletteDraw(canvas, m.width, m.height)
		}
		view.SetContent(canvas.Render())
		return view
	}
	if m.queryLogDetail != nil {
		// Full-screen overlay for query log detail
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		m.drawQueryLogDetail(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.cellEditor != nil || m.explainPicker != nil || m.savedQueryPicker != nil || m.quitDialog != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() ||
		m.connection.confirmation != nil || m.contextMenu != nil || m.deleteConfirm != nil || m.queryConfirmation != nil {
		// UV overlay path: render full UI, then overlay centered.
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.contextMenu != nil {
			m.drawContextMenu(canvas)
		} else if dialog := m.activeConfirmation(); dialog != nil {
			dialog.draw(canvas)
		} else if dialog := m.confirmContent(); dialog != "" {
			m.drawConfirmDialog(canvas, dialog)
		}
		view.SetContent(canvas.Render())
		return view
	}
	view.SetContent(fullContent)
	return view
}

func (m Model) hasConfirming() bool {
	return m.explainPicker != nil || m.quitDialog != nil || m.queryConfirmation != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() || m.connection.confirmation != nil ||
		(m.cellEditor != nil && m.cellEditor.confirming)
}

func (m Model) activeConfirmation() *confirmationDialog {
	switch {
	case m.queryConfirmation != nil:
		return m.queryConfirmation.dialog
	case m.quitDialog != nil:
		return m.quitDialog
	case m.columnForm.confirming():
		return m.columnForm.confirmation
	case m.browseForm.confirming():
		return m.browseForm.confirmation
	case m.indexForm.confirming():
		return m.indexForm.confirmation
	case m.foreignKeyForm.confirming():
		return m.foreignKeyForm.confirmation
	case m.connection.confirmation != nil:
		return m.connection.confirmation
	case m.deleteConfirm != nil:
		return m.deleteConfirm
	case m.cellEditor != nil && m.cellEditor.confirming:
		return m.cellEditor.confirm
	default:
		return nil
	}
}

func (m Model) hasOverlay() bool {
	return m.commandPalette.visible || m.themePicker != nil || m.queryLogDetail != nil || m.explainPicker != nil || m.quitDialog != nil || m.cellEditor != nil || m.contextMenu != nil || m.deleteConfirm != nil || m.hasConfirming()
}

func (m Model) confirmContent() string {
	var raw string
	switch {
	case m.cellEditor != nil:
		return m.cellEditor.confirmContent()
	case m.queryConfirmation != nil:
		raw = m.queryConfirmation.dialog.content(m.width)
	case m.explainPicker != nil:
		raw = m.explainPicker.form.View()
	case m.savedQueryPicker != nil:
		raw = m.savedQueryPicker.form.View()
	case m.quitDialog != nil:
		raw = m.quitDialog.content(m.width)
	case m.columnForm.confirming():
		raw = m.columnForm.confirmation.content(m.width)
	case m.browseForm.confirming():
		raw = m.browseForm.confirmation.content(m.width)
	case m.indexForm.confirming():
		raw = m.indexForm.confirmation.content(m.width)
	case m.foreignKeyForm.confirming():
		raw = m.foreignKeyForm.confirmation.content(m.width)
	case m.connection.confirmation != nil:
		raw = m.connection.confirmation.content(m.width)
	case m.deleteConfirm != nil:
		raw = m.deleteConfirm.content(m.width)
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

	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(x, y, x+borderW, y+borderH))

	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
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

func (m Model) drawContextMenu(canvas uv.ScreenBuffer) {
	menu := m.contextMenu
	if menu == nil || !menu.visible || len(menu.options) == 0 {
		return
	}

	// Compute layout.
	maxLabel := 0
	maxKeys := 0
	for _, opt := range menu.options {
		if len(opt.label) > maxLabel {
			maxLabel = len(opt.label)
		}
		if len(opt.keys) > maxKeys {
			maxKeys = len(opt.keys)
		}
	}
	const title = "Row actions"
	pad := 2
	keyGap := 2
	optWidth := maxLabel + pad
	keyColWidth := maxKeys + keyGap
	contentWidth := max(optWidth+keyColWidth, len(title)+pad, 24)
	borderW := contentWidth + 2

	// Position, clamped to screen.
	bounds := canvas.Bounds()
	menuX := menu.x
	menuY := menu.y
	if menuX+borderW > bounds.Dx() {
		menuX = bounds.Dx() - borderW
	}
	totalH := 4 + len(menu.options)
	if menuY+totalH > bounds.Dy() {
		menuY = bounds.Dy() - totalH
	}
	menuX = max(0, menuX)
	menuY = max(0, menuY)

	bg := uv.Style{Bg: chrome.ParseHex(colorPanel)}
	selectedBg := uv.Style{Bg: chrome.ParseHex(colorAccent), Fg: chrome.ParseHex(colorCanvas)}
	inkFg := uv.Style{Fg: chrome.ParseHex(colorInk)}
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}

	// Fill background.
	bgCell := uv.Cell{Content: " ", Width: 1, Style: bg}
	canvas.FillArea(&bgCell, image.Rect(menuX, menuY, menuX+borderW, menuY+totalH))

	// Draw border.
	canvas.SetCell(menuX, menuY, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX+borderW-1, menuY, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX, menuY+totalH-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX+borderW-1, menuY+totalH-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})
	for cx := menuX + 1; cx < menuX+borderW-1; cx++ {
		canvas.SetCell(cx, menuY, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, menuY+totalH-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for cy := menuY + 1; cy < menuY+totalH-1; cy++ {
		canvas.SetCell(menuX, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(menuX+borderW-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}

	// Title row.
	cx0 := menuX + 1
	titleLine := " " + title + strings.Repeat(" ", contentWidth-len(title)-1)
	for i, ch := range titleLine {
		canvas.SetCell(cx0+i, menuY+1, &uv.Cell{Content: string(ch), Width: 1, Style: inkFg})
	}

	// Separator row.
	for cx := cx0; cx < menuX+borderW-1; cx++ {
		canvas.SetCell(cx, menuY+2, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}

	mutedFg := uv.Style{Fg: chrome.ParseHex(colorMuted)}

	// Option rows.
	for idx, opt := range menu.options {
		optY := menuY + 3 + idx
		labelWidth := maxLabel + pad - 1
		labelPart := " " + opt.label + strings.Repeat(" ", labelWidth-len(opt.label))
		keyPart := strings.Repeat(" ", keyColWidth-len(opt.keys)) + opt.keys
		line := labelPart + keyPart
		if len(line) < contentWidth {
			line += strings.Repeat(" ", contentWidth-len(line))
		}
		optStyle := inkFg
		keyStyle := mutedFg
		if idx == menu.selected {
			optStyle = selectedBg
			keyStyle = selectedBg
		}
		for i, ch := range line {
			style := optStyle
			if i >= len(labelPart) && i < len(labelPart)+len(keyPart) {
				style = keyStyle
			}
			canvas.SetCell(cx0+i, optY, &uv.Cell{Content: string(ch), Width: 1, Style: style})
		}
	}

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
	b.WriteString(ansi.Wordwrap(safeText(chrome.DetailValue(d.statement)), innerW-4, "\n    "))
	b.WriteString("\n")
	b.WriteString("  Message:  ")
	b.WriteString(ansi.Wordwrap(safeText(chrome.DetailValue(d.message)), innerW-14, " "))
	b.WriteString("\n\n  y copy | e explain | enter/esc close")

	// Fill background
	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(1, 1, m.width-1, m.height-1))

	// Border
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
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
	content := tableViewportViewWithAlignment(m.queryLog, nil, m.queryLogOffset, m.tableViewportWidth, m.queryLogColumn)
	summary := m.queryLogSummary() + colsHint(m.queryLog.Columns(), m.tableViewportWidth)
	padding := max(m.queryLogHeight-1-lipgloss.Height(content)-1, 0)
	return content + strings.Repeat("\n", padding+1) +
		chrome.PaneStatus(statusStyle.Render("y copy cell | enter detail | e explain"), statusStyle.Render(summary), m.tableViewportWidth)
}

func (m Model) workspaceView() string {
	tabs := []string{"Columns", "Browse", "SQL", "Indexes", "Foreign Keys"}
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
	modeLine := m.modeBadge()
	if m.compact && m.SelectedTable != "" {
		modeLine += "  " + statusStyle.Render(m.SelectedTable)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, modeLine+" "+statusStyle.Render("L/H tabs"))
}

func (m Model) sqlPaneView() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.editor.View(),
		tableViewportViewWithAlignment(m.results, m.resultsNumericColumns, m.resultsOffset, m.tableViewportWidth, m.resultsColumn),
	)

	if dropdown := m.completionOverlay(); dropdown != "" {
		lines := strings.Split(content, "\n")
		overlayLines := strings.Split(dropdown, "\n")
		startLine := m.completionCursorOffset() + 1
		if startLine < 1 {
			startLine = 1
		}
		for i, ol := range overlayLines {
			if startLine+i < len(lines) {
				lines[startLine+i] = ol
			}
		}
		content = strings.Join(lines, "\n")
	}

	return content + "\n" + chrome.PaneStatus("", m.resultsStatus, m.tableViewportWidth)
}

func (m Model) structureView() string {
	if m.columnForm.active() {
		return m.formViewport(m.columnForm.View(), m.columnForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.structure, nil, m.structureOffset, m.tableViewportWidth, m.structureColumn)
}

func (m Model) browseView() string {
	if m.browseForm.active() {
		return m.formViewport(m.browseForm.View(), m.browseForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.browse, m.browseNumericColumns, m.browseOffset, m.tableViewportWidth, m.browseColumn) + "\n" + chrome.PaneStatus("", m.browseStatus, m.tableViewportWidth)
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

func (m Model) indexesView() string {
	if m.indexForm.active() {
		return m.indexForm.View()
	}
	return tableViewportViewWithAlignment(m.indexes, nil, m.indexesOffset, m.tableViewportWidth, m.indexesColumn)
}

func (m Model) foreignKeysView() string {
	if m.foreignKeyForm.active() {
		return m.foreignKeyForm.View()
	}
	if m.relationshipDiagram {
		return m.relationshipView()
	}
	return tableViewportViewWithAlignment(m.foreignKeys, nil, m.foreignKeysOffset, m.tableViewportWidth, m.foreignKeysColumn)
}

func (m Model) footer() string {
	if m.State == stateConnection {
		quitKey := m.keybindings.DisplayKey("app.quit")
		quitHint := chrome.FormatFooterKey(quitKey) + " quit"
		return safeText(m.Status + " | 1 profiles | 2 form | tab controls | a add | e edit | d delete | / filter | " + quitHint)
	}
	if m.State == stateReady {
		quitKey := m.keybindings.DisplayKey("app.quit_dialog")
		quitHint := chrome.FormatFooterKey(quitKey) + " quit"
		parts := []string{}
		if m.Status != "" {
			parts = append(parts, m.Status)
		}
		if m.databaseInfo.Product != "" && m.databaseInfo.Version != "" {
			parts = append(parts, m.databaseInfo.Product+" "+m.databaseInfo.Version)
		}
		parts = append(parts, "1 tables", "2 tabs", "3 history", "f fullscreen", "^p palette")
		parts = append(parts, quitHint)
		return safeText(strings.Join(parts, " | "))
	}
	quitKey := m.keybindings.DisplayKey("app.quit")
	quitHint := chrome.FormatFooterKey(quitKey) + " quit"
	return safeText(m.Status + " | " + quitHint)
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

// cellText truncates a cell value to MaxRunes runes and appends "…" for the
// table display. The original full value remains in browseResult for editing.
func cellText(input string) string {
	return sharedsql.SanitizeDisplay(input, sharedsql.MaxRunes)
}
