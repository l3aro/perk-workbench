package workbench

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestCellViewer_opens_for_SQL_results(t *testing.T) {
	// Given a ready SQL result with a long value and ANSI/control characters
	model := resizeModel(readyModel(t), 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	longValue := "\x1b[31mred\x1b[0m " + strings.Repeat("x", 200) + "\nnew line\x07"
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 'test'", result: sqlite.Result{
		Columns: []string{"note"},
		Rows: [][]*string{{
			stringPointer("short"),
		}},
		UntruncatedRows: [][]*string{{
			stringPointer(longValue),
		}},
	}})
	model = updated.(Model)
	model.Focus = focusWorkspace
	model.Tab = tabSQL
	model.results.Focus()

	// When — press v to view the cell
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	model = updated.(Model)

	// Then — a viewer exists with the full untruncated value and column title
	if model.cellViewer == nil {
		t.Fatal("cell.view did not open a cell viewer")
	}
	// GetContent returns the entire stored string, not just the visible viewport.
	// ANSI codes and BEL should be sanitized.
	full := model.cellViewer.viewport.GetContent()
	if !strings.Contains(full, "red ") || !strings.Contains(full, strings.Repeat("x", 200)) {
		t.Fatalf("cell viewer GetContent missing full value (got %d chars, want 200+):\n%s", len(full), full)
	}
	if !strings.Contains(full, "new line") {
		t.Fatalf("cell viewer GetContent missing second line, newlines were lost:\n%s", full)
	}
	if !strings.Contains(full, "\n") {
		t.Fatal("newline not preserved in cell viewer")
	}
	if strings.Contains(full, "\x07") || strings.Contains(full, "\x1b") {
		t.Fatal("control characters not sanitized from cell viewer")
	}
	if !strings.Contains(model.cellViewer.content(), "note") {
		t.Fatalf("cell viewer content missing column title")
	}
	if cmd != nil {
		t.Fatal("openCellViewer returned a non-nil command")
	}

	// When — press w to toggle soft wrap off (changing wrap resets horizontal scroll)
	cmd = model.cellViewer.update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if cmd != nil {
		t.Fatal("cell viewer w returned non-nil command")
	}
	if model.cellViewer.viewport.SoftWrap {
		t.Fatal("soft wrap not disabled after w (default is on)")
	}
	// Press w again to toggle back on
	cmd = model.cellViewer.update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !model.cellViewer.viewport.SoftWrap {
		t.Fatal("soft wrap should be enabled after second w")
	}

	// When — press Escape to close
	updated, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then — viewer cleared, selection unchanged
	if model.cellViewer != nil {
		t.Fatal("cell viewer not cleared after Escape")
	}
	if model.results.Cursor() != 0 {
		t.Fatalf("result cursor changed after Escape: %d, want 0", model.results.Cursor())
	}
	if model.resultsColumn != 0 {
		t.Fatalf("result column changed after Escape: %d, want 0", model.resultsColumn)
	}
}

func TestCellViewer_opens_for_Browse_selected_column(t *testing.T) {
	// Given a Browse table with a selected second column
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusWorkspace
	model.Tab = tabBrowse
	model.SelectedTable = "projects"
	model.browseResult = sqlite.Result{
		UntruncatedRows: [][]*string{{stringPointer("1"), stringPointer("target-value"), stringPointer("active")}},
	}
	model.browse.SetColumns(tableColumns([]string{"id", "name", "state"}, []table.Row{{"1", "target-value", "active"}}))
	model.browse.SetRows([]table.Row{{"1", "target-value", "active"}})
	model.browseColumn = 1
	model.browse.SetCursor(0)
	resizeResultsTable(&model.browse, model.tableViewportWidth, 5)
	model.focusActiveTable()

	// When — press v
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	model = updated.(Model)

	// Then — viewer shows the selected column's value, not its row peers
	if model.cellViewer == nil {
		t.Fatal("cell.view did not open a cell viewer for Browse")
	}
	content := model.cellViewer.content()
	if !strings.Contains(content, "target-value") {
		t.Fatalf("cell viewer content = %q, want 'target-value'", content)
	}
	if strings.Contains(content, "1") && strings.Contains(content, "active") {
		t.Fatalf("cell viewer shows full row content, want only cell value:\n%s", content)
	}
}

func TestCellViewer_not_opened_during_SQL_edit(t *testing.T) {
	// Given SQL editor editing mode with focused results
	model := resizeModel(readyModel(t), 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 'test'", result: sqlite.Result{
		Columns: []string{"note"},
		Rows:    [][]*string{{stringPointer("data")}},
	}})
	model = updated.(Model)
	model.Focus = focusWorkspace
	model.Tab = tabSQL
	model.editor.setValue("SELECT 1")
	model.formMode.beginInsert(model.editor)

	// When — press v while SQL editor active
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	model = updated.(Model)

	// Then — no viewer
	if model.cellViewer != nil {
		t.Fatal("cell viewer opened during active SQL editor")
	}
}

