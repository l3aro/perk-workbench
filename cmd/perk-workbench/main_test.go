package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/app"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestUsage_includesWorkbenchCommand(t *testing.T) {
	if !strings.Contains(usage, "perk-workbench") {
		t.Fatalf("usage = %q, want perk-workbench command", usage)
	}
}

func TestVersion_default_is_devel(t *testing.T) {
	if version != "devel" {
		t.Fatalf("version = %q, want %q", version, "devel")
	}
}

func TestVersionOutput_format(t *testing.T) {
	want := fmt.Sprintf("perk-workbench %s\n", version)
	if got := versionOutput(); got != want {
		t.Fatalf("versionOutput() = %q, want %q", got, want)
	}
}

func TestVersionOutput_includes_devel_by_default(t *testing.T) {
	out := versionOutput()
	if !strings.HasPrefix(out, "perk-workbench ") {
		t.Fatalf("versionOutput() = %q, want prefix perk-workbench ", out)
	}
	if !strings.HasSuffix(out, "devel\n") {
		t.Fatalf("versionOutput() = %q, want suffix devel\\n", out)
	}
}

func envLookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nDB_CONNECTION=sqlite\nexport DB_DATABASE=/tmp/chinook.db\nDB_PASSWORD='p@ss:w/rd'\nDB_HOST=\"my host\"\n  DB_USERNAME  =  root  \nINVALID_LINE_NO_EQUALS\n\nEMPTY_VALUE=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}

	got := loadDotEnv(path)

	want := map[string]string{
		"DB_CONNECTION": "sqlite",
		"DB_DATABASE":   "/tmp/chinook.db",
		"DB_PASSWORD":   "p@ss:w/rd",
		"DB_HOST":       "my host",
		"DB_USERNAME":   "root",
		"EMPTY_VALUE":   "",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("loadDotEnv() %s = %q, want %q", key, got[key], value)
		}
	}
	if len(got) != len(want) {
		t.Errorf("loadDotEnv() = %d vars, want %d (malformed line must be skipped)", len(got), len(want))
	}
}

func TestLoadDotEnv_missingFileIsNil(t *testing.T) {
	if got := loadDotEnv(filepath.Join(t.TempDir(), "absent.env")); got != nil {
		t.Fatalf("loadDotEnv() = %v, want nil", got)
	}
}

func TestPreferEnv_realWins(t *testing.T) {
	lookup := preferEnv(envLookup(map[string]string{
		"DB_CONNECTION": "mysql",
		"DB_HOST":       "db.example.com",
		"DB_USERNAME":   "root",
	}), map[string]string{"DB_CONNECTION": "sqlite"})
	if got := environmentTarget(lookup); !strings.HasPrefix(got, "mysql:") {
		t.Fatalf("environmentTarget() = %q, want mysql DSN from real env", got)
	}
}

func TestPreferEnv_fileFallback(t *testing.T) {
	lookup := preferEnv(envLookup(nil), map[string]string{"DB_CONNECTION": "sqlite", "DB_DATABASE": "/tmp/chinook.db"})
	if got := environmentTarget(lookup); got != "/tmp/chinook.db" {
		t.Fatalf("environmentTarget() = %q, want sqlite path from .env file", got)
	}
}

