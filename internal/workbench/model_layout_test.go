package workbench

import (
	"context"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestResize_wide_and_compact_focus_layout(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 24)

	// Then
	if model.layout.compact {
		t.Fatal("wide terminal unexpectedly used compact layout")
	}
	if model.layout.schemaWidth <= 0 || model.layout.editorWidth <= 0 || model.layout.editorHeight < 0 || model.layout.resultsHeight < 0 {
		t.Fatalf("wide layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.layout.schemaWidth, model.layout.editorWidth, model.layout.editorHeight, model.layout.resultsHeight)
	}

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = resizeModel(model, 80, 24)

	// Then
	if !model.layout.compact {
		t.Fatal("80-column terminal did not use compact layout")
	}
	if model.Tab != tabBrowse {
		t.Fatalf("tab = %v, want Browse after SQL tab", model.Tab)
	}
	if model.layout.schemaWidth <= 0 || model.layout.editorWidth < 0 || model.layout.editorHeight < 0 || model.layout.resultsHeight < 0 {
		t.Fatalf("compact layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.layout.schemaWidth, model.layout.editorWidth, model.layout.editorHeight, model.layout.resultsHeight)
	}
	if got, want := model.layout.tableViewportWidth, 74; got != want {
		t.Errorf("compact table viewport width = %d, want %d", got, want)
	}

	// When
	model = resizeModel(model, 0, 0)

	// Then
	if model.layout.schemaWidth < 0 || model.layout.editorWidth < 0 || model.layout.editorHeight < 0 || model.layout.resultsHeight < 0 {
		t.Fatalf("edge layout has negative dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.layout.schemaWidth, model.layout.editorWidth, model.layout.editorHeight, model.layout.resultsHeight)
	}
}

func TestResize_wide_layout_uses_plan_formula(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 24)

	// Then
	if got := model.layout.schemaWidth; got != 44 {
		t.Errorf("schema width = %d, want 44", got)
	}
	if got := model.layout.editorWidth; got != 54 {
		t.Errorf("workspace width = %d, want 54 with stacked query log pane", got)
	}
	if got := model.layout.chatWidth; got != 0 {
		t.Errorf("chat width = %d, want hidden without an Assistant", got)
	}
	if got := model.layout.editorHeight; got != 6 {
		t.Errorf("editor height = %d, want 6", got)
	}
	if got := model.layout.resultsHeight; got != 3 {
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
			model := New("", context.Background(), testOpen, false)
			model.State, model.Focus = test.state, test.focus

			// When
			model = resizeModel(model, test.width, test.height)
			view := model.View()

			// Then
			if model.layout.schemaWidth < 0 || model.layout.editorWidth < 0 || model.layout.editorHeight < 0 || model.layout.resultsHeight < 0 {
				t.Fatalf("negative layout dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.layout.schemaWidth, model.layout.editorWidth, model.layout.editorHeight, model.layout.resultsHeight)
			}
			if view.Content == "" {
				t.Fatal("view content is empty")
			}
		})
	}
}

func TestResize_short_wide_terminal_uses_compact_single_pane(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.State = stateReady

	// When
	model = resizeModel(model, 100, 5)
	view := model.View()

	// Then
	if !model.layout.compact {
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
	if !strings.Contains(view.Content, "PERK WORKBENCH") || !strings.Contains(view.Content, "quit") {
		t.Fatal("compact view does not retain header and footer")
	}
}

func TestHeader_buttonsPinnedToFarRight(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)

	// When
	headerLine := strings.Split(ansi.Strip(model.View().Content), "\n")[0]

	// Then — header spans the full width.
	if got := ansi.StringWidth(headerLine); got != model.layout.width {
		t.Fatalf("header row width = %d, want %d", got, model.layout.width)
	}

	// The quit button is the rightmost non-space content.
	trimmed := strings.TrimRight(headerLine, " ")
	if !strings.HasSuffix(trimmed, headerQuitButtonLabel) {
		t.Fatalf("header right edge = %q, want it to end with %q", trimmed[len(trimmed)-10:], headerQuitButtonLabel)
	}

	// The palette button sits left of the quit button with the fixed gap
	// (plus each button's own padding) between their glyphs.
	palIdx := strings.Index(headerLine, headerButtonLabel)
	quitIdx := strings.LastIndex(headerLine, headerQuitButtonLabel)
	if palIdx < 0 || quitIdx < 0 || palIdx > quitIdx {
		t.Fatalf("header button order wrong: palette@%d quit@%d", palIdx, quitIdx)
	}
	if gap := quitIdx - palIdx - len(headerButtonLabel); gap < headerButtonGap+2 {
		t.Fatalf("gap between button glyphs = %d cells, want at least %d", gap, headerButtonGap+2)
	}

	// A margin of at least headerRightMargin cells follows the quit button.
	if margin := len(headerLine) - quitIdx - len(headerQuitButtonLabel); margin < headerRightMargin {
		t.Fatalf("margin right of quit button = %d cells, want at least %d", margin, headerRightMargin)
	}
}

func TestHeader_buttonsSameWidth(t *testing.T) {
	// Given — both buttons are rendered through the same header path.
	width := headerButtonWidth()
	pal := renderHeaderButton(headerButtonStyle, headerButtonLabel, width)
	quit := renderHeaderButton(headerQuitButtonStyle, headerQuitButtonLabel, width)

	// Then — they render at the shared width.
	if pw, qw := ansi.StringWidth(pal), ansi.StringWidth(quit); pw != qw || pw != width {
		t.Fatalf("button widths palette=%d quit=%d, want both %d", pw, qw, width)
	}
}

func TestHeader_paletteButtonClickOpensPalette(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)

	// When — click on the logo area of the header.
	updated, _ := model.Update(tea.MouseClickMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — nothing opens.
	if model.overlay.commandPalette.visible {
		t.Fatal("logo-area click opened the palette")
	}

	// When — click the palette button, left of the quit button.
	updated, _ = model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - headerButtonGap - (headerButtonWidth()+1)/2, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the command palette opens.
	if !model.overlay.commandPalette.visible {
		t.Fatal("header palette click did not open the command palette")
	}

	// When — click just left of both header buttons (outside their boxes).
	updated, _ = model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - headerButtonGap - headerButtonWidth() - 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the outside click dismisses the palette.
	if model.overlay.commandPalette.visible {
		t.Fatal("header click left of the buttons did not close the palette")
	}
}

func TestHeader_quitButtonOpensQuitDialog(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)

	// When — click the I/O button, right of the palette button.
	updated, _ := model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the quit confirmation dialog opens (like Ctrl+Q).
	if model.overlay.quitDialog == nil {
		t.Fatal("header quit button click did not open the quit dialog")
	}

	// When — the dialog is dismissed, then the margin cell is clicked.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.quitDialog != nil {
		t.Fatal("escape did not dismiss the quit dialog")
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: 100 - 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the margin click does nothing.
	if model.overlay.quitDialog != nil || model.overlay.commandPalette.visible {
		t.Fatal("margin click opened a dialog or the palette")
	}

	// When — the palette button is clicked.
	updated, _ = model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - headerButtonGap - (headerButtonWidth()+1)/2, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the palette opens, not a second dialog.
	if !model.overlay.commandPalette.visible {
		t.Fatal("header palette click did not open the command palette")
	}
}

