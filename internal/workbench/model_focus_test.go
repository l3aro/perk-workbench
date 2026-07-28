package workbench

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func TestView_sql_renders_huh_text_at_wide_and_compact_sizes(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
	}{
		{name: "wide", width: 100, height: 24},
		{name: "compact", width: 80, height: 24},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", context.Background(), testOpen)
			model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
			model.editor.setValue("SELECT 1")

			// When
			updated, _ := model.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			view := updated.(Model).View()

			// Then
			if !strings.Contains(ansi.Strip(view.Content), "SELECT 1") {
				t.Fatalf("SQL view = %q, want Huh Text value", view.Content)
			}
		})
	}
}

func TestWorkspace_tabs_route_input_to_the_active_view(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
	model.editor.setValue("select ")

	// When
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusWorkspace)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabBrowse)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "select " {
		t.Fatalf("non-SQL tab changed editor value = %q, want %q", got, "select ")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabIndexes)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabForeignKeys)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "select " {
		t.Fatalf("non-SQL tab changed editor value = %q, want %q", got, "select ")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabSQL)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabBrowse)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabSQL)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabForeignKeys)
}

func TestWorkspace_HLNavigateTabs(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabBrowse)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabSQL)
}

func TestFocus_sql_keeps_q_as_text_after_input_starts(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
	model.editor.setValue("select ")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "select q" {
		t.Fatalf("editor value = %q, want %q", got, "select q")
	}
}

func TestFocus_sql_insertModeKeepsPaneShortcutsAsText(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
	model.editor.setValue("select ")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "select 1" {
		t.Fatalf("editor value = %q, want %q", got, "select 1")
	}
}

func TestView_workspaceTabsShowModeBadge(t *testing.T) {
	for _, mode := range []struct {
		name  string
		value formMode
		label string
		badge string
	}{
		{name: "normal", value: formModeNormal, label: "NORMAL", badge: modeNormalStyle.Render("NORMAL")},
		{name: "insert", value: formModeInsert, label: "INSERT", badge: modeInsertStyle.Render("INSERT")},
	} {
		for _, tab := range []workspaceTab{tabStructure, tabBrowse, tabSQL, tabIndexes, tabForeignKeys} {
			t.Run(mode.name+"/"+string(rune('0'+tab)), func(t *testing.T) {
				// Given
				model := New("", context.Background(), testOpen)
				model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tab
				model.formMode.mode = mode.value
				model.layout(100, 24)

				// When
				view := model.workspaceView()

				// Then
				bottom := strings.TrimSpace(ansi.Strip(view[strings.LastIndex(view, "\n")+1:]))
				if !strings.HasPrefix(bottom, mode.label) {
					t.Errorf("tab %d pane = %q, want bottom-left %s badge", tab, ansi.Strip(view), mode.label)
				}
				if !strings.Contains(view, mode.badge) {
					t.Errorf("tab %d badge = %q, want expected mode badge styling", tab, view)
				}
			})
		}
	}
}

func TestView_contextualHintsRenderInTheirPanes(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
	model.layout(100, 24)

	// When
	workspace := ansi.Strip(model.workspaceView())
	history := ansi.Strip(model.queryLogContentView())
	footer := ansi.Strip(model.footer())

	// Then
	if bottom := strings.TrimSpace(workspace[strings.LastIndex(workspace, "\n")+1:]); !strings.HasPrefix(bottom, "NORMAL") || !strings.HasSuffix(bottom, "L/H tabs") {
		t.Fatalf("workspace hint = %q, want bottom-left NORMAL followed by tab view", workspace)
	}
	if bottom := strings.TrimSpace(history[strings.LastIndex(history, "\n")+1:]); !strings.HasPrefix(bottom, "y copy cell | enter detail | e explain") {
		t.Fatalf("history hint = %q, want query-history shortcuts", history)
	}
	if strings.Contains(footer, "e explain") || strings.Contains(footer, "L/H tabs") {
		t.Fatalf("footer = %q, want contextual hints omitted", footer)
	}
}

func TestView_requestsKeyboardEnhancements(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)

	// When
	view := model.View()

	// Then
	if !view.KeyboardEnhancements.ReportEventTypes {
		t.Fatal("view did not request enhanced keyboard event types")
	}
}

