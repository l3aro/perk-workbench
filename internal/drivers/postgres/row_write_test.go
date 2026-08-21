package postgres

import (
	"strings"
	"testing"

	driver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func TestPostgresInsertStatement_allDefaultsUsesDefaultValues(t *testing.T) {
	if got, want := postgresInsertStatement("audit.events", nil), `INSERT INTO "audit"."events" DEFAULT VALUES`; got != want {
		t.Fatalf("postgresInsertStatement() = %q, want %q", got, want)
	}
}

func TestPostgresInsertStatement_numbersPlaceholders(t *testing.T) {
	got := postgresInsertStatement("events", []string{`"name"`, `"note"`})
	want := `INSERT INTO "public"."events" ("name", "note") VALUES ($1, $2)`
	if got != want {
		t.Fatalf("postgresInsertStatement() = %q, want %q", got, want)
	}
}

func TestPostgresInsertParts_omitsDefaultsAndBindsValues(t *testing.T) {
	columns, args, err := postgresInsertParts([]driver.RowValue{
		{Name: "id", Value: driver.Value{Kind: driver.ValueDefault}},
		{Name: "name", Value: driver.Value{Kind: driver.ValueString, String: "x"}},
		{Name: "note", Value: driver.Value{Kind: driver.ValueNull}},
	})
	if err != nil {
		t.Fatalf("postgresInsertParts: %v", err)
	}
	if len(columns) != 2 || columns[0] != `"name"` || columns[1] != `"note"` {
		t.Fatalf("columns = %#v, want [\"name\" \"note\"]", columns)
	}
	if len(args) != 2 || args[0] != "x" || args[1] != nil {
		t.Fatalf("args = %#v, want [x nil]", args)
	}
}

func TestPostgresUpdateParts_rejectsDefault(t *testing.T) {
	_, _, err := postgresUpdateParts([]driver.RowValue{{Name: "name", Value: driver.Value{Kind: driver.ValueDefault}}})
	if err == nil || !strings.Contains(err.Error(), "cannot update name to DEFAULT") {
		t.Fatalf("error = %v, want DEFAULT update rejection", err)
	}
}

func TestPostgresUpdateParts_numbersPlaceholders(t *testing.T) {
	sets, args, err := postgresUpdateParts([]driver.RowValue{
		{Name: "name", Value: driver.Value{Kind: driver.ValueString, String: "y"}},
		{Name: "note", Value: driver.Value{Kind: driver.ValueNull}},
	})
	if err != nil {
		t.Fatalf("postgresUpdateParts: %v", err)
	}
	if len(sets) != 2 || sets[0] != `"name" = $1` || sets[1] != `"note" = $2` {
		t.Fatalf("sets = %#v, want [\"name\" = $1 \"note\" = $2]", sets)
	}
	if len(args) != 2 || args[0] != "y" || args[1] != nil {
		t.Fatalf("args = %#v, want [y nil]", args)
	}
	if got := postgresUpdateStatement("events", sets, `"id" = $3`); got != `UPDATE "public"."events" SET "name" = $1, "note" = $2 WHERE "id" = $3` {
		t.Fatalf("postgresUpdateStatement() = %q", got)
	}
}

func TestPostgresKeyCondition_preservesNullAndContinuesNumbering(t *testing.T) {
	where, args, err := postgresKeyCondition([]driver.RowValue{
		{Name: "tenant", Value: driver.Value{Kind: driver.ValueNull}},
		{Name: "id", Value: driver.Value{Kind: driver.ValueString, String: "7"}},
	}, 2) // two update placeholders already bound
	if err != nil {
		t.Fatalf("postgresKeyCondition: %v", err)
	}
	if want := `"tenant" IS NULL AND "id" = $3`; where != want {
		t.Fatalf("where = %q, want %q", where, want)
	}
	if len(args) != 1 || args[0] != "7" {
		t.Fatalf("args = %#v, want [7]", args)
	}
	if got := postgresDeleteStatement("events", where); got != `DELETE FROM "public"."events" WHERE "tenant" IS NULL AND "id" = $3` {
		t.Fatalf("postgresDeleteStatement() = %q", got)
	}
}

func TestPostgresKeyCondition_rejectsEmptyKey(t *testing.T) {
	if _, _, err := postgresKeyCondition(nil, 0); err == nil || !strings.Contains(err.Error(), "row key is empty") {
		t.Fatalf("error = %v, want empty-key rejection", err)
	}
}
