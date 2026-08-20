package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestConnectionOptions(t *testing.T) {
	profiles := []profile.Profile{
		{Driver: profile.DriverSQLite, Name: "Local", Target: "/tmp/a.db"},
		{Driver: profile.DriverMySQL, Name: "Remote", Target: "mysql:alice@tcp(db.example.test:3306)/app", Host: "db.example.test", Port: "3306", User: "alice", ReadOnly: true},
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
	if options[0].Value != "/tmp/a.db" {
		t.Fatalf("option[0] value = %q, want /tmp/a.db", options[0].Value)
	}
	if got := options[1].Key; !strings.Contains(got, "Remote") || !strings.Contains(got, "mysql") || !strings.Contains(got, "[READONLY]") {
		t.Fatalf("option[1] key = %q, want Remote/mysql/[READONLY] before sidecar load", got)
	}
	if options[1].Value != "mysql:alice@tcp(db.example.test:3306)/app" {
		t.Fatalf("option[1] value = %q, want the mysql DSN", options[1].Value)
	}
}

func TestSelectConnection_noSavedConnections(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	selected, err := selectConnection(io.Discard)
	if !errors.Is(err, errNoConnections) {
		t.Fatalf("selectConnection() error = %v, want errNoConnections", err)
	}
	if selected != "" {
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
	if selected != "" {
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
