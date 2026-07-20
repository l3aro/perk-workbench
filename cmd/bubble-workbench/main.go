package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"bubble-workbench/internal/workbench"
	tea "charm.land/bubbletea/v2"
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
	model := workbench.New(target, workbench.Open(ctx))
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
