package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestBrowsePager_clickNextLoadsFollowingPage(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wide: the status line stays on one row
	model.browse.component.Result.HasMore = true
	model.focusActiveTable()

	pager := model.browsePager()
	if pager.PrevEnabled || !pager.NextEnabled {
		t.Fatalf("enabled = %t/%t, want only Next at page 0 with HasMore", pager.PrevEnabled, pager.NextEnabled)
	}

	// When — click the Next button on the button row (screen row
	// Height()+7: contentY = Height()+6 plus the header row).
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 1 + pager.NextStart,
		Y:      model.browse.component.Table.Height() + 7,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command == nil {
		t.Fatal("clicking Next returned no command")
	}
	model = driveCommand(model, command)

	// Then — the debounced load advanced to page 1.
	if model.BrowsePage != 1 {
		t.Fatalf("page = %d, want 1", model.BrowsePage)
	}
	if model.browse.component.Loading {
		t.Fatal("browse still loading after the page load")
	}
}

func TestBrowsePager_clickPrevLoadsPreviousPage(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wide: the status line stays on one row
	model.BrowsePage = 1
	model.browse.component.Result.HasMore = false
	model.focusActiveTable()

	pager := model.browsePager()
	if !pager.PrevEnabled || pager.NextEnabled {
		t.Fatalf("enabled = %t/%t, want only Prev at page 1 without HasMore", pager.PrevEnabled, pager.NextEnabled)
	}

	// When — click the Prev button on the button row.
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 1 + pager.PrevStart,
		Y:      model.browse.component.Table.Height() + 7,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command == nil {
		t.Fatal("clicking Prev returned no command")
	}
	model = driveCommand(model, command)

	// Then — the debounced load returned to page 0 with the fixture rows.
	if model.BrowsePage != 0 {
		t.Fatalf("page = %d, want 0", model.BrowsePage)
	}
	if rows := model.browse.component.Table.Rows(); len(rows) != 2 {
		t.Fatalf("rows = %d, want the two fixture rows", len(rows))
	}
}

func TestBrowsePager_clickDisabledButtonDoesNothing(t *testing.T) {
	cases := []struct {
		name    string
		page    int
		hasMore bool
		click   int // index of the button to click: 0 = Prev, 1 = Next
	}{
		{name: "prev on first page", page: 0, hasMore: true, click: 0},
		{name: "next on last page", page: 1, hasMore: false, click: 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := readyBrowseModel(t)
			model = resizeModel(model, 140, 24) // wide: the status line stays on one row
			model.BrowsePage = test.page
			model.browse.component.Result.HasMore = test.hasMore
			model.focusActiveTable()

			pager := model.browsePager()
			start := pager.PrevStart
			if test.click == 1 {
				start = pager.NextStart
			}
			updated, command := model.Update(tea.MouseClickMsg{
				X:      model.layout.schemaWidth + 1 + start,
				Y:      model.browse.component.Table.Height() + 7,
				Button: tea.MouseLeft,
			})
			model = updated.(Model)

			// Then — the disabled button ignores the click.
			if command != nil {
				t.Fatal("clicking a disabled button returned a command")
			}
			if model.BrowsePage != test.page {
				t.Fatalf("page = %d, want %d unchanged", model.BrowsePage, test.page)
			}
		})
	}
}

func TestBrowsePager_clickPrevWorksOnEmptyPage(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wide: the status line stays on one row
	model.BrowsePage = 1
	model.focusActiveTable()
	// The last page loaded no rows (e.g. rows were deleted since the
	// previous load); Prev stays enabled and must still page back.
	updated, _ := model.Update(browseTableMsg{table: "items", page: 1, tag: model.browse.component.PageTag, result: sqlite.Result{Columns: []string{"id", "name"}}})
	model = updated.(Model)
	if rows := model.browse.component.Table.Rows(); len(rows) != 0 {
		t.Fatalf("fixture: browse rows = %d, want an empty page", len(rows))
	}

	pager := model.browsePager()
	if !pager.PrevEnabled || pager.NextEnabled {
		t.Fatalf("enabled = %t/%t, want only Prev on an empty last page", pager.PrevEnabled, pager.NextEnabled)
	}

	// When — click Prev.
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 1 + pager.PrevStart,
		Y:      model.browse.component.Table.Height() + 7,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command == nil {
		t.Fatal("clicking Prev on an empty page returned no command")
	}
	model = driveCommand(model, command)

	// Then — page 0 reloads with the fixture rows.
	if model.BrowsePage != 0 {
		t.Fatalf("page = %d, want 0", model.BrowsePage)
	}
	if rows := model.browse.component.Table.Rows(); len(rows) != 2 {
		t.Fatalf("rows = %d, want the two fixture rows", len(rows))
	}
}

