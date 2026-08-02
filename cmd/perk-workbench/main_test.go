package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
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
		wantErr    bool
	}{
		{name: "accepts no target", args: nil, wantTarget: ""},
		{name: "accepts memory target", args: []string{":memory:"}, wantTarget: ":memory:"},
		{name: "accepts one path target", args: []string{"workbench.db"}, wantTarget: "workbench.db"},
		{name: "accepts read-only flag", args: []string{"--read-only", "db.sqlite"}, wantTarget: "db.sqlite", wantRO: true},
		{name: "accepts short read-only flag", args: []string{"-r", "db.sqlite"}, wantTarget: "db.sqlite", wantRO: true},
		{name: "accepts read-only without target", args: []string{"--read-only"}, wantTarget: "", wantRO: true},
		{name: "rejects two targets", args: []string{"first.db", "second.db"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, readOnly, err := parseTarget(test.args)

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
		})
	}
}
