package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

var testOpen OpenDatabase = openTestDatabase

func TestOpen_existing_target_populates_schema(t *testing.T) {
	// Given
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "existing.db")
	file, err := os.Create(target)
	if err != nil {
		t.Fatalf("creating fixture database: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing fixture database file: %v", err)
	}
	service, err := openTestSQLite(ctx, target)
	if err != nil {
		t.Fatalf("opening fixture database: %v", err)
	}
	if _, err := service.Execute(ctx, "CREATE TABLE projects (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("closing fixture database before workbench open: %v", err)
	}

	model := New(target, ctx, testOpen, false)

	// When
	message := model.Init()()
	updated, _ := model.Update(message)
	model = updated.(Model)

	// Then
	if model.State != stateReady {
		t.Fatalf("model state = %v, want ready", model.State)
	}
	if model.Focus != focusSchema {
		t.Fatalf("model focus = %v, want schema", model.Focus)
	}
	if got := model.schema.component.List.Items(); len(got) != 2 {
		t.Fatalf("schema items = %d, want database root and table", len(got))
	}
	if model.Database == nil {
		t.Fatal("model service = nil, want opened service")
	}
	if model.databaseInfo.Product != "SQLite" || model.databaseInfo.Version == "" {
		t.Fatalf("database info = %#v, want SQLite version", model.databaseInfo)
	}
	t.Cleanup(func() {
		if model.Database != nil {
			if err := model.Database.Close(); err != nil {
				t.Errorf("closing workbench service: %v", err)
			}
		}
	})
}

func TestOpen_missing_target_is_a_recoverable_failure(t *testing.T) {
	// Given
	target := filepath.Join(t.TempDir(), "missing.db")
	model := New(target, context.Background(), testOpen, false)

	// When
	message := model.Init()()
	updated, _ := model.Update(message)
	model = updated.(Model)

	// Then
	if model.State != stateFailure {
		t.Fatalf("model state = %v, want failure", model.State)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("missing target was created or could not be inspected: %v", err)
	}
}

func TestNew_connectionScreenFocusesRecentConnections(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	if model.connection.component.Form.Focus != connectionFocusRecent {
		t.Fatalf("connection focus = %d, want recent connections", model.connection.component.Form.Focus)
	}
}

func TestNew_schemaListUsesSimpleTableRows(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	if err := model.schema.component.List.SetItems([]list.Item{schema.Item{Name: "projects"}}); err != nil {
		t.Fatalf("setting schema items: %v", err)
	}
	model.schema.component.List.SetSize(20, 5)

	// When
	view := ansi.Strip(model.schema.component.List.View())

	// Then
	if strings.Contains(view, "> ") {
		t.Fatalf("schema list = %q, selected-item shift still present", view)
	}
}

func TestSchemaTree_groups_tables_under_databases(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
		{Database: "app", Type: "database", Name: "app"},
		{Database: "app", Type: "table", Name: "accounts"},
	})
	model.schema.component.List.SetSize(30, 8)

	// When
	expanded := ansi.Strip(model.schema.component.List.View())
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)
	collapsed := ansi.Strip(model.schema.component.List.View())

	// Then
	for _, label := range []string{"▣ analytics", "  ▪ events", "▣ app", "  ▪ accounts"} {
		if !strings.Contains(expanded, label) {
			t.Fatalf("expanded schema tree = %q, want %q", expanded, label)
		}
	}
	for _, label := range []string{"▣ analytics", "▣ app"} {
		if !strings.HasSuffix(strings.TrimRight(sidebarRow(expanded, label), " "), " (1)") {
			t.Fatalf("expanded schema tree = %q, want a right-aligned count badge on %s", expanded, label)
		}
	}
	if strings.Contains(collapsed, "  ▪ events") {
		t.Fatalf("collapsed schema tree = %q, want analytics root without child tables", collapsed)
	}
}

