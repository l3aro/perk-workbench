package database_test

// The shim tests live in an external test package so a fake plugin driver
// registered here never pollutes the in-package registry assertions
// (internal and external test files compile into separate binaries).

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// fakeRedisShim stands in for a plugin transport serving a redis driver:
// declarative capabilities plus the dialect (target serialization and
// service opening) bridged over the wire in a real transport.
type fakeRedisShim struct{}

func (fakeRedisShim) Capabilities() database.Capabilities {
	return database.Capabilities{
		Name:    "redis",
		Display: "Redis",
		Driver:  "redis",
		Targets: []database.TargetPattern{
			{Prefix: "redis://", KeepTarget: true},
			{Prefix: "redis:"},
		},
		Form: &database.FormSpec{
			Prefix: "redis:",
			Fields: []database.FormField{
				{Key: "host", Title: "Host", Kind: database.FormInput, Placeholder: "localhost"},
				{Key: "port", Title: "Port", Kind: database.FormInput, Default: "6379", Validate: database.FormPort, Error: "port must be between 1 and 65535"},
			},
		},
	}
}

func (fakeRedisShim) BuildTarget(values database.FormValues) (string, bool) {
	return "redis:" + values.Host + ":" + values.Port, true
}

func (fakeRedisShim) Open(ctx context.Context, target string) (sharedsql.Service, error) {
	return &stubService{}, nil
}

// stubService is a minimal Service for Open flows; embedding the
// interface keeps the fake to the methods the flow actually calls.
type stubService struct {
	sharedsql.Service
}

func (s *stubService) Close() error { return nil }

func (s *stubService) Info() sharedsql.DatabaseInfo {
	return sharedsql.DatabaseInfo{Product: "Redis", Version: "fake"}
}

func (s *stubService) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	return nil, nil
}

