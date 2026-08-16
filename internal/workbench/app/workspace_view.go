package app

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// workspaceViewLoadedMsg delivers one custom workspace view load. The
// message carries the request identity (view id, structured target) and
// the load tag so a stale reply — superseded by a newer load, a target
// change, or a connection change — is dropped without touching state.
type workspaceViewLoadedMsg struct {
	viewID string
	target sharedsql.WorkspaceViewTarget
	tag    uint64
	result sharedsql.Result
	err    error
}

// workspaceViewApplies reports whether a custom view serves the active
// target kind: the advertised scopes decide the tab's visibility.
func workspaceViewApplies(view sharedsql.CustomWorkspaceView, kind core.WorkspaceTargetKind) bool {
	var want sharedsql.WorkspaceViewKind
	switch kind {
	case core.WorkspaceTable:
		want = sharedsql.WorkspaceViewTable
	case core.WorkspaceDatabase:
		want = sharedsql.WorkspaceViewDatabase
	case core.WorkspaceSchema:
		want = sharedsql.WorkspaceViewSchema
	default:
		return false
	}
	for _, scope := range view.Scopes {
		if scope == want {
			return true
		}
	}
	return false
}

// workspaceViewTarget maps the active workspace target to the shared
// structured target of a view request, carrying the scope identifiers
// preserved at selection time (a table's database and schema). ok=false
// when no target is active — custom views are scoped, so no load ever
// starts then.
func (m Model) workspaceViewTarget() (sharedsql.WorkspaceViewTarget, bool) {
	switch m.WorkspaceTarget.Kind {
	case core.WorkspaceTable:
		return sharedsql.WorkspaceViewTarget{
			Kind:     sharedsql.WorkspaceViewTable,
			Database: m.WorkspaceTarget.Database,
			Schema:   m.WorkspaceTarget.Schema,
			Table:    m.WorkspaceTarget.Table,
		}, true
	case core.WorkspaceDatabase:
		return sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewDatabase, Database: m.WorkspaceTarget.Database}, true
	case core.WorkspaceSchema:
		return sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewSchema, Database: m.WorkspaceTarget.Database, Schema: m.WorkspaceTarget.Schema}, true
	}
	return sharedsql.WorkspaceViewTarget{}, false
}

// workspaceViewByID resolves an advertised custom view by id.
func (m Model) workspaceViewByID(id string) (sharedsql.CustomWorkspaceView, bool) {
	if m.workspace.advertised == nil {
		return sharedsql.CustomWorkspaceView{}, false
	}
	for _, view := range m.workspace.advertised.CustomViews {
		if view.ID == id {
			return view, true
		}
	}
	return sharedsql.CustomWorkspaceView{}, false
}

// activeWorkspaceViewLabel returns the rendered label of the active
// custom view; "" when no custom view is active.
func (m Model) activeWorkspaceViewLabel() string {
	view, ok := m.workspaceViewByID(m.workspace.active)
	if !ok {
		return ""
	}
	return view.Label
}

// resetWorkspaceView drops the workspace-view selection and loaded data
// for a target or connection change: the in-flight request is canceled,
// the selection falls back to the standard tab row, and the loaded
// result is cleared so a stale table never renders for a new scope. The
// advertisement itself is kept — only updateOpen replaces it.
func (m *Model) resetWorkspaceView() {
	m.cancelWorkspaceViewLoad()
	m.workspace.active = ""
	m.workspace.loading = false
	m.workspace.err = nil
	m.workspace.result = sharedsql.Result{}
	m.workspace.status = ""
	m.workspace.table.SetRows(nil)
	m.workspace.table.SetColumns([]table.Column{{Title: "Results", Width: 1}})
	m.workspace.numericColumns = nil
	m.workspace.selectedColumn = 0
	m.workspace.offset = 0
	m.workspace.tag++
	if m.Tab == tabCustom {
		m.Tab = tabQuery
	}
}

// cancelWorkspaceViewLoad cancels the in-flight view request, if any.
// Cancellation is idempotent: the loaded message (if it ever arrives)
// carries a stale tag and is dropped.
func (m *Model) cancelWorkspaceViewLoad() {
	if m.workspace.cancel != nil {
		m.workspace.cancel()
		m.workspace.cancel = nil
	}
}

// selectWorkspaceTab activates one rendered tab item: a standard tab
// switches m.Tab and refreshes the pending per-tab loads; a custom view
// becomes the active view and loads lazily for the active structured
// target. Selecting the already-active item is a no-op.
func (m *Model) selectWorkspaceTab(item workspaceTabItem) tea.Cmd {
	if item.custom.ID != "" {
		if m.workspace.active == item.custom.ID && m.Tab == tabCustom {
			return nil
		}
		m.cancelWorkspaceViewLoad()
		m.Tab = tabCustom
		m.workspace.active = item.custom.ID
		m.workspace.loading = true
		m.workspace.err = nil
		m.workspace.status = ""
		m.workspace.tag++
		m.workspace.table.Focus()
		return m.startWorkspaceViewLoad(item.custom.ID)
	}
	if m.workspace.active == "" && m.Tab == item.standard {
		return nil
	}
	m.cancelWorkspaceViewLoad()
	m.workspace.active = ""
	m.Tab = item.standard
	m.focusActiveTable()
	return tea.Batch(m.loadPendingBrowse(), m.loadPendingDiagram())
}

