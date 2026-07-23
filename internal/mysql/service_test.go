package mysql

import (
	"context"
	"testing"
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
