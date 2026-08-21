package database

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestOpenRoutesMongoTarget verifies that mongo-prefixed and mongodb://
// targets reach the MongoDB driver. It needs a live server:
// PERK_WORKBENCH_TEST_MONGO_URI=mongodb://localhost:27017/perk-test
func TestOpenRoutesMongoTarget(t *testing.T) {
	uri := os.Getenv("PERK_WORKBENCH_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("PERK_WORKBENCH_TEST_MONGO_URI not set; skipping live MongoDB routing test")
	}
	for _, target := range []string{uri, "mongo:" + uri} {
		opened, err := Open(context.Background(), "mongodb", target)
		if err != nil {
			t.Fatalf("Open(%q) error = %v", target, err)
		}
		if opened.Info.Product != "MongoDB" {
			t.Fatalf("Open(%q) product = %q, want MongoDB", target, opened.Info.Product)
		}
		foundRoot := false
		for _, object := range opened.Objects {
			if object.Type == "database" {
				foundRoot = true
			}
		}
		if !foundRoot {
			t.Fatalf("Open(%q) objects = %+v, want a database root", target, opened.Objects)
		}
		if strings.HasPrefix(target, "mongo:") && opened.Target != strings.TrimPrefix(target, "mongo:") {
			t.Fatalf("Open(%q) target = %q, want prefix stripped", target, opened.Target)
		}
		if err := opened.Service.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

// TestOpen_sqliteFallbackCarriesSQLQueryLanguage: an unmatched target
// opens through the SQLite fallback, and the returned Opened carries the
// legacy SQL query language so the editor never sees a zero
// advertisement for a compiled-in SQL backend.
func TestOpen_sqliteFallbackCarriesSQLQueryLanguage(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fallback.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("creating fixture database: %v", err)
	}
	opened, err := Open(context.Background(), "sqlite", target)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", target, err)
	}
	defer opened.Service.Close()
	if !reflect.DeepEqual(opened.QueryLanguage, SQLQueryLanguage) {
		t.Fatalf("Open(%q) query language = %+v, want the SQL default", target, opened.QueryLanguage)
	}
}
