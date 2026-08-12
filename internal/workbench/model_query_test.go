package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

func startQuery(t *testing.T, model *Model) uint64 {
	t.Helper()
	requestID := model.StartQueryForTest(context.Background())
	if requestID == 0 {
		t.Fatal("starting test query")
	}
	return requestID
}

func TestExecute_success_message_populates_results(t *testing.T) {
	// Given
	model := readyModel(t)
	requestID := model.StartQueryForTest(context.Background())
	result := sqlite.Result{
		Columns:      []string{"name", "note"},
		Rows:         [][]*string{{stringPointer("projects"), nil}},
		RowsAffected: 1,
		Duration:     12 * time.Millisecond,
		Truncated:    true,
	}

	// When
	updated, command := model.Update(querySucceededMsg{requestID: requestID, statement: "INSERT INTO projects VALUES ('projects')", result: result})
	model = updated.(Model)

	// Then
	if command == nil {
		t.Fatal("success message did not schedule SQL revalidation")
	}
	if _, ok := command().(sqlValidationTickMsg); !ok {
		t.Fatal("success message returned an unexpected command")
	}
	if model.Running() {
		t.Fatal("success message did not clear active request state")
	}
	if got := model.queryLog.results.Rows(); len(got) != 1 || got[0][0] != "projects" || got[0][1] != "NULL" {
		t.Fatalf("result rows = %#v, want populated sanitized cells", got)
	}
	if !strings.Contains(model.queryLog.resultsStatus, "1 row affected") || !strings.Contains(model.queryLog.resultsStatus, "truncated") {
		t.Fatalf("result status = %q, want row count and truncation", model.queryLog.resultsStatus)
	}
	if got, want := model.queryLog.component.Entries[0].Message, "inserted 1 row"; got != want {
		t.Fatalf("query log message = %q, want %q", got, want)
	}
}

func TestMessages_empty_metadata_replaces_prior_headers(t *testing.T) {
	t.Run("results", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.queryLog.results.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.queryLog.results.SetRows([]table.Row{{"prior", "row"}})
		requestID := startQuery(t, &model)

		// When
		updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{}})
		model = updated.(Model)

		// Then
		assertResultsPlaceholder(t, model.queryLog.results)
	})

	t.Run("browse", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.SelectedTable = "projects"
		model.browse.table.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.browse.table.SetRows([]table.Row{{"prior", "row"}})

		// When
		updated, _ := model.Update(browseTableMsg{table: "projects", page: 0, result: sqlite.Result{}})
		model = updated.(Model)

		// Then
		assertResultsPlaceholder(t, model.browse.table)
	})
}

