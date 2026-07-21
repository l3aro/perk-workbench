package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"slices"
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
		if primaryKey > 0 {
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexPrimaryKey}
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
	indexKinds, err := tableIndexKinds(ctx, queryer, name)
	if err != nil {
		return nil, err
	}
	for index := range columns {
		for _, kind := range indexKinds[columns[index].Name] {
			if !slices.Contains(columns[index].Indexes, kind) {
				columns[index].Indexes = append(columns[index].Indexes, kind)
			}
		}
	}
	return columns, nil
}

type tableIndex struct {
	name string
	kind sharedsql.IndexKind
}

func tableIndexKinds(ctx context.Context, queryer tableInfoQuerier, table string) (map[string][]sharedsql.IndexKind, error) {
	rows, err := queryer.QueryContext(ctx, "PRAGMA index_list("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("reading table indexes: %w", err)
	}
	indexes := []tableIndex{}
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table indexes", err)
		}
		kind := sharedsql.IndexRegular
		if origin == "pk" {
			kind = sharedsql.IndexPrimaryKey
		} else if unique != 0 {
			kind = sharedsql.IndexUnique
		}
		indexes = append(indexes, tableIndex{name: name, kind: kind})
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating table indexes", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table index rows: %w", err)
	}

	columnIndexes := map[string][]sharedsql.IndexKind{}
	for _, index := range indexes {
		rows, err := queryer.QueryContext(ctx, "PRAGMA index_info("+quoteIdentifier(index.name)+")")
		if err != nil {
			return nil, fmt.Errorf("reading index %q columns: %w", index.name, err)
		}
		for rows.Next() {
			var sequence, columnID int
			var columnName stdsql.NullString
			if err := rows.Scan(&sequence, &columnID, &columnName); err != nil {
				return nil, sharedsql.CloseRows(rows, "scanning index columns", err)
			}
			if columnName.Valid {
				name := sharedsql.SanitizeDisplay(columnName.String)
				if !slices.Contains(columnIndexes[name], index.kind) {
					columnIndexes[name] = append(columnIndexes[name], index.kind)
				}
			}
		}
		if err := rows.Err(); err != nil {
			return nil, sharedsql.CloseRows(rows, "iterating index columns", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("closing index %q columns: %w", index.name, err)
		}
	}
	return columnIndexes, nil
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
