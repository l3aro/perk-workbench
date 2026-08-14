package schema

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// scopeDiagramLayout is a workspace wide and tall enough for the test
// diagrams; the fallback tests narrow it.
var scopeDiagramLayout = uikit.Layout{ViewportWidth: 100, Height: 40}

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

// scopeDiagramMySQLCaches returns the office/analytics relationship data:
// orders references customers twice (customer_id non-unique, billing_id
// unique → the merged edge is N:1), passports references customers once
// (unique → 1:1), and analytics.events references office.customers from
// outside the office scope.
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

// scopeDiagramPostgresObjects is a mixed PostgreSQL sidebar fixture: two
// schemas under the connected database.
func scopeDiagramPostgresObjects() []sharedsql.SchemaObject {
	return []sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
		{Database: "main", Type: "table", Name: "public.orders"},
		{Database: "main", Type: "schema", Name: "archive"},
		{Database: "main", Type: "table", Name: "archive.audit"},
		{Database: "other", Type: "database", Name: "other"},
		{Database: "other", Type: "schema", Name: "public"},
		{Database: "other", Type: "table", Name: "public.leaked"},
	}
}

// scopeDiagramPostgresCaches returns the public/archive relationship data:
// orders references accounts (in scope for both scopes), accounts
// references archive.audit (outside the public schema scope, internal to
// the database scope).
func scopeDiagramPostgresCaches() (map[string][]sharedsql.ForeignKeyInfo, map[string][]sharedsql.IndexInfo) {
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{
		"public.orders": {
			{ID: "orders_account_id_fkey", Columns: []string{"account_id"}, ReferenceTable: "public.accounts", ReferenceColumns: []string{"id"}},
		},
		"public.accounts": {
			{ID: "accounts_audit_id_fkey", Columns: []string{"audit_id"}, ReferenceTable: "archive.audit", ReferenceColumns: []string{"id"}},
		},
	}
	indexes := map[string][]sharedsql.IndexInfo{
		"public.accounts": {{Name: "accounts_pkey", PrimaryKey: true, Columns: []string{"id"}}},
		"public.orders": {
			{Name: "orders_pkey", PrimaryKey: true, Columns: []string{"id"}},
			{Name: "orders_account_id_idx", Columns: []string{"account_id"}},
		},
		"archive.audit": {{Name: "audit_pkey", PrimaryKey: true, Columns: []string{"id"}}},
	}
	return foreignKeys, indexes
}

func scopeDiagramModel(objects []sharedsql.SchemaObject) Model {
	model := New()
	model.Objects = objects
	return model
}

func mysqlScopeSnapshot() Snapshot {
	foreignKeys, indexes := scopeDiagramMySQLCaches()
	return Snapshot{
		Database:        sharedsql.DatabaseInfo{Product: "MySQL"},
		WorkspaceTarget: core.WorkspaceTarget{Kind: core.WorkspaceDatabase, Database: "office"},
		ForeignKeysAll:  foreignKeys,
		IndexesAll:      indexes,
	}
}

// TestScopeDiagram_mysqlDatabaseScope_showsEveryInScopeCard drives the
// MySQL database scope: every in-scope table/view is a card, the internal
// edge renders with its cardinality labels, and outside tables and
// references are excluded.
func TestScopeDiagram_mysqlDatabaseScope_showsEveryInScopeCard(t *testing.T) {
	model := scopeDiagramModel(scopeDiagramMySQLObjects())
	view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, mysqlScopeSnapshot()))

	for _, present := range []string{"office.customers", "office.orders", "office.passports", "office.vip_customers"} {
		if !strings.Contains(view, present) {
			t.Fatalf("scope diagram view misses %q: %q", present, view)
		}
	}
	for _, absent := range []string{"analytics", "events", "daily_events"} {
		if strings.Contains(view, absent) {
			t.Fatalf("scope diagram view leaks %q: %q", absent, view)
		}
	}
	if got := strings.Count(view, "┌─ "); got != 4 {
		t.Fatalf("card count = %d, want 4 in %q", got, view)
	}
	// The orders card carries its FK mappings.
	if !strings.Contains(view, "customer_id → customers.id") || !strings.Contains(view, "billing_id → customers.id") {
		t.Fatalf("scope diagram view misses the orders FK pairs: %q", view)
	}
	// The internal edge renders with the known cardinality labels: the
	// merged orders→customers edge is N:1 (one of its two FKs is
	// non-unique), the passports→customers edge 1:1.
	for _, label := range []string{"(N)", "(1)", "▼"} {
		if !strings.Contains(view, label) {
			t.Fatalf("scope diagram view misses %q: %q", label, view)
		}
	}
	// Only edge-receiving parents connect: the edge-less vip_customers
	// card sits beside customers on the same lane but draws no stem,
	// arrow, or label — the merge has exactly one parent center.
	if got := strings.Count(view, "▼"); got != 1 {
		t.Fatalf("arrow count = %d, want 1 (customers only) in %q", got, view)
	}
	if got := strings.Count(view, "┴"); got != 1 {
		t.Fatalf("parent stem count = %d, want 1 (customers only) in %q", got, view)
	}
}

