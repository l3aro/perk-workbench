package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	plugindriver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	_ "modernc.org/sqlite"
)

type Service struct {
	db        *stdsql.DB
	rawTarget string
	dsn       string
	info      plugindriver.DatabaseInfo
}

type Result = plugindriver.Result
type SchemaObject = plugindriver.SchemaObject
type ColumnInfo = plugindriver.ColumnInfo
type ColumnChange = plugindriver.ColumnChange

func SanitizeDisplay(input string) string { return sanitizeDisplay(input) }

func displayRow(values []any) []*string { return DisplayRow(values) }

func Open(ctx context.Context, target string) (*Service, error) {
	dsn := target
	if target != ":memory:" {
		// Relative targets would build "file://<dir>/<name>" with the first
		// path element as URI authority, which SQLite rejects. Resolve
		// absolute so the file: URI carries no authority.
		absolute, err := filepath.Abs(target)
		if err != nil {
			return nil, fmt.Errorf("opening sqlite database: resolving target: %w", err)
		}
		dsn = (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=rw"}).String()
	}

	db, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	if target == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging sqlite database: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("reading sqlite version: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("reading sqlite version: %w", err)
	}
	return &Service{db: db, rawTarget: target, dsn: dsn, info: plugindriver.DatabaseInfo{Product: "SQLite", Version: version}}, nil
}

func (s *Service) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing sqlite database: %w", err)
	}
	return nil
}

func (s *Service) Info() plugindriver.DatabaseInfo { return s.info }

func (s *Service) ListSchema(ctx context.Context) ([]plugindriver.SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'main', type, name
		FROM sqlite_schema
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("listing schema: %w", err)
	}

	objects := []plugindriver.SchemaObject{{Database: "main", Type: "database", Name: "main"}}
	for rows.Next() {
		var object plugindriver.SchemaObject
		if err := rows.Scan(&object.Database, &object.Type, &object.Name); err != nil {
			return nil, CloseRows(rows, "scanning schema", err)
		}
		object.Type = sanitizeDisplay(object.Type)
		object.Name = sanitizeDisplay(object.Name)
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, CloseRows(rows, "iterating schema", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing schema rows: %w", err)
	}
	return objects, nil
}
