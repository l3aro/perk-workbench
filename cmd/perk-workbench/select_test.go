package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestConnectionOptions(t *testing.T) {
	profiles := []profile.Profile{
		{Plugin: "sqlite", Driver: profile.DriverSQLite, Name: "Local", Target: "/tmp/a.db"},
		{Plugin: "mysql", Driver: profile.DriverMySQL, Name: "Remote", Target: "mysql:alice@tcp(db.example.test:3306)/app", Host: "db.example.test", Port: "3306", User: "alice", ReadOnly: true},
		// Unusable targets must not be offered: an empty target and a
		// retained undecryptable envelope fail on open.
		{Driver: profile.DriverSQLite, Name: "Empty", Target: ""},
		{Driver: profile.DriverSQLite, Name: "Broken", Target: "enc:v2:deadbeef"},
	}

	options := connectionOptions(profiles)
	if len(options) != 2 {
		t.Fatalf("connectionOptions() options = %d, want 2 (unusable profiles skipped)", len(options))
	}
	if got := options[0].Key; !strings.Contains(got, "Local") || !strings.Contains(got, "sqlite") || !strings.Contains(got, "/tmp/a.db") {
		t.Fatalf("option[0] key = %q, want Local/sqlite/path before sidecar load", got)
	}
	if options[0].Value.Target != "/tmp/a.db" {
		t.Fatalf("option[0] value = %q, want /tmp/a.db", options[0].Value)
	}
	if got := options[1].Key; !strings.Contains(got, "Remote") || !strings.Contains(got, "mysql") || !strings.Contains(got, "[READONLY]") {
		t.Fatalf("option[1] key = %q, want Remote/mysql/[READONLY] before sidecar load", got)
	}
	if options[1].Value.Target != "mysql:alice@tcp(db.example.test:3306)/app" {
		t.Fatalf("option[1] value = %q, want the mysql DSN", options[1].Value)
	}
}

func TestSelectConnection_noSavedConnections(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	selected, err := selectConnection(io.Discard)
	if !errors.Is(err, errNoConnections) {
		t.Fatalf("selectConnection() error = %v, want errNoConnections", err)
	}
	if selected.Target != "" {
		t.Fatalf("selectConnection() selected = %q, want empty", selected)
	}
}

func TestSelectConnection_allProfilesUnusable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := profile.Path()
	if err != nil {
		t.Fatal(err)
	}
	profiles := []profile.Profile{
		{ID: mustProfileID(t), Driver: profile.DriverSQLite, Name: "Broken", Target: "enc:v2:deadbeef"},
	}
	if err := profile.Save(path, profiles); err != nil {
		t.Fatal(err)
	}
	selected, err := selectConnection(io.Discard)
	if !errors.Is(err, errNoConnections) {
		t.Fatalf("selectConnection() error = %v, want errNoConnections", err)
	}
	if selected.Target != "" {
		t.Fatalf("selectConnection() selected = %q, want empty", selected)
	}
}

