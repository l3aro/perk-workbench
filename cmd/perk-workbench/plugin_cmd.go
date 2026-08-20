package main

import (
	"context"
	"encoding/json"
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
      and allowlist; invalid entries are reported per entry. Each entry
      also reports its trust state (unpinned, or pinned with the
      configured fingerprint). Exit status 1 when any entry is invalid.
  inspect [--json] EXECUTABLE
      Resolve, initialize, and validate one plugin over perk/v1, then
      close it and report its capabilities, fingerprint/trust state,
      and final diagnostic snapshot. Works for executables not listed
      in config. A pinned executable whose bytes drifted is refused
      before anything spawns.
  doctor [--json] [EXECUTABLE...]
      Run the full resolve/initialize/register/shutdown lifecycle for
      every configured entry, or exactly the given executables. Each
      item runs independently and failures never stop later items.
      Pinned drift fails the item before its child spawns. Exit status
      1 when any item fails.
  add [--json] [--approve SHA256] EXECUTABLE
      Resolve, inspect, and fingerprint one plugin: the report shows
      the capabilities and the lowercase SHA-256 of the canonical
      executable bytes, and the config is NOT touched. Rerun with
      --approve <that exact digest> to pin and atomically persist the
      plugin. With --approve the resolve/inspect/hash is repeated and
      fails closed when the digest does not match the current bytes;
      nothing is written on any failure.
  remove [--json] NAME_OR_EXECUTABLE
      Atomically remove one configured plugin and its trust record.
      NAME_OR_EXECUTABLE matches a configured entry exactly, or an
      executable that resolves to exactly one configured plugin;
      ambiguous matches fail instead of removing multiple entries.
  test [--json] EXECUTABLE
      Run the perk/v1 conformance suite against one executable:
      fixture-driven protocol cases and generated transport cases, each
      in a fresh child that is terminated when the case ends. --json
      emits one self-contained release evidence document (evidence
      schema/version, protocol and host versions, contract and
      executable digests, capabilities identity, case results, final
      pass/fail) suitable for release evidence. Exit status 1 when any
      case fails.

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

// Lifecycle phase names, stable in JSON and human output. They mirror
// the plugin package's inspect lifecycle plus the command-level
// verification phases: trust (a pinned digest that cannot be verified
// or does not match the current bytes) and hash (the fingerprint could
// not be computed).
const (
	phaseResolve    = plugin.PhaseResolve
	phaseInitialize = plugin.PhaseInitialize
	phaseProtocol   = plugin.PhaseProtocol
	phaseRegister   = plugin.PhaseRegister
	phaseShutdown   = plugin.PhaseShutdown
	phaseOK         = plugin.PhaseOK
	phaseTrust      = "trust"
	phaseHash       = "hash"
)

// Trust states, stable in JSON and human output: unpinned (no trust
// record), pinned (record present, verification not run — the list
// command), match (record present and verified against the current
// bytes), and mismatch (record present but the bytes drifted).
const (
	trustUnpinned = "unpinned"
	trustPinned   = "pinned"
	trustMatch    = "match"
	trustMismatch = "mismatch"
)

const (
	pluginSourceOfficial = "official"
	pluginSourceUser     = "user"
)

type pluginCommandEntry struct {
	entry  string
	source string
}

// pluginReport is the per-item outcome of a plugin command: the input
// entry, the resolved canonical path, the failing lifecycle phase, and
// the structured diagnostics. It is also the JSON document shape, so
// JSON output is stable and machine-readable by construction. It never
// carries target or form values, credentials, statements, or stdout
// protocol frames: the lifecycle below never exchanges them.
type pluginReport struct {
	Entry  string `json:"entry"`
	Source string `json:"source,omitempty"`
	// Path is the canonical executable path once resolution succeeded.
	Path string `json:"path,omitempty"`
	// OK reports whether the full lifecycle succeeded.
	OK bool `json:"ok"`
	// Phase is the failing lifecycle phase — resolve, initialize,
	// protocol, register, shutdown, or trust — or "ok" when every phase
	// passed.
	Phase string `json:"phase"`
	// Error is the failure text when OK is false.
	Error string `json:"error,omitempty"`
	// Trust is the fingerprint/trust state: unpinned, match, or
	// mismatch (list reports unpinned or pinned).
	Trust string `json:"trust,omitempty"`
	// SHA256 is the computed digest of the canonical executable bytes.
	SHA256 string `json:"sha256,omitempty"`
	// Expected is the pinned digest from the trust record when one
	// exists.
	Expected string `json:"expected_sha256,omitempty"`
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
// positional operands; --approve consumes the following argument;
// unknown flags and malformed operand counts are usage errors.
func dispatchPlugin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "perk-workbench plugin: missing command")
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}
	command := args[0]
	switch command {
	case "list", "inspect", "doctor", "add", "remove", "test":
	case "--help", "-h":
		fmt.Fprint(stdout, pluginUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "perk-workbench plugin: unknown command %q\n", command)
		fmt.Fprint(stderr, pluginUsage)
		return 2
	}

	jsonOut := false
	approve := ""
	var operands []string
	args = args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			jsonOut = true
		case arg == "--approve":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				fmt.Fprintf(stderr, "perk-workbench plugin %s: --approve requires a SHA-256 digest\n", command)
				return 2
			}
			i++
			approve = args[i]
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

	if approve != "" && command != "add" {
		fmt.Fprintf(stderr, "perk-workbench plugin %s: --approve is only valid with add\n", command)
		return 2
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
	case "add":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "perk-workbench plugin add: expected exactly one executable")
			return 2
		}
		return runPluginAdd(jsonOut, approve, operands[0], stdout, stderr)
	case "remove":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "perk-workbench plugin remove: expected exactly one name or executable")
			return 2
		}
		return runPluginRemove(jsonOut, operands[0], stdout, stderr)
	case "test":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "perk-workbench plugin test: expected exactly one executable")
			return 2
		}
		return runPluginTest(jsonOut, operands[0], stdout, stderr)
	default: // doctor
		return runPluginDoctor(jsonOut, operands, stdout, stderr)
	}
}