func TestCellViewer_resizes_on_window_size(t *testing.T) {
	// Given an active cell viewer
	model := resizeModel(readyModel(t), 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 'test'", result: sqlite.Result{
		Columns: []string{"note"},
		Rows:    [][]*string{{stringPointer("value")}},
		UntruncatedRows: [][]*string{{
			stringPointer("full-value"),
		}},
	}})
	model = updated.(Model)
	model.Focus = focusWorkspace
	model.Tab = tabSQL
	model.results.Focus()
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	model = updated.(Model)
	if model.cellViewer == nil {
		t.Fatal("test precondition: viewer not opened")
	}

	// When — resize window
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = updated.(Model)

	// Then — viewport dimensions updated
	wantW := max(model.width-8, 1)
	wantH := max(model.height-10, 1)
	if model.cellViewer == nil {
		t.Fatal("cell viewer cleared after resize")
	}
	if got, want := model.cellViewer.viewport.Width(), wantW; got != want {
		t.Fatalf("cell viewer width = %d, want %d", got, want)
	}
	if got, want := model.cellViewer.viewport.Height(), wantH; got != want {
		t.Fatalf("cell viewer height = %d, want %d", got, want)
	}
}

func TestCellViewer_palette_opens_and_shows_in_context(t *testing.T) {
	// Given ready SQL with focused results
	model := resizeModel(readyModel(t), 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 'test'", result: sqlite.Result{
		Columns: []string{"note"},
		Rows:    [][]*string{{stringPointer("palette-value")}},
		UntruncatedRows: [][]*string{{
			stringPointer("full-palette-value"),
		}},
	}})
	model = updated.(Model)
	model.Focus = focusWorkspace
	model.Tab = tabSQL
	model.results.Focus()

	// Then — palette includes cell.view
	palette := newCommandPalette(model)
	hasView := false
	for _, item := range palette.items {
		if item.id == "cell.view" {
			hasView = true
			break
		}
	}
	if !hasView {
		t.Fatal("command palette does not include cell.view for SQL results")
	}

	// When — palette dispatches cell.view
	updated, _ = model.handlePaletteCommand("cell.view")
	model = updated.(Model)

	// Then — viewer opened
	if model.cellViewer == nil {
		t.Fatal("handlePaletteCommand('cell.view') did not open viewer for SQL")
	}
	content := model.cellViewer.content()
	if !strings.Contains(content, "palette-value") {
		t.Fatalf("palette-opened viewer content = %q, want 'palette-value'", content)
	}

	// Given Browse state
	model.cellViewer = nil
	model.Tab = tabBrowse
	model.SelectedTable = "projects"
	model.browseResult = sqlite.Result{
		UntruncatedRows: [][]*string{{stringPointer("1"), stringPointer("browse-palette")}},
	}
	model.browse.SetColumns(tableColumns([]string{"id", "name"}, []table.Row{{"1", "browse-palette"}}))
	model.browse.SetRows([]table.Row{{"1", "browse-palette"}})
	model.browseColumn = 1
	model.browse.SetCursor(0)
	resizeResultsTable(&model.browse, model.tableViewportWidth, 5)
	model.focusActiveTable()

	// Then — palette includes cell.view for Browse
	palette = newCommandPalette(model)
	hasView = false
	for _, item := range palette.items {
		if item.id == "cell.view" {
			hasView = true
			break
		}
	}
	if !hasView {
		t.Fatal("command palette does not include cell.view for Browse")
	}

	// When — palette dispatches cell.view
	updated, _ = model.handlePaletteCommand("cell.view")
	model = updated.(Model)

	// Then — viewer opens with selected column value
	if model.cellViewer == nil {
		t.Fatal("handlePaletteCommand('cell.view') did not open viewer for Browse")
	}
	content = model.cellViewer.content()
	if !strings.Contains(content, "browse-palette") {
		t.Fatalf("palette-opened viewer content = %q, want 'browse-palette'", content)
	}
}

func TestCellViewer_not_in_palette_when_inactive(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) Model
	}{
		{
			name: "Browse with active form",
			setup: func(t *testing.T) Model {
				model := resizeModel(readyModel(t), 100, 24)
				model.Tab = tabBrowse
				model.browse.SetColumns(tableColumns(nil, nil))
				model.browse.SetRows(nil)
				model.Focus = focusWorkspace
				model.browseForm = browseForm{columns: []string{"id"}}
				model.formMode.mode = formModeInsert
				return model
			},
		},
		{
			name: "SQL without focused results",
			setup: func(t *testing.T) Model {
				model := resizeModel(readyModel(t), 100, 24)
				model.Tab = tabSQL
				model.Focus = focusWorkspace
				model.results.Blur()
				return model
			},
		},
		{
			name: "SQL with editor editing",
			setup: func(t *testing.T) Model {
				model := resizeModel(readyModel(t), 100, 24)
				model.Tab = tabSQL
				model.results.Focus()
				model.results.SetColumns(tableColumns([]string{"x"}, nil))
				model.results.SetRows(nil)
				model.editor.setValue("SELECT ")
				model.formMode.beginInsert(model.editor)
				return model
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := test.setup(t)
			palette := newCommandPalette(model)
			for _, item := range palette.items {
				if item.id == "cell.view" {
					t.Fatalf("cell.view present in palette when %s", test.name)
				}
			}
		})
	}
}

