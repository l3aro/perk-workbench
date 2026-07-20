package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bubble-workbench/internal/sqlite"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

func TestExecute_success_message_populates_results(t *testing.T) {
	// Given
	model := readyModel(t)
	model.running, model.activeRequestID = true, 1
	model.cancel = func() {}
	result := sqlite.Result{
		Columns:      []string{"name", "note"},
		Rows:         [][]*string{{stringPointer("projects"), nil}},
		RowsAffected: 1,
		Duration:     12 * time.Millisecond,
		Truncated:    true,
	}

	// When
	updated, command := model.Update(querySucceededMsg{requestID: 1, result: result})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("success message returned an unexpected command")
	}
	if model.running || model.cancel != nil {
		t.Fatal("success message did not clear active request state")
	}
	if got := model.results.Rows(); len(got) != 1 || got[0][0] != "projects" || got[0][1] != "NULL" {
		t.Fatalf("result rows = %#v, want populated sanitized cells", got)
	}
	if !strings.Contains(model.status, "1 row affected") || !strings.Contains(model.status, "truncated") {
		t.Fatalf("success status = %q, want row count and truncation", model.status)
	}
}

func TestSchema_enter_runs_selected_object_ddl_query(t *testing.T) {
	// Given
	model := readyModel(t)
	if _, err := model.service.Execute(context.Background(), `CREATE TABLE "project's" (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
	model.focus = focusSchema
	model.schema.SetItems([]list.Item{schemaItem{title: "project's", description: "table"}})

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("schema enter did not start a query")
	}
	message := command()

	// Then
	if got := model.editor.textarea.Value(); got != "SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'project''s'" {
		t.Fatalf("schema statement = %q", got)
	}
	success, ok := message.(querySucceededMsg)
	if !ok {
		t.Fatalf("schema query message = %T, want querySucceededMsg", message)
	}
	updated, _ = model.Update(success)
	model = updated.(Model)
	if got := model.results.Rows(); len(got) != 1 || !strings.Contains(got[0][0], `CREATE TABLE "project's"`) {
		t.Fatalf("schema result = %#v, want selected table DDL", got)
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
	if !strings.Contains(model.status, "query failed") {
		t.Fatalf("error status = %q, want inline failure", model.status)
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
	if !strings.Contains(model.status, "canceled") {
		t.Fatalf("late success status = %q, want cancellation", model.status)
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
			model.editor.textarea.SetValue("SELECT 1")

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
	model.editor.textarea.SetValue("SELECT 1")
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
