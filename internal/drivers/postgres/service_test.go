package postgres

import (
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestPostgresTableIdentifier_qualifiesAndQuotesSchema(t *testing.T) {
	if got, want := postgresTableIdentifier(`audit.events`), `"audit"."events"`; got != want {
		t.Fatalf("postgresTableIdentifier() = %q, want %q", got, want)
	}
	if got, want := postgresTableIdentifier(`events`), `"public"."events"`; got != want {
		t.Fatalf("postgresTableIdentifier() = %q, want %q", got, want)
	}
}

func TestReturnsRows_recognizesPostgreSQLReturningStatements(t *testing.T) {
	for _, test := range []struct {
		statement string
		want      bool
	}{
		{statement: "SELECT 1", want: true},
		{statement: "INSERT INTO projects (name) VALUES ('next') RETURNING id", want: true},
		{statement: "UPDATE projects SET name = 'next'", want: false},
		{statement: "DELETE FROM projects RETURNING id", want: true},
	} {
		if got := ReturnsRows(test.statement); got != test.want {
			t.Errorf("ReturnsRows(%q) = %t, want %t", test.statement, got, test.want)
		}
	}
}

func TestPostgresForeignKeyClause_qualifiesReferenceTable(t *testing.T) {
	change := sharedsql.ForeignKeyChange{
		Columns:          []string{"account_id"},
		ReferenceTable:   "billing.accounts",
		ReferenceColumns: []string{"id"},
		OnDelete:         "CASCADE",
		OnUpdate:         "RESTRICT",
	}
	got := postgresForeignKeyClause(change)
	want := `FOREIGN KEY ("account_id") REFERENCES "billing"."accounts" ("id") ON DELETE CASCADE ON UPDATE RESTRICT`
	if got != want {
		t.Fatalf("postgresForeignKeyClause() = %q, want %q", got, want)
	}
}
