package schema

import (
	"sort"
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

// IndexesView renders the indexes tab: the index form when active, the
// indexes diagram when toggled, otherwise the indexes table with its
// filter status line.
func (m Model) IndexesView(layout uikit.Layout, offset int, snapshot Snapshot) string {
	if m.Structure.IndexForm.Active() {
		return viewportSlice(m.Structure.IndexForm.View(), m.Structure.IndexForm.ScrollOffset, layout)
	}
	if m.Structure.IndexDiagram {
		return m.IndexDiagramView(layout, snapshot)
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
// foreign key connecting the two tables. unique is the card-level FK
// uniqueness (all FKs between the two tables unique → 1:1); uniqueOK is
// false when the index cache cannot answer.
type relationshipEdge struct {
	table    string   // neighbor table name
	pairs    []string // "fk column → referenced column" labels
	unique   bool
	uniqueOK bool
}

// RelationshipView renders the relationship diagram, falling back to the
// flat relationship list when the diagram does not fit the workspace.
func (m Model) RelationshipView(layout uikit.Layout, snapshot Snapshot) string {
	art := m.relationshipDiagramArt(snapshot)
	if !diagramFits(art, layout) {
		return m.relationshipListView(layout, snapshot) + "\n" + chrome.PaneStatus("", uikit.StatusStyle.Render("Press f for full-screen diagram"), layout.ViewportWidth)
	}
	return strings.Join(art.lines, "\n")
}

// diagramArt is a rendered focus diagram: the output lines.
type diagramArt struct {
	lines []string
}

// diagramFits reports whether the diagram fits the workspace viewport.
func diagramFits(art diagramArt, layout uikit.Layout) bool {
	diagram := strings.Join(art.lines, "\n")
	return lipgloss.Width(diagram) <= layout.ViewportWidth && strings.Count(diagram, "\n")+1 <= max(layout.Height-2, 1)
}

// diagramNode is one card's content: the table name and its body rows.
type diagramNode struct {
	table string
	rows  []string
}

// diagramLane is one rendered ring level: the lane rows and the column of
// each card's center (where its connector attaches).
type diagramLane struct {
	rows  []string
	stubs []int
}

// relationshipDiagramArt renders the selected table as the hub of a focus
// ERD: tables referencing it above, tables it references below, every
// connector landing on the center of the selected card. The hub's ring
// carries the column mappings; deeper rings are title-only.
func (m Model) relationshipDiagramArt(snapshot Snapshot) diagramArt {
	top, bottom := m.relationshipRings(snapshot)
	hub := m.relationshipCenterCard(len(bottom) > 0, snapshot)
	topLanes := make([]diagramLane, len(top))
	topLabels := make([]*connectorLabels, len(top))
	for level, edges := range top {
		topLanes[level] = diagramLaneFor(relationshipNodes(edges))
		if level == 0 {
			topLabels[level] = relationshipLaneLabels(edges)
		}
	}
	bottomLanes := make([]diagramLane, len(bottom))
	bottomLabels := make([]*connectorLabels, len(bottom))
	for level, edges := range bottom {
		bottomLanes[level] = diagramLaneFor(relationshipNodes(edges))
		if level == 0 {
			bottomLabels[level] = relationshipLaneLabels(edges)
		}
	}
	return focusDiagramArt(topLanes, bottomLanes, hub, topLabels, bottomLabels)
}

// relationshipRings returns the selected table's focus rings: top levels
// hold tables referencing the ring below, bottom levels tables the ring
// above references. Level-0 cards carry the FK column mappings to the hub;
// deeper levels are title-only. Without the whole-schema cache the rings
// degrade to the selected table's own foreign-key data (depth 1).
func (m Model) relationshipRings(snapshot Snapshot) (top, bottom [][]relationshipEdge) {
	hub := snapshot.SelectedTable
	if hub == "" {
		return nil, nil
	}
	if len(snapshot.ForeignKeysAll) > 0 {
		hub = diagramHubKey(snapshot.ForeignKeysAll, hub)
		depth := max(m.Structure.DiagramDepth, 1)
		topNames, bottomNames := ringsFromMap(snapshot.ForeignKeysAll, hub, depth)
		top = make([][]relationshipEdge, len(topNames))
		for level, names := range topNames {
			top[level] = relationshipEdgesForLevel(names, hub, snapshot.ForeignKeysAll, snapshot.IndexesAll, true, level == 0)
		}
		bottom = make([][]relationshipEdge, len(bottomNames))
		for level, names := range bottomNames {
			bottom[level] = relationshipEdgesForLevel(names, hub, snapshot.ForeignKeysAll, snapshot.IndexesAll, false, level == 0)
		}
		return top, bottom
	}
	incoming, outgoing := m.relationshipEdges(snapshot)
	if len(incoming) > 0 {
		top = append(top, incoming)
	}
	if len(outgoing) > 0 {
		bottom = append(bottom, outgoing)
	}
	return top, bottom
}

// relationshipEdgesForLevel builds one ring level's edges from the
// whole-schema map. Incoming levels list tables referencing the hub;
// labeled levels carry the FK column mappings to the hub (the hub's own
// list only when it declares foreign keys in the map). Each level-0 edge
// also records the card-level FK uniqueness: 1:1 only when every FK
// between the two tables is unique.
func relationshipEdgesForLevel(names []string, hub string, foreignKeys map[string][]sharedsql.ForeignKeyInfo, indexes map[string][]sharedsql.IndexInfo, incoming, labeled bool) []relationshipEdge {
	edges := make([]relationshipEdge, 0, len(names))
	for _, name := range names {
		edge := relationshipEdge{table: name}
		if !labeled {
			edges = append(edges, edge)
			continue
		}
		unique, uniqueOK := true, false
		appendFK := func(foreignKey sharedsql.ForeignKeyInfo) {
			edge.pairs = appendUnique(edge.pairs, []string{relationshipPairLabel(foreignKey)})
			u, ok := fkCardinality(indexes, foreignKeyTable(foreignKey, name, hub, incoming), foreignKey.Columns)
			uniqueOK = uniqueOK || ok
			unique = unique && u
		}
		if incoming {
			for _, foreignKey := range foreignKeys[name] {
				if strings.EqualFold(foreignKey.ReferenceTable, hub) {
					appendFK(foreignKey)
				}
			}
		} else if key := diagramTableKey(foreignKeys, hub); key != "" {
			for _, foreignKey := range foreignKeys[key] {
				if strings.EqualFold(foreignKey.ReferenceTable, name) {
					appendFK(foreignKey)
				}
			}
		}
		edge.unique, edge.uniqueOK = unique, uniqueOK
		edges = append(edges, edge)
	}
	return edges
}

// foreignKeyTable returns the FK holder for a cardinality lookup: the
// card on incoming edges (it declares the FK), the hub on outgoing edges.
func foreignKeyTable(foreignKey sharedsql.ForeignKeyInfo, name, hub string, incoming bool) string {
	if incoming {
		return name
	}
	return hub
}

// connectorLabels are the level-0 edge labels of one side: the child-end
// ("(N)" unless the FK columns are unique, then "(1)") and parent-end
// ("(1)") labels per edge. The arrow glyph between them always points
// from the parent (the referenced table) to the child (the FK holder) —
// "(1)---->(N)" — and the parent is always the lower of the two tables,
// so the arrow reads upward. Both ends stay per edge so fan-outs with
// mixed uniqueness never mislabel a shared hub column.
//
// Placement differs per side: top sides (child = cards) render the child
// labels at the stub columns beside the cards and the parent label at
// the hub column beside the hub; bottom sides (child = hub, parent =
// cards) render both rows at the stub columns — the child row on the hub
// side of the arrow, the parent row beside the cards.
type connectorLabels struct {
	child  []string
	parent []string
}

// relationshipLaneLabels builds the per-edge endpoint labels for one
// side. It returns nil when the index cache cannot answer any edge, so
// no label rows are rendered.
func relationshipLaneLabels(edges []relationshipEdge) *connectorLabels {
	labels := &connectorLabels{
		child:  make([]string, len(edges)),
		parent: make([]string, len(edges)),
	}
	for index, edge := range edges {
		if !edge.uniqueOK {
			return nil
		}
		if edge.unique {
			labels.child[index] = "(1)"
		} else {
			labels.child[index] = "(N)"
		}
		labels.parent[index] = "(1)"
	}
	return labels
}

// fkCardinality reports whether the referencing table's foreign-key
// columns are unique — a unique or primary index over exactly those
// columns — which turns the N:1 edge into 1:1. ok is false only when the
// index cache is not loaded; a loaded cache keys every table (SQLite) or
// every indexed table (MySQL/PostgreSQL), so a missing key means the
// table has no indexes and its FK columns cannot be unique.
func fkCardinality(indexes map[string][]sharedsql.IndexInfo, table string, columns []string) (unique, ok bool) {
	if len(indexes) == 0 {
		return false, false
	}
	key := diagramIndexKey(indexes, table)
	if key == "" {
		return false, true
	}
	for _, index := range indexes[key] {
		if (index.Unique || index.PrimaryKey) && sameColumnSet(index.Columns, columns) {
			return true, true
		}
	}
	return false, true
}

// sameColumnSet reports whether two column lists cover the same set,
// case-insensitively and order-insensitively (a composite unique index on
// (a, b) still guarantees uniqueness of the (b, a) key).
func sameColumnSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, column := range got {
		found := false
		for _, other := range want {
			if strings.EqualFold(column, other) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ringsFromMap computes the focus rings around hub in the whole-schema
// foreign-key map: top levels hold tables that reference the level below
// them, bottom levels tables referenced by the level above. Both depth-1
// sides are seeded before either expands, so a direct neighbor is never
// suppressed by a longer path on the other side. Every level is sorted by
// name and each table appears once, on the side that reaches it first.
// Identifiers are case-insensitive (SQLite, MySQL): adjacency and the seen
// set are keyed by lowercase while the levels carry the display names.
func ringsFromMap(foreignKeys map[string][]sharedsql.ForeignKeyInfo, hub string, depth int) (top, bottom [][]string) {
	if depth < 1 {
		depth = 1
	}
	referencing := map[string][]string{} // lower(table) -> display names of tables referencing it
	referenced := map[string][]string{}  // lower(table) -> display names of tables it references
	for table, keys := range foreignKeys {
		for _, key := range keys {
			if strings.EqualFold(table, key.ReferenceTable) {
				continue
			}
			referencing[strings.ToLower(key.ReferenceTable)] = append(referencing[strings.ToLower(key.ReferenceTable)], table)
			referenced[strings.ToLower(table)] = append(referenced[strings.ToLower(table)], key.ReferenceTable)
		}
	}
	seen := map[string]bool{strings.ToLower(hub): true}
	// Both depth-1 candidate sets are collected before either is marked
	// seen, so a direct neighbor is never suppressed by the other side's
	// seed or a longer path. A table directly adjacent on both sides goes
	// to the top (referencing) ring; the sets are keyed by lowercase with
	// display names preserved.
	topSet := map[string]string{}
	for _, candidate := range referencing[strings.ToLower(hub)] {
		topSet[strings.ToLower(candidate)] = candidate
	}
	bottomSet := map[string]string{}
	for _, candidate := range referenced[strings.ToLower(hub)] {
		key := strings.ToLower(candidate)
		if _, taken := topSet[key]; !taken {
			bottomSet[key] = candidate
		}
	}
	topLevel := unseenSorted(mapValues(topSet), seen)
	bottomLevel := unseenSorted(mapValues(bottomSet), seen)
	if len(topLevel) > 0 {
		top = append(top, topLevel)
	}
	if len(bottomLevel) > 0 {
		bottom = append(bottom, bottomLevel)
	}
	for hop := 1; hop < depth; hop++ {
		next := []string{}
		for _, table := range topLevel {
			next = append(next, referencing[strings.ToLower(table)]...)
		}
		topLevel = unseenSorted(next, seen)
		if len(topLevel) == 0 {
			break
		}
		top = append(top, topLevel)
	}
	for hop := 1; hop < depth; hop++ {
		next := []string{}
		for _, table := range bottomLevel {
			next = append(next, referenced[strings.ToLower(table)]...)
		}
		bottomLevel = unseenSorted(next, seen)
		if len(bottomLevel) == 0 {
			break
		}
		bottom = append(bottom, bottomLevel)
	}
	return top, bottom
}

// unseenSorted filters out already-seen tables (by lowercase), marks the
// rest seen, and sorts them.
func unseenSorted(candidates []string, seen map[string]bool) []string {
	next := []string{}
	for _, candidate := range candidates {
		key := strings.ToLower(candidate)
		if !seen[key] {
			seen[key] = true
			next = append(next, candidate)
		}
	}
	sort.Strings(next)
	return next
}

// mapValues returns the values of a lowercase→display name map in
// arbitrary order.
func mapValues(set map[string]string) []string {
	values := make([]string, 0, len(set))
	for _, value := range set {
		values = append(values, value)
	}
	return values
}

// diagramHubKey resolves the selected table to the whole-schema map's name
// convention: the exact key when present, the bare key (MySQL keys are
// bare while the selected table is database-qualified), any case-insensitive
// key match, or any matching reference-table name when the table only
// receives foreign keys.
func diagramHubKey(foreignKeys map[string][]sharedsql.ForeignKeyInfo, table string) string {
	if _, ok := foreignKeys[table]; ok {
		return table
	}
	bare := table
	if dot := strings.LastIndex(table, "."); dot >= 0 {
		bare = table[dot+1:]
		if _, ok := foreignKeys[bare]; ok {
			return bare
		}
	}
	for key := range foreignKeys {
		if strings.EqualFold(key, table) || strings.EqualFold(key, bare) {
			return key
		}
	}
	for _, keys := range foreignKeys {
		for _, key := range keys {
			if strings.EqualFold(key.ReferenceTable, table) || strings.EqualFold(key.ReferenceTable, bare) {
				return key.ReferenceTable
			}
		}
	}
	return table
}

// diagramTableKey matches a table name against a whole-schema foreign-key
// map: exact first, then the bare name after the last dot (MySQL keys are
// bare while the selected table is database-qualified), then any
// case-insensitive key match.
func diagramTableKey(foreignKeys map[string][]sharedsql.ForeignKeyInfo, table string) string {
	if _, ok := foreignKeys[table]; ok {
		return table
	}
	bare := table
	if dot := strings.LastIndex(table, "."); dot >= 0 {
		bare = table[dot+1:]
		if _, ok := foreignKeys[bare]; ok {
			return bare
		}
	}
	for key := range foreignKeys {
		if strings.EqualFold(key, table) || strings.EqualFold(key, bare) {
			return key
		}
	}
	return ""
}

// diagramIndexKey matches a table name against the whole-schema index map,
// mirroring diagramTableKey.
func diagramIndexKey(indexes map[string][]sharedsql.IndexInfo, table string) string {
	if _, ok := indexes[table]; ok {
		return table
	}
	bare := table
	if dot := strings.LastIndex(table, "."); dot >= 0 {
		bare = table[dot+1:]
		if _, ok := indexes[bare]; ok {
			return bare
		}
	}
	for key := range indexes {
		if strings.EqualFold(key, table) || strings.EqualFold(key, bare) {
			return key
		}
	}
	return ""
}

// focusDiagramArt emits the hub-and-rings diagram: top lanes above the
// hub, bottom lanes below, every lane's stubs merging into the centers of
// the next-inner lane (the hub column for the innermost ring). The
// innermost rings carry per-edge cardinality labels beside their cards.
func focusDiagramArt(top, bottom []diagramLane, hub []string, topLabels, bottomLabels []*connectorLabels) diagramArt {
	centerWidth := ansi.StringWidth(hub[0])
	left, right := -centerWidth/2, centerWidth-centerWidth/2
	for _, lane := range top {
		laneWidth := ansi.StringWidth(lane.rows[0])
		mid := laneMid(lane)
		left = min(left, -mid)
		right = max(right, laneWidth-mid)
	}
	for _, lane := range bottom {
		laneWidth := ansi.StringWidth(lane.rows[0])
		mid := laneMid(lane)
		left = min(left, -mid)
		right = max(right, laneWidth-mid)
	}
	width := right - left
	centerCol := -left

	art := diagramArt{}
	// Top side, outermost ring first: each lane's stubs merge into the
	// centers of the ring below it (top[0] is the innermost ring, merging
	// into the hub column).
	for level := len(top) - 1; level >= 0; level-- {
		centers := []int{centerCol}
		if level > 0 {
			inner := top[level-1]
			centers = shiftStubs(inner.stubs, centerCol-laneMid(inner))
		}
		labels := (*connectorLabels)(nil)
		if len(topLabels) > level {
			labels = topLabels[level]
		}
		appendLane(&art, top[level], centers, centerCol, width, true, labels)
	}
	hubShift := centerCol - centerWidth/2
	art.lines = append(art.lines, shiftRows(hub, hubShift, width)...)
	// Bottom side, innermost ring first: each lane merges from the ring
	// above, the innermost ring from the hub column.
	for level := 0; level < len(bottom); level++ {
		centers := []int{centerCol}
		if level > 0 {
			inner := bottom[level-1]
			centers = shiftStubs(inner.stubs, centerCol-laneMid(inner))
		}
		labels := (*connectorLabels)(nil)
		if len(bottomLabels) > level {
			labels = bottomLabels[level]
		}
		appendLane(&art, bottom[level], centers, centerCol, width, false, labels)
	}
	return art
}

// appendLane shifts one ring lane into the diagram and emits its merge
// row: after the lane rows on the top side (stubs reach down into the
// ring below), before them on the bottom side. labels, when non-nil,
// renders the level-0 cardinality rows: the child-end labels at the stub
// columns, an upward arrow glyph on the connector path, and the
// parent-end labels at the hub column (top) or the stub columns (bottom)
// — the parent (1) is always the lower table, so the arrow always points
// from (1) to (N).
func appendLane(art *diagramArt, lane diagramLane, centers []int, centerCol, width int, top bool, labels *connectorLabels) {
	shift := centerCol - laneMid(lane)
	stubs := shiftStubs(lane.stubs, shift)
	merge := func() {
		if len(stubs) > 1 {
			art.lines = append(art.lines, mergeRow(stubs, centers, width, top))
		} else if labels == nil {
			art.lines = append(art.lines, stubRow(stubs, width))
		}
	}
	childRow := func() {
		if labels != nil {
			art.lines = append(art.lines, connectorLabelRow(labels.child, stubs, width))
		}
	}
	arrowAndParent := func(columns []int) {
		if labels == nil {
			return
		}
		glyphRow := func(glyph rune) {
			line := []rune(strings.Repeat(" ", width))
			for _, column := range columns {
				if column >= 0 && column < width {
					line[column] = glyph
				}
			}
			art.lines = append(art.lines, string(line))
		}
		// Upward arrow from the parent (1) to the child (N): head at the
		// top, shaft running down to the parent-end labels.
		glyphRow('▲')
		glyphRow('│')
		art.lines = append(art.lines, connectorLabelRow(labels.parent, columns, width))
	}
	if !top {
		merge()
		childRow()
		arrowAndParent(stubs)
	}
	art.lines = append(art.lines, shiftRows(lane.rows, shift, width)...)
	if top {
		childRow()
		merge()
		arrowAndParent([]int{centerCol})
	}
}

// connectorLabelRow centers one cardinality label on each of the lane's
// stub columns, between the lane and its connector.
func connectorLabelRow(labels []string, stubs []int, width int) string {
	line := []rune(strings.Repeat(" ", width))
	for index, label := range labels {
		if label == "" || index >= len(stubs) {
			continue
		}
		runes := []rune(label)
		start := stubs[index] - len(runes)/2
		start = min(max(start, 0), width-len(runes))
		for offset, character := range runes {
			if start+offset >= 0 && start+offset < width {
				line[start+offset] = character
			}
		}
	}
	return string(line)
}

// laneMid returns the lane's midpoint column: the average of the
// outermost card centers.
func laneMid(lane diagramLane) int {
	return (lane.stubs[0] + lane.stubs[len(lane.stubs)-1]) / 2
}

// diagramLaneFor lays one ring level's cards out side by side and returns
// the rendered rows and each card's center column.
func diagramLaneFor(nodes []diagramNode) diagramLane {
	if len(nodes) == 0 {
		return diagramLane{}
	}
	cards := make([][]string, len(nodes))
	stubs := make([]int, len(nodes))
	width := 0
	for index, node := range nodes {
		cards[index] = boxCard(node.table, node.rows, false, 0, false)
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
	return diagramLane{rows: rows, stubs: stubs}
}

// relationshipNodes converts one ring level's edges into card nodes; the
// level-0 pairs double as the card's column list.
func relationshipNodes(edges []relationshipEdge) []diagramNode {
	nodes := make([]diagramNode, len(edges))
	for index, edge := range edges {
		nodes[index] = diagramNode{table: edge.table, rows: relationshipCardRows(edge)}
	}
	return nodes
}

// relationshipCardRows renders one neighbor card's body: the FK column
// mappings, or nothing for title-only rings.
func relationshipCardRows(edge relationshipEdge) []string {
	if len(edge.pairs) == 0 {
		return nil
	}
	return []string{strings.Join(edge.pairs, "; ")}
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

// mergeRow joins lane stubs (above for top, below for bottom) into the
// centers of the next-inner lane's cards: a horizontal bar spanning the
// outermost stubs and centers, corner glyphs where the outermost stubs
// meet the bar, up/down stems at interior stubs, and a stem at every
// center where the connector continues to the next ring.
func mergeRow(stubs, centers []int, width int, top bool) string {
	line := []rune(strings.Repeat(" ", width))
	left, right := min(stubs[0], centers[0]), max(stubs[len(stubs)-1], centers[len(centers)-1])
	for column := left; column <= right; column++ {
		if column >= 0 && column < width {
			line[column] = '─'
		}
	}
	cornerLeft, cornerRight, stubStem, centerStem := '└', '┘', '┴', '┬'
	if !top {
		cornerLeft, cornerRight, stubStem, centerStem = '┌', '┐', '┬', '┴'
	}
	centerSet := make(map[int]bool, len(centers))
	for _, center := range centers {
		centerSet[center] = true
	}
	for _, center := range centers {
		if center >= 0 && center < width {
			line[center] = centerStem
		}
	}
	for _, stub := range stubs {
		if stub < 0 || stub >= width || centerSet[stub] {
			continue
		}
		glyph := stubStem
		switch {
		case stub <= left:
			glyph = cornerLeft
		case stub >= right:
			glyph = cornerRight
		}
		line[stub] = glyph
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

// indexDiagramArt renders the indexes-tab focus diagram: the hub card and
// every ring card list the table's indexes, rings follow the foreign-key
// graph.
func (m Model) indexDiagramArt(snapshot Snapshot) diagramArt {
	top, bottom := m.relationshipRings(snapshot)
	hub := snapshot.SelectedTable
	hubRows := indexCardRows(m.indexesForKey(snapshot, hub))
	topLanes := make([]diagramLane, len(top))
	for level, edges := range top {
		topLanes[level] = diagramLaneFor(m.indexNodes(edges, snapshot))
	}
	bottomLanes := make([]diagramLane, len(bottom))
	for level, edges := range bottom {
		bottomLanes[level] = diagramLaneFor(m.indexNodes(edges, snapshot))
	}
	return focusDiagramArt(topLanes, bottomLanes, boxCard(hub, hubRows, true, 0, len(bottomLanes) > 0), nil, nil)
}

// IndexDiagramView renders the indexes-tab focus diagram, falling back to
// the flat table/index list when the diagram does not fit the workspace.
func (m Model) IndexDiagramView(layout uikit.Layout, snapshot Snapshot) string {
	art := m.indexDiagramArt(snapshot)
	if !diagramFits(art, layout) {
		return m.indexDiagramList(layout, snapshot) + "\n" + chrome.PaneStatus("", uikit.StatusStyle.Render("Press f for full-screen diagram"), layout.ViewportWidth)
	}
	return strings.Join(art.lines, "\n")
}

// indexDiagramList is the fit fallback for the indexes diagram: one line
// per ring table with its indexes.
func (m Model) indexDiagramList(layout uikit.Layout, snapshot Snapshot) string {
	lines := []string{uikit.HeaderStyle.Render(relationshipLine(snapshot.SelectedTable+" indexes", max(layout.ViewportWidth-2, 1)))}
	top, bottom := m.relationshipRings(snapshot)
	for _, level := range append(append([][]relationshipEdge{}, top...), bottom...) {
		for _, edge := range level {
			rows := indexCardRows(m.indexesForKey(snapshot, edge.table))
			line := edge.table
			if len(rows) > 0 {
				line += ": " + strings.Join(rows, "; ")
			}
			lines = append(lines, relationshipLine(line, layout.ViewportWidth))
		}
	}
	return strings.Join(lines[:min(len(lines), max(layout.Height-3, 0))], "\n")
}

// indexNodes converts one ring level's edges into index cards.
func (m Model) indexNodes(edges []relationshipEdge, snapshot Snapshot) []diagramNode {
	nodes := make([]diagramNode, len(edges))
	for index, edge := range edges {
		nodes[index] = diagramNode{table: edge.table, rows: indexCardRows(m.indexesForKey(snapshot, edge.table))}
	}
	return nodes
}

// indexesForKey returns the indexes of a diagram table: the whole-schema
// cache first, the selected table's own listing as fallback.
func (m Model) indexesForKey(snapshot Snapshot, table string) []sharedsql.IndexInfo {
	if len(snapshot.IndexesAll) > 0 {
		if key := diagramIndexKey(snapshot.IndexesAll, table); key != "" {
			return snapshot.IndexesAll[key]
		}
		return nil
	}
	if strings.EqualFold(table, snapshot.SelectedTable) {
		return m.Structure.IndexInfo
	}
	return nil
}

// indexCardRows renders a table's indexes as card rows: the primary key
// with a key glyph, unique indexes with a lock, regular indexes bare.
func indexCardRows(indexes []sharedsql.IndexInfo) []string {
	rows := make([]string, 0, len(indexes))
	for _, index := range indexes {
		prefix := ""
		switch {
		case index.PrimaryKey:
			prefix = "🔑 "
		case index.Unique:
			prefix = "🔒 "
		}
		row := prefix + index.Name
		if columns := strings.Join(index.Columns, ", "); columns != "" {
			row += " (" + columns + ")"
		}
		rows = append(rows, row)
	}
	return rows
}
