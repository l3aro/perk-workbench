package workbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3aro/perk/internal/sqlite"
)

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

	model := New(target, Open(ctx))

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
	if got := model.schema.Items(); len(got) != 1 {
		t.Fatalf("schema items = %d, want 1", len(got))
	}
	if model.Database == nil {
		t.Fatal("model service = nil, want opened service")
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
	model := New(target, Open(context.Background()))

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
	model := New("", Open(context.Background()))
	if model.connection.focus != connectionFocusRecent {
		t.Fatalf("connection focus = %d, want recent connections", model.connection.focus)
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
	model := New("", Open(context.Background()))
	model.State, model.Database = stateReady, service
	return model
}

func stringPointer(value string) *string { return &value }
