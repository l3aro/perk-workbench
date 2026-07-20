package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type ColumnInfo struct {
	Name         string
	Type         string
	Nullable     bool
	DefaultValue *string
	PrimaryKey   int
}

func (s *Service) TableInfo(ctx context.Context, name string) ([]ColumnInfo, error) {
	if s.driver == "mysql" {
		return s.mysqlTableInfo(ctx, name)
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(s.driver, name)+")")
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}

	columns := []ColumnInfo{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var column ColumnInfo
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &column.Name, &column.Type, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, closeRows(rows, "scanning table info", err)
		}
		column.Name = SanitizeDisplay(column.Name)
		column.Type = SanitizeDisplay(column.Type)
		column.Nullable = notNull == 0
		column.PrimaryKey = primaryKey
		if defaultValue.Valid {
			value := SanitizeDisplay(defaultValue.String)
			column.DefaultValue = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating table info", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table info rows: %w", err)
	}
	return columns, nil
}

func (s *Service) BrowseTable(ctx context.Context, name string, offset, limit int) (Result, error) {
	if offset < 0 || limit < 1 {
		return Result{}, fmt.Errorf("invalid page: offset=%d limit=%d", offset, limit)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+quoteIdentifier(s.driver, name)+" LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return Result{}, fmt.Errorf("browsing table: %w", err)
	}
	return collectRows(rows)
}

func (s *Service) mysqlTableInfo(ctx context.Context, name string) ([]ColumnInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT column_name, column_type, is_nullable, column_default, column_key
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, name)
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}

	columns := []ColumnInfo{}
	for rows.Next() {
		var column ColumnInfo
		var nullable, key string
		var defaultValue sql.NullString
		if err := rows.Scan(&column.Name, &column.Type, &nullable, &defaultValue, &key); err != nil {
			return nil, closeRows(rows, "scanning table info", err)
		}
		column.Name = SanitizeDisplay(column.Name)
		column.Type = SanitizeDisplay(column.Type)
		column.Nullable = nullable == "YES"
		if key == "PRI" {
			column.PrimaryKey = 1
		}
		if defaultValue.Valid {
			value := SanitizeDisplay(defaultValue.String)
			column.DefaultValue = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating table info", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table info rows: %w", err)
	}
	return columns, nil
}

func quoteIdentifier(driver, name string) string {
	if driver == "mysql" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
