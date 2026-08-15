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

const usage = `Usage: perk-workbench [--read-only] [database]

Connect to a database and browse, query, and edit it.

Commands:
  plugin list [--json]                    List configured plugin executables
  plugin inspect [--json] EXECUTABLE      Inspect one plugin over perk/v1
  plugin doctor [--json] [EXECUTABLE...]  Check configured plugins or given executables
  plugin test [--json] EXECUTABLE        Conformance-test one plugin over perk/v1

Run "perk-workbench plugin --help" for plugin command help.
`

func versionOutput() string {
	return "perk-workbench " + version + "\n"
}

func parseTarget(args []string) (target string, readOnly bool, _ error) {
	nonFlags := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--read-only", "-r":
			readOnly = true
		default:
			nonFlags = append(nonFlags, a)
		}
	}
	switch len(nonFlags) {
	case 0:
		return "", readOnly, nil
	case 1:
		return nonFlags[0], readOnly, nil
	default:
		return "", false, fmt.Errorf("expected zero or one target, got %d", len(nonFlags))
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
	target, readOnly, err := parseTarget(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if target == "" {
		// .env in the working directory is a fallback for unset variables;
		// real environment variables take precedence and a CLI target
		// overrides everything.
		if envTarget := environmentTarget(preferEnv(os.LookupEnv, loadDotEnv(".env"))); envTarget != "" {
			target = envTarget
		}
	}
	if err := run(target, readOnly); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

func run(target string, readOnly bool) error {
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
	app.SetAppConfig(config)
	// External driver plugins load after config resolution and before the
	// model is built, because the connection form enumerates registered
	// drivers during construction. Rejected entries are logged and startup
	// continues with the built-in drivers.
	loader := loadPlugins(ctx, config.Plugins, database.RegisterShim)
	client, history, err := loadAI()
	if err != nil {
		return errors.Join(err, loader.Close())
	}

	model := app.New(target, ctx, database.Open, readOnly)
	model.SetKeybindings(keybindings)
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

// loadPlugins starts every plugin child listed in config. Rejected
// entries are logged and skipped: a broken plugin must never block
// startup, which continues with the built-in drivers. The returned
// loader is never nil and must be closed after the program exits.
func loadPlugins(ctx context.Context, entries []string, register func(database.Shim) error) *plugin.Loader {
	loader, errs := plugin.Load(ctx, app.ConfigPath(), entries, register)
	for _, err := range errs {
		log.Error("loading plugin", err)
	}
	return loader
}

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