func TestSchemaTree_mysqlLoadExpandsConnectedDatabase(t *testing.T) {
	// Given — a MySQL session connected to app; the tree loads with app
	// expanded and analytics collapsed, not every database at once.
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	model.Target = "mysql:alice:secret@tcp(db.example.test:3306)/app"
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
		{Database: "app", Type: "database", Name: "app"},
		{Database: "app", Type: "table", Name: "users"},
	})
	model.schema.component.List.SetSize(30, 8)

	// Then
	view := ansi.Strip(model.schema.component.List.View())
	if !strings.Contains(view, "  ▪ users") {
		t.Fatalf("loaded tree = %q, want app expanded with users", view)
	}
	if strings.Contains(view, "  ▪ events") {
		t.Fatalf("loaded tree = %q, want analytics collapsed", view)
	}
}

func TestSchemaTree_mysqlExpandsOneDatabaseAtATime(t *testing.T) {
	// Given — two MySQL databases with tables; only the first root is
	// expanded at load.
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "app", Type: "database", Name: "app"},
		{Database: "app", Type: "table", Name: "users"},
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
	})
	model.schema.component.List.SetSize(30, 8)

	// When — Enter on the analytics root.
	model.schema.component.List.Select(2)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)

	// Then — analytics is expanded and app collapsed: one at a time.
	view := ansi.Strip(model.schema.component.List.View())
	for _, label := range []string{"▣ analytics", "  ▪ events"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expanded analytics tree = %q, want %q", view, label)
		}
	}
	if strings.Contains(view, "  ▪ users") {
		t.Fatalf("expanded analytics tree = %q, want app tables collapsed", view)
	}

	// When — Enter again on the analytics root (the rebuild moved the
	// selection; the root now sits at index 1). Selecting the root
	// focused the workspace, so the sidebar takes focus back first.
	model.Focus = focusSchema
	model.schema.component.List.Select(1)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)

	// Then — everything is collapsed; app tables stay hidden until app is
	// expanded again.
	view = ansi.Strip(model.schema.component.List.View())
	if strings.Contains(view, "  ▪ events") || strings.Contains(view, "  ▪ users") {
		t.Fatalf("collapsed tree = %q, want no database tables", view)
	}
}

func TestSchemaTree_postgresExpandsOneSchemaAtATime(t *testing.T) {
	// Given — a PostgreSQL database with two schemas; only the first
	// schema is expanded at load.
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
		{Database: "main", Type: "schema", Name: "audit"},
		{Database: "main", Type: "table", Name: "audit.events"},
	})
	model.schema.component.List.SetSize(30, 8)

	// When — Enter on the audit schema row.
	model.schema.component.List.Select(3)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)

	// Then — audit is expanded and public collapsed: one at a time.
	view := ansi.Strip(model.schema.component.List.View())
	for _, label := range []string{"  ▤ audit", "    ▪ events"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expanded audit tree = %q, want %q", view, label)
		}
	}
	if strings.Contains(view, "    ▪ accounts") {
		t.Fatalf("expanded audit tree = %q, want public tables collapsed", view)
	}
}

func TestSchemaTree_collections_count_in_database_badge(t *testing.T) {
	// MongoDB lists collections with Type "collection"; they must count
	// toward the database root badge like tables and views.
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	model.databaseInfo.Product = "MongoDB"
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "mydb", Type: "database", Name: "mydb"},
		{Database: "mydb", Type: "collection", Name: "users"},
		{Database: "mydb", Type: "collection", Name: "orders"},
	})
	model.schema.component.List.SetSize(30, 8)

	expanded := ansi.Strip(model.schema.component.List.View())
	for _, label := range []string{"▣ mydb", "  ▪ users", "  ▪ orders"} {
		if !strings.Contains(expanded, label) {
			t.Fatalf("expanded schema tree = %q, want %q", expanded, label)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(sidebarRow(expanded, "▣ mydb"), " "), " (2)") {
		t.Fatalf("expanded schema tree = %q, want (2) count badge on mydb", expanded)
	}
}

