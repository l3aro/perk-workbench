package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
)

// restartRecorder returns a register callback that records every shim
// it accepts, like the CLI's database.RegisterShim but conflict-free.
func restartRecorder(registered *[]database.Shim) func(database.Shim) error {
	return func(shim database.Shim) error {
		*registered = append(*registered, shim)
		return nil
	}
}

// restartPluginName returns a driver name unique to one test run, so a
// restarted entry's database.ValidateShim can never collide with the
// global registrations other tests leave behind.
func restartPluginName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("restartkv%d", time.Now().UnixNano())
	t.Setenv("PERK_PLUGIN_NAME", name)
	t.Setenv("PERK_PLUGIN_TARGETS", name+":")
	return name
}

// TestStatuses_healthyCrashedClosed: statuses reflect the live child
// (identity, protocol version, pid, in-flight), the crashed child
// (reaped, exit status, terminal error), and the closed loader (final
// state retained). Status reads never disturb the child.
func TestStatuses_healthyCrashedClosed(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_execute")
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	entryText := "./plugin-status-child"
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// The entry resolves relative to the config directory; copy the test
	// binary next to the config so ./plugin-status-child resolves.
	dir := filepath.Dir(configPath)
	copyPlugin(t, filepath.Join(dir, "plugin-status-child"))

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{entryText}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	statuses := loader.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("Statuses = %d entries, want 1", len(statuses))
	}
	status := statuses[0]
	if status.Entry != entryText {
		t.Fatalf("status entry = %q, want the configured entry %q", status.Entry, entryText)
	}
	if status.Path != filepath.Join(dir, "plugin-status-child") {
		t.Fatalf("status path = %q, want the canonical path", status.Path)
	}
	if status.Plugin != "pluginkv" {
		t.Fatalf("status plugin = %q, want the handshake identity", status.Plugin)
	}
	if status.ProtocolVersion != ProtocolVersion {
		t.Fatalf("status protocol version = %d, want %d", status.ProtocolVersion, ProtocolVersion)
	}
	if status.Trusted || status.Fingerprint != "" {
		t.Fatalf("status trust = %t/%q, want unpinned", status.Trusted, status.Fingerprint)
	}
	if !status.Running || status.PID <= 0 {
		t.Fatalf("status = %+v, want the child running with a pid", status)
	}
	if status.ExitStatus != -1 {
		t.Fatalf("status exit status = %d, want -1 while running", status.ExitStatus)
	}
	if status.InitDuration <= 0 {
		t.Fatalf("status init duration = %v, want positive", status.InitDuration)
	}
	if status.Error != "" {
		t.Fatalf("status error = %q, want none while healthy", status.Error)
	}

	// In-flight count reflects a pending request: block_execute holds the
	// execute response until stdin closes.
	service, err := registered[0].Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.Execute(context.Background(), "select 1")
		done <- err
	}()
	waitForMarkerLines(t, marker, 1) // execute reached the plugin
	if got := loader.Statuses()[0].InFlight; got != 1 {
		t.Fatalf("status in-flight = %d, want 1 while the execute is pending", got)
	}

	// Close: the pending call is released, the child reaped, and the
	// final state stays inspectable. The close RPC itself cannot
	// complete against the stuck child, so Close's error is expected
	// (and bounded); the requirement is termination and a clean reap.
	_ = loader.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pending Execute not released by Loader.Close")
	}
	status = loader.Statuses()[0]
	if status.Running || status.PID != 0 {
		t.Fatalf("status after close = %+v, want the child reaped", status)
	}
	if status.ExitStatus != 0 {
		t.Fatalf("status exit status after close = %d, want 0", status.ExitStatus)
	}
	if status.InFlight != 0 {
		t.Fatalf("status in-flight after close = %d, want 0", status.InFlight)
	}
}