func TestFocus_schema_filters_with_slash_and_esc(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus = stateReady, focusSchema
	model.schema.SetItems([]list.Item{
		schemaItem{title: "accounts", description: "table"},
		schemaItem{title: "queue_1", description: "table"},
	})

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)
	model = updateFromCommand(model, command)
	updated, command = model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// Then
	if !model.schema.SettingFilter() {
		t.Fatal("schema filter is not active")
	}
	if got := model.schema.FilterValue(); got != "q1" {
		t.Fatalf("filter value = %q, want %q", got, "q1")
	}
	if got := model.schema.VisibleItems(); len(got) != 1 || got[0].FilterValue() != "queue_1" {
		t.Fatalf("visible items = %#v, want queue_1", got)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if model.schema.SettingFilter() {
		t.Fatal("schema filter remains active after escape")
	}
	if got := model.schema.FilterValue(); got != "" {
		t.Fatalf("filter value = %q, want empty", got)
	}
	if got := model.schema.VisibleItems(); len(got) != 2 {
		t.Fatalf("visible items = %#v, want both tables", got)
	}
}

func TestFocus_numeric_keys_switch_between_tables_and_tabs(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusSchema)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusWorkspace)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusChat)
}

func TestFocus_tab_and_brackets_cycle_panes(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus = stateReady, focusSchema
	model.SetAI(fakeChatClient{}, nil)

	// When — Tab forward cycles to workspace
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusWorkspace)

	// When — Tab forward cycles to query log
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusQueryLog)

	// When — Tab forward moves to chat
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusChat)

	// When — Tab forward wraps to schema
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusSchema)

	// When — `]` also cycles forward
	updated, _ = model.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusWorkspace)

	// When — Shift+Tab cycles backward
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusSchema)

	// When — `[` also cycles backward
	updated, _ = model.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusChat)
}

func TestResults_jk_and_arrows_move_the_selected_row(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus = focusSchema
	model.schema.SetItems([]list.Item{schemaItem{title: "main", root: true}})
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"ID"},
		Rows: [][]*string{
			{stringPointer("1")},
			{stringPointer("2")},
			{stringPointer("3")},
		},
	}})
	model = updated.(Model)

	// When
	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 0},
		{key: tea.KeyPressMsg{Code: tea.KeyDown}, want: 1},
		{key: tea.KeyPressMsg{Code: tea.KeyUp}, want: 0},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 0},
	} {
		updated, _ = model.Update(test.key)
		model = updated.(Model)

		// Then
		if got := model.results.Cursor(); got != test.want {
			t.Fatalf("result cursor = %d, want %d after %s", got, test.want, test.key.String())
		}
	}
}

