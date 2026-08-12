package connection

import (
	"path/filepath"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestForm_buildsMySQLDSNWithTLSMode(t *testing.T) {
	form := NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = DriverMySQL, "127.0.0.1", "3306"
	form.Values.User, form.Values.Pass, form.Values.Target = "alice", "secret", "app"
	form.Values.MySQLTLS = MySQLTLSSkipVerify

	dsn, err := mysql.ParseDSN(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.User != "alice" || dsn.Passwd != "secret" || dsn.Addr != "127.0.0.1:3306" || dsn.DBName != "app" {
		t.Fatalf("MySQL DSN = %#v, want separate field values", dsn)
	}
	if dsn.TLSConfig != "skip-verify" {
		t.Fatalf("MySQL TLS config = %q, want skip-verify", dsn.TLSConfig)
	}
}

func TestForm_blankTLSDefaultsToDisabled(t *testing.T) {
	form := NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = DriverMySQL, "localhost", "3306"
	form.Values.User = "alice"
	dsn, err := mysql.ParseDSN(form.TargetValue())
	if err != nil {
		t.Fatal(err)
	}
	if dsn.TLSConfig != "false" {
		t.Fatalf("blank MySQL TLS = %q, want disabled", dsn.TLSConfig)
	}
}

func TestForm_blankHostAndPortUseDefaults(t *testing.T) {
	form := NewForm()
	form.Values.Driver, form.Values.User = DriverPostgreSQL, "alice"
	if err := form.Validate(); err != nil {
		t.Fatalf("blank host/port rejected: %v", err)
	}
	if got := form.HostValue(); got != "localhost" {
		t.Fatalf("host = %q, want localhost", got)
	}
	if got := form.PortValue(); got != "5432" {
		t.Fatalf("port = %q, want the PostgreSQL default", got)
	}
}

func TestForm_validatesRequiredFields(t *testing.T) {
	form := NewForm()
	if err := form.Validate(); err == nil {
		t.Fatal("fresh SQLite form validated without a target")
	}
	form.Values.Driver = DriverMySQL
	form.Values.Host, form.Values.Port = "localhost", "not-a-port"
	if err := form.Validate(); err == nil {
		t.Fatal("MySQL form validated with an invalid port")
	}
}

func TestForm_profileCarriesRemoteFields(t *testing.T) {
	form := NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = DriverMySQL, "db.example.test", "3307"
	form.Values.User, form.Values.Pass, form.Values.Target = "alice", "secret", "app"
	form.Values.MySQLTLS = MySQLTLSSkipVerify
	p := form.Profile()
	if p.Host != "db.example.test" || p.Port != "3307" || p.User != "alice" || p.Target != "app" || p.MySQLTLS != MySQLTLSSkipVerify {
		t.Fatalf("profile = %#v, want remote fields carried", p)
	}
}

func TestRecord_reusesIdentityForEquivalentConnections(t *testing.T) {
	m := New()
	m.Path = filepath.Join(t.TempDir(), "profiles.json")
	m.Form.Values.Driver, m.Form.Values.Name, m.Form.Values.Target = DriverSQLite, "Local", "/tmp/a.db"

	first, err := m.Record("/tmp/a.db", false)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.ValidID(first.ID) {
		t.Fatalf("recorded identity = %q, want a valid profile scope", first.ID)
	}
	second, err := m.Record("/tmp/a.db", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("equivalent connection got a new identity %q, want %q reused", second.ID, first.ID)
	}
	if len(m.Profiles) != 1 {
		t.Fatalf("profiles = %d, want the duplicate deduped", len(m.Profiles))
	}
}

func TestRecord_sqliteTargetResolution(t *testing.T) {
	m := New()
	m.Path = filepath.Join(t.TempDir(), "profiles.json")
	m.Form.Values.Driver, m.Form.Values.Name, m.Form.Values.Target = DriverSQLite, "Local", "/entered/path.db"

	// The opened (resolved) target wins for SQLite.
	saved, err := m.Record("/resolved/a.db", true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Target != "/resolved/a.db" || !saved.ReadOnly {
		t.Fatalf("saved = %#v, want the resolved target and read-only flag", saved)
	}
}

func TestLoadValues_normalizesEmptyTLS(t *testing.T) {
	m := New()
	m.LoadValues(profile.Profile{Driver: DriverMySQL, Name: "Legacy", Target: "app"})
	if m.Form.Values.MySQLTLS != MySQLTLSDisabled {
		t.Fatalf("MySQL TLS = %q, want the disabled default", m.Form.Values.MySQLTLS)
	}
	if m.Form.Values.PostgreSQLTLS != PostgreSQLTLSDisabled {
		t.Fatalf("PostgreSQL TLS = %q, want the disabled default", m.Form.Values.PostgreSQLTLS)
	}
}

func TestDelete_removesMatchingProfile(t *testing.T) {
	m := New()
	m.Path = filepath.Join(t.TempDir(), "profiles.json")
	m.Form.Values.Driver, m.Form.Values.Name, m.Form.Values.Target = DriverSQLite, "A", "/tmp/a.db"
	first, _ := m.Record("/tmp/a.db", false)
	m.Form.Values.Name, m.Form.Values.Target = "B", "/tmp/b.db"
	if _, err := m.Record("/tmp/b.db", false); err != nil {
		t.Fatal(err)
	}
	m.Delete(first)
	if len(m.Profiles) != 1 || m.Profiles[0].Name != "B" {
		t.Fatalf("profiles after delete = %#v, want only B", m.Profiles)
	}
}
