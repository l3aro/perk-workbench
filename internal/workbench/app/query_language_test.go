package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

var testRedisLanguage = sharedsql.QueryLanguage{
	Name:        "Redis",
	EditorLabel: "Command",
	Placeholder: "Enter a command…",
	Lexer:       "redis",
	Examples:    []string{"GET user:2"},
	Commands: []sharedsql.QueryCommand{
		{Name: "GET", Usage: "GET key", Summary: "Get the string value stored at key"},
		{Name: "HGET", Usage: "HGET key field", Summary: "Get one field of the hash at key"},
		{Name: "HGETALL", Usage: "HGETALL key", Summary: "Get all fields of the hash at key"},
		{Name: "HSET", Usage: "HSET key field value", Summary: "Set one field of the hash at key"},
		{Name: "PING", Usage: "PING", Summary: "Check that the connection is alive"},
		{Name: "SELECT", Usage: "SELECT index", Summary: "Select the logical database"},
	},
}

// TestOpen_appliesQueryLanguage: a successful open applies the matched
// driver's query language to the model, the editor placeholder, and the
// workspace tab label.
func TestOpen_appliesQueryLanguage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("redis:svc", context.Background(), func(_ context.Context, _ string) (sharedsql.Opened, error) {
		return sharedsql.Opened{
			Service:       &stubService{},
			Info:          sharedsql.DatabaseInfo{Product: "Redis", Version: "fake"},
			QueryLanguage: testRedisLanguage,
		}, nil
	}, false)
	model.queryLog.path = t.TempDir() + "/data.db"
	model.queryLog.component.Entries = nil

	updated, _ := model.Update(model.Init()())
	model = updated.(Model)

	if !reflect.DeepEqual(model.queryLanguage, testRedisLanguage) {
		t.Fatalf("model query language = %+v, want %+v", model.queryLanguage, testRedisLanguage)
	}
	if got := model.queryLog.editor.text.input.Placeholder; got != "Enter a command…" {
		t.Fatalf("editor placeholder = %q, want the advertised placeholder", got)
	}
	if got := model.workspaceTabLabel(standardWorkspaceTabItem(tabQuery)); got != "Command" {
		t.Fatalf("query tab label = %q, want Command", got)
	}

	// The rendered workspace tab row carries the language label.
	model.Focus, model.Tab = focusWorkspace, tabQuery
	if view := ansi.Strip(model.workspaceView()); !strings.Contains(view, "Command") {
		t.Fatalf("workspace view = %q, want the Command tab label", view)
	}
}

// TestOpen_switchAppliesLanguage: a sidebar database switch applies the
// new connection's language, while a failed or superseded switch leaves
// the active language untouched.
func TestOpen_switchAppliesLanguage(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.queryLanguage = sharedsql.SQLQueryLanguage
	model.queryLog.editor.setLanguage(sharedsql.SQLQueryLanguage)

	// When — a successful reconnect switch carries a new language.
	model.openTag++
	updated, _ := model.Update(databaseOpenedMsg{
		service:       &stubService{},
		queryLanguage: testRedisLanguage,
		reconnect:     true,
		openTag:       model.openTag,
	})
	model = updated.(Model)
	if !reflect.DeepEqual(model.editorLanguage(), testRedisLanguage) {
		t.Fatalf("after switch editor language = %+v, want %+v", model.editorLanguage(), testRedisLanguage)
	}

	// When — a failed switch keeps the current session and language.
	model.openTag++
	updated, _ = model.Update(databaseOpenedMsg{err: errors.New("boom"), reconnect: true, openTag: model.openTag})
	model = updated.(Model)
	if !reflect.DeepEqual(model.editorLanguage(), testRedisLanguage) {
		t.Fatalf("after failed switch editor language = %+v, want the active language", model.editorLanguage())
	}
	if got := model.queryLog.editor.text.input.Placeholder; got != "Enter a command…" {
		t.Fatalf("editor placeholder after failed switch = %q, want unchanged", got)
	}

	// When — a superseded open (stale tag) arrives with another language.
	updated, _ = model.Update(databaseOpenedMsg{
		service:       &stubService{},
		queryLanguage: sharedsql.SQLQueryLanguage,
		reconnect:     true,
		openTag:       model.openTag - 1,
	})
	model = updated.(Model)
	if !reflect.DeepEqual(model.editorLanguage(), testRedisLanguage) {
		t.Fatalf("after stale open editor language = %+v, want the active language", model.editorLanguage())
	}
}