func TestSchema_enter_defers_browse_until_browse_tab_is_focused(t *testing.T) {
	// Given
	model := readyModel(t)
	if _, err := model.Database.Execute(context.Background(), `CREATE TABLE "project's" (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
	if _, err := model.Database.Execute(context.Background(), `INSERT INTO "project's" (name) VALUES ('first')`); err != nil {
		t.Fatalf("creating fixture row: %v", err)
	}
	model.Focus = focusSchema
	model.schema.list.SetItems([]list.Item{schemaItem{title: "project's", description: "table"}})

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then
	if command == nil || model.Focus != focusWorkspace || model.Tab != tabStructure || model.SelectedTable != "project's" {
		t.Fatalf("schema selection = focus:%v tab:%v table:%q command:%t", model.Focus, model.Tab, model.SelectedTable, command != nil)
	}
	model = updateFromCommand(model, command)

	// Then
	if got := model.structure.table.Rows(); len(got) != 2 || got[0][0] != "id" || got[0][2] != "INTEGER" {
		t.Fatalf("structure rows = %#v, want selected table columns", got)
	}
	if got := model.browse.table.Rows(); len(got) != 0 {
		t.Fatalf("browse rows = %#v, want no query before Browse tab focus", got)
	}
	if got := len(model.queryLog.component.Entries); got != 0 {
		t.Fatalf("query log entries = %d, want no browse query before Browse tab focus", got)
	}

	// When
	updated, command = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// Then
	if model.Tab != tabBrowse {
		t.Fatalf("tab = %v, want Browse", model.Tab)
	}
	if got := model.browse.table.Rows(); len(got) != 1 || got[0][1] != "first" {
		t.Fatalf("browse rows = %#v, want selected table data", got)
	}
}

func TestSchema_enter_lands_on_configured_target_tab(t *testing.T) {
	// Given — config points table selection at the Browse tab.
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{TableOpenTarget: "browse"})

	model := readyModel(t)
	if _, err := model.Database.Execute(context.Background(), `CREATE TABLE "project's" (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
	if _, err := model.Database.Execute(context.Background(), `INSERT INTO "project's" (name) VALUES ('first')`); err != nil {
		t.Fatalf("creating fixture row: %v", err)
	}
	model.Focus = focusSchema
	model.schema.list.SetItems([]list.Item{schemaItem{title: "project's", description: "table"}})

	// When — select the table.
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — focus lands on the configured Browse tab.
	if command == nil || model.Focus != focusWorkspace || model.Tab != tabBrowse || model.SelectedTable != "project's" {
		t.Fatalf("schema selection = focus:%v tab:%v table:%q command:%t", model.Focus, model.Tab, model.SelectedTable, command != nil)
	}
	model = updateFromCommand(model, command)

	// Then — the browse query ran immediately, no tab toggle needed.
	if got := model.browse.table.Rows(); len(got) != 1 || got[0][1] != "first" {
		t.Fatalf("browse rows = %#v, want selected table data", got)
	}
}

func TestMessages_populated_metadata_replaces_prior_rows(t *testing.T) {
	t.Run("results", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.queryLog.results.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.queryLog.results.SetRows([]table.Row{{"prior", "row"}})
		requestID := startQuery(t, &model)

		// When
		updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{Columns: []string{"ID", "Name", "State"}, Rows: [][]*string{{stringPointer("2"), stringPointer("next"), stringPointer("ready")}}}})
		model = updated.(Model)

		// Then
		columns := model.queryLog.results.Columns()
		if len(columns) != 3 || columns[0].Title != "ID" || columns[1].Title != "Name" || columns[2].Title != "State" {
			t.Fatalf("result columns = %#v, want ID, Name, State", columns)
		}
		if got := model.queryLog.results.Rows(); len(got) != 1 || got[0][0] != "2" || got[0][1] != "next" || got[0][2] != "ready" {
			t.Fatalf("result rows = %#v, want replacement row", got)
		}
	})

	t.Run("browse", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.SelectedTable = "projects"
		model.browse.table.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.browse.table.SetRows([]table.Row{{"prior", "row"}})

		// When
		updated, _ := model.Update(browseTableMsg{table: "projects", page: 0, result: sqlite.Result{Columns: []string{"ID", "Name", "State"}, Rows: [][]*string{{stringPointer("2"), stringPointer("next"), stringPointer("ready")}}}})
		model = updated.(Model)

		// Then
		columns := model.browse.table.Columns()
		if len(columns) != 3 || columns[0].Title != "ID" || columns[1].Title != "Name" || columns[2].Title != "State" {
			t.Fatalf("browse columns = %#v, want ID, Name, State", columns)
		}
		if got := model.browse.table.Rows(); len(got) != 1 || got[0][0] != "2" || got[0][1] != "next" || got[0][2] != "ready" {
			t.Fatalf("browse rows = %#v, want replacement row", got)
		}
	})
}