func TestSchemaTree_stateColors(t *testing.T) {
	// Given — the workspace has accounts open; the sidebar marks the path
	// from the connected root down to that table.
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
		{Database: "main", Type: "table", Name: "public.orders"},
		{Database: "archive", Type: "database", Name: "archive"},
	})
	model.schema.component.List.SetSize(30, 8)
	model.SelectedTable = "public.accounts"
	_ = model.rebuildSchemaTree()
	model.schema.component.List.Select(0)
	// The marker renders bold (heavier strokes, same cell), the label
	// regular; both carry the state color.
	row := func(marker, indent, label, color string) string {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		return indent + style.Bold(true).Render(marker+" ") + style.Render(label)
	}

	// Then — each level has its own marker, and state shows only in color:
	// the open path is secondary, the selected row is primary, everything
	// else is idle muted. Count badges pin to the row's right edge instead
	// of sitting inline after the name.
	view := model.schema.component.List.View()
	for _, label := range []string{"▣ main", "  ▤ public", "    ▪ accounts", "    ▪ orders", "▣ archive"} {
		if !strings.Contains(ansi.Strip(view), label) {
			t.Fatalf("schema tree = %q, want %q", ansi.Strip(view), label)
		}
	}
	if !strings.Contains(view, row("▣", "", "main", colorPrimary)) {
		t.Fatalf("selected row = %q, want primary color", view)
	}
	if !strings.Contains(view, row("▤", "  ", "public", colorSecondary)) || !strings.Contains(view, row("▪", "    ", "accounts", colorSecondary)) {
		t.Fatalf("open path rows = %q, want secondary color", view)
	}
	if !strings.Contains(view, row("▪", "    ", "orders", colorMuted)) || !strings.Contains(view, row("▣", "", "archive", colorMuted)) {
		t.Fatalf("idle rows = %q, want muted color", view)
	}
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary)).Render(" (2)")
	if !strings.Contains(view, badge) {
		t.Fatalf("selected row = %q, want the count badge in the row color", view)
	}

	// When — Enter opens the orders table: the path moves with it.
	model.schema.component.List.Select(3)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	// selectSchemaTable schedules the sidebar rebuild in its command batch;
	// run it directly here because the batch's table-load commands need a
	// real service (the stub panics). SetItems refreshes the list
	// synchronously; the returned command only feeds filter matches.
	model.rebuildSchemaTree()
	// Move the selection off the opened table: the path keeps its color.
	model.schema.component.List.Select(2)
	view = model.schema.component.List.View()
	if !strings.Contains(view, row("▪", "    ", "orders", colorSecondary)) {
		t.Fatalf("opened table row = %q, want secondary color", view)
	}
	if !strings.Contains(view, row("▪", "    ", "accounts", colorPrimary)) {
		t.Fatalf("selected table = %q, want primary color", view)
	}

	// When — no table is open, only the selection keeps a non-idle color.
	model.SelectedTable = ""
	_ = model.rebuildSchemaTree()
	model.schema.component.List.Select(2)
	view = model.schema.component.List.View()
	if !strings.Contains(view, row("▣", "", "main", colorMuted)) {
		t.Fatalf("root without open table = %q, want muted color", view)
	}
	if !strings.Contains(view, row("▪", "    ", "accounts", colorPrimary)) {
		t.Fatalf("selected table = %q, want primary color", view)
	}
	if !strings.Contains(view, row("▪", "    ", "orders", colorMuted)) {
		t.Fatalf("idle table = %q, want muted color", view)
	}
}

func TestSchemaClick_selectsTheRenderedTable(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.Focus = focusSchema
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "table", Name: "accounts"},
		{Database: "main", Type: "table", Name: "projects"},
	})

	for _, want := range []string{"accounts", "projects"} {
		lines := strings.Split(ansi.Strip(model.View().Content), "\n")
		tableY := -1
		for y, line := range lines {
			if strings.Contains(line, "  ▪ "+want) {
				tableY = y
				break
			}
		}
		if tableY < 0 {
			t.Fatalf("rendered schema does not contain %s", want)
		}

		updated, _ := model.Update(tea.MouseClickMsg{X: 2, Y: tableY, Button: tea.MouseLeft})
		model = updated.(Model)
		if got := model.SelectedTable; got != want {
			t.Fatalf("click on rendered %s selected table %q", want, got)
		}
		model.Focus = focusSchema
	}
}

