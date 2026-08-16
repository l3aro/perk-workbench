package app

import (
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// workspaceTabItem is one rendered workspace tab: a standard (built-in)
// tab, or a driver-advertised custom view. The zero custom view means a
// standard tab.
type workspaceTabItem struct {
	standard workspaceTab
	custom   sharedsql.CustomWorkspaceView
}

// standardWorkspaceTabItem builds the item for a standard tab.
func standardWorkspaceTabItem(tab workspaceTab) workspaceTabItem {
	return workspaceTabItem{standard: tab}
}

// workspaceTabs returns the workspace tab row for the active target:
// the standard tabs first — the legacy per-product policy without a
// workspace advertisement, the target-kind scope rules filtered by the
// explicit standard-tab advertisement with one — then the advertised
// custom views in order, filtered by their scopes. Query and Browse
// keep their scope policy in both paths.
func (m Model) workspaceTabs() []workspaceTabItem {
	tabs := make([]workspaceTabItem, 0, 9)
	for _, tab := range m.standardWorkspaceTabs() {
		tabs = append(tabs, standardWorkspaceTabItem(tab))
	}
	if m.workspace.advertised != nil {
		for _, view := range m.workspace.advertised.CustomViews {
			if workspaceViewApplies(view, m.WorkspaceTarget.Kind) {
				tabs = append(tabs, workspaceTabItem{custom: view})
			}
		}
	}
	return tabs
}

// standardWorkspaceTabs returns the standard tabs for the active target
// kind. Without a workspace advertisement the legacy per-product policy
// applies exactly (MongoDB scopes have no foreign keys, SQLite has no
// scope targets, and Diagram is a relational-database scope tab). With
// one, the target-kind scope rules apply uniformly and the advertised
// standard-tab set filters Columns/Indexes/Foreign Keys/Diagram; Query
// and Browse are never advertisement-gated.
func (m Model) standardWorkspaceTabs() []workspaceTab {
	if m.workspace.advertised == nil {
		return m.legacyWorkspaceTabs()
	}
	var scopeTabs []workspaceTab
	switch m.WorkspaceTarget.Kind {
	case core.WorkspaceTable:
		scopeTabs = []workspaceTab{tabQuery, tabBrowse, tabStructure, tabIndexes, tabForeignKeys}
	case core.WorkspaceDatabase, core.WorkspaceSchema:
		scopeTabs = []workspaceTab{tabQuery, tabBrowse, tabDiagram}
	default:
		return []workspaceTab{tabQuery}
	}
	filtered := make([]workspaceTab, 0, len(scopeTabs))
	for _, tab := range scopeTabs {
		if tab == tabQuery || tab == tabBrowse || m.advertisesStandardTab(tab) {
			filtered = append(filtered, tab)
		}
	}
	return filtered
}

// legacyWorkspaceTabs is the pre-advertisement tab policy: the query
// tab alone without a sidebar selection; the table tabs for a selected
// table; Browse plus Diagram for relational database/schema scopes.
// MongoDB scopes have no foreign keys, so their database target keeps
// Query + Browse; SQLite has no scope targets at all. Product switches
// live only here — a driver that advertises workspace metadata is
// filtered by the explicit advertisement instead.
func (m Model) legacyWorkspaceTabs() []workspaceTab {
	switch m.WorkspaceTarget.Kind {
	case core.WorkspaceTable:
		return []workspaceTab{tabQuery, tabBrowse, tabStructure, tabIndexes, tabForeignKeys}
	case core.WorkspaceDatabase:
		switch m.databaseInfo.Product {
		case "MongoDB":
			return []workspaceTab{tabQuery, tabBrowse}
		case "MySQL", "PostgreSQL":
			return []workspaceTab{tabQuery, tabBrowse, tabDiagram}
		}
	case core.WorkspaceSchema:
		if m.databaseInfo.Product == "PostgreSQL" {
			return []workspaceTab{tabQuery, tabBrowse, tabDiagram}
		}
	}
	return []workspaceTab{tabQuery}
}

// advertisesStandardTab reports whether the active advertisement
// includes the standard tab's key. Query and Browse are never
// advertisement-gated.
func (m Model) advertisesStandardTab(tab workspaceTab) bool {
	var key sharedsql.StandardWorkspaceTab
	switch tab {
	case tabStructure:
		key = sharedsql.StandardWorkspaceTabColumns
	case tabIndexes:
		key = sharedsql.StandardWorkspaceTabIndexes
	case tabForeignKeys:
		key = sharedsql.StandardWorkspaceTabForeignKeys
	case tabDiagram:
		key = sharedsql.StandardWorkspaceTabDiagram
	default:
		return true
	}
	for _, advertised := range m.workspace.advertised.StandardTabs {
		if advertised == key {
			return true
		}
	}
	return false
}

// workspaceTabLabel returns the rendered label for a workspace tab item;
// the query tab carries the active connection's editor language label
// ("SQL" for every SQL backend, "Command" once a plugin advertises it),
// and a custom view its advertised label.
func (m Model) workspaceTabLabel(item workspaceTabItem) string {
	if item.custom.ID != "" {
		return item.custom.Label
	}
	switch item.standard {
	case tabQuery:
		return m.editorLanguage().EditorLabel
	case tabBrowse:
		return "Browse"
	case tabStructure:
		return "Columns"
	case tabIndexes:
		return "Indexes"
	case tabForeignKeys:
		return "Foreign Keys"
	case tabDiagram:
		return "Diagram"
	}
	return ""
}

// workspaceTabMeta returns the rendered labels and widths of a workspace
// tab row in order; the widths match the rendered tab styles, so the click
// hit-test and the view stay in sync.
func (m Model) workspaceTabMeta(tabs []workspaceTabItem) (labels []string, widths []int) {
	labels = make([]string, len(tabs))
	widths = make([]int, len(tabs))
	for index, tab := range tabs {
		labels[index] = m.workspaceTabLabel(tab)
		widths[index] = lipgloss.Width(statusStyle.Render(labels[index]))
	}
	return labels, widths
}

// workspaceTabActive reports whether item is the current selection: the
// standard tab in m.Tab, or the custom view in the workspace-view state.
func (m Model) workspaceTabActive(item workspaceTabItem) bool {
	if item.custom.ID != "" {
		return m.Tab == tabCustom && m.workspace.active == item.custom.ID
	}
	return m.Tab == item.standard && m.workspace.active == ""
}

// activeWorkspaceTabItem returns the currently selected tab item,
// resolving the workspace-view state for a custom selection.
func (m Model) activeWorkspaceTabItem() workspaceTabItem {
	if m.Tab == tabCustom && m.workspace.active != "" {
		if view, ok := m.workspaceViewByID(m.workspace.active); ok {
			return workspaceTabItem{custom: view}
		}
	}
	return standardWorkspaceTabItem(m.Tab)
}

// sameWorkspaceTab reports whether two tab items are the same selection:
// same custom view id, or same standard tab.
func sameWorkspaceTab(a, b workspaceTabItem) bool {
	if a.custom.ID != b.custom.ID {
		return false
	}
	if a.custom.ID != "" {
		return true
	}
	return a.standard == b.standard
}

// cycleWorkspaceTab moves the selection one step forward or backward
// through the visible tabs, wrapping at both ends. A tab that is not in
// the policy list (stale from a target change) clamps to the first visible
// tab; forward moves to the first, backward to the last.
func cycleWorkspaceTab(tabs []workspaceTabItem, current workspaceTabItem, forward bool) workspaceTabItem {
	index := -1
	for i, tab := range tabs {
		if sameWorkspaceTab(tab, current) {
			index = i
			break
		}
	}
	if index < 0 {
		if !forward {
			return tabs[len(tabs)-1]
		}
		return tabs[0]
	}
	if forward {
		return tabs[(index+1)%len(tabs)]
	}
	return tabs[(index+len(tabs)-1)%len(tabs)]
}
