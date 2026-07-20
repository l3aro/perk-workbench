package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Service) Execute(ctx context.Context, statement string) (result Result, err error) {
	if err := validateStatement(statement); err != nil {
		return Result{}, err
	}

	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquiring %s connection: %w", s.driver, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing %s connection: %w", s.driver, closeErr))
				return
			}
			result = Result{}
			err = fmt.Errorf("closing %s connection: %w", s.driver, closeErr)
		}
	}()

	if s.driver == "mysql" && !returnsRows(statement) {
		execution, err := conn.ExecContext(ctx, statement)
		if err != nil {
			return Result{}, fmt.Errorf("executing statement: %w", err)
		}
		result.RowsAffected, err = execution.RowsAffected()
		if err != nil {
			return Result{}, fmt.Errorf("reading affected rows: %w", err)
		}
		result.Duration = time.Since(started)
		return result, nil
	}

	before := int64(0)
	if s.driver == "sqlite" {
		before, err = totalChanges(ctx, conn)
		if err != nil {
			return Result{}, err
		}
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return Result{}, fmt.Errorf("executing statement: %w", err)
	}

	result, err = collectRows(rows)
	if err != nil {
		return Result{}, err
	}
	if s.driver == "sqlite" {
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
	}
	result.Duration = time.Since(started)
	return result, nil
}

func returnsRows(statement string) bool {
	for {
		statement = strings.TrimSpace(strings.TrimLeft(statement, "("))
		switch {
		case strings.HasPrefix(statement, "--"):
			if index := strings.IndexByte(statement, '\n'); index >= 0 {
				statement = statement[index+1:]
				continue
			}
			return false
		case strings.HasPrefix(statement, "/*"):
			index := strings.Index(statement[2:], "*/")
			if index < 0 {
				return false
			}
			statement = statement[index+4:]
			continue
		}
		break
	}
	if index := strings.IndexAny(statement, " \t\n\r("); index >= 0 {
		statement = statement[:index]
	}
	switch strings.ToUpper(statement) {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH":
		return true
	default:
		return false
	}
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