// TestStatuses_crashedChild: a child killed mid-session is reaped by the
// reader, and the status reports the exit state and terminal error.
func TestStatuses_crashedChild(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	client := loader.clients[0]
	pid := client.Snapshot().PID
	if pid <= 0 {
		t.Fatal("no child pid to kill")
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("killing plugin child: %v", err)
	}
	waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running })

	status := loader.Statuses()[0]
	if status.Running || status.PID != 0 {
		t.Fatalf("crashed status = %+v, want the child reaped", status)
	}
	if status.ExitStatus != -1 {
		t.Fatalf("crashed exit status = %d, want -1 for a signal kill", status.ExitStatus)
	}
	if status.Error == "" {
		t.Fatal("crashed status carries no terminal error")
	}
}

// TestStatuses_rejectedEntriesInConfigOrder: resolution failures and
// pin drift are retained as statuses in config order, with the entry
// identity, trust state, and failure text — and no child.
func TestStatuses_rejectedEntriesInConfigOrder(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-such-plugin")
	drifted := filepath.Join(dir, "drifted-plugin")
	copyPlugin(t, drifted)
	pin, err := SHA256File(drifted)
	if err != nil {
		t.Fatal(err)
	}
	// Drift the bytes before load: the pin no longer matches, so the
	// entry is refused at startup and no child ever spawns.
	if err := os.WriteFile(drifted, []byte("\n# drifted before load\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")

	loader, errs := LoadPinned(context.Background(), configPath, []string{missing, drifted},
		map[string]string{drifted: pin}, restartRecorder(&[]database.Shim{}))
	if len(errs) != 2 {
		t.Fatalf("Load errors = %v, want both rejections", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	statuses := loader.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses = %d entries, want 2", len(statuses))
	}
	if statuses[0].Entry != missing || statuses[0].Path != "" || statuses[0].Error == "" {
		t.Fatalf("missing-entry status = %+v, want the resolution failure retained", statuses[0])
	}
	if statuses[1].Entry != drifted || !statuses[1].Trusted || statuses[1].Fingerprint != pin {
		t.Fatalf("drifted status = %+v, want the pin retained", statuses[1])
	}
	if statuses[1].Error == "" || !strings.Contains(statuses[1].Error, "refusing to start") {
		t.Fatalf("drifted status error = %q, want the drift refusal", statuses[1].Error)
	}
	for _, status := range statuses {
		if status.Running || status.PID != 0 {
			t.Fatalf("rejected status = %+v, want no child", status)
		}
	}
}

// TestRestart_rejectedAtStartEntryRetainedAndRestartable: an entry
// rejected at the handshake stays inspectable and a later Restart —
// when the plugin speaks the right version — recovers it, installs the
// shim through the load-time register callback, and serves new sessions.
func TestRestart_rejectedAtStartEntryRetainedAndRestartable(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_PROTOCOL_VERSION", "2")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
	if len(errs) != 1 {
		t.Fatalf("Load errors = %v, want the version rejection", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	status := loader.Statuses()[0]
	if status.Error == "" || !strings.Contains(status.Error, "protocol version 2") {
		t.Fatalf("rejected status error = %q, want the version mismatch", status.Error)
	}
	if len(registered) != 0 {
		t.Fatalf("registered %d shims at load, want none", len(registered))
	}

	// The plugin now speaks the host version; restart recovers the entry.
	setEnv(t, "PERK_PLUGIN_PROTOCOL_VERSION", "1")
	if err := loader.Restart(context.Background(), executable); err != nil {
		t.Fatalf("Restart = %v, want nil", err)
	}
	status = loader.Statuses()[0]
	if status.Error != "" {
		t.Fatalf("restarted status error = %q, want cleared", status.Error)
	}
	if !status.Running || status.PID <= 0 {
		t.Fatalf("restarted status = %+v, want a running replacement", status)
	}
	if status.Plugin != "pluginkv" || status.ProtocolVersion != ProtocolVersion {
		t.Fatalf("restarted status = %+v, want the handshake identity and version", status)
	}
	if len(registered) != 1 {
		t.Fatalf("registered %d shims after restart, want 1", len(registered))
	}

	// The recovered shim serves new sessions.
	service, err := registered[0].Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open after restart: %v", err)
	}
	result, err := service.Execute(context.Background(), "select 1")
	if err != nil {
		t.Fatalf("Execute after restart: %v", err)
	}
	if len(result.Rows) != 1 || *result.Rows[0][0] != "widgets" {
		t.Fatalf("Execute after restart = %+v, want the plugin's widgets row", result)
	}
}

// TestRestart_pinDriftFailsClosedWithoutSpawn: a pin that no longer
// matches the bytes is refused before the replacement spawns — proven
// by a spawn log — and the running old child is untouched.
func TestRestart_pinDriftFailsClosedWithoutSpawn(t *testing.T) {
	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	script := filepath.Join(dir, "pinned-plugin")
	t.Setenv("PERK_HELPER_BINARY", os.Args[0])
	t.Setenv("PERK_PLUGIN_SPAWN_LOG", spawnLog)
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	body := "#!/bin/sh\necho spawned >> \"$PERK_PLUGIN_SPAWN_LOG\"\nexec \"$PERK_HELPER_BINARY\" -test.run=TestPluginHelperChild\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	pin, err := SHA256File(script)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")

	var registered []database.Shim
	loader, errs := LoadPinned(context.Background(), configPath, []string{script}, map[string]string{script: pin}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })
	pid := loader.clients[0].Snapshot().PID

	// Drift the pinned bytes; the child keeps running.
	if err := os.WriteFile(script, append([]byte(body), []byte("\n# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	err = loader.Restart(context.Background(), script)
	if err == nil || !strings.Contains(err.Error(), "pinned executable changed") {
		t.Fatalf("Restart error = %v, want the pinned-drift refusal", err)
	}
	contents, err := os.ReadFile(spawnLog)
	if err != nil {
		t.Fatalf("spawn log unreadable: %v", err)
	}
	if lines := strings.Count(string(contents), "spawned"); lines != 1 {
		t.Fatalf("spawn log has %d spawns, want exactly the load-time one (no restart spawn)", lines)
	}
	status := loader.Statuses()[0]
	if !status.Trusted || status.Fingerprint != pin {
		t.Fatalf("status trust = %t/%q, want the configured pin retained", status.Trusted, status.Fingerprint)
	}
	if !status.Running || status.PID != pid {
		t.Fatalf("status = %+v, want the old child still running untouched", status)
	}
	if status.Error == "" || !strings.Contains(status.Error, "pinned executable changed") {
		t.Fatalf("status error = %q, want the drift refusal", status.Error)
	}
}

// TestRestart_swapsFutureSessionsNotActiveGeneration: after a crash and
// restart, the pre-restart session keeps failing deterministically with
// the terminal error (never the replacement's responses), while fresh
// opens use the replacement and work.
func TestRestart_swapsFutureSessionsNotActiveGeneration(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	restartPluginName(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := registered[0].Open(context.Background(), "pluginkv:gen1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := service.Execute(context.Background(), "select 1"); err != nil {
		t.Fatalf("Execute before crash: %v", err)
	}

	// Crash the child out from under the session.
	oldClient := loader.clients[0]
	oldPID := oldClient.Snapshot().PID
	if err := syscall.Kill(oldPID, syscall.SIGKILL); err != nil {
		t.Fatalf("killing plugin child: %v", err)
	}
	waitForSnapshot(t, oldClient, func(s Snapshot) bool { return !s.Running })
	if _, err := service.Execute(context.Background(), "select 1"); err == nil {
		t.Fatal("Execute on the crashed session succeeded, want the terminal error")
	}

	if err := loader.Restart(context.Background(), executable); err != nil {
		t.Fatalf("Restart = %v, want nil", err)
	}
	status := loader.Statuses()[0]
	if !status.Running || status.PID == oldPID {
		t.Fatalf("restarted status = %+v, want a running replacement with a fresh pid", status)
	}
	if len(loader.clients) != 2 {
		t.Fatalf("loader tracks %d clients, want the old and the replacement", len(loader.clients))
	}

	// The old generation fails deterministically: terminal error, never
	// the replacement's response.
	_, err = service.Execute(context.Background(), "select 1")
	if err == nil {
		t.Fatal("old-generation Execute succeeded after restart, want a deterministic terminal error")
	}
	if !IsTerminal(err) {
		t.Fatalf("old-generation error = %v, want a terminal error", err)
	}
	if strings.Contains(err.Error(), "widgets") {
		t.Fatalf("old-generation error = %v, must never carry the replacement's response", err)
	}

	// New opens land on the replacement and work.
	replacement, err := registered[0].Open(context.Background(), "pluginkv:gen2")
	if err != nil {
		t.Fatalf("Open after restart: %v", err)
	}
	result, err := replacement.Execute(context.Background(), "select 1")
	if err != nil {
		t.Fatalf("Execute on the replacement: %v", err)
	}
	if len(result.Rows) != 1 || *result.Rows[0][0] != "widgets" {
		t.Fatalf("replacement Execute = %+v, want the widgets row", result)
	}

	// EntryForService follows generations: the old session is not the
	// current generation, the replacement is.
	if identifier, ok := loader.EntryForService(service); ok {
		t.Fatalf("EntryForService(old session) = %q, want no match", identifier)
	}
	identifier, ok := loader.EntryForService(replacement)
	if !ok || identifier != executable {
		t.Fatalf("EntryForService(replacement) = %q/%t, want the configured entry", identifier, ok)
	}
	if identifier, ok := loader.EntryForService(nil); ok || identifier != "" {
		t.Fatalf("EntryForService(nil) = %q/%t, want no match", identifier, ok)
	}

	// The old child is fully reaped.
	waitForSnapshot(t, oldClient, func(s Snapshot) bool { return !s.Running && s.PID == 0 })
}

// TestRestart_failureLeavesStatusUseful: a failed restart (handshake
// version drift, identity change, unknown identifier) reports the
// failure in the status while the old child stays untouched.
func TestRestart_failureLeavesStatusUseful(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	name := restartPluginName(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	load := func(t *testing.T) (*Loader, []database.Shim) {
		t.Helper()
		var registered []database.Shim
		loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
		if len(errs) != 0 {
			t.Fatalf("Load errors = %v, want none", errs)
		}
		t.Cleanup(func() { _ = loader.Close() })
		return loader, registered
	}

	t.Run("handshake drift", func(t *testing.T) {
		loader, _ := load(t)
		pid := loader.clients[0].Snapshot().PID
		setEnv(t, "PERK_PLUGIN_PROTOCOL_VERSION", "2")
		err := loader.Restart(context.Background(), executable)
		if err == nil || !strings.Contains(err.Error(), "protocol version 2") {
			t.Fatalf("Restart error = %v, want the version refusal", err)
		}
		status := loader.Statuses()[0]
		if !status.Running || status.PID != pid {
			t.Fatalf("status = %+v, want the old child untouched", status)
		}
		if !strings.Contains(status.Error, "protocol version 2") {
			t.Fatalf("status error = %q, want the restart failure", status.Error)
		}
	})

	t.Run("identity change", func(t *testing.T) {
		loader, _ := load(t)
		setEnv(t, "PERK_PLUGIN_NAME", name+"-other")
		err := loader.Restart(context.Background(), executable)
		if err == nil || !strings.Contains(err.Error(), "identity changed on restart") {
			t.Fatalf("Restart error = %v, want the identity refusal", err)
		}
		status := loader.Statuses()[0]
		if status.Plugin != name {
			t.Fatalf("status plugin = %q, want the registered identity %q retained", status.Plugin, name)
		}
		if !strings.Contains(status.Error, "identity changed") {
			t.Fatalf("status error = %q, want the restart failure", status.Error)
		}
	})

	t.Run("unknown identifier", func(t *testing.T) {
		loader, _ := load(t)
		err := loader.Restart(context.Background(), "no-such-entry")
		if err == nil || !strings.Contains(err.Error(), "no configured plugin entry") {
			t.Fatalf("Restart error = %v, want the unknown-entry refusal", err)
		}
	})
}

// TestRestart_registeredThroughDatabaseRegisterShim: the production
// registration path installs the driver globally; a restart must
// validate the replacement against the other drivers while tolerating
// the entry's own registration (which the replacement swaps in place),
// and the recovered driver serves new opens through database.Open.
func TestRestart_registeredThroughDatabaseRegisterShim(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	name := restartPluginName(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	loader, errs := Load(context.Background(), configPath, []string{executable}, database.RegisterShim)
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	opened, err := database.Open(context.Background(), name+":first")
	if err != nil {
		t.Fatalf("Open before restart: %v", err)
	}
	if _, err := opened.Service.Execute(context.Background(), "select 1"); err != nil {
		t.Fatalf("Execute before restart: %v", err)
	}

	// Crash the child, then restart through the global registration.
	client := loader.clients[0]
	if err := syscall.Kill(client.Snapshot().PID, syscall.SIGKILL); err != nil {
		t.Fatalf("killing plugin child: %v", err)
	}
	waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running })
	if err := loader.Restart(context.Background(), executable); err != nil {
		t.Fatalf("Restart = %v, want nil", err)
	}
	status := loader.Statuses()[0]
	if !status.Running || status.Error != "" {
		t.Fatalf("restarted status = %+v, want a running clean replacement", status)
	}

	// The recovered driver serves new opens; the old session generation
	// keeps failing deterministically.
	recovered, err := database.Open(context.Background(), name+":second")
	if err != nil {
		t.Fatalf("Open after restart: %v", err)
	}
	if _, err := recovered.Service.Execute(context.Background(), "select 1"); err != nil {
		t.Fatalf("Execute after restart: %v", err)
	}
	if _, err := opened.Service.Execute(context.Background(), "select 1"); err == nil {
		t.Fatal("old-generation Execute succeeded after restart, want a terminal error")
	}
}

// TestRestart_closeRaceWinsAndLeavesNoChild: Close racing an in-flight
// Restart aborts the restart at the swap point and reaps the
// replacement, so no child survives the loader.
func TestRestart_closeRaceWinsAndLeavesNoChild(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	restartPluginName(t)
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	t.Setenv("PERK_PLUGIN_INIT_DELAY_MS", "3000")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}

	restartErr := make(chan error, 1)
	go func() { restartErr <- loader.Restart(context.Background(), executable) }()
	waitForMarkerLines(t, marker, 1) // the replacement reached initialize

	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
	select {
	case err := <-restartErr:
		if err == nil {
			t.Fatal("Restart succeeded across Close, want the closed-loader refusal")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Restart did not return after Close")
	}

	// Every client ever spawned — including the aborted replacement —
	// is reaped: no child or goroutine leaks.
	for i, client := range loader.clients {
		select {
		case <-client.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("client %d was never reaped after Close", i)
		}
		if snap := client.Snapshot(); snap.Running || snap.PID != 0 {
			t.Fatalf("client %d snapshot = %+v, want the child reaped", i, snap)
		}
	}
	// Statuses stay inspectable after Close, including the entry whose
	// restart lost the race.
	if statuses := loader.Statuses(); len(statuses) != 1 {
		t.Fatalf("Statuses after Close = %d entries, want 1", len(statuses))
	}
}

// TestRestart_concurrentSameEntryAndStatuses: concurrent restarts of
// one entry are serialized, and Statuses stays safe throughout — the
// race detector guards the lock discipline.
func TestRestart_concurrentSameEntryAndStatuses(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	restartPluginName(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	var wg sync.WaitGroup
	restartErrs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			restartErrs <- loader.Restart(context.Background(), executable)
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				loader.Statuses()
			}
		}()
	}
	wg.Wait()
	close(restartErrs)
	for err := range restartErrs {
		if err != nil {
			t.Fatalf("concurrent Restart = %v, want nil", err)
		}
	}
	status := loader.Statuses()[0]
	if !status.Running || status.PID <= 0 {
		t.Fatalf("final status = %+v, want a running replacement", status)
	}
	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
	for i, client := range loader.clients {
		select {
		case <-client.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("client %d was never reaped after Close", i)
		}
	}
}

// TestStatuses_immutableCopies: mutating a returned status or its
// Stderr slice never affects the loader.
func TestStatuses_immutableCopies(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), []string{executable}, restartRecorder(&[]database.Shim{}))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	statuses := loader.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("Statuses = %d entries, want 1", len(statuses))
	}
	statuses[0].Entry = "tampered"
	statuses[0].Error = "tampered"
	statuses[0].Stderr = append(statuses[0].Stderr, "tampered")

	again := loader.Statuses()[0]
	if again.Entry == "tampered" || again.Error == "tampered" {
		t.Fatalf("status mutation leaked into the loader: %+v", again)
	}
	for _, line := range again.Stderr {
		if line == "tampered" {
			t.Fatalf("Stderr mutation leaked into the loader: %q", line)
		}
	}
}

