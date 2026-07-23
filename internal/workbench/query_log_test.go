package workbench

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestQueryLog_records_completions_newest_first_and_limits_entries(t *testing.T) {
	// Given
	model := readyModel(t)
	started := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)

	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 1", startedAt: started, result: sqlite.Result{Rows: [][]*string{{stringPointer("1")}}, Duration: time.Millisecond}})
	model = updated.(Model)

	requestID = model.StartQueryForTest(context.Background())
	updated, _ = model.Update(queryFailedMsg{requestID: requestID, statement: "bad SQL", startedAt: started.Add(time.Second), err: errors.New("syntax error")})
	model = updated.(Model)

	requestID = model.StartQueryForTest(context.Background())
	updated, _ = model.Update(queryCanceledMsg{requestID: requestID, statement: "SELECT sleep", startedAt: started.Add(2 * time.Second)})
	model = updated.(Model)

	for index := 0; index < queryLogLimit-2; index++ {
		model.appendQueryLog(queryLogEntry{startedAt: started.Add(time.Duration(index+3) * time.Second), statement: "SELECT many", duration: time.Millisecond, fetched: index})
	}

	// Then
	rows := model.queryLog.Rows()
	if got, want := len(rows), queryLogLimit; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	want := table.Row{"09:01:40", "SELECT many", "1ms", "97"}
	if got := rows[0]; !equalTableRow(got, want) {
		t.Fatalf("latest query log row = %#v, want %#v", got, want)
	}
	if got := rows[len(rows)-1][1]; got != "bad SQL" {
		t.Fatalf("oldest retained query = %q, want failed query after capped entries", got)
	}
}

func TestNew_queryLog_has_history_columns(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))

	// When
	columns := model.queryLog.Columns()

	// Then
	if got, want := len(columns), 4; got != want {
		t.Fatalf("query log columns = %d, want %d", got, want)
	}
	for index, want := range []string{"Time", "Query", "Duration", "Fetch"} {
		if got := columns[index].Title; got != want {
			t.Errorf("query log column %d = %q, want %q", index, got, want)
		}
	}
}

func TestQueryLog_records_browse_page_load(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable, model.BrowsePage = "projects", 1

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 1, result: sqlite.Result{Rows: [][]*string{{stringPointer("second")}}, Duration: 2 * time.Millisecond}})
	model = updated.(Model)

	// Then
	rows := model.queryLog.Rows()
	if got, want := len(rows), 1; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	want := table.Row{"SELECT * FROM \"projects\" LIMIT 25 OFFSET 25", "2ms", "1"}
	if got := rows[0][1:]; !equalTableRow(got, want) {
		t.Fatalf("browse query log row = %#v, want %#v", got, want)
	}
}

func TestQueryLog_records_structure_and_index_actions(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "items"
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	model.columnForm = newColumnForm(sqlite.ColumnInfo{Name: "name", Type: "TEXT", Nullable: true}, sharedsql.ColumnTypes(model.databaseInfo))
	model.columnForm.values.name = "title"

	// When
	updated, _ := model.Update(model.alterColumn()())
	model = updated.(Model)
	model.indexForm = newIndexForm(nil)
	model.indexForm.values.name = "items_title"
	model.indexForm.values.columns = "title"
	updated, _ = model.Update(model.saveIndex()())
	model = updated.(Model)

	// Then
	rows := model.queryLog.Rows()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	if got, want := rows[0][1], `CREATE INDEX "items_title" ON "items" ("title")`; got != want {
		t.Fatalf("index action log = %q, want %q", got, want)
	}
	if got, want := rows[1][1], `ALTER TABLE "items" RENAME COLUMN "name" TO "title"`; got != want {
		t.Fatalf("structure action log = %q, want %q", got, want)
	}
}

func TestQueryLog_records_index_replacement_and_deletion(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "items"
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (name TEXT, title TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := model.Database.Execute(model.appContext, "CREATE INDEX items_name ON items (name)"); err != nil {
		t.Fatalf("creating index: %v", err)
	}
	model.indexForm = newIndexForm(&sharedsql.IndexInfo{Name: "items_name", Columns: []string{"name"}})
	model.indexForm.values.name = "items_title"
	model.indexForm.values.columns = "title"

	// When
	updated, _ := model.Update(model.saveIndex()())
	model = updated.(Model)
	model.indexForm = newIndexForm(&sharedsql.IndexInfo{Name: "items_title", Columns: []string{"title"}})
	updated, _ = model.Update(model.deleteIndex()())
	model = updated.(Model)

	// Then
	rows := model.queryLog.Rows()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	if got, want := rows[0][1], `DROP INDEX "items_title"`; got != want {
		t.Fatalf("index deletion log = %q, want %q", got, want)
	}
	if got, want := rows[1][1], `DROP INDEX "items_name"; CREATE INDEX "items_title" ON "items" ("title")`; got != want {
		t.Fatalf("index replacement log = %q, want %q", got, want)
	}
}

func TestQueryLog_shows_browse_fetch_count(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "projects"

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", result: sqlite.Result{Rows: [][]*string{{stringPointer("one")}, {stringPointer("two")}}}})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.Rows()[0][3], "2"; got != want {
		t.Fatalf("browse log fetch = %q, want %q", got, want)
	}
}

func TestQueryLog_uses_mysql_identifier_quoting_for_browse_statement(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "projects"
	model.databaseInfo.Product = "MySQL"

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects"})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.Rows()[0][1], "SELECT * FROM `projects` LIMIT 25 OFFSET 0"; got != want {
		t.Fatalf("browse statement = %q, want %q", got, want)
	}
}

func TestQueryLog_uses_elapsed_duration_for_live_browse_load(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "projects"

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", startedAt: time.Now().Add(-time.Millisecond)})
	model = updated.(Model)

	// Then
	if got := model.queryLogEntries[0].duration; got <= 0 {
		t.Fatalf("browse log duration = %s, want elapsed duration", got)
	}
}

func TestQueryLog_summary_reports_session_extrema(t *testing.T) {
	// Given
	model := readyModel(t)
	model.queryLogEntries = []queryLogEntry{
		{duration: 8 * time.Millisecond, fetched: 25},
		{duration: 2 * time.Millisecond, fetched: 3},
		{duration: 15 * time.Millisecond, fetched: 12},
	}

	// When
	summary := model.queryLogSummary()

	// Then
	if got, want := summary, "fastest 2ms | slowest 15ms | most fetch 25 | least fetch 3"; got != want {
		t.Fatalf("query log summary = %q, want %q", got, want)
	}
}

func equalTableRow(got, want table.Row) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
