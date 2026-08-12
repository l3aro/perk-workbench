package sqlite

import (
	"context"
	"strings"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func newRowWriteService(t *testing.T) *Service {
	t.Helper()
	service := newMemoryService(t)
	if _, err := service.db.ExecContext(context.Background(), "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, note TEXT)"); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	return service
}

func selectItem(t *testing.T, service *Service, id string) (name, note *string) {
	t.Helper()
	result, err := service.Execute(context.Background(), "SELECT name, note FROM items WHERE id = "+id)
	if err != nil {
		t.Fatalf("selecting row: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	return result.Rows[0][0], result.Rows[0][1]
}

func TestRowWriter_insertAllDefaultsUsesEngineRow(t *testing.T) {
	service := newRowWriteService(t)

	result, err := service.InsertRow(context.Background(), "items", nil)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", result.RowsAffected)
	}
	name, _ := selectItem(t, service, "1")
	if name != nil {
		t.Fatalf("name = %q, want NULL", *name)
	}
}

func TestRowWriter_insertBindsValues(t *testing.T) {
	service := newRowWriteService(t)

	values := []sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: ""}},
		{Name: "note", Value: sharedsql.Value{Kind: sharedsql.ValueNull}},
	}
	result, err := service.InsertRow(context.Background(), "items", values)
	if err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", result.RowsAffected)
	}
	name, note := selectItem(t, service, "1")
	if name == nil || *name != "" {
		t.Fatalf("name = %v, want empty string", name)
	}
	if note != nil {
		t.Fatalf("note = %q, want NULL", *note)
	}

	// A quote-containing string must bind, not break the statement.
	values = []sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "O'Brien"}},
	}
	if _, err := service.InsertRow(context.Background(), "items", values); err != nil {
		t.Fatalf("InsertRow quoted: %v", err)
	}
	name, _ = selectItem(t, service, "2")
	if name == nil || *name != "O'Brien" {
		t.Fatalf("name = %v, want O'Brien", name)
	}
}

func TestRowWriter_updateAndDeleteByPrimaryKey(t *testing.T) {
	service := newRowWriteService(t)
	key := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "1"}}}
	if _, err := service.InsertRow(context.Background(), "items", []sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "first"}}}); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	result, err := service.UpdateRow(context.Background(), "items", key, []sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueNull}},
	})
	if err != nil {
		t.Fatalf("UpdateRow: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("update RowsAffected = %d, want 1", result.RowsAffected)
	}
	name, _ := selectItem(t, service, "1")
	if name != nil {
		t.Fatalf("name = %q, want NULL after update", *name)
	}

	result, err = service.DeleteRow(context.Background(), "items", key)
	if err != nil {
		t.Fatalf("DeleteRow: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("delete RowsAffected = %d, want 1", result.RowsAffected)
	}
	result, err = service.Execute(context.Background(), "SELECT COUNT(*) FROM items")
	if err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if got := *result.Rows[0][0]; got != "0" {
		t.Fatalf("row count = %q, want 0", got)
	}
}

func TestRowWriter_updateRejectsDefaultAndTypedKinds(t *testing.T) {
	service := newRowWriteService(t)
	key := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "1"}}}

	_, err := service.UpdateRow(context.Background(), "items", key, []sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueDefault}},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot update name to DEFAULT") {
		t.Fatalf("error = %v, want DEFAULT update rejection", err)
	}

	_, err = service.UpdateRow(context.Background(), "items", key, []sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueBool, Bool: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported row value kind bool") {
		t.Fatalf("error = %v, want unsupported kind rejection", err)
	}

	_, err = service.InsertRow(context.Background(), "items", []sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueInteger, Integer: 7}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported row value kind integer") {
		t.Fatalf("error = %v, want unsupported kind rejection", err)
	}
}
