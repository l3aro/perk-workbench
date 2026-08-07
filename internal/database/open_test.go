package database

import (
	"context"
	"os"
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
		opened, err := Open(context.Background(), target)
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
