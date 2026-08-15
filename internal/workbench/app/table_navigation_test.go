package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

// renderedRowY returns the screen Y of the first rendered line containing
// needle, or fails the test.
func renderedRowY(t *testing.T, model Model, needle string) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	for y, line := range lines {
		if strings.Contains(line, needle) {
			return y
		}
	}
	t.Fatalf("rendered view does not contain %q", needle)
	return -1
}

// accentBgRGB returns the ANSI truecolor background sequence for the current
// theme's selection accent, so assertions track palette changes.
func accentBgRGB() string {
	var red, green, blue uint8
	fmt.Sscanf(colorPrimary, "#%02x%02x%02x", &red, &green, &blue)
	return fmt.Sprintf("48;2;%d;%d;%d", red, green, blue)
}

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
	if got, want := model.layout.resultsColumn, 3; got != want {
		t.Fatalf("selected result column = %d, want %d", got, want)
	}
	if got, want := model.queryLog.results.Cursor(), 0; got != want {
		t.Fatalf("result cursor = %d, want %d after F5 and Right", got, want)
	}
	if model.layout.resultsOffset == 0 {
		t.Fatalf("right-selected column was not revealed: columns=%#v tableWidth=%d viewportWidth=%d", model.queryLog.results.Columns(), model.queryLog.results.Width(), model.layout.tableViewportWidth)
	}
	resultLines := strings.Split(tableViewportViewWithAlignment(model.queryLog.results, model.queryLog.resultsNumericColumns, model.layout.resultsOffset, model.layout.tableViewportWidth, model.layout.resultsColumn), "\n")
	if !strings.Contains(resultLines[1], accentBgRGB()) {
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
	if got, want := model.layout.resultsColumn, 0; got != want {
		t.Fatalf("selected result column = %d, want %d", got, want)
	}
	if got := model.layout.resultsOffset; got != 0 {
		t.Fatalf("results offset = %d, want 0 after selecting the first column", got)
	}
}

func TestTableCellNavigation_wideColumn_revealsItsHead(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"id", "payload"},
		Rows:    [][]*string{{stringPointer("1"), stringPointer(strings.Repeat("x", 300))}},
	}})
	model = updated.(Model)
	if got, want := model.queryLog.results.Columns()[1].Width, model.layout.tableViewportWidth; got <= want {
		t.Fatalf("payload column width = %d, viewport = %d; want a column wider than the viewport", got, want)
	}

	// When: move right onto the wide column.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)

	// Then: the viewport aligns with the column start, showing its head
	// rather than pinning the viewport to the column's tail.
	if got, want := model.layout.resultsOffset, model.queryLog.results.Columns()[0].Width+2*spaceCompact; got != want {
		t.Fatalf("offset = %d, want %d (wide column aligned at its start)", got, want)
	}
	line := strings.Split(tableViewportView(model.queryLog.results, model.layout.resultsOffset, model.layout.tableViewportWidth), "\n")[1]
	if stripped := ansi.Strip(line); !strings.HasPrefix(stripped, " xxxx") {
		t.Fatalf("visible line = %q, want the head of the wide cell", stripped)
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

func TestRowTables_scrollWithoutCellSelection(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Model)
		table  func(*Model) *table.Model
		column func(*Model) *int
		offset func(*Model) *int
		view   func(Model) string
	}{
		{
			name:   "columns",
			setup:  func(m *Model) { m.Tab = tabStructure },
			table:  func(m *Model) *table.Model { return &m.schema.component.Structure.Table },
			column: func(m *Model) *int { return &m.layout.structureColumn },
			offset: func(m *Model) *int { return &m.layout.structureOffset },
			view:   func(m Model) string { return m.structureView() },
		},
		{
			name:   "indexes",
			setup:  func(m *Model) { m.Tab = tabIndexes },
			table:  func(m *Model) *table.Model { return &m.schema.component.Structure.Indexes },
			column: func(m *Model) *int { return &m.layout.indexesColumn },
			offset: func(m *Model) *int { return &m.layout.indexesOffset },
			view:   func(m Model) string { return m.indexesView() },
		},
		{
			name:   "foreign keys",
			setup:  func(m *Model) { m.Tab = tabForeignKeys },
			table:  func(m *Model) *table.Model { return &m.schema.component.Structure.ForeignKeys },
			column: func(m *Model) *int { return &m.layout.foreignKeysColumn },
			offset: func(m *Model) *int { return &m.layout.foreignKeysOffset },
			view:   func(m Model) string { return m.foreignKeysView() },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := resizeModel(readyModel(t), 40, 24)
			model.Focus = focusWorkspace
			test.setup(&model)
			resultTable := test.table(&model)
			resultTable.SetColumns([]table.Column{{Title: "first", Width: 30}, {Title: "second", Width: 30}})
			resultTable.SetRows([]table.Row{{strings.Repeat("one", 20), strings.Repeat("two", 20)}})
			resizeResultsTable(resultTable, model.layout.tableViewportWidth, 2)
			*test.column(&model) = 1
			model.focusActiveTable()

			updated, _ := model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
			model = updated.(Model)

			if got := *test.offset(&model); got == 0 {
				t.Fatal("right row-motion did not scroll the table")
			}
			if got := *test.column(&model); got != 1 {
				t.Fatalf("selected column = %d, want unchanged", got)
			}
			model = resizeModel(model, 42, 24)
			if got := *test.offset(&model); got == 0 {
				t.Fatal("resize reset the row table's horizontal scroll")
			}
			body := strings.Split(test.view(model), "\n")[1]
			if strings.Contains(body, accentBgRGB()) {
				t.Fatalf("row view rendered a selected cell: %q", body)
			}
		})
	}
}

