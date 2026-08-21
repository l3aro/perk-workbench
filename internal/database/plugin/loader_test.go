package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestLoad_configRelativeResolutionAndRegistration: an entry with a path
// separator resolves relative to the config file's directory.
func TestLoad_configRelativeResolutionAndRegistration(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyPlugin(t, filepath.Join(cfgDir, "plugin-child"))
	configPath := filepath.Join(cfgDir, "config.json")

	var shims []database.Shim
	loader, errs := Load(context.Background(), configPath, testEntries("./plugin-child"), func(shim database.Shim) error {
		shims = append(shims, shim)
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })
	if len(shims) != 1 {
		t.Fatalf("registered %d shims, want 1", len(shims))
	}
	if caps := shims[0].Capabilities(); caps.Name != "pluginkv" {
		t.Fatalf("caps.Name = %q, want pluginkv", caps.Name)
	}
	target, ok := shims[0].BuildTarget(database.FormValues{Host: "svc"})
	if !ok || target != "pluginkv:svc" {
		t.Fatalf("BuildTarget = %q/%t, want the canned pluginkv target", target, ok)
	}
	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
}

// TestLoad_bareNameLookPath: a bare entry resolves through PATH (a
// symlink here); a PATH hit that is not a plugin (true) fails the
// handshake with one nonfatal error and no registration.
func TestLoad_bareNameLookPath(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(executable, filepath.Join(dir, "pluginkv-child")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var shims []database.Shim
	loader, errs := Load(context.Background(), filepath.Join(dir, "config.json"), testEntries("pluginkv-child", "true"), func(shim database.Shim) error {
		shims = append(shims, shim)
		return nil
	})
	t.Cleanup(func() { _ = loader.Close() })
	if len(shims) != 1 {
		t.Fatalf("registered %d shims, want 1", len(shims))
	}
	if len(errs) != 1 {
		t.Fatalf("Load errors = %v, want exactly the true handshake failure", errs)
	}
	if !strings.Contains(errs[0].Error(), "true") {
		t.Fatalf("error = %v, want it to name the true entry", errs[0])
	}
}

// TestLoad_rejectsMissingAndNonExecutable: missing paths, directories,
// and non-executable files each produce one nonfatal error; nothing is
// registered.
func TestLoad_rejectsMissingAndNonExecutable(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "noexec"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")

	var shims []database.Shim
	loader, errs := Load(context.Background(), configPath, testEntries("./missing", "./adir", "./noexec"), func(shim database.Shim) error {
		shims = append(shims, shim)
		return nil
	})
	if len(errs) != 3 {
		t.Fatalf("Load errors = %v, want one per rejected entry", errs)
	}
	if len(shims) != 0 {
		t.Fatalf("registered %d shims, want none", len(shims))
	}
	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
}

// TestLoad_duplicateDriverAndOverlap: same-name plugins reject the second
// registration; cross-driver target prefixes reject the second driver;
// the same canonical path listed twice registers once.
func TestLoad_duplicateDriverAndOverlap(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	dir := t.TempDir()
	copyA := filepath.Join(dir, "a-plugin")
	copyB := filepath.Join(dir, "b-plugin")
	copyPlugin(t, copyA)
	copyPlugin(t, copyB)
	configPath := filepath.Join(dir, "config.json")

	registerRecorder := func(registered *[]database.Shim) func(database.Shim) error {
		return func(shim database.Shim) error {
			if err := database.RegisterShim(shim); err != nil {
				return err
			}
			*registered = append(*registered, shim)
			return nil
		}
	}

	// Duplicate driver name: the first entry registers, the second is
	// rejected with one nonfatal error. The target dup: stays clear of
	// every other test driver.
	setEnv(t, "PERK_PLUGIN_NAME", "pluginkvdup")
	setEnv(t, "PERK_PLUGIN_TARGETS", "dup:")
	var registered []database.Shim
	loader, errs := Load(context.Background(), configPath, testEntries(copyA, copyB), registerRecorder(&registered))
	if len(errs) != 1 {
		t.Fatalf("duplicate-name Load errors = %v, want exactly one", errs)
	}
	if len(registered) != 1 {
		t.Fatalf("duplicate-name Load registered %d drivers, want 1", len(registered))
	}
	_ = loader.Close()

	// Target overlap: the first copy owns alpha:, the second's alpha:b
	// must be rejected (prefix overlap), even under a fresh driver name.
	setEnv(t, "PERK_PLUGIN_NAME", "pluginkvo")
	setEnv(t, "PERK_PLUGIN_TARGETS", "alpha:")
	registered = nil
	loader, errs = Load(context.Background(), configPath, testEntries(copyA), registerRecorder(&registered))
	if len(errs) != 0 || len(registered) != 1 {
		t.Fatalf("first overlap Load: errs = %v, registered = %d, want none and one", errs, len(registered))
	}
	_ = loader.Close()

	setEnv(t, "PERK_PLUGIN_NAME", "pluginkvo2")
	setEnv(t, "PERK_PLUGIN_TARGETS", "alpha:b")
	registered = nil
	loader, errs = Load(context.Background(), configPath, testEntries(copyB), registerRecorder(&registered))
	if len(errs) != 1 {
		t.Fatalf("overlap Load errors = %v, want exactly one", errs)
	}
	if len(registered) != 0 {
		t.Fatalf("overlap Load registered %d drivers, want none", len(registered))
	}
	if !strings.Contains(errs[0].Error(), "alpha:b") || !strings.Contains(errs[0].Error(), "alpha:") {
		t.Fatalf("overlap error = %v, want it to mention both prefixes", errs[0])
	}
	_ = loader.Close()

	// Dedupe: the same canonical path listed twice registers once.
	setEnv(t, "PERK_PLUGIN_NAME", "pluginkvd")
	setEnv(t, "PERK_PLUGIN_TARGETS", "dupz:")
	registered = nil
	loader, errs = Load(context.Background(), configPath, testEntries(copyA, copyA), registerRecorder(&registered))
	if len(errs) != 0 || len(registered) != 1 {
		t.Fatalf("dedupe Load: errs = %v, registered = %d, want none and one", errs, len(registered))
	}
	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
}

// TestLoad_wrongVersion: a plugin speaking a different protocol version
// is rejected before registration.
func TestLoad_wrongVersion(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_PROTOCOL_VERSION", "2")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shims []database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(shim database.Shim) error {
		shims = append(shims, shim)
		return nil
	})
	t.Cleanup(func() { _ = loader.Close() })
	if len(errs) != 1 {
		t.Fatalf("Load errors = %v, want exactly one", errs)
	}
	if len(shims) != 0 {
		t.Fatalf("registered %d shims, want none", len(shims))
	}
	if !strings.Contains(errs[0].Error(), "protocol version 2") {
		t.Fatalf("error = %v, want it to name the protocol version", errs[0])
	}
}

