package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// Open connects to target and returns its schema for the initial workbench view.
// The driver group routes the target form to a registered driver; anything
// without a registered target form opens as SQLite after path resolution.
// The returned Opened carries the matched driver's query language (the
// SQLite fallback carries the legacy SQL default) and workspace tab
// capability.
func Open(ctx context.Context, target string) (sharedsql.Opened, error) {
	if spec, dsn, ok := Match(target); ok {
		return open(ctx, dsn, spec.Open, spec.QueryLanguage, spec.Workspace)
	}

	resolved, err := resolveSQLiteTarget(target)
	if err != nil {
		return sharedsql.Opened{}, err
	}
	sqliteSpec, ok := ByName("sqlite")
	if !ok {
		return sharedsql.Opened{}, errors.New("sqlite driver not registered")
	}
	return open(ctx, resolved, sqliteSpec.Open, sqliteSpec.QueryLanguage, sqliteSpec.Workspace)
}

func open(ctx context.Context, target string, openService func(context.Context, string) (sharedsql.Service, error), language sharedsql.QueryLanguage, workspace *sharedsql.WorkspaceCapability) (sharedsql.Opened, error) {
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

	return sharedsql.Opened{Target: target, Service: service, Info: service.Info(), Objects: objects, QueryLanguage: language, Workspace: workspace}, nil
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