func TestEnvironmentTarget_sqlite(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "returns database path", env: map[string]string{"DB_CONNECTION": "sqlite", "DB_DATABASE": "  demo/chinook-sqlite.db  "}, want: "demo/chinook-sqlite.db"},
		{name: "normalizes connection name", env: map[string]string{"DB_CONNECTION": " SQLite ", "DB_DATABASE": "workbench.db"}, want: "workbench.db"},
		{name: "missing database", env: map[string]string{"DB_CONNECTION": "sqlite"}, want: ""},
		{name: "blank database", env: map[string]string{"DB_CONNECTION": "sqlite", "DB_DATABASE": "   "}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := environmentTarget(envLookup(test.env)); got != test.want {
				t.Fatalf("environmentTarget() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEnvironmentTarget_mysql(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantAddr   string
		wantDB     string
		wantPasswd string
	}{
		{
			name:       "complete with default port",
			env:        map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "db.example.com", "DB_USERNAME": "root", "DB_PASSWORD": "s3cret", "DB_DATABASE": "office"},
			wantAddr:   net.JoinHostPort("db.example.com", "3306"),
			wantDB:     "office",
			wantPasswd: "s3cret",
		},
		{
			name:       "supplied port and reserved password characters",
			env:        map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "db.example.com", "DB_PORT": "3307", "DB_USERNAME": "root", "DB_PASSWORD": "p@ss:w/rd"},
			wantAddr:   net.JoinHostPort("db.example.com", "3307"),
			wantDB:     "",
			wantPasswd: "p@ss:w/rd",
		},
		{
			name:       "blank optional password and database",
			env:        map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "db.example.com", "DB_USERNAME": "root", "DB_PASSWORD": "", "DB_DATABASE": ""},
			wantAddr:   net.JoinHostPort("db.example.com", "3306"),
			wantDB:     "",
			wantPasswd: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := environmentTarget(envLookup(test.env))
			if !strings.HasPrefix(target, "mysql:") {
				t.Fatalf("environmentTarget() = %q, want mysql: prefix", target)
			}
			cfg, err := mysql.ParseDSN(strings.TrimPrefix(target, "mysql:"))
			if err != nil {
				t.Fatalf("ParseDSN(%q) error = %v", target, err)
			}
			if cfg.User != "root" {
				t.Errorf("parsed user = %q, want root", cfg.User)
			}
			if cfg.Passwd != test.wantPasswd {
				t.Errorf("parsed password = %q, want %q", cfg.Passwd, test.wantPasswd)
			}
			if cfg.Addr != test.wantAddr {
				t.Errorf("parsed addr = %q, want %q", cfg.Addr, test.wantAddr)
			}
			if cfg.DBName != test.wantDB {
				t.Errorf("parsed dbname = %q, want %q", cfg.DBName, test.wantDB)
			}
			if cfg.TLSConfig != "false" {
				t.Errorf("parsed TLSConfig = %q, want false", cfg.TLSConfig)
			}
		})
	}
}

func TestEnvironmentTarget_pgsql(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantHost   string
		wantUser   string
		wantPasswd string
		wantDB     string
	}{
		{
			name:       "complete with default port",
			env:        map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "db.example.com", "DB_USERNAME": "admin", "DB_PASSWORD": "hunter2", "DB_DATABASE": "employees"},
			wantHost:   net.JoinHostPort("db.example.com", "5432"),
			wantUser:   "admin",
			wantPasswd: "hunter2",
			wantDB:     "employees",
		},
		{
			name:       "escaped username password and database",
			env:        map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "db.example.com", "DB_PORT": "5433", "DB_USERNAME": "u ser", "DB_PASSWORD": "p@ss:w/rd", "DB_DATABASE": "my db"},
			wantHost:   net.JoinHostPort("db.example.com", "5433"),
			wantUser:   "u ser",
			wantPasswd: "p@ss:w/rd",
			wantDB:     "my db",
		},
		{
			name:       "blank optional password and database",
			env:        map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "db.example.com", "DB_USERNAME": "admin"},
			wantHost:   net.JoinHostPort("db.example.com", "5432"),
			wantUser:   "admin",
			wantPasswd: "",
			wantDB:     "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := environmentTarget(envLookup(test.env))
			if !strings.HasPrefix(target, "postgres:") {
				t.Fatalf("environmentTarget() = %q, want postgres: prefix", target)
			}
			u, err := url.Parse(strings.TrimPrefix(target, "postgres:"))
			if err != nil {
				t.Fatalf("url.Parse(%q) error = %v", target, err)
			}
			if got := u.User.Username(); got != test.wantUser {
				t.Errorf("parsed user = %q, want %q", got, test.wantUser)
			}
			if got, _ := u.User.Password(); got != test.wantPasswd {
				t.Errorf("parsed password = %q, want %q", got, test.wantPasswd)
			}
			if u.Host != test.wantHost {
				t.Errorf("parsed host = %q, want %q", u.Host, test.wantHost)
			}
			if got := strings.TrimPrefix(u.Path, "/"); got != test.wantDB {
				t.Errorf("parsed database = %q, want %q", got, test.wantDB)
			}
			if got := u.Query().Get("sslmode"); got != "disable" {
				t.Errorf("parsed sslmode = %q, want disable", got)
			}
		})
	}
}