// startWorkspaceViewLoad launches the async view request for the active
// custom view and the active structured target. The caller has already
// bumped the staleness tag and set loading; the returned command owns
// the request context, so its cancellation reaches the driver as a
// perk/v1/cancel like any other session call. A missing provider (a
// driver that advertises custom views without implementing them) fails
// deterministically into the view's error state.
func (m *Model) startWorkspaceViewLoad(viewID string) tea.Cmd {
	target, ok := m.workspaceViewTarget()
	if !ok {
		m.workspace.loading = false
		return nil
	}
	provider, ok := m.Database.(sharedsql.WorkspaceViewProvider)
	if !ok {
		m.workspace.loading = false
		m.workspace.err = fmt.Errorf("custom view %q is advertised but not implemented", viewID)
		return nil
	}
	ctx, cancel := context.WithCancel(m.appContext)
	m.workspace.cancel = cancel
	tag := m.workspace.tag
	return func() tea.Msg {
		defer cancel()
		result, err := provider.WorkspaceView(ctx, sharedsql.WorkspaceViewRequest{ViewID: viewID, Target: target})
		return workspaceViewLoadedMsg{viewID: viewID, target: target, tag: tag, result: result, err: err}
	}
}

// reloadWorkspaceView reloads the active custom view for the active
// target, dropping the loaded result's state until the reply lands.
func (m *Model) reloadWorkspaceView() tea.Cmd {
	if m.Tab != tabCustom || m.workspace.active == "" {
		return nil
	}
	m.cancelWorkspaceViewLoad()
	m.workspace.loading = true
	m.workspace.err = nil
	m.workspace.tag++
	return m.startWorkspaceViewLoad(m.workspace.active)
}

// updateWorkspaceView applies one view load reply. A stale reply — a
// tag from a superseded load, a view that is no longer active, or a
// target that changed while the request was in flight — is dropped
// without touching state.
func (m Model) updateWorkspaceView(message workspaceViewLoadedMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.workspace.tag || message.viewID != m.workspace.active || m.Tab != tabCustom {
		return m, nil
	}
	target, ok := m.workspaceViewTarget()
	if !ok || target != message.target {
		return m, nil
	}
	m.workspace.cancel = nil
	m.workspace.loading = false
	if message.err != nil {
		m.workspace.err = message.err
		m.workspace.status = ""
		m.setStatus(safeText(pluginFailureStatus(message.err, fmt.Sprintf("loading view: %v", message.err))))
		return m, nil
	}
	m.workspace.err = nil
	m.setWorkspaceViewResult(message.result)
	return m, nil
}

// workspaceViewHasData reports whether the loaded result carries a
// renderable table (at least one column).
func (m Model) workspaceViewHasData() bool {
	return len(m.workspace.result.Columns) > 0
}

// setWorkspaceViewResult applies a successful view load to the shared
// results table: safe-text cells capped at the display rune bound, the
// table sized to the workspace body, and the status line summarizing the
// rows and duration.
func (m *Model) setWorkspaceViewResult(result sharedsql.Result) {
	m.workspace.result = result
	m.workspace.numericColumns = numericColumns(result.ColumnTypes)
	titles := make([]string, len(result.Columns))
	for index, column := range result.Columns {
		titles[index] = safeText(column)
	}
	rows := make([]table.Row, len(result.Rows))
	for rowIndex, row := range result.Rows {
		cells := make(table.Row, len(row))
		for cellIndex, cell := range row {
			if cell == nil {
				cells[cellIndex] = "NULL"
			} else {
				cells[cellIndex] = cellText(*cell)
			}
		}
		rows[rowIndex] = cells
	}
	m.workspace.table.SetRows(nil)
	m.workspace.table.SetColumns(tableColumns(titles, rows))
	resizeResultsTable(&m.workspace.table, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-7, 2))
	m.workspace.table.SetRows(rows)
	m.workspace.table.SetCursor(0)
	m.workspace.table.Focus()
	m.workspace.selectedColumn, m.workspace.offset = 0, 0
	m.workspace.status = fmt.Sprintf("%d rows | %s", len(rows), result.Duration)
	if result.Truncated {
		m.workspace.status += " | truncated"
	}
	m.workspace.status += colsHint(m.workspace.table.Columns(), m.layout.tableViewportWidth)
}