func TestHeader_quitButtonOpensDialogOnConnectionScreen(t *testing.T) {
	// Given — connection screen: Ctrl+Q's state/form guards would block, the
	// button must still open the dialog (its actions are safe in every state).
	model := resizeModel(New("", context.Background(), testOpen, false), 100, 24)

	// When — click the I/O button, right of the palette button.
	updated, _ := model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)

	// Then — the quit confirmation dialog opens.
	if model.overlay.quitDialog == nil {
		t.Fatal("quit button did not open the dialog on the connection screen")
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
	if got, want := model.layout.queryLogHeight, 20; got != want {
		t.Fatalf("query log height = %d, want %d", got, want)
	}
	if got := lipgloss.Width(view); got > 80 {
		t.Fatalf("compact view width = %d, want at most 80", got)
	}
	if !strings.Contains(view, "Time") {
		t.Fatalf("compact query log view = %q, want query log table", view)
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
	assertTableTitlesAndPositiveWidths(t, model.queryLog.results, []string{"ID", "Name", "Status"})
	assertTableRows(t, model.queryLog.results, []table.Row{{"1", "first", "NULL"}, {"2", "second", "active"}})
	assertTableRenderGeometry(t, model.queryLog.results)
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
	assertTableTitlesAndPositiveWidths(t, model.structure.table, []string{"Column", "Indexes", "Type", "Attributes", "Nullable", "Default"})
	rows := model.structure.table.Rows()
	if len(rows) != 2 || !strings.Contains(rows[0][1], iconPrimaryKey+" PK") || !strings.Contains(rows[1][1], iconUnique+" UQ") || !strings.Contains(rows[1][1], iconRegular+" IX") || ansi.Strip(rows[1][1]) != iconUnique+" UQ | "+iconRegular+" IX" || !strings.Contains(rows[0][1], "\x1b[") || !strings.Contains(rows[1][1], "\x1b[") {
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
	assertTableRenderGeometry(t, model.structure.table)

	assertTableTitlesAndPositiveWidths(t, model.browse.table, []string{"id", "name", "state"})
	assertTableRows(t, model.browse.table, []table.Row{{"1", "first", "open"}})
	assertTableRenderGeometry(t, model.browse.table)
}

func TestResize_tiny_multicolumn_results_render_within_viewport(t *testing.T) {
	// Given
	model := readyModel(t)
	model.queryLog.results.SetColumns(tableColumns([]string{"ID", "Name", "Status"}, []table.Row{{"1", "first", "open"}}))
	model.queryLog.results.SetRows([]table.Row{{"1", "first", "open"}})

	// When
	model = resizeModel(model, 1, 4)

	// Then
	if model.layout.schemaWidth < 0 || model.layout.editorWidth < 0 || model.layout.editorHeight < 0 || model.layout.resultsHeight < 0 {
		t.Fatalf("negative layout dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.layout.schemaWidth, model.layout.editorWidth, model.layout.editorHeight, model.layout.resultsHeight)
	}
	assertTableTitlesAndPositiveWidths(t, model.queryLog.results, []string{"ID", "Name", "Status"})
	assertTableRenderGeometry(t, model.queryLog.results)
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

func TestResultsTable_selected_cell_keeps_trailing_row_highlight(t *testing.T) {
	model := newResultsTable()
	resizeResultsTable(&model, 18, 2)
	model.SetColumns([]table.Column{{Title: "ID", Width: 4}, {Title: "Name", Width: 6}, {Title: "State", Width: 4}})
	model.SetRows([]table.Row{{"1", "Ada", "active"}})

	body := strings.Split(tableViewportViewWithAlignment(model, nil, 0, 18, 1), "\n")[1]
	want := strings.TrimSuffix(lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary)).Background(lipgloss.Color(colorStripe)).Render(" act"), "\x1b[m")
	if !strings.Contains(body, want) {
		t.Fatalf("trailing cell lost selected-row highlight: %q", body)
	}
}

func TestResultsTable_selected_row_styles_viewport_padding(t *testing.T) {
	model := newResultsTable()
	resizeResultsTable(&model, 10, 2)
	model.SetColumns([]table.Column{{Title: "ID", Width: 1}})
	model.SetRows([]table.Row{{""}})

	body := strings.Split(tableViewportView(model, 0, 10), "\n")[1]
	want := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary)).Background(lipgloss.Color(colorStripe)).Render(strings.Repeat(" ", 10))
	if body != want {
		t.Fatalf("viewport padding lost selected-row highlight: %q", body)
	}
}

