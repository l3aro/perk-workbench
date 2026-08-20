package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
)

// runCLI invokes dispatch with captured stdout/stderr and returns the
// exit status and both outputs.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	status := dispatch(args, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

// writeConfig writes a config.json under an isolated XDG_CONFIG_HOME
// and returns the config directory (where config.json lives). A nil
// plugins list omits the key entirely.
func writeConfig(t *testing.T, plugins []string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	configDir := filepath.Join(dir, "perk-workbench")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{}
	if plugins != nil {
		config["plugins"] = plugins
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return configDir
}

// writePluginHelperScriptAt writes an executable wrapper at path that
// re-executes the current test binary as the plugin helper child, so
// the real Loader spawn path is exercised end to end: plugin.Load
// spawns the script, and the script execs this test binary with
// -test.run=TestPluginHelperChild. PERK_HELPER_BINARY (set here) keeps
// the script free of embedded paths.
func writePluginHelperScriptAt(t *testing.T, path string) string {
	t.Helper()
	t.Setenv("PERK_HELPER_BINARY", os.Args[0])
	script := "#!/bin/sh\nexec \"$PERK_HELPER_BINARY\" -test.run=TestPluginHelperChild\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// setupPluginHelper prepares the environment for one test that
// exercises real plugin children: the helper binary env, the helper
// guard, and the given PERK_PLUGIN_* overrides. It returns a fresh
// helper executable path.
func setupPluginHelper(t *testing.T, env map[string]string) string {
	t.Helper()
	script := writePluginHelperScriptAt(t, filepath.Join(t.TempDir(), "cli-plugin-helper"))
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	for key, value := range env {
		t.Setenv(key, value)
	}
	return script
}

// TestPluginHelperChild is the re-executed plugin child for CLI tests.
// It serves the perk/v1 protocol on stdio, driven by PERK_PLUGIN_*
// env vars, and always ends with os.Exit — never returning to the
// testing framework, so no PASS/ok output corrupts the protocol stream.
func TestPluginHelperChild(t *testing.T) {
	if os.Getenv("PERK_PLUGIN_HELPER") != "1" {
		return
	}
	serveCLIPluginHelper()
}

// wire types: the minimal perk/v1 envelopes the helper child speaks.
type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *wireError      `json:"error"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeWireResult struct {
	ProtocolVersion int              `json:"protocol_version"`
	Capabilities    capabilitiesWire `json:"capabilities"`
}

type targetPatternWire struct {
	Prefix string `json:"prefix"`
}

type capabilitiesWire struct {
	Name    string              `json:"name"`
	Display string              `json:"display"`
	Targets []targetPatternWire `json:"targets,omitempty"`
}

type cliPluginHelper struct {
	name            string
	display         string
	targets         []string
	protocolVersion int
	behavior        string
}

func serveCLIPluginHelper() {
	helper := &cliPluginHelper{
		name:            os.Getenv("PERK_PLUGIN_NAME"),
		display:         os.Getenv("PERK_PLUGIN_DISPLAY"),
		targets:         strings.Split(os.Getenv("PERK_PLUGIN_TARGETS"), ","),
		protocolVersion: envIntOr(os.Getenv("PERK_PLUGIN_PROTOCOL_VERSION"), 1),
		behavior:        os.Getenv("PERK_PLUGIN_BEHAVIOR"),
	}
	if helper.name == "" {
		helper.name = "clihelper"
	}
	if helper.display == "" {
		helper.display = "CLI Helper"
	}
	if len(helper.targets) == 1 && helper.targets[0] == "" {
		helper.targets = []string{"clihelper:"}
	}
	if flood := envIntOr(os.Getenv("PERK_PLUGIN_STDERR_FLOOD"), 0); flood > 0 {
		noise := []byte(strings.Repeat("stderr noise\n", 64)) // 1536 bytes
		for written := 0; written < flood; written += len(noise) {
			_, _ = os.Stderr.Write(noise)
		}
	}
	if marker := os.Getenv("PERK_PLUGIN_MARKER"); marker != "" {
		appendMarker(marker, "start")
	}
	helper.serve()
}

// appendMarker appends one event line to the marker file, flushing each
// write, so tests can observe child events deterministically.
func appendMarker(path, line string) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(line + "\n")
	_ = file.Close()
}

// serve reads request frames and answers until stdin closes.
func (h *cliPluginHelper) serve() {
	reader := bufio.NewReaderSize(os.Stdin, 16<<20)
	malformed := false
	for {
		frame, err := readHelperFrame(reader)
		if err != nil {
			if h.behavior == "exit_on_close" {
				os.Exit(5) // refuse the clean close: nonzero exit at EOF
			}
			os.Exit(0) // stdin closed: normal end of service
		}
		var incoming struct {
			ID     *uint64         `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(frame, &incoming); err != nil {
			os.Exit(1)
		}
		if incoming.ID == nil {
			continue // notifications never get a response
		}
		if marker := os.Getenv("PERK_PLUGIN_MARKER"); marker != "" {
			appendMarker(marker, incoming.Method)
		}
		switch h.behavior {
		case "malformed":
			if !malformed {
				malformed = true
				fmt.Fprint(os.Stdout, "not json\n")
			}
			continue
		case "crash":
			if incoming.Method == "perk/v1/initialize" {
				os.Exit(3)
			}
		case "sleep_init":
			if incoming.Method == "perk/v1/initialize" {
				// Answer late from a goroutine while the main loop keeps
				// reading, so a host that times out and closes stdin
				// reaps the child quickly instead of waiting for the
				// sleep.
				go func() {
					time.Sleep(30 * time.Second)
					h.respond(*incoming.ID, h.initializeResult(), nil)
				}()
				continue
			}
		case "wrong_version":
			if incoming.Method == "perk/v1/initialize" {
				h.respond(*incoming.ID, initializeWireResult{ProtocolVersion: 2}, nil)
				continue
			}
		case "bad_caps":
			if incoming.Method == "perk/v1/initialize" {
				h.respond(*incoming.ID, initializeWireResult{
					ProtocolVersion: h.protocolVersion,
					Capabilities:    capabilitiesWire{Name: "", Display: h.display},
				}, nil)
				continue
			}
		}
		// log_methods records every request method to stderr, so tests
		// can prove which protocol methods the CLI lifecycle sends.
		if h.behavior == "log_methods" {
			fmt.Fprintln(os.Stderr, incoming.Method)
		}
		h.respond(*incoming.ID, h.resultFor(incoming.Method), nil)
	}
}

