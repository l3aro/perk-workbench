package app

import (
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/core"
)

// workspaceTabs returns the workspace tab row for the active target: the
// query tab alone without a sidebar selection; the table tabs for a
// selected table; Browse plus Diagram for relational database/schema
// scopes. MongoDB scopes have no foreign keys, so their database target
// keeps Query + Browse; SQLite has no scope targets at all.
func (m Model) workspaceTabs() []workspaceTab {
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

// workspaceTabLabel returns the rendered label for a workspace tab; the
// query tab carries the active connection's editor language label ("SQL"
// for every SQL backend, "Command" once a plugin advertises it).
func (m Model) workspaceTabLabel(tab workspaceTab) string {
	switch tab {
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
func (m Model) workspaceTabMeta(tabs []workspaceTab) (labels []string, widths []int) {
	labels = make([]string, len(tabs))
	widths = make([]int, len(tabs))
	for index, tab := range tabs {
		labels[index] = m.workspaceTabLabel(tab)
		widths[index] = lipgloss.Width(statusStyle.Render(labels[index]))
	}
	return labels, widths
}

// cycleWorkspaceTab moves the selection one step forward or backward
// through the visible tabs, wrapping at both ends. A tab that is not in
// the policy list (stale from a target change) clamps to the first visible
// tab; forward moves to the first, backward to the last.
func cycleWorkspaceTab(tabs []workspaceTab, current workspaceTab, forward bool) workspaceTab {
	index := -1
	for i, tab := range tabs {
		if tab == current {
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
