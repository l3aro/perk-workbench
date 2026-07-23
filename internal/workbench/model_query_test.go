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
	"github.com/l3aro/perk/internal/sqlite"
)

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
	updated, command := model.Update(querySucceededMsg{requestID: requestID, result: result})
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
}

func TestMessages_empty_metadata_replaces_prior_headers(t *testing.T) {
	t.Run("results", func(t *testing.T) {
		// Given
		model := readyModel(t)
		model.results.SetColumns([]table.Column{{Title: "Previous", Width: 8}, {Title: "Columns", Width: 8}})
		model.results.SetRows([]table.Row{{"prior", "row"}})
		model.running, model.activeRequestID, model.cancel = true, 1, func() {}

		// When
		updated, _ := model.Update(querySucceededMsg{requestID: 1, result: sqlite.Result{}})
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

func TestSchema_enter_loads_selected_table_structure_and_browse(t *testing.T) {
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

	// When
	updated, _ = model.Update(tableInfoMsg{table: "project's", columns: []sqlite.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}, {Name: "name", Type: "TEXT", Nullable: true}}})
	model = updated.(Model)
	updated, _ = model.Update(browseTableMsg{table: "project's", page: 0, result: sqlite.Result{Columns: []string{"id", "name"}, Rows: [][]*string{{stringPointer("1"), stringPointer("first")}}}})
	model = updated.(Model)

	// Then
	if got := model.structure.Rows(); len(got) != 2 || got[0][0] != "id" || got[0][2] != "INTEGER" {
		t.Fatalf("structure rows = %#v, want selected table columns", got)
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
		model.running, model.activeRequestID, model.cancel = true, 1, func() {}

		// When
		updated, _ := model.Update(querySucceededMsg{requestID: 1, result: sqlite.Result{Columns: []string{"ID", "Name", "State"}, Rows: [][]*string{{stringPointer("2"), stringPointer("next"), stringPointer("ready")}}}})
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

func TestBrowse_status_shows_current_batch_and_total(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.BrowsePage = "projects", 1
	rows := make([][]*string, browsePageSize)

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 1, result: sqlite.Result{Rows: rows, TotalRows: 1000}})
	model = updated.(Model)

	// Then
	if got, want := model.browseStatus, "projects | 26-50 of 1,000"; got != want {
		t.Fatalf("browse status = %q, want %q", got, want)
	}
}

func TestExecute_error_message_retains_prior_results(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetRows([]table.Row{{"prior"}})
	model.running, model.activeRequestID = true, 2
	model.cancel = func() {}

	// When
	updated, command := model.Update(queryFailedMsg{requestID: 2, err: errors.New("near \"bad\": syntax error")})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("error message returned an unexpected command")
	}
	if model.running || model.cancel != nil {
		t.Fatal("error message did not clear active request state")
	}
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("error replaced prior rows: %#v", got)
	}
	if !strings.Contains(model.Status, "query failed") {
		t.Fatalf("error status = %q, want inline failure", model.Status)
	}
}

func TestExecute_cancellation_rejects_later_success(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetRows([]table.Row{{"prior"}})
	model.running, model.cancelRequested, model.activeRequestID = true, true, 3
	model.cancel = func() {}

	// When
	updated, command := model.Update(querySucceededMsg{requestID: 3, result: sqlite.Result{Rows: [][]*string{{stringPointer("late")}}}})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("canceled request success returned an unexpected command")
	}
	if model.running || model.cancel != nil {
		t.Fatal("canceled request success did not clear active request state")
	}
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "prior" {
		t.Fatalf("late success replaced prior rows: %#v", got)
	}
	if !strings.Contains(model.Status, "canceled") {
		t.Fatalf("late success status = %q, want cancellation", model.Status)
	}
}

func TestExecute_stale_older_request_message_is_ignored(t *testing.T) {
	// Given
	model := readyModel(t)
	model.results.SetRows([]table.Row{{"prior"}})
	model.running, model.activeRequestID = true, 4
	model.cancel = func() {}

	// When
	updated, command := model.Update(querySucceededMsg{requestID: 3, result: sqlite.Result{Rows: [][]*string{{stringPointer("stale")}}}})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("stale message returned an unexpected command")
	}
	if !model.running || model.activeRequestID != 4 {
		t.Fatalf("stale message changed active request: running=%t id=%d", model.running, model.activeRequestID)
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
			if command == nil || !model.running || model.activeRequestID != 1 {
				t.Fatalf("execute key did not start query: command=%v running=%t id=%d", command != nil, model.running, model.activeRequestID)
			}
			message := command()
			success, ok := message.(querySucceededMsg)
			if !ok || success.requestID != 1 {
				t.Fatalf("execute command message = %#v, want success for request 1", message)
			}
			updated, _ = model.Update(success)
			model = updated.(Model)
			if model.running || len(model.results.Rows()) != 1 {
				t.Fatalf("execute command did not complete: running=%t rows=%#v", model.running, model.results.Rows())
			}
		})
	}
}

func TestExecute_ignores_blank_and_repeated_requests(t *testing.T) {
	// Given
	model := readyModel(t)

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	// Then
	if command != nil || model.running {
		t.Fatalf("blank query started: command=%v running=%t", command != nil, model.running)
	}

	// Given
	model.editor.setValue("SELECT 1")
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	if command == nil || !model.running {
		t.Fatal("initial query did not start")
	}

	// When
	updated, repeated := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if repeated != nil || model.activeRequestID != 1 {
		t.Fatalf("repeated query changed active request: command=%v id=%d", repeated != nil, model.activeRequestID)
	}
}

func TestExecute_q_waits_for_matching_cancellation_before_quitting(t *testing.T) {
	// Given
	model := readyModel(t)
	canceled := false
	model.running, model.activeRequestID = true, 5
	model.cancel = func() { canceled = true }

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)

	// Then
	if command != nil || !canceled || !model.pendingQuit || !model.cancelRequested {
		t.Fatalf("q did not defer quit and cancel: command=%v canceled=%t pending=%t requested=%t", command != nil, canceled, model.pendingQuit, model.cancelRequested)
	}

	// When
	updated, command = model.Update(queryCanceledMsg{requestID: 5})
	model = updated.(Model)

	// Then
	if model.pendingQuit || command == nil {
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
