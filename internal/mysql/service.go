package mysql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type Service struct{ db *stdsql.DB }

func Open(ctx context.Context, dsn string) (*Service, error) {
	db, err := stdsql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening mysql database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging mysql database: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("pinging mysql database: %w", err)
	}
	return &Service{db: db}, nil
}

func (s *Service) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing mysql database: %w", err)
	}
	return nil
}

func (s *Service) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT CASE table_type WHEN 'BASE TABLE' THEN 'table' ELSE 'view' END, table_name
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_type, table_name`)
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

func (s *Service) Execute(ctx context.Context, statement string) (result sharedsql.Result, err error) {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return sharedsql.Result{}, err
	}
	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("acquiring mysql connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing mysql connection: %w", closeErr))
				return
			}
			result = sharedsql.Result{}
			err = fmt.Errorf("closing mysql connection: %w", closeErr)
		}
	}()
	if !ReturnsRows(statement) {
		execution, err := conn.ExecContext(ctx, statement)
		if err != nil {
			return sharedsql.Result{}, fmt.Errorf("executing statement: %w", err)
		}
		result.RowsAffected, err = execution.RowsAffected()
		if err != nil {
			return sharedsql.Result{}, fmt.Errorf("reading affected rows: %w", err)
		}
		result.Duration = time.Since(started)
		return result, nil
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("executing statement: %w", err)
	}
	result, err = sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.Duration = time.Since(started)
	return result, nil
}

func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT column_name, column_type, is_nullable, column_default, column_key
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, name)
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}
	columns := []sharedsql.ColumnInfo{}
	for rows.Next() {
		var column sharedsql.ColumnInfo
		var nullable, key string
		var defaultValue stdsql.NullString
		if err := rows.Scan(&column.Name, &column.Type, &nullable, &defaultValue, &key); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		column.Name = sharedsql.SanitizeDisplay(column.Name)
		column.Type = sharedsql.SanitizeDisplay(column.Type)
		column.Nullable = nullable == "YES"
		if key == "PRI" {
			column.PrimaryKey = 1
		}
		if defaultValue.Valid {
			value := sharedsql.SanitizeDisplay(defaultValue.String)
			column.DefaultValue = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating table info", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table info rows: %w", err)
	}
	return columns, nil
}

func (s *Service) BrowseTable(ctx context.Context, name string, offset, limit int) (sharedsql.Result, error) {
	if offset < 0 || limit < 1 {
		return sharedsql.Result{}, fmt.Errorf("invalid page: offset=%d limit=%d", offset, limit)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+quoteIdentifier(name)+" LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("browsing table: %w", err)
	}
	return sharedsql.CollectRows(rows)
}

func ReturnsRows(statement string) bool {
	for {
		statement = strings.TrimSpace(strings.TrimLeft(statement, "("))
		switch {
		case strings.HasPrefix(statement, "--"):
			if index := strings.IndexByte(statement, '\n'); index >= 0 {
				statement = statement[index+1:]
				continue
			}
			return false
		case strings.HasPrefix(statement, "/*"):
			index := strings.Index(statement[2:], "*/")
			if index < 0 {
				return false
			}
			statement = statement[index+4:]
			continue
		}
		break
	}
	if index := strings.IndexAny(statement, " \t\n\r("); index >= 0 {
		statement = statement[:index]
	}
	switch strings.ToUpper(statement) {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH":
		return true
	default:
		return false
	}
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
