package workbench

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func titledPane(title, content string, style lipgloss.Style) string {
	border := style.GetBorderStyle()
	borderStyle := lipgloss.NewStyle().
		Foreground(style.GetBorderTopForeground()).
		Background(style.GetBorderTopBackground())
	width := style.GetWidth()
	labelWidth := max(width-lipgloss.Width(border.TopLeft)-lipgloss.Width(border.TopRight), 0)
	label := ""
	if labelWidth >= 3 {
		label = " " + ansi.Truncate(title, labelWidth-2, "") + " "
	}
	top := borderStyle.Render(border.TopLeft) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorSecondary)).Bold(true).Render(label) +
		borderStyle.Render(strings.Repeat(border.Top, max(width-lipgloss.Width(border.TopLeft)-lipgloss.Width(label)-lipgloss.Width(border.TopRight), 0))) +
		borderStyle.Render(border.TopRight)
	bodyStyle := style.Copy().BorderTop(false)
	if height := style.GetHeight(); height > 0 {
		bodyStyle = bodyStyle.Height(height - 1)
	}
	return top + "\n" + bodyStyle.Render(content)
}

func paneStyle(focused bool) lipgloss.Style {
	if focused {
		return focusStyle
	}
	return panelStyle
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
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color(colorPrimary))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color(colorPrimary))
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

// treeMarkers returns the node marker for each tree level: database, schema,
// table/view. Nerd Font icons are the default; config.json "nerd_font":
// false falls back to geometric symbols for terminals without a Nerd Font.
func treeMarkers() (database, schema, table string) {
	if appConfig.NerdFont == nil || *appConfig.NerdFont {
		return "\uf1c0", "\uf07b", "\uf0ce" // nf-fa-database, nf-fa-folder, nf-fa-table
	}
	return "▣", "▤", "▪"
}

// schemaItemDelegate renders each tree level with its own marker (database,
// schema, table/view); state is conveyed by color: muted when idle,
// secondary on the open table's path, primary when selected.
func (schemaItemDelegate) Render(writer io.Writer, model list.Model, index int, item list.Item) {
	schema, ok := item.(schemaItem)
	if !ok {
		return
	}
	dbMarker, schemaMarker, tableMarker := treeMarkers()
	marker, indent := tableMarker, ""
	switch {
	case schema.root:
		marker = dbMarker
	case schema.kind == "schema":
		marker, indent = schemaMarker, "  "
	case schema.kind == "view", schema.table != "":
		if schema.schema != "" {
			indent = "    "
		} else {
			indent = "  "
		}
	}
	label := schema.Title()
	if schema.root || schema.kind == "schema" {
		if schema.count >= 0 {
			label += fmt.Sprintf(" (%d)", schema.count)
		}
	}
	if schema.kind == "view" {
		label += " (view)"
	}
	// Estimated row counts (PostgreSQL reltuples, MySQL table_rows) show as
	// a badge.
	if schema.rowCount != nil {
		label += " (" + abbreviateCount(*schema.rowCount) + ")"
	}
	color := colorMuted // idle
	if schema.open {
		color = colorSecondary
	}
	if index == model.Index() {
		color = colorPrimary
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	// The marker renders bold so the icon reads larger than the regular
	// label; terminal cells are fixed size, so weight is the only scaling
	// that keeps the layout intact.
	prefix := indent + style.Bold(true).Render(marker+" ")
	// Parenthetical badges (row counts, view marker) pin to the right edge
	// of the sidebar; the name truncates with an ellipsis so a long name
	// never wraps or overlaps the badge.
	name, badge, hasBadge := strings.Cut(label, " (")
	if hasBadge {
		badge = " (" + badge
		if lipgloss.Width(badge) >= model.Width()-lipgloss.Width(prefix) {
			badge, hasBadge = "", false // sidebar too narrow for the badge
		}
	}
	if limit := model.Width() - lipgloss.Width(prefix) - lipgloss.Width(badge); lipgloss.Width(name) > limit {
		name = ansi.Truncate(name, max(limit, 0), "…")
	}
	line := prefix + style.Render(name)
	if hasBadge {
		line += strings.Repeat(" ", model.Width()-lipgloss.Width(line)-lipgloss.Width(badge)) + style.Render(badge)
	}
	fmt.Fprint(writer, line)
}

func newSchemaList() list.Model {
	model := newList("", true)
	model.SetShowTitle(false)
	model.SetShowFilter(false)
	model.SetShowStatusBar(false)
	// The sidebar renders its own persistent filter input; the list's
	// built-in filter bar and keybinding are unused.
	model.KeyMap.Filter = key.NewBinding(key.WithDisabled())
	model.SetDelegate(schemaItemDelegate{})
	return model
}

// newSchemaFilterInput returns the sidebar's persistent filter input. The
// list's built-in filter is driven externally via SetFilterText so the input
// can stay visible with its own placeholder and icon.
func newSchemaFilterInput() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "filter"
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	input.SetStyles(styles)
	input.CharLimit = 64
	return input
}

// schemaFilterShown reports whether the pane is wide enough for the filter
// box (3 rows: top border, input, bottom border).
func (m Model) schemaFilterShown() bool {
	return m.schemaWidth >= 7
}

