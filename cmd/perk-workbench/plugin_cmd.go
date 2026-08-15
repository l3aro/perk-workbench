package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	app "github.com/l3aro/perk-workbench/internal/workbench/app"
)

const pluginUsage = `Usage: perk-workbench plugin COMMAND [--json] [ARGUMENTS...]

Manage external database driver plugins.

Commands:
  list [--json]
      List configured plugin entries in config order without spawning
      children. Each entry is resolved with the exact startup resolution
      and allowlist; invalid entries are reported per entry. Exit status
      1 when any entry is invalid.
  inspect [--json] EXECUTABLE
      Resolve, initialize, and validate one plugin over perk/v1, then
      close it and report its capabilities and final diagnostic
      snapshot. Works for executables not listed in config.
  doctor [--json] [EXECUTABLE...]
      Run the full resolve/initialize/register/shutdown lifecycle for
      every configured entry, or exactly the given executables. Each
      item runs independently and failures never stop later items. Exit
      status 1 when any item fails.

Options:
  --json       Machine-readable JSON on stdout; diagnostics for the
               invocation go to stderr only when no JSON document can
               be produced
  -h, --help   Show this help

Exit status: 0 success, 1 plugin or operational failure, 2 usage error.
`

// pluginInitTimeout bounds one plugin initialize handshake in the
// inspect and doctor commands: a plugin that does not answer in time
// fails the item and is closed forcibly. Every initialize and close is
// therefore context- and time-bounded and signal-aware (the command
// context derives from the process signals). Tests shorten the bound.
var pluginInitTimeout = 30 * time.Second

// Lifecycle phase names, stable in JSON and human output.
const (
	phaseResolve    = "resolve"
	phaseInitialize = "initialize"
	phaseProtocol   = "protocol"
	phaseRegister   = "register"
	phaseShutdown   = "shutdown"
	phaseOK         = "ok"
)

// pluginReport is the per-item outcome of a plugin command: the input
// entry, the resolved canonical path, the failing lifecycle phase, and
// the structured diagnostics. It is also the JSON document shape, so
// JSON output is stable and machine-readable by construction. It never
// carries target or form values, credentials, statements, or stdout
// protocol frames: the lifecycle below never exchanges them.
type pluginReport struct {
	Entry string `json:"entry"`
	// Path is the canonical executable path once resolution succeeded.
	Path string `json:"path,omitempty"`
	// OK reports whether the full lifecycle succeeded.
	OK bool `json:"ok"`
	// Phase is the failing lifecycle phase — resolve, initialize,
	// protocol, register, or shutdown — or "ok" when every phase passed.
	Phase string `json:"phase"`
	// Error is the failure text when OK is false.
	Error string `json:"error,omitempty"`
	// Capabilities is the driver advertisement once the initialize
	// handshake succeeded: declarative identity, target patterns, form
	// description, and write interfaces — never user-supplied values.
	Capabilities *database.Capabilities `json:"capabilities,omitempty"`
	// Snapshot is the final diagnostic snapshot, taken after the child
	// was closed: canonical path, init duration, exit/running state, and
	// the bounded stderr tail.
	Snapshot *plugin.Snapshot `json:"snapshot,omitempty"`
}

// dispatchPlugin parses and runs one plugin subcommand, returning the
// process exit status. --json is accepted before or after the
// positional operands; unknown flags and malformed operand counts are
// usage errors.
func dispatchPlugin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "perk-workbench plugin: missing command")
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	command := args[0]
	switch command {
	case "list", "inspect", "doctor":
	case "--help", "-h":
		fmt.Fprint(stdout, pluginUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "perk-workbench plugin: unknown command %q\n", command)
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}

	jsonOut := false
	var operands []string
	for _, arg := range args[1:] {
		switch {
		case arg == "--json":
			jsonOut = true
		case arg == "--help" || arg == "-h":
			fmt.Fprint(stdout, pluginUsage)
			return 0
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(stderr, "perk-workbench plugin %s: unknown flag %q\n", command, arg)
			return 2
		default:
			operands = append(operands, arg)
		}
	}

	switch command {
	case "list":
		if len(operands) > 0 {
			fmt.Fprintf(stderr, "perk-workbench plugin list: unexpected argument %q\n", operands[0])
			return 2
		}
		return runPluginList(jsonOut, stdout, stderr)
	case "inspect":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "perk-workbench plugin inspect: expected exactly one executable")
			return 2
		}
		return runPluginInspect(jsonOut, operands[0], stdout, stderr)
	default: // doctor
		return runPluginDoctor(jsonOut, operands, stdout, stderr)
	}
}