func TestCellViewer_draw_has_title_padding_footer(t *testing.T) {
	// Given — a cell viewer with short content (2 lines, compact dialog)
	cv := newCellViewer("testcol", "hello\nworld", 40, 10)

	// Set up model with viewer
	model := resizeModel(readyModel(t), 60, 20)
	model.cellViewer = cv

	// When — render to canvas
	canvas := uv.NewScreenBuffer(60, 20)
	screen.Clear(canvas)
	model.drawCellViewer(canvas)

	// Strip ANSI for plain-text inspection
	rendered := ansi.Strip(canvas.Render())
	lines := strings.Split(rendered, "\n")

	// Title contains exactly the column name (no percentages mixed in)
	titleLine := ""
	for _, line := range lines {
		if strings.Contains(line, "View testcol") {
			titleLine = line
			break
		}
	}
	if titleLine == "" {
		t.Fatal("title 'View testcol' not found")
	}
	if strings.Contains(titleLine, "V:") || strings.Contains(titleLine, "H:") {
		t.Fatalf("title contains percentage info (should be in footer): %q", titleLine)
	}

	// Content rows are present
	foundHello := false
	foundWorld := false
	helloLine := ""
	for _, line := range lines {
		// Match content within the dialog interior (after border)
		if strings.Contains(line, "hello") && !strings.Contains(line, "View") {
			foundHello = true
			helloLine = line
		}
		if strings.Contains(line, "world") {
			foundWorld = true
		}
	}
	if !foundHello || !foundWorld {
		t.Fatalf("content lines 'hello'/'world' not found in rendered output")
	}

	// Left padding: content after border "│" has leading spaces
	// The dialog border is drawn at the left, so content lines start with "│  hello..."
	if !strings.Contains(helloLine, "│  hello") {
		t.Fatalf("content line missing 2-space left padding (after border): %q", helloLine)
	}

	// Footer has both bindings (left) and percentages (right)
	footerLine := ""
	for _, line := range lines {
		if strings.Contains(line, "w wrap") && strings.Contains(line, "V:") {
			footerLine = line
			break
		}
	}
	if footerLine == "" {
		t.Fatal("footer with bindings and percentages not found")
	}
	// Bindings before percentages = left vs right aligned
	bindIdx := strings.Index(footerLine, "w wrap")
	pctIdx := strings.Index(footerLine, "V:")
	if bindIdx < 0 || pctIdx < 0 || bindIdx >= pctIdx {
		t.Fatalf("bindings should be left of percentages: %q", footerLine)
	}

	// Title above content above footer — structural order
	titleRow := -1
	firstContentRow := -1
	footerRow := -1
	for i, line := range lines {
		if strings.Contains(line, "View testcol") {
			titleRow = i
		}
		if strings.Contains(line, "hello") && firstContentRow < 0 {
			firstContentRow = i
		}
		if strings.Contains(line, "w wrap") && strings.Contains(line, "V:") {
			footerRow = i
		}
	}
	if titleRow < 0 || firstContentRow < 0 || footerRow < 0 {
		t.Fatal("could not locate all structural rows")
	}
	if !(titleRow < firstContentRow && firstContentRow < footerRow) {
		t.Fatalf("structural order wrong: title=%d content=%d footer=%d", titleRow, firstContentRow, footerRow)
	}
	// At least 1 blank row between title and content, and between content and footer
	if firstContentRow-titleRow < 2 {
		t.Fatalf("no padding row between title (row %d) and content (row %d)", titleRow, firstContentRow)
	}
	if footerRow-firstContentRow < 2 {
		t.Fatalf("no padding row between content (row %d) and footer (row %d)", firstContentRow, footerRow)
	}
}

func TestSanitizeCellViewer_strips_ansi_preserves_newlines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "preserves plain text", input: "hello world", want: "hello world"},
		{name: "strips ANSI escape", input: "\x1b[31mred\x1b[0m", want: "red"},
		{name: "strips OSC sequence", input: "a\x1b]0;title\x07b", want: "ab"},
		{name: "preserves newlines", input: "line1\nline2", want: "line1\nline2"},
		{name: "strips carriage return", input: "a\rb", want: "ab"},
		{name: "preserves tabs", input: "a\tb", want: "a\tb"},
		{name: "strips BEL", input: "a\x07b", want: "ab"},
		{name: "preserves full length", input: "abc " + strings.Repeat("x", 500), want: "abc " + strings.Repeat("x", 500)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeCellViewer(test.input)
			if got != test.want {
				t.Fatalf("sanitizeCellViewer(%q) = %q (len %d), want %q (len %d)",
					test.input, got, len(got), test.want, len(test.want))
			}
		})
	}
}
