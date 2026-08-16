package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// fakePluginControl is the injected PluginControl for plugin-manager
// tests: statuses served from memory, restart recorded, and a pluggable
// service-to-entry mapping.
type fakePluginControl struct {
	statuses   []plugin.Status
	restartErr error
	restarted  []string
	entryFor   func(sharedsql.Service) (string, bool)
}

func (f *fakePluginControl) Statuses() []plugin.Status { return f.statuses }

func (f *fakePluginControl) Restart(_ context.Context, identifier string) error {
	f.restarted = append(f.restarted, identifier)
	return f.restartErr
}

func (f *fakePluginControl) EntryForService(service sharedsql.Service) (string, bool) {
	if f.entryFor != nil {
		return f.entryFor(service)
	}
	return "", false
}

// fakePluginService is a minimal sharedsql.Service standing in for a
// plugin session: Close is observable (counted, with a scripted error)
// and Execute answers.
type fakePluginService struct {
	sharedsql.Service
	closed     bool
	closeCount int
	closeErr   error
	executed   int
}

func (s *fakePluginService) Close() error {
	s.closed = true
	s.closeCount++
	return s.closeErr
}

func (s *fakePluginService) Execute(context.Context, string) (sharedsql.Result, error) {
	s.executed++
	return sharedsql.Result{Columns: []string{"reply"}, Rows: [][]*string{{ptrString("pong")}}}, nil
}

func ptrString(value string) *string { return &value }

// unwrapFirstBatchCommand runs a status-writing update's command and
// returns the first batch element — the status write batches the real
// command with the notification popup command.
func unwrapFirstBatchCommand(t *testing.T, command tea.Cmd) tea.Cmd {
	t.Helper()
	switch msg := command().(type) {
	case tea.BatchMsg:
		if len(msg) == 0 {
			t.Fatal("empty command batch")
		}
		return msg[0]
	default:
		return func() tea.Msg { return msg }
	}
}

// openPluginManagerOn drives the command palette of an existing model to
// the plugins overlay.
func openPluginManagerOn(model Model) Model {
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl, Text: "p"})
	model = updated.(Model)
	for _, character := range "/plugins" {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // exit filtering
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // select
	return updated.(Model)
}

// openStatusView walks an open plugin manager to the status view.
func openStatusView(model Model) Model {
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Remove
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Status
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model)
}

// statusFixture is a fully populated healthy status.
func statusFixture() plugin.Status {
	return plugin.Status{
		Entry:           "./kv",
		Path:            "/abs/kv",
		Plugin:          "pluginkv",
		ProtocolVersion: 1,
		Trusted:         true,
		Fingerprint:     strings.Repeat("ab", 32),
		PID:             4242,
		Running:         true,
		ExitStatus:      -1,
		InitDuration:    12 * time.Millisecond,
		Stderr:          []string{"ready on stdio"},
	}
}

func TestPluginManager_statusViewRendersLiveFieldsAndRefreshes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	control := &fakePluginControl{statuses: []plugin.Status{statusFixture()}}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	manager := model.overlay.pluginManager
	if manager == nil || manager.view != "status" {
		t.Fatalf("manager = %+v, want the status view", manager)
	}
	content := model.pluginManagerContent()
	for _, want := range []string{
		"pluginkv (running)",
		"entry: ./kv",
		"path: /abs/kv",
		"name: pluginkv",
		"protocol: perk/v1 (1)",
		"trust: pinned (sha256 " + strings.Repeat("ab", 8) + "…)",
		"pid 4242",
		"initialize: 12ms",
		"in-flight: 0",
		"failure: none",
		"stderr (last 6):",
		"ready on stdio",
		"r: refresh",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("status content = %q, want it to contain %q", content, want)
		}
	}

	// Refresh is explicit: r re-reads the control.
	control.statuses[0].PID = 9999
	control.statuses[0].Stderr = []string{"reloaded"}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	content = model.pluginManagerContent()
	if !strings.Contains(content, "pid 9999") || !strings.Contains(content, "reloaded") {
		t.Fatalf("refreshed content = %q, want the fresh pid and stderr", content)
	}
}

