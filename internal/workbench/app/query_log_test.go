package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

func TestQueryLog_records_completions_newest_first_and_limits_entries(t *testing.T) {
	// Given
	model := readyModel(t)
	started := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)

	requestID := model.StartQueryForTest(context.Background())
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 1", startedAt: started, result: sharedsql.Result{Rows: [][]*string{{stringPointer("1")}}, Duration: time.Millisecond}})
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
	model.SelectTable("projects")

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
	model.queryLog.component.Detail = &queryLogEntry{Statement: "SELECT 1", Replayable: true}

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
			model.appendQueryLog(queryLogEntry{Statement: "SELECT 1", Status: "success", Replayable: true})
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
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 1, result: sharedsql.Result{Rows: [][]*string{{stringPointer("second")}}, Duration: 2 * time.Millisecond}})
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
	form := schema.NewColumnForm(sharedsql.ColumnInfo{Name: "name", Type: "TEXT", Nullable: true}, sharedsql.ColumnTypes(model.databaseInfo))
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
	updated, _ := model.Update(browseTableMsg{table: "projects", result: sharedsql.Result{Rows: [][]*string{{stringPointer("one")}, {stringPointer("two")}}}})
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

// TestActionLogEntry_legacyMetadataDefaults proves a nonblank statement
// without metadata keeps the current semantics: replayable, not
// sensitive, no language.
func TestActionLogEntry_legacyMetadataDefaults(t *testing.T) {
	startedAt := time.Now()
	entry := actionLogEntry("RENAME key user:2 user:3", nil, startedAt, nil, "updated 1 row")
	if !entry.Replayable || entry.Sensitive || entry.Language != "" {
		t.Fatalf("legacy entry = %#v, want replayable, not sensitive, no language", entry)
	}
	if entry.Status != "" || entry.Message != "updated 1 row" {
		t.Fatalf("legacy entry = %#v, want empty status with the given message", entry)
	}
	// Failure entries keep the metadata defaults too.
	failed := actionLogEntry("RENAME key user:2 user:3", nil, startedAt, errors.New("boom"), "updated 1 row")
	if !failed.Replayable || failed.Status != "failed" || failed.Message != "boom" {
		t.Fatalf("failed legacy entry = %#v, want replayable with the error message", failed)
	}
}

// TestActionLogEntry_resolvesStatementMetadata proves a present metadata
// object is authoritative for every field, including explicit
// non-replayable and sensitive flags.
func TestActionLogEntry_resolvesStatementMetadata(t *testing.T) {
	metadata := &sharedsql.StatementMetadata{Language: "redis", Replayable: false, Sensitive: true}
	entry := actionLogEntry("SET key 1", metadata, time.Now(), nil, "completed")
	if entry.Language != "redis" || entry.Replayable || !entry.Sensitive {
		t.Fatalf("metadata entry = %#v, want language redis, not replayable, sensitive", entry)
	}
	// A metadata object with only a language keeps the entry non-replayable:
	// the object is authoritative, unlike an omitted object.
	entry = actionLogEntry("SET key 1", &sharedsql.StatementMetadata{Language: "redis"}, time.Now(), nil, "completed")
	if entry.Language != "redis" || entry.Replayable {
		t.Fatalf("language-only metadata entry = %#v, want language redis and not replayable", entry)
	}
	// A sensitive failure drops the backend error text entirely: Redis
	// errors echo the offending command and its arguments.
	echoing := fmt.Errorf("ERR unknown command %q", "SET key 1")
	entry = actionLogEntry("SET key 1", &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), echoing, "completed")
	if entry.Status != "failed" || entry.Message != "" {
		t.Fatalf("sensitive failure entry = %#v, want failed status with no message", entry)
	}
}

