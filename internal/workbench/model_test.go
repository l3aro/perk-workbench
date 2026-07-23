package workbench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk/internal/database"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

var testOpen OpenDatabase = database.Open

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
	service, err := sqlite.Open(ctx, target)
	if err != nil {
		t.Fatalf("opening fixture database: %v", err)
	}
	if _, err := service.Execute(ctx, "CREATE TABLE projects (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("closing fixture database before workbench open: %v", err)
	}

	model := New(target, ctx, testOpen)

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
	if got := model.schema.Items(); len(got) != 2 {
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
	model := New(target, context.Background(), testOpen)

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
	model := New("", context.Background(), testOpen)
	if model.connection.focus != connectionFocusRecent {
		t.Fatalf("connection focus = %d, want recent connections", model.connection.focus)
	}
}

func TestNew_schemaListUsesSimpleTableRows(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	if err := model.schema.SetItems([]list.Item{schemaItem{title: "projects"}}); err != nil {
		t.Fatalf("setting schema items: %v", err)
	}
	model.schema.SetSize(20, 5)

	// When
	view := ansi.Strip(model.schema.View())

	// Then
	if !strings.Contains(view, "> projects") {
		t.Fatalf("schema list = %q, want simple selected row", view)
	}
}

func TestSchemaTree_groups_tables_under_databases(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus = stateReady, focusSchema
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
		{Database: "app", Type: "database", Name: "app"},
		{Database: "app", Type: "table", Name: "accounts"},
	})
	model.schema.SetSize(30, 8)

	// When
	expanded := ansi.Strip(model.schema.View())
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	collapsed := ansi.Strip(model.schema.View())

	// Then
	for _, label := range []string{"▾ analytics", "└ events", "▾ app", "└ accounts"} {
		if !strings.Contains(expanded, label) {
			t.Fatalf("expanded schema tree = %q, want %q", expanded, label)
		}
	}
	if !strings.Contains(collapsed, "▸ analytics") || strings.Contains(collapsed, "└ events") {
		t.Fatalf("collapsed schema tree = %q, want analytics root without child tables", collapsed)
	}
}

func readyModel(t *testing.T) Model {
	t.Helper()
	service, err := sqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("closing test service: %v", err)
		}
	})
	model := New("", context.Background(), testOpen)
	model.State, model.Database = stateReady, service
	return model
}

func stringPointer(value string) *string { return &value }
