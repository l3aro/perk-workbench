package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/clipboard"
	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	"github.com/l3aro/perk-workbench/internal/log"
	app "github.com/l3aro/perk-workbench/internal/workbench/app"
)

// version is injected at build time with -ldflags=-X main.version=<version>.
// A bare build reports "devel".
var version = "devel"

const usage = `Usage: perk-workbench [--read-only] [--select] [--pin] [database]

Connect to a database and browse, query, and edit it.

Commands:
  plugin list [--json]                    List configured plugin executables
  plugin inspect [--json] EXECUTABLE      Inspect one plugin over perk/v1
  plugin add [--json] [--approve SHA256] EXECUTABLE
                                          Preview or pin and enable a plugin
  plugin remove [--json] NAME_OR_EXECUTABLE
                                          Remove a configured plugin
  plugin doctor [--json] [EXECUTABLE...]  Check configured plugins or given executables
  plugin test [--json] EXECUTABLE        Conformance-test one plugin over perk/v1

Options:
  --select           Choose a saved connection interactively from the CLI.
                     Cannot be combined with a database target.
  --pin              Lock the session: every in-app quit affordance
                     (Ctrl+C, Ctrl+Q, the header quit button, the palette
                     quit entry, the footer hints) is disabled. The
                     program still exits when its context is cancelled,
                     so the embedding host owns the session lifecycle.
  --version, -v   Print the build version: "perk-workbench <version>"
                  with <version> injected at build time via
                  -ldflags "-X main.version=<version>", or
                  "perk-workbench devel" when nothing was injected
  -h, --help      Show this help

Run "perk-workbench plugin --help" for plugin command help.
`

// hostVersion is the host build identity reported by --version and
// carried by the plugin test evidence document: the version injected
// at build time via -ldflags=-X main.version=<version>, or "devel"
// when no injection happened. An uninjected build is honestly labeled.
func hostVersion() string {
	return "perk-workbench " + version
}

func versionOutput() string {
	return hostVersion() + "\n"
}

func parseTarget(args []string) (target string, readOnly, selectMode, pin bool, _ error) {
	nonFlags := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--read-only", "-r":
			readOnly = true
		case "--select":
			selectMode = true
		case "--pin":
			pin = true
		default:
			nonFlags = append(nonFlags, a)
		}
	}
	if selectMode && len(nonFlags) > 0 {
		return "", false, false, false, fmt.Errorf("--select cannot be combined with a database target")
	}
	switch len(nonFlags) {
	case 0:
		return "", readOnly, selectMode, pin, nil
	case 1:
		return nonFlags[0], readOnly, selectMode, pin, nil
	default:
		return "", false, false, false, fmt.Errorf("expected zero or one target, got %d", len(nonFlags))
	}
}

// environmentTarget builds a connection target from Laravel-style DB_*
// environment variables, or "" when no complete supported configuration is
// present. Unsupported drivers and incomplete configs fall through to the
// picker rather than failing startup on ambient unrelated variables.
func environmentTarget(lookup func(string) (string, bool)) string {
	get := func(key string) string {
		v, ok := lookup(key)
		if !ok {
			return ""
		}
		return v
	}
	switch strings.ToLower(strings.TrimSpace(get("DB_CONNECTION"))) {
	case "sqlite":
		return strings.TrimSpace(get("DB_DATABASE"))
	case "mysql":
		host := strings.TrimSpace(get("DB_HOST"))
		user := strings.TrimSpace(get("DB_USERNAME"))
		if host == "" || user == "" {
			return ""
		}
		port := strings.TrimSpace(get("DB_PORT"))
		if port == "" {
			port = "3306"
		} else if !validPort(port) {
			return ""
		}
		config := mysql.NewConfig()
		config.User = user
		config.Passwd = get("DB_PASSWORD") // untrimmed
		config.Net = "tcp"
		config.Addr = net.JoinHostPort(host, port)
		config.DBName = strings.TrimSpace(get("DB_DATABASE"))
		config.TLSConfig = "false" // match the connection form's default
		return "mysql:" + config.FormatDSN()
	case "pgsql":
		host := strings.TrimSpace(get("DB_HOST"))
		user := strings.TrimSpace(get("DB_USERNAME"))
		if host == "" || user == "" {
			return ""
		}
		port := strings.TrimSpace(get("DB_PORT"))
		if port == "" {
			port = "5432"
		} else if !validPort(port) {
			return ""
		}
		target := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(user, get("DB_PASSWORD")), // untrimmed
			Host:   net.JoinHostPort(host, port),
			Path:   strings.TrimSpace(get("DB_DATABASE")),
		}
		target.RawQuery = url.Values{"sslmode": {"disable"}}.Encode() // match the connection form's default
		return "postgres:" + target.String()
	default:
		return ""
	}
}

func validPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}

// loadDotEnv reads KEY=VALUE pairs from path (typically ".env" in the
// working directory). A missing file is not an error; malformed lines are
// skipped. Supported syntax: blank lines, # comments, an optional "export "
// prefix, and single- or double-quoted values.
func loadDotEnv(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	vars := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			vars[key] = value
		}
	}
	return vars
}

// preferEnv returns a lookup that consults the real environment first and
// falls back to .env file values, so exported variables always win.
func preferEnv(real func(string) (string, bool), file map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if value, ok := real(key); ok {
			return value, true
		}
		value, ok := file[key]
		return value, ok
	}
}

func loadKeybindings() (app.Keybindings, error) {
	path := app.KeybindingsPath()
	if path == "" {
		return app.DefaultKeybindings(), nil
	}
	return app.LoadKeybindings(path)
}

func loadConfig() (app.Config, error) {
	path := app.ConfigPath()
	if path == "" {
		return app.Config{}, nil
	}
	return app.LoadConfig(path)
}

func loadAI() (*ai.Client, *ai.History, error) {
	config, err := ai.Load()
	if err != nil {
		return nil, nil, err
	}
	if _, ok := config.Agents["assistant"]; !ok {
		return nil, nil, nil
	}
	client, err := ai.NewClient(config)
	if err != nil {
		return nil, nil, err
	}
	path, err := ai.HistoryPath()
	if err != nil {
		return nil, nil, err
	}
	history, err := ai.OpenHistory(path)
	if err != nil {
		return nil, nil, err
	}
	return client, history, nil
}

