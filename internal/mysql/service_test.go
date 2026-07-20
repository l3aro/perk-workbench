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