// TestLoad_incompatibleHandshake: terminal initialize failures reject the
// entry with one nonfatal error, no registration, and a safe idempotent
// Close.
func TestLoad_incompatibleHandshake(t *testing.T) {
	for _, behavior := range []string{"malformed", "oversized", "wrong_id", "duplicate"} {
		t.Run(behavior, func(t *testing.T) {
			t.Setenv("PERK_PLUGIN_HELPER", "1")
			t.Setenv("PERK_PLUGIN_BEHAVIOR", behavior)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			var shims []database.Shim
			loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(shim database.Shim) error {
				shims = append(shims, shim)
				return nil
			})
			if len(errs) != 1 {
				t.Fatalf("Load errors = %v, want exactly one", errs)
			}
			if len(shims) != 0 {
				t.Fatalf("registered %d shims, want none", len(shims))
			}
			_ = loader.Close() // must not panic
			_ = loader.Close() // idempotent
		})
	}
}

// TestLoad_databaseOpenSemantics exercises the full path: Match, shim
// Open, the session proxy, and the loader lifecycle.
func TestLoad_databaseOpenSemantics(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return database.RegisterShim(s)
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	loader.mu.Lock()
	client := loader.clients[0]
	loader.mu.Unlock()

	opened, err := database.Open(context.Background(), "pluginkv:whatever")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Target != "whatever" {
		t.Fatalf("opened.Target = %q, want the stripped target", opened.Target)
	}
	if opened.Info.Product != "PluginKV" {
		t.Fatalf("opened.Info.Product = %q, want PluginKV", opened.Info.Product)
	}
	if len(opened.Objects) != 2 ||
		opened.Objects[0] != (sharedsql.SchemaObject{Database: "pluginkv", Type: "database", Name: "pluginkv"}) ||
		opened.Objects[1].Name != "widgets" {
		t.Fatalf("opened.Objects = %+v, want the synthesized pluginkv root then the widgets collection", opened.Objects)
	}

	service := opened.Service
	if info := service.Info(); info != opened.Info {
		t.Fatalf("service.Info() = %+v, want the cached open info %+v", info, opened.Info)
	}
	result, err := service.Execute(context.Background(), "select 1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "name" {
		t.Fatalf("Execute columns = %v, want [name]", result.Columns)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0] == nil || *result.Rows[0][0] != "widgets" {
		t.Fatalf("Execute rows = %v, want [[widgets]]", result.Rows)
	}

	target, ok := shim.BuildTarget(database.FormValues{Host: "svc"})
	if !ok || target != "pluginkv:svc" {
		t.Fatalf("BuildTarget = %q/%t, want the canned target", target, ok)
	}

	if err := service.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil (idempotent)", err)
	}

	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
	select {
	case <-client.done:
	case <-time.After(5 * time.Second):
		t.Fatal("plugin child did not exit after Loader.Close")
	}
}