func TestPluginManager_statusBoundedStderrAndCrashState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	status := statusFixture()
	status.Running = false
	status.PID = 0
	status.ExitStatus = 3
	status.Error = "EOF"
	status.Stderr = nil
	for i := range 10 {
		status.Stderr = append(status.Stderr, fmt.Sprintf("stderr line %d", i))
	}
	control := &fakePluginControl{statuses: []plugin.Status{status}}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	content := model.pluginManagerContent()
	if !strings.Contains(content, "pluginkv (crashed)") {
		t.Fatalf("status content = %q, want the crashed state label", content)
	}
	if !strings.Contains(content, "failure: EOF") {
		t.Fatalf("status content = %q, want the terminal failure", content)
	}
	if !strings.Contains(content, "stderr line 4") || !strings.Contains(content, "stderr line 9") {
		t.Fatalf("status content = %q, want the newest stderr lines", content)
	}
	if strings.Contains(content, "stderr line 0") || strings.Contains(content, "stderr line 3") {
		t.Fatalf("status content = %q, want the stderr display capped at the newest 6 lines", content)
	}
}

func TestPluginManager_statusRedactsCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const (
		secret = "secret-pw-9"
		target = "redis://user:" + secret + "@host:6379"
	)
	control := &fakePluginControl{statuses: []plugin.Status{{
		Entry:  "./kv",
		Path:   "/abs/kv",
		Error:  "connection failed with password " + secret,
		Stderr: []string{"dialing " + target + " failed; unrelated monitoring.example:8080 is fine"},
	}}}
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Values.Pass = secret
	model.SetPluginControl(control)
	model.connectionTarget = target
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	content := model.pluginManagerContent()
	for _, forbidden := range []string{secret, "redis://", "host:6379", "user:", "user:" + secret} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("status content leaks %q: %q", forbidden, content)
		}
	}
	if !strings.Contains(content, "[redacted]") {
		t.Fatalf("status content = %q, want the redaction marker", content)
	}
	// The operational failure text and unrelated hosts survive.
	if !strings.Contains(content, "connection failed") || !strings.Contains(content, "monitoring.example:8080") {
		t.Fatalf("status content = %q, lost operational or unrelated context", content)
	}
}

func TestPluginManager_statusEmptyWithoutControl(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	content := model.pluginManagerContent()
	if !strings.Contains(content, "no plugin status available") {
		t.Fatalf("status content = %q, want the unavailable notice", content)
	}
	// With no statuses there is nothing to confirm or restart.
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil || model.overlay.pluginManager.restartConfirm != nil {
		t.Fatal("enter on an empty status view opened a confirmation or ran a command")
	}
}

// TestPluginManager_restartConfirmationCancel: declining the restart
// confirmation never calls the control.
func TestPluginManager_restartConfirmationCancel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	control := &fakePluginControl{statuses: []plugin.Status{statusFixture()}}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.pluginManager.restartConfirm == nil {
		t.Fatal("enter did not open the restart confirmation")
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if command != nil || model.overlay.pluginManager.restartConfirm != nil {
		t.Fatal("declining the confirmation ran a command or kept the dialog")
	}
	if len(control.restarted) != 0 {
		t.Fatalf("declined restart called the control %d times", len(control.restarted))
	}
}

// TestPluginManager_restartUnrelatedRestartsOnlyTheChild: with no
// connection (or an unrelated one), confirming restarts the child and
// refreshes the status view without any reconnect.
func TestPluginManager_restartUnrelatedRestartsOnlyTheChild(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	control := &fakePluginControl{statuses: []plugin.Status{statusFixture()}}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if command == nil || !model.overlay.pluginManager.busy {
		t.Fatal("confirming did not start the async restart")
	}
	msg := command().(pluginRestartMsg)
	if msg.err != nil || msg.reconnect || msg.target != "" {
		t.Fatalf("restart message = %+v, want a child-only restart", msg)
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)
	if len(control.restarted) != 1 || control.restarted[0] != "./kv" {
		t.Fatalf("restarted = %v, want exactly the selected entry", control.restarted)
	}
	if model.Status != "plugin restarted" {
		t.Fatalf("status = %q, want the restart notice", model.Status)
	}
	if model.overlay.pluginManager == nil || model.overlay.pluginManager.view != "status" {
		t.Fatal("successful child restart closed the status view")
	}
	// The view refreshed with the control's fresh statuses.
	control.statuses[0].PID = 7777
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if !strings.Contains(model.pluginManagerContent(), "pid 7777") {
		t.Fatalf("refreshed content = %q, want the fresh pid", model.pluginManagerContent())
	}
}

