package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
	"strings"
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

	for index := range queryLogLimit - 2 {
		model.appendQueryLog(queryLogEntry{StartedAt: started.Add(time.Duration(index+3) * time.Second), Statement: "SELECT many", Duration: time.Millisecond})
	}

	// Then
	rows := model.queryLog.component.Table.Rows()
	if got, want := len(rows), defaultQueryLogPageSize; got != want {
		t.Fatalf("visible query log rows = %d, want %d", got, want)
	}
	if got := strings.TrimSpace(ansi.Strip(rows[0][1])); got != iconSuccess {
		t.Fatalf("latest query log status = %q, want %q", got, iconSuccess)
	}
	if got, want := rows[0][2], "SELECT many"; got != want {
		t.Fatalf("latest query log statement = %q, want %q", got, want)
	}
	if got := model.queryLog.component.Entries[len(model.queryLog.component.Entries)-1].Statement; got != "bad SQL" {
		t.Fatalf("oldest retained query = %q, want failed query after capped entries", got)
	}
}

func TestNew_queryLog_has_history_columns(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)

	// When
	columns := model.queryLog.component.Table.Columns()

	// Then
	if got, want := len(columns), 5; got != want {
		t.Fatalf("query log columns = %d, want %d", got, want)
	}
	for index, want := range []string{"Time", "Status", "Statement", "Duration", "Message"} {
		if got := columns[index].Title; got != want {
			t.Errorf("query log column %d = %q, want %q", index, got, want)
		}
	}
}

func TestQueryLogDetail_shows_statement(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	statement := "SELECT id, name FROM projects WHERE active = 1"
	model.queryLog.component.Detail = &queryLogEntry{Statement: statement}

	// When
	view := ansi.Strip(model.View().Content)

	// Then
	if !strings.Contains(view, "Statement:") {
		t.Fatalf("query log detail = %q, want statement label", view)
	}
	if !strings.Contains(view, statement) {
		t.Fatalf("query log detail = %q, want statement %q", view, statement)
	}
}

func TestQueryLogDetail_prettyPrintsJSON(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	value := `{"customer":{"name":"Ada"},"ids":[1,2]}`
	model.queryLog.component.Detail = &queryLogEntry{Message: value}

	// When
	view := ansi.Strip(model.View().Content)

	// Then
	for _, want := range []string{"\"customer\": {", "\"name\": \"Ada\"", "\"ids\": ["} {
		if !strings.Contains(view, want) {
			t.Fatalf("query log detail = %q, want formatted JSON containing %q", view, want)
		}
	}
}

func TestWorkspaceTabs_labelSchemaInspectionViews(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 100, 24)

	// When
	view := ansi.Strip(model.workspaceView())

	// Then
	for _, want := range []string{"Columns", "Indexes", "Foreign Keys"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace tabs = %q, want %q", view, want)
		}
	}
}

func TestQueryLogDetail_y_doesNotOpenCopyDialog(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	model.queryLog.component.Detail = &queryLogEntry{Statement: "SELECT 1"}

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then
	if model.queryLog.component.Detail == nil {
		t.Fatal("y closed the detail overlay")
	}
	if command != nil {
		t.Fatal("y opened a copy command")
	}
}

func TestQueryLog_displayTruncatesLongStatementAndMessage(t *testing.T) {
	// Given
	model := readyModel(t)
	statement := strings.Repeat("s", 41)
	message := strings.Repeat("m", 41)

	// When
	model.appendQueryLog(queryLogEntry{Statement: statement, Message: message})

	// Then
	if got, want := model.queryLog.component.Table.Rows()[0][2], cellText(statement); got != want {
		t.Fatalf("display statement = %q, want %q", got, want)
	}
	if got, want := model.queryLog.component.Table.Rows()[0][4], cellText(message); got != want {
		t.Fatalf("display message = %q, want %q", got, want)
	}
	if got := model.queryLog.component.Table.Columns()[2].Width; got > 50 {
		t.Fatalf("statement column width = %d, want at most 50", got)
	}
	if got := model.queryLog.component.Table.Columns()[4].Width; got > 50 {
		t.Fatalf("message column width = %d, want at most 50", got)
	}
	model = resizeModel(model, 160, 24)
	if got := model.queryLog.component.Table.Columns()[2].Width; got > 50 {
		t.Fatalf("statement column width after resize = %d, want at most 50", got)
	}
	if got, want := model.queryLog.component.Entries[0].Statement, statement; got != want {
		t.Fatalf("stored statement = %q, want full value", got)
	}
	if got, want := model.queryLog.component.Entries[0].Message, message; got != want {
		t.Fatalf("stored message = %q, want full value", got)
	}
}

func TestQueryLogDetail_e_opens_explain_picker(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 80, 24)
	model.databaseInfo.Product = "SQLite"
	model.queryLog.component.Detail = &queryLogEntry{Statement: "SELECT 1"}

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)

	// Then
	if model.queryLog.component.Detail != nil {
		t.Fatal("e did not close the detail overlay")
	}
	if model.overlay.explainPicker == nil {
		t.Fatal("e did not open the explain picker")
	}
	if command == nil {
		t.Fatal("explain picker init command is nil")
	}
}

