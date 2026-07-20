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
	if model.state != stateReady {
		t.Fatalf("model state = %v, want ready", model.state)
	}
	if got := model.schema.Items(); len(got) != 1 {
		t.Fatalf("schema items = %d, want 1", len(got))
	}
	if model.service == nil {
		t.Fatal("model service = nil, want opened service")
	}
	t.Cleanup(func() {
		if model.service != nil {
			if err := model.service.Close(); err != nil {
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
	if model.state != stateFailure {
		t.Fatalf("model state = %v, want failure", model.state)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("missing target was created or could not be inspected: %v", err)
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
	model.state, model.service = stateReady, service
	return model
}

func stringPointer(value string) *string { return &value }