// TestOpen_switchDropsVisibleCompletionOverlay: a SQL completion popup
// visible before a successful switch to a non-SQL language must not
// survive the transition.
func TestOpen_switchDropsVisibleCompletionOverlay(t *testing.T) {
	model := readyModel(t) // SQL default language
	model.queryLog.editor.setValue("SEL")
	if cmd := model.startCompletion(); cmd != nil {
		t.Fatal("SQL generic completion should be synchronous")
	}
	if !model.queryLog.editor.completionVisible() {
		t.Fatal("test setup: SQL completion popup should be visible")
	}

	model.openTag++
	updated, _ := model.Update(databaseOpenedMsg{
		service:       &stubService{},
		queryLanguage: testRedisLanguage,
		reconnect:     true,
		openTag:       model.openTag,
	})
	model = updated.(Model)
	if model.queryLog.editor.completionVisible() {
		t.Fatal("SQL completion popup survived a switch to a non-SQL language")
	}
}

// TestWorkspaceTabLabel_queryUsesEditorLanguage: the query tab label
// follows the active language advertisement; SQL stays "SQL" and a zero
// advertisement falls back to SQL.
func TestWorkspaceTabLabel_queryUsesEditorLanguage(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	if got := model.workspaceTabLabel(standardWorkspaceTabItem(tabQuery)); got != "SQL" {
		t.Fatalf("default query tab label = %q, want SQL", got)
	}
	model.queryLanguage = testRedisLanguage
	if got := model.workspaceTabLabel(standardWorkspaceTabItem(tabQuery)); got != "Command" {
		t.Fatalf("query tab label = %q, want Command", got)
	}
	model.queryLanguage = sharedsql.QueryLanguage{}
	if got := model.workspaceTabLabel(standardWorkspaceTabItem(tabQuery)); got != "SQL" {
		t.Fatalf("zero-advertisement query tab label = %q, want the SQL fallback", got)
	}
}

// TestQueryStyledLines_sqlPaletteAndPlainFallback: SQL keeps the keyword
// styling palette, while a blank or unknown lexer advertisement renders
// plain unstyled ink text — never SQL.
func TestQueryStyledLines_sqlPaletteAndPlainFallback(t *testing.T) {
	if got := queryLexer(sharedsql.SQLQueryLanguage); got == nil {
		t.Fatal("SQL lexer advertisement resolved to nil")
	}
	if got := queryLexer(sharedsql.QueryLanguage{Name: "X", EditorLabel: "X", Placeholder: "x", Lexer: "not-a-lexer"}); got != nil {
		t.Fatalf("unknown lexer advertisement resolved to %T, want nil (plain)", got)
	}
	if got := queryLexer(sharedsql.QueryLanguage{Name: "X", EditorLabel: "X", Placeholder: "x"}); got != nil {
		t.Fatalf("blank lexer advertisement resolved to %T, want nil (plain)", got)
	}

	sqlLines := queryStyledLines("SELECT 1", 40, queryLexer(sharedsql.SQLQueryLanguage))
	if len(sqlLines) != 1 || len(sqlLines[0].runes) != 8 {
		t.Fatalf("SQL styled lines = %+v, want one line with 8 runes", sqlLines)
	}
	keyword := sqlLines[0].runes[0]
	if !keyword.style.GetBold() || keyword.style.GetForeground() != lipgloss.Color(colorPrimary) {
		t.Fatalf("SQL keyword style = bold:%t fg:%v, want bold accent", keyword.style.GetBold(), keyword.style.GetForeground())
	}
	// chroma's category grouping places every literal (strings and
	// numbers) under one category, so numbers render with the string
	// palette color — the pre-existing SQL look must not change.
	number := sqlLines[0].runes[7]
	if number.style.GetForeground() != lipgloss.Color(colorModeInsert) {
		t.Fatalf("SQL number style fg = %v, want the string palette color", number.style.GetForeground())
	}

	plain := queryStyledLines("SELECT 1", 40, nil)
	if len(plain) != 1 || len(plain[0].runes) != 8 {
		t.Fatalf("plain styled lines = %+v, want one line with 8 runes", plain)
	}
	for i, character := range plain[0].runes {
		if character.style.GetBold() || character.style.GetForeground() != lipgloss.Color(colorInk) {
			t.Fatalf("plain rune %d style = bold:%t fg:%v, want plain ink", i, character.style.GetBold(), character.style.GetForeground())
		}
	}

	// Multiline plain values keep the same hard-line structure as the
	// token path; the cursor code depends on it.
	multi := queryStyledLines("one\ntwo", 40, nil)
	if len(multi) != 2 || multi[0].hardLine != 0 || multi[1].hardLine != 1 {
		t.Fatalf("plain multiline visual lines = %+v, want two hard lines", multi)
	}
	if len(multi[0].runes) != 3 || len(multi[1].runes) != 3 {
		t.Fatalf("plain multiline rune counts = %d/%d, want 3/3", len(multi[0].runes), len(multi[1].runes))
	}
}

