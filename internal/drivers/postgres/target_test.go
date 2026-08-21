package postgres

import (
	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"net/url"
	"testing"
)

func TestTarget_buildsURL(t *testing.T) {
	target := Target(driver.FormValues{User: "alice", Pass: "secret", Host: "127.0.0.1", Port: "5432", Database: "app", TLS: "verify-full"})
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parsing URL %q: %v", target, err)
	}
	if parsed.Scheme != "postgres" {
		t.Fatalf("scheme = %q, want postgres", parsed.Scheme)
	}
	if parsed.User.Username() != "alice" {
		t.Fatalf("user = %q, want alice", parsed.User.Username())
	}
	if pass, _ := parsed.User.Password(); pass != "secret" {
		t.Fatalf("password = %q, want secret", pass)
	}
	if parsed.Host != "127.0.0.1:5432" || parsed.Path != "/app" {
		t.Fatalf("host/path = %q/%q, want 127.0.0.1:5432//app", parsed.Host, parsed.Path)
	}
	if got := parsed.Query().Get("sslmode"); got != "verify-full" {
		t.Fatalf("sslmode = %q, want verify-full", got)
	}
}

func TestTarget_blankSSLModesDefaultsToDisabled(t *testing.T) {
	parsed, err := url.Parse(Target(driver.FormValues{User: "alice", Host: "localhost", Port: "5432"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("sslmode"); got != "disable" {
		t.Fatalf("blank sslmode = %q, want disable", got)
	}
}
