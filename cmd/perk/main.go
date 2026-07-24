package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk/internal/clipboard"
	"github.com/l3aro/perk/internal/database"
	"github.com/l3aro/perk/internal/workbench"
)

func parseTarget(args []string) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("expected zero or one target, got %d", len(args))
	}
}

func loadKeybindings() (workbench.Keybindings, error) {
	path := workbench.KeybindingsPath()
	if path == "" {
		return workbench.DefaultKeybindings(), nil
	}
	return workbench.LoadKeybindings(path)
}

func main() {
	target, err := parseTarget(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(target string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Native clipboard support is optional: OSC 52 remains available in
	// headless environments such as the development container.
	_ = clipboard.Init()

	keybindings, err := loadKeybindings()
	if err != nil {
		return err
	}

	model := workbench.New(target, ctx, database.Open)
	model.SetKeybindings(keybindings)

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
	if errors.Is(runErr, tea.ErrProgramKilled) {
		runErr = nil
	}
	return errors.Join(runErr, closeErr)
}