func TestRegisterShim_makesDriverIndistinguishable(t *testing.T) {
	if err := database.RegisterShim(fakeRedisShim{}); err != nil {
		t.Fatalf("RegisterShim: %v", err)
	}
	spec, ok := database.ByPlugin("redis")
	if !ok {
		t.Fatal("redis not registered after RegisterShim")
	}
	if spec.Display != "Redis" || spec.Form == nil || len(spec.Targets) != 2 {
		t.Fatalf("redis spec = %+v, want display, form, and two target forms", spec)
	}
	plugins := database.FormPlugins()
	if len(plugins) < 4 {
		t.Fatalf("FormPlugins = %d plugins, want built-ins plus redis", len(plugins))
	}
	if plugins[len(plugins)-1].PluginID != "redis" {
		t.Fatalf("last plugin = %q, want redis", plugins[len(plugins)-1].PluginID)
	}
	matches := database.Matches("redis:svc:6379")
	if len(matches) != 1 || matches[0].Spec.PluginID != "redis" || matches[0].DSN != "svc:6379" {
		t.Fatalf("Matches(redis:) = %+v, want redis with label stripped", matches)
	}
	matches = database.Matches("redis://svc:6379")
	if len(matches) != 1 || matches[0].Spec.PluginID != "redis" || matches[0].DSN != "redis://svc:6379" {
		t.Fatalf("Matches(redis://) = %+v, want redis with scheme kept", matches)
	}
	if len(database.Matches("plain.db")) != 0 {
		t.Fatal("Matches(plain.db) matched, want the SQLite fallback to own it")
	}
	target, ok := database.BuildTarget(spec, database.FormValues{Host: "svc", Port: "6379"})
	if !ok || target != "redis:svc:6379" {
		t.Fatalf("BuildTarget = %q/%t, want the shim grammar", target, ok)
	}
	opened, err := database.Open(context.Background(), "redis", "redis://svc:6379")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Target != "redis://svc:6379" || opened.Info.Product != "Redis" {
		t.Fatalf("opened = %+v, want the kept target and shim service", opened)
	}
	if err := opened.Service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func driverNames(specs []database.Spec) []string {
	names := make([]string, len(specs))
	for i, spec := range specs {
		names[i] = spec.PluginID
	}
	return names
}

func TestRegisterShim_rejectsMisconfigured(t *testing.T) {
	if err := database.RegisterShim(nil); err == nil {
		t.Fatal("RegisterShim(nil) succeeded, want an error")
	}

	base := fakeRedisShim{}.Capabilities()

	noName := base
	noName.Name = ""
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return noName })); err == nil {
		t.Fatal("RegisterShim without a name succeeded, want an error")
	}

	blankName := base
	blankName.Name = " \t"
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return blankName })); err == nil {
		t.Fatal("RegisterShim with whitespace-only name succeeded, want an error")
	}

	trimmed := base
	trimmed.Name = "  redis-copy  "
	trimmed.Driver = "  redis  "
	trimmed.Targets = []database.TargetPattern{{Prefix: "redis-copy:"}}
	trimmed.Form = nil
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return trimmed })); err != nil {
		t.Fatalf("RegisterShim with padded identities = %v, want nil", err)
	}
	if _, ok := database.ByPlugin("redis-copy"); !ok {
		t.Fatal("trimmed plugin identity was not registered")
	}

	if err := database.RegisterShim(fakeRedisShim{}); err == nil {
		t.Fatal("duplicate RegisterShim succeeded, want an error")
	}

	noTargets := base
	noTargets.Name, noTargets.Targets = "nokeys", nil
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return noTargets })); err == nil {
		t.Fatal("RegisterShim without target forms succeeded, want an error")
	}

	emptyPrefix := base
	emptyPrefix.Name, emptyPrefix.Targets = "catchall", []database.TargetPattern{{Prefix: ""}}
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return emptyPrefix })); err == nil {
		t.Fatal("RegisterShim with an empty target prefix succeeded, want an error")
	}

	unrouted := base
	unrouted.Name = "unrouted"
	unrouted.Targets = []database.TargetPattern{{Prefix: "kv://", KeepTarget: true}}
	unrouted.Form = &database.FormSpec{Prefix: "kv:"}
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return unrouted })); err == nil {
		t.Fatal("RegisterShim with an unrouted form prefix succeeded, want an error")
	}

	// Formless drivers (MongoDB-style) stay legal and never register a
	// target builder.
	formless := base
	formless.Name, formless.Form = "sidekick", nil
	formless.Targets = []database.TargetPattern{{Prefix: "ks:"}}
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return formless })); err != nil {
		t.Fatalf("RegisterShim(formless) = %v, want success", err)
	}
	if _, ok := database.BuildTarget(database.Spec{PluginID: "sidekick", Driver: "sidekick"}, database.FormValues{}); ok {
		t.Fatal("BuildTarget(formless driver) succeeded, want the raw-target fallback")
	}
}

// shimFunc adapts a fixed capability set to the Shim interface for
// misconfiguration cases.
type shimFunc func() database.Capabilities

func (f shimFunc) Capabilities() database.Capabilities { return f() }

func (f shimFunc) BuildTarget(database.FormValues) (string, bool) { return "", false }

func (f shimFunc) Open(context.Context, string) (sharedsql.Service, error) {
	return &stubService{}, nil
}