func TestEnvironmentTarget_rejectsIncomplete(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "unknown driver", env: map[string]string{"DB_CONNECTION": "oracle", "DB_HOST": "h", "DB_USERNAME": "u"}},
		{name: "missing connection", env: map[string]string{"DB_HOST": "h", "DB_USERNAME": "u"}},
		{name: "mysql missing host", env: map[string]string{"DB_CONNECTION": "mysql", "DB_USERNAME": "u"}},
		{name: "mysql blank host", env: map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "  ", "DB_USERNAME": "u"}},
		{name: "mysql missing username", env: map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "h"}},
		{name: "mysql malformed port", env: map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "h", "DB_PORT": "abc", "DB_USERNAME": "u"}},
		{name: "mysql zero port", env: map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "h", "DB_PORT": "0", "DB_USERNAME": "u"}},
		{name: "mysql out-of-range port", env: map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "h", "DB_PORT": "65536", "DB_USERNAME": "u"}},
		{name: "pgsql missing host", env: map[string]string{"DB_CONNECTION": "pgsql", "DB_USERNAME": "u"}},
		{name: "pgsql missing username", env: map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "h"}},
		{name: "pgsql malformed port", env: map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "h", "DB_PORT": "abc", "DB_USERNAME": "u"}},
		{name: "pgsql out-of-range port", env: map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "h", "DB_PORT": "70000", "DB_USERNAME": "u"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := environmentTarget(envLookup(test.env)); got != "" {
				t.Fatalf("environmentTarget() = %q, want \"\" to fall through to the picker", got)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantTarget string
		wantRO     bool
		wantSelect bool
		wantPin    bool
		wantErr    bool
	}{
		{name: "accepts no target", args: nil, wantTarget: ""},
		{name: "accepts memory target", args: []string{":memory:"}, wantTarget: ":memory:"},
		{name: "accepts one path target", args: []string{"workbench.db"}, wantTarget: "workbench.db"},
		{name: "accepts read-only flag", args: []string{"--read-only", "db.sqlite"}, wantTarget: "db.sqlite", wantRO: true},
		{name: "accepts short read-only flag", args: []string{"-r", "db.sqlite"}, wantTarget: "db.sqlite", wantRO: true},
		{name: "accepts read-only without target", args: []string{"--read-only"}, wantTarget: "", wantRO: true},
		{name: "accepts select flag", args: []string{"--select"}, wantSelect: true},
		{name: "accepts select with read-only", args: []string{"--select", "--read-only"}, wantRO: true, wantSelect: true},
		{name: "accepts pin flag", args: []string{"--pin"}, wantPin: true},
		{name: "accepts pin with target", args: []string{"--pin", "db.sqlite"}, wantTarget: "db.sqlite", wantPin: true},
		{name: "accepts pin with read-only", args: []string{"--read-only", "--pin"}, wantRO: true, wantPin: true},
		{name: "accepts select with pin", args: []string{"--select", "--pin"}, wantSelect: true, wantPin: true},
		{name: "rejects select with target", args: []string{"--select", "db.sqlite"}, wantErr: true},
		{name: "rejects select with pin and target", args: []string{"--select", "--pin", "db.sqlite"}, wantErr: true},
		{name: "rejects two targets", args: []string{"first.db", "second.db"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, readOnly, selectMode, pin, err := parseTarget(test.args)

			if test.wantErr {
				if err == nil {
					t.Fatal("parseTarget() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget() error = %v", err)
			}
			if got != test.wantTarget {
				t.Fatalf("parseTarget() target = %q, want %q", got, test.wantTarget)
			}
			if readOnly != test.wantRO {
				t.Fatalf("parseTarget() readOnly = %v, want %v", readOnly, test.wantRO)
			}
			if selectMode != test.wantSelect {
				t.Fatalf("parseTarget() selectMode = %v, want %v", selectMode, test.wantSelect)
			}
			if pin != test.wantPin {
				t.Fatalf("parseTarget() pin = %v, want %v", pin, test.wantPin)
			}
		})
	}
}

// closeRecorder is a closer stub that records its call order and returns
// a fixed error.
type closeRecorder struct {
	order *[]string
	label string
	err   error
}

func (c closeRecorder) Close() error {
	*c.order = append(*c.order, c.label)
	return c.err
}

func TestCloseProgram_closesServiceThenLoaderThenHistory(t *testing.T) {
	var order []string
	serviceErr := errors.New("service close failed")
	loaderErr := errors.New("loader close failed")
	historyErr := errors.New("history close failed")

	err := closeProgram(
		closeRecorder{order: &order, label: "service", err: serviceErr},
		closeRecorder{order: &order, label: "loader", err: loaderErr},
		closeRecorder{order: &order, label: "history", err: historyErr},
	)

	if got := strings.Join(order, ","); got != "service,loader,history" {
		t.Fatalf("close order = %q, want service,loader,history", got)
	}
	for _, want := range []error{serviceErr, loaderErr, historyErr} {
		if !errors.Is(err, want) {
			t.Fatalf("closeProgram error = %v, want it to join %v", err, want)
		}
	}
}

func TestCloseProgram_skipsNilClosers(t *testing.T) {
	if err := closeProgram(nil, nil, nil); err != nil {
		t.Fatalf("closeProgram(nil, nil, nil) = %v, want nil", err)
	}
}

// TestCloseProgram_skipsTypedNilClosers guards the typed-nil trap: a
// disabled AI provider returns a nil *ai.History that crosses into
// closeProgram as a non-nil interface, so the interface equality check
// alone would call Close on a nil receiver (a nil closeRecorder panics
// dereferencing its order slice).
func TestCloseProgram_skipsTypedNilClosers(t *testing.T) {
	var history *closeRecorder
	if err := closeProgram(nil, nil, history); err != nil {
		t.Fatalf("closeProgram(nil, nil, typed nil) = %v, want nil", err)
	}
	var service *closeRecorder
	var loader *closeRecorder
	if err := closeProgram(service, loader, history); err != nil {
		t.Fatalf("closeProgram(typed nils) = %v, want nil", err)
	}
}

func TestLoadPlugins_logsRejectedEntriesAndStaysClosable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	log.SetLevel(log.LevelDebug)
	defer log.SetLevel(log.LevelInfo)
	var entries []log.Entry
	log.SetNotifier(func(entry log.Entry) { entries = append(entries, entry) })
	defer log.SetNotifier(nil)

	registered := false
	loader := loadPlugins(context.Background(), app.Config{Plugins: []app.PluginConfig{{Path: "./no-such-plugin"}}}, func(database.Shim) error {
		registered = true
		return nil
	})
	if loader == nil {
		t.Fatal("loadPlugins returned a nil loader, want a closable loader")
	}
	if err := closeProgram(nil, loader, nil); err != nil {
		t.Fatalf("closing the loader after rejected entries = %v, want nil", err)
	}
	if registered {
		t.Fatal("register was called for an entry that failed to resolve")
	}
	var logged bool
	for _, entry := range entries {
		if entry.Level == log.LevelError && strings.Contains(entry.Message, "loading plugin") && strings.Contains(entry.Message, "no-such-plugin") {
			logged = true
		}
	}
	if !logged {
		t.Fatalf("log entries = %v, want an error entry naming the rejected plugin", entries)
	}
}

