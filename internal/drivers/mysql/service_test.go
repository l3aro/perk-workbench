package mysql

import (
	"context"
	stdsql "database/sql"
	"testing"

	driver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
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

func TestMySQLColumnDeclaration_usesUserAttributesWhenProvided(t *testing.T) {
	defaultValue := "Vietnam"
	comment := "COMMENT 'updated'"
	change := driver.ColumnChange{
		Type:         "varchar(50)",
		Nullable:     false,
		DefaultValue: &defaultValue,
		Attributes:   &comment,
	}
	attributes := mysqlColumnAttributes{
		characterSet: stdsql.NullString{String: "utf8mb4", Valid: true},
		collation:    stdsql.NullString{String: "utf8mb4_0900_ai_ci", Valid: true},
		comment:      stdsql.NullString{String: "original", Valid: true},
	}
	const want = "varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT 'Vietnam' COMMENT 'updated'"
	if got := mysqlColumnDeclaration(change, attributes); got != want {
		t.Fatalf("mysqlColumnDeclaration() = %q, want %q", got, want)
	}
}

func TestMySQLColumnDeclaration_dropsDbCommentWhenUserAttributesProvided(t *testing.T) {
	change := driver.ColumnChange{
		Type:     "varchar(50)",
		Nullable: true,
	}
	attributes := mysqlColumnAttributes{
		characterSet: stdsql.NullString{Valid: true, String: "utf8mb4"},
		collation:    stdsql.NullString{Valid: true, String: "utf8mb4_0900_ai_ci"},
		comment:      stdsql.NullString{Valid: true, String: "visible"},
	}
	const want = "varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL COMMENT 'visible'"
	if got := mysqlColumnDeclaration(change, attributes); got != want {
		t.Fatalf("mysqlColumnDeclaration() = %q, want %q", got, want)
	}
}

func TestMySQLColumnDeclaration_placesCharsetBeforeConstraints(t *testing.T) {
	defaultValue := "Vietnam"
	change := driver.ColumnChange{
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

func TestMySQLColumnDeclaration_appendsCommentAfterDefault(t *testing.T) {
	defaultValue := "active"
	comment := "COMMENT 'column status'"
	change := driver.ColumnChange{
		Type:         "varchar(20)",
		Nullable:     false,
		DefaultValue: &defaultValue,
		Attributes:   &comment,
	}
	attributes := mysqlColumnAttributes{
		characterSet: stdsql.NullString{Valid: false},
		collation:    stdsql.NullString{Valid: false},
	}
	const want = "varchar(20) NOT NULL DEFAULT 'active' COMMENT 'column status'"
	if got := mysqlColumnDeclaration(change, attributes); got != want {
		t.Fatalf("mysqlColumnDeclaration() = %q, want %q", got, want)
	}
}

func TestMySQLColumnDeclaration_appendsAutoIncrementAfterDefault(t *testing.T) {
	change := driver.ColumnChange{
		Type:       "BIGINT UNSIGNED",
		Nullable:   false,
		Attributes: strPtr("AUTO_INCREMENT"),
	}
	attributes := mysqlColumnAttributes{
		characterSet: stdsql.NullString{Valid: false},
		collation:    stdsql.NullString{Valid: false},
	}
	const want = "BIGINT UNSIGNED NOT NULL AUTO_INCREMENT"
	if got := mysqlColumnDeclaration(change, attributes); got != want {
		t.Fatalf("mysqlColumnDeclaration() = %q, want %q", got, want)
	}
}

func TestMySQLColumnDeclaration_preservesDbCommentWhenAttributesNil(t *testing.T) {
	change := driver.ColumnChange{
		Type:     "varchar(50)",
		Nullable: true,
	}
	attributes := mysqlColumnAttributes{
		characterSet: stdsql.NullString{Valid: false},
		collation:    stdsql.NullString{Valid: false},
		comment:      stdsql.NullString{String: "existing comment", Valid: true},
	}
	const want = "varchar(50) NULL COMMENT 'existing comment'"
	if got := mysqlColumnDeclaration(change, attributes); got != want {
		t.Fatalf("mysqlColumnDeclaration() = %q, want %q", got, want)
	}
}

func strPtr(s string) *string { return &s }