// TestRegisterShim_queryLanguage: the host normalizes an omitted or
// all-zero query_language advertisement to the legacy SQL default before
// registration, passes a valid advertisement through, and rejects
// invalid nonzero metadata — never silently defaulting it.
func TestRegisterShim_queryLanguage(t *testing.T) {
	register := func(name string, ql *database.QueryLanguage) error {
		caps := fakeRedisShim{}.Capabilities()
		caps.Name = name
		caps.Targets = []database.TargetPattern{{Prefix: name + ":"}}
		caps.Form = nil
		caps.QueryLanguage = ql
		return database.RegisterShim(shimFunc(func() database.Capabilities { return caps }))
	}

	for _, test := range []struct {
		name string
		ql   *database.QueryLanguage
		want database.QueryLanguage
	}{
		{name: "qldefault", want: database.SQLQueryLanguage},
		{name: "qlzero", ql: &database.QueryLanguage{}, want: database.SQLQueryLanguage},
		{
			name: "qlcustom",
			ql: &database.QueryLanguage{
				Name:        "Redis",
				EditorLabel: "Command",
				Placeholder: "Enter a command…",
				Lexer:       "redis",
				Examples:    []string{"GET user:2", "SET user:2 alice"},
			},
			want: database.QueryLanguage{
				Name:        "Redis",
				EditorLabel: "Command",
				Placeholder: "Enter a command…",
				Lexer:       "redis",
				Examples:    []string{"GET user:2", "SET user:2 alice"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := register(test.name, test.ql); err != nil {
				t.Fatalf("RegisterShim(%s) = %v, want success", test.name, err)
			}
			spec, ok := database.ByPlugin(test.name)
			if !ok {
				t.Fatalf("%s not registered", test.name)
			}
			if !reflect.DeepEqual(spec.QueryLanguage, test.want) {
				t.Fatalf("%s query language = %+v, want %+v", test.name, spec.QueryLanguage, test.want)
			}
		})
	}

	for _, test := range []struct {
		name string
		ql   *database.QueryLanguage
	}{
		{name: "qlbadname", ql: &database.QueryLanguage{Name: "  ", EditorLabel: "SQL", Placeholder: "Enter a query…"}},
		{name: "qlbadeditor", ql: &database.QueryLanguage{Name: "SQL", Placeholder: "Enter a query…"}},
		{name: "qlbadplaceholder", ql: &database.QueryLanguage{Name: "SQL", EditorLabel: "SQL"}},
		{name: "qlbadexample", ql: &database.QueryLanguage{Name: "SQL", EditorLabel: "SQL", Placeholder: "Enter a query…", Examples: []string{"select 1", ""}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := register(test.name, test.ql); err == nil {
				t.Fatalf("RegisterShim(%s) succeeded, want an error", test.name)
			}
		})
	}
}

// TestOpen_carriesMatchedDriverQueryLanguage: database.Open carries the
// matched driver's registered query language into the returned Opened,
// so the workbench can present the connection's editor language without
// re-matching the target itself.
func TestOpen_carriesMatchedDriverQueryLanguage(t *testing.T) {
	language := database.QueryLanguage{
		Name:        "KV",
		EditorLabel: "Command",
		Placeholder: "Enter a command…",
		Lexer:       "kv",
		Examples:    []string{"GET user:2"},
	}
	caps := database.Capabilities{
		Name:          "langkv",
		Display:       "KV",
		Targets:       []database.TargetPattern{{Prefix: "langkv:"}},
		QueryLanguage: &language,
	}
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return caps })); err != nil {
		t.Fatalf("RegisterShim: %v", err)
	}
	opened, err := database.Open(context.Background(), "langkv", "langkv:svc:6379")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer opened.Service.Close()
	if !reflect.DeepEqual(opened.QueryLanguage, language) {
		t.Fatalf("Open query language = %+v, want %+v", opened.QueryLanguage, language)
	}
}

func TestCapabilities_surviveJSONRoundTrip(t *testing.T) {
	caps := fakeRedisShim{}.Capabilities()
	var decoded database.Capabilities
	contents, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(caps, decoded) {
		t.Fatalf("round trip = %+v, want %+v", decoded, caps)
	}
}

func TestCapabilities_writeCapabilitiesRoundTrip(t *testing.T) {
	caps := database.Capabilities{
		Name:    "roundtrip",
		Display: "RoundTrip",
		Targets: []database.TargetPattern{{Prefix: "rt:"}},
		WriteCapabilities: sharedsql.WriteCapabilities{
			RowWriter: true,
			Document: &sharedsql.DocumentWriteCapability{
				Format: sharedsql.DocumentFormatMongoExtendedJSON,
				Text:   true,
			},
		},
	}
	var decoded database.Capabilities
	contents, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(caps, decoded) {
		t.Fatalf("round trip = %+v, want %+v", decoded, caps)
	}
}

