package workbench

import (
	"context"
	"errors"
	"slices"
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
	if command != nil {
		t.Fatal("success message returned an unexpected command")
	}
	if model.Running() {
		t.Fatal("success message did not clear active request state")
	}
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "projects" || got[0][1] != "NULL" {
		t.Fatalf("result rows = %#v, want populated sanitized cells", got)
	}
	if !strings.Contains(model.resultsStatus, "1 row affected") || !strings.Contains(model.resultsStatus, "truncated") {
		t.Fatalf("result status = %q, want row count and truncation", model.resultsStatus)
	}
	if got, want := model.queryLogEntries[0].message, "inserted 1 row"; got != want {
		t.Fatalf("query log message = %q, want %q", got, want)
	}
}

func TestMessages_empty_metadata_replaces_prior_headers(t *testing.T) {
	t.Run("results", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.results.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.results.SetRows([]table.Row{{"prior", "row"}})
		requestID := startQuery(t, &model)

		// When
		updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{}})
		model = updated.(Model)

		// Then
		assertResultsPlaceholder(t, model.results)
	})

	t.Run("browse", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.SelectedTable = "projects"
		model.browse.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.browse.SetRows([]table.Row{{"prior", "row"}})

		// When
		updated, _ := model.Update(browseTableMsg{table: "projects", page: 0, result: sqlite.Result{}})
		model = updated.(Model)

		// Then
		assertResultsPlaceholder(t, model.browse)
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
	model.schema.SetItems([]list.Item{schemaItem{title: "project's", description: "table"}})

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then
	if command == nil || model.Focus != focusWorkspace || model.Tab != tabStructure || model.SelectedTable != "project's" {
		t.Fatalf("schema selection = focus:%v tab:%v table:%q command:%t", model.Focus, model.Tab, model.SelectedTable, command != nil)
	}
	model = updateFromCommand(model, command)

	// Then
	if got := model.structure.Rows(); len(got) != 2 || got[0][0] != "id" || got[0][2] != "INTEGER" {
		t.Fatalf("structure rows = %#v, want selected table columns", got)
	}
	if got := model.browse.Rows(); len(got) != 0 {
		t.Fatalf("browse rows = %#v, want no query before Browse tab focus", got)
	}
	if got := len(model.queryLogEntries); got != 0 {
		t.Fatalf("query log entries = %d, want no browse query before Browse tab focus", got)
	}

	// When
	updated, command = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// Then
	if model.Tab != tabBrowse {
		t.Fatalf("tab = %v, want Browse", model.Tab)
	}
	if got := model.browse.Rows(); len(got) != 1 || got[0][1] != "first" {
		t.Fatalf("browse rows = %#v, want selected table data", got)
	}
}

func TestMessages_populated_metadata_replaces_prior_rows(t *testing.T) {
	t.Run("results", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.results.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.results.SetRows([]table.Row{{"prior", "row"}})
		requestID := startQuery(t, &model)

		// When
		updated, _ := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{Columns: []string{"ID", "Name", "State"}, Rows: [][]*string{{stringPointer("2"), stringPointer("next"), stringPointer("ready")}}}})
		model = updated.(Model)

		// Then
		columns := model.results.Columns()
		if len(columns) != 3 || columns[0].Title != "ID" || columns[1].Title != "Name" || columns[2].Title != "State" {
			t.Fatalf("result columns = %#v, want ID, Name, State", columns)
		}
		if got := model.results.Rows(); len(got) != 1 || got[0][0] != "2" || got[0][1] != "next" || got[0][2] != "ready" {
			t.Fatalf("result rows = %#v, want replacement row", got)
		}
	})

	t.Run("browse", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.SelectedTable = "projects"
		model.browse.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.browse.SetRows([]table.Row{{"prior", "row"}})

		// When
		updated, _ := model.Update(browseTableMsg{table: "projects", page: 0, result: sqlite.Result{Columns: []string{"ID", "Name", "State"}, Rows: [][]*string{{stringPointer("2"), stringPointer("next"), stringPointer("ready")}}}})
		model = updated.(Model)

		// Then
		columns := model.browse.Columns()
		if len(columns) != 3 || columns[0].Title != "ID" || columns[1].Title != "Name" || columns[2].Title != "State" {
			t.Fatalf("browse columns = %#v, want ID, Name, State", columns)
		}
		if got := model.browse.Rows(); len(got) != 1 || got[0][0] != "2" || got[0][1] != "next" || got[0][2] != "ready" {
			t.Fatalf("browse rows = %#v, want replacement row", got)
		}
	})
}

func TestBrowse_status_shows_current_batch_without_total(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.BrowsePage = "projects", 1
	rows := make([][]*string, browsePageSize)

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 1, result: sqlite.Result{Rows: rows, HasMore: true}})
	model = updated.(Model)

	// Then
	if got, want := model.browseStatus, "projects | 26-50"; got != want {
		t.Fatalf("browse status = %q, want %q", got, want)
	}
}