// TestLoad_childExitDuringRequest: a child that dies mid-request fails
// the call, makes the client unusable, releases every pending call, and
// keeps Loader.Close safe.
func TestLoad_childExitDuringRequest(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "exit_on_execute")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := service.Execute(context.Background(), "select 1"); err == nil {
		t.Fatal("Execute succeeded, want the child-exit error")
	}
	if err := service.Validate(context.Background(), "select 1"); err == nil {
		t.Fatal("Validate after child exit succeeded, want an immediate error")
	}
	_ = loader.Close() // must not panic or hang
}

// TestLoad_loaderCloseWithPendingCall: Loader.Close while an execute is
// still in flight on a live session releases the pending call with an
// error and finishes without hanging (the stuck close RPC is bounded by
// the session's 5-second Close, then stdin closes and the child exits).
func TestLoad_loaderCloseWithPendingCall(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_execute")
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	callErr := make(chan error, 1)
	go func() {
		_, err := service.Execute(context.Background(), "select 1")
		callErr <- err
	}()
	waitForMarkerLines(t, marker, 1) // execute reached the plugin

	closed := make(chan error, 1)
	go func() { closed <- loader.Close() }()

	select {
	case err := <-callErr:
		if err == nil {
			t.Fatal("pending Execute succeeded across Loader.Close, want an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pending Execute not released by Loader.Close")
	}
	select {
	case <-closed:
		// The close RPC cannot complete: the child is stuck in
		// block_execute and exits only when stdin closes, so an error is
		// expected; the requirement is that Close terminates.
	case <-time.After(15 * time.Second):
		t.Fatal("Loader.Close hung with a pending call")
	}
}

// TestProxy_canceledExecute: canceling the caller's context notifies the
// plugin with the original request id and the call returns
// context.Canceled.
func TestProxy_canceledExecute(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_execute")
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var execErr error
	go func() {
		defer close(done)
		_, execErr = service.Execute(ctx, "select 1")
	}()
	waitForMarkerLines(t, marker, 1) // execute reached the plugin
	cancel()
	waitForMarkerLines(t, marker, 2) // cancel notification reached the plugin
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return after cancel")
	}
	if !errors.Is(execErr, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", execErr)
	}
	lines := waitForMarkerLines(t, marker, 2)
	if executeID := strings.TrimPrefix(lines[0], "execute "); executeID == lines[0] {
		t.Fatalf("marker line %q, want execute <id>", lines[0])
	} else if cancelID := strings.TrimPrefix(lines[1], "cancel "); cancelID == lines[1] {
		t.Fatalf("marker line %q, want cancel <id>", lines[1])
	} else if executeID != cancelID {
		t.Fatalf("cancel id %q does not match execute id %q", cancelID, executeID)
	}
}

