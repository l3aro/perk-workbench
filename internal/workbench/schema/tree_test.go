package schema

import (
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestRebuildTree_synthesizedRootsRender pins the external-plugin
// contract end to end: the plugin boundary synthesizes a database root
// for flat perk/v1 responses, and RebuildTree must render that root with
// the flat objects beneath it. Without the root, the table row would be
// dropped by the tree builder (roots come only from Type == "database").
func TestRebuildTree_synthesizedRootsRender(t *testing.T) {
	model := New()
	// The exact post-normalization ListSchema shape for a flat plugin
	// response: one synthesized root, then the plugin-provided table.
	model.SetObjects([]sharedsql.SchemaObject{
		{Database: "demo", Type: "database", Name: "demo"},
		{Database: "demo", Type: "table", Name: "kv"},
	}, Snapshot{Database: sharedsql.DatabaseInfo{Product: "PluginKV"}})

	items := model.List.Items()
	if len(items) != 2 {
		t.Fatalf("tree items = %d, want the root and the table", len(items))
	}
	root, ok := items[0].(Item)
	if !ok || !root.Root || root.Name != "demo" || root.Database != "demo" {
		t.Fatalf("first item = %+v, want the synthesized demo root", items[0])
	}
	table, ok := items[1].(Item)
	if !ok || table.Root || table.Kind != "table" || table.Name != "kv" || table.Database != "demo" {
		t.Fatalf("second item = %+v, want the kv table under demo", items[1])
	}
}
