package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"net/url"

	sharedsql "github.com/l3aro/perk/internal/sql"
	_ "modernc.org/sqlite"
)

type Service struct {
	db        *stdsql.DB
	rawTarget string
	dsn       string
}

type Result = sharedsql.Result
type SchemaObject = sharedsql.SchemaObject
type ColumnInfo = sharedsql.ColumnInfo
type ColumnChange = sharedsql.ColumnChange

const (
	maxRows  = sharedsql.MaxRows
	maxRunes = sharedsql.MaxRunes
)

func SanitizeDisplay(input string) string { return sharedsql.SanitizeDisplay(input) }

func displayRow(values []any) []*string { return sharedsql.DisplayRow(values) }

func Open(ctx context.Context, target string) (*Service, error) {
	dsn := target
	if target != ":memory:" {
		dsn = (&url.URL{Scheme: "file", Path: target, RawQuery: "mode=rw"}).String()
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
	return &Service{db: db, rawTarget: target, dsn: dsn}, nil
}

func (s *Service) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing sqlite database: %w", err)
	}
	return nil
}

func (s *Service) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT type, name
		FROM sqlite_schema
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("listing schema: %w", err)
	}

	objects := []sharedsql.SchemaObject{}
	for rows.Next() {
		var object sharedsql.SchemaObject
		if err := rows.Scan(&object.Type, &object.Name); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning schema", err)
		}
		object.Type = sharedsql.SanitizeDisplay(object.Type)
		object.Name = sharedsql.SanitizeDisplay(object.Name)
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating schema", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing schema rows: %w", err)
	}
	return objects, nil
}
