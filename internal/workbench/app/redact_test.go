package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// credentialOpen returns an OpenDatabase whose errors echo the target,
// the worst case a plugin or driver error could produce.
func credentialOpen(target string) OpenDatabase {
	return func(_ context.Context, _ string, opened string) (sharedsql.Opened, error) {
		return sharedsql.Opened{}, fmt.Errorf("connect refused: %s", opened)
	}
}

func TestRedactCredentials_valueAwareAndURLUserinfoAware(t *testing.T) {
	const password = "secret-pw-9"
	tests := []struct {
		name   string
		text   string
		want   []string // substrings that must NOT survive
		keep   []string // useful non-secret context that must survive
		marked bool     // whether a redaction marker is expected
	}{
		{
			name:   "postgres URL userinfo",
			text:   "connect refused: postgres://alice:secret-pw-9@db.example.test:5432/app?sslmode=disable",
			want:   []string{"secret-pw-9", "alice:secret-pw-9@", ":secret-pw-9@"},
			keep:   []string{"connect refused", "postgres://alice:", "@db.example.test:5432/app?sslmode=disable"},
			marked: true,
		},
		{
			name:   "label-prefixed URL target",
			text:   "connect refused: postgres:postgres://alice:secret-pw-9@db.example.test:5432/app",
			want:   []string{"secret-pw-9", "alice:secret-pw-9@"},
			keep:   []string{"connect refused", "postgres://alice:", "@db.example.test:5432/app"},
			marked: true,
		},
		{
			name:   "mysql DSN raw password",
			text:   "dial error: mysql:alice:secret-pw-9@tcp(db.example.test:3306)/app",
			want:   []string{"secret-pw-9", "alice:secret-pw-9@"},
			keep:   []string{"dial error", "alice:", "@tcp(db.example.test:3306)/app"},
			marked: true,
		},
		{
			name:   "percent-encoded secret",
			text:   "bad uri: redis://alice:p%40ss%3Aw%2Frd@127.0.0.1:6379/0",
			want:   []string{"p%40ss%3Aw%2Frd"},
			keep:   []string{"bad uri", "redis://alice:", "@127.0.0.1:6379/0"},
			marked: true,
		},
		{
			name:   "benign text untouched",
			text:   "connection refused to secretary@example.com; see file:///home/alice/secret-notes.txt",
			want:   []string{"secret-pw-9"},
			keep:   []string{"secretary@example.com", "file:///home/alice/secret-notes.txt", "connection refused"},
			marked: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := redactCredentials(test.text, []string{password, "p@ss:w/rd"})
			for _, forbidden := range test.want {
				if strings.Contains(got, forbidden) {
					t.Fatalf("redacted text %q still contains %q", got, forbidden)
				}
			}
			for _, keep := range test.keep {
				if !strings.Contains(got, keep) {
					t.Fatalf("redacted text %q lost useful context %q", got, keep)
				}
			}
			if test.marked && !strings.Contains(got, "[redacted]") {
				t.Fatalf("redacted text %q carries no redaction marker", got)
			}
			if !test.marked && strings.Contains(got, "[redacted]") {
				t.Fatalf("benign text %q was redacted into %q", test.text, got)
			}
		})
	}
}

func TestTargetPasswords_extractsURLAndDSNPasswords(t *testing.T) {
	tests := []struct {
		target string
		want   []string
	}{
		{"postgres://alice:secret-pw-9@db:5432/app?sslmode=disable", []string{"secret-pw-9"}},
		{"postgres:postgres://alice:secret-pw-9@db:5432/app", []string{"secret-pw-9"}},
		{"redis:redis://alice:p%40ss%3Aw%2Frd@127.0.0.1:6379/0", []string{"p@ss:w/rd", "p%40ss%3Aw%2Frd"}},
		{"mysql:alice:secret-pw-9@tcp(db:3306)/app", []string{"secret-pw-9"}},
		{"postgres://alice@db:5432/app", nil},
		{"/tmp/local.db", nil},
		{"", nil},
	}
	for _, test := range tests {
		got := targetPasswords(test.target)
		if len(got) != len(test.want) {
			t.Fatalf("targetPasswords(%q) = %v, want %v", test.target, got, test.want)
		}
		for i := range test.want {
			if got[i] != test.want[i] {
				t.Fatalf("targetPasswords(%q) = %v, want %v", test.target, got, test.want)
			}
		}
	}
}