// TestLoadPlugins_refusesPinnedDriftBeforeSpawn: a configured pin whose
// digest no longer matches the executable's bytes is refused with a
// clear expected/actual error and the child is never spawned (proven by
// the marker file), while other entries still load.
func TestLoadPlugins_refusesPinnedDriftBeforeSpawn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	log.SetLevel(log.LevelDebug)
	defer log.SetLevel(log.LevelInfo)
	var entries []log.Entry
	log.SetNotifier(func(entry log.Entry) { entries = append(entries, entry) })
	defer log.SetNotifier(nil)

	dir := t.TempDir()
	marker := filepath.Join(dir, "events.log")
	script := writePluginHelperScriptAt(t, filepath.Join(dir, "drift-plugin"))
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	pin, err := plugin.SHA256File(script)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, append(contents, []byte("\n# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}

	registered := false
	loader := loadPlugins(context.Background(), app.Config{
		Plugins: []app.PluginConfig{{Path: script, SHA256: pin}},
	}, func(database.Shim) error {
		registered = true
		return nil
	})
	if err := closeProgram(nil, loader, nil); err != nil {
		t.Fatalf("closing the loader = %v", err)
	}
	if registered {
		t.Fatal("register was called for a drifted pinned plugin; it must never execute")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker file exists (%v): a drifted child was spawned", err)
	}
	var drifted bool
	for _, entry := range entries {
		if entry.Level == log.LevelError && strings.Contains(entry.Message, "pinned executable changed: expected sha256 "+pin) && strings.Contains(entry.Message, "got ") && strings.Contains(entry.Message, "refusing to start") {
			drifted = true
		}
	}
	if !drifted {
		t.Fatalf("log entries = %v, want a drift error with expected and actual digests", entries)
	}
}

// TestLoadPlugins_spawnsMatchingPin: a pin that matches the current
// bytes loads normally.
func TestLoadPlugins_spawnsMatchingPin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	marker := filepath.Join(dir, "events.log")
	script := writePluginHelperScriptAt(t, filepath.Join(dir, "pinned-plugin"))
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	digest, err := plugin.SHA256File(script)
	if err != nil {
		t.Fatal(err)
	}

	registered := false
	loader := loadPlugins(context.Background(), app.Config{
		Plugins: []app.PluginConfig{{Path: script, SHA256: digest}},
	}, func(database.Shim) error {
		registered = true
		return nil
	})
	if err := closeProgram(nil, loader, nil); err != nil {
		t.Fatalf("closing the loader = %v", err)
	}
	if !registered {
		t.Fatal("matching pin did not register the plugin")
	}
	if starts := markerLineCount(t, marker, "start"); starts != 1 {
		t.Fatalf("marker records %d child starts, want exactly the pinned one", starts)
	}
}

