package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestBrowsePager_buttonsAlwaysRenderedAndStyledByAvailability(t *testing.T) {
	cases := []struct {
		name                             string
		page                             int
		hasMore                          bool
		wantPrevEnabled, wantNextEnabled bool
	}{
		{name: "first page without more rows", page: 0, hasMore: false, wantPrevEnabled: false, wantNextEnabled: false},
		{name: "first page with more rows", page: 0, hasMore: true, wantPrevEnabled: false, wantNextEnabled: true},
		{name: "later page without more rows", page: 1, hasMore: false, wantPrevEnabled: true, wantNextEnabled: false},
		{name: "later page with more rows", page: 1, hasMore: true, wantPrevEnabled: true, wantNextEnabled: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := readyBrowseModel(t)
			model.BrowsePage = test.page
			model.browseResult.HasMore = test.hasMore

			pager := model.browsePager()
			if pager.line == "" {
				t.Fatal("pager row must always render")
			}
			if pager.prevEnabled != test.wantPrevEnabled || pager.nextEnabled != test.wantNextEnabled {
				t.Fatalf("enabled = %t/%t, want %t/%t", pager.prevEnabled, pager.nextEnabled, test.wantPrevEnabled, test.wantNextEnabled)
			}
			wantPrev := formCancelButtonStyle.Render(browsePrevLabel)
			if test.wantPrevEnabled {
				wantPrev = formSaveButtonStyle.Render(browsePrevLabel)
			}
			if pager.prev != wantPrev {
				t.Fatalf("Prev = %q, want %q (disabled keeps the secondary color, enabled switches to primary)", pager.prev, wantPrev)
			}
			wantNext := formCancelButtonStyle.Render(browseNextLabel)
			if test.wantNextEnabled {
				wantNext = formSaveButtonStyle.Render(browseNextLabel)
			}
			if pager.next != wantNext {
				t.Fatalf("Next = %q, want %q (disabled keeps the secondary color, enabled switches to primary)", pager.next, wantNext)
			}
			row := ansi.Strip(pager.line)
			if !strings.Contains(row, "Prev") || !strings.Contains(row, "Next") {
				t.Fatalf("row = %q, want both buttons always visible", row)
			}
		})
	}
}

func TestBrowsePager_buttonsStayPinnedToTheirEdges(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)

	// Both buttons always occupy the same edges, enabled or not.
	for _, test := range []struct {
		page    int
		hasMore bool
	}{
		{page: 0, hasMore: false},
		{page: 0, hasMore: true},
		{page: 1, hasMore: false},
		{page: 1, hasMore: true},
	} {
		model.BrowsePage, model.browseResult.HasMore = test.page, test.hasMore
		row := strings.TrimSpace(ansi.Strip(model.browsePagerLine()))
		if !strings.HasPrefix(row, "◀ Prev") || !strings.HasSuffix(row, "Next ▶") {
			t.Fatalf("row = %q, want Prev left and Next right", row)
		}
	}

	// The status line above the row keeps the other hints; the pager
	// button row always renders the page keys, so the n/p hint is gone.
	view := ansi.Strip(model.browseView())
	if !strings.Contains(view, "/ filter | r reset | s sort column") {
		t.Fatalf("browse view = %q, want the filter/reset/sort hints kept", view)
	}
	if strings.Contains(view, "n/p page") {
		t.Fatalf("browse view = %q, want the n/p page hint dropped (the buttons always render it)", view)
	}
}

func TestBrowsePager_statusLineNeverWraps(t *testing.T) {
	model := readyBrowseModel(t)
	model.BrowsePage = 1
	model.browseResult.HasMore = true

	for _, width := range []int{90, 100, 101, 102, 103, 120, 160} {
		model = resizeModel(model, width, 24)
		status := model.browseStatusLine()
		if strings.Contains(status, "\n") {
			t.Fatalf("status line at width %d = %q, want a single line", width, status)
		}
		if got := ansi.StringWidth(status); got > model.tableViewportWidth {
			t.Fatalf("status line at width %d is %d cells wide, viewport %d", width, got, model.tableViewportWidth)
		}
	}
}