func TestBrowsePager_clickOnRowGapDoesNothing(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wide: the status line stays on one row
	model.browse.component.Result.HasMore = true
	model.focusActiveTable()
	cursor := model.browse.component.Table.Cursor()

	// When — click the gap between the pinned buttons.
	pager := model.browsePager()
	if !pager.NextEnabled {
		t.Fatal("Next should be enabled at page 0 with HasMore")
	}
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 1 + pager.NextStart - 1,
		Y:      model.browse.component.Table.Height() + 7,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)

	// Then — no navigation, no cell selection.
	if command != nil {
		t.Fatal("clicking the button-row gap returned a command")
	}
	if model.BrowsePage != 0 {
		t.Fatalf("page = %d, want 0", model.BrowsePage)
	}
	if model.browse.component.Table.Cursor() != cursor {
		t.Fatalf("cursor = %d, want %d unchanged", model.browse.component.Table.Cursor(), cursor)
	}
}

func TestBrowsePager_clickNextWorksWithSplitStatus(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24) // narrow: the status line splits onto two rows
	model.browse.component.Result.HasMore = true
	model.focusActiveTable()

	if !model.browseStatusSplit() {
		t.Fatal("fixture: status line should be split at width 100")
	}
	pager := model.browsePager()
	if !pager.NextEnabled {
		t.Fatal("Next should be enabled at page 0 with HasMore")
	}

	// Two status rows push the button row down one: screen row
	// Height()+8 (contentY = Height()+7).
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 1 + pager.NextStart,
		Y:      model.browse.component.Table.Height() + 8,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command == nil {
		t.Fatal("clicking Next on the split layout returned no command")
	}
	model = driveCommand(model, command)

	// Then — the debounced load advanced to page 1.
	if model.BrowsePage != 1 {
		t.Fatalf("page = %d, want 1", model.BrowsePage)
	}
}

func TestBrowse_horizontalWheelTravelsCells(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	// Widen the table beyond the viewport so travel has room: content
	// width 4+40+40 plus per-cell padding.
	model.browse.component.Table.SetColumns([]table.Column{
		{Title: "ID", Width: 4},
		{Title: "Left", Width: 40},
		{Title: "Right", Width: 40},
	})
	model.browse.component.Table.SetRows([]table.Row{
		{"1", strings.Repeat("a", 40), strings.Repeat("b", 40)},
		{"2", strings.Repeat("c", 40), strings.Repeat("d", 40)},
	})
	resizeResultsTable(&model.browse.component.Table, model.layout.tableViewportWidth, 5)
	model.browse.component.Offset, model.browse.component.SelectedColumn = 0, 0

	// Horizontal trackpad wheel travels the selected column right, and the
	// viewport reveals it.
	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelRight})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 1; got != want {
		t.Fatalf("column after wheel right = %d, want %d", got, want)
	}
	if view := ansi.Strip(model.browseView()); !strings.Contains(view, "Left") {
		t.Fatalf("browse view after wheel right = %q, want the selected Left column visible", view)
	}
	// A second tick reaches the last column; the offset pins to the
	// content edge.
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelRight})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 2; got != want {
		t.Fatalf("column after two wheel rights = %d, want %d", got, want)
	}
	if view := ansi.Strip(model.browseView()); !strings.Contains(view, "Right") {
		t.Fatalf("browse view after two wheel rights = %q, want the Right column visible", view)
	}

	// Wheel left travels back; the first tick returns to the middle
	// column, the second home.
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 1; got != want {
		t.Fatalf("column after wheel left = %d, want %d", got, want)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 0; got != want {
		t.Fatalf("column after two wheel lefts = %d, want %d", got, want)
	}
	if got, want := model.browse.component.Offset, 0; got != want {
		t.Fatalf("offset after two wheel lefts = %d, want %d", got, want)
	}

	// Shift+vertical wheel travels horizontally the same way.
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, Mod: tea.ModShift})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 1; got != want {
		t.Fatalf("column after shift+wheel down = %d, want %d", got, want)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, Mod: tea.ModShift})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 0; got != want {
		t.Fatalf("column after shift+wheel up = %d, want %d", got, want)
	}

	// The column selection clamps at the table edges.
	for range 10 {
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelRight})
		model = updated.(Model)
	}
	if got, want := model.browse.component.SelectedColumn, 2; got != want {
		t.Fatalf("column after repeated wheel right = %d, want %d", got, want)
	}
	if got, want := model.browse.component.Offset, tableOffset(model.browse.component.Table, 1<<20, model.layout.tableViewportWidth); got != want {
		t.Fatalf("offset after repeated wheel right = %d, want clamped %d", got, want)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	model = updated.(Model)
	if got, want := model.browse.component.SelectedColumn, 1; got != want {
		t.Fatalf("column after one wheel left from the last = %d, want %d", got, want)
	}

	// Plain vertical wheel still moves the cursor, not the cell.
	model.browse.component.Offset, model.browse.component.SelectedColumn = 0, 0
	model.browse.component.Table.SetCursor(0)
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.browse.component.Offset != 0 || model.browse.component.SelectedColumn != 0 {
		t.Fatalf("plain wheel down moved cell to offset=%d column=%d, want 0/0", model.browse.component.Offset, model.browse.component.SelectedColumn)
	}
	if got, want := model.browse.component.Table.Cursor(), 1; got != want {
		t.Fatalf("cursor after plain wheel down = %d, want %d", got, want)
	}
}