func TestBrowse_status_shows_position_within_page(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.BrowsePage = "projects", 1
	rows := make([][]*string, defaultBrowsePageSize)
	// Give the table rows first so the cursor lands on row 7 of the page.
	model.browse.table.SetRows(make([]table.Row, defaultBrowsePageSize))
	model.browse.table.SetCursor(6)

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 1, result: sqlite.Result{Rows: rows, HasMore: true}})
	model = updated.(Model)

	// Then
	if got, want := model.browse.status, "projects | 26-50 | 7/25 | page 2"; got != want {
		t.Fatalf("browse status = %q, want %q", got, want)
	}
}

func TestBrowse_status_fresh_load_reports_first_position(t *testing.T) {
	// Given — a fresh table with no rows yet (cursor -1) loading a
	// nonempty first page.
	model := readyModel(t)
	model.SelectedTable, model.BrowsePage = "projects", 0
	rows := make([][]*string, defaultBrowsePageSize)

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 0, result: sqlite.Result{Rows: rows, HasMore: true}})
	model = updated.(Model)

	// Then — the position never reads 0 of N on a nonempty page.
	if got, want := model.browse.status, "projects | 1-25 | 1/25 | page 1"; got != want {
		t.Fatalf("browse status = %q, want %q", got, want)
	}
}

func TestBrowse_status_empty_page_reports_zero_position(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.BrowsePage = "projects", 2

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 2, result: sqlite.Result{Rows: nil, HasMore: false}})
	model = updated.(Model)

	// Then
	if got, want := model.browse.status, "projects | 0-50 | 0/0 | page 3"; got != want {
		t.Fatalf("browse status = %q, want %q", got, want)
	}
}

func TestExecute_error_message_retains_prior_results(t *testing.T) {
	// Given
	model := readyModel(t)
	model.queryLog.results.SetRows([]table.Row{{"prior"}})
	requestID := startQuery(t, &model)

	// When
	updated, command := model.Update(queryFailedMsg{requestID: requestID, err: errors.New("near \"bad\": syntax error")})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("error message returned an unexpected command")
	}
	if model.Running() {
		t.Fatal("error message did not clear active request state")
	}
	if got := model.queryLog.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("error replaced prior rows: %#v", got)
	}
	if len(model.queryLog.component.Entries) == 0 {
		t.Fatal("no query log entry recorded for failure")
	}
	if got, want := model.queryLog.component.Entries[0].Status, "failed"; got != want {
		t.Fatalf("query log status = %q, want %q", got, want)
	}
	if got, want := model.queryLog.component.Entries[0].Message, "near \"bad\": syntax error"; got != want {
		t.Fatalf("query log message = %q, want %q", got, want)
	}
}

func TestExecute_cancellation_rejects_later_success(t *testing.T) {
	// Given
	model := readyModel(t)
	model.queryLog.results.SetRows([]table.Row{{"prior"}})
	requestID := startQuery(t, &model)
	model.CancelQuery()

	// When
	updated, command := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{Rows: [][]*string{{stringPointer("late")}}}})
	model = updated.(Model)

	// Then
	if command == nil {
		t.Fatal("canceled request success did not schedule SQL revalidation")
	}
	if _, ok := command().(sqlValidationTickMsg); !ok {
		t.Fatal("canceled request success returned an unexpected command")
	}
	if model.Running() {
		t.Fatal("canceled request success did not clear active request state")
	}
	if got := model.queryLog.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("late success replaced prior rows: %#v", got)
	}
	if len(model.queryLog.component.Entries) == 0 {
		t.Fatal("no query log entry recorded for cancellation")
	}
	if got, want := model.queryLog.component.Entries[0].Status, "canceled"; got != want {
		t.Fatalf("query log status = %q, want %q", got, want)
	}
}

func TestExecute_stale_older_request_message_is_ignored(t *testing.T) {
	// Given
	model := readyModel(t)
	model.queryLog.results.SetRows([]table.Row{{"prior"}})
	requestID := startQuery(t, &model)

	// When
	updated, command := model.Update(querySucceededMsg{requestID: requestID - 1, result: sqlite.Result{Rows: [][]*string{{stringPointer("stale")}}}})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("stale message returned an unexpected command")
	}
	if !model.Running() {
		t.Fatal("stale message changed active request state")
	}
	if got := model.queryLog.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("stale success replaced prior rows: %#v", got)
	}
}