func TestBrowsePager_rowIsLastBrowseViewLine(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	model.BrowsePage = 1
	model.browseResult.HasMore = true

	// The browse view is header + Height() rows + status line + gap +
	// pager row.
	lines := strings.Split(ansi.Strip(model.browseView()), "\n")
	if want := model.browse.Height() + 4; len(lines) != want {
		t.Fatalf("browse view height = %d, want %d", len(lines), want)
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, "◀ Prev") || !strings.HasSuffix(last, "Next ▶") {
		t.Fatalf("last browse view line = %q, want the pinned pager row", last)
	}
}

func TestBrowsePager_clickNextLoadsFollowingPage(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	model.browseResult.HasMore = true
	model.focusActiveTable()

	pager := model.browsePager()
	if pager.prevEnabled || !pager.nextEnabled {
		t.Fatalf("enabled = %t/%t, want only Next at page 0 with HasMore", pager.prevEnabled, pager.nextEnabled)
	}

	// When — click the Next button on the button row (screen row
	// Height()+7: contentY = Height()+6 plus the header row).
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.schemaWidth + 1 + pager.nextStart,
		Y:      model.browse.Height() + 7,
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
	if model.browseLoading {
		t.Fatal("browse still loading after the page load")
	}
}

func TestBrowsePager_clickPrevLoadsPreviousPage(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	model.BrowsePage = 1
	model.browseResult.HasMore = false
	model.focusActiveTable()

	pager := model.browsePager()
	if !pager.prevEnabled || pager.nextEnabled {
		t.Fatalf("enabled = %t/%t, want only Prev at page 1 without HasMore", pager.prevEnabled, pager.nextEnabled)
	}

	// When — click the Prev button on the button row.
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.schemaWidth + 1 + pager.prevStart,
		Y:      model.browse.Height() + 7,
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
	if rows := model.browse.Rows(); len(rows) != 2 {
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
			model = resizeModel(model, 100, 24)
			model.BrowsePage = test.page
			model.browseResult.HasMore = test.hasMore
			model.focusActiveTable()

			pager := model.browsePager()
			start := pager.prevStart
			if test.click == 1 {
				start = pager.nextStart
			}
			updated, command := model.Update(tea.MouseClickMsg{
				X:      model.schemaWidth + 1 + start,
				Y:      model.browse.Height() + 7,
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
	model = resizeModel(model, 100, 24)
	model.BrowsePage = 1
	model.focusActiveTable()
	// The last page loaded no rows (e.g. rows were deleted since the
	// previous load); Prev stays enabled and must still page back.
	updated, _ := model.Update(browseTableMsg{table: "items", page: 1, tag: model.browsePageTag, result: sqlite.Result{Columns: []string{"id", "name"}}})
	model = updated.(Model)
	if rows := model.browse.Rows(); len(rows) != 0 {
		t.Fatalf("fixture: browse rows = %d, want an empty page", len(rows))
	}

	pager := model.browsePager()
	if !pager.prevEnabled || pager.nextEnabled {
		t.Fatalf("enabled = %t/%t, want only Prev on an empty last page", pager.prevEnabled, pager.nextEnabled)
	}

	// When — click Prev.
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.schemaWidth + 1 + pager.prevStart,
		Y:      model.browse.Height() + 7,
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
	if rows := model.browse.Rows(); len(rows) != 2 {
		t.Fatalf("rows = %d, want the two fixture rows", len(rows))
	}
}

func TestBrowsePager_clickOnRowGapDoesNothing(t *testing.T) {
	model := readyBrowseModel(t)
	model = resizeModel(model, 100, 24)
	model.browseResult.HasMore = true
	model.focusActiveTable()
	cursor := model.browse.Cursor()

	// When — click the gap between the pinned buttons.
	pager := model.browsePager()
	if !pager.nextEnabled {
		t.Fatal("Next should be enabled at page 0 with HasMore")
	}
	updated, command := model.Update(tea.MouseClickMsg{
		X:      model.schemaWidth + 1 + pager.nextStart - 1,
		Y:      model.browse.Height() + 7,
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
	if model.browse.Cursor() != cursor {
		t.Fatalf("cursor = %d, want %d unchanged", model.browse.Cursor(), cursor)
	}
}
