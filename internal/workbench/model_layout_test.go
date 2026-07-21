package workbench

import (
	"context"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestResize_wide_and_compact_focus_layout(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 24)

	// Then
	if model.compact {
		t.Fatal("wide terminal unexpectedly used compact layout")
	}
	if model.schemaWidth <= 0 || model.editorWidth <= 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("wide layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	model = resizeModel(model, 80, 24)

	// Then
	if !model.compact {
		t.Fatal("80-column terminal did not use compact layout")
	}
	if model.Tab != tabStructure {
		t.Fatalf("tab = %v, want structure after tab", model.Tab)
	}
	if model.schemaWidth <= 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("compact layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}

	// When
	model = resizeModel(model, 0, 0)

	// Then
	if model.schemaWidth < 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("edge layout has negative dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}
}

func TestResize_wide_layout_uses_plan_formula(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 24)

	// Then
	if got := model.schemaWidth; got != 30 {
		t.Errorf("schema width = %d, want 30", got)
	}
	if got := model.editorWidth; got != 68 {
		t.Errorf("right width = %d, want 68", got)
	}
	if got := model.editorHeight; got != 6 {
		t.Errorf("editor height = %d, want 6", got)
	}
	if got := model.resultsHeight; got != 14 {
		t.Errorf("results height = %d, want 14", got)
	}
}

func TestResize_small_nonzero_dimensions_render_without_negative_sizes(t *testing.T) {
	tests := []struct {
		name          string
		state         modelState
		focus         focus
		width, height int
	}{
		{name: "picking at 1x4", state: statePicking, focus: focusWorkspace, width: 1, height: 4},
		{name: "opening at 2x5", state: stateOpening, focus: focusWorkspace, width: 2, height: 5},
		{name: "failure at 1x4", state: stateFailure, focus: focusWorkspace, width: 1, height: 4},
		{name: "schema at 2x5", state: stateReady, focus: focusSchema, width: 2, height: 5},
		{name: "workspace at 1x4", state: stateReady, focus: focusWorkspace, width: 1, height: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.State, model.Focus = test.state, test.focus

			// When
			model = resizeModel(model, test.width, test.height)
			view := model.View()

			// Then
			if model.schemaWidth < 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
				t.Fatalf("negative layout dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
			}
			if view.Content == "" {
				t.Fatal("view content is empty")
			}
		})
	}
}

func TestResize_short_wide_terminal_uses_compact_single_pane(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 5)
	view := model.View()

	// Then
	if !model.compact {
		t.Fatal("100x5 terminal used the wide layout")
	}
	if view.Content == "" {
		t.Fatal("compact view content is empty")
	}
	if strings.Contains(view.Content, "Results") {
		t.Fatal("compact editor pane rendered the results pane")
	}
	if got := lipgloss.Height(view.Content); got > 5 {
		t.Fatalf("compact view height = %d, want at most 5", got)
	}
	if !strings.Contains(view.Content, "BUBBLE WORKBENCH") || !strings.Contains(view.Content, "q quit") {
		t.Fatal("compact view does not retain header and footer")
	}
}

func TestResize_results_reflows_loaded_titles_without_replacing_rows(t *testing.T) {
	// Given
	model := readyModel(t)
	requestID := model.StartQueryForTest(context.Background())
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"ID", "Name", "Status"},
		Rows: [][]*string{
			{stringPointer("1"), stringPointer("first"), nil},
			{stringPointer("2"), stringPointer("second"), stringPointer("active")},
		},
	}})
	model = updated.(Model)

	// When
	model = resizeModel(model, 80, 24)

	// Then
	assertTableTitlesAndPositiveWidths(t, model.results, []string{"ID", "Name", "Status"})
	assertTableRows(t, model.results, []table.Row{{"1", "first", "NULL"}, {"2", "second", "active"}})
	assertTableRenderGeometry(t, model.results)
}

func TestResize_browse_and_structure_reflow_loaded_titles_without_replacing_rows(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "projects"
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(tableInfoMsg{table: "projects", columns: []sqlite.ColumnInfo{
		{Name: "id", Type: "INTEGER", PrimaryKey: 1},
		{Name: "name", Type: "TEXT", Nullable: true},
	}})
	model = updated.(Model)
	updated, _ = model.Update(browseTableMsg{table: "projects", page: 0, result: sqlite.Result{
		Columns: []string{"id", "name", "state"},
		Rows:    [][]*string{{stringPointer("1"), stringPointer("first"), stringPointer("open")}},
	}})
	model = updated.(Model)

	// When
	model = resizeModel(model, 80, 24)

	// Then
	assertTableTitlesAndPositiveWidths(t, model.structure, []string{"Column", "Type", "Nullable", "Default", "PK"})
	assertTableRows(t, model.structure, []table.Row{{"id", "INTEGER", "no", "NULL", "1"}, {"name", "TEXT", "yes", "NULL", ""}})
	assertTableRenderGeometry(t, model.structure)

	assertTableTitlesAndPositiveWidths(t, model.browse, []string{"id", "name", "state"})
	assertTableRows(t, model.browse, []table.Row{{"1", "first", "open"}})
	assertTableRenderGeometry(t, model.browse)
}

