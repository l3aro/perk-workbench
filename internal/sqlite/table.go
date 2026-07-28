package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"slices"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	return tableInfo(ctx, s.db, name)
}

type tableInfoQuerier interface {
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
}

func tableInfo(ctx context.Context, queryer tableInfoQuerier, name string) ([]sharedsql.ColumnInfo, error) {
	rows, err := queryer.QueryContext(ctx, "PRAGMA table_xinfo("+quoteIdentifier(name)+")")
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}

	columns := []sharedsql.ColumnInfo{}
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var column sharedsql.ColumnInfo
		var defaultValue stdsql.NullString
		if err := rows.Scan(&cid, &column.Name, &column.Type, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		column.Name = sharedsql.SanitizeDisplay(column.Name)
		column.Type = sharedsql.SanitizeDisplay(column.Type)
		column.Nullable = notNull == 0
		column.PrimaryKey = primaryKey
		switch hidden {
		case 2:
			column.Attributes = "GENERATED VIRTUAL"
		case 3:
			column.Attributes = "GENERATED STORED"
		}
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

func (s *Service) BrowseTable(ctx context.Context, name string, options sharedsql.BrowseOptions) (sharedsql.Result, error) {
	if options.Offset < 0 || options.Limit < 1 || options.Limit > sharedsql.MaxRows {
		return sharedsql.Result{}, fmt.Errorf("invalid browse range: offset=%d limit=%d", options.Offset, options.Limit)
	}
	statement := "SELECT * FROM " + quoteIdentifier(name)
	args := make([]any, 0, len(options.Filters)+2)
	valid := make(map[string]bool, len(options.Columns))
	for _, column := range options.Columns {
		valid[column] = true
	}
	if len(options.Filters) > 0 {
		terms := make([]string, 0, len(options.Filters))
		for _, filter := range options.Filters {
			if !valid[filter.Column] {
				return sharedsql.Result{}, fmt.Errorf("invalid browse filter column: %s", filter.Column)
			}
			column := quoteIdentifier(filter.Column)
			switch filter.Operator {
			case sharedsql.BrowseFilterLike, sharedsql.BrowseFilterNotLike:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterLess, sharedsql.BrowseFilterLessEqual, sharedsql.BrowseFilterGreater, sharedsql.BrowseFilterGreaterEqual:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull:
				terms = append(terms, column+" "+string(filter.Operator))
			default:
				return sharedsql.Result{}, fmt.Errorf("invalid browse filter operator: %q", filter.Operator)
			}
		}
		statement += " WHERE " + strings.Join(terms, " AND ")
	}
	if len(options.Sorts) > 0 {
		orders := make([]string, 0, len(options.Sorts))
		for _, sort := range options.Sorts {
			if !valid[sort.Column] {
				continue
			}
			order := quoteIdentifier(sort.Column)
			if sort.Descending {
				order += " DESC"
			}
			orders = append(orders, order)
		}
		if len(orders) > 0 {
			statement += " ORDER BY " + strings.Join(orders, ", ")
		}
	}
	args = append(args, options.Limit+1, options.Offset)
	rows, err := s.db.QueryContext(ctx, statement+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("browsing table: %w", err)
	}
	result, err := sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.HasMore = len(result.Rows) > options.Limit
	if result.HasMore {
		result.Rows = result.Rows[:options.Limit]
	}
	return result, nil
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