func TestResultsTable_selected_cell_survives_left_crop(t *testing.T) {
	model := newResultsTable()
	model.SetColumns([]table.Column{{Title: "Name", Width: 4}, {Title: "State", Width: 4}})
	model.SetRows([]table.Row{{"one", "two"}})
	resizeResultsTable(&model, 5, 2)

	body := strings.Split(tableViewportViewWithAlignment(model, nil, 3, 5, 0), "\n")[1]
	want := selectedCellStyle.Render("e  ")
	if !strings.Contains(body, want) {
		t.Fatalf("left-cropped selected cell lost its highlight: %q", body)
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
	line := "abＷＷcd"

	// When
	leftEdge := cropTableLine(line, 2, 2)
	rightEdge := cropTableLine(line, 3, 2)

	// Then
	if got, want := leftEdge, "Ｗ"; got != want {
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
	if got, want := lipgloss.Width(body), model.structure.table.Width(); got != want {
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

func TestSQLPane_borderRightCornersStayAligned(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 100, 24)
	model.Tab = tabSQL
	model.queryLog.editor.setValue("SELECT 1")
	border := lipgloss.RoundedBorder()
	lines := strings.Split(ansi.Strip(model.rightView()), "\n")

	// The SQL text identifies the nested frame independently of the outer pane.
	contentLine, sqlColumn := -1, -1
	for index, line := range lines {
		if column := strings.Index(line, "SELECT 1"); column >= 0 {
			contentLine, sqlColumn = index, utf8.RuneCountInString(line[:column])
			break
		}
	}
	if contentLine < 1 || sqlColumn < 1 {
		t.Fatalf("SQL editor not rendered in pane: %q", strings.Join(lines, "\n"))
	}

	// Then — both right corners share their rows with the SQL frame's left
	// corners, rather than being wrapped into the next line.
	left := sqlColumn - 1
	width := lipgloss.Width(sqlEditorBox(model.queryLog.editor.View(), colorSuccess))
	for _, corner := range []struct {
		line  int
		left  string
		right string
	}{
		{contentLine - 1, border.TopLeft, border.TopRight},
		{contentLine + model.queryLog.editor.height, border.BottomLeft, border.BottomRight},
	} {
		runes := []rune(lines[corner.line])
		if len(runes) <= left+width-1 || string(runes[left]) != corner.left || string(runes[left+width-1]) != corner.right {
			t.Fatalf("nested border line = %q, want %q at column %d and %q at column %d", lines[corner.line], corner.left, left, corner.right, left+width-1)
		}
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

func schemaModel(t *testing.T, fullscreen bool, width, height int) Model {
	t.Helper()
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	model.layout.fullscreen = fullscreen
	model.applyLayout(width, height)
	model.schema.list.SetItems([]list.Item{
		schemaItem{title: "accounts", description: "table", database: "main", table: "accounts", kind: "table"},
		schemaItem{title: "queue_1", description: "table", database: "main", table: "queue_1", kind: "table"},
	})
	model.schema.list.Select(0)
	return model
}

func TestCompactClick_schemaRowSelectsRenderedTable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		width      int
		height     int
		fullscreen bool
	}{
		{name: "narrow window", width: 80, height: 24},
		{name: "fullscreen toggle", width: 140, height: 40, fullscreen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := schemaModel(t, tc.fullscreen, tc.width, tc.height)
			if !model.layout.compact {
				t.Fatal("test setup did not produce the compact layout")
			}
			clickY := renderedRowY(t, model, "queue_1")
			updated, _ := model.Update(tea.MouseClickMsg{X: 20, Y: clickY, Button: tea.MouseLeft})
			model = updated.(Model)
			if got := model.SelectedTable; got != "queue_1" {
				t.Fatalf("compact click selected %q, want queue_1", got)
			}
		})
	}
}

func TestCompactClick_workspaceTabSwitchesOnRenderedRow(t *testing.T) {
	model := readyModel(t)
	model.Focus = focusWorkspace
	model = resizeModel(model, 80, 24)
	if !model.layout.compact {
		t.Fatal("test setup did not produce the compact layout")
	}
	tabY := renderedRowY(t, model, "Foreign Keys")
	updated, _ := model.Update(tea.MouseClickMsg{X: 11, Y: tabY, Button: tea.MouseLeft})
	model = updated.(Model)
	if got, want := model.Tab, tabBrowse; got != want {
		t.Fatalf("compact tab click = %v, want %v", got, want)
	}
}

func TestCompactClick_browseRowSelectsClickedCell(t *testing.T) {
	model := resizeModel(readyBrowseModel(t), 80, 24)
	if !model.layout.compact {
		t.Fatal("test setup did not produce the compact layout")
	}
	for _, tc := range []struct {
		needle  string
		wantRow int
	}{
		{needle: "first", wantRow: 0},
		{needle: "second", wantRow: 1},
	} {
		clickY := renderedRowY(t, model, tc.needle)
		updated, _ := model.Update(tea.MouseClickMsg{X: 3, Y: clickY, Button: tea.MouseLeft})
		model = updated.(Model)
		if got, want := model.browse.table.Cursor(), tc.wantRow; got != want {
			t.Fatalf("compact click on %q selected row %d, want %d", tc.needle, got, want)
		}
	}
}

func TestCompactClick_queryLogSelectsRenderedRow(t *testing.T) {
	model := resizeModel(readyModel(t), 80, 24)
	model.Focus = focusQueryLog
	model.appendQueryLog(queryLogEntry{statement: "SELECT first"})
	model.appendQueryLog(queryLogEntry{statement: "SELECT second"})
	// The query log renders newest first, so the newest entry is row 0.
	updated, _ := model.Update(tea.MouseClickMsg{X: 20, Y: renderedRowY(t, model, "SELECT second"), Button: tea.MouseLeft})
	model = updated.(Model)
	if got, want := model.queryLog.table.Cursor(), 0; got != want {
		t.Fatalf("compact click on newest row = cursor %d, want %d", got, want)
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: 20, Y: renderedRowY(t, model, "SELECT first"), Button: tea.MouseLeft})
	model = updated.(Model)
	if got, want := model.queryLog.table.Cursor(), 1; got != want {
		t.Fatalf("compact click on oldest row = cursor %d, want %d", got, want)
	}
	// A click at the start of the Statement column (third column) must
	// select that column, not fall back to column 0.
	clickX := 1 // pane left border
	for _, column := range model.queryLog.table.Columns()[:2] {
		clickX += column.Width + 2*spaceCompact
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: renderedRowY(t, model, "SELECT second"), Button: tea.MouseLeft})
	model = updated.(Model)
	if got, want := model.layout.queryLogColumn, 2; got != want {
		t.Fatalf("compact click on Statement column = column %d, want %d", got, want)
	}
}

func TestCompactRightClick_schemaOpensContextMenu(t *testing.T) {
	model := schemaModel(t, true, 140, 40)
	clickY := renderedRowY(t, model, "queue_1")
	updated, _ := model.Update(tea.MouseClickMsg{X: 20, Y: clickY, Button: tea.MouseRight})
	model = updated.(Model)
	if model.overlay.contextMenu == nil {
		t.Fatal("compact right-click did not open a context menu")
	}
	if len(model.overlay.contextMenu.options) != 2 {
		t.Fatalf("context menu options = %d, want 2", len(model.overlay.contextMenu.options))
	}
}

func TestCompactClick_formFieldFocusesOnClick(t *testing.T) {
	model := resizeModel(openColumn(t, "name", "TEXT"), 80, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: 40, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.structure.columnForm.form.GetFocusedField().GetKey(); got != "type" {
		t.Fatalf("compact form click focused %q, want type", got)
	}
}