// TestQueryLanguage_nonSQLDisablesValidationAndCompletion: non-SQL
// editors neither schedule SQL validation nor offer relational
// completion; SQL keeps both.
func TestQueryLanguage_nonSQLDisablesValidationAndCompletion(t *testing.T) {
	model := readyModel(t)
	model.queryLanguage = testRedisLanguage
	model.queryLog.editor.setLanguage(testRedisLanguage)
	model.queryLog.editor.setValue("GET user:2")
	model.queryLog.editorValidity = sqlValidityPending

	if cmd := model.scheduleSQLValidation(); cmd != nil {
		t.Fatal("non-SQL editor scheduled SQL validation")
	}
	updated, cmd := model.Update(sqlValidationTickMsg{tag: model.queryLog.validationTag})
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("non-SQL validation tick produced a validation command")
	}
	updated, _ = model.Update(sqlValidationMsg{statement: "GET user:2", err: errors.New("invalid")})
	model = updated.(Model)
	if model.queryLog.editorValidity != sqlValidityPending {
		t.Fatalf("non-SQL editor validity = %v, want pending (no SQL validation)", model.queryLog.editorValidity)
	}

	if cmd := model.startCompletion(); cmd != nil {
		t.Fatal("non-SQL editor started SQL completion")
	}
	if model.queryLog.editor.completionVisible() {
		t.Fatal("non-SQL editor shows completion items")
	}

	// An in-flight SQL TableInfo response landing after the language
	// switch must not surface relational column completion either.
	model.queryLog.completionRequestTag, model.queryLog.completionTable = 7, "projects"
	updated, _ = model.Update(completionColumnsMsg{
		tag:     7,
		table:   "projects",
		columns: []sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER"}},
	})
	model = updated.(Model)
	if model.queryLog.editor.completionVisible() {
		t.Fatal("stale SQL completion columns shown on a non-SQL editor")
	}
}

// TestQueryLanguage_sqlKeepsValidationAndCompletion: the SQL language
// retains statement validation and relational completion.
func TestQueryLanguage_sqlKeepsValidationAndCompletion(t *testing.T) {
	model := readyModel(t) // SQL default language
	model.queryLog.editor.setValue("SELECT 1")
	model.queryLog.editorValidity = sqlValidityPending

	updated, cmd := model.Update(model.scheduleSQLValidation()())
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("SQL validation tick produced no validation command")
	}
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	if model.queryLog.editorValidity != sqlValidityValid {
		t.Fatalf("SQL editor validity = %v, want valid", model.queryLog.editorValidity)
	}

	// A typed word prefix still triggers keyword completion.
	model.queryLog.editor.setValue("SEL")
	if cmd := model.startCompletion(); cmd != nil {
		t.Fatal("SQL generic completion should be synchronous")
	}
	if !model.queryLog.editor.completionVisible() {
		t.Fatal("SQL editor should offer completion items")
	}
}