func TestQueryLog_contextMenuExposesAndExecutesShortcuts(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyPressMsg
		check func(*testing.T, Model)
	}{
		{
			name: "copy",
			keys: []tea.KeyPressMsg{{Code: 'y', Text: "y"}},
			check: func(t *testing.T, model Model) {
				if model.Status != "copied to clipboard" {
					t.Fatalf("status = %q, want copied status", model.Status)
				}
			},
		},
		{
			name: "detail",
			keys: []tea.KeyPressMsg{{Code: tea.KeyEnter}},
			check: func(t *testing.T, model Model) {
				if model.queryLog.component.Detail == nil {
					t.Fatal("detail action did not open query log detail")
				}
			},
		},
		{
			name: "explain",
			keys: []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyEnter}},
			check: func(t *testing.T, model Model) {
				if model.overlay.explainPicker == nil {
					t.Fatal("explain action did not open explain picker")
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := resizeModel(readyModel(t), 100, 24)
			model.Focus = focusQueryLog
			model.queryLog.component.Table.Focus()
			model.appendQueryLog(queryLogEntry{Statement: "SELECT 1", Status: "success"})
			model.queryLog.component.Table.SetCursor(0)
			model.databaseInfo.Product = "SQLite"
			updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
			model = updated.(Model)
			if model.overlay.contextMenu == nil || len(model.overlay.contextMenu.options) != 3 {
				t.Fatalf("context menu = %#v, want three query-log actions", model.overlay.contextMenu)
			}
			for index, want := range []string{"enter", "y", "e"} {
				if got := model.overlay.contextMenu.options[index].keys; got != want {
					t.Errorf("option %d shortcut = %q, want %q", index, got, want)
				}
			}
			for _, key := range test.keys {
				updated, _ = model.Update(key)
				model = updated.(Model)
			}
			if model.overlay.contextMenu != nil {
				t.Fatal("query-log context menu remained open after action")
			}
			test.check(t, model)
		})
	}
}

