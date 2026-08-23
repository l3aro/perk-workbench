package sqlite

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	plugindriver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func (s *Service) Execute(ctx context.Context, statement string) (plugindriver.Result, error) {
	if err := ValidateStatement(statement); err != nil {
		return plugindriver.Result{}, err
	}
	return s.executeValidated(ctx, statement)
}

func (s *Service) executeValidated(ctx context.Context, statement string) (result plugindriver.Result, err error) {
	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return plugindriver.Result{}, fmt.Errorf("acquiring sqlite connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing sqlite connection: %w", closeErr))
				return
			}
			result = plugindriver.Result{}
			err = fmt.Errorf("closing sqlite connection: %w", closeErr)
		}
	}()

	before, err := totalChanges(ctx, conn)
	if err != nil {
		return plugindriver.Result{}, err
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return plugindriver.Result{}, fmt.Errorf("executing statement: %w", err)
	}
	result, err = CollectRows(rows)
	if err != nil {
		return plugindriver.Result{}, err
	}
	after, err := totalChanges(ctx, conn)
	if err != nil {
		return plugindriver.Result{}, err
	}
	result.RowsAffected = after - before
	result.DurationNS = time.Since(started).Nanoseconds()
	return result, nil
}

func (s *Service) ExecuteReadOnly(ctx context.Context, statement string) (plugindriver.Result, error) {
	if err := ValidateStatement(statement); err != nil {
		return plugindriver.Result{}, err
	}
	return s.executeReadOnlyValidated(ctx, statement)
}

func (s *Service) executeReadOnlyValidated(ctx context.Context, statement string) (result plugindriver.Result, err error) {
	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return plugindriver.Result{}, fmt.Errorf("acquiring sqlite connection: %w", err)
	}

	// Check and enable query_only if not already active.
	var queryOnly int
	if err := conn.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil {
		_ = conn.Close()
		return plugindriver.Result{}, fmt.Errorf("reading query_only pragma: %w", err)
	}
	needsReset := queryOnly == 0
	if needsReset {
		if _, err := conn.ExecContext(ctx, "PRAGMA query_only = 1"); err != nil {
			_ = conn.Close()
			return plugindriver.Result{}, fmt.Errorf("enabling query_only: %w", err)
		}
	}

	// Single cleanup defer: runs on ALL exit paths.
	defer func() {
		if needsReset {
			resetCtx, resetCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer resetCancel()
			if _, resetErr := conn.ExecContext(resetCtx, "PRAGMA query_only = 0"); resetErr != nil {
				// Reset failed — discard the poisoned connection from the pool.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				if closeErr := conn.Close(); closeErr != nil {
					resetErr = errors.Join(resetErr, closeErr)
				}
				if err != nil {
					err = errors.Join(err, fmt.Errorf("resetting query_only: %w", resetErr))
				} else {
					result = plugindriver.Result{}
					err = fmt.Errorf("resetting query_only: %w", resetErr)
				}
				return
			}
		}
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing sqlite connection: %w", closeErr))
				return
			}
			result = plugindriver.Result{}
			err = fmt.Errorf("closing sqlite connection: %w", closeErr)
		}
	}()

	rows, qErr := conn.QueryContext(ctx, statement)
	if qErr != nil {
		return plugindriver.Result{}, fmt.Errorf("executing read-only statement: %w", qErr)
	}
	result, qErr = CollectRows(rows)
	if qErr != nil {
		return plugindriver.Result{}, qErr
	}

	result.DurationNS = time.Since(started).Nanoseconds()
	return result, nil
}

// Validate prepares the statement against the open database without executing
// it, so syntax and schema errors surface without any side effects.
func (s *Service) Validate(ctx context.Context, statement string) error {
	if err := ValidateStatement(statement); err != nil {
		return err
	}
	return s.validateValidated(ctx, statement)
}

func (s *Service) validateValidated(ctx context.Context, statement string) error {
	prepared, err := s.db.PrepareContext(ctx, statement)
	if err != nil {
		return fmt.Errorf("validating statement: %w", err)
	}
	return prepared.Close()
}

func totalChanges(ctx context.Context, conn *stdsql.Conn) (int64, error) {
	var changes int64
	if err := conn.QueryRowContext(ctx, "SELECT total_changes()").Scan(&changes); err != nil {
		return 0, fmt.Errorf("reading total changes: %w", err)
	}
	return changes, nil
}