// TestProxy_concurrentOutOfOrder: concurrent calls with out-of-order
// responses are routed to the right pending call.
func TestProxy_concurrentOutOfOrder(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "out_of_order")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var (
		wg        sync.WaitGroup
		execRes   sharedsql.Result
		execErr   error
		validErr  error
		objects   []sharedsql.SchemaObject
		schemaErr error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		execRes, execErr = service.Execute(context.Background(), "select 1")
	}()
	go func() {
		defer wg.Done()
		validErr = service.Validate(context.Background(), "select 1")
	}()
	go func() {
		defer wg.Done()
		objects, schemaErr = service.ListSchema(context.Background())
	}()
	wg.Wait()

	if execErr != nil {
		t.Fatalf("Execute error = %v", execErr)
	}
	if len(execRes.Columns) != 1 || execRes.Columns[0] != "EXECUTED" {
		t.Fatalf("Execute columns = %v, want the late distinctive [EXECUTED]", execRes.Columns)
	}
	if validErr != nil {
		t.Fatalf("Validate error = %v", validErr)
	}
	if schemaErr != nil {
		t.Fatalf("ListSchema error = %v", schemaErr)
	}
	if len(objects) != 2 ||
		objects[0] != (sharedsql.SchemaObject{Database: "pluginkv", Type: "database", Name: "pluginkv"}) ||
		objects[1].Name != "LISTS" {
		t.Fatalf("ListSchema = %+v, want the synthesized pluginkv root then the immediate distinctive LISTS object", objects)
	}
}

