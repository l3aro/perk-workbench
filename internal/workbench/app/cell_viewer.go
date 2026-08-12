package app

import (
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// cellViewer is the shared cell-view overlay widget, owned by the UI
// contract layer so the browse pane and the notification history modal
// draw identically.
type cellViewer = uikit.CellViewer

// rawCellValue returns the untruncated value for the selected cell, falling
// back to the table's display value when raw data is unavailable.
func (m *Model) rawCellValue(tableType string, row, col int, displayValue string) string {
	var source [][]*string
	switch tableType {
	case "browse":
		source = m.browse.component.Result.UntruncatedRows
	case "results":
		source = m.queryLog.resultsRaw
	}
	if row >= 0 && row < len(source) && col >= 0 && col < len(source[row]) {
		if cell := source[row][col]; cell != nil {
			return *cell
		}
		return "NULL"
	}
	return displayValue
}

// openCellViewer creates a cell viewer for the selected cell in the given
// result table. Returns nil if there is no selection or column is out of
// range.
func (m *Model) openCellViewer(resultTable table.Model, selectedColumn int, rawValue string) tea.Cmd {
	row := resultTable.Cursor()
	if row < 0 || row >= len(resultTable.Rows()) {
		return nil
	}
	columns := resultTable.Columns()
	if selectedColumn < 0 || selectedColumn >= len(columns) {
		return nil
	}

	columnTitle := columns[selectedColumn].Title

	m.browse.component.CellViewer = uikit.NewCellViewer(columnTitle, rawValue, max(m.layout.width-8, 1), max(m.layout.height-10, 1))
	return nil
}