// TestLoadPlugins_legacyUnpinnedEntryStillLoads: entries without a
// trust record keep the pre-trust behavior — they load normally and
// report no drift.
func TestLoadPlugins_legacyUnpinnedEntryStillLoads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	marker := filepath.Join(dir, "events.log")
	script := writePluginHelperScriptAt(t, filepath.Join(dir, "legacy-plugin"))
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_MARKER", marker)

	registered := false
	loader := loadPlugins(context.Background(), app.Config{Plugins: []app.PluginConfig{{Path: script}}}, func(database.Shim) error {
		registered = true
		return nil
	})
	if err := closeProgram(nil, loader, nil); err != nil {
		t.Fatalf("closing the loader = %v", err)
	}
	if !registered {
		t.Fatal("unpinned legacy entry did not register")
	}
	if starts := markerLineCount(t, marker, "start"); starts != 1 {
		t.Fatalf("marker records %d child starts, want exactly the legacy entry's", starts)
	}
}

// fakePluginShim is a minimal database.Shim standing in for a plugin
// child. run() registers real plugin shims through database.RegisterShim
// before app.New builds the connection form, so a driver registered that
// way must be selectable and drive the form layout.
type fakePluginShim struct {
	name string
}

func (s fakePluginShim) Capabilities() database.Capabilities {
	return database.Capabilities{
		Name:    s.name,
		Display: "Reggie",
		Targets: []database.TargetPattern{{Prefix: s.name + ":", KeepTarget: false}},
		Form: &database.FormSpec{
			Prefix: s.name + ":",
			Fields: []database.FormField{{Key: "reggie_key", Title: "Reggie Key", Kind: database.FormInput}},
		},
	}
}

func (s fakePluginShim) BuildTarget(values database.FormValues) (string, bool) {
	return s.name + ":" + values.Database, true
}

func (fakePluginShim) Open(context.Context, string) (sharedsql.Service, error) {
	return nil, errors.New("not opened in test")
}

type builtinFormShim struct {
	database.Shim
	caps database.Capabilities
}

func (s builtinFormShim) Capabilities() database.Capabilities { return s.caps }
func (builtinFormShim) PluginSource() string                  { return "builtin" }

func sqliteFormCapabilities(shim database.Shim) database.Shim {
	caps := shim.Capabilities()
	caps.Form = &database.FormSpec{
		Fields: []database.FormField{{
			Key:         "target",
			Title:       "Target*",
			Kind:        database.FormInput,
			Placeholder: "path/to/database.db or :memory:",
			Validate:    database.FormRequired,
			Error:       "target is required",
		}},
	}
	return builtinFormShim{Shim: shim, caps: caps}
}

