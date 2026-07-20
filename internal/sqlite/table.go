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
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(name)+")")
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
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+quoteIdentifier(name)+" LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return Result{}, fmt.Errorf("browsing table: %w", err)
	}
	return collectRows(rows)
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