// TestScopeDiagram_externalEdgesDrawNoConnectors verifies that an outside
// reference is ignored rather than drawn as a stub: the office scope has
// no internal edges, so the diagram renders its cards without any
// connector, arrow, or cardinality rows.
func TestScopeDiagram_externalEdgesDrawNoConnectors(t *testing.T) {
	foreignKeys, indexes := scopeDiagramMySQLCaches()
	// The office tables declare nothing; only analytics.events references
	// office.customers, from outside the office scope.
	delete(foreignKeys, "office.orders")
	delete(foreignKeys, "office.passports")
	model := scopeDiagramModel(scopeDiagramMySQLObjects())
	snapshot := mysqlScopeSnapshot()
	snapshot.ForeignKeysAll = foreignKeys
	snapshot.IndexesAll = indexes
	view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))

	if got := strings.Count(view, "┌"); got != 4 {
		t.Fatalf("card count = %d, want 4 in %q", got, view)
	}
	for _, glyph := range []string{"┬", "┴", "▲", "▼", "(1)", "(N)"} {
		if strings.Contains(view, glyph) {
			t.Fatalf("outside reference drew %q: %q", glyph, view)
		}
	}
}

// TestScopeDiagram_postgresSchemaAndDatabaseScopes drives the PostgreSQL
// scopes: the schema scope shows only its own tables and keeps the edge
// to the archive table out; the database scope includes every table of
// the connected database and draws that edge.
func TestScopeDiagram_postgresSchemaAndDatabaseScopes(t *testing.T) {
	foreignKeys, indexes := scopeDiagramPostgresCaches()
	model := scopeDiagramModel(scopeDiagramPostgresObjects())

	t.Run("schema scope", func(t *testing.T) {
		snapshot := Snapshot{
			Database:        sharedsql.DatabaseInfo{Product: "PostgreSQL"},
			WorkspaceTarget: core.WorkspaceTarget{Kind: core.WorkspaceSchema, Database: "main", Schema: "public"},
			ForeignKeysAll:  foreignKeys,
			IndexesAll:      indexes,
		}
		view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))

		for _, present := range []string{"public.accounts", "public.orders"} {
			if !strings.Contains(view, present) {
				t.Fatalf("schema scope view misses %q: %q", present, view)
			}
		}
		if strings.Contains(view, "archive.audit") {
			t.Fatalf("schema scope view leaks the archive table: %q", view)
		}
		if got := strings.Count(view, "┌"); got != 2 {
			t.Fatalf("schema scope card count = %d, want 2 in %q", got, view)
		}
		// The internal orders→accounts edge renders with its label.
		if !strings.Contains(view, "(N)") || !strings.Contains(view, "▼") {
			t.Fatalf("schema scope view misses the internal edge labels: %q", view)
		}
	})

	t.Run("database scope", func(t *testing.T) {
		snapshot := Snapshot{
			Database:        sharedsql.DatabaseInfo{Product: "PostgreSQL"},
			WorkspaceTarget: core.WorkspaceTarget{Kind: core.WorkspaceDatabase, Database: "main"},
			ForeignKeysAll:  foreignKeys,
			IndexesAll:      indexes,
		}
		view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))

		for _, present := range []string{"public.accounts", "public.orders", "archive.audit"} {
			if !strings.Contains(view, present) {
				t.Fatalf("database scope view misses %q: %q", present, view)
			}
		}
		if strings.Contains(view, "public.leaked") {
			t.Fatalf("database scope leaks a different database card: %q", view)
		}
		if got := strings.Count(view, "┌"); got != 3 {
			t.Fatalf("database scope card count = %d, want 3 in %q", got, view)
		}
		// The accounts→audit edge is internal at database scope.
		if !strings.Contains(view, "▼") {
			t.Fatalf("database scope view misses the archive edge: %q", view)
		}
	})
}

// TestScopeDiagram_cardinalityLabelsVerbatim pins the rendered labels: a
// unique FK edge is 1:1, a non-unique one N:1, and a loaded-but-empty
// index cache omits the label rows instead of guessing.
func TestScopeDiagram_cardinalityLabelsVerbatim(t *testing.T) {
	foreignKeys, indexes := scopeDiagramMySQLCaches()
	// passports declares a unique FK to customers: its edge is 1:1.
	model := scopeDiagramModel(scopeDiagramMySQLObjects())
	snapshot := mysqlScopeSnapshot()
	snapshot.ForeignKeysAll = foreignKeys
	snapshot.IndexesAll = indexes

	view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))
	if strings.Count(view, "(1)") < 1 || !strings.Contains(view, "(N)") {
		t.Fatalf("cardinality labels = %q, want both (1) and (N)", view)
	}

	// Without the index cache the labels must not be guessed: an empty
	// loaded map answers nothing and the label rows disappear.
	snapshot.IndexesAll = map[string][]sharedsql.IndexInfo{}
	view = ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))
	if strings.Contains(view, "(1)") || strings.Contains(view, "(N)") {
		t.Fatalf("empty index cache still rendered labels: %q", view)
	}
	if !strings.Contains(view, "┌") {
		t.Fatalf("empty index cache dropped the cards: %q", view)
	}
}