// runPluginList reads the same config path and parser as startup and
// reports the configured entries in config order, resolving each with
// the exact startup resolution and allowlist without spawning anything.
// An empty config is successful and explicit.
func runPluginList(jsonOut bool, stdout, stderr io.Writer) int {
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "perk-workbench plugin list: %v\n", err)
		return 1
	}
	configPath := app.ConfigPath()

	// listItem is the stable machine-readable shape of one entry.
	type listItem struct {
		Entry string `json:"entry"`
		Path  string `json:"path,omitempty"`
		Error string `json:"error,omitempty"`
	}
	items := make([]listItem, 0, len(config.Plugins))
	failed := false
	for _, entry := range config.Plugins {
		item := listItem{Entry: entry}
		path, err := plugin.ResolveExecutable(entry, configPath)
		if err != nil {
			item.Error = err.Error()
			failed = true
		} else {
			item.Path = path
		}
		items = append(items, item)
	}

	if jsonOut {
		return emitJSON(stdout, items, exitCode(!failed))
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "no plugins configured")
		return 0
	}
	for _, item := range items {
		if item.Error != "" {
			fmt.Fprintf(stdout, "%s -> invalid: %s\n", item.Entry, item.Error)
		} else {
			fmt.Fprintf(stdout, "%s -> %s\n", item.Entry, item.Path)
		}
	}
	return exitCode(!failed)
}

// runPluginInspect resolves one executable (not required to be in
// config), runs it through the real loader lifecycle, and reports its
// capabilities plus the final diagnostic snapshot.
func runPluginInspect(jsonOut bool, entry string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, pluginInitTimeout)
	defer cancel()

	report := checkPlugin(ctx, entry, "")
	if jsonOut {
		return emitJSON(stdout, report, exitCode(report.OK))
	}
	printPluginReport(stdout, report)
	return exitCode(report.OK)
}

// runPluginDoctor checks every configured entry in order, or exactly
// the given executables. Each item runs the full lifecycle
// independently, so duplicate identities or target prefixes across
// items never mutate or contaminate the global driver registration;
// failures never stop later items.
func runPluginDoctor(jsonOut bool, operands []string, stdout, stderr io.Writer) int {
	entries := operands
	configPath := ""
	if len(operands) == 0 {
		config, err := loadConfig()
		if err != nil {
			fmt.Fprintf(stderr, "perk-workbench plugin doctor: %v\n", err)
			return 1
		}
		entries = config.Plugins
		configPath = app.ConfigPath()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reports, failed := checkPluginItems(ctx, entries, configPath, checkPlugin)

	if jsonOut {
		return emitJSON(stdout, reports, exitCode(!failed))
	}
	for i, report := range reports {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		printPluginReport(stdout, report)
	}
	return exitCode(!failed)
}

// checkPluginItems runs the full plugin lifecycle for every entry in
// order under ctx. Once the context is canceled (SIGINT/SIGTERM) no
// further children are spawned and the check fails overall: an
// interrupted check is incomplete even when every item that ran
// passed. Load itself only observes the context during the initialize
// call, so the loop must stop before launching the next child. check is
// the per-item lifecycle — checkPlugin in production; tests inject a
// fake to control cancellation between items.
func checkPluginItems(ctx context.Context, entries []string, configPath string, check func(context.Context, string, string) pluginReport) ([]pluginReport, bool) {
	reports := make([]pluginReport, 0, len(entries))
	failed := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			failed = true
			break
		}
		itemCtx, cancel := context.WithTimeout(ctx, pluginInitTimeout)
		report := check(itemCtx, entry, configPath)
		cancel()
		reports = append(reports, report)
		if !report.OK {
			failed = true
		}
	}
	return reports, failed
}

