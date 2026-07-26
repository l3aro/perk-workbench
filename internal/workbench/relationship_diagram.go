package workbench

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

func (m Model) relationshipView() string {
	diagram := m.relationshipDiagramView()
	if lipgloss.Width(diagram) > m.tableViewportWidth || strings.Count(diagram, "\n")+1 > max(m.workspaceHeight-2, 1) {
		return m.relationshipListView() + "\n" + chrome.PaneStatus("", statusStyle.Render("Press f for full-screen diagram"), m.tableViewportWidth)
	}
	return diagram
}

func (m Model) relationshipDiagramView() string {
	lines := []string{}
	for _, foreignKey := range m.foreignKeyInfo {
		lines = append(lines, relationshipCard(m.SelectedTable))
		if strings.EqualFold(foreignKey.ReferenceTable, m.SelectedTable) {
			lines = append(lines, statusStyle.Render("↺"))
			continue
		}
		lines = append(lines, statusStyle.Render("│"), statusStyle.Render("▼"), relationshipCard(foreignKey.ReferenceTable))
	}
	for _, foreignKey := range m.referencingForeignKeyInfo {
		if strings.EqualFold(foreignKey.Table, m.SelectedTable) {
			continue
		}
		lines = append(lines, relationshipCard(foreignKey.Table), statusStyle.Render("│"), statusStyle.Render("▼"), relationshipCard(m.SelectedTable))
	}
	if len(lines) == 0 {
		return relationshipCard(m.SelectedTable)
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

func relationshipCard(name string) string {
	lines := []string{name}
	width := 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	box := make([]string, len(lines)+2)
	box[0] = "┌" + strings.Repeat("─", width+2) + "┐"
	for index, line := range lines {
		box[index+1] = "│ " + line + strings.Repeat(" ", width-ansi.StringWidth(line)) + " │"
	}
	box[len(box)-1] = "└" + strings.Repeat("─", width+2) + "┘"
	return strings.Join(box, "\n")
}
