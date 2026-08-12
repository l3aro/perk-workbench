package schema

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// View renders the schema pane body: the persistent filter row above the
// schema list. The row is omitted when the pane is too narrow to show it.
func (m Model) View(layout uikit.Layout) string {
	if row := m.FilterRow(layout); row != "" {
		return row + "\n" + m.List.View()
	}
	return m.List.View()
}

// Draw renders nothing: the schema pane is a lipgloss pane; its overlays
// (confirmations, context menus) are drawn by the root from the shared
// uikit dialogs. The contract mirrors the other feature components.
func (m Model) Draw(canvas uv.ScreenBuffer, layout uikit.Layout) {}

// Resize refits the schema list and filter to the pane geometry. The
// equations mirror the root layout pass exactly: the list spans the pane
// body below the filter box (3 rows when the pane is wide enough).
func (m *Model) Resize(layout uikit.Layout) {
	contentHeight := max(layout.Height-4, 0)
	schemaListHeight := max(contentHeight-2, 0)
	if m.FilterShown(layout) {
		// The filter box takes 3 rows; the list must yield two of its own.
		schemaListHeight = max(schemaListHeight-2, 0)
	}
	// The pane body content is two cells narrower than the pane (border +
	// padding each side); sizing the list wider wraps every full-width row
	// onto a second line inside the bordered pane.
	m.List.SetSize(max(layout.ViewportWidth-6, 0), schemaListHeight)
	m.Filter.SetWidth(max(layout.ViewportWidth-6, 0))
}

// ResizeTables fits the structure, index, and foreign-key tables to the
// workspace viewport. The height equation is preserved from the root layout
// pass: the tab tables yield one row to the blank line that separates their
// status line from the mode/tab-hint footer.
func (m *Model) ResizeTables(width, height int) {
	uikit.ResizeResultsTable(&m.Structure.Table, width, height)
	uikit.ResizeResultsTable(&m.Structure.Indexes, width, height)
	uikit.ResizeResultsTable(&m.Structure.ForeignKeys, width, height)
}

// RefreshTheme re-applies the shared theme styles to the schema list and
// any open schema forms after the root switches the palette. The root calls
// it from its theme apply path, matching the pre-refactor list re-theming.
func (m *Model) RefreshTheme() {
	m.List.SetDelegate(schemaItemDelegate{})
	applyListTheme(&m.List)
	for _, form := range []*huh.Form{
		m.Structure.ColumnForm.Form,
		m.Structure.IndexForm.Form,
		m.Structure.ForeignKeyForm.Form,
	} {
		if form != nil {
			form.WithTheme(uikit.FormTheme)
		}
	}
}

// FilterRow renders the schema sidebar's filter input, omitted when the
// pane is too narrow to show it.
func (m Model) FilterRow(layout uikit.Layout) string {
	if !m.FilterShown(layout) {
		return ""
	}
	return filterInputRow(m.Filter, max(layout.ViewportWidth-4, 0))
}

