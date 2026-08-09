package workbench

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

// treeAnimFrame is the tick rate of the sidebar accordion animation; it
// matches the spring's FPS so the simulation runs at real time.
const treeAnimFrame = time.Second / 60

// treeAnimMaxTicks bounds the animation (4s at 60fps) so a stalled spring
// can never keep the tick loop alive forever.
const treeAnimMaxTicks = 240

// treeAnimTickMsg advances the sidebar accordion animation by one frame.
type treeAnimTickMsg struct{}

// treeAnim drives the accordion reveal of a toggled subtree: the child rows
// of database (all of them, or just schema's tables) appear top-to-bottom
// when expanding and disappear bottom-to-top when collapsing. The spring is
// stateless, so the animation keeps the position and velocity itself.
type treeAnim struct {
	spring     harmonica.Spring
	value      float64 // spring position, 0..1 (may overshoot slightly)
	velocity   float64
	ticks      int
	database   string
	schema     string // "" animates the whole database subtree
	collapsing bool
	total      int // child rows in the animated subtree
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
	m.treeAnim = &treeAnim{
		spring:     harmonica.NewSpring(harmonica.FPS(60), 12.6, 0.8),
		database:   database,
		schema:     schema,
		collapsing: !expanding,
		total:      total,
	}
	return tea.Tick(treeAnimFrame, func(time.Time) tea.Msg { return treeAnimTickMsg{} })
}

func (m Model) armTreeAnim() tea.Cmd {
	return tea.Tick(treeAnimFrame, func(time.Time) tea.Msg { return treeAnimTickMsg{} })
}

// updateTreeAnim advances the accordion by one spring step and rebuilds the
// tree with the revealed rows; the animation ends at the first frame that
// shows (or hides) every child row, before any overshoot can bounce rows.
func (m Model) updateTreeAnim(treeAnimTickMsg) (tea.Model, tea.Cmd) {
	anim := m.treeAnim
	if anim == nil {
		return m, nil
	}
	if anim.ticks >= treeAnimMaxTicks {
		m.treeAnim = nil
		return m, m.rebuildSchemaTree()
	}
	anim.ticks++
	anim.value, anim.velocity = anim.spring.Update(anim.value, anim.velocity, 1)
	if anim.complete() {
		m.treeAnim = nil
		return m, m.rebuildSchemaTree()
	}
	return m, tea.Batch(m.rebuildSchemaTree(), m.armTreeAnim())
}

// complete reports whether the reveal reached its final state: every child
// row shown when expanding, none when collapsing.
func (a *treeAnim) complete() bool {
	if a.collapsing {
		return a.revealedRows() == 0
	}
	return a.revealedRows() == a.total
}

// revealedRows returns how many child rows of the animated subtree may
// render this frame.
func (a *treeAnim) revealedRows() int {
	value := math.Min(1, math.Max(0, a.value))
	shown := int(value * float64(a.total))
	if a.collapsing {
		return a.total - shown
	}
	return shown
}

// schemaReveal reports the animated subtree and how many of its child rows
// may render this frame; revealed is -1 when no animation is running.
func (m Model) schemaReveal() (database, schema string, revealed int) {
	anim := m.treeAnim
	if anim == nil {
		return "", "", -1
	}
	return anim.database, anim.schema, anim.revealedRows()
}

// schemaChildRowCount returns the number of rows the accordion will reveal
// or hide when database (or just schema's tables) is toggled: the
// post-toggle state when expanding, the pre-toggle state when collapsing,
// mirroring what the rebuild renders at the first animation frame.
func (m Model) schemaChildRowCount(database, schema string, expanding bool) int {
	dbExpanded := m.expandedDatabases[database] || expanding
	count := 0
	for _, object := range m.schemaObjects {
		switch object.Type {
		case "schema":
			if schema == "" && object.Database == database && dbExpanded {
				count++
			}
		case "table", "view":
			if object.Database != database {
				continue
			}
			if m.databaseInfo.Product == "PostgreSQL" {
				objSchema, _, found := strings.Cut(object.Name, ".")
				if !found {
					continue
				}
				expanded := m.expandedSchemas[m.schemaExpansionKey(database, objSchema)]
				if expanding && schema == objSchema {
					expanded = true // toggleSchema expands exactly this one
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