// TestPluginManager_restartFailureLeavesStatusUseful: a failed restart
// shows the redacted failure in the status view and keeps the entry
// selectable.
func TestPluginManager_restartFailureLeavesStatusUseful(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	control := &fakePluginControl{
		statuses:   []plugin.Status{statusFixture()},
		restartErr: errors.New("pinned executable changed: expected aabb, got ccdd"),
	}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	msg := command().(pluginRestartMsg)
	if msg.err == nil {
		t.Fatal("restart message carried no error")
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)
	manager := model.overlay.pluginManager
	if manager == nil || manager.view != "status" {
		t.Fatalf("manager = %+v, want the status view retained", manager)
	}
	if !strings.Contains(model.pluginManagerContent(), "pinned executable changed") {
		t.Fatalf("status content = %q, want the restart failure", model.pluginManagerContent())
	}
}

// TestScrubPluginTarget_removesKnownTargetForms: the scrub removes the
// raw target, its credential-redacted URL form, the literal-marker form
// the redactor can leave behind, and the authority/hostname fragments —
// while unrelated hosts and the operational text survive, and non-URL
// targets are removed as a whole.
func TestScrubPluginTarget_removesKnownTargetForms(t *testing.T) {
	const (
		secret = "secret-pw-9"
		target = "redis://user:" + secret + "@db:6379/2"
	)
	tests := []struct {
		name string
		text string
		keep []string
	}{
		{
			name: "raw target form",
			text: "failed to connect to " + target + ": timeout",
			keep: []string{"failed to connect", "timeout"},
		},
		{
			name: "credential-redacted form after masking",
			text: "dialing redis://user:xxxxx@db:6379/2 refused",
			keep: []string{"dialing", "refused"},
		},
		{
			name: "literal marker form the redactor leaves behind",
			text: "dialing redis://user:[redacted]@db:6379/2 refused",
			keep: []string{"dialing", "refused"},
		},
		{
			name: "userinfo-stripped URL form",
			text: "dialing redis://db:6379/2 refused",
			keep: []string{"dialing", "refused"},
		},
		{
			name: "authority and hostname fragments",
			text: "dialing db:6379 via db refused",
			keep: []string{"dialing", "via", "refused"},
		},
		{
			name: "slash path of the known target",
			text: "the number /2 is where the data lives",
			keep: []string{"the number", "is where the data lives"},
		},
		{
			name: "unrelated paths survive",
			text: "check /other and /var/log/app.log",
			keep: []string{"/other", "/var/log/app.log"},
		},
		{
			name: "unrelated hosts survive",
			text: "check monitoring.example:8080 and backups.internal",
			keep: []string{"monitoring.example:8080", "backups.internal"},
		},
		{
			name: "bare path target removed as a whole",
			text: "opening /var/lib/app/db.sqlite failed",
			keep: []string{"opening", "failed"},
		},
		{
			name: "label-prefixed URL target",
			text: "dialing redis:redis://user:xxxxx@db:6379/2 refused",
			keep: []string{"dialing", "refused"},
		},
		{
			name: "blank target is a no-op",
			text: "some diagnostic text",
			keep: []string{"some diagnostic text"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := scrubPluginTarget(test.text, target)
			for _, forbidden := range []string{secret, "redis://", "db:6379", "user:", "user:" + secret, "/2"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("scrubbed text %q still contains %q", got, forbidden)
				}
			}
			for _, keep := range test.keep {
				if !strings.Contains(got, keep) {
					t.Fatalf("scrubbed text %q lost useful context %q", got, keep)
				}
			}
		})
	}

	// A bare path target is scrubbed as a whole; the marker shows where.
	pathTarget := "/var/lib/app/db.sqlite"
	if got := scrubPluginTarget("opening /var/lib/app/db.sqlite failed", pathTarget); strings.Contains(got, "db.sqlite") || !strings.Contains(got, "[redacted]") {
		t.Fatalf("path target scrub = %q, want the whole target replaced", got)
	}
	if got := scrubPluginTarget("opening /var/lib/app/db.sqlite failed", ""); got != "opening /var/lib/app/db.sqlite failed" {
		t.Fatalf("blank target scrub = %q, want the text untouched", got)
	}

	// The known path's percent-encoded and decoded renderings are both
	// scrubbed — inside the URL and as the exact slash-prefixed fragment —
	// while unrelated paths survive exact matching.
	encoded := "redis://user:secret-pw-9@db:6379/tenant%2Fprod"
	for _, text := range []string{
		"dialing redis://user:xxxxx@db:6379/tenant%2Fprod refused",
		"dialing redis://user:xxxxx@db:6379/tenant/prod refused",
		"dialing redis://user:[redacted]@db:6379/tenant/prod refused",
		"dialing redis://db:6379/tenant%2Fprod refused",
		"refusing /tenant%2Fprod",
		"refusing /tenant/prod",
	} {
		got := scrubPluginTarget(text, encoded)
		for _, forbidden := range []string{"/tenant%2Fprod", "/tenant/prod", "redis://", "db:6379", "user:", "user:" + secret} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("scrub of %q left %q in %q", text, forbidden, got)
			}
		}
	}
	if got := scrubPluginTarget("check /other and /var/log/app.log", encoded); !strings.Contains(got, "/other") || !strings.Contains(got, "/var/log/app.log") {
		t.Fatalf("unrelated paths were scrubbed: %q", got)
	}
}

