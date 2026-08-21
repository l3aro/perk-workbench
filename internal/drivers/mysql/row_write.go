package mysql

import (
	"context"
	"fmt"
	"strings"

	driver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

// WriteCapabilities reports the SQL row-write capability; the mysql
// driver has no document store.
func (s *Service) WriteCapabilities() driver.WriteCapabilities {
	return driver.WriteCapabilities{RowWriter: true}
}

// InsertRow inserts one row, binding values as parameters instead of
// quoting them by hand. ValueDefault columns are omitted so engine
// defaults and auto-increment apply; a row of pure defaults uses MySQL's
// empty-column-list syntax.
func (s *Service) InsertRow(ctx context.Context, table string, values []driver.RowValue) (driver.Result, error) {
	columns, args, err := mysqlInsertParts(values)
	if err != nil {
		return driver.Result{}, err
	}
	execution, err := s.db.ExecContext(ctx, mysqlInsertStatement(table, columns), args...)
	if err != nil {
		return driver.Result{}, fmt.Errorf("inserting row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return driver.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return driver.Result{RowsAffected: affected}, nil
}

// UpdateRow sets the given columns on the row identified by key. A
// ValueDefault update value is rejected: DEFAULT is an insert-only state.
func (s *Service) UpdateRow(ctx context.Context, table string, key []driver.RowValue, values []driver.RowValue) (driver.Result, error) {
	sets, args, err := mysqlUpdateParts(values)
	if err != nil {
		return driver.Result{}, err
	}
	where, whereArgs, err := mysqlKeyCondition(key)
	if err != nil {
		return driver.Result{}, err
	}
	statement := mysqlUpdateStatement(table, sets, where)
	execution, err := s.db.ExecContext(ctx, statement, append(args, whereArgs...)...)
	if err != nil {
		return driver.Result{}, fmt.Errorf("updating row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return driver.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return driver.Result{RowsAffected: affected}, nil
}

// DeleteRow removes the row identified by key. NULL key values become
// IS NULL predicates so NULL primary-key parts still match.
func (s *Service) DeleteRow(ctx context.Context, table string, key []driver.RowValue) (driver.Result, error) {
	where, args, err := mysqlKeyCondition(key)
	if err != nil {
		return driver.Result{}, err
	}
	execution, err := s.db.ExecContext(ctx, mysqlDeleteStatement(table, where), args...)
	if err != nil {
		return driver.Result{}, fmt.Errorf("deleting row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return driver.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return driver.Result{RowsAffected: affected}, nil
}

// mysqlInsertParts maps insert values to quoted columns and bound args,
// omitting DEFAULT columns.
func mysqlInsertParts(values []driver.RowValue) (columns []string, args []any, err error) {
	columns = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	for _, row := range values {
		if row.Value.Kind == driver.ValueDefault {
			continue
		}
		arg, err := rowWriteArg(row.Value)
		if err != nil {
			return nil, nil, err
		}
		columns = append(columns, quoteIdentifier(row.Name))
		args = append(args, arg)
	}
	return columns, args, nil
}

// mysqlUpdateParts maps update values to "col = ?" terms and bound args,
// rejecting DEFAULT.
func mysqlUpdateParts(values []driver.RowValue) (sets []string, args []any, err error) {
	sets = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	for _, row := range values {
		if row.Value.Kind == driver.ValueDefault {
			return nil, nil, fmt.Errorf("cannot update %s to DEFAULT", row.Name)
		}
		arg, err := rowWriteArg(row.Value)
		if err != nil {
			return nil, nil, err
		}
		sets = append(sets, quoteIdentifier(row.Name)+" = ?")
		args = append(args, arg)
	}
	return sets, args, nil
}

// mysqlInsertStatement builds the INSERT: the empty-column-list form when
// every column is DEFAULT.
func mysqlInsertStatement(table string, columns []string) string {
	if len(columns) == 0 {
		return "INSERT INTO " + mysqlTableIdentifier(table) + " () VALUES ()"
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	return "INSERT INTO " + mysqlTableIdentifier(table) + " (" + strings.Join(columns, ", ") + ") VALUES (" + placeholders + ")"
}

// mysqlUpdateStatement builds the UPDATE with the given SET terms and
// WHERE condition.
func mysqlUpdateStatement(table string, sets []string, where string) string {
	return "UPDATE " + mysqlTableIdentifier(table) + " SET " + strings.Join(sets, ", ") + " WHERE " + where
}

// mysqlDeleteStatement builds the DELETE with the given WHERE condition.
func mysqlDeleteStatement(table string, where string) string {
	return "DELETE FROM " + mysqlTableIdentifier(table) + " WHERE " + where
}

// rowWriteArg maps one UI-produced tagged value to a bound driver argument.
// Typed kinds (bool, integer, ...) are rejected until a typed editor emits
// them; the tri-state form only produces DEFAULT, NULL, and String.
func rowWriteArg(value driver.Value) (any, error) {
	switch value.Kind {
	case driver.ValueNull:
		return nil, nil
	case driver.ValueString:
		return value.String, nil
	default:
		return nil, fmt.Errorf("unsupported row value kind %s", value.Kind)
	}
}

// mysqlKeyCondition builds the WHERE clause identifying a row by key
// values, preserving NULL predicates and returning the bound arguments in
// order.
func mysqlKeyCondition(key []driver.RowValue) (string, []any, error) {
	if len(key) == 0 {
		return "", nil, fmt.Errorf("row key is empty")
	}
	terms := make([]string, 0, len(key))
	args := make([]any, 0, len(key))
	for _, row := range key {
		if row.Value.Kind == driver.ValueNull {
			terms = append(terms, quoteIdentifier(row.Name)+" IS NULL")
			continue
		}
		arg, err := rowWriteArg(row.Value)
		if err != nil {
			return "", nil, err
		}
		terms = append(terms, quoteIdentifier(row.Name)+" = ?")
		args = append(args, arg)
	}
	return strings.Join(terms, " AND "), args, nil
}