// TestScopeDiagram_absentCachesShowLoading verifies the loading and empty
// states: absent caches never produce guessed edges, and a scope without
// tables shows the empty state instead of a blank pane.
func TestScopeDiagram_absentCachesShowLoading(t *testing.T) {
	model := scopeDiagramModel(scopeDiagramMySQLObjects())
	snapshot := mysqlScopeSnapshot()
	snapshot.ForeignKeysAll = nil
	snapshot.IndexesAll = nil

	view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))
	if !strings.Contains(view, "loading schema") {
		t.Fatalf("absent caches view = %q, want the loading state", view)
	}
	if strings.Contains(view, "┌") {
		t.Fatalf("absent caches view rendered cards: %q", view)
	}

	// One loaded cache is still loading: both are needed.
	snapshot.ForeignKeysAll, _ = scopeDiagramMySQLCaches()
	view = ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))
	if !strings.Contains(view, "loading schema") {
		t.Fatalf("half-loaded caches view = %q, want the loading state", view)
	}
}

// TestScopeDiagram_emptyScopeAndUnsupportedTargets verifies the empty
// state: an object-less scope and targets without a scope diagram (table
// targets, MongoDB scopes) render the empty state.
func TestScopeDiagram_emptyScopeAndUnsupportedTargets(t *testing.T) {
	foreignKeys, indexes := scopeDiagramMySQLCaches()
	model := scopeDiagramModel(scopeDiagramPostgresObjects())

	// The staging schema holds no tables in the fixture.
	snapshot := Snapshot{
		Database:        sharedsql.DatabaseInfo{Product: "PostgreSQL"},
		WorkspaceTarget: core.WorkspaceTarget{Kind: core.WorkspaceSchema, Database: "main", Schema: "staging"},
		ForeignKeysAll:  foreignKeys,
		IndexesAll:      indexes,
	}
	view := ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, snapshot))
	if !strings.Contains(view, "no tables in scope") {
		t.Fatalf("empty scope view = %q, want the empty state", view)
	}

	// A table target has no scope diagram.
	tableTarget := Snapshot{
		Database:        sharedsql.DatabaseInfo{Product: "MySQL"},
		WorkspaceTarget: core.WorkspaceTarget{Kind: core.WorkspaceTable, Table: "office.customers"},
		ForeignKeysAll:  foreignKeys,
		IndexesAll:      indexes,
	}
	view = ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, tableTarget))
	if !strings.Contains(view, "no tables in scope") {
		t.Fatalf("table target view = %q, want the empty state", view)
	}

	// MongoDB has no relational scope diagram.
	mongoTarget := Snapshot{
		Database:        sharedsql.DatabaseInfo{Product: "MongoDB"},
		WorkspaceTarget: core.WorkspaceTarget{Kind: core.WorkspaceDatabase, Database: "mydb"},
		ForeignKeysAll:  foreignKeys,
		IndexesAll:      indexes,
	}
	view = ansiStrip(model.ScopeDiagramView(scopeDiagramLayout, mongoTarget))
	if !strings.Contains(view, "no tables in scope") {
		t.Fatalf("MongoDB target view = %q, want the empty state", view)
	}
}

// TestScopeDiagram_fallsBackToListWhenTooWide verifies the fit fallback:
// a diagram wider than the workspace renders as a flat relationship list
// with the full-screen affordance, like the focus diagrams.
func TestScopeDiagram_fallsBackToListWhenTooWide(t *testing.T) {
	model := scopeDiagramModel(scopeDiagramMySQLObjects())
	layout := uikit.Layout{ViewportWidth: 40, Height: 24}
	view := ansiStrip(model.ScopeDiagramView(layout, mysqlScopeSnapshot()))

	if !strings.Contains(view, "relationships") {
		t.Fatalf("fallback view = %q, want the relationship list", view)
	}
	if !strings.Contains(view, "office.orders → office.custom") {
		t.Fatalf("fallback view = %q, want the truncated internal edge line", view)
	}
	if !strings.Contains(view, "Press f for full-screen diagram") {
		t.Fatalf("fallback view = %q, want the full-screen affordance", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lineWidth := ansiWidth(line); lineWidth > layout.ViewportWidth {
			t.Fatalf("fallback line width = %d, want <= %d: %q", lineWidth, layout.ViewportWidth, line)
		}
	}
}

func ansiStrip(value string) string {
	return strings.TrimRight(ansi.Strip(value), " ")
}

func ansiWidth(value string) int {
	return ansi.StringWidth(value)
}