func (h *cliPluginHelper) initializeResult() initializeWireResult {
	targets := make([]targetPatternWire, 0, len(h.targets))
	for _, prefix := range h.targets {
		if strings.TrimSpace(prefix) == "" {
			continue
		}
		targets = append(targets, targetPatternWire{Prefix: prefix})
	}
	return initializeWireResult{
		ProtocolVersion: h.protocolVersion,
		Capabilities:    capabilitiesWire{Name: h.name, Display: h.display, Targets: targets},
	}
}

func (h *cliPluginHelper) resultFor(method string) any {
	if method == "perk/v1/initialize" {
		return h.initializeResult()
	}
	return struct{}{}
}

func (h *cliPluginHelper) respond(id uint64, result any, rpcErr *wireError) {
	frame := wireResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if rpcErr == nil {
		payload, err := json.Marshal(result)
		if err != nil {
			os.Exit(1)
		}
		frame.Result = payload
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(append(payload, '\n'))
}

func readHelperFrame(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		return nil, fmt.Errorf("oversized frame")
	}
	return line, err
}

func envIntOr(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func TestDispatch_pluginGrammar(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStatus int
		wantStdout string
		wantStderr string
	}{
		{name: "missing command", args: []string{"plugin"}, wantStatus: 2, wantStderr: "missing command"},
		{name: "unknown command", args: []string{"plugin", "frobnicate"}, wantStatus: 2, wantStderr: `unknown command "frobnicate"`},
		{name: "help", args: []string{"plugin", "--help"}, wantStatus: 0, wantStdout: "Usage: perk-workbench plugin"},
		{name: "short help", args: []string{"plugin", "-h"}, wantStatus: 0, wantStdout: "Usage: perk-workbench plugin"},
		{name: "subcommand help", args: []string{"plugin", "doctor", "--help"}, wantStatus: 0, wantStdout: "Usage: perk-workbench plugin"},
		{name: "list unknown flag", args: []string{"plugin", "list", "--bogus"}, wantStatus: 2, wantStderr: "unknown flag"},
		{name: "list unexpected operand", args: []string{"plugin", "list", "extra"}, wantStatus: 2, wantStderr: "unexpected argument"},
		{name: "inspect missing operand", args: []string{"plugin", "inspect"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "inspect extra operand", args: []string{"plugin", "inspect", "a", "b"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "inspect unknown flag", args: []string{"plugin", "inspect", "--json", "--bogus", "x"}, wantStatus: 2, wantStderr: "unknown flag"},
		{name: "doctor unknown flag", args: []string{"plugin", "doctor", "x", "-y"}, wantStatus: 2, wantStderr: "unknown flag"},
		{name: "test missing operand", args: []string{"plugin", "test"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "test --json missing operand", args: []string{"plugin", "test", "--json"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "test extra operand", args: []string{"plugin", "test", "a", "b"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "test unknown flag", args: []string{"plugin", "test", "--json", "--bogus", "x"}, wantStatus: 2, wantStderr: "unknown flag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runCLI(t, test.args...)
			if status != test.wantStatus {
				t.Fatalf("exit status = %d, want %d (stdout %q, stderr %q)", status, test.wantStatus, stdout, stderr)
			}
			if test.wantStdout != "" && !strings.Contains(stdout, test.wantStdout) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout, test.wantStdout)
			}
			if test.wantStderr != "" && !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr, test.wantStderr)
			}
		})
	}
}

