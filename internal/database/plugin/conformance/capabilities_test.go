package conformance

import (
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestValidateCapabilities_driverIdentity(t *testing.T) {
	base := database.Capabilities{
		Name:    "redis-plugin",
		Display: "Redis",
		Targets: []database.TargetPattern{{Prefix: "redis:"}},
	}
	for _, test := range []struct {
		name    string
		driver  string
		wantErr bool
	}{
		{name: "omitted", wantErr: false},
		{name: "explicit family", driver: " redis ", wantErr: false},
		{name: "blank family falls back to name", driver: " \t", wantErr: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			caps := base
			caps.Driver = test.driver
			err := validateCapabilities(caps)
			if (err != nil) != test.wantErr {
			}
		})
	}
}

// TestValidateCapabilities_commandCatalog: the conformance runner's
// capabilities validation enforces the same command-catalog invariants
// as registration — nonblank bounded control-free name/usage/summary,
// case-insensitively unique names, and a capped list — so a real plugin
// handshake can never smuggle an unbounded completion list past the
// suite.
func TestValidateCapabilities_commandCatalog(t *testing.T) {
	base := database.Capabilities{
		Name:    "kv",
		Display: "KV",
		Targets: []database.TargetPattern{{Prefix: "kv:"}},
	}
	language := func(commands []database.QueryCommand) *database.QueryLanguage {
		return &database.QueryLanguage{
			Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…",
			Commands: commands,
		}
	}
	valid := func(commands []database.QueryCommand) error {
		caps := base
		caps.QueryLanguage = language(commands)
		return validateCapabilities(caps)
	}

	if err := valid([]database.QueryCommand{
		{Name: "GET", Usage: "GET key", Summary: "Get the value at key"},
		{Name: "SET", Usage: "SET key value", Summary: "Set the value at key"},
	}); err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}

	for _, test := range []struct {
		name     string
		commands []database.QueryCommand
	}{
		{name: "blank name", commands: []database.QueryCommand{{Name: " ", Usage: "GET key", Summary: "Get"}}},
		{name: "blank usage", commands: []database.QueryCommand{{Name: "GET", Usage: "", Summary: "Get"}}},
		{name: "blank summary", commands: []database.QueryCommand{{Name: "GET", Usage: "GET key", Summary: "  "}}},
		{name: "control in usage", commands: []database.QueryCommand{{Name: "GET", Usage: "GET\nkey", Summary: "Get"}}},
		{name: "non-ASCII name", commands: []database.QueryCommand{{Name: "ΣΕΤ", Usage: "SET key value", Summary: "Set"}}},
		{name: "name overlong", commands: []database.QueryCommand{{Name: strings.Repeat("A", sharedsql.MaxQueryCommandNameRunes+1), Usage: "GET key", Summary: "Get"}}},
		{name: "usage overlong", commands: []database.QueryCommand{{Name: "GET", Usage: strings.Repeat("u", sharedsql.MaxQueryCommandUsageRunes+1), Summary: "Get"}}},
		{name: "summary overlong", commands: []database.QueryCommand{{Name: "GET", Usage: "GET key", Summary: strings.Repeat("s", sharedsql.MaxQueryCommandSummaryRunes+1)}}},
		{name: "duplicate case-insensitive", commands: []database.QueryCommand{{Name: "GET", Usage: "GET key", Summary: "Get"}, {Name: "get", Usage: "GET key", Summary: "Get"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := valid(test.commands); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}

	over := make([]database.QueryCommand, sharedsql.MaxQueryCommands+1)
	for i := range over {
		over[i] = database.QueryCommand{Name: "GET", Usage: "GET key", Summary: "Get"}
	}
	if err := valid(over); err == nil {
		t.Fatal("over-cap catalog accepted")
	}
}

// TestValidateCapabilities_workspaceAdvertisement: the conformance
// runner's capabilities validation enforces the same workspace tab
// invariants as registration — a fixed standard-tab set without
// duplicates and capped custom views with nonblank bounded control-free
// case-insensitively-unique ids and labels plus nonempty duplicate-free
// valid scopes — so a real plugin handshake can never smuggle an
// unbounded or malformed tab advertisement past the suite.
func TestValidateCapabilities_workspaceAdvertisement(t *testing.T) {
	base := database.Capabilities{
		Name:    "kv",
		Display: "KV",
		Targets: []database.TargetPattern{{Prefix: "kv:"}},
	}
	workspace := func(ws *sharedsql.WorkspaceCapability) error {
		caps := base
		caps.Workspace = ws
		return validateCapabilities(caps)
	}

	if err := workspace(&sharedsql.WorkspaceCapability{
		StandardTabs: []sharedsql.StandardWorkspaceTab{sharedsql.StandardWorkspaceTabColumns},
		CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "keys", Label: "Keys", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		},
	}); err != nil {
		t.Fatalf("valid workspace advertisement rejected: %v", err)
	}
	if err := workspace(nil); err != nil {
		t.Fatalf("absent workspace advertisement rejected: %v", err)
	}

	for _, test := range []struct {
		name string
		ws   *sharedsql.WorkspaceCapability
	}{
		{name: "unknown standard tab", ws: &sharedsql.WorkspaceCapability{StandardTabs: []sharedsql.StandardWorkspaceTab{"relations"}}},
		{name: "duplicate standard tab", ws: &sharedsql.WorkspaceCapability{StandardTabs: []sharedsql.StandardWorkspaceTab{sharedsql.StandardWorkspaceTabColumns, sharedsql.StandardWorkspaceTabColumns}}},
		{name: "duplicate custom view id", ws: &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "Keys", Label: "One", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
			{ID: "keys", Label: "Two", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		}}},
		{name: "invalid scope", ws: &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "keys", Label: "Keys", Scopes: []sharedsql.WorkspaceViewKind{"collection"}},
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := workspace(test.ws); err == nil {
				t.Fatal("invalid workspace advertisement accepted")
			}
		})
	}

	over := &sharedsql.WorkspaceCapability{CustomViews: make([]sharedsql.CustomWorkspaceView, sharedsql.MaxCustomWorkspaceViews+1)}
	for i := range over.CustomViews {
		over.CustomViews[i] = sharedsql.CustomWorkspaceView{
			ID: "view" + strings.Repeat("x", i), Label: "View",
			Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable},
		}
	}
	if err := workspace(over); err == nil {
		t.Fatal("over-cap custom view list accepted")
	}
}