func TestStructure_horizontalWheelPansViewport(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	// Row-based tables have no column selection: the wheel pans.
	resizeResultsTable(&model.schema.component.Structure.Table, model.layout.tableViewportWidth, 5)
	model.schema.component.Structure.Table.SetRows(nil) // drop the fixture's 6-cell rows first
	model.schema.component.Structure.Table.SetColumns([]table.Column{
		{Title: "ID", Width: 4},
		{Title: "Left", Width: 40},
		{Title: "Right", Width: 40},
	})
	model.schema.component.Structure.Table.SetRows([]table.Row{{"1", strings.Repeat("a", 40), strings.Repeat("b", 40)}})
	resizeResultsTable(&model.schema.component.Structure.Table, model.layout.tableViewportWidth, 5)
	model.Tab, model.layout.structureOffset = tabStructure, 0

	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelRight})
	model = updated.(Model)
	if got, want := model.layout.structureOffset, mouseHorizontalStep; got != want {
		t.Fatalf("structure offset after wheel right = %d, want %d", got, want)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	model = updated.(Model)
	if got, want := model.layout.structureOffset, 0; got != want {
		t.Fatalf("structure offset after wheel left = %d, want 0", got)
	}
	// Shift+wheel pans too.
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, Mod: tea.ModShift})
	model = updated.(Model)
	if got, want := model.layout.structureOffset, mouseHorizontalStep; got != want {
		t.Fatalf("structure offset after shift+wheel down = %d, want %d", got, want)
	}
}

func TestQueryLog_horizontalWheelTravelsCells(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	model.queryLog.component.Table.SetRows(nil) // drop the fixture's rows first
	model.queryLog.component.Table.SetColumns([]table.Column{
		{Title: "A", Width: 4},
		{Title: "B", Width: 4},
		{Title: "C", Width: 4},
	})
	model.queryLog.component.Table.SetRows([]table.Row{{"1", "2", "3"}})
	resizeResultsTable(&model.queryLog.component.Table, model.layout.tableViewportWidth, 3)
	model.queryLog.component.Table.SetCursor(0)
	model.Focus, model.queryLog.component.Column = focusQueryLog, 0

	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelRight})
	model = updated.(Model)
	if got, want := model.queryLog.component.Column, 1; got != want {
		t.Fatalf("query log column after wheel right = %d, want %d", got, want)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	model = updated.(Model)
	if got, want := model.queryLog.component.Column, 0; got != want {
		t.Fatalf("query log column after wheel left = %d, want %d", got, want)
	}
	if got, want := model.queryLog.component.Table.Cursor(), 0; got != want {
		t.Fatalf("query log cursor after horizontal wheel = %d, want %d untouched", got, want)
	}
}

func TestBrowse_status_followsCursorMoves(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 140, 24) // wheel handling needs a sized model
	if got, want := model.browse.component.Status, "items | 1-2 | 1/2 | page 1"; got != want {
		t.Fatalf("browse status = %q, want %q", got, want)
	}

	// j moves down: the position in the status follows the cursor.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if got, want := model.browse.component.Status, "items | 1-2 | 2/2 | page 1"; got != want {
		t.Fatalf("browse status after j = %q, want %q", got, want)
	}

	// Mouse wheel up moves back.
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if got, want := model.browse.component.Status, "items | 1-2 | 1/2 | page 1"; got != want {
		t.Fatalf("browse status after wheel = %q, want %q", got, want)
	}
}

func TestBrowsePager_longSummarySplitsUntilWideEnough(t *testing.T) {
	model := readyBrowseModel(t)
	model.BrowsePage = 1
	model.browse.component.Result.HasMore = true
	model.browse.component.Status = "orderdetails | 2,996-3,020 | 7/25 | page 9"

	// A long summary never fits beside the hints until the viewport is
	// wide enough for both in full.
	for _, width := range []int{100, 110, 120} {
		model = resizeModel(model, width, 24)
		if !model.browseStatusSplit() {
			t.Fatalf("width %d: browseStatusSplit = false, want two lines", width)
		}
		lines := strings.Split(model.browseStatusLine(), "\n")
		if len(lines) != 2 {
			t.Fatalf("status at width %d = %q, want two lines", width, model.browseStatusLine())
		}
		if stripped := ansi.Strip(lines[1]); !strings.Contains(stripped, "orderdetails | 2,996-3,020 | 7/25 | page 9") {
			t.Fatalf("second line at width %d = %q, want the full summary", width, stripped)
		}
	}

	// Wide enough: single line with the full summary, like before the
	// change.
	for _, width := range []int{134, 140} {
		model = resizeModel(model, width, 24)
		if model.browseStatusSplit() {
			t.Fatalf("width %d: browseStatusSplit = true, want single line", width)
		}
		status := model.browseStatusLine()
		if strings.Contains(status, "\n") {
			t.Fatalf("status at width %d = %q, want a single line", width, status)
		}
		if !strings.Contains(ansi.Strip(status), "orderdetails | 2,996-3,020 | 7/25 | page 9") {
			t.Fatalf("status at width %d = %q, want the full summary", width, status)
		}
	}
}
