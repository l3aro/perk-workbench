package workbench

import "strings"

// tableTargetChoices are the workspace tabs a table selection can land on,
// in picker display order.
var tableTargetChoices = []workspaceTab{tabStructure, tabBrowse, tabSQL, tabIndexes, tabForeignKeys}

// tableTargetKey returns the config.json value for a workspace tab.
func tableTargetKey(tab workspaceTab) string {
	switch tab {
	case tabStructure:
		return "structure"
	case tabBrowse:
		return "browse"
	case tabSQL:
		return "sql"
	case tabIndexes:
		return "indexes"
	case tabForeignKeys:
		return "foreign_keys"
	}
	return ""
}

// tableTargetName returns the display label for a workspace tab.
func tableTargetName(tab workspaceTab) string {
	switch tab {
	case tabStructure:
		return "Structure"
	case tabBrowse:
		return "Browse"
	case tabSQL:
		return "SQL"
	case tabIndexes:
		return "Indexes"
	case tabForeignKeys:
		return "Foreign Keys"
	}
	return ""
}

type tableTargetPicker struct {
	original workspaceTab
	selected int
}

func newTableTargetPicker() *tableTargetPicker {
	picker := &tableTargetPicker{original: tableOpenTargetTab()}
	for i, tab := range tableTargetChoices {
		if tab == picker.original {
			picker.selected = i
			break
		}
	}
	return picker
}

func (p *tableTargetPicker) tab() workspaceTab {
	return tableTargetChoices[p.selected]
}

func (p *tableTargetPicker) move(delta int) {
	p.selected = max(0, min(p.selected+delta, len(tableTargetChoices)-1))
}

func (p *tableTargetPicker) content() string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Open Table → "))
	content.WriteString("\n\n")
	for i, tab := range tableTargetChoices {
		prefix := "  "
		label := tableTargetName(tab)
		if i == p.selected {
			prefix = "> "
			label = selectedItemStyle.Render(label)
		}
		content.WriteString(prefix + label + "\n")
	}
	content.WriteString("\n")
	content.WriteString(mutedStyle.Render(" j/k or arrows select | enter set | esc cancel"))
	return content.String()
}
