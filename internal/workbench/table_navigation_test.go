package workbench

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestResults_cellNavigation_movesColumns_and_revealsSelection(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT first, second, third, fourth, fifth, sixth, seventh, eighth", result: sqlite.Result{
		Columns: []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth"},
		Rows: [][]*string{{
			stringPointer(strings.Repeat("first ", 20)),
			stringPointer("second"),
			stringPointer("third"),
			stringPointer("fourth"),
			stringPointer("fifth"),
			stringPointer("sixth"),
			stringPointer("seventh"),
			stringPointer("eighth"),
		}},
	}})
	model = updated.(Model)
	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)

	// Then
	if got, want := model.resultsColumn, 3; got != want {
		t.Fatalf("selected result column = %d, want %d", got, want)
	}
	if got, want := model.results.Cursor(), 0; got != want {
		t.Fatalf("result cursor = %d, want %d after F5 and Right", got, want)
	}
	if model.resultsOffset == 0 {
		t.Fatalf("right-selected column was not revealed: columns=%#v tableWidth=%d viewportWidth=%d", model.results.Columns(), model.results.Width(), model.tableViewportWidth)
	}
	resultLines := strings.Split(tableViewportViewWithAlignment(model.results, model.resultsNumericColumns, model.resultsOffset, model.tableViewportWidth, model.resultsColumn), "\n")
	if !strings.Contains(resultLines[1], "48;2;85;214;190") {
		t.Fatal("selected result cell is not visibly styled after F5 and Right")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updated.(Model)
	// Then
	if got, want := model.resultsColumn, 0; got != want {
		t.Fatalf("selected result column = %d, want %d", got, want)
	}
	if got := model.resultsOffset; got != 0 {
		t.Fatalf("results offset = %d, want 0 after selecting the first column", got)
	}
}

func TestTableCellNavigation_consumesMotionKeys_atColumnEdges(t *testing.T) {
	// Given
	resultTable := newResultsTable()
	resultTable.SetColumns([]table.Column{{Title: "first", Width: 5}, {Title: "second", Width: 6}})
	selectedColumn, offset := 0, 0

	// When
	consumed := moveTableCell(&resultTable, &selectedColumn, &offset, 10, tea.KeyPressMsg{Code: tea.KeyLeft})

	// Then
	if !consumed {
		t.Fatal("left cell-motion key was not consumed at the first column")
	}
	if selectedColumn != 0 || offset != 0 {
		t.Fatalf("edge navigation changed selection: column=%d offset=%d", selectedColumn, offset)
	}

	// Given
	selectedColumn = 1

	// When
	consumed = moveTableCell(&resultTable, &selectedColumn, &offset, 10, tea.KeyPressMsg{Code: 'l', Text: "l"})

	// Then
	if !consumed {
		t.Fatal("right cell-motion key was not consumed at the last column")
	}
	if got, want := selectedColumn, 1; got != want {
		t.Fatalf("selected column = %d, want %d", got, want)
	}
}

func TestTableCellNavigation_movesRows_withFixedKeys(t *testing.T) {
	// Given
	resultTable := newResultsTable()
	resultTable.SetColumns([]table.Column{{Title: "first", Width: 5}})
	resultTable.SetRows([]table.Row{{"one"}, {"two"}})
	selectedColumn, offset := 0, 0

	// When
	consumed := moveTableCell(&resultTable, &selectedColumn, &offset, 10, tea.KeyPressMsg{Code: 'j', Text: "j"})

	// Then
	if !consumed {
		t.Fatal("down cell-motion key was not consumed")
	}
	if got, want := resultTable.Cursor(), 1; got != want {
		t.Fatalf("row cursor = %d, want %d", got, want)
	}

	// When
	consumed = moveTableCell(&resultTable, &selectedColumn, &offset, 10, tea.KeyPressMsg{Code: tea.KeyUp})

	// Then
	if !consumed {
		t.Fatal("up cell-motion key was not consumed")
	}
	if got := resultTable.Cursor(); got != 0 {
		t.Fatalf("row cursor = %d, want 0", got)
	}
}