// TestIsTerminal_markerSemantics: terminal failures carry the marker,
// operation errors and ordinary errors do not, and the marker preserves
// the wrapped text and unwraps to the original error.
func TestIsTerminal_markerSemantics(t *testing.T) {
	terminal := wrapTerminal(ioEOF)
	if !IsTerminal(terminal) {
		t.Fatal("wrapped terminal error not detected by IsTerminal")
	}
	if terminal.Error() != "EOF" {
		t.Fatalf("terminal error text = %q, want the wrapped text", terminal.Error())
	}
	if !errors.Is(terminal, ioEOF) {
		t.Fatal("errors.Is lost the wrapped error")
	}
	if IsTerminal(&Error{Method: methodExecute, Message: "boom", Code: -32000}) {
		t.Fatal("operation error detected as terminal")
	}
	if IsTerminal(errors.New("EOF")) {
		t.Fatal("plain error detected as terminal")
	}
	if wrapped := wrapTerminal(nil); wrapped != nil {
		t.Fatalf("wrapTerminal(nil) = %v, want nil", wrapped)
	}
	// errors.Join preserves the marker through joins.
	joined := errors.Join(terminal, errors.New("wait"))
	if !IsTerminal(joined) {
		t.Fatal("joined terminal error not detected")
	}
}

// TestIsTerminal_inFlightKill: a child killed while a call is pending
// fails the pending call with the terminal marker — an operation
// interrupted by child death is indistinguishable from a later call on
// the dead client, so callers can surface the recovery path either way.
func TestIsTerminal_inFlightKill(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_execute")
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, []string{executable}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := registered[0].Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.Execute(context.Background(), "select 1")
		done <- err
	}()
	waitForMarkerLines(t, marker, 1) // execute reached the plugin

	client := loader.clients[0]
	if err := syscall.Kill(client.Snapshot().PID, syscall.SIGKILL); err != nil {
		t.Fatalf("killing plugin child: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("in-flight Execute succeeded across the kill, want a terminal error")
		}
		if !IsTerminal(err) {
			t.Fatalf("in-flight Execute error = %v, want the terminal marker", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight Execute not released by the child kill")
	}
	waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running })
}

// ioEOF is the sentinel errors.Is unwraps to in the marker test.
var ioEOF = errors.New("EOF")
