package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// scopeDiagramMySQLObjects is a mixed MySQL sidebar fixture: the office
// database with three tables and a view, plus an analytics database whose
// events table references office.customers (an outside reference for the
// office scope).
func scopeDiagramMySQLObjects() []sharedsql.SchemaObject {
	return []sharedsql.SchemaObject{
		{Database: "office", Type: "database", Name: "office"},
		{Database: "office", Type: "table", Name: "customers"},
		{Database: "office", Type: "table", Name: "orders"},
		{Database: "office", Type: "table", Name: "passports"},
		{Database: "office", Type: "view", Name: "vip_customers"},
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
	}
}

// scopeDiagramMySQLCaches returns the office relationship data: orders
// references customers twice (customer_id non-unique, billing_id unique →
// the merged edge is N:1), passports references customers once (unique →
// 1:1), and analytics.events references office.customers from outside the
// office scope.
func scopeDiagramMySQLCaches() (map[string][]sharedsql.ForeignKeyInfo, map[string][]sharedsql.IndexInfo) {
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"office.orders": {
			{ID: "orders_customer_id_fkey", Columns: []string{"customer_id"}, ReferenceTable: "office.customers", ReferenceColumns: []string{"id"}},
			{ID: "orders_billing_id_fkey", Columns: []string{"billing_id"}, ReferenceTable: "office.customers", ReferenceColumns: []string{"id"}},
		},
		"office.passports": {
			{ID: "passports_customer_id_fkey", Columns: []string{"customer_id"}, ReferenceTable: "office.customers", ReferenceColumns: []string{"id"}},
		},
		"analytics.events": {
			{ID: "events_customer_id_fkey", Columns: []string{"customer_id"}, ReferenceTable: "office.customers", ReferenceColumns: []string{"id"}},
		},
	}
	indexes := map[string][]sharedsql.IndexInfo{
		"office.customers": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
		"office.orders": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "orders_customer_id_idx", Columns: []string{"customer_id"}},
			{Name: "orders_billing_id_key", Unique: true, Columns: []string{"billing_id"}},
		},
		"office.passports": {
			{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "passports_customer_id_key", Unique: true, Columns: []string{"customer_id"}},
		},
		"analytics.events": {{Name: "PRIMARY", PrimaryKey: true, Columns: []string{"id"}}},
	}
	return foreignKeys, indexes
}

// scopeDiagramModelWithOffice builds a MySQL model on the office database
// scope with the relationship caches loaded.
func scopeDiagramModelWithOffice(t *testing.T) Model {
	t.Helper()
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(scopeDiagramMySQLObjects())
	model.selectDatabaseTarget("office")
	model.schema.foreignKeysAll, model.schema.indexesAll = scopeDiagramMySQLCaches()
	return resizeModel(model, 120, 40)
}

// TestScopeDiagram_tabRendersScopeCards drives the Diagram tab on a MySQL
// database scope: every in-scope table/view renders as a card, outside
// tables and references stay out, and the internal edge keeps its
// cardinality labels.
func TestScopeDiagram_tabRendersScopeCards(t *testing.T) {
	model := scopeDiagramModelWithOffice(t)
	model.Tab = tabDiagram

	view := ansi.Strip(model.workspaceView())
	for _, present := range []string{"office.customers", "office.orders", "office.passports", "office.vip_customers"} {
		if !strings.Contains(view, present) {
			t.Fatalf("diagram tab view misses %q: %q", present, view)
		}
	}
	for _, absent := range []string{"analytics", "events", "daily_events"} {
		if strings.Contains(view, absent) {
			t.Fatalf("diagram tab view leaks %q: %q", absent, view)
		}
	}
	if got := strings.Count(view, "┌"); got != 4 {
		t.Fatalf("card count = %d, want 4 in %q", got, view)
	}
	for _, label := range []string{"(N)", "(1)", "▲"} {
		if !strings.Contains(view, label) {
			t.Fatalf("diagram tab view misses %q: %q", label, view)
		}
	}
}

// TestScopeDiagram_postgresDatabaseScopeRendersAllTables drives the
// Diagram tab on a PostgreSQL database scope: every table of the
// connected database is a card, including the table the public schema
// references in archive.
func TestScopeDiagram_postgresDatabaseScopeRendersAllTables(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(postgresScopeObjects())
	model.selectDatabaseTarget("main")
	model.schema.foreignKeysAll = map[string][]sharedsql.ForeignKeyInfo{
		"public.orders":   {{ID: "orders_account_id_fkey", Columns: []string{"account_id"}, ReferenceTable: "public.accounts", ReferenceColumns: []string{"id"}}},
		"public.accounts": {{ID: "accounts_audit_id_fkey", Columns: []string{"audit_id"}, ReferenceTable: "archive.audit", ReferenceColumns: []string{"id"}}},
	}
	model.schema.indexesAll = map[string][]sharedsql.IndexInfo{
		"public.accounts": {{Name: "accounts_pkey", PrimaryKey: true, Columns: []string{"id"}}},
		"public.orders":   {{Name: "orders_pkey", PrimaryKey: true, Columns: []string{"id"}}},
		"archive.audit":   {{Name: "audit_pkey", PrimaryKey: true, Columns: []string{"id"}}},
	}
	model.Tab = tabDiagram
	model = resizeModel(model, 120, 40)

	view := ansi.Strip(model.workspaceView())
	for _, present := range []string{"public.accounts", "public.orders", "archive.audit"} {
		if !strings.Contains(view, present) {
			t.Fatalf("database scope diagram view misses %q: %q", present, view)
		}
	}
	if got := strings.Count(view, "┌"); got != 3 {
		t.Fatalf("database scope card count = %d, want 3 in %q", got, view)
	}
	if !strings.Contains(view, "▲") {
		t.Fatalf("database scope diagram view misses the internal edges: %q", view)
	}
}