func TestResults_cellNavigation_doesNotInterceptSQLInsertMode(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.queryLog.results.SetColumns([]table.Column{{Title: "first", Width: 50}, {Title: "second", Width: 6}})
	model.queryLog.results.SetRows([]table.Row{{"first", "second"}})
	model.queryLog.results.Focus()
	model.queryLog.editor.setValue("SELECT ")
	beginInsert(model.overlay.formMode, model.queryLog.editor)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got := model.layout.resultsColumn; got != 0 {
		t.Fatalf("selected result column = %d, want 0 while SQL editing", got)
	}
	if got, want := model.queryLog.editor.value, "SELECT l"; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
}

func TestResize_doesNotRecomputeColumnWidths_whenViewportWidthIsUnchanged(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.queryLog.results.SetColumns([]table.Column{{Title: "ID", Width: 2}})
	model.queryLog.results.SetRows([]table.Row{{strings.Repeat("value", 20)}})

	// When
	model = resizeModel(model, 100, 25)

	// Then
	if got, want := model.queryLog.results.Columns()[0].Width, 2; got != want {
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
	if !strings.Contains(lines[1], accentBgRGB()) {
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
	model.appendQueryLog(queryLogEntry{Statement: "SELECT first"})
	model.appendQueryLog(queryLogEntry{Statement: "SELECT second"})
	columns := model.queryLog.component.Table.Columns()
	clickX := model.layout.schemaWidth + 1
	for _, column := range columns[:2] {
		clickX += column.Width + 2*spaceCompact
	}
	clickY := renderedRowY(t, model, "SELECT second")

	// When
	updated, _ := model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then
	if got, want := model.Focus, focusQueryLog; got != want {
		t.Fatalf("focus = %v, want %v", got, want)
	}
	if got, want := model.queryLog.component.Table.Cursor(), 0; got != want {
		t.Fatalf("query log cursor = %d, want %d", got, want)
	}
	if got, want := model.queryLog.component.Column, 2; got != want {
		t.Fatalf("query log column = %d, want %d", got, want)
	}
}

func TestQueryLog_mouseRelease_selectsClickedCell(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)
	model.appendQueryLog(queryLogEntry{Statement: "SELECT first"})
	model.appendQueryLog(queryLogEntry{Statement: "SELECT second"})
	columns := model.queryLog.component.Table.Columns()
	clickX := model.layout.schemaWidth + 1
	for _, column := range columns[:2] {
		clickX += column.Width + 2*spaceCompact
	}
	clickY := renderedRowY(t, model, "SELECT second")

	// When
	updated, _ := model.Update(tea.MouseReleaseMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then
	if got, want := model.Focus, focusQueryLog; got != want {
		t.Fatalf("focus = %v, want %v", got, want)
	}
	if got, want := model.queryLog.component.Table.Cursor(), 0; got != want {
		t.Fatalf("query log cursor = %d, want %d", got, want)
	}
	if got, want := model.queryLog.component.Column, 2; got != want {
		t.Fatalf("query log column = %d, want %d", got, want)
	}
}

func TestSchemaTable_mouseClickUsesRenderedRow(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
	updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
		{Name: "idx_name", Columns: []string{"name"}},
		{Name: "idx_unique_rendered_target", Columns: []string{"category"}},
	}})
	model = updated.(Model)

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	clickY := -1
	for y, line := range lines {
		if strings.Contains(line, "idx_unique_rendered_target") {
			clickY = y
			break
		}
	}
	if clickY < 0 {
		t.Fatal("rendered indexes table does not contain target row")
	}

	updated, _ = model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 10,
		Y:      clickY,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if got := model.schema.component.Structure.Indexes.Cursor(); got != 1 {
		t.Fatalf("clicked rendered row selected cursor %d, want 1", got)
	}
}

