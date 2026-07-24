package workbench

import (
	"context"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestResize_wide_and_compact_focus_layout(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
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
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = resizeModel(model, 80, 24)

	// Then
	if !model.compact {
		t.Fatal("80-column terminal did not use compact layout")
	}
	if model.Tab != tabIndexes {
		t.Fatalf("tab = %v, want indexes after tab", model.Tab)
	}
	if model.schemaWidth <= 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("compact layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}
	if got, want := model.tableViewportWidth, 74; got != want {
		t.Errorf("compact table viewport width = %d, want %d", got, want)
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
	model := New("", context.Background(), testOpen)
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 24)

	// Then
	if got := model.schemaWidth; got != 30 {
		t.Errorf("schema width = %d, want 30", got)
	}
	if got := model.editorWidth; got != 68 {
		t.Errorf("workspace width = %d, want 68 with stacked query log pane", got)
	}
	if got := model.editorHeight; got != 6 {
		t.Errorf("editor height = %d, want 6", got)
	}
	if got := model.resultsHeight; got != 3 {
		t.Errorf("results height = %d, want 3 with an expanded lower query log pane", got)
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
			model := New("", context.Background(), testOpen)
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
	model := New("", context.Background(), testOpen)
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
	if !strings.Contains(view.Content, "BUBBLE WORKBENCH") || !strings.Contains(view.Content, "quit") {
		t.Fatal("compact view does not retain header and footer")
	}
}

func TestResize_compact_query_log_fills_single_pane(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 80, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)
	view := ansi.Strip(model.View().Content)

	// Then
	if model.Focus != focusQueryLog {
		t.Fatalf("focus = %v, want query log", model.Focus)
	}
	if got, want := model.queryLogHeight, 20; got != want {
		t.Fatalf("query log height = %d, want %d", got, want)
	}
	if got := lipgloss.Width(view); got > 80 {
		t.Fatalf("compact view width = %d, want at most 80", got)
	}
	if !strings.Contains(view, "Time") {
		t.Fatalf("compact query log view = %q, want query log table", view)
	}
}

func TestFullscreen_focuses_each_pane_at_a_wide_viewport(t *testing.T) {
	// Given
	model := readyModel(t)
	model.schema.SetItems([]list.Item{schemaItem{title: "projects"}})
	model = resizeModel(model, 100, 24)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	model = updated.(Model)

	// Then
	if !model.compact {
		t.Fatal("fullscreen mode did not use the single-pane layout")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "projects") {
		t.Fatalf("fullscreen schema view = %q, want schema content", view)
	}

	for _, test := range []struct {
		key  rune
		want string
	}{
		{key: '2', want: "Structure"},
		{key: '3', want: "Time"},
	} {
		// When
		updated, _ = model.Update(tea.KeyPressMsg{Code: test.key, Text: string(test.key)})
		model = updated.(Model)

		// Then
		if view := ansi.Strip(model.View().Content); !strings.Contains(view, test.want) {
			t.Fatalf("fullscreen pane %q view = %q, want %q", string(test.key), view, test.want)
		}
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	model = updated.(Model)

	// Then
	if model.compact {
		t.Fatal("fullscreen toggle did not restore the split layout")
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
		{Name: "id", Type: "INTEGER", PrimaryKey: 1, Indexes: []sharedsql.IndexKind{sharedsql.IndexPrimaryKey}},
		{Name: "name", Type: "TEXT", Attributes: "GENERATED STORED", Nullable: true, Indexes: []sharedsql.IndexKind{sharedsql.IndexUnique, sharedsql.IndexRegular}},
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
	assertTableTitlesAndPositiveWidths(t, model.structure, []string{"Column", "Indexes", "Type", "Attributes", "Nullable", "Default"})
	rows := model.structure.Rows()
	if len(rows) != 2 || !strings.Contains(rows[0][1], iconPrimaryKey+"PK") || !strings.Contains(rows[1][1], iconUnique+"UQ") || !strings.Contains(rows[1][1], iconRegular+"IX") || ansi.Strip(rows[1][1]) != iconUnique+"UQ "+iconRegular+"IX" || !strings.Contains(rows[0][1], "\x1b[") || !strings.Contains(rows[1][1], "\x1b[") {
		t.Fatalf("structure index markers = %#v, want colored primary, unique, and regular icons", rows)
	}
	if got, want := rows[1][3], "GENERATED STORED"; got != want {
		t.Fatalf("structure attributes = %q, want %q", got, want)
	}
	if got, want := rows[0][5], "NONE"; got != want {
		t.Fatalf("non-null column without default = %q, want %q", got, want)
	}
	if got, want := rows[1][5], "NULL"; got != want {
		t.Fatalf("nullable column without default = %q, want %q", got, want)
	}
	assertTableRenderGeometry(t, model.structure)

	assertTableTitlesAndPositiveWidths(t, model.browse, []string{"id", "name", "state"})
	assertTableRows(t, model.browse, []table.Row{{"1", "first", "open"}})
	assertTableRenderGeometry(t, model.browse)
}

func TestResize_tiny_multicolumn_results_render_within_viewport(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetColumns(tableColumns([]string{"ID", "Name", "Status"}, []table.Row{{"1", "first", "open"}}))
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
	model.SetColumns([]table.Column{{Title: "ID", Width: 4}, {Title: "Indexes", Width: 8}, {Title: "State", Width: 4}})
	model.SetRows([]table.Row{{"1", indexIcons([]sharedsql.IndexKind{sharedsql.IndexPrimaryKey, sharedsql.IndexUnique}), ""}})

	body := strings.Split(tableViewportView(model, 0, 18), "\n")[1]
	if strings.Contains(body, "\x1b[38;2;163;113;247m") || strings.Contains(body, "\x1b[38;2;227;179;65m") {
		t.Fatalf("selected row retains inline index colors that interrupt its highlight: %q", body)
	}
	if got, want := lipgloss.Width(body), model.Width(); got != want {
		t.Fatalf("selected row width = %d, want table width %d", got, want)
	}
}

func TestTableViewport_keeps_every_line_at_the_viewport_width(t *testing.T) {
	// Given
	model := newResultsTable()
	model.SetColumns([]table.Column{{Title: "A", Width: 1}})
	model.SetRows([]table.Row{{"x"}})
	resizeResultsTable(&model, 4, 3)

	// When
	lines := strings.Split(tableViewportView(model, 0, 4), "\n")

	// Then
	for index, line := range lines {
		if got, want := ansi.StringWidth(line), 4; got != want {
			t.Errorf("line %d width = %d, want %d", index, got, want)
		}
	}
}

func TestResize_wide_browse_table_does_not_wrap_inside_workspace_pane(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "customers", tabBrowse
	model.focusActiveTable()
	model = resizeModel(model, 160, 24)
	updated, _ := model.Update(browseTableMsg{table: "customers", page: 0, result: sqlite.Result{
		Columns: []string{"CustomerId", "FirstName", "LastName", "Company", "Address", "City", "State", "Country", "PostalCode", "Phone", "Fax", "Email", "SupportRepId"},
		Rows: [][]*string{{
			stringPointer("7"),
			stringPointer("Astrid"),
			stringPointer("Gruber"),
			stringPointer("NULL"),
			stringPointer("Rotenturmstraße 4, 1010 Innere Stadt"),
			stringPointer("Vienna"),
			stringPointer("NULL"),
			stringPointer("Austria"),
			stringPointer("1010"),
			stringPointer("+43 1 512 33 55"),
			stringPointer("+43 1 512 33 44"),
			stringPointer("astrid@example.com"),
			stringPointer("3"),
		}},
	}})
	model = updated.(Model)

	// When
	view := ansi.Strip(model.View().Content)

	// Then
	if strings.Contains(view, "││ 5") {
		t.Fatalf("wide workspace table wrapped a cell: %q", view)
	}
}

func TestCropTableLine_skips_wide_characters_cut_by_viewport_edges(t *testing.T) {
	// Given
	line := "ab中文cd"

	// When
	leftEdge := cropTableLine(line, 2, 2)
	rightEdge := cropTableLine(line, 3, 2)

	// Then
	if got, want := leftEdge, "中"; got != want {
		t.Errorf("left-edge crop = %q, want %q", got, want)
	}
	if got, want := rightEdge, "  "; got != want {
		t.Errorf("right-edge crop = %q, want %q", got, want)
	}
}

func TestStructureTable_selected_empty_primary_key_preserves_final_cell(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(tableInfoMsg{table: "projects", columns: []sqlite.ColumnInfo{{Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)

	// Then
	body := strings.Split(model.structureView(), "\n")[1]
	if got, want := lipgloss.Width(body), model.structure.Width(); got != want {
		t.Fatalf("selected structure row width = %d, want table width %d", got, want)
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
	if width > resultTable.Width() {
		t.Errorf("column footprint = %d, exceeds table width %d", width, resultTable.Width())
	}
}

func TestWorkspaceView_separatesTabsAndContent(t *testing.T) {
	model := readyModel(t)
	model = resizeModel(model, 100, 24)
	model.Tab = tabStructure
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sqlite.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}}})
	model = updated.(Model)

	lines := strings.Split(ansi.Strip(model.workspaceView()), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) != "" {
		t.Errorf("workspace lines = %#v, want a blank line after tabs", lines)
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

func TestTableColumns_keep_full_header_and_cell_widths(t *testing.T) {
	// Given
	titles := []string{"ID", "Name", "Value"}
	rows := []table.Row{{"1", "long value", "open"}}

	// When
	columns := tableColumns(titles, rows)

	// Then
	if got, want := len(columns), len(titles); got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	for index, column := range columns {
		if got, want := column.Title, titles[index]; got != want {
			t.Errorf("column %d title = %q, want %q", index, got, want)
		}
		if got, want := column.Width, []int{2, 10, 5}[index]; got != want {
			t.Errorf("column %d width = %d, want %d", index, got, want)
		}
	}
}

func TestTableColumns_keep_positive_widths(t *testing.T) {
	// Given
	titles := []string{"ID", "Name", "Value"}

	// When
	columns := tableColumns(titles, nil)

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
	columns := tableColumns(nil, nil)

	// Then
	if got, want := len(columns), 1; got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	if got, want := columns[0].Title, "Results"; got != want {
		t.Errorf("column title = %q, want %q", got, want)
	}
	if got := columns[0].Width; got != len("Results") {
		t.Errorf("placeholder width = %d, want %d", got, len("Results"))
	}
}

func TestTableLine_aligns_numeric_cells_right(t *testing.T) {
	// Given
	columns := []table.Column{{Title: "count", Width: 5}, {Title: "name", Width: 5}}

	// When
	line := tableLine(columns, table.Row{"12", "oak"}, numericColumns([]string{"INTEGER", "TEXT"}), 0, 14)

	// Then
	if got, want := ansi.Strip(line), "    12  oak   "; got != want {
		t.Fatalf("table line = %q, want %q", got, want)
	}
}
