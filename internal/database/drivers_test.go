package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	godrv "github.com/go-sql-driver/mysql"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

var officialTestSQLLanguage = SQLQueryLanguage

var officialTestMongoLanguage = QueryLanguage{
	Name:        "MongoDB",
	EditorLabel: "Command",
	Placeholder: "Enter a mongosh statement…",
	Lexer:       "javascript",
	Examples: []string{
		`db.restaurants.find({"borough": "Bronx"}).limit(5)`,
		`db.restaurants.countDocuments({"cuisine": "Chinese"})`,
		`show collections`,
	},
}

type officialTestService struct {
	sharedsql.Service
	product string
}

func (s *officialTestService) Close() error { return nil }

func (s *officialTestService) Info() sharedsql.DatabaseInfo {
	return sharedsql.DatabaseInfo{Product: s.product, Version: "test"}
}

func (s *officialTestService) ListSchema(context.Context) ([]sharedsql.SchemaObject, error) {
	return nil, nil
}

type officialTestShim struct {
	caps  Capabilities
	build func(FormValues) (string, bool)
}

func (s officialTestShim) Capabilities() Capabilities { return s.caps }

func (s officialTestShim) BuildTarget(values FormValues) (string, bool) {
	return s.build(values)
}

func (s officialTestShim) Open(context.Context, string) (sharedsql.Service, error) {
	return &officialTestService{product: s.caps.Display}, nil
}

