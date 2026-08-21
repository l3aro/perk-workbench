package connection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestForm_buildsMySQLDSNWithTLSMode(t *testing.T) {
	form := NewForm()
	form.Values.Plugin, form.Values.Driver, form.Values.Host, form.Values.Port = "mysql", DriverMySQL, "127.0.0.1", "3306"
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
	form.Values.Plugin, form.Values.Driver, form.Values.Host, form.Values.Port = "mysql", DriverMySQL, "localhost", "3306"
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
	form.Values.Plugin, form.Values.Driver, form.Values.User = "postgres", DriverPostgreSQL, "alice"
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
	form.Values.Plugin, form.Values.Driver = "mysql", DriverMySQL
	if err := form.Validate(); err == nil {
		t.Fatal("MySQL form validated with an invalid port")
	}
}

func TestForm_profileCarriesRemoteFields(t *testing.T) {
	form := NewForm()
	form.Values.Plugin, form.Values.Driver, form.Values.Host, form.Values.Port = "mysql", DriverMySQL, "db.example.test", "3307"
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
	m.Form.Values.Plugin = "sqlite"

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
	m.Form.Values.Plugin = "sqlite"

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
	m.Form.Values.Plugin = "sqlite"
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

func TestFieldTitles_followDriverSpec(t *testing.T) {
	form := NewForm()
	// A fresh form materializes the first registered driver (SQLite).
	if form.Values.Driver != DriverSQLite {
		t.Fatalf("fresh form driver = %q, want sqlite", form.Values.Driver)
	}
	wantSQLite := []string{"Plugin", "Name", "Target*", "Read-Only", "Action"}
	if got := form.FieldTitles(); !slicesEqual(got, wantSQLite) {
		t.Fatalf("sqlite titles = %v, want %v", got, wantSQLite)
	}
	form.Values.Plugin, form.Values.Driver = "mysql", DriverMySQL
	wantServer := []string{"Plugin", "Name", "Host", "Port", "Username*", "Password", "Database", "TLS", "Privacy", "Read-Only", "Action"}
	if got := form.FieldTitles(); !slicesEqual(got, wantServer) {
		t.Fatalf("mysql titles = %v, want %v", got, wantServer)
	}
	form.Values.Plugin, form.Values.Driver = "postgres", DriverPostgreSQL
	if got := form.FieldTitles(); !slicesEqual(got, wantServer) {
		t.Fatalf("postgres titles = %v, want %v", got, wantServer)
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestDriverSelect_offersRegisteredFormDrivers(t *testing.T) {
	form := NewForm()
	if got := form.selectLabels("plugin"); !slicesEqual(got, []string{"SQLite · sqlite", "MySQL · mysql", "PostgreSQL · postgres"}) {
		t.Fatalf("driver labels = %v, want SQLite/MySQL/PostgreSQL", got)
	}
	form.Values.Plugin, form.Values.Driver = "mysql", DriverMySQL
	if got := form.selectLabels("tls"); !slicesEqual(got, []string{"Verify certificate", "Encrypt, don't verify certificate", "Don't encrypt"}) {
		t.Fatalf("mysql TLS labels = %v, want the three modes", got)
	}
	form.Values.Plugin, form.Values.Driver = "postgres", DriverPostgreSQL
	if got := form.selectLabels("tls"); !slicesEqual(got, []string{"Verify certificate", "Encrypt, don't verify certificate", "Don't encrypt"}) {
		t.Fatalf("postgres TLS labels = %v, want the three modes", got)
	}
}

func TestExtras_roundTripThroughProfile(t *testing.T) {
	m := New()
	m.Path = filepath.Join(t.TempDir(), "profiles.json")
	m.Form.Values.Driver, m.Form.Values.Name, m.Form.Values.Target = DriverMySQL, "Remote", "app"
	m.Form.Values.Plugin = "mysql"
	m.Form.Values.Host, m.Form.Values.Port, m.Form.Values.User = "db.example.test", "3306", "alice"
	m.Form.Values.Extras = map[string]string{"schema": "public"}

	p := m.Form.Profile()
	if p.Extras["schema"] != "public" {
		t.Fatalf("profile extras = %v, want the driver field carried", p.Extras)
	}

	m.LoadValues(p)
	if m.Form.Values.Extras["schema"] != "public" {
		t.Fatalf("form extras after load = %v, want the profile field carried", m.Form.Values.Extras)
	}

	recorded, err := m.Record("/unused", false)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Extras["schema"] != "public" {
		t.Fatalf("recorded extras = %v, want the driver field carried", recorded.Extras)
	}
	// Recording must not alias the form's map: mutating one side never
	// leaks into the other.
	recorded.Extras["schema"] = "mutated"
	if m.Form.Values.Extras["schema"] != "public" {
		t.Fatal("recorded profile aliases the form extras map")
	}
}

// --- undecryptable-blob fail-closed round-trip ---------------------------

func TestLoadValues_carriesUndecryptableMarker(t *testing.T) {
	model := New()
	blob := "enc:v2:" + strings.Repeat("A", 100)
	saved := profile.Profile{Driver: DriverMySQL, Name: "Prod", Host: "h", Port: "3306", User: "u", Pass: blob}
	saved.Undecryptable = map[string]string{"pass": blob}
	model.LoadValues(saved)
	if got := model.Form.Values.Undecryptable["pass"]; got != blob {
		t.Fatalf("form marker = %q, want the retained blob carried", got)
	}
	if model.Form.Values.Pass != blob {
		t.Fatalf("form pass = %q, want the retained blob (never a literal)", model.Form.Values.Pass)
	}
}

func TestRecord_retainedBlobRefusesSaveUntilReentered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	blob := "enc:v2:" + strings.Repeat("B", 100)
	model := New()
	model.Path = filepath.Join(t.TempDir(), "connections.json")
	model.Form.Values.Driver = DriverMySQL
	model.Form.Values.Plugin = "mysql"
	model.Form.Values.Name = "Prod"
	model.Form.Values.Host, model.Form.Values.Port, model.Form.Values.User = "h", "3306", "u"
	model.Form.Values.Pass = blob
	model.Form.Values.Undecryptable = map[string]string{"pass": blob}

	// An untouched retained blob must never be re-wrapped as a literal.
	if _, err := model.Record("", false); err == nil || !strings.Contains(err.Error(), "undecryptable") {
		t.Fatalf("Record error = %v, want the undecryptable refusal", err)
	}
	if _, err := os.Stat(model.Path); !os.IsNotExist(err) {
		t.Fatalf("refused save still wrote %s", model.Path)
	}

	// Re-entering the password clears the refusal and saves normally.
	model.Form.Values.Pass = "fresh-password"
	recorded, err := model.Record("", false)
	if err != nil {
		t.Fatalf("Record after re-entering: %v", err)
	}
	if recorded.Pass != "fresh-password" {
		t.Fatalf("recorded pass = %q, want the re-entered value", recorded.Pass)
	}
	loaded, _, secretFail := profile.Load(model.Path)
	if secretFail || len(loaded) != 1 || loaded[0].Pass != "fresh-password" {
		t.Fatalf("loaded = %#v/%t, want the re-entered password persisted", loaded, secretFail)
	}
}
func TestPluginSelectorPersistsExactPlugin(t *testing.T) {
	err := database.RegisterShim(connectionTestShim{
		caps: database.Capabilities{
			Name: "mysql-cloud", Driver: "mysql", Display: "MySQL",
			Targets: []database.TargetPattern{{Prefix: "mysql:"}},
			Form: &database.FormSpec{
				Prefix: "mysql:",
				Fields: []database.FormField{{Key: "database", Title: "Database", Kind: database.FormInput}},
			},
		},
		product: "MySQL Cloud",
		build: func(values database.FormValues) (string, bool) {
			return values.Database, true
		},
	})
	if err != nil {
		t.Fatalf("RegisterShim(mysql-cloud): %v", err)
	}
	database.Register(database.Spec{
		PluginID: "mysql-builtin-label",
		Driver:   "mysql",
		Display:  "MySQL",
		Source:   "builtin",
		Targets:  []database.TargetPattern{{Prefix: "mysql:"}},
		Form:     &database.FormSpec{Prefix: "mysql:", Fields: []database.FormField{{Key: "database", Title: "Database", Kind: database.FormInput}}},
		Open: func(context.Context, string) (sharedsql.Service, error) {
			return &connectionTestService{product: "MySQL Built-in"}, nil
		},
	})
	form := NewForm()
	labels := form.selectLabels("plugin")
	foundCloud, foundBuiltin := false, false
	for _, label := range labels {
		switch label {
		case "MySQL · mysql-cloud":
			foundCloud = true
		case "MySQL · Built-in":
			foundBuiltin = true
		}
	}
	if !foundCloud || !foundBuiltin {
		t.Fatalf("plugin labels = %v, want mysql-cloud and Built-in labels", labels)
	}
	form.SelectPlugin("mysql-cloud")
	form.Values.Name = "Cloud"
	form.Values.Target = "app"
	model := New()
	model.Path = filepath.Join(t.TempDir(), "connections.json")
	model.Form = form
	saved, err := model.Record("", false)
	if err != nil {
		t.Fatalf("Record(mysql-cloud): %v", err)
	}
	if saved.Plugin != "mysql-cloud" || saved.Driver != DriverMySQL {
		t.Fatalf("saved profile = %#v, want exact plugin and mysql family", saved)
	}
	model.LoadValues(saved)
	if model.Form.Values.Plugin != "mysql-cloud" {
		t.Fatalf("reloaded plugin = %q, want mysql-cloud", model.Form.Values.Plugin)
	}
	spec, ok := database.ByPlugin(saved.Plugin)
	if !ok {
		t.Fatalf("saved plugin %q not registered", saved.Plugin)
	}
	opened, err := spec.Open(context.Background(), "app")
	if err != nil {
		t.Fatalf("reopen %q: %v", saved.Plugin, err)
	}
	if opened.Info().Product != "MySQL Cloud" {
		t.Fatalf("reopened product = %q, want MySQL Cloud", opened.Info().Product)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close reopened service: %v", err)
	}
}