// TestScopeDiagram_absentCachesShowLoading verifies the app-level loading
// state: with the connection-level caches still absent, the Diagram tab
// shows the loading state instead of guessing edges.
func TestScopeDiagram_absentCachesShowLoading(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(scopeDiagramMySQLObjects())
	model.selectDatabaseTarget("office")
	model.Tab = tabDiagram
	model = resizeModel(model, 120, 40)

	view := ansi.Strip(model.workspaceView())
	if !strings.Contains(view, "loading schema") {
		t.Fatalf("diagram tab with absent caches = %q, want the loading state", view)
	}
	if strings.Contains(view, "┌") {
		t.Fatalf("diagram tab with absent caches rendered cards: %q", view)
	}
}

// TestScopeDiagram_tabSwitchLoadsCaches verifies the deferred cache load:
// switching to the Diagram tab with absent caches starts the
// connection-level loads, and once they land the scope renders its cards.
func TestScopeDiagram_tabSwitchLoadsCaches(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(scopeDiagramMySQLObjects())
	model.selectDatabaseTarget("office")
	// A single-lane diagram of the four office cards is 86 cells wide.
	model = resizeModel(model, 160, 40)
	if model.Tab != tabBrowse {
		t.Fatalf("setup: Tab = %v, want Browse", model.Tab)
	}

	// L cycles Browse → Diagram and returns the cache-load command.
	updated, command := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	if model.Tab != tabDiagram {
		t.Fatalf("Tab = %v, want Diagram after L", model.Tab)
	}
	if command == nil {
		t.Fatal("switching to the Diagram tab returned no cache-load command")
	}
	model = runTableCommand(model, command)

	// The stub's empty maps are loaded: the state is no longer loading,
	// and the scope's tables render as cards without any edges.
	if model.schema.foreignKeysAll == nil || model.schema.indexesAll == nil {
		t.Fatal("cache loads did not land")
	}
	view := ansi.Strip(model.workspaceView())
	if strings.Contains(view, "loading schema") {
		t.Fatalf("loaded diagram tab still shows the loading state: %q", view)
	}
	if got := strings.Count(view, "┌"); got != 4 {
		t.Fatalf("card count = %d, want 4 in %q", got, view)
	}
}

// TestScopeDiagram_clickSelectsDiagramAndLoads drives a tab-row click on
// the Diagram label: it selects the tab and starts the cache loads like
// the L key.
func TestScopeDiagram_clickSelectsDiagramAndLoads(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(scopeDiagramMySQLObjects())
	model.selectDatabaseTarget("office")
	model = resizeModel(model, 120, 40)

	tabs := model.workspaceTabs()
	_, widths := workspaceTabMeta(tabs)
	cx := 2 // pane left border (1) + left padding (1)
	for i, tab := range tabs {
		if tab == tabDiagram {
			break
		}
		cx += widths[i]
	}
	updated, command := model.Update(tea.MouseClickMsg{X: model.layout.schemaWidth + cx, Y: 2, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Tab != tabDiagram {
		t.Fatalf("Tab = %v, want Diagram after the tab-row click", model.Tab)
	}
	if command == nil {
		t.Fatal("tab-row click on Diagram returned no cache-load command")
	}
	model = runTableCommand(model, command)
	if model.schema.foreignKeysAll == nil || model.schema.indexesAll == nil {
		t.Fatal("tab-row click did not load the caches")
	}
}

// TestScopeDiagram_loadPendingDiagram_guards pins the load trigger: it
// fires only on the Diagram tab with absent caches, and a loaded cache
// stays loaded.
func TestScopeDiagram_loadPendingDiagram_guards(t *testing.T) {
	model := serverProductModel(t, "MySQL", &createDatabaseStub{})
	_ = model.setSchemaObjects(scopeDiagramMySQLObjects())
	model.selectDatabaseTarget("office")
	model.Tab = tabDiagram

	if cmd := model.loadPendingDiagram(); cmd == nil {
		t.Fatal("loadPendingDiagram with absent caches returned no command")
	}
	model.schema.foreignKeysAll, model.schema.indexesAll = scopeDiagramMySQLCaches()
	if cmd := model.loadPendingDiagram(); cmd != nil {
		t.Fatal("loadPendingDiagram with loaded caches returned a command")
	}
	model.Tab = tabBrowse
	if cmd := model.loadPendingDiagram(); cmd != nil {
		t.Fatal("loadPendingDiagram off the Diagram tab returned a command")
	}
	// The scope target decides the tab row: a table target has no Diagram
	// tab, so switching to it must not leave a stale diagram behind.
	model.Tab = tabDiagram
	model.WorkspaceTarget = core.WorkspaceTarget{Kind: core.WorkspaceTable, Table: "office.customers"}
	if cmd := model.loadPendingDiagram(); cmd != nil {
		t.Fatal("loadPendingDiagram on a table target returned a command")
	}
}