func TestExecute_keys_start_a_nonblank_query(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "ctrl enter", key: tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}},
		{name: "ctrl s", key: tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}},
		{name: "f5", key: tea.KeyPressMsg{Code: tea.KeyF5}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := readyModel(t)
			model.queryLog.editor.setValue("SELECT 1")

			// When
			updated, command := model.Update(test.key)
			model = updated.(Model)

			// Then
			if command == nil || !model.Running() {
				t.Fatalf("execute key did not start query: command=%v running=%t", command != nil, model.Running())
			}
			success, ok := firstQuerySucceeded(executeCommandAll(command))
			if !ok || success.requestID != 1 {
				t.Fatalf("execute command messages = %#v, want success for request 1", executeCommandAll(command))
			}
			updated, _ = model.Update(success)
			model = updated.(Model)
			if model.Running() || len(model.queryLog.results.Rows()) != 1 {
				t.Fatalf("execute command did not complete: running=%t rows=%#v", model.Running(), model.queryLog.results.Rows())
			}
		})
	}
}

func TestExecute_history_recall_cycles_executed_statements(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.queryLog.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		model = driveCommand(model, command)
	}
	model.appendQueryLog(queryLogEntry{Statement: "SELECT * FROM projects"})
	model.queryLog.editor.setValue("")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first recalled statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second recalled statement = %q, want %q", got, want)
	}

	// When — past the oldest entry recall must not wrap.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 1"; got != want {
		t.Fatalf("third recalled statement = %q, want %q (no wrap)", got, want)
	}
}

func TestExecute_history_arrow_recall_and_edit_exit(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.queryLog.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		model = driveCommand(model, command)
	}
	model.queryLog.editor.setValue("")
	model.Focus, model.Tab = focusWorkspace, tabSQL
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}) // enter insert
	model = updated.(Model)
	press := func(code rune) {
		t.Helper()
		text := ""
		if code > 0 && code < 128 {
			text = string(rune(code))
		}
		updated, _ := model.Update(tea.KeyPressMsg{Code: code, Text: text})
		model = updated.(Model)
	}

	// When — Up recalls newest, then older, then stops at the oldest.
	press(tea.KeyUp)
	if got, want := model.queryLog.editor.value, "SELECT 2"; got != want {
		t.Fatalf("Up from blank = %q, want %q", got, want)
	}
	press(tea.KeyUp)
	if got, want := model.queryLog.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second Up = %q, want %q", got, want)
	}
	press(tea.KeyUp)
	if got, want := model.queryLog.editor.value, "SELECT 1"; got != want {
		t.Fatalf("third Up = %q, want %q (oldest boundary, no wrap)", got, want)
	}

	// When — Down recalls newer, then clears.
	press(tea.KeyDown)
	if got, want := model.queryLog.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first Down = %q, want %q", got, want)
	}
	press(tea.KeyDown)
	if got, want := model.queryLog.editor.value, ""; got != want {
		t.Fatalf("second Down = %q, want cleared editor", got)
	}

	// When — Up on a non-empty editor must not replace it.
	model.queryLog.editor.setValue("my query")
	model.overlay.formMode.beginInsert(model.queryLog.editor)
	press(tea.KeyUp)
	if got, want := model.queryLog.editor.value, "my query"; got != want {
		t.Fatalf("Up on non-empty editor = %q, want %q", got, want)
	}

	// When — a value-changing edit after recall exits recall mode.
	model.queryLog.editor.setValue("")
	press(tea.KeyUp) // recall SELECT 2
	if got, want := model.queryLog.editor.value, "SELECT 2"; got != want {
		t.Fatalf("recalled statement = %q, want %q", got, want)
	}
	press('x') // edit the recalled text
	if got, want := model.queryLog.editor.value, "SELECT 2x"; got != want {
		t.Fatalf("edited value = %q, want %q", got, want)
	}
	press(tea.KeyDown) // must not overwrite the edited text
	if got, want := model.queryLog.editor.value, "SELECT 2x"; got != want {
		t.Fatalf("Down after edit = %q, want %q (recall exited)", got, want)
	}
}