// TestRegisterShim_allowsSharedFamiliesAndPrefixes verifies that plugin IDs
// disambiguate registrations even when families and target prefixes match.
func TestRegisterShim_allowsSharedFamiliesAndPrefixes(t *testing.T) {
	register := func(name string, targets []database.TargetPattern) error {
		return database.RegisterShim(shimFunc(func() database.Capabilities {
			return database.Capabilities{Name: name, Driver: "mysql", Targets: targets}
		}))
	}
	for _, test := range []struct {
		name    string
		targets []database.TargetPattern
	}{
		{name: "olap", targets: []database.TargetPattern{{Prefix: "olap:"}}},
		{name: "olapext", targets: []database.TargetPattern{{Prefix: "olap:ext"}}},
		{name: "olaprev", targets: []database.TargetPattern{{Prefix: "olap"}}},
		{name: "olapeq", targets: []database.TargetPattern{{Prefix: "eq:"}}},
		{name: "olapeq2", targets: []database.TargetPattern{{Prefix: "eq:"}}},
		{name: "kvshim", targets: []database.TargetPattern{{Prefix: "kv://", KeepTarget: true}, {Prefix: "kv:"}}},
	} {
		if err := register(test.name, test.targets); err != nil {
			t.Fatalf("RegisterShim(%s) = %v, want success", test.name, err)
		}
	}
	if got := len(database.Matches("eq:value")); got != 2 {
		t.Fatalf("Matches(eq:value) = %d, want two plugin instances", got)
	}
}

// TestRegisterShim_workspace: a valid workspace advertisement passes
// registration and reaches the registered spec and the opened service;
// invalid metadata is rejected — never silently defaulted.
func TestRegisterShim_workspace(t *testing.T) {
	workspace := &database.WorkspaceCapability{
		StandardTabs: []database.StandardWorkspaceTab{database.StandardWorkspaceTabColumns},
		CustomViews: []database.CustomWorkspaceView{
			{ID: "keys", Label: "Keys", Scopes: []database.WorkspaceViewKind{database.WorkspaceViewTable}},
		},
	}
	caps := fakeRedisShim{}.Capabilities()
	caps.Name = "wskv"
	caps.Targets = []database.TargetPattern{{Prefix: "wskv:"}}
	caps.Form = nil
	caps.Workspace = workspace
	if err := database.RegisterShim(shimFunc(func() database.Capabilities { return caps })); err != nil {
		t.Fatalf("RegisterShim: %v", err)
	}
	spec, ok := database.ByPlugin("wskv")
	if !ok {
		t.Fatal("wskv not registered")
	}
	if !reflect.DeepEqual(spec.Workspace, workspace) {
		t.Fatalf("spec workspace = %+v, want %+v", spec.Workspace, workspace)
	}

	opened, err := database.Open(context.Background(), "wskv", "wskv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer opened.Service.Close()
	if !reflect.DeepEqual(opened.Workspace, workspace) {
		t.Fatalf("Opened workspace = %+v, want %+v", opened.Workspace, workspace)
	}

	for _, test := range []struct {
		name      string
		workspace *database.WorkspaceCapability
	}{
		{name: "unknown standard tab", workspace: &database.WorkspaceCapability{StandardTabs: []database.StandardWorkspaceTab{"relations"}}},
		{name: "duplicate view id", workspace: &database.WorkspaceCapability{CustomViews: []database.CustomWorkspaceView{
			{ID: "Keys", Label: "One", Scopes: []database.WorkspaceViewKind{database.WorkspaceViewTable}},
			{ID: "keys", Label: "Two", Scopes: []database.WorkspaceViewKind{database.WorkspaceViewTable}},
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := fakeRedisShim{}.Capabilities()
			bad.Name = "wsbad"
			bad.Targets = []database.TargetPattern{{Prefix: "wsbad:"}}
			bad.Form = nil
			bad.Workspace = test.workspace
			if err := database.RegisterShim(shimFunc(func() database.Capabilities { return bad })); err == nil {
				t.Fatalf("RegisterShim(%s) succeeded, want an error", test.name)
			}
		})
	}
}

// TestCapabilities_workspaceJSONRoundTrip: the workspace advertisement
// survives the Capabilities JSON round trip unchanged.
func TestCapabilities_workspaceJSONRoundTrip(t *testing.T) {
	caps := fakeRedisShim{}.Capabilities()
	caps.Workspace = &database.WorkspaceCapability{
		StandardTabs: []database.StandardWorkspaceTab{database.StandardWorkspaceTabColumns, database.StandardWorkspaceTabIndexes},
		CustomViews: []database.CustomWorkspaceView{
			{ID: "keys", Label: "Keys", Scopes: []database.WorkspaceViewKind{database.WorkspaceViewTable, database.WorkspaceViewDatabase}},
		},
	}
	var decoded database.Capabilities
	contents, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(caps, decoded) {
		t.Fatalf("round trip = %+v, want %+v", decoded, caps)
	}
}