// drainNotifications flushes stale entries from the package-global log
// notification queue so a test's own entries surface as the popup.
func drainNotifications(model Model) {
	for range 5 {
		model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	}
}

func readEventLog(t *testing.T) []byte {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func TestOpenFailure_redactsCredentialsAcrossSurfaces(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), credentialOpen(""), false)
	model.connection.component.Form.Values.Driver = driverPostgreSQL
	model.connection.component.Form.Values.Plugin = "postgres"
	model.connection.component.Form.Values.Name = "Prod"
	model.connection.component.Form.Values.Host, model.connection.component.Form.Values.Port = "db.example.test", "5432"
	model.connection.component.Form.Values.User, model.connection.component.Form.Values.Pass = "alice", "secret-pw-9"
	model.connection.component.Form.Focus = connectionFocusForm
	drainNotifications(model)

	updated, command := model.openConnection()
	model = updated.(Model)
	if command == nil {
		t.Fatal("no open command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	// Status line and failure surfaces carry the useful context but no
	// credential material.
	if !strings.Contains(model.Status, "database unavailable") {
		t.Fatalf("status = %q, want the failure context preserved", model.Status)
	}
	for _, forbidden := range []string{"secret-pw-9", "alice:secret-pw-9@", ":secret-pw-9@"} {
		if strings.Contains(model.Status, forbidden) {
			t.Fatalf("status %q still contains %q", model.Status, forbidden)
		}
	}
	if popup := model.notifications.component.Popup; popup != nil {
		for _, forbidden := range []string{"secret-pw-9", "alice:secret-pw-9@"} {
			if strings.Contains(popup.Description, forbidden) || strings.Contains(popup.Title, forbidden) {
				t.Fatalf("popup %#v still contains %q", popup, forbidden)
			}
		}
	} else {
		t.Fatal("no notification popup surfaced for the open failure")
	}
	// The event log (persisted to disk) never receives the raw error.
	logBytes := readEventLog(t)
	for _, forbidden := range []string{"secret-pw-9", "alice:secret-pw-9@"} {
		if bytes.Contains(logBytes, []byte(forbidden)) {
			t.Fatalf("event log contains %q: %s", forbidden, logBytes)
		}
	}
	if !bytes.Contains(logBytes, []byte("connect refused")) {
		t.Fatalf("event log lost the useful error context: %s", logBytes)
	}
}

func TestReconnectFailure_persistsRedactedNotificationHistory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connectionID = "scope-1"
	model.Target = "postgres://alice:secret-pw-9@db.example.test:5432/app?sslmode=disable"
	drainNotifications(model)

	const failure = "switch failed: postgres://alice:secret-pw-9@db.example.test:5432/app?sslmode=disable"
	updated, _ := model.Update(databaseOpenedMsg{err: errors.New(failure), reconnect: true, openTag: model.openTag})
	model = updated.(Model)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	if !strings.Contains(model.Status, "database switch failed") || strings.Contains(model.Status, "secret-pw-9") {
		t.Fatalf("status = %q, want the failure context without credentials", model.Status)
	}
	logBytes := readEventLog(t)
	if bytes.Contains(logBytes, []byte("secret-pw-9")) || bytes.Contains(logBytes, []byte("alice:secret-pw-9@")) {
		t.Fatalf("event log contains credentials: %s", logBytes)
	}
	// The scoped notification history (SQLite, persisted) receives only
	// the redacted entry.
	path, err := notificationPath()
	if err != nil {
		t.Fatal(err)
	}
	history, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(history, []byte("secret-pw-9")) || bytes.Contains(history, []byte("alice:secret-pw-9@")) {
		t.Fatalf("notification history contains credentials: %s", history)
	}
	if !bytes.Contains(history, []byte("database switch failed")) {
		t.Fatalf("notification history lost the useful context: %s", history)
	}
}