func TestBrowse_mouseClickUsesRenderedRow(t *testing.T) {
	// Tall enough that the two fixture rows fit in the browse viewport,
	// which yields a row to the pager button row.
	model := resizeModel(readyModel(t), 100, 30)
	model.SelectedTable, model.Tab, model.Focus = "items", tabBrowse, focusWorkspace
	updated, _ := model.Update(browseTableMsg{
		table: "items",
		page:  0,
		result: sqlite.Result{
			Columns: []string{"name"},
			Rows: [][]*string{
				{stringPointer("first-rendered-row")},
				{stringPointer("second-unique-rendered-row")},
			},
		},
	})
	model = updated.(Model)

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	clickY := -1
	for y, line := range lines {
		if strings.Contains(line, "second-unique-rendered-row") {
			clickY = y
			break
		}
	}
	if clickY < 0 {
		t.Fatal("rendered browse table does not contain target row")
	}

	updated, _ = model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 10,
		Y:      clickY,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if got := model.browse.component.Table.Cursor(); got != 1 {
		t.Fatalf("clicked rendered row selected cursor %d, want 1", got)
	}
}

func TestSchemaTable_mouseClick_selectsRow(t *testing.T) {
	// Schema metadata tables: Columns (structure), Indexes, Foreign Keys.
	// All share the same coordinate-to-row mapping; Indexes is representative.
	t.Run("indexes click on first data row selects that row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		// Provide 2 index rows so the table is populated.
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
			{Name: "idx_category", Columns: []string{"category"}},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 5 // contentY=4 → tableLine=1 → first data row

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Indexes.Cursor(); got != 0 {
			t.Fatalf("indexes cursor on first row = %d, want 0", got)
		}
	})

	t.Run("indexes click on second data row selects that row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
			{Name: "idx_category", Columns: []string{"category"}},
			{Name: "idx_code", Columns: []string{"code"}},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 6 // contentY=5 → tableLine=2 → second data row

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Indexes.Cursor(); got != 1 {
			t.Fatalf("indexes cursor on second row = %d, want 1", got)
		}
	})

	t.Run("indexes click on visible row after scroll selects correct row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		// >10 rows so cursor at end scrolls start > 0.
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "i00", Columns: []string{"c0"}},
			{Name: "i01", Columns: []string{"c1"}},
			{Name: "i02", Columns: []string{"c2"}},
			{Name: "i03", Columns: []string{"c3"}},
			{Name: "i04", Columns: []string{"c4"}},
			{Name: "i05", Columns: []string{"c5"}},
			{Name: "i06", Columns: []string{"c6"}},
			{Name: "i07", Columns: []string{"c7"}},
			{Name: "i08", Columns: []string{"c8"}},
			{Name: "i09", Columns: []string{"c9"}},
		}})
		model = updated.(Model)
		// Scroll near end.
		rows := model.schema.component.Structure.Indexes.Rows()
		model.schema.component.Structure.Indexes.SetCursor(len(rows) - 1)
		// Compute expected first visible row from the handler's own formula.
		h := model.schema.component.Structure.Indexes.Height()
		want := min(max(model.schema.component.Structure.Indexes.Cursor()-h+1, 0), max(len(rows)-h, 0))
		if want <= 0 {
			t.Fatalf("test setup: want=%d should be >0 for a scrolled assertion", want)
		}
		clickX := model.layout.schemaWidth + 10
		clickY := 5 // first visible line

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Indexes.Cursor(); got != want {
			t.Fatalf("scrolled first-line cursor = %d, want %d", got, want)
		}
	})

	t.Run("indexes click on header does not change cursor", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
			{Name: "idx_category", Columns: []string{"category"}},
		}})
		model = updated.(Model)
		model.schema.component.Structure.Indexes.SetCursor(0)
		clickX := model.layout.schemaWidth + 10
		clickY := 4 // contentY=3 → tableLine=0 → header

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Indexes.Cursor(); got != 0 {
			t.Fatalf("header click changed cursor to %d, want 0", got)
		}
	})

	t.Run("indexes click outside X bounds does not change cursor", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
		}})
		model = updated.(Model)
		model.schema.component.Structure.Indexes.SetCursor(0)
		// Right of table content: workspaceX >= tableViewportWidth.
		clickX := model.layout.schemaWidth + 1 + model.layout.tableViewportWidth + 5
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Indexes.Cursor(); got != 0 {
			t.Fatalf("out-of-bounds X changed cursor to %d, want 0", got)
		}
	})

	t.Run("indexes click with form active does not select row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
		}})
		model = updated.(Model)
		model = openIndexEditor(t, model, &sharedsql.IndexInfo{Name: "items_name", Columns: []string{"name"}})
		model.schema.component.Structure.Indexes.SetCursor(0)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if !model.schema.component.Structure.IndexForm.Active() {
			t.Fatal("index form deactivated after click")
		}
		if got := model.schema.component.Structure.Indexes.Cursor(); got != 0 {
			t.Fatalf("form-active click changed cursor to %d, want 0", got)
		}
	})

	t.Run("structure click on data row selects that row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabStructure, focusWorkspace
		updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT", Nullable: true},
			{Name: "category", Type: "TEXT"},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Table.Cursor(); got != 0 {
			t.Fatalf("structure cursor = %d, want 0", got)
		}
	})

	t.Run("structure click with form active does not select row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab = "items", tabStructure
		updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{
			{Name: "name", Type: "TEXT", Nullable: true},
		}})
		model = updated.(Model)
		// Open column form.
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)
		_ = model.schema.component.Structure.ColumnForm.Form.Init()
		model.schema.component.Structure.Table.SetCursor(0)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if !model.schema.component.Structure.ColumnForm.Active() {
			t.Fatal("column form deactivated after click")
		}
	})

	t.Run("foreign keys click on data row selects that row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "children", tabForeignKeys, focusWorkspace
		updated, _ := model.Update(foreignKeysLoadedMsg{table: "children", foreignKeys: []sharedsql.ForeignKeyInfo{
			{ID: "fk1", Columns: []string{"parent_id"}, ReferenceTable: "parents", ReferenceColumns: []string{"id"}, OnDelete: "CASCADE", OnUpdate: "NO ACTION"},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.ForeignKeys.Cursor(); got != 0 {
			t.Fatalf("foreign keys cursor = %d, want 0", got)
		}
	})

	t.Run("foreign keys click with relationship diagram does not select row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "children", tabForeignKeys, focusWorkspace
		model.schema.component.Structure.RelationshipDiagram = true
		updated, _ := model.Update(foreignKeysLoadedMsg{table: "children", foreignKeys: []sharedsql.ForeignKeyInfo{
			{ID: "fk1", Columns: []string{"parent_id"}, ReferenceTable: "parents", ReferenceColumns: []string{"id"}, OnDelete: "CASCADE", OnUpdate: "NO ACTION"},
			{ID: "fk2", Columns: []string{"code"}, ReferenceTable: "parents", ReferenceColumns: []string{"code"}},
		}})
		model = updated.(Model)
		model.schema.component.Structure.ForeignKeys.SetCursor(1)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.ForeignKeys.Cursor(); got != 1 {
			t.Fatalf("diagram-mode click changed cursor to %d, want 1", got)
		}
	})

	t.Run("foreign keys click with form active does not select row", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "children", tabForeignKeys, focusWorkspace
		updated, _ := model.Update(foreignKeysLoadedMsg{table: "children", foreignKeys: []sharedsql.ForeignKeyInfo{
			{ID: "fk1", Columns: []string{"parent_id"}, ReferenceTable: "parents", ReferenceColumns: []string{"id"}},
		}})
		model = updated.(Model)
		// Open FK form.
		form := schema.NewForeignKeyForm(nil)
		form.SetKeys(DefaultKeybindings())
		model.schema.component.Structure.ForeignKeyForm = form
		_ = model.schema.component.Structure.ForeignKeyForm.Form.Init()
		model.schema.component.Structure.ForeignKeys.SetCursor(0)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if !model.schema.component.Structure.ForeignKeyForm.Active() {
			t.Fatal("FK form deactivated after click")
		}
	})

	t.Run("click below table rows does not change cursor", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
		}})
		model = updated.(Model)
		model.schema.component.Structure.Indexes.SetCursor(0)
		// Y in the query log area below the workspace.
		clickX := model.layout.schemaWidth + 10
		clickY := model.layout.workspaceHeight + 10

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if got := model.schema.component.Structure.Indexes.Cursor(); got != 0 {
			t.Fatalf("Y out-of-bounds changed cursor to %d, want 0", got)
		}
	})
}