// TestLoad_queryLanguageNormalization exercises the full wire path: an
// omitted query_language becomes the legacy SQL default on the
// registered spec, a valid advertisement passes through unchanged, and
// invalid nonzero metadata rejects the entry.
func TestLoad_queryLanguageNormalization(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	registerRecorder := func(registered *[]database.Shim) func(database.Shim) error {
		return func(shim database.Shim) error {
			if err := database.RegisterShim(shim); err != nil {
				return err
			}
			*registered = append(*registered, shim)
			return nil
		}
	}

	// Omitted advertisement: the wire carries no query_language, and the
	// registered driver gets the legacy SQL default.
	setEnv(t, "PERK_PLUGIN_NAME", "qlomit")
	setEnv(t, "PERK_PLUGIN_TARGETS", "qlomit:")
	var shims []database.Shim
	loader, errs := Load(context.Background(), configPath, testEntries(executable), registerRecorder(&shims))
	if len(errs) != 0 {
		t.Fatalf("omitted-advertisement Load errors = %v, want none", errs)
	}
	if len(shims) != 1 {
		t.Fatalf("registered %d shims, want 1", len(shims))
	}
	if got := shims[0].Capabilities().QueryLanguage; got != nil {
		t.Fatalf("wire query_language = %+v, want nil (not advertised)", got)
	}
	spec, ok := database.ByName("qlomit")
	if !ok {
		t.Fatal("qlomit not registered")
	}
	if !reflect.DeepEqual(spec.QueryLanguage, database.SQLQueryLanguage) {
		t.Fatalf("qlomit query language = %+v, want the SQL default", spec.QueryLanguage)
	}
	_ = loader.Close()

	// A valid advertisement passes through to the registered spec,
	// including the optional static command catalog.
	setEnv(t, "PERK_PLUGIN_NAME", "qlpass")
	setEnv(t, "PERK_PLUGIN_TARGETS", "qlpass:")
	setEnv(t, "PERK_PLUGIN_QUERY_LANGUAGE",
		`{"name":"QL","editor_label":"Command","placeholder":"Enter a statement…","lexer":"plaintext","examples":["GET k","SET k v"],"commands":[{"name":"GET","usage":"GET key","summary":"Get the value at key"},{"name":"SET","usage":"SET key value","summary":"Set the value at key"}]}`)
	shims = nil
	loader, errs = Load(context.Background(), configPath, testEntries(executable), registerRecorder(&shims))
	if len(errs) != 0 {
		t.Fatalf("valid-advertisement Load errors = %v, want none", errs)
	}
	_ = loader.Close()
	want := database.QueryLanguage{
		Name:        "QL",
		EditorLabel: "Command",
		Placeholder: "Enter a statement…",
		Lexer:       "plaintext",
		Examples:    []string{"GET k", "SET k v"},
		Commands: []database.QueryCommand{
			{Name: "GET", Usage: "GET key", Summary: "Get the value at key"},
			{Name: "SET", Usage: "SET key value", Summary: "Set the value at key"},
		},
	}
	if spec, ok := database.ByName("qlpass"); !ok {
		t.Fatal("qlpass not registered")
	} else if !reflect.DeepEqual(spec.QueryLanguage, want) {
		t.Fatalf("qlpass query language = %+v, want %+v", spec.QueryLanguage, want)
	}

	// Invalid nonzero metadata rejects the entry, never silently
	// defaulting it — including an invalid command catalog.
	setEnv(t, "PERK_PLUGIN_NAME", "qlbad")
	setEnv(t, "PERK_PLUGIN_TARGETS", "qlbad:")
	setEnv(t, "PERK_PLUGIN_QUERY_LANGUAGE", `{"name":"QL","placeholder":"Enter a statement…"}`)
	shims = nil
	loader, errs = Load(context.Background(), configPath, testEntries(executable), registerRecorder(&shims))
	if len(errs) != 1 {
		t.Fatalf("invalid-advertisement Load errors = %v, want exactly one", errs)
	}
	if len(shims) != 0 {
		t.Fatalf("invalid-advertisement Load registered %d drivers, want none", len(shims))
	}
	if _, ok := database.ByName("qlbad"); ok {
		t.Fatal("qlbad registered despite invalid query language")
	}
	_ = loader.Close()

	// A command catalog with a case-insensitively repeated name rejects
	// the entry like any other invalid advertisement.
	setEnv(t, "PERK_PLUGIN_NAME", "qlbadcmd")
	setEnv(t, "PERK_PLUGIN_TARGETS", "qlbadcmd:")
	setEnv(t, "PERK_PLUGIN_QUERY_LANGUAGE",
		`{"name":"QL","editor_label":"Command","placeholder":"Enter a statement…","commands":[{"name":"GET","usage":"GET key","summary":"Get"},{"name":"get","usage":"GET key","summary":"Get"}]}`)
	shims = nil
	loader, errs = Load(context.Background(), configPath, testEntries(executable), registerRecorder(&shims))
	if len(errs) != 1 {
		t.Fatalf("duplicate-command Load errors = %v, want exactly one", errs)
	}
	if len(shims) != 0 {
		t.Fatalf("duplicate-command Load registered %d drivers, want none", len(shims))
	}
	if _, ok := database.ByName("qlbadcmd"); ok {
		t.Fatal("qlbadcmd registered despite duplicate command names")
	}
	_ = loader.Close()
}