// checkPlugin runs one plugin through the full resolve, initialize and
// registration-validation, shutdown lifecycle with its own Loader, so
// items never mutate or contaminate each other or the global driver
// registry. configPath is the config file path used to resolve relative
// entries ("" for explicit operands, which resolve against the working
// directory). Registration validation uses the side-effect-free
// database.ValidateShim — no global driver is ever installed. The
// snapshot is taken after Loader.Close so it reflects the final
// exit/running state, and it remains available because the loader
// retains its clients.
func checkPlugin(ctx context.Context, entry, configPath string) pluginReport {
	report := pluginReport{Entry: entry}

	path, err := plugin.ResolveExecutable(entry, configPath)
	if err != nil {
		report.Phase = phaseResolve
		report.Error = err.Error()
		return report
	}
	report.Path = path

	var registerErr error
	loader, errs := plugin.Load(ctx, configPath, []string{path}, func(shim database.Shim) error {
		caps := shim.Capabilities()
		report.Capabilities = &caps
		registerErr = database.ValidateShim(shim)
		return registerErr
	})
	closeErr := loader.Close()
	snapshots := loader.Snapshots()
	if len(snapshots) > 0 {
		snapshot := snapshots[0]
		report.Snapshot = &snapshot
	}

	switch {
	case len(errs) > 0:
		switch {
		case registerErr != nil:
			report.Phase = phaseRegister
			report.Error = registerErr.Error()
		case report.Snapshot != nil && !benignProtocolError(report.Snapshot.Error):
			// The child hit a terminal protocol or process failure: a
			// malformed response stream, a crash, or a premature exit
			// mid-handshake. Its text is already part of the snapshot.
			report.Phase = phaseProtocol
			report.Error = report.Snapshot.Error
		default:
			report.Phase = phaseInitialize
			report.Error = errors.Join(errs...).Error()
		}
	case closeErr != nil:
		report.Phase = phaseShutdown
		report.Error = closeErr.Error()
	default:
		report.Phase = phaseOK
		report.OK = true
	}
	return report
}

// benignProtocolError reports whether a snapshot's terminal error text
// is the clean-close artifact rather than a protocol or process
// failure: EOF on the response stream after a normal child exit, or the
// client-closed marker a clean close leaves behind.
func benignProtocolError(errText string) bool {
	return errText == "" || errText == "EOF" || errText == "perk/v1: client closed"
}

// printPluginReport renders one plugin report as concise deterministic
// human output, visibly separating resolution, initialize, protocol,
// registration, and shutdown failures. The bounded stderr tail is shown
// only when present.
func printPluginReport(w io.Writer, report pluginReport) {
	fmt.Fprintf(w, "plugin %s:\n", report.Entry)
	if report.Path != "" {
		fmt.Fprintf(w, "  path: %s\n", report.Path)
	}
	if report.OK {
		if report.Snapshot != nil {
			fmt.Fprintf(w, "  initialize: ok (%s)\n", report.Snapshot.InitDuration)
		}
		fmt.Fprintf(w, "  capabilities: %s\n", formatCapabilities(report.Capabilities))
		fmt.Fprintf(w, "  shutdown: ok\n")
	} else {
		fmt.Fprintf(w, "  %s: FAILED: %s\n", report.Phase, report.Error)
	}
	if report.Snapshot != nil && len(report.Snapshot.Stderr) > 0 {
		fmt.Fprintln(w, "  stderr (tail):")
		for _, line := range report.Snapshot.Stderr {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}
}

// formatCapabilities renders one plugin's driver advertisement for
// human output. Only declarative data is shown: identity, target
// prefixes, query language, and write interfaces — never form values,
// credentials, or statements.
func formatCapabilities(caps *database.Capabilities) string {
	if caps == nil {
		return "none"
	}
	targets := make([]string, 0, len(caps.Targets))
	for _, pattern := range caps.Targets {
		targets = append(targets, pattern.Prefix)
	}
	language := "sql" // the legacy default for an absent advertisement
	if caps.QueryLanguage != nil && strings.TrimSpace(caps.QueryLanguage.Name) != "" {
		language = caps.QueryLanguage.Name
	}
	writes := "none"
	switch {
	case caps.WriteCapabilities.RowWriter && caps.WriteCapabilities.Document != nil:
		writes = "row+document"
	case caps.WriteCapabilities.RowWriter:
		writes = "row"
	case caps.WriteCapabilities.Document != nil:
		writes = "document"
	}
	return fmt.Sprintf("name=%s display=%q targets=[%s] query_language=%s writes=%s",
		caps.Name, caps.Display, strings.Join(targets, ","), language, writes)
}

// emitJSON writes one indented JSON document to stdout followed by a
// newline. The document shapes are plain structs, so marshaling cannot
// fail. exit is the command's exit status; item-level failures are
// encoded in the document and never spill to stderr.
func emitJSON(stdout io.Writer, document any, exit int) int {
	data, _ := json.MarshalIndent(document, "", "  ")
	fmt.Fprintln(stdout, string(data))
	return exit
}

// exitCode maps an outcome onto the process exit status: 0 success, 1
// plugin or operational failure.
func exitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