func TestQueryLog_contextMenuMouseSelectsRenderedDetail(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusQueryLog
	model.queryLog.component.Table.Focus()
	model.appendQueryLog(queryLogEntry{Statement: "SELECT 1", Status: "success"})
	model.queryLog.component.Table.SetCursor(0)

	updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	if model.overlay.contextMenu == nil {
		t.Fatal("comma did not open query-log context menu")
	}

	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	clickX, clickY := -1, -1
	for y, line := range lines {
		if x := strings.Index(line, "Detail"); x >= 0 {
			clickX, clickY = x, y
			break
		}
	}
	if clickX < 0 {
		t.Fatal("rendered query-log context menu does not contain Detail")
	}

	updated, _ = model.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.overlay.contextMenu != nil {
		t.Fatal("query-log context menu remained open after mouse selection")
	}
	if model.queryLog.component.Detail == nil {
		t.Fatal("mouse-selected Detail action did not open query-log detail")
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
	rows := model.queryLog.component.Table.Rows()
	if got, want := len(rows), 1; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	if got := strings.TrimSpace(ansi.Strip(rows[0][1])); got != iconSuccess {
		t.Fatalf("browse query log status = %q, want %q", got, iconSuccess)
	}
	if got, want := rows[0][2], cellText(`SELECT * FROM "projects" LIMIT 25 OFFSET 25`); got != want {
		t.Fatalf("browse query log statement = %q, want %q", got, want)
	}
	if got, want := rows[0][3], "2ms"; got != want {
		t.Fatalf("browse query log duration = %q, want %q", got, want)
	}
	if got, want := rows[0][4], "fetched 1 row"; got != want {
		t.Fatalf("browse query log message = %q, want %q", got, want)
	}
}

func TestQueryLog_records_structure_and_index_actions(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "items"
	if _, err := model.Database.Execute(model.appContext, "CREATE TABLE items (name TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	form := schema.NewColumnForm(sqlite.ColumnInfo{Name: "name", Type: "TEXT", Nullable: true}, sharedsql.ColumnTypes(model.databaseInfo))
	form.SetKeys(DefaultKeybindings())
	model.schema.component.Structure.ColumnForm = form
	model.schema.component.Structure.ColumnForm.Values.Name = "title"

	// When
	updated, _ := model.Update(model.alterColumn()())
	model = updated.(Model)
	indexForm := schema.NewIndexForm(nil)
	indexForm.SetKeys(DefaultKeybindings())
	model.schema.component.Structure.IndexForm = indexForm
	model.schema.component.Structure.IndexForm.Values.Name = "items_title"
	model.schema.component.Structure.IndexForm.Values.Columns = "title"
	updated, _ = model.Update(model.saveIndex()())
	model = updated.(Model)

	// Then
	rows := model.queryLog.component.Table.Rows()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	if got, want := rows[0][2], cellText(`CREATE INDEX "items_title" ON "items" ("title")`); got != want {
		t.Fatalf("index action log = %q, want %q", got, want)
	}
	if got, want := rows[1][2], cellText(`ALTER TABLE "items" RENAME COLUMN "name" TO "title"`); got != want {
		t.Fatalf("structure action log = %q, want %q", got, want)
	}
	if got, want := rows[0][4], "updated index"; got != want {
		t.Fatalf("index action message = %q, want %q", got, want)
	}
	if got, want := rows[1][4], "altered column"; got != want {
		t.Fatalf("structure action message = %q, want %q", got, want)
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
	form := schema.NewIndexForm(&sharedsql.IndexInfo{Name: "items_name", Columns: []string{"name"}})
	form.SetKeys(DefaultKeybindings())
	model.schema.component.Structure.IndexForm = form
	model.schema.component.Structure.IndexForm.Values.Name = "items_title"
	model.schema.component.Structure.IndexForm.Values.Columns = "title"

	// When
	updated, _ := model.Update(model.saveIndex()())
	model = updated.(Model)
	indexForm2 := schema.NewIndexForm(&sharedsql.IndexInfo{Name: "items_title", Columns: []string{"title"}})
	indexForm2.SetKeys(DefaultKeybindings())
	model.schema.component.Structure.IndexForm = indexForm2
	updated, _ = model.Update(model.deleteIndex()())
	model = updated.(Model)

	// Then
	rows := model.queryLog.component.Table.Rows()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	if got, want := rows[0][2], `DROP INDEX "items_title"`; got != want {
		t.Fatalf("index deletion log = %q, want %q", got, want)
	}
	if got, want := rows[1][2], cellText(`DROP INDEX "items_name"; CREATE INDEX "items_title" ON "items" ("title")`); got != want {
		t.Fatalf("index replacement log = %q, want %q", got, want)
	}
}

func TestQueryLog_shows_browse_message(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "projects"

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", result: sqlite.Result{Rows: [][]*string{{stringPointer("one")}, {stringPointer("two")}}}})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.component.Table.Rows()[0][4], "fetched 2 rows"; got != want {
		t.Fatalf("browse log message = %q, want %q", got, want)
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
	if got, want := model.queryLog.component.Table.Rows()[0][2], cellText("SELECT * FROM `projects` LIMIT 25 OFFSET 0"); got != want {
		t.Fatalf("browse statement = %q, want %q", got, want)
	}
}

func TestQueryLog_records_applied_browse_filters(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable = "office.customers"
	model.databaseInfo.Product = "MySQL"
	model.browse.component.Settings.Filters = []sharedsql.BrowseFilter{
		{Column: "City", Operator: sharedsql.BrowseFilterLike, Value: "A%"},
		{Column: "SupportRepId", Operator: sharedsql.BrowseFilterIsNotNull},
	}

	updated, _ := model.Update(browseTableMsg{table: "office.customers"})
	model = updated.(Model)

	want := "SELECT * FROM `office`.`customers` WHERE `City` LIKE 'A%' AND `SupportRepId` IS NOT NULL LIMIT 25 OFFSET 0"
	if got := model.queryLog.component.Table.Rows()[0][2]; got != cellText(want) {
		t.Fatalf("browse statement = %q, want %q", got, cellText(want))
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
	if got := model.queryLog.component.Entries[0].Duration; got <= 0 {
		t.Fatalf("browse log duration = %s, want elapsed duration", got)
	}
}

func TestQueryLog_summary_reports_session_extrema(t *testing.T) {
	// Given
	model := readyModel(t)
	model.queryLog.component.Entries = []queryLogEntry{
		{Duration: 8 * time.Millisecond},
		{Duration: 2 * time.Millisecond},
		{Duration: 15 * time.Millisecond},
	}

	// When
	summary := model.queryLog.component.Summary()

	// Then
	if got, want := summary, "page 1/1 | fastest 2ms | slowest 15ms"; got != want {
		t.Fatalf("query log summary = %q, want %q", got, want)
	}
}

func TestQueryLogMessage_describes_statement_results(t *testing.T) {
	tests := []struct {
		statement    string
		rowsAffected int64
		rows         int
		want         string
	}{
		{statement: "SELECT * FROM projects", rows: 10, want: "fetched 10 rows"},
		{statement: "INSERT INTO projects VALUES (1)", rowsAffected: 1, want: "inserted 1 row"},
		{statement: "UPDATE projects SET name = 'new'", rowsAffected: 3, want: "updated 3 rows"},
	}

	for _, test := range tests {
		t.Run(test.statement, func(t *testing.T) {
			if got := queryLogMessage(test.statement, test.rowsAffected, test.rows); got != test.want {
				t.Fatalf("query log message = %q, want %q", got, test.want)
			}
		})
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