// TestAppendQueryLog_sensitiveEntriesRedactedInMemoryAndAtRest proves the
// decision point: a sensitive statement is never retained verbatim, in
// the component or in the store, and the entry is forced non-replayable.
func TestAppendQueryLog_sensitiveEntriesRedactedInMemoryAndAtRest(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	secret := "SET api_token 8f14e45fceea167a5a36dedd4bea2543"
	entry := actionLogEntry(secret, &sharedsql.StatementMetadata{Language: "redis", Replayable: true, Sensitive: true}, time.Now(), nil, "completed")
	persistCmd := model.appendQueryLog(entry)
	if persistCmd == nil {
		t.Fatal("appendQueryLog returned nil persistence command")
	}
	rawMessage := persistCmd()
	if _, ok := rawMessage.(queryLogPersistedMsg); !ok {
		t.Fatalf("persistence command message = %T, want queryLogPersistedMsg", rawMessage)
	}

	// In memory: the component holds the marker, never the secret.
	if got := model.queryLog.component.Entries[0].Statement; got != redactedStatement {
		t.Fatalf("in-memory statement = %q, want redacted marker %q", got, redactedStatement)
	}
	if model.queryLog.component.Entries[0].Replayable {
		t.Fatal("sensitive entry is replayable in memory, want forced non-replayable")
	}
	if model.queryLog.component.Entries[0].Sensitive != entry.Sensitive {
		t.Fatalf("in-memory sensitive flag = %t, want preserved", model.queryLog.component.Entries[0].Sensitive)
	}
	if model.queryLog.component.Entries[0].Language != "redis" {
		t.Fatalf("in-memory language = %q, want redis", model.queryLog.component.Entries[0].Language)
	}

	// At rest: the store row holds the marker, and the raw database never
	// contains the secret text.
	store, err := querylog.Open(model.queryLog.path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Load("conn-a", queryLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Statement != redactedStatement || loaded[0].Replayable || !loaded[0].Sensitive {
		t.Fatalf("stored entry = %#v, want redacted marker, non-replayable, sensitive", loaded)
	}
	db, err := sql.Open("sqlite", model.queryLog.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow(`SELECT statement FROM query_log WHERE connection_id = 'conn-a'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != redactedStatement {
		t.Fatalf("raw statement = %q, want %q", raw, redactedStatement)
	}
	if strings.Contains(raw, "api_token") {
		t.Fatal("raw query log row retains the secret")
	}
}

// TestAppendQueryLog_sensitiveFailureDropsAdvisories proves a sensitive
// failed entry loses its backend error message and its advisory guidance
// at append time: advisories are backend text that can name keys of the
// redacted statement, exactly like the message, so neither is ever
// rendered or persisted for a sensitive entry.
func TestAppendQueryLog_sensitiveFailureDropsAdvisories(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	entry := queryLogEntry{
		StartedAt:          time.Now(),
		Statement:          "SET api_token 8f14e45fceea167a5a36dedd4bea2543",
		Status:             "failed",
		Message:            "redis: WRONGTYPE Operation against a key holding the wrong kind of value",
		Hint:               "GET accepts strings, but api_token is a hash",
		SuggestedStatement: "HGETALL api_token",
		Replayable:         true,
		Sensitive:          true,
	}
	persistCmd := model.appendQueryLog(entry)
	if persistCmd == nil {
		t.Fatal("appendQueryLog returned nil persistence command")
	}
	rawMessage := persistCmd()
	if _, ok := rawMessage.(queryLogPersistedMsg); !ok {
		t.Fatalf("persistence command message = %T, want queryLogPersistedMsg", rawMessage)
	}
	got := model.queryLog.component.Entries[0]
	if got.Statement != redactedStatement || got.Replayable {
		t.Fatalf("sensitive entry = %#v, want the redacted marker, non-replayable", got)
	}
	if got.Message != "failed" {
		t.Fatalf("failure message = %q, want the generic %q", got.Message, "failed")
	}
	if got.Hint != "" || got.SuggestedStatement != "" {
		t.Fatalf("advisories survived a sensitive failure: hint %q, try %q", got.Hint, got.SuggestedStatement)
	}
	// At rest the store row carries the generic message, never backend
	// error or advisory text.
	store, err := querylog.Open(model.queryLog.path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Load("conn-a", queryLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Message != "failed" {
		t.Fatalf("stored entry = %#v, want the generic failed message", loaded)
	}
}

// TestAppendQueryLog_metadataRoundTrip proves metadata flows from a
// native row-write result through the message into the persisted entry
// without changing the statement selection (the native statement wins).
func TestAppendQueryLog_metadataRoundTrip(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	native := "RENAME key user:2 user:3"
	metadata := &sharedsql.StatementMetadata{Language: "redis", Replayable: false, Sensitive: false}
	updated, persistCmd := model.Update(browseRowUpdatedMsg{statement: native, metadata: metadata, startedAt: time.Now()})
	model = updated.(Model)
	if persistCmd == nil {
		t.Fatal("row update returned nil query-log persistence command")
	}
	persisted := false
	for _, message := range executeCommandAll(persistCmd) {
		if _, ok := message.(queryLogPersistedMsg); ok {
			persisted = true
		}
	}
	if !persisted {
		t.Fatal("query-log persistence command did not emit queryLogPersistedMsg")
	}
	entry := model.queryLog.component.Entries[0]
	if entry.Statement != native || entry.Language != "redis" || entry.Replayable || entry.Sensitive {
		t.Fatalf("logged entry = %#v, want native statement with language redis, not replayable", entry)
	}
	store, err := querylog.Open(model.queryLog.path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Load("conn-a", queryLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Statement != native || loaded[0].Language != "redis" || loaded[0].Replayable || loaded[0].Sensitive {
		t.Fatalf("persisted entry = %#v, want the metadata round trip", loaded)
	}
	// The chat context mirrors a failed statement; a sensitive failure
	// must never leak the secret — or backend error text that echoes it —
	// into the log, the chat context, or the status. The backend error
	// here contains the full statement, like real Redis errors do.
	model = readyModel(t)
	model.connectionID = "conn-a"
	secret := "SET api_token 8f14e45fceea167a5a36dedd4bea2543"
	echoing := fmt.Errorf("ERR unknown command %q", secret)
	updated, persistCmd = model.Update(browseRowUpdatedMsg{statement: secret, metadata: &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, startedAt: time.Now(), err: echoing})
	model = updated.(Model)
	if persistCmd == nil {
		t.Fatal("sensitive row update returned nil query-log persistence command")
	}
	persisted = false
	for _, message := range executeCommandAll(persistCmd) {
		if _, ok := message.(queryLogPersistedMsg); ok {
			persisted = true
		}
	}
	if !persisted {
		t.Fatal("query-log persistence command did not emit queryLogPersistedMsg")
	}
	if model.chat.component.LastFailedQuery != redactedStatement {
		t.Fatalf("chat context = %q, want the redacted marker %q", model.chat.component.LastFailedQuery, redactedStatement)
	}
	if got := model.chat.component.LastFailedError; got != "failed" {
		t.Fatalf("chat error context = %q, want the generic %q", got, "failed")
	}
	if got := model.queryLog.component.Entries[0].Message; got != "failed" {
		t.Fatalf("in-memory failure message = %q, want the generic %q", got, "failed")
	}
	if got := model.Status; got != "updating row: failed" {
		t.Fatalf("status = %q, want %q without backend error text", got, "updating row: failed")
	}
	for _, surface := range []string{model.Status, model.chat.component.LastFailedQuery, model.chat.component.LastFailedError, model.queryLog.component.Entries[0].Statement, model.queryLog.component.Entries[0].Message} {
		if strings.Contains(surface, "api_token") {
			t.Fatalf("sensitive failure leaked the statement into %q", surface)
		}
	}
	store, err = querylog.Open(model.queryLog.path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if loaded, err := store.Load("conn-a", queryLogLimit); err != nil || len(loaded) != 1 || loaded[0].Message != "failed" {
		t.Fatalf("persisted failure message = %#v (err %v), want the generic %q", loaded, err, "failed")
	}
	// A non-sensitive failure keeps the backend error text in status and
	// message.
	model = readyModel(t)
	model.connectionID = "conn-a"
	updated, _ = model.Update(browseRowUpdatedMsg{statement: "RENAME key user:2 user:3", metadata: &sharedsql.StatementMetadata{Language: "redis", Replayable: false}, startedAt: time.Now(), err: errors.New("boom")})
	model = updated.(Model)
	if got := model.Status; !strings.Contains(got, "boom") {
		t.Fatalf("non-sensitive failure status = %q, want the backend error text", got)
	}
	if got := model.queryLog.component.Entries[0].Message; got != "boom" {
		t.Fatalf("non-sensitive failure message = %q, want the backend error text", got)
	}
}

// TestQueryLog_gatesNonReplayableEntries proves explain is a no-op with
// an explicit safe status for non-replayable (including sensitive)
// entries, that non-statement cell copy is unrestricted (the displayed
// cell text carries no statement), and that rendering still shows the
// entry. Statement-cell copy follows the newer policy in the yank tests
// below.
func TestQueryLog_gatesNonReplayableEntries(t *testing.T) {
	build := func(t *testing.T, entry queryLogEntry) Model {
		t.Helper()
		model := resizeModel(readyModel(t), 100, 24)
		model.Focus = focusQueryLog
		model.queryLog.component.Table.Focus()
		model.databaseInfo.Product = "SQLite"
		model.appendQueryLog(entry)
		model.queryLog.component.Table.SetCursor(0)
		return model
	}
	nonReplayable := queryLogEntry{Statement: "SET key 1", Status: "success", Replayable: false}
	sensitive := queryLogEntry{Statement: "SET key 1", Status: "success", Replayable: true, Sensitive: true}

	t.Run("yank non-statement cell", func(t *testing.T) {
		// The Time cell (column 0) carries no statement: it copies freely
		// even for non-replayable and sensitive entries.
		for name, entry := range map[string]queryLogEntry{"non-replayable": nonReplayable, "sensitive": sensitive} {
			t.Run(name, func(t *testing.T) {
				model := build(t, entry)
				updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
				model = updated.(Model)
				if model.Status != "copied to clipboard" {
					t.Fatalf("yank status = %q, want %q", model.Status, "copied to clipboard")
				}
				if command == nil {
					t.Fatal("yank command = nil, want clipboard command")
				}
			})
		}
	})
	t.Run("explain", func(t *testing.T) {
		model := build(t, nonReplayable)
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
		model = updated.(Model)
		if model.overlay.explainPicker != nil {
			t.Fatal("explain opened a picker, want no-op")
		}
		if model.Status != "not replayable" {
			t.Fatalf("explain status = %q, want %q", model.Status, "not replayable")
		}
	})
	t.Run("detail explain", func(t *testing.T) {
		model := build(t, nonReplayable)
		model.queryLog.component.Detail = &nonReplayable
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
		model = updated.(Model)
		if model.overlay.explainPicker != nil || model.queryLog.component.Detail == nil {
			t.Fatalf("detail explain: picker=%t detail=%t, want no-op with detail open", model.overlay.explainPicker != nil, model.queryLog.component.Detail == nil)
		}
		if model.Status != "not replayable" {
			t.Fatalf("detail explain status = %q, want %q", model.Status, "not replayable")
		}
	})
	t.Run("context menu", func(t *testing.T) {
		model := build(t, nonReplayable)
		updated, _ := model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
		model = updated.(Model)
		if model.overlay.contextMenu == nil {
			t.Fatal("comma did not open the query-log context menu")
		}
		updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		model = updated.(Model)
		if model.Status != "copied to clipboard" {
			t.Fatalf("context-menu yank status = %q, want %q", model.Status, "copied to clipboard")
		}
		if command == nil {
			t.Fatal("context-menu yank command = nil, want clipboard command")
		}
	})
	t.Run("renders", func(t *testing.T) {
		model := build(t, nonReplayable)
		if got := model.queryLog.component.Table.Rows()[0][2]; got != cellText("SET key 1") {
			t.Fatalf("rendered statement = %q, want the entry rendered", got)
		}
	})
}

// queryLogFocused builds a model focused on the query-log pane with one
// appended entry selected on row 0 (column 0).
func queryLogFocused(t *testing.T, entry queryLogEntry) Model {
	t.Helper()
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusQueryLog
	model.queryLog.component.Table.Focus()
	model.databaseInfo.Product = "SQLite"
	model.appendQueryLog(entry)
	model.queryLog.component.Table.SetCursor(0)
	return model
}

// clipboardPayload executes the OSC 52 write inside a copy command and
// returns the exact text it would put on the clipboard. copyQueryLogStatement
// produces a command sequence whose SetClipboard write carries the text as a
// string-kind message; the app batches that sequence with bookkeeping
// commands (e.g. the notification dismiss tick), so the search descends
// through batch/sequence messages. The native clipboard write in the
// sequence's tail is a harmless no-op without a display.
func clipboardPayload(t *testing.T, command tea.Cmd) string {
	t.Helper()
	payload, ok := clipboardPayloadIn(command)
	if !ok {
		t.Fatal("copy command carries no clipboard text")
	}
	return payload
}

// clipboardPayloadIn searches a command's message tree for the OSC 52
// clipboard write, whose message is a string-kind type.
func clipboardPayloadIn(command tea.Cmd) (string, bool) {
	if command == nil {
		return "", false
	}
	msg := command()
	if msg == nil {
		return "", false
	}
	value := reflect.ValueOf(msg)
	if value.Kind() == reflect.String {
		return value.String(), true
	}
	if value.Kind() != reflect.Slice {
		return "", false
	}
	for index := 0; index < value.Len(); index++ {
		element, ok := value.Index(index).Interface().(tea.Cmd)
		if !ok {
			continue
		}
		if payload, ok := clipboardPayloadIn(element); ok {
			return payload, true
		}
	}
	return "", false
}

// TestQueryLog_yank_sensitiveStatementCopiesSessionOriginal proves an
// explicit copy of a sensitive entry's statement yields the exact
// session original — through both the direct y key and the context menu —
// while the component, rendering, and store keep only the redacted
// marker, and chat context never receives the original.
func TestQueryLog_yank_sensitiveStatementCopiesSessionOriginal(t *testing.T) {
	secret := "SET api_token 8f14e45fceea167a5a36dedd4bea2543"
	entry := actionLogEntry(secret, &sharedsql.StatementMetadata{Language: "redis", Replayable: true, Sensitive: true}, time.Now(), nil, "completed")

	model := resizeModel(readyModel(t), 100, 24)
	model.connectionID = "conn-a"
	model.Focus = focusQueryLog
	model.queryLog.component.Table.Focus()
	model.databaseInfo.Product = "SQLite"
	persistCmd := model.appendQueryLog(entry)
	if persistCmd == nil {
		t.Fatal("appendQueryLog returned nil persistence command")
	}
	if rawMessage := persistCmd(); rawMessage == nil {
		t.Fatal("query-log persistence command emitted nil message")
	} else if _, ok := rawMessage.(queryLogPersistedMsg); !ok {
		t.Fatalf("persistence command message = %T, want queryLogPersistedMsg", rawMessage)
	}
	model.queryLog.component.Table.SetCursor(0)
	if got := model.queryLog.component.Entries[0].Statement; got != redactedStatement {
		t.Fatalf("in-memory statement = %q, want redacted marker %q", got, redactedStatement)
	}

	// When — direct y on the statement cell.
	model.queryLog.component.Column = queryLogStatementColumn
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then — the exact original reaches the clipboard.
	if model.Status != "copied to clipboard" {
		t.Fatalf("yank status = %q, want %q", model.Status, "copied to clipboard")
	}
	if got := clipboardPayload(t, command); got != secret {
		t.Fatalf("copied statement = %q, want the exact original %q", got, secret)
	}
	if got := model.queryLog.component.Entries[0].Statement; got != redactedStatement {
		t.Fatalf("statement after copy = %q, want the marker still %q", got, redactedStatement)
	}
	store, err := querylog.Open(model.queryLog.path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if loaded, err := store.Load("conn-a", queryLogLimit); err != nil || len(loaded) != 1 || loaded[0].Statement != redactedStatement {
		t.Fatalf("stored entry = %#v (err %v), want only the redacted marker", loaded, err)
	}

	// When — the same copy through the context menu.
	model = queryLogFocused(t, entry)
	model.queryLog.component.Column = queryLogStatementColumn
	updated, _ = model.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	model = updated.(Model)
	if model.overlay.contextMenu == nil {
		t.Fatal("comma did not open the query-log context menu")
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if got := clipboardPayload(t, command); got != secret {
		t.Fatalf("context-menu copy = %q, want the exact original %q", got, secret)
	}

	// Chat context mirrors only the marker for a failed sensitive entry;
	// the echoing backend error is never surfaced.
	model = queryLogFocused(t, actionLogEntry(secret, &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), fmt.Errorf("ERR unknown command %q", secret), "completed"))
	if got := model.chat.component.LastFailedQuery; got != redactedStatement {
		t.Fatalf("chat context = %q, want the redacted marker %q", got, redactedStatement)
	}
	if got := model.chat.component.LastFailedError; got != "failed" {
		t.Fatalf("chat error context = %q, want the generic %q", got, "failed")
	}
}

// TestQueryLog_yank_collisionSafeAssociation proves the transient cache
// resolves each sensitive entry's own original even when two entries
// share an identical timestamp (position-based association, never
// timestamp identity).
func TestQueryLog_yank_collisionSafeAssociation(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	started := time.Now()
	model.appendQueryLog(actionLogEntry("SET first_secret 1", &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, started, nil, "completed"))
	model.appendQueryLog(actionLogEntry("SET second_secret 2", &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, started, nil, "completed"))
	if got := model.queryLog.component.Entries[0].StartedAt; got != started {
		t.Fatalf("setup: entries must share one timestamp, got %v", got)
	}

	model.queryLog.component.Column = queryLogStatementColumn
	model.queryLog.component.Table.SetCursor(0)
	if text, ok := model.queryLogYankText(model.queryLog.component.Entries[0]); !ok || text != "SET second_secret 2" {
		t.Fatalf("newest entry copy = %q (ok %t), want its own original", text, ok)
	}
	model.queryLog.component.Table.SetCursor(1)
	if text, ok := model.queryLogYankText(model.queryLog.component.Entries[1]); !ok || text != "SET first_secret 1" {
		t.Fatalf("older entry copy = %q (ok %t), want its own original", text, ok)
	}
}

// TestQueryLog_yank_nonReplayableStatementCopiesWhenNotSensitive proves
// a non-sensitive but non-replayable statement stays copyable while
// explain remains blocked.
func TestQueryLog_yank_nonReplayableStatementCopiesWhenNotSensitive(t *testing.T) {
	statement := "RENAME key user:2 user:3"
	model := queryLogFocused(t, queryLogEntry{Statement: statement, Status: "success", Replayable: false})
	model.queryLog.component.Column = queryLogStatementColumn

	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if model.Status != "copied to clipboard" {
		t.Fatalf("yank status = %q, want %q", model.Status, "copied to clipboard")
	}
	if got := clipboardPayload(t, command); got != statement {
		t.Fatalf("copied statement = %q, want %q", got, statement)
	}

	// Explain stays blocked for the same entry.
	model = queryLogFocused(t, queryLogEntry{Statement: statement, Status: "success", Replayable: false})
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.overlay.explainPicker != nil || model.Status != "not replayable" {
		t.Fatalf("explain picker=%t status=%q, want no picker and %q", model.overlay.explainPicker != nil, model.Status, "not replayable")
	}
}

// TestQueryLog_explain_blockedForSensitiveEntries proves sensitive
// entries reject explain from the pane and from the detail overlay even
// though their session statement is copyable.
func TestQueryLog_explain_blockedForSensitiveEntries(t *testing.T) {
	model := queryLogFocused(t, actionLogEntry("SET key secret", &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), nil, "completed"))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.overlay.explainPicker != nil || model.Status != "not replayable" {
		t.Fatalf("pane explain picker=%t status=%q, want no picker and %q", model.overlay.explainPicker != nil, model.Status, "not replayable")
	}

	model.queryLog.component.Detail = &model.queryLog.component.Entries[0]
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if model.overlay.explainPicker != nil || model.queryLog.component.Detail == nil || model.Status != "not replayable" {
		t.Fatalf("detail explain picker=%t detail=%t status=%q, want no-op and %q", model.overlay.explainPicker != nil, model.queryLog.component.Detail == nil, model.Status, "not replayable")
	}
}

// TestQueryLog_yank_loadedSensitiveEntryCannotRecoverOriginal proves a
// persisted sensitive entry reloaded through the real open path keeps
// only the redacted marker, holds no transient original, and rejects
// copy.
func TestQueryLog_yank_loadedSensitiveEntryCannotRecoverOriginal(t *testing.T) {
	secret := "SET api_token 8f14e45fceea167a5a36dedd4bea2543"
	model := readyModel(t)
	model.connectionID = "conn-a"
	persistCmd := model.appendQueryLog(actionLogEntry(secret, &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), nil, "completed"))
	if persistCmd == nil {
		t.Fatal("appendQueryLog returned nil persistence command")
	}
	if rawMessage := persistCmd(); rawMessage == nil {
		t.Fatal("query-log persistence command emitted nil message")
	} else if _, ok := rawMessage.(queryLogPersistedMsg); !ok {
		t.Fatalf("persistence command message = %T, want queryLogPersistedMsg", rawMessage)
	}

	// Disconnect (scope reset) and reopen the same connection scope
	// through the production open handler: loaded entries must carry no
	// transient originals.
	model.disconnect()
	model.connectionID = "conn-a"
	service, err := openTestSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("closing reload service: %v", err)
		}
	})
	model.openTag++
	updated, openCmd := model.Update(databaseOpenedMsg{target: "reload.db", service: service, info: sharedsql.DatabaseInfo{}, reconnect: true, openTag: model.openTag})
	model = updated.(Model)
	model = driveCommand(model, openCmd)

	if got := model.queryLog.component.Entries; len(got) != 1 || got[0].Statement != redactedStatement || !got[0].Sensitive {
		t.Fatalf("reloaded entries = %#v, want one redacted sensitive entry", got)
	}
	if got := model.queryLog.transientStatements; len(got) != 1 || got[0] != "" {
		t.Fatalf("transient cache after reload = %#v, want one empty slot", got)
	}

	model.Focus = focusQueryLog
	model.queryLog.component.Table.Focus()
	model.queryLog.component.Table.SetCursor(0)
	model.queryLog.component.Column = queryLogStatementColumn
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if model.Status != "not replayable" {
		t.Fatalf("reloaded sensitive yank status = %q, want %q", model.Status, "not replayable")
	}
	if payload, ok := clipboardPayloadIn(command); ok {
		t.Fatalf("reloaded sensitive yank put %q on the clipboard, want none", payload)
	}
	if text, ok := model.queryLogYankText(model.queryLog.component.Entries[0]); ok || strings.Contains(text, "api_token") {
		t.Fatalf("reloaded sensitive statement resolvable: %q (ok %t), want rejection", text, ok)
	}
}

// TestQueryLog_yank_transientCacheCappedAndAligned proves the transient
// originals obey the same cap as the component list and stay
// index-aligned with it under eviction.
func TestQueryLog_yank_transientCacheCappedAndAligned(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	started := time.Now()
	for index := 1; index <= queryLogLimit+1; index++ {
		model.appendQueryLog(actionLogEntry(fmt.Sprintf("SET secret %d", index), &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, started.Add(time.Duration(index)*time.Second), nil, "completed"))
	}
	entries := model.queryLog.component.Entries
	if len(entries) != queryLogLimit {
		t.Fatalf("component entries = %d, want the %d cap", len(entries), queryLogLimit)
	}
	if got := len(model.queryLog.transientStatements); got != queryLogLimit {
		t.Fatalf("transient cache = %d slots, want the %d cap", got, queryLogLimit)
	}
	for index := range entries {
		want := fmt.Sprintf("SET secret %d", queryLogLimit+1-index)
		if got := model.queryLog.transientStatements[index]; got != want {
			t.Fatalf("cache slot %d = %q, want %q", index, got, want)
		}
	}
	// The oldest retained row (index limit-1) copies its own original.
	model.Focus = focusQueryLog
	model.queryLog.component.Table.Focus()
	model.queryLog.component.SetPage((queryLogLimit - 1) / defaultQueryLogPageSize)
	model.queryLog.component.Table.SetCursor((queryLogLimit - 1) % defaultQueryLogPageSize)
	model.queryLog.component.Column = queryLogStatementColumn
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if got := clipboardPayload(t, command); got != "SET secret 2" {
		t.Fatalf("oldest retained copy = %q, want %q", got, "SET secret 2")
	}
}

// TestQueryLog_yank_cacheClearedOnScopeReset proves transient originals
// die with the connection scope: disconnect empties the cache and a new
// scope never sees the previous connection's statements.
func TestQueryLog_yank_cacheClearedOnScopeReset(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	secretA := "SET api_token 8f14e45fceea167a5a36dedd4bea2543"
	model.appendQueryLog(actionLogEntry(secretA, &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), nil, "completed"))

	model.disconnect()
	if model.queryLog.transientStatements != nil {
		t.Fatalf("transient cache after disconnect = %#v, want nil", model.queryLog.transientStatements)
	}
	if len(model.queryLog.component.Entries) != 0 {
		t.Fatalf("component entries after disconnect = %d, want none", len(model.queryLog.component.Entries))
	}

	model.connectionID = "conn-b"
	secretB := "SET other_token 2"
	model.appendQueryLog(actionLogEntry(secretB, &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), nil, "completed"))
	if got := model.queryLog.transientStatements; len(got) != 1 || got[0] != secretB {
		t.Fatalf("new-scope cache = %#v, want only %q", got, secretB)
	}
	if strings.Contains(strings.Join(model.queryLog.transientStatements, " "), "api_token") {
		t.Fatal("new connection scope retained the previous connection's secret")
	}
}

func TestAppendQueryLog_visibleBeforePersistenceCommand(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	cmd := model.appendQueryLog(queryLogEntry{Statement: "SELECT visible", Status: "success", Replayable: true})
	if cmd == nil {
		t.Fatal("appendQueryLog returned nil command for active scope")
	}
	if len(model.queryLog.component.Entries) != 1 || model.queryLog.component.Entries[0].Statement != "SELECT visible" {
		t.Fatalf("entry not visible before command execution: %#v", model.queryLog.component.Entries)
	}
}

func TestAppendQueryLog_commandCapturesRedactedEntryAndScope(t *testing.T) {
	model := readyModel(t)
	model.connectionID = "conn-a"
	model.openTag = 7
	path := model.queryLog.path
	secret := "SET api_token captured-secret"
	cmd := model.appendQueryLog(actionLogEntry(secret, &sharedsql.StatementMetadata{Language: "redis", Sensitive: true}, time.Now(), nil, "completed"))
	if cmd == nil {
		t.Fatal("appendQueryLog returned nil command for active scope")
	}
	model.connectionID = "conn-b"
	model.openTag = 8
	model.queryLog.path = t.TempDir() + "/other.db"
	rawMessage := cmd()
	message, ok := rawMessage.(queryLogPersistedMsg)
	if !ok {
		t.Fatalf("persistence command message = %T, want queryLogPersistedMsg", rawMessage)
	}
	if message.openTag != 7 || message.connectionID != "conn-a" {
		t.Fatalf("persistence scope = %#v, want tag 7 and conn-a", message)
	}
	store, err := querylog.Open(path, queryLogRetentionDays())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	entries, err := store.Load("conn-a", queryLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Statement != redactedStatement || strings.Contains(entries[0].Statement, secret) {
		t.Fatalf("persisted sensitive entry = %#v, want only redacted marker", entries)
	}
}