func TestResults_left_and_right_select_wide_table_cells_without_changing_row(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 100, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"first column", "second column", "third column", "fourth column", "fifth column"},
		Rows:    [][]*string{{stringPointer(strings.Repeat("first ", 20)), stringPointer("second value that is wide enough to exceed the viewport"), stringPointer("third value"), stringPointer("fourth value"), stringPointer("fifth value")}},
	}})
	model = updated.(Model)
	initialRow := model.results.Cursor()
	view := tableViewportView(model.results, model.resultsOffset, model.tableViewportWidth)
	if got, want := len(strings.Split(view, "\n")), model.results.Height()+1; got != want {
		t.Fatalf("rendered result lines = %d, want fixed viewport height %d", got, want)
	}
	for index, line := range strings.Split(view, "\n") {
		if got := ansi.StringWidth(line); got > model.tableViewportWidth {
			t.Fatalf("rendered line %d width = %d, exceeds viewport %d: %q", index, got, model.tableViewportWidth, line)
		}
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)

	// Then
	if got, want := model.resultsColumn, 1; got != want {
		t.Fatalf("selected result column = %d, want %d after right", got, want)
	}
	if model.resultsOffset == 0 {
		t.Fatal("right-selected result column was not revealed")
	}
	if got := model.results.Cursor(); got != initialRow {
		t.Fatalf("result cursor = %d, want %d after right", got, initialRow)
	}
	if got := tableViewportView(model.results, model.resultsOffset, model.tableViewportWidth); got == view {
		t.Fatal("right-scrolled result viewport did not change")
	}
	if got := model.results.View(); got == tableViewportView(model.results, model.resultsOffset, model.tableViewportWidth) {
		t.Fatal("wide result table did not require a horizontal viewport")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updated.(Model)

	// Then
	if got, want := model.resultsColumn, 0; got != want {
		t.Fatalf("selected result column = %d, want %d after h", got, want)
	}
	if got := model.resultsOffset; got != 0 {
		t.Fatalf("results offset = %d, want 0 after selecting the first column", got)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got, want := model.resultsColumn, 1; got != want {
		t.Fatalf("selected result column = %d, want %d after l", got, want)
	}
}

func TestQueryLog_l_selects_history_cells_without_changing_row(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	model.appendQueryLog(queryLogEntry{statement: strings.Repeat("select a very long query ", 20)})
	model.appendQueryLog(queryLogEntry{statement: "select 2"})
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)
	model.queryLog.SetCursor(1)
	initialRow := model.queryLog.Cursor()
	view := model.queryLogContentView()

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got := model.queryLog.Cursor(); got != initialRow {
		t.Fatalf("query log cursor = %d, want %d after l", got, initialRow)
	}
	if got, want := model.queryLogColumn, 1; got != want {
		t.Fatalf("selected query-log column = %d, want %d after l", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updated.(Model)

	// Then
	if got := model.queryLog.Cursor(); got != initialRow {
		t.Fatalf("query log cursor = %d, want %d after h", got, initialRow)
	}
	if got, want := model.queryLogColumn, 0; got != want {
		t.Fatalf("selected query-log column = %d, want %d after h", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	model = updated.(Model)

	// Then
	if got := model.queryLogContentView(); got != view {
		t.Fatal("query-log cell motion changed the viewport after returning left")
	}
}

func TestResults_l_scrolls_after_returning_to_SQL(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	model.formMode.beginInsert(model.editor)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"first column", "second column", "third column", "fourth column"},
		Rows:    [][]*string{{stringPointer(strings.Repeat("first ", 20)), stringPointer("second value that is wide enough to overflow viewport"), stringPointer("third value"), stringPointer("fourth value")}},
	}})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got, want := model.Tab, tabSQL; got != want {
		t.Fatalf("tab = %v, want %v", got, want)
	}
	if got, want := model.resultsColumn, 1; got != want {
		t.Fatalf("selected result column = %d, want %d after l", got, want)
	}
	if model.resultsOffset == 0 {
		t.Fatal("right-selected result column was not revealed")
	}
}

func TestResults_l_scrolls_a_visible_distance(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"first column", "second column", "third column", "fourth column"},
		Rows:    [][]*string{{stringPointer(strings.Repeat("first ", 20)), stringPointer("second value that is wide enough to overflow viewport"), stringPointer("third value"), stringPointer("fourth value")}},
	}})
	model = updated.(Model)
	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got, want := model.resultsColumn, 1; got != want {
		t.Fatalf("selected result column = %d, want %d after l", got, want)
	}
	if model.resultsOffset == 0 {
		t.Fatal("right-selected result column was not revealed")
	}
}
func TestResults_l_scrolls_visible_empty_results(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	model.formMode.beginInsert(model.editor)
	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{
		Columns: []string{"first column", "second column", "third column", "fourth column", "fifth column", "sixth column", "seventh column", "eighth column"},
	}})
	model = updated.(Model)
	model.focusActiveTable()

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)

	// Then
	if got, want := model.resultsColumn, 1; got != want {
		t.Fatalf("selected result column = %d, want %d after l", got, want)
	}
}