func TestResults_cellNavigation_doesNotInterceptSQLInsertMode(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.results.SetColumns([]table.Column{{Title: "first", Width: 50}, {Title: "second", Width: 6}})
	model.results.SetRows([]table.Row{{"first", "second"}})
	model.results.Focus()
	model.editor.setValue("SELECT ")
	model.formMode.beginInsert(model.editor)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got := model.resultsColumn; got != 0 {
		t.Fatalf("selected result column = %d, want 0 while SQL editing", got)
	}
	if got, want := model.editor.value, "SELECT l"; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
}

func TestResize_doesNotRecomputeColumnWidths_whenViewportWidthIsUnchanged(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.results.SetColumns([]table.Column{{Title: "ID", Width: 2}})
	model.results.SetRows([]table.Row{{strings.Repeat("value", 20)}})

	// When
	model = resizeModel(model, 100, 25)

	// Then
	if got, want := model.results.Columns()[0].Width, 2; got != want {
		t.Fatalf("result column width = %d, want %d without a viewport width change", got, want)
	}
}

func TestTableViewport_selectedCellOverridesSelectedRowStyle(t *testing.T) {
	// Given
	resultTable := newResultsTable()
	resultTable.SetColumns([]table.Column{{Title: "first", Width: 5}, {Title: "second", Width: 5}})
	resultTable.SetRows([]table.Row{{"1", "22"}})
	resizeResultsTable(&resultTable, 14, 2)
	resultTable.SetCursor(0)

	// When
	lines := strings.Split(tableViewportViewWithAlignment(resultTable, []bool{false, true}, 0, 14, 1), "\n")

	// Then
	if got, want := ansi.Strip(lines[1]), " 1         22 "; got != want {
		t.Fatalf("selected numeric row = %q, want %q", got, want)
	}
	if !strings.Contains(lines[1], "48;2;28;40;56") {
		t.Fatalf("selected row does not retain its stripe background: %q", lines[1])
	}
	if !strings.Contains(lines[1], "48;2;85;214;190") {
		t.Fatal("selected cell does not override the selected-row background")
	}
}

func TestTableCellNavigation_consumesMotionKeys_forEmptyResultsPlaceholder(t *testing.T) {
	// Given
	resultTable := newResultsTable()
	selectedColumn, offset := 0, 0

	// When
	consumed := moveTableCell(&resultTable, &selectedColumn, &offset, 10, tea.KeyPressMsg{Code: tea.KeyRight})

	// Then
	if !consumed {
		t.Fatal("cell-motion key was not consumed for the empty results placeholder")
	}
	if selectedColumn != 0 || offset != 0 {
		t.Fatalf("empty results navigation changed selection: column=%d offset=%d", selectedColumn, offset)
	}
}

func TestQueryLog_mouseClick_selectsClickedCell(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.appendQueryLog(queryLogEntry{statement: "SELECT first"})
	model.appendQueryLog(queryLogEntry{statement: "SELECT second"})
	columns := model.queryLog.Columns()
	clickX := model.schemaWidth + 1
	for _, column := range columns[:2] {
		clickX += column.Width + 2*spaceCompact
	}
	clickY := model.workspaceHeight + 5

	// When
	updated, _ := model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then
	if got, want := model.Focus, focusQueryLog; got != want {
		t.Fatalf("focus = %v, want %v", got, want)
	}
	if got, want := model.queryLog.Cursor(), 1; got != want {
		t.Fatalf("query log cursor = %d, want %d", got, want)
	}
	if got, want := model.queryLogColumn, 2; got != want {
		t.Fatalf("query log column = %d, want %d", got, want)
	}
}

func TestQueryLog_mouseRelease_selectsClickedCell(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.appendQueryLog(queryLogEntry{statement: "SELECT first"})
	model.appendQueryLog(queryLogEntry{statement: "SELECT second"})
	columns := model.queryLog.Columns()
	clickX := model.schemaWidth + 1
	for _, column := range columns[:2] {
		clickX += column.Width + 2*spaceCompact
	}
	clickY := model.workspaceHeight + 5

	// When
	updated, _ := model.Update(tea.MouseReleaseMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then
	if got, want := model.Focus, focusQueryLog; got != want {
		t.Fatalf("focus = %v, want %v", got, want)
	}
	if got, want := model.queryLog.Cursor(), 1; got != want {
		t.Fatalf("query log cursor = %d, want %d", got, want)
	}
	if got, want := model.queryLogColumn, 2; got != want {
		t.Fatalf("query log column = %d, want %d", got, want)
	}
}
