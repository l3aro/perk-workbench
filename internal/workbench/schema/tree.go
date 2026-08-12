package schema

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

// TreeAnimFrame is the tick rate of the sidebar accordion animation; it
// matches the spring's FPS so the simulation runs at real time.
const TreeAnimFrame = time.Second / 60

// TreeAnimMaxTicks bounds the animation (4s at 60fps) so a stalled spring
// can never keep the tick loop alive forever.
const TreeAnimMaxTicks = 240

// TreeAnimTickMsg advances the sidebar accordion animation by one frame.
type TreeAnimTickMsg struct{}

// Anim drives the accordion reveal of a toggled subtree: the child rows
// of database (all of them, or just schema's tables) appear top-to-bottom
// when expanding and disappear bottom-to-top when collapsing. The spring is
// stateless, so the animation keeps the position and velocity itself.
type Anim struct {
	Spring     harmonica.Spring
	Value      float64 // spring position, 0..1 (may overshoot slightly)
	Velocity   float64
	Ticks      int
	Database   string
	Schema     string // "" animates the whole database subtree
	Collapsing bool
	Total      int // child rows in the animated subtree
}

// treeToggleCmd batches the accordion tick with the tree rebuild, dropping
// the tick when there is nothing to animate.
func treeToggleCmd(anim, rebuild tea.Cmd) tea.Cmd {
	if anim == nil {
		return rebuild
	}
	return tea.Batch(anim, rebuild)
}

// startTreeAnim launches the accordion animation for a subtree toggle over
// total child rows. It returns the first frame tick, or nil when the
// subtree has no child rows.
func (m *Model) startTreeAnim(database, schema string, expanding bool, total int) tea.Cmd {
	if total == 0 {
		return nil
	}
	// Angular frequency 2π/0.5s ≈ 12.6 with a 0.8 damping ratio settles in
	// about half a second with a barely visible overshoot.
	m.Anim = &Anim{
		Spring:     harmonica.NewSpring(harmonica.FPS(60), 12.6, 0.8),
		Database:   database,
		Schema:     schema,
		Collapsing: !expanding,
		Total:      total,
	}
	return tea.Tick(TreeAnimFrame, func(time.Time) tea.Msg { return TreeAnimTickMsg{} })
}

func (m Model) armTreeAnim() tea.Cmd {
	return tea.Tick(TreeAnimFrame, func(time.Time) tea.Msg { return TreeAnimTickMsg{} })
}

// UpdateTreeAnim advances the accordion by one spring step and rebuilds the
// tree with the revealed rows; the animation ends at the first frame that
// shows (or hides) every child row, before any overshoot can bounce rows.
func (m Model) UpdateTreeAnim(message TreeAnimTickMsg, snapshot Snapshot) (Model, tea.Cmd) {
	anim := m.Anim
	if anim == nil {
		return m, nil
	}
	if anim.Ticks >= TreeAnimMaxTicks {
		m.Anim = nil
		return m, m.RebuildTree(snapshot)
	}
	anim.Ticks++
	anim.Value, anim.Velocity = anim.Spring.Update(anim.Value, anim.Velocity, 1)
	if anim.Complete() {
		m.Anim = nil
		return m, m.RebuildTree(snapshot)
	}
	return m, tea.Batch(m.RebuildTree(snapshot), m.armTreeAnim())
}

// Complete reports whether the reveal reached its final state: every child
// row shown when expanding, none when collapsing.
func (a *Anim) Complete() bool {
	if a.Collapsing {
		return a.RevealedRows() == 0
	}
	return a.RevealedRows() == a.Total
}

// RevealedRows returns how many child rows of the animated subtree may
// render this frame.
func (a *Anim) RevealedRows() int {
	value := math.Min(1, math.Max(0, a.Value))
	shown := int(value * float64(a.Total))
	if a.Collapsing {
		return a.Total - shown
	}
	return shown
}

// SchemaReveal reports the animated subtree and how many of its child rows
// may render this frame; revealed is -1 when no animation is running.
func (m Model) SchemaReveal() (database, schema string, revealed int) {
	anim := m.Anim
	if anim == nil {
		return "", "", -1
	}
	return anim.Database, anim.Schema, anim.RevealedRows()
}

// schemaChildRowCount returns the number of rows the accordion will reveal
// or hide when database (or just schema's tables) is toggled: the
// post-toggle state when expanding, the pre-toggle state when collapsing,
// mirroring what the rebuild renders at the first animation frame.
func (m Model) schemaChildRowCount(database, schema string, expanding bool, snapshot Snapshot) int {
	dbExpanded := m.ExpandedDatabases[database] || expanding
	count := 0
	for _, object := range m.Objects {
		switch object.Type {
		case "schema":
			if schema == "" && object.Database == database && dbExpanded {
				count++
			}
		case "table", "view":
			if object.Database != database {
				continue
			}
			if snapshot.Database.Product == "PostgreSQL" {
				objSchema, _, found := strings.Cut(object.Name, ".")
				if !found {
					continue
				}
				expanded := m.ExpandedSchemas[m.schemaExpansionKey(database, objSchema)]
				if expanding && schema == objSchema {
					expanded = true // ToggleSchema expands exactly this one
				}
				if schema == "" {
					if dbExpanded && expanded {
						count++
					}
				} else if objSchema == schema && expanded {
					count++
				}
			} else if schema == "" && dbExpanded {
				count++
			}
		}
	}
	return count
}