func TestConnectionTestFailure_redactsErrorBeforeLog(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), credentialOpen(""), false)
	model.connection.component.Form.Values.Driver = driverMySQL
	model.connection.component.Form.Values.Plugin = "mysql"
	model.connection.component.Form.Values.Name = "Prod"
	model.connection.component.Form.Values.Host, model.connection.component.Form.Values.Port = "db.example.test", "3306"
	model.connection.component.Form.Values.User, model.connection.component.Form.Values.Pass = "alice", "secret-pw-9"
	drainNotifications(model)

	message := model.testConnection()()
	updated, _ := model.Update(message)
	model = updated.(Model)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	if model.Status != "connection test failed" {
		t.Fatalf("status = %q, want connection test failed", model.Status)
	}
	logBytes := readEventLog(t)
	for _, forbidden := range []string{"secret-pw-9", "alice:secret-pw-9@"} {
		if bytes.Contains(logBytes, []byte(forbidden)) {
			t.Fatalf("event log contains %q: %s", forbidden, logBytes)
		}
	}
	if !bytes.Contains(logBytes, []byte("connect refused")) {
		t.Fatalf("event log lost the useful error context: %s", logBytes)
	}
}

func TestChatContext_neverCarriesConnectionCredentials(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.chat.component.Target = "postgres://alice:secret-pw-9@db.example.test:5432/app?sslmode=disable"
	text := model.chat.component.ContextText(chat.Context{Database: sharedsql.DatabaseInfo{Product: "PostgreSQL", Version: "16"}})
	if strings.Contains(text, "secret-pw-9") || strings.Contains(text, "postgres://alice") {
		t.Fatalf("chat context carries credentials: %q", text)
	}
	if !strings.Contains(text, "Database: PostgreSQL") {
		t.Fatalf("chat context lost the database context: %q", text)
	}
	// SQLite context carries the database line only — never credentials.
	model.chat.component.Target = "/data/chinook.db"
	text = model.chat.component.ContextText(chat.Context{Database: sharedsql.DatabaseInfo{Product: "SQLite", Version: "3.45"}})
	if strings.Contains(text, "secret-pw-9") || strings.Contains(text, "postgres://alice") {
		t.Fatalf("chat context carries credentials: %q", text)
	}
	if !strings.Contains(text, "Database: SQLite") {
		t.Fatalf("chat context lost the database context: %q", text)
	}
}

func TestNew_surfacesUndecryptableSecretsWithoutRewriting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := profile.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.Save(path, []profile.Profile{{
		Driver: profile.DriverPostgreSQL, Name: "Prod", Target: "db", Host: "h", Port: "5432", User: "u", Pass: "secret-pw-9",
	}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Tamper one byte of the envelope body (not the scope ID) so Load
	// must fail closed on the GCM authentication.
	marker := []byte(`"pass": "enc:v2:`)
	envelope := bytes.Index(before, marker)
	if envelope < 0 {
		t.Fatalf("no v2 envelope in %s", before)
	}
	bodyStart := envelope + len(marker)
	bodyEnd := bytes.IndexByte(before[bodyStart:], '"')
	if bodyEnd < 0 {
		t.Fatal("unterminated envelope")
	}
	tampered := append([]byte{}, before...)
	first := tampered[bodyStart]
	if first == 'A' {
		first = 'B'
	} else {
		first = 'A'
	}
	tampered[bodyStart] = first
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	model := New("", context.Background(), testOpen, false)
	if !strings.Contains(model.Status, "could not be decrypted") {
		t.Fatalf("status = %q, want the undecryptable-secret notice", model.Status)
	}
	if len(model.connection.component.Profiles) != 1 || model.connection.component.Profiles[0].Undecryptable["pass"] == "" {
		t.Fatalf("profiles = %#v, want the retained blob marked", model.connection.component.Profiles)
	}
	// New must never rewrite the file while a secret is undecryptable:
	// the tampered bytes (retained blob included) survive verbatim.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tampered, after) {
		t.Fatal("New rewrote the profiles file despite an undecryptable secret")
	}
}