func TestExecute_error_message_retains_prior_results(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetRows([]table.Row{{"prior"}})
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
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("error replaced prior rows: %#v", got)
	}
	if len(model.queryLogEntries) == 0 {
		t.Fatal("no query log entry recorded for failure")
	}
	if got, want := model.queryLogEntries[0].status, "failed"; got != want {
		t.Fatalf("query log status = %q, want %q", got, want)
	}
	if got, want := model.queryLogEntries[0].message, "near \"bad\": syntax error"; got != want {
		t.Fatalf("query log message = %q, want %q", got, want)
	}
}

func TestExecute_cancellation_rejects_later_success(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetRows([]table.Row{{"prior"}})
	requestID := startQuery(t, &model)
	model.CancelQuery()

	// When
	updated, command := model.Update(querySucceededMsg{requestID: requestID, result: sqlite.Result{Rows: [][]*string{{stringPointer("late")}}}})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("canceled request success returned an unexpected command")
	}
	if model.Running() {
		t.Fatal("canceled request success did not clear active request state")
	}
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("late success replaced prior rows: %#v", got)
	}
	if len(model.queryLogEntries) == 0 {
		t.Fatal("no query log entry recorded for cancellation")
	}
	if got, want := model.queryLogEntries[0].status, "canceled"; got != want {
		t.Fatalf("query log status = %q, want %q", got, want)
	}
}

func TestExecute_stale_older_request_message_is_ignored(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetRows([]table.Row{{"prior"}})
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
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
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
			model.editor.setValue("SELECT 1")

			// When
			updated, command := model.Update(test.key)
			model = updated.(Model)

			// Then
			if command == nil || !model.Running() {
				t.Fatalf("execute key did not start query: command=%v running=%t", command != nil, model.Running())
			}
			message := command()
			success, ok := message.(querySucceededMsg)
			if !ok || success.requestID != 1 {
				t.Fatalf("execute command message = %#v, want success for request 1", message)
			}
			updated, _ = model.Update(success)
			model = updated.(Model)
			if model.Running() || len(model.results.Rows()) != 1 {
				t.Fatalf("execute command did not complete: running=%t rows=%#v", model.Running(), model.results.Rows())
			}
		})
	}
}

func TestExecute_history_recall_cycles_executed_statements(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		updated, _ = model.Update(command())
		model = updated.(Model)
	}
	model.appendQueryLog(queryLogEntry{statement: "SELECT * FROM projects"})
	model.editor.setValue("")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first recalled statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second recalled statement = %q, want %q", got, want)
	}
}

func TestSavedQueries_save_and_select_restores_editor(t *testing.T) {
	// Given
	model := readyModel(t)
	model.savedQueriesPath = t.TempDir() + "/saved-queries.json"
	model.editor.setValue("SELECT name FROM projects")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	model = updated.(Model)
	if got, want := model.Status, "saved query"; got != want {
		t.Fatalf("save status = %q, want %q", got, want)
	}
	model.editor.setValue("SELECT changed")
	updated, command := model.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	model = updated.(Model)
	if model.savedQueryPicker == nil {
		t.Fatal("open saved queries did not show a picker")
	}
	model = updateFromCommand(model, command)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = resolveYankCommand(model, command)

	// Then
	if got, want := model.editor.value, "SELECT name FROM projects"; got != want {
		t.Fatalf("selected saved statement = %q, want %q", got, want)
	}
}

func TestSavedQueries_persistAndReload(t *testing.T) {
	// Given
	path := t.TempDir() + "/saved-queries.json"
	model := readyModel(t)
	model.savedQueriesPath = path
	model.editor.setValue("SELECT name FROM projects")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	model = updated.(Model)
	reloaded := loadSavedQueries(path)

	// Then
	if got, want := reloaded, []string{"SELECT name FROM projects"}; !slices.Equal(got, want) {
		t.Fatalf("reloaded saved queries = %#v, want %#v", got, want)
	}
}

