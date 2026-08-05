package workbench

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// relationshipEdge describes one neighbor of the selected table and every
// foreign key connecting the two tables.
type relationshipEdge struct {
	table string   // neighbor table name
	pairs []string // "fk column → referenced column" labels
}

func (m Model) relationshipView() string {
	diagram := m.relationshipDiagramView()
	if lipgloss.Width(diagram) > m.tableViewportWidth || strings.Count(diagram, "\n")+1 > max(m.workspaceHeight-2, 1) {
		return m.relationshipListView() + "\n" + chrome.PaneStatus("", statusStyle.Render("Press f for full-screen diagram"), m.tableViewportWidth)
	}
	return diagram
}

// relationshipDiagramView renders the selected table as the hub of an ERD:
// tables referencing it above, tables it references below, every connector
// landing on the center of the selected card and labeled with its column
// mapping.
func (m Model) relationshipDiagramView() string {
	incoming, outgoing := m.relationshipEdges()
	topRows, topStubs := relationshipLane(incoming)
	bottomRows, bottomStubs := relationshipLane(outgoing)
	center := m.relationshipCenterCard(len(bottomStubs) > 0)
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

func (m Model) relationshipListView() string {
	lines := []string{headerStyle.Render(relationshipLine(m.SelectedTable+" relationships", max(m.tableViewportWidth-2, 1)))}
	for _, foreignKey := range m.foreignKeyInfo {
		lines = append(lines, relationshipLine(m.SelectedTable+" → "+foreignKey.ReferenceTable, m.tableViewportWidth))
	}
	for _, foreignKey := range m.referencingForeignKeyInfo {
		if strings.EqualFold(foreignKey.Table, m.SelectedTable) {
			continue
		}
		lines = append(lines, relationshipLine(foreignKey.Table+" → "+m.SelectedTable, m.tableViewportWidth))
	}
	return strings.Join(lines[:min(len(lines), max(m.workspaceHeight-3, 0))], "\n")
}

func relationshipLine(value string, width int) string {
	return ansi.Truncate(safeText(value), max(width, 1), "…")
}

// relationshipEdges groups the foreign keys by neighbor table. incoming are
// tables that reference the selected table; outgoing are tables the selected
// table references. Self-references stay on the selected card.
func (m Model) relationshipEdges() (incoming, outgoing []relationshipEdge) {
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
	for _, foreignKey := range m.foreignKeyInfo {
		if strings.EqualFold(foreignKey.ReferenceTable, m.SelectedTable) {
			continue
		}
		merge(&outgoing, "out:"+strings.ToLower(foreignKey.ReferenceTable), relationshipEdge{
			table: foreignKey.ReferenceTable,
			pairs: []string{relationshipPairLabel(foreignKey)},
		})
	}
	for _, foreignKey := range m.referencingForeignKeyInfo {
		if strings.EqualFold(foreignKey.Table, m.SelectedTable) {
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
func (m Model) relationshipCenterCard(connector bool) []string {
	foreignKeys := map[string]bool{}
	for _, foreignKey := range m.foreignKeyInfo {
		for _, column := range foreignKey.Columns {
			foreignKeys[column] = true
		}
	}
	referenced := map[string]bool{}
	for _, foreignKey := range m.referencingForeignKeyInfo {
		for _, column := range foreignKey.ReferenceColumns {
			referenced[column] = true
		}
	}
	rows := make([]string, 0, len(m.structureColumns)+2)
	for _, column := range m.structureColumns {
		switch {
		case column.PrimaryKey > 0:
			rows = append(rows, "🔑 "+column.Name)
		case foreignKeys[column.Name] || referenced[column.Name]:
			rows = append(rows, "🔗 "+column.Name)
		}
	}
	for _, foreignKey := range m.foreignKeyInfo {
		if strings.EqualFold(foreignKey.ReferenceTable, m.SelectedTable) {
			rows = append(rows, "↺ "+relationshipPairLabel(foreignKey))
		}
	}
	return boxCard(m.SelectedTable, rows, true, 0, connector)
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
		title = connectionActionSelectedStyle.Render(title)
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
		border := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary))
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