func readyModel(t *testing.T) Model {
	t.Helper()
	service, err := openTestSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("closing test service: %v", err)
		}
	})
	model := New("", context.Background(), testOpen, false)
	model.queryLog.path = t.TempDir() + "/data.db"
	model.queryLog.component.Entries = nil
	model.queryLog.component.Render()
	model.State, model.Database = stateReady, service
	model.chat.component.Executor = chatExecutor{service: service}
	return model
}

func firstQuerySucceeded(messages []tea.Msg) (querySucceededMsg, bool) {
	for _, message := range messages {
		if typed, ok := message.(querySucceededMsg); ok {
			return typed, true
		}
	}
	return querySucceededMsg{}, false
}

func stringPointer(value string) *string { return &value }

func assertOneUUIDv7Profile(t *testing.T, loaded []profile.Profile) {
	t.Helper()
	if len(loaded) != 1 {
		t.Fatalf("saved profiles = %#v, want exactly one", loaded)
	}
	if !profile.ValidID(loaded[0].ID) {
		t.Fatalf("profile ID = %q, want a UUIDv7", loaded[0].ID)
	}
}

// TestConnectionProfiles_successfulOpenPathsRecordOneProfile guards every
// successful-open entry path (startup target, database picker, connection
// form): each must leave exactly one saved profile with a nonempty UUIDv7 ID,
// while a failed open records nothing.
func TestConnectionProfiles_successfulOpenPathsRecordOneProfile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "open.db")
	file, err := os.Create(target)
	if err != nil {
		t.Fatalf("creating fixture database: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing fixture database file: %v", err)
	}
	service, err := openTestSQLite(context.Background(), target)
	if err != nil {
		t.Fatalf("opening fixture database: %v", err)
	}
	if _, err := service.Execute(context.Background(), "CREATE TABLE projects (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("closing fixture database: %v", err)
	}
	closeOpened := func(model Model) {
		t.Helper()
		if model.Database != nil {
			if err := model.Database.Close(); err != nil {
				t.Errorf("closing workbench service: %v", err)
			}
		}
	}

	t.Run("startup target", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		model := New(target, context.Background(), testOpen, false)
		updated, _ := model.Update(model.Init()())
		model = updated.(Model)
		if model.State != stateReady {
			t.Fatalf("state = %v, want ready", model.State)
		}
		loaded, _, _ := profile.Load(model.connection.component.Path)
		assertOneUUIDv7Profile(t, loaded)
		closeOpened(model)
	})

	t.Run("database picker", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		model := New("", context.Background(), testOpen, false)
		updated, command := model.Update(pickerSelectionMsg{target: target})
		model = updated.(Model)
		if command == nil {
			t.Fatal("picker selection sent no open command")
		}
		model = driveCommand(model, command)
		if model.State != stateReady {
			t.Fatalf("state = %v, want ready", model.State)
		}
		loaded, _, _ := profile.Load(model.connection.component.Path)
		assertOneUUIDv7Profile(t, loaded)
		closeOpened(model)
	})

	t.Run("connection form", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		model := New("", context.Background(), testOpen, false)
		model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Form", target
		updated, command := model.openConnection()
		model = updated.(Model)
		if command == nil {
			t.Fatal("connection form sent no open command")
		}
		updated, _ = model.Update(command())
		model = updated.(Model)
		if model.State != stateReady {
			t.Fatalf("state = %v, want ready", model.State)
		}
		loaded, _, _ := profile.Load(model.connection.component.Path)
		assertOneUUIDv7Profile(t, loaded)
		closeOpened(model)
	})

	t.Run("failed open records nothing", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		missing := filepath.Join(t.TempDir(), "missing.db")
		model := New(missing, context.Background(), testOpen, false)
		updated, _ := model.Update(model.Init()())
		model = updated.(Model)
		if model.State != stateFailure {
			t.Fatalf("state = %v, want failure", model.State)
		}
		if model.connectionID != "" {
			t.Fatalf("failed open set connection ID %q", model.connectionID)
		}
		loaded, _, _ := profile.Load(model.connection.component.Path)
		if len(loaded) != 0 {
			t.Fatalf("failed open saved profiles = %#v, want none", loaded)
		}
	})
}