func TestPluginRegistration_isVisibleToConnectionForm(t *testing.T) {
	// RegisterShim is exactly the callback run() passes to plugin.Load;
	// a driver installed through it must be offered by the connection
	// form built afterwards.
	name := fmt.Sprintf("reggie%d", time.Now().UnixNano())
	if err := database.RegisterShim(fakePluginShim{name: name}); err != nil {
		t.Fatalf("RegisterShim: %v", err)
	}

	form := connection.NewForm()
	form.Values.Driver = connection.Driver(name)
	form.Rebuild()
	var hasField bool
	for _, title := range form.FieldTitles() {
		if title == "Reggie Key" {
			hasField = true
		}
	}
	if !hasField {
		t.Fatalf("form titles for %q = %v, want the shim's Reggie Key field", name, form.FieldTitles())
	}
}

func TestExistingConfigBuiltinDescriptorReachesSavedProfilePicker(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configPath := filepath.Join(configHome, "perk-workbench", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"theme":"nord"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := app.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil error", err)
	}
	if len(config.Plugins) != 4 || config.Plugins[0].Builtin != "sqlite" {
		t.Fatalf("loaded plugin descriptors = %#v, want the four bundled built-ins", config.Plugins)
	}
	unrelated := fmt.Sprintf("unrelated%d", time.Now().UnixNano())
	if err := database.RegisterShim(fakePluginShim{name: unrelated}); err != nil {
		t.Fatalf("register unrelated plugin: %v", err)
	}
	helper := setupPluginHelper(t, map[string]string{
		"PERK_PLUGIN_NAME":    "sqlite",
		"PERK_PLUGIN_DISPLAY": "SQLite",
	})
	entries := pluginEntries(config)
	if len(entries) == 0 {
		t.Fatal("pluginEntries returned no configured built-ins")
	}
	entry := entries[0]
	entry.Executable = helper
	entry.Args = nil
	loader, errs := plugin.Load(context.Background(), configPath, []plugin.Entry{entry}, func(shim database.Shim) error {
		return database.RegisterShim(sqliteFormCapabilities(shim))
	})
	if len(errs) != 0 {
		t.Fatalf("loading configured built-in descriptor = %v, want no errors", errs)
	}
	t.Cleanup(func() {
		if err := loader.Close(); err != nil {
			t.Errorf("closing plugin loader: %v", err)
		}
	})

	component := connection.New()
	component.LoadValues(profile.Profile{
		Plugin: "sqlite",
		Driver: profile.DriverSQLite,
		Name:   "Existing",
		Target: ":memory:",
	})
	_ = component.Form.Rebuild()
	view := component.Form.View()
	if !strings.Contains(view, "SQLite · Built-in") {
		t.Fatalf("saved-profile plugin picker view = %q, want the loaded built-in option", view)
	}
	if !strings.Contains(view, "Reggie · "+unrelated) {
		t.Fatalf("saved-profile plugin picker view = %q, want unrelated registered options retained", view)
	}
	if component.Form.Values.Plugin != "sqlite" {
		t.Fatalf("saved-profile selected plugin = %q, want the migrated sqlite plugin", component.Form.Values.Plugin)
	}
	if err := component.Form.Validate(); err != nil {
		t.Fatalf("saved profile validation = %v, driver=%q plugin=%q candidates=%v", err, component.Form.Values.Driver, component.Form.Values.Plugin, database.PluginsByDriver(string(component.Form.Values.Driver)))
	}
}

func TestParseOSC11Payload(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"\x1b]11;rgb:1c1c/1c1c/1c1c\x1b\\", "rgb:1c1c/1c1c/1c1c"},
		{"prefix\x1b]11;rgb:ffff/ffff/ffff\x1b\\trailing", "rgb:ffff/ffff/ffff"},
		{"\x1b]11;rgb:1c1c/1c1c/1c1c\x07", "rgb:1c1c/1c1c/1c1c"}, // BEL terminator
		{"  \x1b]11;rgb:2e2e/3434/4040\x1b\\  ", "rgb:2e2e/3434/4040"},
		{"no sequence here", ""},
		{"\x1b]11;notanrgb\x1b\\", ""},
		{"", ""},
	} {
		if got := parseOSC11Payload(tc.raw); got != tc.want {
			t.Fatalf("parseOSC11Payload(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
