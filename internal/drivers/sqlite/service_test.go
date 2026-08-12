package sqlite

import (
	"context"
	"testing"
)

func newMemoryService(t *testing.T) *Service {
	t.Helper()
	service, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return service
}

func TestServiceInfo_reportsSQLiteVersion(t *testing.T) {
	// Given
	service := newMemoryService(t)

	// When
	info := service.Info()

	// Then
	if info.Product != "SQLite" {
		t.Errorf("product = %q, want SQLite", info.Product)
	}
	if info.Version == "" {
		t.Fatal("version is empty")
	}
}

func slicesEqual(got, want []string) bool {
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

func schemaEqual(got, want []SchemaObject) bool {
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
