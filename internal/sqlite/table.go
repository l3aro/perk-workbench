package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	return tableInfo(ctx, s.db, name)
}

type tableInfoQuerier interface {
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
}

func tableInfo(ctx context.Context, queryer tableInfoQuerier, name string) ([]sharedsql.ColumnInfo, error) {
	rows, err := queryer.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(name)+")")
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}

	columns := []sharedsql.ColumnInfo{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var column sharedsql.ColumnInfo
		var defaultValue stdsql.NullString
		if err := rows.Scan(&cid, &column.Name, &column.Type, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		column.Name = sharedsql.SanitizeDisplay(column.Name)
		column.Type = sharedsql.SanitizeDisplay(column.Type)
		column.Nullable = notNull == 0
		column.PrimaryKey = primaryKey
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

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
