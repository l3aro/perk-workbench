package schema

import (
	"testing"

	"charm.land/bubbles/v2/list"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// selectRootModel builds a schema model with one database root selected,
// ready for SchemaSelect; the snapshot carries the given product and the
// host-owned database-scope capability.
func selectRootModel(product string, capable bool) (Model, Snapshot) {
	model := New()
	if err := model.List.SetItems([]list.Item{
		Item{Name: "demo", Database: "demo", Root: true},
	}); err != nil {
		panic(err)
	}
	model.List.Select(0)
	return model, Snapshot{
		Database:             sharedsql.DatabaseInfo{Product: product},
		DatabaseScopeCapable: capable,
		WorkspaceTarget:      core.WorkspaceTarget{Kind: core.WorkspaceNone},
	}
}

// TestSchemaSelect_databaseRootCapability pins the scope-selection gate:
// an unknown/non-built-in product becomes a database target only when the
// snapshot carries the driver's explicit workspace capability, SQLite
// stays SQL-only even then, and the legacy products are untouched.
func TestSchemaSelect_databaseRootCapability(t *testing.T) {
	tests := []struct {
		name      string
		product   string
		target    string
		capable   bool
		wantEvent string // "", "DatabaseSelected", or "ReconnectRequested"
	}{
		{
			name:      "unknown product with capability selects the root",
			product:   "PluginKV",
			capable:   true,
			wantEvent: "DatabaseSelected",
		},
		{
			name:    "unknown product without capability only toggles",
			product: "PluginKV",
			capable: false,
		},
		{
			name:    "SQLite stays SQL-only even with capability",
			product: "SQLite",
			capable: true,
		},
		{
			name:    "SQLite without capability stays SQL-only",
			product: "SQLite",
			capable: false,
		},
		{
			name:      "MySQL selects regardless of capability",
			product:   "MySQL",
			capable:   false,
			wantEvent: "DatabaseSelected",
		},
		{
			name:      "MongoDB selects regardless of capability",
			product:   "MongoDB",
			capable:   false,
			wantEvent: "DatabaseSelected",
		},
		{
			name:      "disconnected PostgreSQL root reconnects, never selects",
			product:   "PostgreSQL",
			target:    "postgres://host/main",
			capable:   true,
			wantEvent: "ReconnectRequested",
		},
		{
			name:      "connected PostgreSQL root selects",
			product:   "PostgreSQL",
			target:    "postgres://host/demo",
			capable:   true,
			wantEvent: "DatabaseSelected",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, snapshot := selectRootModel(test.product, test.capable)
			snapshot.Target = test.target
			model, event, _ := model.SchemaSelect(snapshot)
			switch test.wantEvent {
			case "":
				if event != nil {
					t.Fatalf("root selection emitted %v, want no scope event", event)
				}
			case "DatabaseSelected":
				selected, ok := event.(DatabaseSelected)
				if !ok {
					t.Fatalf("root selection emitted %v, want DatabaseSelected", event)
				}
				if selected.Database != "demo" {
					t.Fatalf("DatabaseSelected = %+v, want the demo database", selected)
				}
			case "ReconnectRequested":
				requested, ok := event.(ReconnectRequested)
				if !ok {
					t.Fatalf("root selection emitted %v, want ReconnectRequested", event)
				}
				if requested.Database != "demo" {
					t.Fatalf("ReconnectRequested = %+v, want the demo database", requested)
				}
			default:
				t.Fatalf("unexpected wantEvent %q", test.wantEvent)
			}
		})
	}
}

// TestSchemaSelect_SQLiteRootTogglesOnly verifies a SQLite root with the
// capability flag set still never emits DatabaseSelected: the tree toggle
// runs (collapse/expand with its accordion tick) but the workspace stays
// SQL-only. The snapshot flag must not sway legacy built-in products.
func TestSchemaSelect_SQLiteRootTogglesOnly(t *testing.T) {
	model, snapshot := selectRootModel("SQLite", true)
	model.Objects = []sharedsql.SchemaObject{
		{Database: "demo", Type: "database", Name: "demo"},
		{Database: "demo", Type: "table", Name: "kv"},
	}
	next, event, cmd := model.SchemaSelect(snapshot)
	if event != nil {
		t.Fatalf("SQLite root emitted %v, want no scope event", event)
	}
	if cmd == nil {
		t.Fatal("SQLite root emitted no tree toggle command")
	}
	if !next.ExpandedDatabases["demo"] {
		t.Fatal("SQLite root did not toggle the database expansion")
	}
}
