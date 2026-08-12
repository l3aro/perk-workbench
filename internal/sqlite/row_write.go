package sqlite

import (
	"context"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// WriteCapabilities reports the SQL row-write capability; the sqlite
// driver has no document store.
func (s *Service) WriteCapabilities() sharedsql.WriteCapabilities {
	return sharedsql.WriteCapabilities{RowWriter: true}
}

// InsertRow inserts one row, binding values as parameters instead of
// quoting them by hand. ValueDefault columns are omitted so engine
// defaults and auto-increment apply; a row of pure defaults uses
// DEFAULT VALUES.
func (s *Service) InsertRow(ctx context.Context, table string, values []sharedsql.RowValue) (sharedsql.Result, error) {
	columns := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, row := range values {
		if row.Value.Kind == sharedsql.ValueDefault {
			continue
		}
		arg, err := rowWriteArg(row.Value)
		if err != nil {
			return sharedsql.Result{}, err
		}
		columns = append(columns, quoteIdentifier(row.Name))
		args = append(args, arg)
	}
	var statement string
	if len(columns) == 0 {
		statement = "INSERT INTO " + quoteIdentifier(table) + " DEFAULT VALUES"
	} else {
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
		statement = "INSERT INTO " + quoteIdentifier(table) + " (" + strings.Join(columns, ", ") + ") VALUES (" + placeholders + ")"
	}
	execution, err := s.db.ExecContext(ctx, statement, args...)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("inserting row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return sharedsql.Result{RowsAffected: affected}, nil
}

// UpdateRow sets the given columns on the row identified by key. A
// ValueDefault update value is rejected: DEFAULT is an insert-only state.
func (s *Service) UpdateRow(ctx context.Context, table string, key []sharedsql.RowValue, values []sharedsql.RowValue) (sharedsql.Result, error) {
	sets := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+len(key))
	for _, row := range values {
		if row.Value.Kind == sharedsql.ValueDefault {
			return sharedsql.Result{}, fmt.Errorf("cannot update %s to DEFAULT", row.Name)
		}
		arg, err := rowWriteArg(row.Value)
		if err != nil {
			return sharedsql.Result{}, err
		}
		sets = append(sets, quoteIdentifier(row.Name)+" = ?")
		args = append(args, arg)
	}
	if len(sets) == 0 {
		return sharedsql.Result{}, nil
	}
	where, whereArgs, err := rowKeyCondition(key)
	if err != nil {
		return sharedsql.Result{}, err
	}
	statement := "UPDATE " + quoteIdentifier(table) + " SET " + strings.Join(sets, ", ") + " WHERE " + where
	execution, err := s.db.ExecContext(ctx, statement, append(args, whereArgs...)...)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("updating row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return sharedsql.Result{RowsAffected: affected}, nil
}

// DeleteRow removes the row identified by key. NULL key values become
// IS NULL predicates so NULL primary-key parts still match.
func (s *Service) DeleteRow(ctx context.Context, table string, key []sharedsql.RowValue) (sharedsql.Result, error) {
	where, args, err := rowKeyCondition(key)
	if err != nil {
		return sharedsql.Result{}, err
	}
	execution, err := s.db.ExecContext(ctx, "DELETE FROM "+quoteIdentifier(table)+" WHERE "+where, args...)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("deleting row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return sharedsql.Result{RowsAffected: affected}, nil
}

// rowWriteArg maps one UI-produced tagged value to a bound driver argument.
// Typed kinds (bool, integer, ...) are rejected until a typed editor emits
// them; the tri-state form only produces DEFAULT, NULL, and String.
func rowWriteArg(value sharedsql.Value) (any, error) {
	switch value.Kind {
	case sharedsql.ValueNull:
		return nil, nil
	case sharedsql.ValueString:
		return value.String, nil
	default:
		return nil, fmt.Errorf("unsupported row value kind %s", value.Kind)
	}
}

// rowKeyCondition builds the WHERE clause identifying a row by key values,
// preserving NULL predicates and returning the bound arguments in order.
func rowKeyCondition(key []sharedsql.RowValue) (string, []any, error) {
	if len(key) == 0 {
		return "", nil, fmt.Errorf("row key is empty")
	}
	terms := make([]string, 0, len(key))
	args := make([]any, 0, len(key))
	for _, row := range key {
		if row.Value.Kind == sharedsql.ValueNull {
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