// TestPluginManager_restartFailureKeepsHealthyConnectionUsable: a
// forced restart failure on a healthy current connection must not close
// the service or mutate model state — the connection stays query-usable
// and the overlay shows the failure.
func TestPluginManager_restartFailureKeepsHealthyConnectionUsable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	control := &fakePluginControl{
		statuses:   []plugin.Status{statusFixture()},
		restartErr: errors.New("pinned executable changed: expected aabb, got ccdd"),
		entryFor:   func(service sharedsql.Service) (string, bool) { return "./kv", true },
	}
	service := &fakePluginService{}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model.Opened("whatever", service, "")
	model.connectionTarget = "pluginkv:whatever"
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	msg := command().(pluginRestartMsg)
	if msg.err == nil {
		t.Fatal("restart message carried no error")
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)

	if service.closed || service.closeCount != 0 {
		t.Fatalf("failed restart closed the healthy service (%d closes)", service.closeCount)
	}
	if model.Database != service || model.State != stateReady {
		t.Fatalf("model = state %v, want the untouched ready state with the same service", model.State)
	}
	if _, err := model.Database.Execute(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("healthy connection no longer usable after the failed restart: %v", err)
	}
	if model.overlay.pluginManager == nil || !strings.Contains(model.pluginManagerContent(), "pinned executable changed") {
		t.Fatalf("overlay = %+v, want the failure shown in the status view", model.overlay.pluginManager)
	}
}

// TestPluginManager_restartFailureLeavesCrashedConnectionUnclosed: a
// failed restart on a crashed current connection must not additionally
// close the dead service — it stays the model's service, still
// actionable, and the overlay shows the failure.
func TestPluginManager_restartFailureLeavesCrashedConnectionUnclosed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	crashed := statusFixture()
	crashed.Running = false
	crashed.PID = 0
	crashed.ExitStatus = 3
	crashed.Error = "EOF"
	control := &fakePluginControl{
		statuses:   []plugin.Status{crashed},
		restartErr: errors.New("pinned executable changed: expected aabb, got ccdd"),
		entryFor:   func(service sharedsql.Service) (string, bool) { return "./kv", true },
	}
	service := &fakePluginService{}
	model := New("", context.Background(), testOpen, false)
	model.SetPluginControl(control)
	model.Opened("whatever", service, "")
	model.connectionTarget = "pluginkv:whatever"
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	msg := command().(pluginRestartMsg)
	if msg.err == nil {
		t.Fatal("restart message carried no error")
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)

	if service.closed || service.closeCount != 0 {
		t.Fatalf("failed restart closed the dead service from the UI (%d closes)", service.closeCount)
	}
	if model.Database != service {
		t.Fatal("model lost the crashed service")
	}
	manager := model.overlay.pluginManager
	if manager == nil || manager.view != "status" {
		t.Fatalf("manager = %+v, want the status view retained", manager)
	}
	if !strings.Contains(model.pluginManagerContent(), "pinned executable changed") {
		t.Fatalf("status content = %q, want the restart failure", model.pluginManagerContent())
	}
}

