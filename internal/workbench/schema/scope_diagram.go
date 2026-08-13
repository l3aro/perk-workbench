package schema

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// ScopeDiagramView renders the hub-less relationship diagram for the
// database/schema scope target of the snapshot: every in-scope table/view
// is a card and only edges between two in-scope tables are drawn, with
// the FK cardinality labels from the index cache. The diagram falls back
// to a flat relationship list when it does not fit the workspace. Absent
// caches render the loading state and an object-less scope the empty
// state, instead of guessing edges.
func (m Model) ScopeDiagramView(layout uikit.Layout, snapshot Snapshot) string {
	if snapshot.ForeignKeysAll == nil || snapshot.IndexesAll == nil {
		return scopeDiagramStatus(layout, "loading schema…")
	}
	names, nodes, edges, levels := m.scopeDiagramGraph(snapshot)
	if len(names) == 0 {
		return scopeDiagramStatus(layout, "no tables in scope")
	}
	lanes := make([]diagramLane, len(levels))
	for levelIndex, level := range levels {
		cards := make([]diagramNode, len(level))
		for index, name := range level {
			cards[index] = nodes[name]
		}
		lanes[levelIndex] = diagramLaneFor(cards)
	}
	labels := make([]*connectorLabels, len(levels))
	parentEdges := make([][]bool, len(levels))
	for levelIndex := 1; levelIndex < len(levels); levelIndex++ {
		labels[levelIndex], parentEdges[levelIndex] = scopeLaneLabels(edges, levels[levelIndex], levels[levelIndex-1])
	}
	art := scopeDiagramArt(lanes, labels, parentEdges)
	if !diagramFits(art, layout) {
		return m.scopeDiagramList(layout, snapshot, names, edges) + "\n" + chrome.PaneStatus("", uikit.StatusStyle.Render("Press f for full-screen diagram"), layout.ViewportWidth)
	}
	return strings.Join(art.lines, "\n")
}

// scopeDiagramStatus renders the Diagram tab's loading and empty states:
// the scope renderer never guesses edges while the connection-level
// caches are absent.
func scopeDiagramStatus(layout uikit.Layout, message string) string {
	return chrome.PaneStatus("", uikit.StatusStyle.Render(message), layout.ViewportWidth)
}

