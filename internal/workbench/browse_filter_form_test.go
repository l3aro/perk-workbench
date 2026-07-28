package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestBrowseFilterGrid_editsAndAppliesFilters(t *testing.T) {
	for _, applyKey := range []tea.KeyPressMsg{
		{Code: tea.KeyF5},
		{Code: 's', Mod: tea.ModCtrl, Text: "s"},
	} {
		t.Run(applyKey.Keystroke(), func(t *testing.T) {
			model := readyBrowseModel(t)
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})

			// id: choose > 1.
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
			for range 5 {
				model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
			}
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '1', Text: "1"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

			// name: choose LIKE '%second%'.
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'h', Text: "h"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
			for _, key := range []rune{'%', 's', 'e', 'c', 'o', 'n', 'd', '%'} {
				model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: key, Text: string(key)})
			}
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

			updated, command := model.Update(applyKey)
			model = updated.(Model)
			if command == nil || model.browseFilterForm != nil || model.BrowsePage != 0 {
				t.Fatalf("apply state = form:%#v page:%d command:%t", model.browseFilterForm, model.BrowsePage, command != nil)
			}
			model = resolveBrowseCommand(model, command())
			want := []sharedsql.BrowseFilter{
				{Column: "id", Operator: sharedsql.BrowseFilterGreater, Value: "1"},
				{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"},
			}
			if got := model.browseSettings.filters; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
				t.Fatalf("filters = %#v, want %#v", got, want)
			}
			if rows := model.browse.Rows(); len(rows) != 1 || rows[0][1] != "second" {
				t.Fatalf("browse rows = %#v, want second", rows)
			}
		})
	}
}

func TestBrowseFilterGrid_escapePreservesInlineValue(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})

	if form := model.browseFilterForm; form == nil || form.fields[0].value != "x" || form.editing {
		t.Fatalf("filter form = %#v, want preserved inline value", form)
	}

	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if form := model.browseFilterForm; form.fields[0].operator != sharedsql.BrowseFilterEqual || form.editing {
		t.Fatalf("filter form = %#v, want preserved operator selection", form)
	}
}

func TestBrowseFilterGrid_rRestoresOpenedState(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseSettings = browseSettings{
		filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%first%"}},
		limit:   1,
	}
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	form := model.browseFilterForm
	form.fields[1].operator, form.fields[1].value, form.limit = sharedsql.BrowseFilterEqual, "second", "2"

	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'r', Text: "r"})
	form = model.browseFilterForm
	if form.fields[1].operator != sharedsql.BrowseFilterLike || form.fields[1].value != "%first%" || form.limit != "1" {
		t.Fatalf("reset form = %#v, want opened filter settings", form)
	}
}

func TestBrowseFilterGrid_backspaceClearsSelectedCell(t *testing.T) {
	model := readyBrowseModel(t)
	model.browseSettings = browseSettings{
		filters: []sharedsql.BrowseFilter{{Column: "id", Operator: sharedsql.BrowseFilterGreater, Value: "1"}},
	}
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	form := model.browseFilterForm
	if form.fields[0].operator != sharedsql.BrowseFilterNone || form.fields[0].value != "1" {
		t.Fatalf("operator clear = %#v, want empty operator with retained value", form.fields[0])
	}

	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if form.fields[0].value != "" {
		t.Fatalf("value clear = %#v, want empty value", form.fields[0])
	}
}

func TestBrowse_rClearsFiltersAndReloads(t *testing.T) {
	model := readyBrowseModel(t)
	model.BrowsePage = 1
	model.browseSettings = browseSettings{
		filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"}},
		sorts:   []browseSort{{column: "id", desc: true}},
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if command == nil || model.BrowsePage != 0 || len(model.browseSettings.filters) != 0 || len(model.browseSettings.sorts) != 1 {
		t.Fatalf("reset state = %#v, page:%d, command:%t", model.browseSettings, model.BrowsePage, command != nil)
	}
	model = resolveBrowseCommand(model, command())
	if rows := model.browse.Rows(); len(rows) != 2 {
		t.Fatalf("browse rows = %#v, want unfiltered rows", rows)
	}
}

func TestBrowseFilterGrid_widthAndSelection(t *testing.T) {
	columns := []sharedsql.ColumnInfo{
		{Name: "identifier", Type: "INTEGER"},
		{Name: "description", Type: "VARCHAR(255)"},
		{Name: "created_at", Type: "TIMESTAMP WITH TIME ZONE"},
	}
	for _, width := range []int{24, 72} {
		t.Run("width", func(t *testing.T) {
			form := newBrowseFilterForm(columns, browseSettings{}, width, 3)
			for range len(columns) {
				form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, DefaultKeybindings())
			}
			if form.row != len(columns) || form.horizontalOffset == 0 && width == 24 {
				t.Fatalf("selection = row:%d offset:%d, want Rows visible", form.row, form.horizontalOffset)
			}
			for _, line := range strings.Split(form.View(), "\n") {
				if got := ansi.StringWidth(ansi.Strip(line)); got > width {
					t.Fatalf("line width = %d, want <= %d: %q", got, width, line)
				}
			}
		})
	}
}

func updateBrowseFilterGrid(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}
