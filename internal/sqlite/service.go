package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

const (
	maxRows  = 500
	maxRunes = 300
)

type Service struct {
	db        *sql.DB
	rawTarget string
	dsn       string
}

type Result struct {
	Columns      []string
	Rows         [][]*string
	RowsAffected int64
	Duration     time.Duration
	Truncated    bool
}

type SchemaObject struct {
	Type string
	Name string
}

func Open(ctx context.Context, target string) (*Service, error) {
	dsn := target
	if target != ":memory:" {
		dsn = (&url.URL{Scheme: "file", Path: target, RawQuery: "mode=rw"}).String()
	}

	db, err := sql.Open("sqlite", dsn)
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

func (s *Service) ListSchema(ctx context.Context) ([]SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT type, name
		FROM sqlite_schema
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("listing schema: %w", err)
	}

	objects := []SchemaObject{}
	for rows.Next() {
		var object SchemaObject
		if err := rows.Scan(&object.Type, &object.Name); err != nil {
			if closeErr := rows.Close(); closeErr != nil {
				return nil, fmt.Errorf("scanning schema: %w", errors.Join(err, closeErr))
			}
			return nil, fmt.Errorf("scanning schema: %w", err)
		}
		object.Type = SanitizeDisplay(object.Type)
		object.Name = SanitizeDisplay(object.Name)
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		if closeErr := rows.Close(); closeErr != nil {
			return nil, fmt.Errorf("iterating schema: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("iterating schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing schema rows: %w", err)
	}
	return objects, nil
}
