package chat

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

func TestSQL_extractsNewestFencedBlock(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: "count rows"},
		{Role: ai.RoleAssistant, Content: "```sql\nSELECT 1\n```"},
		{Role: ai.RoleAssistant, Content: "here:\n```sql\nSELECT 2\n```"},
	}
	if got := SQL(messages); got != "SELECT 2" {
		t.Fatalf("SQL = %q, want the newest block", got)
	}
	if got := SQL(messages[:2]); got != "SELECT 1" {
		t.Fatalf("SQL = %q, want the first block", got)
	}
	if got := SQL(nil); got != "" {
		t.Fatalf("SQL(nil) = %q, want empty", got)
	}
}

func TestPromptHistory_recall(t *testing.T) {
	cm := New()
	cm.RecordPromptHistory("first")
	cm.RecordPromptHistory("second")

	// Up from a blank input recalls newest first.
	if got, ok := cm.RecallPromptHistory(1); !ok || got != "second" {
		t.Fatalf("recall up 1 = %q/%t, want second", got, ok)
	}
	if got, ok := cm.RecallPromptHistory(1); !ok || got != "first" {
		t.Fatalf("recall up 2 = %q/%t, want first", got, ok)
	}
	// Never wraps at the oldest entry.
	if _, ok := cm.RecallPromptHistory(1); ok {
		t.Fatal("recall wrapped past the oldest entry")
	}
	// Down returns to the newest entry, then clears.
	if got, ok := cm.RecallPromptHistory(-1); !ok || got != "second" {
		t.Fatalf("recall down = %q/%t, want second", got, ok)
	}
	if got, ok := cm.RecallPromptHistory(-1); !ok || got != "" {
		t.Fatalf("recall down to newest = %q/%t, want cleared", got, ok)
	}
}

func TestContextText_includesSnapshotAndSessionState(t *testing.T) {
	cm := New()
	cm.LastFailedQuery = "SELECT broken"
	cm.LastFailedError = "no such table"
	ctx := Context{
		Database: sharedsql.DatabaseInfo{Product: "SQLite", Version: "3.53"},
		Schema:   []sharedsql.SchemaObject{{Type: "table", Database: "main", Name: "a_table"}},
		Query:    "SELECT 1",
	}
	text := cm.ContextText(ctx)
	for _, want := range []string{"Database: SQLite 3.53", "Last failed query:", "SELECT broken", "Schema:", "main.a_table", "Current SQL:", "SELECT 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("context = %q, want %q", text, want)
		}
	}
}

func TestResultsContext_rendersRowsWithNulls(t *testing.T) {
	value := "alice"
	ctx := Context{Results: sharedsql.Result{
		Columns: []string{"id", "name"},
		Rows:    [][]*string{{&value}, {nil}},
	}}
	text := ResultsContext(ctx)
	if !strings.Contains(text, "alice") || !strings.Contains(text, "NULL") {
		t.Fatalf("results context = %q, want both cells", text)
	}
}