func TestExecute_destructive_statement_requires_confirmation(t *testing.T) {
	// Given
	model := readyModel(t)
	model = resizeModel(model, 80, 24)
	model.editor.setValue("CREATE TABLE projects (id INTEGER PRIMARY KEY)")

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// Then
	if model.Running() || model.queryConfirmation == nil {
		t.Fatalf("destructive query = running:%t confirmation:%t, want confirmation before execution", model.Running(), model.queryConfirmation != nil)
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
	model.editor.setValue("DELETE FROM projects")
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
	model.editor.setValue("CREATE TABLE projects (id INTEGER PRIMARY KEY)")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	dialog := model.queryConfirmation.dialog
	layout := dialog.layout(model.width, model.height)

	// When
	updated, command := model.Update(tea.MouseClickMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseLeft})
	model = updated.(Model)

	// Then
	if command == nil || !model.Running() || model.queryConfirmation != nil {
		t.Fatalf("click confirmation = command:%t running:%t confirmation:%t, want query running", command != nil, model.Running(), model.queryConfirmation != nil)
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
	model.editor.setValue("CREATE TABLE projects (id INTEGER PRIMARY KEY)")

	// When — F5 opens queryConfirmation
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	if model.queryConfirmation == nil {
		t.Fatal("F5 did not open query confirmation")
	}
	if model.formMode.mode != formModeInsert {
		t.Fatalf("form mode = %d, want insert after confirmation opened", model.formMode.mode)
	}

	// When — Enter confirms the dialog
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Then — dialog completed, query running, formMode unchanged
	if model.queryConfirmation != nil {
		t.Fatal("Enter did not complete the query confirmation dialog")
	}
	if command == nil || !model.Running() {
		t.Fatal("Enter did not confirm the destructive query")
	}
	if model.formMode.mode != formModeInsert {
		t.Fatalf("Enter on confirmation changed form mode to %d, want insert", model.formMode.mode)
	}
}

func TestExecute_history_recall_cycles_executed_statements_merged_2(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		updated, _ = model.Update(command())
		model = updated.(Model)
	}
	model.appendQueryLog(queryLogEntry{statement: "SELECT * FROM projects"})
	model.editor.setValue("")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first recalled statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second recalled statement = %q, want %q", got, want)
	}
}

func TestSavedQueries_save_and_select_restores_editor_merged_2(t *testing.T) {
	// Given
	model := readyModel(t)
	model.savedQueriesPath = t.TempDir() + "/saved-queries.json"
	model.editor.setValue("SELECT name FROM projects")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	model = updated.(Model)
	if got, want := model.Status, "saved query"; got != want {
		t.Fatalf("save status = %q, want %q", got, want)
	}
	model.editor.setValue("SELECT changed")
	updated, command := model.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	model = updated.(Model)
	if model.savedQueryPicker == nil {
		t.Fatal("open saved queries did not show a picker")
	}
	model = updateFromCommand(model, command)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = resolveYankCommand(model, command)

	// Then
	if got, want := model.editor.value, "SELECT name FROM projects"; got != want {
		t.Fatalf("selected saved statement = %q, want %q", got, want)
	}
}

func TestSavedQueries_persistAndReload_merged_2(t *testing.T) {
	// Given
	path := t.TempDir() + "/saved-queries.json"
	model := readyModel(t)
	model.savedQueriesPath = path
	model.editor.setValue("SELECT name FROM projects")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	model = updated.(Model)
	reloaded := loadSavedQueries(path)

	// Then
	if got, want := reloaded, []string{"SELECT name FROM projects"}; !slices.Equal(got, want) {
		t.Fatalf("reloaded saved queries = %#v, want %#v", got, want)
	}
}

func TestExecute_history_recall_cycles_executed_statements_merged_3(t *testing.T) {
	// Given
	model := readyModel(t)
	for _, statement := range []string{"SELECT 1", "SELECT 2"} {
		model.editor.setValue(statement)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
		model = updated.(Model)
		updated, _ = model.Update(command())
		model = updated.(Model)
	}
	model.appendQueryLog(queryLogEntry{statement: "SELECT * FROM projects"})
	model.editor.setValue("")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.value, "SELECT 2"; got != want {
		t.Fatalf("first recalled statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.value, "SELECT 1"; got != want {
		t.Fatalf("second recalled statement = %q, want %q", got, want)
	}
}

func TestSavedQueries_save_and_select_restores_editor_merged_3(t *testing.T) {
	// Given
	model := readyModel(t)
	model.savedQueriesPath = t.TempDir() + "/saved-queries.json"
	model.editor.setValue("SELECT name FROM projects")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	model = updated.(Model)
	if got, want := model.Status, "saved query"; got != want {
		t.Fatalf("save status = %q, want %q", got, want)
	}
	model.editor.setValue("SELECT changed")
	updated, command := model.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	model = updated.(Model)
	if model.savedQueryPicker == nil {
		t.Fatal("open saved queries did not show a picker")
	}
	model = updateFromCommand(model, command)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = resolveYankCommand(model, command)

	// Then
	if got, want := model.editor.value, "SELECT name FROM projects"; got != want {
		t.Fatalf("selected saved statement = %q, want %q", got, want)
	}
}

func TestSavedQueries_persistAndReload_merged_3(t *testing.T) {
	// Given
	path := t.TempDir() + "/saved-queries.json"
	model := readyModel(t)
	model.savedQueriesPath = path
	model.editor.setValue("SELECT name FROM projects")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	model = updated.(Model)
	reloaded := loadSavedQueries(path)

	// Then
	if got, want := reloaded, []string{"SELECT name FROM projects"}; !slices.Equal(got, want) {
		t.Fatalf("reloaded saved queries = %#v, want %#v", got, want)
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
	model.editor.setValue("SELECT 1")
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
	if command != nil || !model.Running() {
		t.Fatalf("ctrl+c did not defer quit and cancel: command=%v running=%t", command != nil, model.Running())
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
