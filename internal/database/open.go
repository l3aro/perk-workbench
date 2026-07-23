package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/l3aro/perk/internal/mysql"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

// Open connects to target and returns its schema for the initial workbench view.
func Open(ctx context.Context, target string) (sharedsql.Opened, error) {
	if dsn, ok := strings.CutPrefix(target, "mysql:"); ok {
		return open(ctx, dsn, func(ctx context.Context, target string) (sharedsql.Service, error) {
			return mysql.Open(ctx, target)
		})
	}

	resolved, err := resolveSQLiteTarget(target)
	if err != nil {
		return sharedsql.Opened{}, err
	}
	return open(ctx, resolved, func(ctx context.Context, target string) (sharedsql.Service, error) {
		return sqlite.Open(ctx, target)
	})
}

func open(ctx context.Context, target string, openService func(context.Context, string) (sharedsql.Service, error)) (sharedsql.Opened, error) {
	service, err := openService(ctx, target)
	if err != nil {
		return sharedsql.Opened{}, fmt.Errorf("opening database: %w", err)
	}

	objects, err := service.ListSchema(ctx)
	if err != nil {
		if closeErr := service.Close(); closeErr != nil {
			return sharedsql.Opened{}, fmt.Errorf("listing schema: %w", errors.Join(err, closeErr))
		}
		return sharedsql.Opened{}, fmt.Errorf("listing schema: %w", err)
	}

	return sharedsql.Opened{Target: target, Service: service, Info: service.Info(), Objects: objects}, nil
}

func resolveSQLiteTarget(target string) (string, error) {
	if target == ":memory:" {
		return target, nil
	}

	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("database target is not a regular file")
	}
	return resolved, nil
}
