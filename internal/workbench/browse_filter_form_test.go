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
			model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'r', Text: "r"})

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

func TestBrowseFilterGrid_escapeRestoresInlineValue(t *testing.T) {
	model := readyBrowseModel(t)
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updateBrowseFilterGrid(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})

	if form := model.browseFilterForm; form == nil || form.fields[0].value != "" || form.editing {
		t.Fatalf("filter form = %#v, want cancelled inline edit", form)
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