func TestStructureAndBrowse_jk_and_arrows_move_the_selected_row(t *testing.T) {
	tests := []struct {
		name  string
		setup func(Model) Model
	}{
		{
			name: "structure",
			setup: func(model Model) Model {
				model.SelectedTable, model.Tab = "projects", tabStructure
				model.focusActiveTable()
				updated, _ := model.Update(tableInfoMsg{table: "projects", columns: []sqlite.ColumnInfo{{Name: "id"}, {Name: "name"}}})
				model = updated.(Model)
				model.structure.SetCursor(1)
				return model
			},
		},
		{
			name: "browse",
			setup: func(model Model) Model {
				model.SelectedTable, model.Tab = "projects", tabBrowse
				model.focusActiveTable()
				updated, _ := model.Update(browseTableMsg{table: "projects", page: model.BrowsePage, result: sqlite.Result{Columns: []string{"ID"}, Rows: [][]*string{{stringPointer("1")}, {stringPointer("2")}}}})
				model = updated.(Model)
				model.browse.SetCursor(1)
				return model
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := test.setup(readyModel(t))

			// When
			updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
			model = updated.(Model)

			// Then
			if test.name == "structure" && model.structure.Cursor() != 0 {
				t.Fatalf("structure cursor = %d, want 0 after k", model.structure.Cursor())
			}
			if test.name == "browse" && model.browse.Cursor() != 0 {
				t.Fatalf("browse cursor = %d, want 0 after k", model.browse.Cursor())
			}
		})
	}
}

func TestBrowse_next_stays_on_final_page(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.BrowsePage = "projects", tabBrowse, 2
	model.browseResult.HasMore = false
	model.focusActiveTable()

	// When
	updated, timer := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	updated, command := model.Update(browseDebounceMsg{tag: model.browsePageTag, delta: 1, table: "projects"})
	model = updated.(Model)

	// Then
	if timer == nil || command != nil || model.BrowsePage != 2 {
		t.Fatalf("next page = %d, commands = %t/%t, want final page without a load", model.BrowsePage, timer != nil, command != nil)
	}
}

func TestBrowse_debounces_navigation(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.Tab = "projects", tabBrowse
	model.browseResult.HasMore = true
	model.focusActiveTable()

	// When
	updated, firstTimer := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	updated, secondTimer := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	updated, staleCommand := model.Update(browseDebounceMsg{tag: 1, delta: 1, table: "projects"})
	model = updated.(Model)
	updated, loadCommand := model.Update(browseDebounceMsg{tag: model.browsePageTag, delta: 1, table: "projects"})
	model = updated.(Model)

	// Then
	if firstTimer == nil || secondTimer == nil || staleCommand != nil || loadCommand == nil || model.BrowsePage != 1 {
		t.Fatalf("page = %d, commands = %t/%t/%t/%t, want one debounced load for page 2", model.BrowsePage, firstTimer != nil, secondTimer != nil, staleCommand != nil, loadCommand != nil)
	}
}

func TestQuitDialog_plainQDoesNotQuitAndCtrlQOpensDialog(t *testing.T) {
	// Given
	model := readyModel(t)
	model.schema.SetItems([]list.Item{schemaItem{title: "main", root: true}})
	_, listCommand := model.schema.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if commandQuits(listCommand) {
		t.Fatal("schema list returned a quit command for plain q")
	}

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)

	// Then
	if commandQuits(command) {
		t.Fatal("plain q returned a quit command")
	}
	if model.quitDialog != nil {
		t.Fatal("plain q opened the quit dialog")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if model.quitDialog == nil {
		t.Fatal("ctrl+q did not open the quit dialog")
	}
}

func commandQuits(command tea.Cmd) bool {
	if command == nil {
		return false
	}
	switch message := command().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, child := range message {
			if commandQuits(child) {
				return true
			}
		}
	}
	return false
}

func updateFromCommand(model Model, command tea.Cmd) Model {
	if command == nil {
		return model
	}

	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, command := range batch {
			model = updateFromCommand(model, command)
		}
		return model
	}

	updated, _ := model.Update(message)
	return updated.(Model)
}

func assertFocus(t *testing.T, model Model, want focus) {
	t.Helper()
	if model.Focus != want {
		t.Fatalf("focus = %v, want %v", model.Focus, want)
	}
}

func assertTab(t *testing.T, model Model, want workspaceTab) {
	t.Helper()
	assertFocus(t, model, focusWorkspace)
	if model.Tab != want {
		t.Fatalf("tab = %v, want %v", model.Tab, want)
	}
}