// scopeDiagramObjects returns the in-scope table/view objects of the
// snapshot's workspace target, in sidebar order. Scope membership mirrors
// the browse object list: MySQL database scopes take the database's own
// tables/views, PostgreSQL database scopes every loaded table/view (all
// belong to the connected database), and PostgreSQL schema scopes the
// schema-qualified names. ok is false when the target has no scope diagram
// (SQLite/MongoDB/table targets).
func (m Model) scopeDiagramObjects(snapshot Snapshot) (objects []sharedsql.SchemaObject, inScope map[string]bool, ok bool) {
	var prefix string
	switch snapshot.WorkspaceTarget.Kind {
	case core.WorkspaceDatabase:
		switch snapshot.Database.Product {
		case "MySQL":
			prefix = snapshot.WorkspaceTarget.Database + "."
		case "PostgreSQL":
			// Every loaded table/view belongs to the connected database.
		default:
			return nil, nil, false
		}
	case core.WorkspaceSchema:
		if snapshot.Database.Product != "PostgreSQL" {
			return nil, nil, false
		}
		prefix = snapshot.WorkspaceTarget.Schema + "."
	default:
		return nil, nil, false
	}
	inScope = map[string]bool{}
	for _, object := range m.Objects {
		if object.Type != "table" && object.Type != "view" {
			continue
		}
		if snapshot.Database.Product == "PostgreSQL" &&
			object.Database != snapshot.WorkspaceTarget.Database {
			continue
		}
		name := object.Name
		if snapshot.Database.Product == "MySQL" {
			name = object.Database + "." + object.Name
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		objects = append(objects, object)
		inScope[strings.ToLower(name)] = true
	}
	return objects, inScope, true
}

// scopeEdge is one relationship between two in-scope cards of the scope
// diagram: the referenced table and every foreign key connecting the
// declaring table to it. unique is the edge-level FK uniqueness (1:1 only
// when every FK between the two tables is unique); uniqueOK is false when
// the index cache cannot answer.
type scopeEdge struct {
	table    string
	pairs    []string
	unique   bool
	uniqueOK bool
}

// scopeDiagramGraph builds the scope diagram's cards and internal edges:
// every in-scope table/view is a card, and an edge exists only when both
// the declaring and the referenced table are in scope — outside
// references are ignored rather than drawn as stubs. Levels order the
// lanes: a table sits one level below its highest referenced table, so
// children render beneath their parents. Cycles clamp to the deepest
// level so they never stretch the diagram.
func (m Model) scopeDiagramGraph(snapshot Snapshot) (names []string, nodes map[string]diagramNode, edges map[string][]scopeEdge, levels [][]string) {
	objects, inScope, ok := m.scopeDiagramObjects(snapshot)
	if !ok || len(objects) == 0 {
		return nil, nil, nil, nil
	}
	names = make([]string, len(objects))
	nodes = make(map[string]diagramNode, len(objects))
	merged := make(map[string]map[string]*scopeEdge, len(objects))
	selfRows := make(map[string][]string)
	for index, object := range objects {
		from := object.Name
		if snapshot.Database.Product == "MySQL" {
			from = object.Database + "." + object.Name
		}
		names[index] = from
		merged[from] = map[string]*scopeEdge{}
		for _, foreignKey := range snapshot.ForeignKeysAll[from] {
			to := foreignKey.ReferenceTable
			if strings.EqualFold(to, from) {
				selfRows[from] = append(selfRows[from], "↺ "+relationshipPairLabel(foreignKey, "", false))
				continue
			}
			if !inScope[strings.ToLower(to)] {
				continue // outside the scope: no edge, no stub
			}
			targetKey := strings.ToLower(to)
			edge, exists := merged[from][targetKey]
			if !exists {
				edge = &scopeEdge{table: to, unique: true}
				merged[from][targetKey] = edge
			}
			edge.pairs = appendUnique(edge.pairs, []string{relationshipPairLabel(foreignKey, to, false)})
			unique, answerable := fkCardinality(snapshot.IndexesAll, from, foreignKey.Columns)
			edge.uniqueOK = edge.uniqueOK || answerable
			edge.unique = edge.unique && unique
		}
	}
	edges = make(map[string][]scopeEdge, len(objects))
	for _, from := range names {
		targets := make([]string, 0, len(merged[from]))
		for key := range merged[from] {
			targets = append(targets, key)
		}
		sort.Strings(targets)
		rows := append([]string{}, selfRows[from]...)
		fromEdges := make([]scopeEdge, 0, len(targets))
		for _, key := range targets {
			edge := *merged[from][key]
			fromEdges = append(fromEdges, edge)
			rows = append(rows, edge.pairs...)
		}
		edges[from] = fromEdges
		nodes[from] = diagramNode{table: from, rows: rows}
	}
	// Longest-path layering: level[from] = 1 + the highest level of the
	// tables it references. Tables without internal references stay at
	// level 0. The bound of len(names)-1 keeps reference cycles from
	// stretching the diagram; they clamp into the deepest lane.
	level := make(map[string]int, len(names))
	for range names {
		changed := false
		for _, from := range names {
			for _, edge := range edges[from] {
				if next := level[edge.table] + 1; next > level[from] {
					level[from] = next
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	byLevel := map[int][]string{}
	for _, from := range names {
		level[from] = min(level[from], len(names)-1)
		byLevel[level[from]] = append(byLevel[level[from]], from)
	}
	levelKeys := make([]int, 0, len(byLevel))
	for key := range byLevel {
		levelKeys = append(levelKeys, key)
	}
	sort.Ints(levelKeys)
	for _, key := range levelKeys {
		sort.Strings(byLevel[key])
		levels = append(levels, byLevel[key])
	}
	return names, nodes, edges, levels
}

// scopeLaneLabels builds the cardinality labels for one scope lane pair:
// the child end renders at each child card's stub column — the first
// edge's label when the card fans out to several parents in the lane
// above — and the parent end at every referenced parent's center column.
// The second result marks the parents an edge from this lane actually
// reaches, so the connector, arrow, and labels render only there.
// Skip-level edges (a parent more than one lane up) carry no labels on
// this pair. The labels are nil when the index cache cannot answer any
// edge (matching the focus-diagram convention); the reachability marks
// still come back so the connector renders without label rows.
func scopeLaneLabels(edges map[string][]scopeEdge, children, parents []string) (*connectorLabels, []bool) {
	parentIndex := make(map[string]int, len(parents))
	for index, parent := range parents {
		parentIndex[strings.ToLower(parent)] = index
	}
	labels := &connectorLabels{
		child:  make([]string, len(children)),
		parent: make([]string, len(parents)),
	}
	parentEdges := make([]bool, len(parents))
	answerable := true
	for childIndex, child := range children {
		for _, edge := range edges[child] {
			parentAt, adjacent := parentIndex[strings.ToLower(edge.table)]
			if !adjacent {
				continue
			}
			// The connector always renders for an adjacent edge; only the
			// label text is suppressed when the cache cannot answer.
			parentEdges[parentAt] = true
			if !edge.uniqueOK {
				answerable = false
				continue
			}
			if labels.child[childIndex] == "" {
				labels.child[childIndex] = "(N)"
				if edge.unique {
					labels.child[childIndex] = "(1)"
				}
			}
			labels.parent[parentAt] = "(1)"
		}
	}
	if !answerable {
		return nil, parentEdges
	}
	return labels, parentEdges
}

// scopeDiagramArt lays the scope lanes out top-down: level 0 at the top,
// every deeper level merging into the lane above it. The merge rows and
// cardinality labels mirror the focus-diagram rendering (appendLane's
// top-side placement): the child labels between the child lane and the
// merge bar, the upward arrow and parent labels at the parent centers.
// Only parents an edge actually reaches connect: an edge-less neighbor
// card in the parent lane gets no stub, arrow, or label.
func scopeDiagramArt(lanes []diagramLane, labels []*connectorLabels, parentEdges [][]bool) diagramArt {
	left, right := 0, 0
	for _, lane := range lanes {
		laneWidth := ansi.StringWidth(lane.rows[0])
		mid := laneMid(lane)
		left = min(left, -mid)
		right = max(right, laneWidth-mid)
	}
	width := right - left
	centerCol := -left
	art := diagramArt{}
	for level := len(lanes) - 1; level >= 0; level-- {
		shift := centerCol - laneMid(lanes[level])
		stubs := shiftStubs(lanes[level].stubs, shift)
		art.lines = append(art.lines, shiftRows(lanes[level].rows, shift, width)...)
		if level == 0 {
			continue
		}
		laneLabels := labels[level]
		parentStubs := shiftStubs(lanes[level-1].stubs, centerCol-laneMid(lanes[level-1]))
		centers := make([]int, 0, len(parentStubs))
		parentLabelRow := make([]string, 0, len(parentStubs))
		for index, stub := range parentStubs {
			if index >= len(parentEdges[level]) || !parentEdges[level][index] {
				continue
			}
			centers = append(centers, stub)
			if laneLabels != nil {
				parentLabelRow = append(parentLabelRow, laneLabels.parent[index])
			}
		}
		if len(centers) == 0 {
			continue // no edge between these lanes: no connector
		}
		if laneLabels != nil {
			art.lines = append(art.lines, connectorLabelRow(laneLabels.child, stubs, width))
		}
		if len(stubs) > 1 {
			art.lines = append(art.lines, mergeRow(stubs, centers, width, true))
		} else if laneLabels == nil {
			art.lines = append(art.lines, stubRow(stubs, width))
		}
		if laneLabels != nil {
			// Upward arrow from the parent (1) to the child (N) at the
			// parent centers, mirroring the focus diagram's label rows.
			for _, glyph := range []rune{'▲', '│'} {
				line := []rune(strings.Repeat(" ", width))
				for _, column := range centers {
					if column >= 0 && column < width {
						line[column] = glyph
					}
				}
				art.lines = append(art.lines, string(line))
			}
			art.lines = append(art.lines, connectorLabelRow(parentLabelRow, centers, width))
		}
	}
	return art
}

// scopeDiagramList is the fit fallback for the scope diagram: one line
// per internal relationship, in sidebar order.
func (m Model) scopeDiagramList(layout uikit.Layout, snapshot Snapshot, names []string, edges map[string][]scopeEdge) string {
	title := snapshot.WorkspaceTarget.Database
	if snapshot.WorkspaceTarget.Kind == core.WorkspaceSchema {
		title = snapshot.WorkspaceTarget.Schema
	}
	lines := []string{uikit.HeaderStyle.Render(relationshipLine(title+" relationships", max(layout.ViewportWidth-2, 1)))}
	for _, from := range names {
		for _, edge := range edges[from] {
			lines = append(lines, relationshipLine(from+" → "+edge.table, layout.ViewportWidth))
		}
	}
	return strings.Join(lines[:min(len(lines), max(layout.Height-3, 0))], "\n")
}
