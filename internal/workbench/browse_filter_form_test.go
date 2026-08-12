package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
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
			if command == nil || model.browse.component.FilterForm != nil || model.BrowsePage != 0 {
				t.Fatalf("apply state = form:%#v page:%d command:%t", model.browse.component.FilterForm, model.BrowsePage, command != nil)
			}
			model = resolveBrowseCommand(model, command())
			want := []sharedsql.BrowseFilter{
				{Column: "id", Operator: sharedsql.BrowseFilterGreater, Value: "1"},
				{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"},
			}
			if got := model.browse.component.Settings.Filters; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
				t.Fatalf("filters = %#v, want %#v", got, want)
			}
			if rows := model.browse.component.Table.Rows(); len(rows) != 1 || rows[0][1] != "second" {
				t.Fatalf("browse rows = %#v, want second", rows)
			}
		})
	}
}

func TestBrowseFilterGrid_patternOperator(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})

	// name: choose PATTERN (index 3 after None, LIKE, NOT LIKE) and enter
	// a shell-style wildcard value.
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for range 3 {
		model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, key := range []rune{'*', 's', 'e', 'c', 'o', 'n', 'd', '*'} {
		model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: key, Text: string(key)})
	}
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	model.browse.component.FilterForm.SetSize(120, 8)
	if view := ansi.Strip(model.browse.component.FilterForm.View()); !strings.Contains(view, "* any, ? one char") {
		t.Fatalf("filter form = %q, want the PATTERN wildcard hint", view)
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = resolveBrowseCommand(model, command())
	want := []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterPattern, Value: "*second*"}}
	if got := model.browse.component.Settings.Filters; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("filters = %#v, want %#v", got, want)
	}
	if rows := model.browse.component.Table.Rows(); len(rows) != 1 || rows[0][1] != "second" {
		t.Fatalf("browse rows = %#v, want second", rows)
	}
}

func TestBrowseFilterGrid_escapePreservesInlineValue(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})

	if form := model.browse.component.FilterForm; form == nil || form.Fields[0].Value != "x" || form.Editing {
		t.Fatalf("filter form = %#v, want preserved inline value", form)
	}

	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if form := model.browse.component.FilterForm; form.Fields[0].Operator != sharedsql.BrowseFilterEqual || form.Editing {
		t.Fatalf("filter form = %#v, want preserved operator selection", form)
	}
}

func TestBrowseFilterGrid_rRestoresOpenedState(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.Settings = browse.Settings{
		Filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%first%"}},
		Limit:   1,
	}
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	form := model.browse.component.FilterForm
	form.Fields[1].Operator, form.Fields[1].Value, form.Limit = sharedsql.BrowseFilterEqual, "second", "2"

	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'r', Text: "r"})
	form = model.browse.component.FilterForm
	if form.Fields[1].Operator != sharedsql.BrowseFilterLike || form.Fields[1].Value != "%first%" || form.Limit != "1" {
		t.Fatalf("reset form = %#v, want opened filter settings", form)
	}
}

func TestBrowseFilterGrid_backspaceClearsSelectedCell(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.component.Settings = browse.Settings{
		Filters: []sharedsql.BrowseFilter{{Column: "id", Operator: sharedsql.BrowseFilterGreater, Value: "1"}},
	}
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	form := model.browse.component.FilterForm
	if form.Fields[0].Operator != sharedsql.BrowseFilterNone || form.Fields[0].Value != "1" {
		t.Fatalf("operator clear = %#v, want empty operator with retained value", form.Fields[0])
	}

	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if form.Fields[0].Value != "" {
		t.Fatalf("value clear = %#v, want empty value", form.Fields[0])
	}
}

func TestBrowse_rClearsFiltersAndReloads(t *testing.T) {
	model := readyBrowseModel(t)
	model.BrowsePage = 1
	model.browse.component.Settings = browse.Settings{
		Filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"}},
		Sorts:   []browse.Sort{{Column: "id", Desc: true}},
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if command == nil || model.BrowsePage != 0 || len(model.browse.component.Settings.Filters) != 0 || len(model.browse.component.Settings.Sorts) != 1 {
		t.Fatalf("reset state = %#v, page:%d, command:%t", model.browse.component.Settings, model.BrowsePage, command != nil)
	}
	model = resolveBrowseCommand(model, command())
	if rows := model.browse.component.Table.Rows(); len(rows) != 2 {
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
			form := browse.NewFilterForm(columns, browse.Settings{}, core.BrowsePageSize, width, 3)
			for range len(columns) {
				form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, DefaultKeybindings())
			}
			if form.Row != len(columns) || form.HorizontalOffset == 0 && width == 24 {
				t.Fatalf("selection = row:%d offset:%d, want Rows visible", form.Row, form.HorizontalOffset)
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