func TestMain(m *testing.M) {
	for _, shim := range officialTestShims() {
		if err := RegisterShim(shim); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

func officialTestShims() []Shim {
	return []Shim{
		officialTestShim{
			caps: Capabilities{
				Name: "sqlite", Display: "SQLite", QueryLanguage: &officialTestSQLLanguage,
				Form: &FormSpec{Fields: []FormField{{
					Key: "target", Title: "Target*", Kind: FormInput,
					Placeholder: "path/to/database.db or :memory:",
					Validate:    FormRequired, Error: "target is required",
				}}},
			},
			build: func(values FormValues) (string, bool) {
				return strings.TrimSpace(values.Database), true
			},
		},
		officialTestShim{
			caps: Capabilities{
				Name: "mysql", Display: "MySQL", Targets: []TargetPattern{{Prefix: "mysql:"}},
				QueryLanguage: &officialTestSQLLanguage,
				Form: &FormSpec{
					Prefix: "mysql:",
					Fields: []FormField{
						{Key: "host", Title: "Host", Kind: FormInput, Placeholder: "localhost"},
						{Key: "port", Title: "Port", Kind: FormInput, Default: "3306", Validate: FormPort, Error: "port must be between 1 and 65535"},
						{Key: "username", Title: "Username*", Kind: FormInput, Validate: FormRequired, Error: "username is required"},
						{Key: "password", Title: "Password", Kind: FormPassword},
						{Key: "database", Title: "Database", Kind: FormInput, Placeholder: "Optional"},
						{Key: "tls", Title: "TLS", Kind: FormSelect, Options: []FormOption{
							{Label: "Verify certificate", Value: "true"},
							{Label: "Encrypt, don't verify certificate", Value: "skip-verify"},
							{Label: "Don't encrypt", Value: "false"},
						}},
					},
				},
			},
			build: func(values FormValues) (string, bool) {
				tls := values.TLS
				if tls == "" {
					tls = "false"
				}
				return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?tls=%s", values.User, values.Pass, values.Host, values.Port, values.Database, tls), true
			},
		},
		officialTestShim{
			caps: Capabilities{
				Name: "postgres", Display: "PostgreSQL",
				Targets: []TargetPattern{
					{Prefix: "postgres://", KeepTarget: true},
					{Prefix: "postgresql://", KeepTarget: true},
					{Prefix: "postgres:"},
				},
				QueryLanguage: &officialTestSQLLanguage,
				Form: &FormSpec{
					Prefix: "postgres:",
					Fields: []FormField{
						{Key: "host", Title: "Host", Kind: FormInput, Placeholder: "localhost"},
						{Key: "port", Title: "Port", Kind: FormInput, Default: "5432", Validate: FormPort, Error: "port must be between 1 and 65535"},
						{Key: "username", Title: "Username*", Kind: FormInput, Validate: FormRequired, Error: "username is required"},
						{Key: "password", Title: "Password", Kind: FormPassword},
						{Key: "database", Title: "Database", Kind: FormInput, Placeholder: "Optional"},
						{Key: "tls", Title: "TLS", Kind: FormSelect, Options: []FormOption{
							{Label: "Verify certificate", Value: "verify-full"},
							{Label: "Encrypt, don't verify certificate", Value: "require"},
							{Label: "Don't encrypt", Value: "disable"},
						}},
					},
				},
			},
			build: func(values FormValues) (string, bool) {
				return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", values.User, values.Pass, values.Host, values.Port, values.Database, values.TLS), true
			},
		},
		officialTestShim{
			caps: Capabilities{
				Name: "mongodb", Display: "MongoDB", QueryLanguage: &officialTestMongoLanguage,
				Targets: []TargetPattern{
					{Prefix: "mongo:"},
					{Prefix: "mongodb://", KeepTarget: true},
					{Prefix: "mongodb+srv://", KeepTarget: true},
				},
			},
			build: func(FormValues) (string, bool) { return "", false },
		},
	}
}

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
		{name: "mysql scheme via label", target: "mysql://user@h/app", wantName: "mysql", wantDSN: "//user@h/app", wantOK: true},
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

func TestQueryLanguage_builtinsAdvertise(t *testing.T) {
	for _, name := range []string{"sqlite", "mysql", "postgres"} {
		spec, ok := ByName(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if !reflect.DeepEqual(spec.QueryLanguage, SQLQueryLanguage) {
			t.Fatalf("%s query language = %+v, want the SQL default", name, spec.QueryLanguage)
		}
	}
	mongo, ok := ByName("mongodb")
	if !ok {
		t.Fatal("mongodb not registered")
	}
	want := QueryLanguage{
		Name:        "MongoDB",
		EditorLabel: "Command",
		Placeholder: "Enter a mongosh statement…",
		Lexer:       "javascript",
		Examples: []string{
			`db.restaurants.find({"borough": "Bronx"}).limit(5)`,
			`db.restaurants.countDocuments({"cuisine": "Chinese"})`,
			`show collections`,
		},
	}
	if !reflect.DeepEqual(mongo.QueryLanguage, want) {
		t.Fatalf("mongodb query language = %+v, want %+v", mongo.QueryLanguage, want)
	}
}

// TestRegister_invalidQueryLanguagePanics: a nonzero query language
// advertisement violating the invariant set — blank name, editor label,
// or placeholder after trimming, or a blank example — is rejected at
// registration. A zero advertisement stays legal (no advertisement).
func TestRegister_invalidQueryLanguagePanics(t *testing.T) {
	open := func(context.Context, string) (sharedsql.Service, error) { return nil, nil }
	for _, test := range []struct {
		name string
		ql   QueryLanguage
	}{
		{name: "blank name", ql: QueryLanguage{Name: "   ", EditorLabel: "SQL", Placeholder: "Enter a query…"}},
		{name: "blank editor label", ql: QueryLanguage{Name: "SQL", Placeholder: "Enter a query…"}},
		{name: "blank placeholder", ql: QueryLanguage{Name: "SQL", EditorLabel: "SQL"}},
		{name: "blank example", ql: QueryLanguage{Name: "SQL", EditorLabel: "SQL", Placeholder: "Enter a query…", Examples: []string{"select 1", "  "}}},
		{name: "blank command name", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "  ", Usage: "GET key", Summary: "Get"}}}},
		{name: "blank command usage", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GET", Usage: " ", Summary: "Get"}}}},
		{name: "blank command summary", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GET", Usage: "GET key", Summary: ""}}}},
		{name: "control char in command name", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GE\nT", Usage: "GET key", Summary: "Get"}}}},
		{name: "non-ASCII command name", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GÉT", Usage: "GET key", Summary: "Get"}}}},
		{name: "command name too long", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: strings.Repeat("A", sharedsql.MaxQueryCommandNameRunes+1), Usage: "GET key", Summary: "Get"}}}},
		{name: "command usage too long", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GET", Usage: strings.Repeat("u", sharedsql.MaxQueryCommandUsageRunes+1), Summary: "Get"}}}},
		{name: "command summary too long", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GET", Usage: "GET key", Summary: strings.Repeat("s", sharedsql.MaxQueryCommandSummaryRunes+1)}}}},
		{name: "duplicate command name", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{{Name: "GET", Usage: "GET key", Summary: "Get"}, {Name: "get", Usage: "GET key", Summary: "Get"}}}},
		{name: "too many commands", ql: QueryLanguage{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: manyCommands(sharedsql.MaxQueryCommands + 1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register with invalid query language did not panic")
				}
			}()
			Register(Spec{
				Name: "qlpanic", Targets: []TargetPattern{{Prefix: "qlpanic:"}},
				Open: open, QueryLanguage: test.ql,
			})
		})
	}

	// A zero advertisement passes validation without advertising.
	Register(Spec{
		Name: "qlzeroreg", Targets: []TargetPattern{{Prefix: "qlzeroreg:"}}, Open: open,
	})
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

// manyCommands builds n distinct valid command entries for cap-boundary
// tests.
func manyCommands(n int) []QueryCommand {
	commands := make([]QueryCommand, n)
	for i := range commands {
		commands[i] = QueryCommand{
			Name:    fmt.Sprintf("CMD%d", i),
			Usage:   fmt.Sprintf("CMD%d arg", i),
			Summary: "summary",
		}
	}
	return commands
}

// TestRegister_commandCatalogRoundTrip: a valid command catalog registers
// and survives the driver registry unchanged, so the editor can build its
// completion list straight from the advertisement.
func TestRegister_commandCatalogRoundTrip(t *testing.T) {
	open := func(context.Context, string) (sharedsql.Service, error) { return nil, nil }
	language := QueryLanguage{
		Name:        "KV",
		EditorLabel: "Command",
		Placeholder: "Enter a command…",
		Commands: []QueryCommand{
			{Name: "GET", Usage: "GET key", Summary: "Get the value at key"},
			{Name: "SET", Usage: "SET key value", Summary: "Set the value at key"},
		},
	}
	Register(Spec{
		Name: "qlcatalog", Targets: []TargetPattern{{Prefix: "qlcatalog:"}},
		Open: open, QueryLanguage: language,
	})
	spec, ok := ByName("qlcatalog")
	if !ok {
		t.Fatal("qlcatalog not registered")
	}
	if !reflect.DeepEqual(spec.QueryLanguage, language) {
		t.Fatalf("qlcatalog query language = %+v, want %+v", spec.QueryLanguage, language)
	}
}
