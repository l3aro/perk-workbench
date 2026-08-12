package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestMatch_routesTargetForms(t *testing.T) {
	tests := []struct {
		name              string
		target            string
		wantName, wantDSN string
		wantOK            bool
	}{
		{name: "mongo colon prefix", target: "mongo:mongodb://h/app", wantName: "mongodb", wantDSN: "mongodb://h/app", wantOK: true},
		{name: "mongo uri", target: "mongodb://h/app", wantName: "mongodb", wantDSN: "mongodb://h/app", wantOK: true},
		{name: "mongo srv uri", target: "mongodb+srv://h/app", wantName: "mongodb", wantDSN: "mongodb+srv://h/app", wantOK: true},
		{name: "mysql prefix", target: "mysql:user@tcp(h:3306)/app", wantName: "mysql", wantDSN: "user@tcp(h:3306)/app", wantOK: true},
		{name: "postgres uri", target: "postgres://u@h/app", wantName: "postgres", wantDSN: "postgres://u@h/app", wantOK: true},
		{name: "postgresql uri", target: "postgresql://u@h/app", wantName: "postgres", wantDSN: "postgresql://u@h/app", wantOK: true},
		{name: "postgres prefix", target: "postgres:host=h", wantName: "postgres", wantDSN: "host=h", wantOK: true},
		{name: "plain path", target: "/tmp/plain.db", wantOK: false},
		{name: "memory target", target: ":memory:", wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, dsn, ok := Match(test.target)
			if ok != test.wantOK {
				t.Fatalf("Match(%q) ok = %v, want %v", test.target, ok, test.wantOK)
			}
			if !ok {
				return
			}
			if spec.Name != test.wantName {
				t.Fatalf("Match(%q) driver = %q, want %q", test.target, spec.Name, test.wantName)
			}
			if dsn != test.wantDSN {
				t.Fatalf("Match(%q) dsn = %q, want %q", test.target, dsn, test.wantDSN)
			}
		})
	}
}

func TestByName(t *testing.T) {
	spec, ok := ByName("sqlite")
	if !ok {
		t.Fatal("ByName(sqlite) not found")
	}
	if spec.Name != "sqlite" {
		t.Fatalf("ByName(sqlite) name = %q", spec.Name)
	}
	if _, ok := ByName("does-not-exist"); ok {
		t.Fatal("ByName(does-not-exist) found, want missing")
	}
}

func TestRegister_duplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	Register(Spec{Name: "mysql", Open: func(context.Context, string) (sharedsql.Service, error) {
		return nil, nil
	}})
}

func TestRegister_invalidSpecPanics(t *testing.T) {
	for _, spec := range []Spec{
		{Name: "", Open: func(context.Context, string) (sharedsql.Service, error) { return nil, nil }},
		{Name: "nameless"},
	} {
		t.Run("invalid", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid Register did not panic")
				}
			}()
			Register(spec)
		})
	}
}

func TestOpenFallsBackToSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer opened.Service.Close()
	if opened.Info.Product != "SQLite" {
		t.Fatalf("Open(%q) product = %q, want SQLite", path, opened.Info.Product)
	}
	if opened.Target != path {
		t.Fatalf("Open(%q) target = %q, want resolved path unchanged", path, opened.Target)
	}
}