func TestSchemaTable_doubleClick_opensEditForm(t *testing.T) {
	t.Run("indexes double click opens index edit form", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
			{Name: "idx_category", Columns: []string{"category"}},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 6 // second data row

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if !model.schema.component.Structure.IndexForm.Active() {
			t.Fatal("double click did not open index edit form")
		}
		if got := model.schema.component.Structure.IndexForm.Values.Name; got != "idx_category" {
			t.Fatalf("index form name = %q, want idx_category", got)
		}
	})

	t.Run("structure double click opens column edit form", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabStructure, focusWorkspace
		updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT", Nullable: true},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 6 // second data row

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if !model.schema.component.Structure.ColumnForm.Active() {
			t.Fatal("double click did not open column edit form")
		}
		if got := model.schema.component.Structure.ColumnForm.Values.Name; got != "name" {
			t.Fatalf("column form name = %q, want name", got)
		}
	})

	t.Run("foreign keys double click opens foreign key edit form", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "children", tabForeignKeys, focusWorkspace
		updated, _ := model.Update(foreignKeysLoadedMsg{table: "children", foreignKeys: []sharedsql.ForeignKeyInfo{
			{ID: "fk1", Columns: []string{"parent_id"}, ReferenceTable: "parents", ReferenceColumns: []string{"id"}, OnDelete: "CASCADE", OnUpdate: "NO ACTION"},
			{ID: "fk2", Columns: []string{"code"}, ReferenceTable: "parents", ReferenceColumns: []string{"code"}},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 6 // second data row

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if !model.schema.component.Structure.ForeignKeyForm.Active() {
			t.Fatal("double click did not open foreign key edit form")
		}
		if got := model.schema.component.Structure.ForeignKeyForm.Values.ReferenceTable; got != "parents" {
			t.Fatalf("foreign key form reference table = %q, want parents", got)
		}
	})

	t.Run("single click does not open edit form", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 5

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		if model.schema.component.Structure.IndexForm.Active() {
			t.Fatal("single click opened index edit form")
		}
	})

	t.Run("first click on another tab at same position stays selection-only", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabStructure, focusWorkspace
		updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{
			{Name: "id", Type: "INTEGER", PrimaryKey: 1},
			{Name: "name", Type: "TEXT", Nullable: true},
		}})
		model = updated.(Model)
		updated, _ = model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{
			{Name: "idx_name", Columns: []string{"name"}},
			{Name: "idx_category", Columns: []string{"category"}},
		}})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 6 // second data row

		// Single click a Columns row, then switch to the Indexes tab while the
		// click state is still within the double-click window.
		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		if model.schema.component.Structure.ColumnForm.Active() {
			t.Fatal("setup: single click opened column form")
		}
		model.Tab = tabIndexes

		// First click on Indexes at the same position must only select.
		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		if model.schema.component.Structure.IndexForm.Active() {
			t.Fatal("first click after tab switch opened index edit form")
		}
		if got := model.schema.component.Structure.Indexes.Cursor(); got != 1 {
			t.Fatalf("cross-tab click selected cursor %d, want 1", got)
		}

		// Second click on the same row is a genuine double-click.
		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		if !model.schema.component.Structure.IndexForm.Active() {
			t.Fatal("second click after tab switch did not open index edit form")
		}
	})

	t.Run("click on shifted row at same position stays selection-only", func(t *testing.T) {
		model := resizeModel(readyModel(t), 100, 24)
		model.SelectedTable, model.Tab, model.Focus = "items", tabIndexes, focusWorkspace
		rows := make([]sharedsql.IndexInfo, 15)
		for i := range rows {
			rows[i] = sharedsql.IndexInfo{Name: fmt.Sprintf("i%02d", i), Columns: []string{"c"}}
		}
		updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: rows})
		model = updated.(Model)
		clickX := model.layout.schemaWidth + 10
		clickY := 6 // second visible data row

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)

		// Scroll so the same Y now maps to a different row.
		model.schema.component.Structure.Indexes.SetCursor(len(rows) - 1)
		start := min(max(model.schema.component.Structure.Indexes.Cursor()-model.schema.component.Structure.Indexes.Height()+1, 0), max(len(rows)-model.schema.component.Structure.Indexes.Height(), 0))
		if start == 0 {
			t.Fatal("test setup: expected scrolled start > 0")
		}

		updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		if model.schema.component.Structure.IndexForm.Active() {
			t.Fatal("click on different row opened index edit form")
		}
		if got := model.schema.component.Structure.Indexes.Cursor(); got != start+1 {
			t.Fatalf("shifted click selected cursor %d, want %d", got, start+1)
		}
	})
}