// filterInputRow renders a filter input in a bordered box with a
// magnifying-glass suffix, sized to the given width. The border turns
// primary while the input is focused. The input is truncated because its
// placeholder view renders one cell wider than Width.
func filterInputRow(input textinput.Model, width int) string {
	icon := lipgloss.NewStyle().Foreground(lipgloss.Color(uikit.ColorMuted)).Render("🔍")
	borderColor := uikit.ColorBorder
	if input.Focused() {
		borderColor = uikit.ColorPrimary
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(max(width-2, 0))
	// Box content area: width-2 (box) - 2 (borders) - 2 (padding) - 2 (icon).
	return box.Render(ansi.Truncate(input.View(), max(width-8, 0), "") + icon)
}

// StructureView renders the structure tab: the column form when active,
// otherwise the columns table with its filter status line.
func (m Model) StructureView(layout uikit.Layout, offset int) string {
	if m.Structure.ColumnForm.Active() {
		return viewportSlice(m.Structure.ColumnForm.View(), m.Structure.ColumnForm.ScrollOffset, layout)
	}
	return uikit.TableViewportViewWithAlignment(m.Structure.Table, nil, offset, layout.ViewportWidth, -1) + "\n" + chrome.PaneStatus(m.TableFilterStatus(core.TabStructure), "", layout.ViewportWidth)
}

// IndexesView renders the indexes tab: the index form when active,
// otherwise the indexes table with its filter status line.
func (m Model) IndexesView(layout uikit.Layout, offset int) string {
	if m.Structure.IndexForm.Active() {
		return viewportSlice(m.Structure.IndexForm.View(), m.Structure.IndexForm.ScrollOffset, layout)
	}
	return uikit.TableViewportViewWithAlignment(m.Structure.Indexes, nil, offset, layout.ViewportWidth, -1) + "\n" + chrome.PaneStatus(m.TableFilterStatus(core.TabIndexes), "", layout.ViewportWidth)
}

// ForeignKeysView renders the foreign-keys tab: the foreign-key form when
// active, the relationship diagram when toggled, otherwise the foreign-keys
// table with its filter status line.
func (m Model) ForeignKeysView(layout uikit.Layout, offset int, snapshot Snapshot) string {
	if m.Structure.ForeignKeyForm.Active() {
		return viewportSlice(m.Structure.ForeignKeyForm.View(), m.Structure.ForeignKeyForm.ScrollOffset, layout)
	}
	if m.Structure.RelationshipDiagram {
		return m.RelationshipView(layout, snapshot)
	}
	return uikit.TableViewportViewWithAlignment(m.Structure.ForeignKeys, nil, offset, layout.ViewportWidth, -1) + "\n" + chrome.PaneStatus(m.TableFilterStatus(core.TabForeignKeys), "", layout.ViewportWidth)
}

// viewportSlice clips a form view to the pane body height at the given
// scroll offset, matching the root formViewport helper.
func viewportSlice(view string, offset int, layout uikit.Layout) string {
	height := layout.PaneHeight
	lines := strings.Split(view, "\n")
	if len(lines) <= height {
		return view
	}
	offset = min(max(offset, 0), len(lines)-height)
	return strings.Join(lines[offset:offset+height], "\n")
}

// relationshipEdge describes one neighbor of the selected table and every
// foreign key connecting the two tables.
type relationshipEdge struct {
	table string   // neighbor table name
	pairs []string // "fk column → referenced column" labels
}

// RelationshipView renders the relationship diagram, falling back to the
// flat relationship list when the diagram does not fit the workspace.
func (m Model) RelationshipView(layout uikit.Layout, snapshot Snapshot) string {
	diagram := m.relationshipDiagramView(layout, snapshot)
	if lipgloss.Width(diagram) > layout.ViewportWidth || strings.Count(diagram, "\n")+1 > max(layout.Height-2, 1) {
		return m.relationshipListView(layout, snapshot) + "\n" + chrome.PaneStatus("", uikit.StatusStyle.Render("Press f for full-screen diagram"), layout.ViewportWidth)
	}
	return diagram
}

// relationshipDiagramView renders the selected table as the hub of an ERD:
// tables referencing it above, tables it references below, every connector
// landing on the center of the selected card and labeled with its column
// mapping.
func (m Model) relationshipDiagramView(layout uikit.Layout, snapshot Snapshot) string {
	incoming, outgoing := m.relationshipEdges(snapshot)
	topRows, topStubs := relationshipLane(incoming)
	bottomRows, bottomStubs := relationshipLane(outgoing)
	center := m.relationshipCenterCard(len(bottomStubs) > 0, snapshot)
	centerWidth := ansi.StringWidth(center[0])

	topMid, bottomMid := 0, 0
	if len(topStubs) > 0 {
		topMid = (topStubs[0] + topStubs[len(topStubs)-1]) / 2
	}
	if len(bottomStubs) > 0 {
		bottomMid = (bottomStubs[0] + bottomStubs[len(bottomStubs)-1]) / 2
	}

	// Every lane is positioned relative to the connector column, the middle
	// of the selected card, where its merge point lands.
	left, right := -centerWidth/2, centerWidth-centerWidth/2
	if len(topStubs) > 0 {
		left = min(left, -topMid)
		right = max(right, ansi.StringWidth(topRows[0])-topMid)
	}
	if len(bottomStubs) > 0 {
		left = min(left, -bottomMid)
		right = max(right, ansi.StringWidth(bottomRows[0])-bottomMid)
	}
	width := right - left
	centerCol := -left

	var lines []string
	if len(topStubs) > 0 {
		lines = append(lines, shiftRows(topRows, centerCol-topMid, width)...)
		stubs := shiftStubs(topStubs, centerCol-topMid)
		if len(stubs) > 1 {
			lines = append(lines, mergeRow(stubs, centerCol, width, true))
		} else {
			lines = append(lines, stubRow(stubs, width))
		}
	}
	lines = append(lines, shiftRows(center, centerCol-centerWidth/2, width)...)
	if len(bottomStubs) > 0 {
		stubs := shiftStubs(bottomStubs, centerCol-bottomMid)
		if len(stubs) > 1 {
			lines = append(lines, mergeRow(stubs, centerCol, width, false))
		} else {
			lines = append(lines, stubRow(stubs, width))
		}
		lines = append(lines, shiftRows(bottomRows, centerCol-bottomMid, width)...)
	}
	return strings.Join(lines, "\n")
}

func (m Model) relationshipListView(layout uikit.Layout, snapshot Snapshot) string {
	lines := []string{uikit.HeaderStyle.Render(relationshipLine(snapshot.SelectedTable+" relationships", max(layout.ViewportWidth-2, 1)))}
	for _, foreignKey := range m.Structure.ForeignKeyInfo {
		lines = append(lines, relationshipLine(snapshot.SelectedTable+" → "+foreignKey.ReferenceTable, layout.ViewportWidth))
	}
	for _, foreignKey := range m.Structure.ReferencingForeignKeyInfo {
		if strings.EqualFold(foreignKey.Table, snapshot.SelectedTable) {
			continue
		}
		lines = append(lines, relationshipLine(foreignKey.Table+" → "+snapshot.SelectedTable, layout.ViewportWidth))
	}
	return strings.Join(lines[:min(len(lines), max(layout.Height-3, 0))], "\n")
}

func relationshipLine(value string, width int) string {
	return ansi.Truncate(uikit.SafeText(value), max(width, 1), "…")
}

// relationshipEdges groups the foreign keys by neighbor table. incoming are
// tables that reference the selected table; outgoing are tables the selected
// table references. Self-references stay on the selected card.
func (m Model) relationshipEdges(snapshot Snapshot) (incoming, outgoing []relationshipEdge) {
	type edgeSlot struct {
		edges *[]relationshipEdge
		at    int
	}
	index := map[string]edgeSlot{}
	merge := func(edges *[]relationshipEdge, key string, edge relationshipEdge) {
		if existing, ok := index[key]; ok {
			(*existing.edges)[existing.at].pairs = appendUnique((*existing.edges)[existing.at].pairs, edge.pairs)
			return
		}
		index[key] = edgeSlot{edges, len(*edges)}
		*edges = append(*edges, edge)
	}
	for _, foreignKey := range m.Structure.ForeignKeyInfo {
		if strings.EqualFold(foreignKey.ReferenceTable, snapshot.SelectedTable) {
			continue
		}
		merge(&outgoing, "out:"+strings.ToLower(foreignKey.ReferenceTable), relationshipEdge{
			table: foreignKey.ReferenceTable,
			pairs: []string{relationshipPairLabel(foreignKey)},
		})
	}
	for _, foreignKey := range m.Structure.ReferencingForeignKeyInfo {
		if strings.EqualFold(foreignKey.Table, snapshot.SelectedTable) {
			continue
		}
		merge(&incoming, "in:"+strings.ToLower(foreignKey.Table), relationshipEdge{
			table: foreignKey.Table,
			pairs: []string{relationshipPairLabel(foreignKey.ForeignKeyInfo)},
		})
	}
	return incoming, outgoing
}

// relationshipPairLabel renders one foreign key as "column → column" pairs.
func relationshipPairLabel(foreignKey sharedsql.ForeignKeyInfo) string {
	pairs := make([]string, 0, len(foreignKey.Columns))
	for index, column := range foreignKey.Columns {
		reference := ""
		if index < len(foreignKey.ReferenceColumns) {
			reference = foreignKey.ReferenceColumns[index]
		}
		pairs = append(pairs, column+" → "+reference)
	}
	return strings.Join(pairs, ", ")
}

// relationshipCenterCard renders the selected table: primary-key and
// foreign-key columns plus self-referencing mappings. Other columns stay
// collapsed — the Columns tab shows the full schema.
func (m Model) relationshipCenterCard(connector bool, snapshot Snapshot) []string {
	foreignKeys := map[string]bool{}
	for _, foreignKey := range m.Structure.ForeignKeyInfo {
		for _, column := range foreignKey.Columns {
			foreignKeys[column] = true
		}
	}
	referenced := map[string]bool{}
	for _, foreignKey := range m.Structure.ReferencingForeignKeyInfo {
		for _, column := range foreignKey.ReferenceColumns {
			referenced[column] = true
		}
	}
	rows := make([]string, 0, len(m.Structure.Columns)+2)
	for _, column := range m.Structure.Columns {
		switch {
		case column.PrimaryKey > 0:
			rows = append(rows, "🔑 "+column.Name)
		case foreignKeys[column.Name] || referenced[column.Name]:
			rows = append(rows, "🔗 "+column.Name)
		}
	}
	for _, foreignKey := range m.Structure.ForeignKeyInfo {
		if strings.EqualFold(foreignKey.ReferenceTable, snapshot.SelectedTable) {
			rows = append(rows, "↺ "+relationshipPairLabel(foreignKey))
		}
	}
	return boxCard(snapshot.SelectedTable, rows, true, 0, connector)
}

// relationshipLane lays cards out side by side and returns the rendered rows
// plus the column of each card's center, where its connector attaches.
func relationshipLane(edges []relationshipEdge) ([]string, []int) {
	if len(edges) == 0 {
		return nil, nil
	}
	cards := make([][]string, len(edges))
	stubs := make([]int, len(edges))
	width := 0
	for index, edge := range edges {
		cards[index] = relationshipCard(edge)
		cardWidth := ansi.StringWidth(cards[index][0])
		stubs[index] = width + cardWidth/2
		width += cardWidth + 2
	}
	height := 0
	for _, card := range cards {
		height = max(height, len(card))
	}
	rows := make([]string, height)
	for row := 0; row < height; row++ {
		var line strings.Builder
		for index, card := range cards {
			if index > 0 {
				line.WriteString("  ")
			}
			cardWidth := ansi.StringWidth(card[0])
			if row < len(card) {
				line.WriteString(card[row])
			} else {
				line.WriteString(strings.Repeat(" ", cardWidth))
			}
		}
		rows[row] = line.String()
	}
	return rows, stubs
}

// relationshipCard renders one neighbor: a single row mapping the foreign
// keys onto the selected table (the mapped columns double as the card's
// column list, keeping the hub short enough for the workspace). The mapping
// carries the direction; cardinality is left out because FK uniqueness —
// and therefore N:1 vs 1:1 — is not part of the loaded foreign-key data.
func relationshipCard(edge relationshipEdge) []string {
	return boxCard(edge.table, []string{strings.Join(edge.pairs, "; ")}, false, 0, false)
}

// boxCard renders a titled box; the selected card gets the accent border and
// a highlighted title. connector marks the card's center column on the bottom
// border so the outgoing connector can attach without patching styled output.
func boxCard(title string, rows []string, selected bool, minWidth int, connector bool) []string {
	if selected {
		title = uikit.ActionSelectedStyle.Render(title)
	}
	titleWidth := ansi.StringWidth(title)
	width := max(minWidth, titleWidth+5)
	for _, row := range rows {
		width = max(width, ansi.StringWidth(row)+4)
	}
	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, "┌─ "+title+strings.Repeat("─", width-titleWidth-5)+" ┐")
	for _, row := range rows {
		lines = append(lines, "│ "+row+strings.Repeat(" ", width-ansi.StringWidth(row)-4)+" │")
	}
	bottom := []rune("└" + strings.Repeat("─", width-2) + "┘")
	if connector {
		// The outgoing connector attaches here, at the card's center column.
		bottom[width/2] = '┬'
	}
	bottomLine := string(bottom)
	lines = append(lines, bottomLine)
	if selected {
		border := lipgloss.NewStyle().Foreground(lipgloss.Color(uikit.ColorPrimary))
		lines[0] = border.Render(lines[0])
		lines[len(lines)-1] = border.Render(lines[len(lines)-1])
	}
	return lines
}