// runPluginList reads the same config path and parser as startup and
// reports the configured entries in config order, resolving each with
// the exact startup resolution and allowlist without spawning anything.
// Each entry also reports its trust state: unpinned, or pinned with the
// configured fingerprint. An empty config is successful and explicit.
func runPluginList(jsonOut bool, stdout, stderr io.Writer) int {
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "perk-workbench plugin list: %v\n", err)
		return 1
	}
	configPath := app.ConfigPath()
	entries, trust, err := configuredPluginCommandEntries(config)
	if err != nil {
		fmt.Fprintf(stderr, "perk-workbench plugin list: %v\n", err)
		return 1
	}

	// listItem is the stable machine-readable shape of one entry.
	type listItem struct {
		Entry    string `json:"entry"`
		Source   string `json:"source"`
		Path     string `json:"path,omitempty"`
		Trust    string `json:"trust,omitempty"`
		Expected string `json:"expected_sha256,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	items := make([]listItem, 0, len(entries))
	failed := false
	for _, entry := range entries {
		item := listItem{Entry: entry.entry, Source: entry.source}
		path, err := plugin.ResolveExecutable(entry.entry, configPath)
		if err != nil {
			item.Error = err.Error()
			failed = true
		} else {
			item.Path = path
			if pin, pinned := trust[path]; pinned {
				item.Trust = trustPinned
				item.Expected = pin
			} else {
				item.Trust = trustUnpinned
			}
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
			fmt.Fprintf(stdout, "%s [%s] -> invalid: %s\n", item.Entry, item.Source, item.Error)
		} else if item.Trust == trustPinned {
			fmt.Fprintf(stdout, "%s [%s] -> %s [pinned sha256:%s]\n", item.Entry, item.Source, item.Path, item.Expected)
		} else {
			fmt.Fprintf(stdout, "%s [%s] -> %s [unpinned]\n", item.Entry, item.Source, item.Path)
		}
	}
	return exitCode(!failed)
}

// runPluginInspect resolves one executable (not required to be in
// config), runs it through the real loader lifecycle, and reports its
// capabilities plus the final diagnostic snapshot. A pinned executable
// whose bytes drifted is refused before anything spawns; the trust
// state of the resolved canonical path is reported either way.
func runPluginInspect(jsonOut bool, entry string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, pluginInitTimeout)
	defer cancel()

	report := checkPlugin(ctx, entry, "", app.ReadPluginTrust(app.ConfigPath()))
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
// failures never stop later items. A pinned executable whose bytes
// drifted fails its item before the child spawns.
func runPluginDoctor(jsonOut bool, operands []string, stdout, stderr io.Writer) int {
	entries := operands
	configPath := ""
	trust := app.ReadPluginTrust(app.ConfigPath())
	sources := pluginSourceMap(entries, pluginSourceUser)
	if len(operands) == 0 {
		config, err := loadConfig()
		if err != nil {
			fmt.Fprintf(stderr, "perk-workbench plugin doctor: %v\n", err)
			return 1
		}
		configuredEntries, configuredTrust, err := configuredPluginCommandEntries(config)
		if err != nil {
			fmt.Fprintf(stderr, "perk-workbench plugin doctor: %v\n", err)
			return 1
		}
		entries = commandEntryNames(configuredEntries)
		configPath = app.ConfigPath()
		trust = configuredTrust
		sources = commandEntrySources(configuredEntries)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	check := func(ctx context.Context, entry, configPath string) pluginReport {
		report := checkPlugin(ctx, entry, configPath, trust)
		report.Source = sources[entry]
		return report
	}
	reports, failed := checkPluginItems(ctx, entries, configPath, check)

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

func configuredPluginCommandEntries(config app.Config) ([]pluginCommandEntry, map[string]string, error) {
	officialEntries, officialTrust, err := officialPluginEntries(config.DisabledOfficialPlugins)
	if err != nil {
		return nil, nil, err
	}
	entries := make([]pluginCommandEntry, 0, len(officialEntries)+len(config.Plugins))
	for _, entry := range officialEntries {
		entries = append(entries, pluginCommandEntry{entry: entry, source: pluginSourceOfficial})
	}
	for _, entry := range config.Plugins {
		entries = append(entries, pluginCommandEntry{entry: entry, source: pluginSourceUser})
	}
	trust := make(map[string]string, len(officialTrust)+len(config.PluginTrust))
	for entry, digest := range officialTrust {
		trust[entry] = digest
	}
	for entry, digest := range config.PluginTrust {
		trust[entry] = digest
	}
	return entries, trust, nil
}

func commandEntryNames(entries []pluginCommandEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.entry)
	}
	return names
}

func commandEntrySources(entries []pluginCommandEntry) map[string]string {
	sources := make(map[string]string, len(entries))
	for _, entry := range entries {
		sources[entry.entry] = entry.source
	}
	return sources
}

func pluginSourceMap(entries []string, source string) map[string]string {
	sources := make(map[string]string, len(entries))
	for _, entry := range entries {
		sources[entry] = source
	}
	return sources
}

// pluginAddResult is the JSON document shape of `plugin add`. It embeds
// the inspect report (entry, path, phases, capabilities, snapshot) and
// adds the fingerprint and the config-mutation outcome: pending for the
// two-stage preview (config untouched, rerun with --approve), changed
// for an approve that persisted, false when the pin was already exact.
type pluginAddResult struct {
	pluginReport
	SHA256  string `json:"sha256,omitempty"`
	Changed bool   `json:"changed"`
	Pending bool   `json:"pending,omitempty"`
}

// runPluginAdd implements the two-stage pin flow. Without --approve it
// resolves, fully inspects, and fingerprints the executable and reports
// the capabilities plus the lowercase SHA-256 of the canonical bytes,
// WITHOUT touching config; the output tells the user to rerun with the
// exact --approve digest. With --approve the resolve/inspect/hash is
// repeated, the supplied digest must exactly match the current bytes
// (fail closed otherwise), and only then is the plugin persisted
// atomically. The preview never reads or materializes config.
func runPluginAdd(jsonOut bool, approve, entry string, stdout, stderr io.Writer) int {
	configPath := app.ConfigPath()
	trust := app.ReadPluginTrust(configPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, pluginInitTimeout)
	defer cancel()

	report := checkPlugin(ctx, entry, "", trust)
	digest := ""
	if report.Phase == phaseOK {
		var err error
		digest, err = plugin.SHA256File(report.Path)
		if err != nil {
			report.OK = false
			report.Phase = phaseHash
			report.Error = err.Error()
		}
	}
	result := pluginAddResult{pluginReport: report, Pending: approve == ""}
	if digest != "" {
		result.SHA256 = digest
	}
	if !report.OK {
		if jsonOut {
			return emitJSON(stdout, result, 1)
		}
		printPluginReport(stdout, result.pluginReport)
		if result.SHA256 != "" {
			fmt.Fprintf(stdout, "  sha256: %s\n", result.SHA256)
		}
		return 1
	}

	if approve == "" {
		if jsonOut {
			return emitJSON(stdout, result, 0)
		}
		printPluginReport(stdout, result.pluginReport)
		fmt.Fprintf(stdout, "  sha256: %s\n", result.SHA256)
		fmt.Fprintf(stdout, "  NOT ENABLED: rerun with --approve %s to pin and enable this plugin\n", result.SHA256)
		return 0
	}

	if !strings.EqualFold(approve, result.SHA256) {
		result.OK = false
		result.Phase = phaseTrust
		result.Error = fmt.Sprintf("sha256 mismatch: supplied %s, current executable is %s; nothing was changed", approve, result.SHA256)
		if jsonOut {
			return emitJSON(stdout, result, 1)
		}
		printPluginReport(stdout, result.pluginReport)
		fmt.Fprintf(stdout, "  sha256: %s\n", result.SHA256)
		return 1
	}

	changed, err := app.SavePlugin(configPath, report.Path, result.SHA256)
	if err != nil {
		if jsonOut {
			result.OK = false
			result.Error = err.Error()
			return emitJSON(stdout, result, 1)
		}
		fmt.Fprintf(stderr, "perk-workbench plugin add: %v\n", err)
		return 1
	}
	result.Changed = changed
	if jsonOut {
		return emitJSON(stdout, result, 0)
	}
	printPluginReport(stdout, result.pluginReport)
	fmt.Fprintf(stdout, "  sha256: %s\n", result.SHA256)
	if changed {
		fmt.Fprintln(stdout, "  enabled: pinned (config updated)")
	} else {
		fmt.Fprintln(stdout, "  enabled: pinned (already configured)")
	}
	return 0
}

// pluginRemoveResult is the JSON document shape of `plugin remove`: the
// removed config entry and its canonical path, and whether the config
// was rewritten.
type pluginRemoveResult struct {
	Entry   string `json:"entry"`
	Path    string `json:"path,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Changed bool   `json:"changed"`
}

