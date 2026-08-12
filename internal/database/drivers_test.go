package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	godrv "github.com/go-sql-driver/mysql"
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

func TestFormDrivers_registrationOrderAndLabels(t *testing.T) {
	want := []struct{ name, display string }{
		{"sqlite", "SQLite"},
		{"mysql", "MySQL"},
		{"postgres", "PostgreSQL"},
	}
	drivers := FormDrivers()
	if len(drivers) != len(want) {
		t.Fatalf("FormDrivers() = %d drivers, want %d", len(drivers), len(want))
	}
	for i, spec := range drivers {
		if spec.Name != want[i].name || spec.Display != want[i].display {
			t.Fatalf("driver %d = %q/%q, want %q/%q", i, spec.Name, spec.Display, want[i].name, want[i].display)
		}
		if spec.Form == nil {
			t.Fatalf("driver %q has no form spec", spec.Name)
		}
	}
	mongo, ok := ByName("mongodb")
	if !ok {
		t.Fatal("mongodb not registered")
	}
	if mongo.Form != nil {
		t.Fatal("mongodb has a form spec, want none (target-URL only)")
	}
}

func TestFormSpecs_carryDeclarativeMetadata(t *testing.T) {
	sqlite, _ := ByName("sqlite")
	if sqlite.Form.Prefix != "" {
		t.Fatalf("sqlite prefix = %q, want none", sqlite.Form.Prefix)
	}
	field := sqlite.Form.Fields[0]
	if field.Key != "target" || field.Validate != FormRequired || field.Error != "target is required" {
		t.Fatalf("sqlite target field = %+v, want required validation", field)
	}

	mysql, _ := ByName("mysql")
	if mysql.Form.Prefix != "mysql:" {
		t.Fatalf("mysql prefix = %q, want mysql:", mysql.Form.Prefix)
	}
	if port := mysqlField(mysql, "port"); port.Default != "3306" || port.Validate != FormPort {
		t.Fatalf("mysql port field = %+v, want default 3306 with port validation", port)
	}
	tls := mysqlField(mysql, "tls")
	if len(tls.Options) != 3 || tls.Options[0].Value != "true" || tls.Options[1].Value != "skip-verify" || tls.Options[2].Value != "false" {
		t.Fatalf("mysql TLS options = %+v, want verify/skip-verify/disabled values", tls.Options)
	}

	postgres, _ := ByName("postgres")
	if postgres.Form.Prefix != "postgres:" {
		t.Fatalf("postgres prefix = %q, want postgres:", postgres.Form.Prefix)
	}
	if port := mysqlField(postgres, "port"); port.Default != "5432" {
		t.Fatalf("postgres port default = %q, want 5432", port.Default)
	}
	tls = mysqlField(postgres, "tls")
	if len(tls.Options) != 3 || tls.Options[0].Value != "verify-full" || tls.Options[1].Value != "require" || tls.Options[2].Value != "disable" {
		t.Fatalf("postgres TLS options = %+v, want verify-full/require/disable values", tls.Options)
	}
}

func mysqlField(spec Spec, key string) FormField {
	for _, field := range spec.Form.Fields {
		if field.Key == key {
			return field
		}
	}
	return FormField{}
}

func TestBuildTarget_serializesPerDriver(t *testing.T) {
	mysql, _ := ByName("mysql")
	dsn, ok := BuildTarget(mysql, FormValues{Host: "db.example.test", Port: "3307", User: "alice", Pass: "secret", Database: "app", TLS: "skip-verify"})
	if !ok {
		t.Fatal("BuildTarget(mysql) reported no builder")
	}
	parsed, err := godrv.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parsing mysql target %q: %v", dsn, err)
	}
	if parsed.User != "alice" || parsed.Passwd != "secret" || parsed.Addr != "db.example.test:3307" || parsed.DBName != "app" || parsed.TLSConfig != "skip-verify" {
		t.Fatalf("mysql target = %#v, want form fields serialized", parsed)
	}

	postgres, _ := ByName("postgres")
	target, ok := BuildTarget(postgres, FormValues{Host: "db.example.test", Port: "5433", User: "alice", Pass: "secret", Database: "app", TLS: "require"})
	if !ok {
		t.Fatal("BuildTarget(postgres) reported no builder")
	}
	if target != "postgres://alice:secret@db.example.test:5433/app?sslmode=require" {
		t.Fatalf("postgres target = %q, want URL form", target)
	}

	sqlite, _ := ByName("sqlite")
	target, ok = BuildTarget(sqlite, FormValues{Database: " /tmp/a.db "})
	if !ok || target != "/tmp/a.db" {
		t.Fatalf("sqlite target = %q/%t, want trimmed raw path", target, ok)
	}

	// Unknown drivers degrade gracefully: no builder, caller falls back.
	if _, ok := BuildTarget(Spec{Name: "nope", Form: &FormSpec{}}, FormValues{Database: "/tmp/x.db"}); ok {
		t.Fatal("BuildTarget(unknown driver) succeeded, want no builder")
	}
	if _, ok := BuildTarget(Spec{Name: "nope"}, FormValues{Database: "/tmp/x.db"}); ok {
		t.Fatal("BuildTarget(driver without form) succeeded, want none")
	}
}