// TestChatContext_doesNotLeakPreviousConnection guards the disconnect reset:
// after opening A, logging a failed query, and disconnecting, opening B must
// not carry A's SQL, error, product, or schema into B's chat context.
func TestChatContext_doesNotLeakPreviousConnection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	aPath := filepath.Join(t.TempDir(), "a.db")
	file, err := os.Create(aPath)
	if err != nil {
		t.Fatalf("creating A: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing A file: %v", err)
	}
	service, err := openTestSQLite(context.Background(), aPath)
	if err != nil {
		t.Fatalf("opening A: %v", err)
	}
	if _, err := service.Execute(context.Background(), "CREATE TABLE a_table (id INTEGER)"); err != nil {
		t.Fatalf("creating A schema: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("closing A: %v", err)
	}

	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "A", aPath
	updated, command := model.openConnection()
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.State != stateReady {
		t.Fatalf("A open state = %v, want ready", model.State)
	}
	if model.connectionID == "" {
		t.Fatal("A open did not set a connection ID")
	}
	model.appendQueryLog(queryLogEntry{Statement: "SELECT broken FROM nope", Status: "failed", Message: "no such table: nope"})
	model.chat.component.LastFailedQuery = "SELECT broken FROM nope"
	model.chat.component.LastFailedError = "no such table: nope"
	if ctx := model.chat.component.ContextText(model.chatLayout()); !strings.Contains(ctx, "Last failed query") || !strings.Contains(ctx, "no such table: nope") {
		t.Fatalf("A chat context = %q, want the failed query", ctx)
	}
	t.Cleanup(func() {
		if model.Database != nil {
			_ = model.Database.Close()
		}
	})

	// Disconnect clears the connection scope, database info, and query log.
	model.disconnect()
	if model.connectionID != "" {
		t.Fatalf("disconnect kept connection ID %q", model.connectionID)
	}
	if model.databaseInfo != (sharedsql.DatabaseInfo{}) {
		t.Fatalf("disconnect kept database info %#v", model.databaseInfo)
	}
	if len(model.queryLog.component.Entries) != 0 {
		t.Fatalf("disconnect kept query log entries %#v", model.queryLog.component.Entries)
	}

	// Open B through a stub backend with a distinct product identity.
	var openedB bool
	bTarget := filepath.Join(t.TempDir(), "b.db")
	model = New("", context.Background(), func(_ context.Context, target string) (sharedsql.Opened, error) {
		if target != bTarget {
			t.Fatalf("open target = %q, want %q", target, bTarget)
		}
		openedB = true
		return sharedsql.Opened{
			Service: &stubService{},
			Info:    sharedsql.DatabaseInfo{Product: "PostgreSQL", Version: "16"},
			Objects: []sharedsql.SchemaObject{{Database: "b_db", Type: "database", Name: "b_db"}},
		}, nil
	}, false)
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "B", bTarget
	updated, command = model.openConnection()
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if !openedB || model.State != stateReady {
		t.Fatalf("B open = opened %v, state %v, want ready", openedB, model.State)
	}

	ctx := model.chat.component.ContextText(model.chatLayout())
	if strings.Contains(ctx, "SELECT broken") || strings.Contains(ctx, "no such table: nope") {
		t.Fatalf("B chat context leaked A's failed query: %q", ctx)
	}
	if strings.Contains(ctx, "a_table") {
		t.Fatalf("B chat context leaked A's schema: %q", ctx)
	}
	if strings.Contains(ctx, "SQLite") || !strings.Contains(ctx, "PostgreSQL 16") {
		t.Fatalf("B chat context = %q, want B's product only", ctx)
	}
	if !strings.Contains(ctx, "b_db") {
		t.Fatalf("B chat context = %q, want B's schema", ctx)
	}
}

type stubService struct {
	sharedsql.Service
}

func (s *stubService) Close() error { return nil }

func (s *stubService) ListForeignKeysAll(context.Context) (map[string][]sharedsql.ForeignKeyInfo, error) {
	return map[string][]sharedsql.ForeignKeyInfo{}, nil
}

func (s *stubService) ListIndexesAll(context.Context) (map[string][]sharedsql.IndexInfo, error) {
	return map[string][]sharedsql.IndexInfo{}, nil
}