// TestPluginManager_restartFailureRedactsCredentials: restart failure
// text passes through the credential redactor and the target scrubber
// before it reaches the overlay: neither the raw URL, its
// credential-redacted URL form, nor the target's host fragments may
// survive, while unrelated diagnostic context stays understandable.
func TestPluginManager_restartFailureRedactsCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const (
		secret = "secret-pw-9"
		target = "redis://user:" + secret + "@db:6379/2"
	)
	control := &fakePluginControl{
		statuses: []plugin.Status{statusFixture()},
		// The failure text carries the target in raw, redacted-URL, and
		// fragment forms, plus an unrelated host that must survive.
		restartErr: errors.New("failed to connect to " + target + ": timeout; check monitoring.example:8080"),
		entryFor:   func(service sharedsql.Service) (string, bool) { return "./kv", true },
	}
	service := &fakePluginService{}
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Values.Pass = secret
	model.SetPluginControl(control)
	model.Opened("2", service, "")
	model.connectionTarget = target
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	msg := command().(pluginRestartMsg)
	if msg.err == nil {
		t.Fatal("restart message carried no error")
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)

	content := model.pluginManagerContent()
	for _, forbidden := range []string{
		secret,           // the credential value
		"redis://",       // the URL scheme
		"db:6379",        // the target authority
		"/2",             // the target path
		"user:",          // the target username
		"user:" + secret, // the raw userinfo
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("restart failure leaks %q: %q", forbidden, content)
		}
	}
	if !strings.Contains(content, "[redacted]") {
		t.Fatalf("restart failure content = %q, want the redaction marker", content)
	}
	// The operational failure stays understandable and unrelated context
	// survives.
	if !strings.Contains(content, "failed to connect") || !strings.Contains(content, "timeout") {
		t.Fatalf("restart failure content = %q, lost the operational failure text", content)
	}
	if !strings.Contains(content, "monitoring.example:8080") {
		t.Fatalf("restart failure content = %q, lost unrelated diagnostic context", content)
	}
}

// TestPluginManager_restartReconnectsCrashedCurrentSession: an entry
// backing the current connection closes the dead service, restarts the
// child, and reconnects the same target through the normal async open
// path; schema/browse/query work again on the recovered session.
func TestPluginManager_restartReconnectsCrashedCurrentSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	crashed := statusFixture()
	crashed.Running = false
	crashed.PID = 0
	crashed.ExitStatus = 3
	crashed.Error = "EOF"
	service := &fakePluginService{closeErr: &plugin.TerminalError{Err: errors.New("perk/v1: client closed")}}
	open := func(ctx context.Context, target string) (sharedsql.Opened, error) {
		return sharedsql.Opened{
			Target:        target,
			Service:       service,
			Info:          sharedsql.DatabaseInfo{Product: "PluginKV", Version: "1"},
			Objects:       []sharedsql.SchemaObject{{Database: "pluginkv", Type: "database", Name: "pluginkv"}, {Database: "pluginkv", Type: "collection", Name: "widgets"}},
			QueryLanguage: sharedsql.SQLQueryLanguage,
		}, nil
	}
	control := &fakePluginControl{
		statuses: []plugin.Status{crashed},
		entryFor: func(service sharedsql.Service) (string, bool) { return "./kv", true },
	}
	model := New("", context.Background(), open, false)
	model.SetPluginControl(control)
	model.Opened("whatever", service, "")
	model.connectionTarget = "pluginkv:whatever"
	model = resizeModel(model, 100, 24)
	model = openPluginManagerOn(model)
	model = openStatusView(model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	msg := command().(pluginRestartMsg)
	if msg.err != nil {
		t.Fatalf("restart error = %v, want the terminal close error never to fail the recovery", msg.err)
	}
	if !msg.reconnect || msg.target != "pluginkv:whatever" {
		t.Fatalf("restart message = %+v, want a reconnect to the same target", msg)
	}
	// The old-generation service was closed exactly once by the restart
	// flow — after the restart succeeded, never before.
	if service.closeCount != 1 {
		t.Fatalf("old service closed %d times, want exactly once", service.closeCount)
	}
	if len(control.restarted) != 1 || control.restarted[0] != "./kv" {
		t.Fatalf("restarted = %v, want the backing entry", control.restarted)
	}
	updated, command = model.Update(msg)
	model = updated.(Model)
	if model.overlay.pluginManager != nil {
		t.Fatal("reconnect left the plugin manager open")
	}
	if model.Status != "plugin restarted; reconnecting" {
		t.Fatalf("status = %q, want the reconnecting notice", model.Status)
	}

	// The reconnect goes through the normal async open path and lands
	// ready: schema, browse, and query all work again. The status write
	// wraps the open command in a batch with the notification popup.
	if command == nil {
		t.Fatal("restart success issued no reconnect command")
	}
	opened := unwrapFirstBatchCommand(t, command)().(databaseOpenedMsg)
	if opened.err != nil || opened.target != "pluginkv:whatever" {
		t.Fatalf("reopen = %+v, want the same target", opened)
	}
	updated, _ = model.Update(opened)
	model = updated.(Model)
	if model.State != stateReady || model.Database == nil {
		t.Fatalf("model state = %v, want ready with a recovered session", model.State)
	}
	if got := model.schema.component.Objects; len(got) != 2 || got[1].Name != "widgets" {
		t.Fatalf("schema objects = %+v, want the plugin's collections", got)
	}
	if _, err := model.Database.Execute(context.Background(), "PING"); err != nil {
		t.Fatalf("Execute on the recovered session: %v", err)
	}
	if service.executed == 0 {
		t.Fatal("the recovered session never served a query")
	}
}

