package mysql

import (
	"testing"

	godrv "github.com/go-sql-driver/mysql"
)

func TestTarget_buildsDSN(t *testing.T) {
	dsn := Target("alice", "secret", "127.0.0.1", "3306", "app", "skip-verify")
	parsed, err := godrv.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parsing DSN %q: %v", dsn, err)
	}
	if parsed.User != "alice" || parsed.Passwd != "secret" || parsed.Net != "tcp" || parsed.Addr != "127.0.0.1:3306" || parsed.DBName != "app" {
		t.Fatalf("DSN = %#v, want separate field values", parsed)
	}
	if parsed.TLSConfig != "skip-verify" {
		t.Fatalf("TLS config = %q, want skip-verify", parsed.TLSConfig)
	}
}

func TestTarget_blankTLSDefaultsToDisabled(t *testing.T) {
	dsn := Target("alice", "", "localhost", "3306", "", "")
	parsed, err := godrv.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TLSConfig != "false" {
		t.Fatalf("blank TLS = %q, want disabled", parsed.TLSConfig)
	}
}