// TestProxy_dynamicWrappers: capability-gated wrappers preserve
// optional-interface discovery exactly as advertised.
func TestProxy_dynamicWrappers(t *testing.T) {
	for _, test := range []struct {
		name          string
		rowWriter     string
		document      string
		wantRowWriter bool
		wantDocument  bool
	}{
		{name: "row writer only", rowWriter: "1", wantRowWriter: true},
		{name: "document only", document: "1", wantDocument: true},
		{name: "both", rowWriter: "1", document: "1", wantRowWriter: true, wantDocument: true},
		{name: "neither"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PERK_PLUGIN_HELPER", "1")
			t.Setenv("PERK_PLUGIN_ROW_WRITER", test.rowWriter)
			t.Setenv("PERK_PLUGIN_DOCUMENT", test.document)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			var shim database.Shim
			loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
				shim = s
				return nil
			})
			if len(errs) != 0 {
				t.Fatalf("Load errors = %v, want none", errs)
			}
			t.Cleanup(func() { _ = loader.Close() })

			service, err := shim.Open(context.Background(), "pluginkv:x")
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			rowWriter, ok := service.(sharedsql.RowWriter)
			if ok != test.wantRowWriter {
				t.Fatalf("RowWriter = %t, want %t", ok, test.wantRowWriter)
			}
			documentReader, ok := service.(sharedsql.DocumentReader)
			if ok != test.wantDocument {
				t.Fatalf("DocumentReader = %t, want %t", ok, test.wantDocument)
			}
			if _, ok := service.(sharedsql.DocumentWriter); ok != test.wantDocument {
				t.Fatalf("DocumentWriter = %t, want %t", ok, test.wantDocument)
			}

			provider, ok := service.(sharedsql.WriteCapabilitiesProvider)
			if ok != (test.wantRowWriter || test.wantDocument) {
				t.Fatalf("WriteCapabilitiesProvider = %t, want %t", ok, test.wantRowWriter || test.wantDocument)
			}
			if ok {
				want := sharedsql.WriteCapabilities{
					RowWriter: test.wantRowWriter,
				}
				if test.wantDocument {
					want.Document = &sharedsql.DocumentWriteCapability{
						Format: sharedsql.DocumentFormatMongoExtendedJSON,
						Text:   true,
					}
				}
				if got := provider.WriteCapabilities(); !reflect.DeepEqual(got, want) {
					t.Fatalf("WriteCapabilities() = %+v, want %+v", got, want)
				}
			}

			// The wrappers bridge real RPCs.
			if test.wantRowWriter {
				result, err := rowWriter.InsertRow(context.Background(), "widgets",
					[]sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "x"}}})
				if err != nil {
					t.Fatalf("InsertRow: %v", err)
				}
				if result.RowsAffected != 1 {
					t.Fatalf("InsertRow RowsAffected = %d, want 1", result.RowsAffected)
				}
			}
			if test.wantDocument {
				id := sharedsql.DocumentPayload{Format: sharedsql.DocumentFormatMongoExtendedJSON, Data: []byte(`{"_id":1}`)}
				got, err := documentReader.ReadDocument(context.Background(), "widgets", id)
				if err != nil {
					t.Fatalf("ReadDocument: %v", err)
				}
				if !reflect.DeepEqual(got, id) {
					t.Fatalf("ReadDocument = %+v, want the id echoed back", got)
				}
			}
		})
	}
}