func TestCommands_stateAware(t *testing.T) {
	cm := New()
	cm.YoloWrites = true
	cm.ShareResults = true
	labels := make([]string, 0, 4)
	for _, item := range cm.Commands() {
		labels = append(labels, item.Label)
	}
	for _, want := range []string{"/new", "/history", "/yolo-off", "/unshare-results"} {
		if !contains(labels, want) {
			t.Fatalf("commands = %v, want %q", labels, want)
		}
	}
	if contains(labels, "/yolo-on") || contains(labels, "/share-results") {
		t.Fatalf("commands = %v, want no on-toggles when already on", labels)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestCompletion_filterMoveAccept(t *testing.T) {
	c := NewCompletion([]CompletionItem{{Label: "/new"}, {Label: "/history"}, {Label: "/yolo-on"}})
	c.Filter("/")
	if !c.Visible() || len(c.Matches) != 3 {
		t.Fatalf("filtered = %#v, want all three", c.Matches)
	}
	c.Filter("/y")
	if len(c.Matches) != 1 || c.Accept().Label != "/yolo-on" {
		t.Fatalf("filtered = %#v, want only the yolo command", c.Matches)
	}
	c.Dismiss()
	if c.Visible() {
		t.Fatal("dismiss left matches")
	}
}

func TestFormatResult_rowsAndTruncation(t *testing.T) {
	value := "x"
	text := FormatResult(sharedsql.Result{Columns: []string{"id"}, Rows: [][]*string{{&value}}})
	if !strings.Contains(text, "id\nx") {
		t.Fatalf("formatted = %q, want header and row", text)
	}
	empty := FormatResult(sharedsql.Result{RowsAffected: 3})
	if !strings.Contains(empty, "3 rows affected") {
		t.Fatalf("affected = %q, want the rows-affected summary", empty)
	}
}

func TestUpdate_ignoresUnrelatedMessages(t *testing.T) {
	cm := New()
	model, event, cmd := cm.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, uikit.Layout{Width: 100, Height: 24}, noopMatcher{}, Context{})
	if event != nil || cmd != nil || model.Input.Value() != "" {
		t.Fatalf("idle chat consumed event %#v cmd %v", event, cmd != nil)
	}
}

// noopMatcher matches no bindings, standing in for root's key registry.
type noopMatcher struct{}

func (noopMatcher) Match(msg tea.KeyPressMsg, id uikit.CommandID, scopes []uikit.Scope) bool {
	return false
}

func TestDatabaseTools_gateWriteOnReadOnly(t *testing.T) {
	cm := New()
	cm.Executor = &fakeService{}
	cm.ReadOnly = true
	ctx := Context{Database: sharedsql.DatabaseInfo{Product: "SQLite"}}
	for _, td := range cm.DatabaseTools(ctx) {
		if td.Name == "sql_write" {
			t.Fatal("read-only model exposed sql_write")
		}
	}
	cm.ReadOnly = false
	found := false
	for _, td := range cm.DatabaseTools(ctx) {
		if td.Name == "sql_write" {
			found = true
		}
	}
	if !found {
		t.Fatal("writable model did not expose sql_write")
	}
}

type fakeService struct{}

func (f *fakeService) Close() error { return nil }
func (f *fakeService) Info() sharedsql.DatabaseInfo {
	return sharedsql.DatabaseInfo{}
}
func (f *fakeService) Execute(ctx context.Context, statement string) (sharedsql.Result, error) {
	return sharedsql.Result{}, nil
}
func (f *fakeService) ExecuteReadOnly(ctx context.Context, statement string) (sharedsql.Result, error) {
	return sharedsql.Result{}, nil
}
func (f *fakeService) Validate(ctx context.Context, statement string) error { return nil }
func (f *fakeService) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	return nil, nil
}
func (f *fakeService) TableInfo(ctx context.Context, table string) ([]sharedsql.ColumnInfo, error) {
	return nil, nil
}
func (f *fakeService) ListIndexes(ctx context.Context, table string) ([]sharedsql.IndexInfo, error) {
	return nil, nil
}
func (f *fakeService) CreateIndex(ctx context.Context, table string, change sharedsql.IndexChange) error {
	return nil
}
func (f *fakeService) ReplaceIndex(ctx context.Context, table, name string, change sharedsql.IndexChange) error {
	return nil
}
func (f *fakeService) DropIndex(ctx context.Context, table, name string) error { return nil }
func (f *fakeService) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	return nil, nil
}
func (f *fakeService) ListReferencingForeignKeys(ctx context.Context, table string) ([]sharedsql.ReferencingForeignKeyInfo, error) {
	return nil, nil
}
func (f *fakeService) CreateForeignKey(ctx context.Context, table string, change sharedsql.ForeignKeyChange) error {
	return nil
}
func (f *fakeService) ReplaceForeignKey(ctx context.Context, table, name string, change sharedsql.ForeignKeyChange) error {
	return nil
}
func (f *fakeService) DropForeignKey(ctx context.Context, table, name string) error { return nil }
func (f *fakeService) AlterColumn(ctx context.Context, table string, change sharedsql.ColumnChange) error {
	return nil
}
func (f *fakeService) DropColumn(ctx context.Context, table, name string) error { return nil }
func (f *fakeService) AddColumn(ctx context.Context, table string, def sharedsql.ColumnDef) error {
	return nil
}
func (f *fakeService) BrowseTable(ctx context.Context, table string, options sharedsql.BrowseOptions) (sharedsql.Result, error) {
	return sharedsql.Result{}, nil
}
