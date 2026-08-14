package database_test

// The shim tests live in an external test package so a fake plugin driver
// registered here never pollutes the in-package registry assertions
// (internal and external test files compile into separate binaries).

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
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

	spec, ok := database.ByName("redis")
	if !ok {
		t.Fatal("redis not registered after RegisterShim")
	}
	if spec.Display != "Redis" || spec.Form == nil || len(spec.Targets) != 2 {
		t.Fatalf("redis spec = %+v, want display, form, and two target forms", spec)
	}

	drivers := database.FormDrivers()
	if len(drivers) < 4 {
		t.Fatalf("FormDrivers = %d drivers, want the built-ins plus redis", len(drivers))
	}
	if drivers[0].Name != "sqlite" || drivers[1].Name != "mysql" || drivers[2].Name != "postgres" {
		t.Fatalf("built-in driver order = %v, want sqlite/mysql/postgres first", driverNames(drivers[:3]))
	}
	if drivers[len(drivers)-1].Name != "redis" {
		t.Fatalf("last driver = %q, want redis appended after the built-ins", drivers[len(drivers)-1].Name)
	}

	matched, dsn, ok := database.Match("redis:svc:6379")
	if !ok || matched.Name != "redis" || dsn != "svc:6379" {
		t.Fatalf("Match(redis:) = %q/%q/%t, want redis with label stripped", matched.Name, dsn, ok)
	}
	matched, dsn, ok = database.Match("redis://svc:6379")
	if !ok || matched.Name != "redis" || dsn != "redis://svc:6379" {
		t.Fatalf("Match(redis://) = %q/%q/%t, want redis with scheme kept", matched.Name, dsn, ok)
	}
	if _, _, ok := database.Match("plain.db"); ok {
		t.Fatal("Match(plain.db) matched, want the SQLite fallback to own it")
	}

	target, ok := database.BuildTarget(spec, database.FormValues{Host: "svc", Port: "6379"})
	if !ok || target != "redis:svc:6379" {
		t.Fatalf("BuildTarget = %q/%t, want the shim grammar", target, ok)
	}

	// The full open path treats the shim driver like a compiled-in one.
	opened, err := database.Open(context.Background(), "redis://svc:6379")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Target != "redis://svc:6379" || opened.Info.Product != "Redis" {
		t.Fatalf("opened = %+v, want the kept target and the shim service", opened)
	}
	if err := opened.Service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func driverNames(specs []database.Spec) []string {
	names := make([]string, len(specs))
	for i, spec := range specs {
		names[i] = spec.Name
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
	if _, ok := database.BuildTarget(database.Spec{Name: "sidekick"}, database.FormValues{}); ok {
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

// TestRegisterShim_rejectsCrossDriverOverlap registers drivers whose
// target prefixes shadow or are shadowed by another driver's; matching is
// registration-order based, so cross-driver overlap is ambiguous and must
// be rejected. Overlaps declared within one driver stay legal.
func TestRegisterShim_rejectsCrossDriverOverlap(t *testing.T) {
	register := func(name string, targets []database.TargetPattern) error {
		return database.RegisterShim(shimFunc(func() database.Capabilities {
			return database.Capabilities{Name: name, Targets: targets}
		}))
	}

	if err := register("olap", []database.TargetPattern{{Prefix: "olap:"}}); err != nil {
		t.Fatalf("RegisterShim(olap) = %v, want success", err)
	}

	// A new pattern that extends an existing driver's prefix.
	err := register("olapext", []database.TargetPattern{{Prefix: "olap:ext"}})
	if err == nil {
		t.Fatal("RegisterShim with a prefix extending another driver succeeded, want error")
	}
	if !strings.Contains(err.Error(), "olap:ext") || !strings.Contains(err.Error(), "olap:") {
		t.Fatalf("overlap error = %v, want it to mention both prefixes", err)
	}

	// The reverse: a new pattern that is a prefix of an existing one.
	err = register("olaprev", []database.TargetPattern{{Prefix: "olap"}})
	if err == nil {
		t.Fatal("RegisterShim with a prefix shadowed by another driver succeeded, want error")
	}
	if !strings.Contains(err.Error(), "olap") || (!strings.Contains(err.Error(), "olap:ext") && !strings.Contains(err.Error(), "olap:")) {
		t.Fatalf("reverse overlap error = %v, want it to mention both prefixes", err)
	}

	// Exact-equal prefixes are overlap too, even with a fresh driver name.
	if err := register("olapeq", []database.TargetPattern{{Prefix: "eq:"}}); err != nil {
		t.Fatalf("RegisterShim(olapeq) = %v, want success", err)
	}
	err = register("olapeq2", []database.TargetPattern{{Prefix: "eq:"}})
	if err == nil {
		t.Fatal("RegisterShim with an exact-equal prefix succeeded, want error")
	}
	if !strings.Contains(err.Error(), "eq:") {
		t.Fatalf("equal-prefix error = %v, want it to mention the prefix", err)
	}

	// Ordered overlap within one driver stays legal: the scheme form is
	// declared before the label form it would otherwise shadow.
	if err := register("kvshim", []database.TargetPattern{
		{Prefix: "kv://", KeepTarget: true},
		{Prefix: "kv:"},
	}); err != nil {
		t.Fatalf("RegisterShim(same-driver ordered overlap) = %v, want success", err)
	}
}
