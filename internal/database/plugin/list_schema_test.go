package plugin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// listSchemaViaPlugin spawns the helper child with the given behavior and
// PERK_PLUGIN_SCHEMA override, opens a session, and returns the
// observable ListSchema result.
func listSchemaViaPlugin(t *testing.T, behavior, schema string) ([]sharedsql.SchemaObject, error) {
	t.Helper()
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	if behavior != "" {
		t.Setenv("PERK_PLUGIN_BEHAVIOR", behavior)
	}
	if schema != "" {
		t.Setenv("PERK_PLUGIN_SCHEMA", schema)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return service.ListSchema(context.Background())
}

// TestProxy_listSchemaNormalization pins the host-side normalization of
// perk/v1 list_schema responses: flat plugin objects get one synthesized
// database root per distinct non-empty Database, prepended in first-seen
// database order, while explicit roots, empty arrays, and null responses
// pass through untouched.
func TestProxy_listSchemaNormalization(t *testing.T) {
	for _, test := range []struct {
		name   string
		schema string
		want   []sharedsql.SchemaObject
	}{
		{
			name:   "flat single table",
			schema: `[{"database":"demo","type":"table","name":"kv"}]`,
			want: []sharedsql.SchemaObject{
				{Database: "demo", Type: "database", Name: "demo"},
				{Database: "demo", Type: "table", Name: "kv"},
			},
		},
		{
			name:   "multiple databases keep first-seen root order",
			schema: `[{"database":"alpha","type":"table","name":"a1"},{"database":"beta","type":"table","name":"b1"},{"database":"alpha","type":"view","name":"a2"}]`,
			want: []sharedsql.SchemaObject{
				{Database: "alpha", Type: "database", Name: "alpha"},
				{Database: "beta", Type: "database", Name: "beta"},
				{Database: "alpha", Type: "table", Name: "a1"},
				{Database: "beta", Type: "table", Name: "b1"},
				{Database: "alpha", Type: "view", Name: "a2"},
			},
		},
		{
			name:   "explicit root suppresses synthesis",
			schema: `[{"database":"db","type":"database","name":"db"},{"database":"db","type":"table","name":"t"},{"database":"db","type":"view","name":"v"}]`,
			want: []sharedsql.SchemaObject{
				{Database: "db", Type: "database", Name: "db"},
				{Database: "db", Type: "table", Name: "t"},
				{Database: "db", Type: "view", Name: "v"},
			},
		},
		{
			name:   "empty array stays empty",
			schema: `[]`,
			want:   []sharedsql.SchemaObject{},
		},
		{
			name:   "null stays nil",
			schema: `null`,
			want:   nil,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects, err := listSchemaViaPlugin(t, "", test.schema)
			if err != nil {
				t.Fatalf("ListSchema error = %v", err)
			}
			if !reflect.DeepEqual(objects, test.want) {
				t.Fatalf("ListSchema = %+v, want %+v", objects, test.want)
			}
		})
	}
}

// TestProxy_listSchemaError: an RPC error surfaces unchanged, and no
// roots are fabricated or partial data normalized on the error path.
func TestProxy_listSchemaError(t *testing.T) {
	objects, err := listSchemaViaPlugin(t, "schema_error", "")
	if err == nil {
		t.Fatal("ListSchema error = nil, want the schema_error RPC failure")
	}
	if objects != nil {
		t.Fatalf("ListSchema objects = %+v, want nil on RPC error", objects)
	}
}

// TestNormalizeSchema_flatEdgeCases covers the pure normalization edge
// cases directly: repeated children yield exactly one root, an explicit
// root suppresses synthesis regardless of its position, and objects with
// an empty Database pass through untouched.
func TestNormalizeSchema_flatEdgeCases(t *testing.T) {
	for _, test := range []struct {
		name string
		in   []sharedsql.SchemaObject
		want []sharedsql.SchemaObject
	}{
		{
			name: "repeated children produce one root",
			in: []sharedsql.SchemaObject{
				{Database: "db", Type: "table", Name: "t1"},
				{Database: "db", Type: "table", Name: "t2"},
				{Database: "db", Type: "collection", Name: "c1"},
			},
			want: []sharedsql.SchemaObject{
				{Database: "db", Type: "database", Name: "db"},
				{Database: "db", Type: "table", Name: "t1"},
				{Database: "db", Type: "table", Name: "t2"},
				{Database: "db", Type: "collection", Name: "c1"},
			},
		},
		{
			name: "explicit root after children suppresses synthesis",
			in: []sharedsql.SchemaObject{
				{Database: "db", Type: "table", Name: "t"},
				{Database: "db", Type: "database", Name: "db"},
			},
			want: []sharedsql.SchemaObject{
				{Database: "db", Type: "table", Name: "t"},
				{Database: "db", Type: "database", Name: "db"},
			},
		},
		{
			name: "empty database untouched",
			in: []sharedsql.SchemaObject{
				{Database: "", Type: "table", Name: "orphan"},
				{Database: "db", Type: "table", Name: "t"},
			},
			want: []sharedsql.SchemaObject{
				{Database: "db", Type: "database", Name: "db"},
				{Database: "", Type: "table", Name: "orphan"},
				{Database: "db", Type: "table", Name: "t"},
			},
		},
		{
			name: "empty database root is not an explicit root",
			in: []sharedsql.SchemaObject{
				{Database: "", Type: "database", Name: "weird"},
				{Database: "db", Type: "table", Name: "t"},
			},
			want: []sharedsql.SchemaObject{
				{Database: "db", Type: "database", Name: "db"},
				{Database: "", Type: "database", Name: "weird"},
				{Database: "db", Type: "table", Name: "t"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeSchema(test.in); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("normalizeSchema = %+v, want %+v", got, test.want)
			}
		})
	}
}