// TestProxy_writeResultsCarryNativeStatement proves the row/document
// write shims map the optional wire statement onto Result.Statement, so
// the workbench can log the plugin's native replayable command instead of
// the generic preview.
func TestProxy_writeResultsCarryNativeStatement(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_ROW_WRITER", "1")
	t.Setenv("PERK_PLUGIN_DOCUMENT", "1")
	const native = "RENAME key user:2 user:3"
	t.Setenv("PERK_PLUGIN_WRITE_STATEMENT", native)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rowWriter, ok := service.(sharedsql.RowWriter)
	if !ok {
		t.Fatal("service is not a RowWriter")
	}
	documentWriter, ok := service.(sharedsql.DocumentWriter)
	if !ok {
		t.Fatal("service is not a DocumentWriter")
	}
	key := []sharedsql.RowValue{{Name: "key", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "user:2"}}}
	values := []sharedsql.RowValue{{Name: "key", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "user:3"}}}
	document := sharedsql.DocumentPayload{Format: sharedsql.DocumentFormatMongoExtendedJSON, Data: []byte(`{"name":"x"}`)}
	id := sharedsql.DocumentPayload{Format: sharedsql.DocumentFormatMongoExtendedJSON, Data: []byte(`{"_id":1}`)}
	for _, call := range []struct {
		name string
		run  func() (sharedsql.Result, error)
	}{
		{name: "insert row", run: func() (sharedsql.Result, error) { return rowWriter.InsertRow(context.Background(), "keys", values) }},
		{name: "update row", run: func() (sharedsql.Result, error) {
			return rowWriter.UpdateRow(context.Background(), "keys", key, values)
		}},
		{name: "delete row", run: func() (sharedsql.Result, error) { return rowWriter.DeleteRow(context.Background(), "keys", key) }},
		{name: "insert document", run: func() (sharedsql.Result, error) {
			return documentWriter.InsertDocument(context.Background(), "keys", document)
		}},
		{name: "replace document", run: func() (sharedsql.Result, error) {
			return documentWriter.ReplaceDocument(context.Background(), "keys", id, document)
		}},
		{name: "delete document", run: func() (sharedsql.Result, error) {
			return documentWriter.DeleteDocument(context.Background(), "keys", id)
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			result, err := call.run()
			if err != nil {
				t.Fatalf("%s: %v", call.name, err)
			}
			if result.RowsAffected != 1 {
				t.Fatalf("%s RowsAffected = %d, want 1", call.name, result.RowsAffected)
			}
			if result.Statement != native {
				t.Fatalf("%s Statement = %q, want the plugin's native statement %q", call.name, result.Statement, native)
			}
		})
	}
}

// copyPlugin copies the current test binary to dest with execute bits.
func copyPlugin(t *testing.T, dest string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		t.Fatal(err)
	}
}

// setEnv sets an env var for the test and restores the previous value at
// cleanup. Unlike t.Setenv it may be called repeatedly for the same key
// within one test.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	original, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, original)
		} else {
			os.Unsetenv(key)
		}
	})
}

// waitForMarkerLines polls the marker file until it holds at least want
// non-empty lines, then returns them.
func waitForMarkerLines(t *testing.T, path string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if contents, err := os.ReadFile(path); err == nil {
			var lines []string
			for _, line := range strings.Split(string(contents), "\n") {
				if line != "" {
					lines = append(lines, line)
				}
			}
			if len(lines) >= want {
				return lines
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("marker file %s never reached %d lines", path, want)
	return nil
}

// TestProxy_workspaceViewWrapper: the workspace-view wrapper is exposed
// exactly when the plugin advertises custom workspace views, and the
// wrapped method bridges a real RPC round trip carrying the view id and
// the structured target. Absent metadata keeps the raw session (no
// optional interface), so old plugins never receive a workspace_view
// request they cannot answer.
func TestProxy_workspaceViewWrapper(t *testing.T) {
	workspace := `{"standard_tabs":["columns"],"custom_views":[{"id":"keys","label":"Keys","scopes":["database","table"]}]}`
	for _, test := range []struct {
		name      string
		workspace string
		want      bool
	}{
		{name: "absent metadata", want: false},
		{name: "custom views advertised", workspace: workspace, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PERK_PLUGIN_HELPER", "1")
			setEnv(t, "PERK_PLUGIN_WORKSPACE", test.workspace)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			var shim database.Shim
			loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
				shim = s
				return nil
			})
			if len(errs) != 0 {
				t.Fatalf("Load errors = %v, want none", errs)
			}
			t.Cleanup(func() { _ = loader.Close() })

			service, err := shim.Open(context.Background(), "pluginkv:x")
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			provider, ok := service.(sharedsql.WorkspaceViewProvider)
			if ok != test.want {
				t.Fatalf("WorkspaceViewProvider = %t, want %t", ok, test.want)
			}
			if !ok {
				return
			}
			result, err := provider.WorkspaceView(context.Background(), sharedsql.WorkspaceViewRequest{
				ViewID: "keys",
				Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewDatabase, Database: "pluginkv"},
			})
			if err != nil {
				t.Fatalf("WorkspaceView: %v", err)
			}
			if len(result.Rows) != 1 || result.Rows[0][0] == nil || *result.Rows[0][0] != "keys" {
				t.Fatalf("WorkspaceView rows = %#v, want the view id echoed back", result.Rows)
			}
			if result.Rows[0][1] == nil || *result.Rows[0][1] != "database" {
				t.Fatalf("WorkspaceView target cell = %#v, want %q", result.Rows[0][1], "database")
			}
		})
	}
}

