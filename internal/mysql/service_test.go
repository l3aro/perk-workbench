package mysql

import (
	"context"
	stdsql "database/sql"
	"testing"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func TestOpenRejectsInvalidDSN(t *testing.T) {
	service, err := Open(context.Background(), "not-a-mysql-dsn")
	if err == nil {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
		t.Fatal("Open() error = nil, want invalid DSN error")
	}
}

func TestMySQLTableIdentifier_quotes_database_and_table_separately(t *testing.T) {
	if got, want := mysqlTableIdentifier("analytics.events"), "`analytics`.`events`"; got != want {
		t.Fatalf("mysqlTableIdentifier() = %q, want %q", got, want)
	}
}

func TestMySQLColumnDeclaration_placesCharsetBeforeConstraints(t *testing.T) {
	defaultValue := "Vietnam"
	change := sharedsql.ColumnChange{
		Type:         "varchar(50)",
		Nullable:     false,
		DefaultValue: &defaultValue,
	}
	attributes := mysqlColumnAttributes{
		characterSet: stdsql.NullString{String: "utf8mb4", Valid: true},
		collation:    stdsql.NullString{String: "utf8mb4_0900_ai_ci", Valid: true},
	}

	const want = "varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT 'Vietnam'"
	if got := mysqlColumnDeclaration(change, attributes); got != want {
		t.Fatalf("mysqlColumnDeclaration() = %q, want %q", got, want)
	}
}