func TestExecute_destructive_statement_requires_confirmation(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 80, 24)
	model.queryLog.editor.setValue("CREATE TABLE projects (id INTEGER PRIMARY KEY)")

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// Then
	if model.Running() || model.overlay.queryConfirmation == nil {
		t.Fatalf("destructive query = running:%t confirmation:%t, want confirmation before execution", model.Running(), model.overlay.queryConfirmation != nil)
	}
}

func TestExecute_destructive_statement_declined_does_not_run(t *testing.T) {
	// Given
	model := readyModel(t)
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE projects (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating fixture table: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "INSERT INTO projects VALUES (1)"); err != nil {
		t.Fatalf("creating fixture row: %v", err)
	}
	model.queryLog.editor.setValue("DELETE FROM projects")
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// When
	result, err := model.Database.Execute(model.appContext, "SELECT COUNT(*) FROM projects")
	if err != nil {
		t.Fatalf("counting fixture rows: %v", err)
	}
	if got := *result.Rows[0][0]; got != "1" {
		t.Fatalf("row count = %q, want 1 after declined delete", got)
	}
}

func TestExecute_destructive_statement_clickingYes_runsQuery(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 80, 24)
	model.queryLog.editor.setValue("CREATE TABLE projects (id INTEGER PRIMARY KEY)")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	dialog := model.overlay.queryConfirmation.dialog
	layout := dialog.layout(model.layout.width, model.layout.height)

	// When
	updated, command := model.Update(tea.MouseClickMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseLeft})
	model = updated.(Model)

	// Then
	if command == nil || !model.Running() || model.overlay.queryConfirmation != nil {
		t.Fatalf("click confirmation = command:%t running:%t confirmation:%t, want query running", command != nil, model.Running(), model.overlay.queryConfirmation != nil)
	}
}

// TestExecute_destructive_fromInsertMode_Enter_confirms exercises:
// SQL tab in insert mode → F5 on destructive query → queryConfirmation appears
// → Enter. Enter must complete the dialog, not let formMode.route() consume it.
// formMode must remain insert mode (dialog processing never changes it).
func TestExecute_destructive_fromInsertMode_Enter_confirms(t *testing.T) {
	// Given — SQL tab in insert mode with destructive query
	model := readyModel(t)
	model = resizeModel(model, 80, 24)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	model.queryLog.editor.setValue("CREATE TABLE projects (id INTEGER PRIMARY KEY)")

	// When — F5 opens queryConfirmation
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	if model.overlay.queryConfirmation == nil {
		t.Fatal("F5 did not open query confirmation")
	}
	if model.overlay.formMode.mode != formModeInsert {
		t.Fatalf("form mode = %d, want insert after confirmation opened", model.overlay.formMode.mode)
	}

	// When — Enter confirms the dialog
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — dialog completed, query running, formMode unchanged
	if model.overlay.queryConfirmation != nil {
		t.Fatal("Enter did not complete the query confirmation dialog")
	}
	if command == nil || !model.Running() {
		t.Fatal("Enter did not confirm the destructive query")
	}
	if model.overlay.formMode.mode != formModeInsert {
		t.Fatalf("Enter on confirmation changed form mode to %d, want insert", model.overlay.formMode.mode)
	}
}

func TestExecute_history_recall_cycles_executed_statements_merged_2(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.queryLog.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		model = driveCommand(model, command)
	}
	model.appendQueryLog(queryLogEntry{Statement: "SELECT * FROM projects"})
	model.queryLog.editor.setValue("")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first recalled statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second recalled statement = %q, want %q", got, want)
	}
}