func TestResize_tiny_multicolumn_results_render_within_viewport(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetColumns(tableColumns(1, []string{"ID", "Name", "Status"}))
	model.results.SetRows([]table.Row{{"1", "first", "open"}})

	// When
	model = resizeModel(model, 1, 4)

	// Then
	if model.schemaWidth < 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("negative layout dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}
	assertTableTitlesAndPositiveWidths(t, model.results, []string{"ID", "Name", "Status"})
	assertTableRenderGeometry(t, model.results)
}

func TestResultsTable_HeaderMatchesBodyWidth(t *testing.T) {
	model := newResultsTable()
	resizeResultsTable(&model, 12, 2)
	model.SetColumns([]table.Column{{Title: "Name", Width: 10}})
	model.SetRows([]table.Row{{"value"}})

	assertTableRenderGeometry(t, model)
}

func TestResultsTable_selected_row_highlights_all_columns(t *testing.T) {
	model := newResultsTable()
	resizeResultsTable(&model, 18, 2)
	model.SetColumns([]table.Column{{Title: "ID", Width: 4}, {Title: "Name", Width: 4}, {Title: "State", Width: 4}})
	model.SetRows([]table.Row{{"1", "first", "x"}})

	body := strings.Split(model.View(), "\n")[1]
	if got := strings.Count(body, "\x1b[m"); got != 1 {
		t.Fatalf("selected row contains %d ANSI resets, want 1 so its highlight spans every column: %q", got, body)
	}
	if got, want := lipgloss.Width(body), model.Width(); got != want {
		t.Fatalf("selected row width = %d, want table width %d", got, want)
	}
}

func resizeModel(model Model, width, height int) Model {
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

func assertTableTitlesAndPositiveWidths(t *testing.T, resultTable table.Model, titles []string) {
	t.Helper()
	columns := resultTable.Columns()
	if got, want := len(columns), len(titles); got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	width := 0
	for index, title := range titles {
		column := columns[index]
		width += column.Width + 2*spaceCompact
		if column.Title != title || column.Width < 1 {
			t.Errorf("column %d = (%q, %d), want (%q, positive)", index, column.Title, column.Width, title)
		}
	}
	if width != resultTable.Width() && resultTable.Width() >= len(columns)*(2*spaceCompact+1) {
		t.Errorf("column footprint = %d, want viewport %d", width, resultTable.Width())
	}
}

func assertTableRows(t *testing.T, resultTable table.Model, want []table.Row) {
	t.Helper()
	if got := resultTable.Rows(); !slices.EqualFunc(got, want, func(gotRow, wantRow table.Row) bool {
		return slices.EqualFunc(gotRow, wantRow, func(gotCell, wantCell string) bool {
			return strings.TrimRight(ansi.Strip(gotCell), " ") == wantCell
		})
	}) {
		t.Fatalf("rows = %#v, want %#v", got, want)
	}
}

func assertTableRenderGeometry(t *testing.T, resultTable table.Model) {
	t.Helper()
	clipped := lipgloss.NewStyle().MaxWidth(resultTable.Width()).Render(resultTable.View())
	lines := strings.Split(clipped, "\n")
	if len(lines) < 2 || lipgloss.Width(lines[0]) != lipgloss.Width(lines[1]) {
		t.Fatalf("table view = %q, want equal-width header and body", clipped)
	}
	for index, line := range lines {
		if got := lipgloss.Width(line); got > resultTable.Width() {
			t.Errorf("rendered line %d width = %d, want at most viewport %d", index, got, resultTable.Width())
		}
	}
}

func TestTableColumns_use_viewport_budget_and_title_order(t *testing.T) {
	// Given
	const viewportWidth = 31
	titles := []string{"ID", "Name", "Value"}

	// When
	columns := tableColumns(viewportWidth, titles)

	// Then
	if got, want := len(columns), len(titles); got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	width := 0
	for index, column := range columns {
		if got, want := column.Title, titles[index]; got != want {
			t.Errorf("column %d title = %q, want %q", index, got, want)
		}
		width += column.Width + 2*spaceCompact
	}
	if got, want := width, viewportWidth; got != want {
		t.Errorf("rendered column width = %d, want %d", got, want)
	}
	if got, want := columns[0].Width, 9; got != want {
		t.Errorf("first column width = %d, want %d", got, want)
	}
}

func TestTableColumns_keep_positive_widths_when_viewport_is_too_narrow(t *testing.T) {
	// Given
	titles := []string{"ID", "Name", "Value"}

	// When
	columns := tableColumns(1, titles)

	// Then
	if got, want := len(columns), len(titles); got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	for index, column := range columns {
		if got, want := column.Title, titles[index]; got != want {
			t.Errorf("column %d title = %q, want %q", index, got, want)
		}
		if column.Width < 1 {
			t.Errorf("column %d width = %d, want at least 1", index, column.Width)
		}
	}
}

func TestTableColumns_return_results_placeholder_when_titles_are_empty(t *testing.T) {
	// Given
	columns := tableColumns(12, nil)

	// Then
	if got, want := len(columns), 1; got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	if got, want := columns[0].Title, "Results"; got != want {
		t.Errorf("column title = %q, want %q", got, want)
	}
	if got := columns[0].Width + 2*spaceCompact; got != 12 {
		t.Errorf("rendered placeholder width = %d, want 12", got)
	}
}