func shiftRows(rows []string, shift, width int) []string {
	out := make([]string, len(rows))
	for index, row := range rows {
		padding := max(shift, 0)
		line := strings.Repeat(" ", padding) + row
		if rest := width - ansi.StringWidth(line); rest > 0 {
			line += strings.Repeat(" ", rest)
		}
		out[index] = line
	}
	return out
}

func shiftStubs(stubs []int, shift int) []int {
	out := make([]int, len(stubs))
	for index, stub := range stubs {
		out[index] = stub + shift
	}
	return out
}

func stubRow(stubs []int, width int) string {
	line := []rune(strings.Repeat(" ", width))
	for _, stub := range stubs {
		if stub >= 0 && stub < width {
			line[stub] = '│'
		}
	}
	return string(line)
}

// mergeRow joins the stubs above (top) or below (bottom) into the connector
// column at the center of the selected card.
func mergeRow(stubs []int, centerCol, width int, top bool) string {
	line := []rune(strings.Repeat(" ", width))
	left, right := stubs[0], stubs[len(stubs)-1]
	for column := left + 1; column < right; column++ {
		if column >= 0 && column < width {
			line[column] = '─'
		}
	}
	line[left], line[centerCol], line[right] = '└', '┬', '┘'
	if !top {
		line[left], line[centerCol], line[right] = '┌', '┴', '┐'
	}
	return string(line)
}

func appendUnique(values []string, additions []string) []string {
	for _, addition := range additions {
		duplicate := false
		for _, existing := range values {
			if strings.EqualFold(existing, addition) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			values = append(values, addition)
		}
	}
	return values
}