// runPluginRemove atomically removes one configured plugin and its
// trust record. NAME_OR_EXECUTABLE matches a configured entry exactly,
// or an executable resolving to exactly one configured plugin;
// ambiguous matches fail instead of removing multiple entries.
func runPluginRemove(jsonOut bool, nameOrExecutable string, stdout, stderr io.Writer) int {
	entry, canonical, changed, err := app.RemovePlugin(app.ConfigPath(), nameOrExecutable)
	if err != nil {
		result := pluginRemoveResult{Entry: nameOrExecutable, Error: err.Error()}
		if jsonOut {
			return emitJSON(stdout, result, 1)
		}
		fmt.Fprintf(stdout, "plugin remove: %v\n", err)
		return 1
	}
	result := pluginRemoveResult{Entry: entry, Path: canonical, OK: true, Changed: changed}
	if jsonOut {
		return emitJSON(stdout, result, 0)
	}
	if canonical != "" {
		fmt.Fprintf(stdout, "removed plugin %q (%s)\n", entry, canonical)
	} else {
		fmt.Fprintf(stdout, "removed plugin %q\n", entry)
	}
	return 0
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

// checkPlugin runs one plugin through the full resolve, trust-verify,
// initialize and registration-validation, shutdown lifecycle with its
// own Loader, so items never mutate or contaminate each other or the
// global driver registry. configPath is the config file path used to
// resolve relative entries ("" for explicit operands, which resolve
// against the working directory). Registration validation uses the
// side-effect-free database.ValidateShim — no global driver is ever
// installed. When the resolved canonical path has a trust record, its
// digest is verified against the current bytes BEFORE the child is
// spawned: a mismatch fails the item with the trust phase and the child
// never executes. The snapshot is taken after Loader.Close so it
// reflects the final exit/running state, and it remains available
// because the loader retains its clients.
func checkPlugin(ctx context.Context, entry, configPath string, trust map[string]string) pluginReport {
	report := pluginReport{Entry: entry}
	path, err := plugin.ResolveExecutable(entry, configPath)
	if err != nil {
		report.Phase = phaseResolve
		report.Error = err.Error()
		return report
	}
	report.Path = path

	pin, pinned := trust[path]
	if pinned {
		digest, err := plugin.SHA256File(path)
		if err != nil {
			report.Trust = trustMismatch
			report.Expected = pin
			report.Phase = phaseTrust
			report.Error = fmt.Sprintf("verifying pinned sha256: %v", err)
			return report
		}
		report.SHA256 = digest
		report.Expected = pin
		if strings.EqualFold(digest, pin) {
			report.Trust = trustMatch
		} else {
			report.Trust = trustMismatch
			report.Phase = phaseTrust
			report.Error = fmt.Sprintf("pinned executable changed: expected sha256 %s, got %s", pin, digest)
			return report
		}
	} else {
		report.Trust = trustUnpinned
	}

	insp := plugin.Inspect(ctx, path, configPath)
	report.Phase = insp.Phase
	report.Error = insp.Error
	report.OK = insp.Phase == phaseOK
	report.Capabilities = insp.Capabilities
	report.Snapshot = insp.Snapshot
	return report
}

// printPluginReport renders one plugin report as concise deterministic
// human output, visibly separating resolution, initialize, protocol,
// registration, trust, and shutdown failures. The bounded stderr tail
// is shown only when present.
func printPluginReport(w io.Writer, report pluginReport) {
	fmt.Fprintf(w, "plugin %s:\n", report.Entry)
	if report.Source != "" {
		fmt.Fprintf(w, "  source: %s\n", report.Source)
	}
	if report.Path != "" {
		fmt.Fprintf(w, "  path: %s\n", report.Path)
	}
	switch report.Trust {
	case trustMatch:
		fmt.Fprintf(w, "  trust: match (sha256 %s)\n", report.SHA256)
	case trustMismatch:
		fmt.Fprintf(w, "  trust: mismatch (expected %s, got %s)\n", report.Expected, report.SHA256)
	case trustPinned:
		fmt.Fprintf(w, "  trust: pinned (sha256 %s)\n", report.Expected)
	case trustUnpinned:
		fmt.Fprintln(w, "  trust: unpinned")
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
