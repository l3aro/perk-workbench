package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"slices"
	"strings"

	plugindriver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func (s *Service) TableInfo(ctx context.Context, name string) ([]plugindriver.ColumnInfo, error) {
	return tableInfo(ctx, s.db, name)
}

type tableInfoQuerier interface {
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
}

func tableInfo(ctx context.Context, queryer tableInfoQuerier, name string) ([]plugindriver.ColumnInfo, error) {
	rows, err := queryer.QueryContext(ctx, "PRAGMA table_xinfo("+quoteIdentifier(name)+")")
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}

	columns := []plugindriver.ColumnInfo{}
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var column plugindriver.ColumnInfo
		var defaultValue stdsql.NullString
		if err := rows.Scan(&cid, &column.Name, &column.Type, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return nil, CloseRows(rows, "scanning table info", err)
		}
		column.Name = sanitizeDisplay(column.Name)
		column.Type = sanitizeDisplay(column.Type)
		column.Nullable = notNull == 0
		column.PrimaryKey = primaryKey
		switch hidden {
		case 2:
			column.Attributes = "GENERATED VIRTUAL"
		case 3:
			column.Attributes = "GENERATED STORED"
		}
		if primaryKey > 0 {
			column.Indexes = []plugindriver.IndexKind{plugindriver.IndexPrimaryKey}
		}
		if defaultValue.Valid {
			value := sanitizeDisplay(defaultValue.String)
			column.DefaultValue = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, CloseRows(rows, "iterating table info", err)
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

func tableIndexKinds(ctx context.Context, queryer tableInfoQuerier, table string) (map[string][]plugindriver.IndexKind, error) {
	// One query instead of 1+N: pragma_index_list and pragma_index_info are
	// table-valued functions, and the latter can reference the former's
	// name column in the same statement.
	rows, err := queryer.QueryContext(ctx,
		"SELECT il.name, il.\"unique\", il.origin, ii.name FROM pragma_index_list("+sqlLiteral(table)+") il JOIN pragma_index_info(il.name) ii")
	if err != nil {
		return nil, fmt.Errorf("reading table indexes: %w", err)
	}
	columnIndexes := map[string][]plugindriver.IndexKind{}
	for rows.Next() {
		var unique int
		var indexName, origin string
		var columnName stdsql.NullString
		if err := rows.Scan(&indexName, &unique, &origin, &columnName); err != nil {
			return nil, CloseRows(rows, "scanning table indexes", err)
		}
		kind := plugindriver.IndexRegular
		if origin == "pk" {
			kind = plugindriver.IndexPrimaryKey
		} else if unique != 0 {
			kind = plugindriver.IndexUnique
		}
		if columnName.Valid {
			name := sanitizeDisplay(columnName.String)
			if !slices.Contains(columnIndexes[name], kind) {
				columnIndexes[name] = append(columnIndexes[name], kind)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, CloseRows(rows, "iterating table indexes", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table index rows: %w", err)
	}
	return columnIndexes, nil
}

func (s *Service) BrowseTable(ctx context.Context, name string, options plugindriver.BrowseOptions) (plugindriver.Result, error) {
	if options.Offset < 0 || options.Limit < 1 || options.Limit > maxRows {
		return plugindriver.Result{}, fmt.Errorf("invalid browse range: offset=%d limit=%d", options.Offset, options.Limit)
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
				return plugindriver.Result{}, fmt.Errorf("invalid browse filter column: %s", filter.Column)
			}
			column := quoteIdentifier(filter.Column)
			switch filter.Operator {
			case BrowseFilterLike, BrowseFilterNotLike:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case BrowseFilterPattern, BrowseFilterNotPattern:
				like := "LIKE"
				if filter.Operator == BrowseFilterNotPattern {
					like = "NOT LIKE"
				}
				terms = append(terms, column+" "+like+" ? ESCAPE '\\'")
				args = append(args, GlobToLike(filter.Value))
			case BrowseFilterEqual, BrowseFilterNotEqual, BrowseFilterLess, BrowseFilterLessEqual, BrowseFilterGreater, BrowseFilterGreaterEqual:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case BrowseFilterIsNull, BrowseFilterIsNotNull:
				terms = append(terms, column+" "+string(filter.Operator))
			default:
				return plugindriver.Result{}, fmt.Errorf("invalid browse filter operator: %q", filter.Operator)
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
		return plugindriver.Result{}, fmt.Errorf("browsing table: %w", err)
	}
	result, err := CollectRows(rows)
	if err != nil {
		return plugindriver.Result{}, err
	}
	result.HasMore = len(result.Rows) > options.Limit
	if result.HasMore {
		result.Rows = result.Rows[:options.Limit]
		result.UntruncatedRows = result.UntruncatedRows[:options.Limit]
	}
	return result, nil
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// sqlLiteral quotes a value as a SQL string literal (for pragma table-valued
// function arguments, which take strings rather than identifiers).
func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
