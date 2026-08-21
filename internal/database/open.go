package database

import (
	"context"
	"errors"
	"fmt"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"os"
	"path/filepath"
	"strings"
)

// Open connects to target through the explicitly selected plugin ID and
// returns its schema for the initial workbench view.
func Open(ctx context.Context, pluginID, target string) (sharedsql.Opened, error) {
	spec, ok := ByPlugin(pluginID)
	if !ok {
		if pluginID == "sqlite" {
			return sharedsql.Opened{}, errors.New("sqlite plugin not registered")
		}
		return sharedsql.Opened{}, fmt.Errorf("database plugin %q not registered", pluginID)
	}
	dsn := target
	matches := Matches(target)
	for _, match := range matches {
		if match.Spec.PluginID == pluginID {
			dsn = match.DSN
			break
		}
	}
	if spec.PluginID == "sqlite" && len(matches) == 0 {
		var err error
		dsn, err = resolveSQLiteTarget(target)
		if err != nil {
			return sharedsql.Opened{}, err
		}
	}
	return open(ctx, dsn, spec.Open, spec.QueryLanguage, spec.Workspace)
}

// ResolvePlugin deterministically chooses the plugin for a direct target.
// Unprefixed targets use the sqlite plugin; a prefixed target must have
// exactly one matching plugin instance.
func ResolvePlugin(target string) (string, error) {
	matches := Matches(target)
	switch len(matches) {
	case 1:
		return matches[0].Spec.PluginID, nil
	case 2:
		return "", ambiguousTargetError(matches)
	case 0:
		if _, ok := ByPlugin("sqlite"); !ok {
			return "", errors.New("sqlite plugin not registered")
		}
		return "sqlite", nil
	default:
		return "", ambiguousTargetError(matches)
	}
}

func ambiguousTargetError(matches []targetMatch) error {
	ids := make([]string, len(matches))
	for i, match := range matches {
		ids[i] = match.Spec.PluginID
	}
	return fmt.Errorf("database target is ambiguous; matching plugins: %s; open the connection form or invoke --select", strings.Join(ids, ", "))
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