// TestProxy_canceledWorkspaceView: canceling the caller's context
// notifies the plugin with the original request id and the call returns
// context.Canceled — the workspace view is a session operation with the
// same cancellation semantics as every other session call.
func TestProxy_canceledWorkspaceView(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_workspace_view")
	setEnv(t, "PERK_PLUGIN_WORKSPACE", `{"custom_views":[{"id":"keys","label":"Keys","scopes":["table"]}]}`)
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	provider, ok := service.(sharedsql.WorkspaceViewProvider)
	if !ok {
		t.Fatal("service is not a WorkspaceViewProvider")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var viewErr error
	go func() {
		defer close(done)
		_, viewErr = provider.WorkspaceView(ctx, sharedsql.WorkspaceViewRequest{
			ViewID: "keys",
			Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewTable, Table: "widgets"},
		})
	}()
	waitForMarkerLines(t, marker, 1) // workspace_view reached the plugin
	cancel()
	waitForMarkerLines(t, marker, 2) // cancel notification reached the plugin
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WorkspaceView did not return after cancel")
	}
	if !errors.Is(viewErr, context.Canceled) {
		t.Fatalf("WorkspaceView error = %v, want context.Canceled", viewErr)
	}
	lines := waitForMarkerLines(t, marker, 2)
	if viewID := strings.TrimPrefix(lines[0], "workspace-view "); viewID == lines[0] {
		t.Fatalf("marker line %q, want workspace-view <id>", lines[0])
	} else if cancelID := strings.TrimPrefix(lines[1], "cancel "); cancelID == lines[1] {
		t.Fatalf("marker line %q, want cancel <id>", lines[1])
	} else if viewID != cancelID {
		t.Fatalf("cancel id %q does not match workspace-view id %q", cancelID, viewID)
	}
}

func TestLoad_sameExecutableDistinctArgsAreNotDeduplicated(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	dir := t.TempDir()
	script := filepath.Join(dir, "plugin-wrapper")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec \"$PERK_HELPER_BINARY\" -test.run=TestPluginHelperChild\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERK_HELPER_BINARY", os.Args[0])
	var registered []database.Shim
	loader, errs := Load(context.Background(), filepath.Join(dir, "config.json"), []Entry{
		{Config: "first", Display: "first", Executable: script, Args: []string{"--plugin", "first"}},
		{Config: "second", Display: "second", Executable: script, Args: []string{"--plugin", "second"}},
	}, restartRecorder(&registered))
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })
	if len(registered) != 2 || len(loader.Statuses()) != 2 {
		t.Fatalf("registered/statuses = %d/%d, want two distinct invocations", len(registered), len(loader.Statuses()))
	}
	statuses := loader.Statuses()
	if statuses[0].Entry != "first" || statuses[1].Entry != "second" {
		t.Fatalf("statuses = %+v, want stable configured identities", statuses)
	}
}
