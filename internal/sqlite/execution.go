package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Service) Execute(ctx context.Context, statement string) (result Result, err error) {
	if err := validateStatement(statement); err != nil {
		return Result{}, err
	}

	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquiring sqlite connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing sqlite connection: %w", closeErr))
				return
			}
			result = Result{}
			err = fmt.Errorf("closing sqlite connection: %w", closeErr)
		}
	}()

	before, err := totalChanges(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return Result{}, fmt.Errorf("executing statement: %w", err)
	}

	result, err = collectRows(rows)
	if err != nil {
		return Result{}, err
	}
	after, err := totalChanges(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if after != before {
		result.RowsAffected, err = changes(ctx, conn)
		if err != nil {
			return Result{}, err
		}
	}
	result.Duration = time.Since(started)
	return result, nil
}

func collectRows(rows *sql.Rows) (Result, error) {
	columns, err := rows.Columns()
	if err != nil {
		return Result{}, closeRows(rows, "reading result columns", err)
	}
	result := Result{Columns: make([]string, len(columns)), Rows: [][]*string{}}
	for index, column := range columns {
		result.Columns[index] = SanitizeDisplay(column)
	}

	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return Result{}, closeRows(rows, "scanning result row", err)
		}
		if len(result.Rows) < maxRows {
			result.Rows = append(result.Rows, displayRow(values))
		} else {
			result.Truncated = true
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, closeRows(rows, "iterating result rows", err)
	}
	if err := rows.Close(); err != nil {
		return Result{}, fmt.Errorf("closing result rows: %w", err)
	}
	return result, nil
}

func closeRows(rows *sql.Rows, action string, err error) error {
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("%s: %w", action, errors.Join(err, closeErr))
	}
	return fmt.Errorf("%s: %w", action, err)
}

func totalChanges(ctx context.Context, conn *sql.Conn) (int64, error) {
	var changes int64
	if err := conn.QueryRowContext(ctx, "SELECT total_changes()").Scan(&changes); err != nil {
		return 0, fmt.Errorf("reading total changes: %w", err)
	}
	return changes, nil
}

func changes(ctx context.Context, conn *sql.Conn) (int64, error) {
	var count int64
	if err := conn.QueryRowContext(ctx, "SELECT changes()").Scan(&count); err != nil {
		return 0, fmt.Errorf("reading statement changes: %w", err)
	}
	return count, nil
}
