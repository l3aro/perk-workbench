package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

const (
	maxRows  = 500
	maxRunes = 300
)

type Service struct {
	db        *sql.DB
	driver    string
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
	return open(ctx, "sqlite", target, dsn)
}

func OpenMySQL(ctx context.Context, dsn string) (*Service, error) {
	return open(ctx, "mysql", dsn, dsn)
}

func open(ctx context.Context, driver, target, dsn string) (*Service, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s database: %w", driver, err)
	}
	if driver == "sqlite" && target == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging %s database: %w", driver, errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("pinging %s database: %w", driver, err)
	}

	return &Service{db: db, driver: driver, rawTarget: target, dsn: dsn}, nil
}

func (s *Service) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing %s database: %w", s.driver, err)
	}
	return nil
}

func (s *Service) ListSchema(ctx context.Context) ([]SchemaObject, error) {
	query := `
		SELECT type, name
		FROM sqlite_schema
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name`
	if s.driver == "mysql" {
		query = `
			SELECT CASE table_type WHEN 'BASE TABLE' THEN 'table' ELSE 'view' END, table_name
			FROM information_schema.tables
			WHERE table_schema = DATABASE() AND table_type IN ('BASE TABLE', 'VIEW')
			ORDER BY table_type, table_name`
	}
	rows, err := s.db.QueryContext(ctx, query)
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
