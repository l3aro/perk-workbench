package plugin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestProxy_writeResultsCarryStatementMetadata proves the row/document
// write shims map the optional wire statement_metadata onto
// Result.StatementMetadata, so the workbench can log the plugin's native
// statement with its metadata.
func TestProxy_writeResultsCarryStatementMetadata(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_ROW_WRITER", "1")
	t.Setenv("PERK_PLUGIN_DOCUMENT", "1")
	const native = "RENAME key user:2 user:3"
	t.Setenv("PERK_PLUGIN_WRITE_STATEMENT", native)
	t.Setenv("PERK_PLUGIN_WRITE_METADATA", `{"language":"redis","replayable":false,"sensitive":false}`)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"),
		[]string{executable}, func(s database.Shim) error {
			shim = s
			return nil
		})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rowWriter, ok := service.(sharedsql.RowWriter)
	if !ok {
		t.Fatal("service is not a RowWriter")
	}
	documentWriter, ok := service.(sharedsql.DocumentWriter)
	if !ok {
		t.Fatal("service is not a DocumentWriter")
	}
	values := []sharedsql.RowValue{{Name: "key", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "user:2"}}}
	document := sharedsql.DocumentPayload{Format: sharedsql.DocumentFormatMongoExtendedJSON, Data: []byte(`{"name":"x"}`)}
	want := &sharedsql.StatementMetadata{Language: "redis", Replayable: false, Sensitive: false}
	for _, call := range []struct {
		name string
		run  func() (sharedsql.Result, error)
	}{
		{name: "insert row", run: func() (sharedsql.Result, error) { return rowWriter.InsertRow(context.Background(), "keys", values) }},
		{name: "insert document", run: func() (sharedsql.Result, error) {
			return documentWriter.InsertDocument(context.Background(), "keys", document)
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			result, err := call.run()
			if err != nil {
				t.Fatalf("%s: %v", call.name, err)
			}
			if result.Statement != native {
				t.Fatalf("%s Statement = %q, want the plugin's native statement %q", call.name, result.Statement, native)
			}
			if !reflect.DeepEqual(result.StatementMetadata, want) {
				t.Fatalf("%s StatementMetadata = %#v, want %#v", call.name, result.StatementMetadata, want)
			}
		})
	}
}

// TestProxy_rejectsOrphanStatementMetadata proves metadata without a
// nonblank statement is a result-shape violation at the plugin boundary:
// an operation error, never terminal, for execute results and write
// responses alike.
func TestProxy_rejectsOrphanStatementMetadata(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_ROW_WRITER", "1")
	t.Setenv("PERK_PLUGIN_DOCUMENT", "1")
	t.Setenv("PERK_PLUGIN_EXECUTE_METADATA", `{"language":"redis"}`)
	t.Setenv("PERK_PLUGIN_WRITE_METADATA", `{"language":"redis"}`)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"),
		[]string{executable}, func(s database.Shim) error {
			shim = s
			return nil
		})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := service.Execute(context.Background(), "GET x"); err == nil || !strings.Contains(err.Error(), "statement_metadata requires a nonblank statement") {
		t.Fatalf("execute orphan metadata error = %v, want a shape violation", err)
	}
	rowWriter, ok := service.(sharedsql.RowWriter)
	if !ok {
		t.Fatal("service is not a RowWriter")
	}
	if _, err := rowWriter.InsertRow(context.Background(), "keys",
		[]sharedsql.RowValue{{Name: "key", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "x"}}}); err == nil || !strings.Contains(err.Error(), "statement_metadata requires a nonblank statement") {
		t.Fatalf("write orphan metadata error = %v, want a shape violation", err)
	}
	// The session still works afterwards: the violation is an operation
	// error, never terminal.
	if _, err := service.ListSchema(context.Background()); err != nil {
		t.Fatalf("list schema after orphan rejection: %v", err)
	}
}
