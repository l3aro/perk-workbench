package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
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