func mustProfileID(t *testing.T) string {
	t.Helper()
	id, err := profile.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// selectTestShim registers a plugin under a run-unique plugin ID AND family
// so each select-seam test controls exactly how many plugins serve its
// family, isolated from registrations left by other tests in the binary.
type selectTestShim struct {
	id     string
	family string
}

func (s selectTestShim) Capabilities() database.Capabilities {
	return database.Capabilities{
		Name:    s.id,
		Driver:  s.family,
		Display: "MySQL",
		Targets: []database.TargetPattern{{Prefix: "mysql:"}},
		Form: &database.FormSpec{
			Prefix: "mysql:",
			Fields: []database.FormField{{Key: "database", Title: "Database", Kind: database.FormInput}},
		},
	}
}

func (s selectTestShim) BuildTarget(values database.FormValues) (string, bool) {
	return s.id + ":" + values.Database, true
}

func (selectTestShim) Open(context.Context, string) (sharedsql.Service, error) {
	return nil, errors.New("not opened at the select seam")
}

func registerSelectFamily(t *testing.T) (pluginID, family string) {
	t.Helper()
	suffix := time.Now().UnixNano()
	pluginID, family = fmt.Sprintf("mysqlsel%d", suffix), fmt.Sprintf("mysqlfam%d", suffix)
	registerShimIn(t, pluginID, family)
	return pluginID, family
}

func registerShimIn(t *testing.T, pluginID, family string) {
	t.Helper()
	if err := database.RegisterShim(selectTestShim{id: pluginID, family: family}); err != nil {
		t.Fatalf("RegisterShim(%s): %v", pluginID, err)
	}
}

func legacyProfile(t *testing.T, driver profile.Driver, name, target string) profile.Profile {
	t.Helper()
	id, err := profile.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return profile.Profile{
		ID: id, Driver: driver, Name: name,
		Target: target, Host: "localhost", Port: "3306", User: "alice",
	}
}

func TestSelectConnection_uniqueLegacyProfileResolvesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pluginID, family := registerSelectFamily(t)
	path, err := profile.Path()
	if err != nil {
		t.Fatal(err)
	}
	legacy := legacyProfile(t, profile.Driver(family), "Legacy", "app")
	if err := profile.Save(path, []profile.Profile{legacy}); err != nil {
		t.Fatal(err)
	}

	loaded, _, _ := profile.Load(path)
	options := connectionOptions(loaded)
	if len(options) != 1 {
		t.Fatalf("connectionOptions() = %d options, want the uniquely served legacy record", len(options))
	}
	if options[0].Value.Plugin != pluginID {
		t.Fatalf("resolved option plugin = %q, want %q", options[0].Value.Plugin, pluginID)
	}

	if err := persistResolvedPlugin(path, loaded, options[0].Value); err != nil {
		t.Fatalf("persistResolvedPlugin: %v", err)
	}
	migrated, _, _ := profile.Load(path)
	if len(migrated) != 1 || migrated[0].Plugin != pluginID {
		t.Fatalf("persisted migration = %#v, want plugin %q on disk", migrated, pluginID)
	}
}
func TestSelectConnection_sharedTargetMigratesOnlySelectedRecord(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pluginID, family := registerSelectFamily(t)
	path, err := profile.Path()
	if err != nil {
		t.Fatal(err)
	}
	first := legacyProfile(t, profile.Driver(family), "First", "app")
	second := legacyProfile(t, profile.Driver(family), "Second", "app")
	if err := profile.Save(path, []profile.Profile{first, second}); err != nil {
		t.Fatal(err)
	}

	loaded, _, _ := profile.Load(path)
	options := connectionOptions(loaded)
	if len(options) != 2 {
		t.Fatalf("connectionOptions() = %d options, want both same-target records", len(options))
	}
	selected := options[1].Value
	if selected.ID != second.ID || selected.Target != "app" {
		t.Fatalf("selected = %+v, want the second record's identity and target", selected)
	}
	if err := persistResolvedPlugin(path, loaded, selected); err != nil {
		t.Fatalf("persistResolvedPlugin: %v", err)
	}
	migrated, _, _ := profile.Load(path)
	if len(migrated) != 2 {
		t.Fatalf("persisted profiles = %d, want both records kept", len(migrated))
	}
	byID := map[string]profile.Profile{}
	for _, p := range migrated {
		byID[p.ID] = p
	}
	if got := byID[first.ID].Plugin; got != "" {
		t.Fatalf("first record plugin = %q, want untouched blank", got)
	}
	if got := byID[second.ID].Plugin; got != pluginID {
		t.Fatalf("second record plugin = %q, want only the selection persisted as %q", got, pluginID)
	}
}

func TestSelectConnection_ambiguousLegacyProfileNeverOffered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	family := fmt.Sprintf("mysqlfam%d", time.Now().UnixNano())
	registerShimIn(t, family+"a", family)
	registerShimIn(t, family+"b", family)
	path, err := profile.Path()
	if err != nil {
		t.Fatal(err)
	}
	legacy := legacyProfile(t, profile.Driver(family), "Ambiguous", "app")
	if err := profile.Save(path, []profile.Profile{legacy}); err != nil {
		t.Fatal(err)
	}

	loaded, _, _ := profile.Load(path)
	if options := connectionOptions(loaded); len(options) != 0 {
		t.Fatalf("connectionOptions() = %+v, want no option for the ambiguous legacy record", options)
	}
	if _, err := selectConnection(io.Discard); !errors.Is(err, errNoConnections) {
		t.Fatalf("selectConnection() error = %v, want errNoConnections so nothing can open", err)
	}
	intact, _, _ := profile.Load(path)
	if len(intact) != 1 || intact[0].Plugin != "" {
		t.Fatalf("ambiguous record = %#v, want the legacy profile left intact", intact)
	}
}