// dispatch runs one CLI invocation and returns its exit status: 0
// success, 1 operational or plugin failure, 2 usage error. It is the
// testable face of main, which stays a thin os.Exit wrapper. Command
// helpers never call os.Exit themselves.
func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "--plugin" {
		return dispatchSelfPlugin(args[1:], os.Stdin, stdout, stderr)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Fprint(stdout, versionOutput())
		return 0
	}
	if len(args) > 0 && args[0] == "plugin" {
		return dispatchPlugin(args[1:], stdout, stderr)
	}
	target, readOnly, selectMode, pin, err := parseTarget(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if selectMode {
		// CLI-selected connection; --pin (if given) locks the session so
		// the embedding host (e.g. a demo website over xterm.js) owns the
		// session lifecycle.
		selected, err := selectConnection(stderr)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if selected == "" {
			// The user cancelled the picker; nothing ran.
			return 0
		}
		if err := run(selected, readOnly, pin); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if target == "" {
		// .env in the working directory is a fallback for unset variables;
		// real environment variables take precedence and a CLI target
		// overrides everything.
		if envTarget := environmentTarget(preferEnv(os.LookupEnv, loadDotEnv(".env"))); envTarget != "" {
			target = envTarget
		}
	}
	if err := run(target, readOnly, pin); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

func run(target string, readOnly, noQuit bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Native clipboard support is optional: OSC 52 remains available in
	// headless environments such as the development container.
	_ = clipboard.Init()

	keybindings, err := loadKeybindings()
	if err != nil {
		return err
	}
	config, err := loadConfig()
	if err != nil {
		return err
	}
	// Auto-following (the default) resolves the effective appearance from
	// the system theme at startup. Detection is best-effort: on failure it
	// returns "" and SetAppConfig falls back to the persisted appearance.
	if config.AutoTheme == nil || *config.AutoTheme {
		app.SetSystemAppearance(detectSystemAppearance())
	}
	app.SetAppConfig(config)

	loader := loadPlugins(ctx, config, database.RegisterShim)

	client, history, err := loadAI()
	if err != nil {
		return errors.Join(err, loader.Close())
	}

	model := app.New(target, ctx, database.Open, readOnly)
	model.SetKeybindings(keybindings)
	if noQuit {
		model.SetNoQuit(true)
	}
	// The loader is the live plugin lifecycle controller: the Plugins
	// manager's Status view and Restart act through it, and the app
	// never owns child processes.
	model.SetPluginControl(loader)
	if client != nil {
		model.SetAI(client, history)
	}

	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)
	// Logs from async commands must wake the idle loop into a notification.
	app.AttachLogProgram(program)
	final, runErr := program.Run()

	stop()
	var service interface{ Close() error }
	if finalModel, ok := final.(app.Model); ok {
		service = finalModel.Service()
	}
	closeErr := closeProgram(service, loader, history)
	if errors.Is(runErr, tea.ErrProgramKilled) {
		runErr = nil
	}
	return errors.Join(runErr, closeErr)
}

// pluginEntries converts persisted descriptors into child-process entries.
// Built-ins intentionally execute this same binary with explicit plugin
// arguments; they are not in-process drivers.
func pluginEntries(config app.Config) []plugin.Entry {
	self, err := os.Executable()
	if err != nil {
		log.Error("resolving self-hosted plugin executable", err)
		return nil
	}
	entries := make([]plugin.Entry, 0, len(config.Plugins))
	for _, descriptor := range config.Plugins {
		switch {
		case descriptor.Builtin != "":
			entries = append(entries, plugin.Entry{
				Config:     descriptor.Builtin,
				Display:    descriptor.Builtin,
				Executable: self,
				Args:       []string{"--plugin", descriptor.Builtin},
				Builtin:    true,
			})
		default:
			entries = append(entries, plugin.Entry{
				Config:     descriptor.Path,
				Display:    descriptor.Path,
				Executable: descriptor.Path,
				SHA256:     descriptor.SHA256,
			})
		}
	}
	return entries
}

// loadPlugins starts every configured plugin child. External pins are
// verified immediately before spawn; built-ins use the self-hosted argv.
func loadPlugins(ctx context.Context, config app.Config, register func(database.Shim) error) *plugin.Loader {
	loader, errs := plugin.Load(ctx, app.ConfigPath(), pluginEntries(config), register)
	for _, err := range errs {
		log.Error("loading plugin", err)
	}
	return loader
}

// detectSystemAppearance asks the terminal for its background color via the
// OSC 11 query and derives a light/dark appearance. It returns "" whenever
// detection is unavailable: no controlling tty, the terminal does not answer
// within a short window, or the reply cannot be parsed. Callers fall back to
// the persisted appearance in that case, so a failed query never forces a
// wrong theme.
func detectSystemAppearance() string {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return ""
	}
	defer tty.Close()
	out, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return ""
	}
	defer out.Close()
	if _, err := out.WriteString("\x1b]11;?\x1b\\"); err != nil {
		return ""
	}
	result := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, readErr := tty.Read(buf)
		if readErr != nil && n == 0 {
			result <- ""
			return
		}
		result <- string(buf[:n])
	}()
	select {
	case raw := <-result:
		return app.AppearanceFromBackground(parseOSC11Payload(raw))
	case <-time.After(400 * time.Millisecond):
		return ""
	}
}

// parseOSC11Payload extracts the rune payload of an OSC 11 background-color
// response (the text between "]11;" and its terminator) from a raw byte
// read. It tolerates leading echoes and a BEL or ST terminator.
func parseOSC11Payload(raw string) string {
	i := strings.Index(raw, "]11;")
	if i < 0 {
		return ""
	}
	payload := raw[i+4:]
	for _, term := range []string{"\x07", "\x1b\\", "\x1b", "\n", "\r"} {
		if j := strings.Index(payload, term); j >= 0 {
			payload = payload[:j]
			break
		}
	}
	payload = strings.TrimSpace(payload)
	if strings.HasPrefix(payload, "rgb:") {
		return payload
	}
	return ""
}

// The loader is the production PluginControl: live statuses, restart,
// and the service-to-entry mapping all come from it.
var _ app.PluginControl = (*plugin.Loader)(nil)

// closeProgram tears down the opened database service first — an active
// plugin session receives its perk/v1/close before the child is
// terminated — then the plugin loader, then the AI history. Cleanup
// errors are joined into one error.
func closeProgram(service, loader, history interface{ Close() error }) error {
	var closeErr error
	for _, closer := range []interface{ Close() error }{service, loader, history} {
		if isNilCloser(closer) {
			continue
		}
		closeErr = errors.Join(closeErr, closer.Close())
	}
	return closeErr
}

// isNilCloser reports whether closer is nil or a typed nil wrapped in the
// interface — e.g. the (*ai.History)(nil) a disabled AI provider returns.
// Comparing the interface against nil alone misses typed nils and would
// call Close on a nil receiver.
func isNilCloser(closer interface{ Close() error }) bool {
	if closer == nil {
		return true
	}
	value := reflect.ValueOf(closer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}