// schemaFilterRow renders the sidebar's filter input in a bordered box with
// a magnifying-glass suffix, sized to the list width. The border turns
// primary while the input is focused. The input is truncated because its
// placeholder view renders one cell wider than Width.
func (m Model) schemaFilterRow() string {
	if !m.schemaFilterShown() {
		return ""
	}
	width := max(m.schemaWidth-4, 0)
	icon := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("🔍")
	borderColor := colorBorder
	if m.schemaFilter.Focused() {
		borderColor = colorPrimary
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(max(width-2, 0))
	// Box content area: width-2 (box) - 2 (borders) - 2 (padding) - 2 (icon).
	return box.Render(ansi.Truncate(m.schemaFilter.View(), max(width-8, 0), "") + icon)
}

// abbreviateCount renders a compact human-readable count: 10k, 490k,
// 1.23M; up to two decimals with trailing zeros trimmed, raw below 1k.
func abbreviateCount(n int64) string {
	trim := func(s string) string { return strings.TrimRight(strings.TrimRight(s, "0"), ".") }
	switch {
	case n >= 1_000_000_000_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000_000_000)) + "T"
	case n >= 1_000_000_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000_000)) + "B"
	case n >= 1_000_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000)) + "M"
	case n >= 1_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000)) + "k"
	default:
		return strconv.FormatInt(n, 10)
	}
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
			Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary)).Background(lipgloss.Color(colorStripe)),
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
			Foreground(lipgloss.Color(colorPrimary)).
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

// cellStyle returns a cached width-fixed table cell style. Styles depend only
// on (width, alignment); distinct widths are bounded by the column count, and
// all access happens on the Bubble Tea UI goroutine.
var (
	cellStyleCache      = map[int]lipgloss.Style{}
	cellStyleCacheRight = map[int]lipgloss.Style{}
)

func cellStyle(width int, numeric bool) lipgloss.Style {
	cache := cellStyleCache
	if numeric {
		cache = cellStyleCacheRight
	}
	if style, ok := cache[width]; ok {
		return style
	}
	style := lipgloss.NewStyle().Width(width).MaxWidth(width).Inline(true)
	if numeric {
		style = style.Align(lipgloss.Right)
	}
	cache[width] = style
	return style
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
		style := cellStyle(column.Width, row != nil && index < len(numericColumns) && numericColumns[index])
		if selectedRow {
			value = ansi.Strip(value)
		}
		cell := strings.Repeat(" ", spaceCompact) + style.Render(ansi.Truncate(value, column.Width, "…")) + strings.Repeat(" ", spaceCompact)
		cells[index] = cell
	}
	line := cropTableLine(strings.Join(cells, ""), offset, width)
	if !selectedRow {
		return line
	}
	if selectedColumn < 0 || selectedColumn >= len(columns) {
		return highlightedTableRow(line, 0, 0)
	}
	selectedStart := 0
	for _, column := range columns[:selectedColumn] {
		selectedStart += column.Width + 2*spaceCompact
	}
	return highlightedTableRow(line, selectedStart-offset, columns[selectedColumn].Width+2*spaceCompact)
}

func cropTableLine(line string, offset, width int) string {
	visible := tableLineSegment(line, offset, width)
	return visible + strings.Repeat(" ", max(width-ansi.StringWidth(visible), 0))
}

// tableLineSegment returns the visible columns [offset, offset+width) of the
// line. Clusters are included on their start column (a cluster may overhang
// the right edge), matching the crop contract; ANSI sequences count as zero
// width and are kept whole so colors survive cropping.
func tableLineSegment(line string, offset, width int) string {
	var visible strings.Builder
	total := offset + width
	lineWidth := 0
	for len(line) > 0 {
		cluster, _ := ansi.FirstGraphemeCluster(line, ansi.WcWidth)
		if strings.HasPrefix(cluster, "\x1b") {
			n := ansiSequenceLen(line)
			if lineWidth >= offset && lineWidth < total {
				visible.WriteString(line[:n])
			}
			line = line[n:]
			continue
		}
		clusterWidth := ansi.StringWidth(cluster)
		if lineWidth+clusterWidth > total {
			break
		}
		if lineWidth >= offset {
			visible.WriteString(cluster)
		}
		line = line[len(cluster):]
		lineWidth += clusterWidth
	}
	return visible.String()
}

func highlightedTableRow(line string, selectedStart, selectedWidth int) string {
	lineWidth := ansi.StringWidth(line)
	selectedEnd := min(max(selectedStart+selectedWidth, 0), lineWidth)
	selectedStart = min(max(selectedStart, 0), lineWidth)
	rowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary)).Background(lipgloss.Color(colorStripe))
	if selectedStart == selectedEnd {
		return rowStyle.Render(line)
	}
	return rowStyle.Render(tableLineSegment(line, 0, selectedStart)) +
		selectedCellStyle.Render(tableLineSegment(line, selectedStart, selectedEnd-selectedStart)) +
		rowStyle.Render(tableLineSegment(line, selectedEnd, lineWidth-selectedEnd))
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
