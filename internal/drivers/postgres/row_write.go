package postgres

import (
	"context"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// WriteCapabilities reports the SQL row-write capability; the postgres
// driver has no document store.
func (s *Service) WriteCapabilities() sharedsql.WriteCapabilities {
	return sharedsql.WriteCapabilities{RowWriter: true}
}

// InsertRow inserts one row, binding values as parameters instead of
// quoting them by hand. ValueDefault columns are omitted so engine
// defaults and auto-increment apply; a row of pure defaults uses
// DEFAULT VALUES. Placeholders are monotonically numbered $1..$N.
func (s *Service) InsertRow(ctx context.Context, table string, values []sharedsql.RowValue) (sharedsql.Result, error) {
	columns, args, err := postgresInsertParts(values)
	if err != nil {
		return sharedsql.Result{}, err
	}
	execution, err := s.db.ExecContext(ctx, postgresInsertStatement(table, columns), args...)
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
	sets, args, err := postgresUpdateParts(values)
	if err != nil {
		return sharedsql.Result{}, err
	}
	if len(sets) == 0 {
		return sharedsql.Result{}, nil
	}
	where, whereArgs, err := postgresKeyCondition(key, len(args))
	if err != nil {
		return sharedsql.Result{}, err
	}
	execution, err := s.db.ExecContext(ctx, postgresUpdateStatement(table, sets, where), append(args, whereArgs...)...)
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
	where, args, err := postgresKeyCondition(key, 0)
	if err != nil {
		return sharedsql.Result{}, err
	}
	execution, err := s.db.ExecContext(ctx, postgresDeleteStatement(table, where), args...)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("deleting row: %w", err)
	}
	affected, err := execution.RowsAffected()
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("reading affected rows: %w", err)
	}
	return sharedsql.Result{RowsAffected: affected}, nil
}

// postgresInsertParts maps insert values to quoted columns and bound args,
// omitting DEFAULT columns.
func postgresInsertParts(values []sharedsql.RowValue) (columns []string, args []any, err error) {
	columns = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	for _, row := range values {
		if row.Value.Kind == sharedsql.ValueDefault {
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

// postgresUpdateParts maps update values to "col = $N" terms and bound
// args, rejecting DEFAULT.
func postgresUpdateParts(values []sharedsql.RowValue) (sets []string, args []any, err error) {
	sets = make([]string, 0, len(values))
	args = make([]any, 0, len(values))
	for _, row := range values {
		if row.Value.Kind == sharedsql.ValueDefault {
			return nil, nil, fmt.Errorf("cannot update %s to DEFAULT", row.Name)
		}
		arg, err := rowWriteArg(row.Value)
		if err != nil {
			return nil, nil, err
		}
		sets = append(sets, fmt.Sprintf("%s = $%d", quoteIdentifier(row.Name), len(args)+1))
		args = append(args, arg)
	}
	return sets, args, nil
}

// postgresInsertStatement builds the INSERT: DEFAULT VALUES when every
// column is DEFAULT, numbered placeholders otherwise.
func postgresInsertStatement(table string, columns []string) string {
	if len(columns) == 0 {
		return "INSERT INTO " + postgresTableIdentifier(table) + " DEFAULT VALUES"
	}
	placeholders := make([]string, len(columns))
	for index := range placeholders {
		placeholders[index] = fmt.Sprintf("$%d", index+1)
	}
	return "INSERT INTO " + postgresTableIdentifier(table) + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
}

// postgresUpdateStatement builds the UPDATE with the given SET terms and
// WHERE condition.
func postgresUpdateStatement(table string, sets []string, where string) string {
	return "UPDATE " + postgresTableIdentifier(table) + " SET " + strings.Join(sets, ", ") + " WHERE " + where
}

// postgresDeleteStatement builds the DELETE with the given WHERE condition.
func postgresDeleteStatement(table string, where string) string {
	return "DELETE FROM " + postgresTableIdentifier(table) + " WHERE " + where
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

// postgresKeyCondition builds the WHERE clause identifying a row by key
// values, preserving NULL predicates. Placeholder numbering continues after
// the update arguments (offset) so $N stays monotonically ordered.
func postgresKeyCondition(key []sharedsql.RowValue, offset int) (string, []any, error) {
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
		offset++
		terms = append(terms, fmt.Sprintf("%s = $%d", quoteIdentifier(row.Name), offset))
		args = append(args, arg)
	}
	return strings.Join(terms, " AND "), args, nil
}