func TestDispatch_preservesTopLevelBehavior(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		status, stdout, stderr := runCLI(t, args...)
		if status != 0 || !strings.Contains(stdout, "perk-workbench") || stderr != "" {
			t.Fatalf("dispatch(%v) = %d, stdout %q, stderr %q", args, status, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"--version"}, {"-v"}} {
		status, stdout, _ := runCLI(t, args...)
		if status != 0 || stdout != versionOutput() {
			t.Fatalf("dispatch(%v) = %d, stdout %q, want %q", args, status, stdout, versionOutput())
		}
	}
	// A top-level usage error exits 2 without starting the TUI.
	status, stdout, stderr := runCLI(t, "first.db", "second.db")
	if status != 2 || stdout != "" || !strings.Contains(stderr, "expected zero or one target") {
		t.Fatalf("dispatch(two targets) = %d, stdout %q, stderr %q", status, stdout, stderr)
	}
}

func TestPluginList_configOrderAndResolution(t *testing.T) {
	dir := t.TempDir()
	first := writeExecutableAt(t, filepath.Join(dir, "first-plugin"))
	second := writeExecutableAt(t, filepath.Join(dir, "second-plugin"))
	writeConfig(t, []string{first, second})

	status, stdout, stderr := runCLI(t, "plugin", "list")
	if status != 0 || stderr != "" {
		t.Fatalf("list = %d, stderr %q", status, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q, want two lines in config order", stdout)
	}
	if lines[0] != first+" -> "+first+" [unpinned]" || lines[1] != second+" -> "+second+" [unpinned]" {
		t.Fatalf("stdout = %q, want %q and %q", stdout, first+" -> "+first+" [unpinned]", second+" -> "+second+" [unpinned]")
	}
}

func TestPluginList_emptyConfigIsExplicit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config.json: defaults materialize

	status, stdout, stderr := runCLI(t, "plugin", "list")
	if status != 0 || stderr != "" || stdout != "no plugins configured\n" {
		t.Fatalf("list = %d, stdout %q, stderr %q", status, stdout, stderr)
	}

	status, stdout, stderr = runCLI(t, "plugin", "list", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("list --json = %d, stderr %q", status, stderr)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(items) != 0 {
		t.Fatalf("list --json = %v, want an empty array", items)
	}
}

func TestPluginList_jsonIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	first := writeExecutableAt(t, filepath.Join(dir, "a-plugin"))
	second := writeExecutableAt(t, filepath.Join(dir, "b-plugin"))
	writeConfig(t, []string{first, second})

	_, stdout1, stderr1 := runCLI(t, "plugin", "list", "--json")
	_, stdout2, stderr2 := runCLI(t, "plugin", "list", "--json")
	if stdout1 != stdout2 || stderr1 != "" || stderr2 != "" {
		t.Fatalf("list --json not deterministic:\n%s\n%s", stdout1, stdout2)
	}
	var items []struct {
		Entry string `json:"entry"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout1), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout1, err)
	}
	if len(items) != 2 || items[0].Entry != first || items[1].Entry != second {
		t.Fatalf("items = %+v, want config order", items)
	}
	if items[0].Path != first || items[1].Path != second {
		t.Fatalf("items = %+v, want resolved canonical paths", items)
	}
}

func TestPluginList_configRelativeEntry(t *testing.T) {
	configDir := writeConfig(t, []string{"./relative-plugin"})
	script := writePluginHelperScriptAt(t, filepath.Join(configDir, "relative-plugin"))

	status, stdout, stderr := runCLI(t, "plugin", "list", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("list = %d, stderr %q", status, stderr)
	}
	var items []struct {
		Entry string `json:"entry"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(items) != 1 || items[0].Path != script {
		t.Fatalf("items = %+v, want the config-relative entry resolved to %q", items, script)
	}
}

func TestPluginList_bareNameResolvesThroughPATH(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	real := writeExecutableAt(t, filepath.Join(binDir, "perk-fake"))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeConfig(t, []string{"perk-fake"})

	status, stdout, stderr := runCLI(t, "plugin", "list", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("list = %d, stderr %q", status, stderr)
	}
	var items []struct {
		Entry string `json:"entry"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(items) != 1 || items[0].Path != real {
		t.Fatalf("items = %+v, want the PATH-resolved entry %q", items, real)
	}
}

func TestPluginList_symlinkResolvesCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	real := writeExecutableAt(t, filepath.Join(dir, "real-plugin"))
	link := filepath.Join(dir, "linked-plugin")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, []string{link})

	status, stdout, stderr := runCLI(t, "plugin", "list", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("list = %d, stderr %q", status, stderr)
	}
	var items []struct {
		Entry string `json:"entry"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(items) != 1 || items[0].Path != real {
		t.Fatalf("items = %+v, want the symlink canonicalized to %q", items, real)
	}
}

func TestPluginList_reportsInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-plugin")
	noexec := filepath.Join(dir, "noexec-plugin")
	if err := os.WriteFile(noexec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, []string{missing, noexec})

	status, stdout, stderr := runCLI(t, "plugin", "list", "--json")
	if status != 1 || stderr != "" {
		t.Fatalf("list = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var items []struct {
		Entry string `json:"entry"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want one per entry", items)
	}
	if items[0].Entry != missing || items[0].Path != "" || items[0].Error == "" {
		t.Fatalf("missing entry = %+v, want an error and no path", items[0])
	}
	if items[1].Entry != noexec || !strings.Contains(items[1].Error, "not executable") {
		t.Fatalf("noexec entry = %+v, want an executable-bit error", items[1])
	}

	status, stdout, stderr = runCLI(t, "plugin", "list")
	if status != 1 || stderr != "" {
		t.Fatalf("human list = %d, stderr %q, want 1", status, stderr)
	}
	if !strings.Contains(stdout, missing+" -> invalid:") || !strings.Contains(stdout, noexec+" -> invalid:") {
		t.Fatalf("human stdout = %q, want per-entry invalid lines", stdout)
	}
}

func TestPluginInspect_success(t *testing.T) {
	helper := setupPluginHelper(t, nil)
	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("inspect = %d, stderr %q", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !report.OK || report.Phase != "ok" || report.Error != "" {
		t.Fatalf("report = %+v, want an ok report", report)
	}
	if report.Entry != helper || report.Path == "" {
		t.Fatalf("report entry/path = %q/%q, want the resolved helper", report.Entry, report.Path)
	}
	if report.Capabilities == nil || report.Capabilities.Name != "clihelper" {
		t.Fatalf("capabilities = %+v, want the advertised clihelper", report.Capabilities)
	}
	if report.Snapshot == nil {
		t.Fatal("report has no snapshot, want the final diagnostics")
	}
	snap := report.Snapshot
	if snap.Path != report.Path || snap.Plugin != "clihelper" {
		t.Fatalf("snapshot path/plugin = %q/%q, want the canonical helper and identity", snap.Path, snap.Plugin)
	}
	if snap.InitDuration <= 0 {
		t.Fatalf("init_duration = %v, want positive", snap.InitDuration)
	}
	if snap.Running || snap.PID != 0 || snap.ExitStatus != 0 {
		t.Fatalf("snapshot = %+v, want a reaped clean child (running=false, pid=0, exit 0)", snap)
	}

	// Human output reports the same lifecycle.
	status, stdout, stderr = runCLI(t, "plugin", "inspect", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("human inspect = %d, stderr %q", status, stderr)
	}
	for _, want := range []string{"plugin " + helper + ":", "initialize: ok", "capabilities: name=clihelper", "shutdown: ok"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("human stdout = %q, want it to contain %q", stdout, want)
		}
	}
}

func TestPluginInspect_jsonFlagPositionIndependent(t *testing.T) {
	helper := setupPluginHelper(t, nil)
	status1, stdout1, stderr1 := runCLI(t, "plugin", "inspect", "--json", helper)
	status2, stdout2, stderr2 := runCLI(t, "plugin", "inspect", helper, "--json")
	if status1 != status2 || stderr1 != "" || stderr2 != "" {
		t.Fatalf("flag position changed exit/stderr: %d/%q vs %d/%q", status1, stderr1, status2, stderr2)
	}
	var report1, report2 pluginReport
	if err := json.Unmarshal([]byte(stdout1), &report1); err != nil {
		t.Fatalf("stdout1 %q is not JSON: %v", stdout1, err)
	}
	if err := json.Unmarshal([]byte(stdout2), &report2); err != nil {
		t.Fatalf("stdout2 %q is not JSON: %v", stdout2, err)
	}
	// init_duration is wall-clock timing and differs between runs; every
	// structural field must be identical.
	report1.Snapshot.InitDuration = 0
	report2.Snapshot.InitDuration = 0
	if !reflect.DeepEqual(report1, report2) {
		t.Fatalf("flag position changed the report:\n%+v\n%+v", report1, report2)
	}
}

func TestPluginInspect_worksOutsideConfig(t *testing.T) {
	// A broken config must not affect an explicit-operand inspect.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	configDir := filepath.Join(dir, "perk-workbench")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := setupPluginHelper(t, nil)

	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("inspect = %d, stderr %q, want success despite broken config", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !report.OK {
		t.Fatalf("report = %+v, want ok", report)
	}
}

func TestPluginInspect_failures(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantPhase string
		wantError string
	}{
		{name: "wrong protocol version", env: map[string]string{"PERK_PLUGIN_PROTOCOL_VERSION": "2"}, wantPhase: "initialize", wantError: "protocol version 2, want 1"},
		{name: "malformed stdout", env: map[string]string{"PERK_PLUGIN_BEHAVIOR": "malformed"}, wantPhase: "protocol", wantError: "malformed response frame"},
		{name: "crash during initialize", env: map[string]string{"PERK_PLUGIN_BEHAVIOR": "crash"}, wantPhase: "protocol", wantError: "exit status 3"},
		{name: "registration rejects empty name", env: map[string]string{"PERK_PLUGIN_BEHAVIOR": "bad_caps"}, wantPhase: "register", wantError: "needs a name"},
		{name: "resolve failure", env: nil, wantPhase: "resolve", wantError: "no such file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var entry string
			if test.wantPhase == "resolve" {
				entry = filepath.Join(t.TempDir(), "no-such-plugin")
			} else {
				entry = setupPluginHelper(t, test.env)
			}
			status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", entry)
			if status != 1 || stderr != "" {
				t.Fatalf("inspect = %d, stderr %q, want 1 with a JSON document", status, stderr)
			}
			var report pluginReport
			if err := json.Unmarshal([]byte(stdout), &report); err != nil {
				t.Fatalf("stdout %q is not JSON: %v", stdout, err)
			}
			if report.OK || report.Phase != test.wantPhase {
				t.Fatalf("report = %+v, want phase %q", report, test.wantPhase)
			}
			if !strings.Contains(report.Error, test.wantError) {
				t.Fatalf("error = %q, want it to contain %q", report.Error, test.wantError)
			}
			if test.wantPhase == "resolve" && (report.Path != "" || report.Snapshot != nil) {
				t.Fatalf("report = %+v, want no path and no snapshot on resolve failure", report)
			}

			// Human output names the failing phase and keeps stderr clean.
			status, stdout, stderr = runCLI(t, "plugin", "inspect", entry)
			if status != 1 || stderr != "" {
				t.Fatalf("human inspect = %d, stderr %q", status, stderr)
			}
			if want := test.wantPhase + ": FAILED"; !strings.Contains(stdout, want) {
				t.Fatalf("human stdout = %q, want it to contain %q", stdout, want)
			}
		})
	}
}

func TestPluginInspect_initTimeout(t *testing.T) {
	helper := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "sleep_init"})
	original := pluginInitTimeout
	pluginInitTimeout = 500 * time.Millisecond
	defer func() { pluginInitTimeout = original }()

	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", helper)
	if status != 1 || stderr != "" {
		t.Fatalf("inspect = %d, stderr %q, want a timeout failure", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if report.Phase != "initialize" || !strings.Contains(report.Error, "context deadline exceeded") {
		t.Fatalf("report = %+v, want an initialize deadline failure", report)
	}
	if report.Snapshot == nil || report.Snapshot.Running || report.Snapshot.PID != 0 {
		t.Fatalf("snapshot = %+v, want the timed-out child reaped", report.Snapshot)
	}
}

func TestPluginInspect_stderrFloodIsBounded(t *testing.T) {
	helper := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_STDERR_FLOOD": "1048576"})
	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("inspect = %d, stderr %q, want success despite the flood", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !report.OK || report.Snapshot == nil {
		t.Fatalf("report = %+v, want ok with a snapshot", report)
	}
	if len(report.Snapshot.Stderr) == 0 {
		t.Fatal("want a retained stderr tail from the flood")
	}
	if len(report.Snapshot.Stderr) > 100 {
		t.Fatalf("stderr tail has %d lines, want at most 100", len(report.Snapshot.Stderr))
	}
	var total int
	for _, line := range report.Snapshot.Stderr {
		total += len(line)
	}
	if total > 64<<10 {
		t.Fatalf("stderr tail is %d bytes, want at most 64 KiB", total)
	}

	// Human output shows the tail under a stderr heading.
	status, stdout, _ = runCLI(t, "plugin", "inspect", helper)
	if status != 0 || !strings.Contains(stdout, "stderr (tail):") {
		t.Fatalf("human inspect = %d, stdout %q, want a stderr tail block", status, stdout)
	}
}

func TestPluginInspect_shutdownFailure(t *testing.T) {
	helper := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "exit_on_close"})
	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", helper)
	if status != 1 || stderr != "" {
		t.Fatalf("inspect = %d, stderr %q, want a shutdown failure", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if report.Phase != "shutdown" || !strings.Contains(report.Error, "exit status 5") {
		t.Fatalf("report = %+v, want a shutdown phase failure", report)
	}
	if report.Snapshot == nil || report.Snapshot.ExitStatus != 5 || report.Snapshot.Running {
		t.Fatalf("snapshot = %+v, want the reaped child with exit status 5", report.Snapshot)
	}
}

func TestPluginDoctor_mixedResultsContinueInOrder(t *testing.T) {
	helper := setupPluginHelper(t, nil)
	configDir := writeConfig(t, []string{helper, "./ok-helper", "./missing-plugin"})
	writePluginHelperScriptAt(t, filepath.Join(configDir, "ok-helper"))

	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json")
	if status != 1 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 3 {
		t.Fatalf("reports = %d items, want one per configured entry", len(reports))
	}
	if reports[0].Entry != helper || !reports[0].OK || reports[0].Phase != "ok" {
		t.Fatalf("reports[0] = %+v, want the absolute helper ok", reports[0])
	}
	if reports[1].Entry != "./ok-helper" || !reports[1].OK {
		t.Fatalf("reports[1] = %+v, want the config-relative helper ok", reports[1])
	}
	if reports[2].Entry != "./missing-plugin" || reports[2].OK || reports[2].Phase != "resolve" {
		t.Fatalf("reports[2] = %+v, want the missing entry resolve-failed", reports[2])
	}

	// Human output renders every item, failing ones visibly, on stdout.
	status, stdout, stderr = runCLI(t, "plugin", "doctor")
	if status != 1 || stderr != "" {
		t.Fatalf("human doctor = %d, stderr %q", status, stderr)
	}
	if !strings.Contains(stdout, "plugin "+helper+":") || !strings.Contains(stdout, "resolve: FAILED") {
		t.Fatalf("human stdout = %q, want every item with the resolve failure visible", stdout)
	}
}

func TestPluginDoctor_explicitOperandsBeatConfig(t *testing.T) {
	helper := setupPluginHelper(t, nil)
	writeConfig(t, []string{filepath.Join(t.TempDir(), "configured-missing")})

	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q, want success on the explicit operand", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 1 || !reports[0].OK {
		t.Fatalf("reports = %+v, want exactly the explicit operand ok", reports)
	}
	if strings.Contains(stdout, "configured-missing") {
		t.Fatalf("stdout = %q, config entries must be ignored when operands are given", stdout)
	}
}

func TestPluginDoctor_explicitRelativeOperandUsesWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	script := writePluginHelperScriptAt(t, filepath.Join(dir, "cwd-helper"))
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Chdir(dir)

	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json", "./cwd-helper")
	if status != 0 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 1 || !reports[0].OK || reports[0].Path != script {
		t.Fatalf("reports = %+v, want the cwd-relative operand resolved to %q", reports, script)
	}
}

func TestPluginDoctor_duplicateIdentityAcrossItems(t *testing.T) {
	helperA := setupPluginHelper(t, nil)
	helperB := setupPluginHelper(t, nil) // same advertised driver name
	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json", helperA, helperB)
	if status != 0 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q, want both items ok", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 2 || !reports[0].OK || !reports[1].OK {
		t.Fatalf("reports = %+v, want both duplicate-identity items ok", reports)
	}
	if _, ok := database.ByName("clihelper"); ok {
		t.Fatal("doctor installed a global driver; the registry must stay untouched")
	}
}

func TestPluginDoctor_overlappingTargetsAcrossItems(t *testing.T) {
	// Both children advertise the same target prefix. Real registration
	// would reject the second; validation-only items must both pass and
	// leave the registry clean.
	helperA := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_TARGETS": "shared:"})
	helperB := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_TARGETS": "shared:"})
	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json", helperA, helperB)
	if status != 0 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 2 || !reports[0].OK || !reports[1].OK {
		t.Fatalf("reports = %+v, want both overlapping-prefix items ok", reports)
	}
	if _, ok := database.ByName("clihelper"); ok {
		t.Fatal("doctor installed a global driver; the registry must stay untouched")
	}
}

func TestPluginDoctor_jsonStaysValidOnItemFailures(t *testing.T) {
	helper := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "crash"})
	missing := filepath.Join(t.TempDir(), "no-such-plugin")
	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json", helper, missing)
	if status != 1 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q, want 1 with valid JSON on stdout", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports = %d items, want one per input", len(reports))
	}
	if reports[0].Phase != "protocol" || reports[1].Phase != "resolve" {
		t.Fatalf("phases = %q/%q, want protocol then resolve", reports[0].Phase, reports[1].Phase)
	}
}

func TestPluginInspect_neverInvokesSessionProtocol(t *testing.T) {
	// log_methods records every request the child receives on stderr.
	// The inspect lifecycle must send only the initialize handshake —
	// never build_target, open, session RPCs, or close — so no
	// user-supplied target/form values, credentials, or statements can
	// cross the wire (redaction by construction).
	helper := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "log_methods"})
	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("inspect = %d, stderr %q", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if report.Snapshot == nil {
		t.Fatal("missing snapshot")
	}
	if got := strings.Join(report.Snapshot.Stderr, ","); got != "perk/v1/initialize" {
		t.Fatalf("methods seen by the plugin = %q, want only perk/v1/initialize", got)
	}
}

func TestPluginDoctor_stopsSpawningOnInterrupt(t *testing.T) {
	// The first entry blocks in initialize; after SIGINT the doctor must
	// fail that item and stop — no further children may be spawned, even
	// though Load itself only observes the context during initialize.
	marker := filepath.Join(t.TempDir(), "events.log")
	blocking := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "sleep_init", "PERK_PLUGIN_MARKER": marker})
	otherA := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	otherB := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_MARKER": marker})

	var status int
	var stdout, stderr string
	done := make(chan struct{})
	go func() {
		status, stdout, stderr = runCLI(t, "plugin", "doctor", "--json", blocking, otherA, otherB)
		close(done)
	}()
	waitForMarkerLine(t, marker, "perk/v1/initialize")
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("doctor did not stop after SIGINT")
	}

	if status != 1 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d items, want only the in-flight item", len(reports))
	}
	if reports[0].Phase != "initialize" || !strings.Contains(reports[0].Error, "context canceled") {
		t.Fatalf("report = %+v, want the interrupted initialize failure", reports[0])
	}
	if starts := markerLineCount(t, marker, "start"); starts != 1 {
		t.Fatalf("marker records %d child starts, want exactly the interrupted one (no children after SIGINT)", starts)
	}
}

// TestPluginDoctorItems_interruptedAfterOKItemFailsOverall: a
// cancellation that lands between items — after an item fully passed —
// must stop the check and fail it overall; partial output must never
// exit 0.
func TestPluginDoctorItems_interruptedAfterOKItemFailsOverall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	check := func(ctx context.Context, entry, configPath string) pluginReport {
		calls++
		if calls == 1 {
			cancel() // interrupt between items: this item passed, the next must not run
		}
		return pluginReport{Entry: entry, OK: true, Phase: phaseOK}
	}

	reports, failed := checkPluginItems(ctx, []string{"one", "two"}, "", check)
	if !failed {
		t.Fatal("interrupted check must fail overall, got status 0 semantics")
	}
	if len(reports) != 1 || reports[0].Entry != "one" || !reports[0].OK {
		t.Fatalf("reports = %+v, want only the completed item", reports)
	}
	if calls != 1 {
		t.Fatalf("check ran %d items, want 1 (no child after the interrupt)", calls)
	}
}

// TestPluginDoctorItems_cancelBeforeStart: a canceled context runs no
// items at all and fails the check.
func TestPluginDoctorItems_cancelBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	check := func(ctx context.Context, entry, configPath string) pluginReport {
		calls++
		return pluginReport{Entry: entry, OK: true, Phase: phaseOK}
	}

	reports, failed := checkPluginItems(ctx, []string{"one", "two"}, "", check)
	if !failed {
		t.Fatal("pre-canceled check must fail overall")
	}
	if len(reports) != 0 || calls != 0 {
		t.Fatalf("reports = %+v with %d calls, want none", reports, calls)
	}
}

// TestPluginDoctorItems_continuesAfterItemFailures: an item failure is
// not an interrupt — later items still run and the check fails overall.
func TestPluginDoctorItems_continuesAfterItemFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	check := func(ctx context.Context, entry, configPath string) pluginReport {
		return pluginReport{Entry: entry, OK: entry == "good", Phase: phaseOK}
	}

	reports, failed := checkPluginItems(ctx, []string{"bad", "good", "bad"}, "", check)
	if !failed {
		t.Fatal("check with failing items must fail overall")
	}
	if len(reports) != 3 {
		t.Fatalf("reports = %d items, want all three despite failures", len(reports))
	}
	if reports[1].Entry != "good" || !reports[1].OK {
		t.Fatalf("reports = %+v, want the middle item to have run and passed", reports)
	}
}

// markerLineCount counts the marker lines equal to want.
func markerLineCount(t *testing.T, path, want string) int {
	t.Helper()
	count := 0
	for _, line := range readMarkerLines(t, path) {
		if line == want {
			count++
		}
	}
	return count
}

// waitForMarkerLine polls the marker file until it holds the wanted
// line.
func waitForMarkerLine(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if markerLineCount(t, path, want) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("marker file %s never saw line %q", path, want)
}

// readMarkerLines returns the non-empty lines of the marker file, or
// nil when it does not exist yet.
func readMarkerLines(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(contents), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// writeExecutableAt writes an executable file (list commands never run
// it) and returns its path.
func writeExecutableAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPluginReport_neverCarriesCredentialMaterial pins the report shape:
// inspect and doctor JSON documents carry declarative data and process
// diagnostics only — never password keys, userinfo URLs, or any '@'-
// containing credential material (the lifecycle never exchanges form
// values, so nothing can leak into the report).
func TestPluginReport_neverCarriesCredentialMaterial(t *testing.T) {
	helper := setupPluginHelper(t, nil)
	for _, args := range [][]string{
		{"plugin", "inspect", "--json", helper},
		{"plugin", "doctor", "--json", helper},
	} {
		status, stdout, stderr := runCLI(t, args...)
		if status != 0 || stderr != "" {
			t.Fatalf("%v = %d, stderr %q", args, status, stderr)
		}
		for _, forbidden := range []string{`"pass":`, `"password":`, "@", "://"} {
			if strings.Contains(stdout, forbidden) {
				t.Fatalf("%v report contains %q: %s", args, forbidden, stdout)
			}
		}
	}
}
