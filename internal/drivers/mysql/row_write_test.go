package mysql

import (
	"strings"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestMySQLInsertStatement_allDefaultsUsesEmptyColumnList(t *testing.T) {
	if got, want := mysqlInsertStatement("analytics.events", nil), "INSERT INTO `analytics`.`events` () VALUES ()"; got != want {
		t.Fatalf("mysqlInsertStatement() = %q, want %q", got, want)
	}
}

func TestMySQLInsertStatement_buildsPlaceholders(t *testing.T) {
	got := mysqlInsertStatement("events", []string{"`name`", "`note`"})
	want := "INSERT INTO `events` (`name`, `note`) VALUES (?, ?)"
	if got != want {
		t.Fatalf("mysqlInsertStatement() = %q, want %q", got, want)
	}
}

func TestMySQLInsertParts_omitsDefaultsAndBindsValues(t *testing.T) {
	columns, args, err := mysqlInsertParts([]sharedsql.RowValue{
		{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueDefault}},
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "x"}},
		{Name: "note", Value: sharedsql.Value{Kind: sharedsql.ValueNull}},
	})
	if err != nil {
		t.Fatalf("mysqlInsertParts: %v", err)
	}
	if len(columns) != 2 || columns[0] != "`name`" || columns[1] != "`note`" {
		t.Fatalf("columns = %#v, want [`name` `note`]", columns)
	}
	if len(args) != 2 || args[0] != "x" || args[1] != nil {
		t.Fatalf("args = %#v, want [x nil]", args)
	}
}

func TestMySQLUpdateParts_rejectsDefault(t *testing.T) {
	_, _, err := mysqlUpdateParts([]sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueDefault}}})
	if err == nil || !strings.Contains(err.Error(), "cannot update name to DEFAULT") {
		t.Fatalf("error = %v, want DEFAULT update rejection", err)
	}
}

func TestMySQLUpdateParts_buildsSetTerms(t *testing.T) {
	sets, args, err := mysqlUpdateParts([]sharedsql.RowValue{
		{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "y"}},
		{Name: "note", Value: sharedsql.Value{Kind: sharedsql.ValueNull}},
	})
	if err != nil {
		t.Fatalf("mysqlUpdateParts: %v", err)
	}
	if len(sets) != 2 || sets[0] != "`name` = ?" || sets[1] != "`note` = ?" {
		t.Fatalf("sets = %#v, want [`name` = ? `note` = ?]", sets)
	}
	if len(args) != 2 || args[0] != "y" || args[1] != nil {
		t.Fatalf("args = %#v, want [y nil]", args)
	}
	if got := mysqlUpdateStatement("events", sets, "`id` = ?"); got != "UPDATE `events` SET `name` = ?, `note` = ? WHERE `id` = ?" {
		t.Fatalf("mysqlUpdateStatement() = %q", got)
	}
}

func TestMySQLKeyCondition_preservesNullPredicates(t *testing.T) {
	where, args, err := mysqlKeyCondition([]sharedsql.RowValue{
		{Name: "tenant", Value: sharedsql.Value{Kind: sharedsql.ValueNull}},
		{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "7"}},
	})
	if err != nil {
		t.Fatalf("mysqlKeyCondition: %v", err)
	}
	if want := "`tenant` IS NULL AND `id` = ?"; where != want {
		t.Fatalf("where = %q, want %q", where, want)
	}
	if len(args) != 1 || args[0] != "7" {
		t.Fatalf("args = %#v, want [7]", args)
	}
	if got := mysqlDeleteStatement("events", where); got != "DELETE FROM `events` WHERE `tenant` IS NULL AND `id` = ?" {
		t.Fatalf("mysqlDeleteStatement() = %q", got)
	}
}

func TestMySQLKeyCondition_rejectsEmptyKey(t *testing.T) {
	if _, _, err := mysqlKeyCondition(nil); err == nil || !strings.Contains(err.Error(), "row key is empty") {
		t.Fatalf("error = %v, want empty-key rejection", err)
	}
}