func TestExecute_history_recall_cycles_executed_statements_merged_3(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.queryLog.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		model = driveCommand(model, command)
	}
	model.appendQueryLog(queryLogEntry{Statement: "SELECT * FROM projects"})
	model.queryLog.editor.setValue("")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first recalled statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second recalled statement = %q, want %q", got, want)
	}
}

func TestExecute_ignores_blank_and_repeated_requests(t *testing.T) {
	// Given
	model := readyModel(t)

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	// Then
	if command != nil || model.Running() {
		t.Fatalf("blank query started: command=%v running=%t", command != nil, model.Running())
	}

	// Given
	model.queryLog.editor.setValue("SELECT 1")
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	if command == nil || !model.Running() {
		t.Fatal("initial query did not start")
	}

	// When
	updated, repeated := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if repeated != nil || !model.Running() {
		t.Fatalf("repeated query changed active request: command=%v running=%t", repeated != nil, model.Running())
	}
}

func TestExecute_ctrlC_waits_for_matching_cancellation_before_quitting(t *testing.T) {
	// Given
	model := readyModel(t)
	requestID := startQuery(t, &model)

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	assertOnlyNotificationTick(t, command)
	if !model.Running() {
		t.Fatalf("ctrl+c did not defer quit and cancel: running=%t", model.Running())
	}

	// When
	updated, command = model.Update(queryCanceledMsg{requestID: requestID})
	model = updated.(Model)

	// Then
	if command == nil {
		t.Fatal("matching cancellation did not release pending quit")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("pending quit command message = %T, want tea.QuitMsg", command())
	}
}

func assertResultsPlaceholder(t *testing.T, resultTable table.Model) {
	t.Helper()
	columns := resultTable.Columns()
	if got, want := len(columns), 1; got != want {
		t.Fatalf("column count = %d, want %d", got, want)
	}
	if got, want := columns[0].Title, "Results"; got != want {
		t.Fatalf("column title = %q, want %q", got, want)
	}
	if got := resultTable.Rows(); len(got) != 0 {
		t.Fatalf("rows = %#v, want no rows", got)
	}
}

func TestSQL_y_yanks_focused_cell_value(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	requestID := startQuery(t, &model)
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 'test'", result: sqlite.Result{
		Columns: []string{"name", "note"},
		Rows: [][]*string{{
			stringPointer("projects"),
			nil,
		}},
		UntruncatedRows: [][]*string{{
			stringPointer("projects"),
			nil,
		}},
	}})
	model = updated.(Model)
	model.Focus = focusWorkspace
	model.Tab = tabSQL
	model.layout.resultsColumn = 1
	model.queryLog.results.Focus()

	// When — y yanks the selected cell
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — nil cell yields empty string, status set, command returned
	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("expected copy command")
	}

	// When — yank another cell with a value
	model.layout.resultsColumn = 0
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("expected copy command")
	}
}

func TestSQL_y_ignored_without_focus_or_during_edit(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	requestID := startQuery(t, &model)
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 'test'", result: sqlite.Result{
		Columns:         []string{"name"},
		Rows:            [][]*string{{stringPointer("projects")}},
		UntruncatedRows: [][]*string{{stringPointer("projects")}},
	}})
	model = updated.(Model)
	model.Focus = focusWorkspace
	model.Tab = tabSQL

	// When — results not focused
	model.queryLog.results.Blur()
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — no status change
	if model.Status == "copied to clipboard" {
		t.Fatal("y copied without focused results")
	}

	// When — editor is editing
	model.overlay.formMode = &formModeController{mode: formModeInsert}
	model.queryLog.results.Focus()
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — no status change
	if model.Status == "copied to clipboard" {
		t.Fatal("y copied while editing form")
	}
}
