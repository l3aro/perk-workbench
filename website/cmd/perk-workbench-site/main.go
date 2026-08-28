package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/l3aro/perk-workbench-site/internal/site"
)

const defaultPort = 8080

var version = "devel"

func main() {
	if err := run(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(version string) error {
	port, err := parsePort(os.Getenv("PORT"))
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: site.New(version),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve website: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown website: %w", err)
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve website: %w", err)
		}
		return nil
	}
}

func parsePort(value string) (int, error) {
	if value == "" {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid PORT %q: must be a number from 1 through 65535", value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid PORT %q: must be between 1 and 65535", value)
	}
	return port, nil
}