// TestQueryFailure_pluginStoppedCTA: an operation failing because the
// plugin child exited surfaces the actionable recovery status while the
// original error stays in the query-log detail.
func TestQueryFailure_pluginStoppedCTA(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := readyModel(t)
	model.setStatus("before")
	requestID := model.StartQueryForTest(context.Background())
	terminal := &plugin.TerminalError{Err: errors.New("EOF")}
	updated, _ := model.Update(queryFailedMsg{requestID: requestID, statement: "SELECT 1", startedAt: time.Now(), err: terminal})
	model = updated.(Model)
	if model.Status != pluginStoppedCTA {
		t.Fatalf("status = %q, want the plugin-stopped call to action %q", model.Status, pluginStoppedCTA)
	}
	entries := model.queryLog.component.Entries
	if len(entries) != 1 || entries[0].Message != "EOF" || entries[0].Status != "failed" {
		t.Fatalf("query log entry = %+v, want the original terminal error preserved", entries)
	}
}

// TestQueryFailure_operationErrorNoCTA: structured operation errors and
// ordinary failures keep the existing behavior — no plugin-stopped
// action, no status change on the plain query path.
func TestQueryFailure_operationErrorNoCTA(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := readyModel(t)
	requestID := model.StartQueryForTest(context.Background())
	operation := &plugin.Error{Code: -32000, Message: "boom", Kind: plugin.KindOperation, Method: "perk/v1/execute", Plugin: "pluginkv"}
	updated, _ := model.Update(queryFailedMsg{requestID: requestID, statement: "SELECT 1", startedAt: time.Now(), err: operation})
	model = updated.(Model)
	// StartQuery leaves "running query"; the failure handler must not
	// overwrite it with the plugin-stopped action.
	if model.Status != "running query" {
		t.Fatalf("status = %q, want unchanged for an operation error", model.Status)
	}
	if got := model.queryLog.component.Entries[0].Message; got != "perk/v1/execute: boom (code -32000)" {
		t.Fatalf("query log message = %q, want the structured error text", got)
	}

	requestID = model.StartQueryForTest(context.Background())
	updated, _ = model.Update(queryFailedMsg{requestID: requestID, statement: "SELECT 1", startedAt: time.Now(), err: errors.New("plain failure")})
	model = updated.(Model)
	if model.Status != "running query" {
		t.Fatalf("status = %q, want unchanged for a plain failure", model.Status)
	}
}

func TestPluginFailureStatus_terminalVsOperation(t *testing.T) {
	if got := pluginFailureStatus(&plugin.TerminalError{Err: errors.New("EOF")}, "fallback"); got != pluginStoppedCTA {
		t.Fatalf("terminal failure status = %q, want the call to action", got)
	}
	if got := pluginFailureStatus(errors.New("boom"), "fallback"); got != "fallback" {
		t.Fatalf("ordinary failure status = %q, want the fallback text", got)
	}
	if got := pluginFailureStatus(nil, "fallback"); got != "fallback" {
		t.Fatalf("nil failure status = %q, want the fallback text", got)
	}
}
