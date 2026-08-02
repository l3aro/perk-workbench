package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/clipboard"
	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/workbench"
)

// version is injected at build time with -ldflags=-X main.version=<version>.
// A bare build reports "devel".
var version = "devel"

const usage = "Usage: perk-workbench [--read-only] [database]\n"

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

func loadKeybindings() (workbench.Keybindings, error) {
	path := workbench.KeybindingsPath()
	if path == "" {
		return workbench.DefaultKeybindings(), nil
	}
	return workbench.LoadKeybindings(path)
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

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Print(usage)
		return
	}
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Print(versionOutput())
		return
	}
	target, readOnly, err := parseTarget(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	client, history, err := loadAI()
	if err != nil {
		return err
	}

	model := workbench.New(target, ctx, database.Open, readOnly)
	model.SetKeybindings(keybindings)
	if client != nil {
		model.SetAI(client, history)
	}

	final, runErr := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	).Run()

	stop()
	var closeErr error
	if finalModel, ok := final.(workbench.Model); ok {
		if service := finalModel.Service(); service != nil {
			closeErr = service.Close()
		}
	}
	if history != nil {
		closeErr = errors.Join(closeErr, history.Close())
	}
	if errors.Is(runErr, tea.ErrProgramKilled) {
		runErr = nil
	}
	return errors.Join(runErr, closeErr)
}
