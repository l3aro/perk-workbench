package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (s *Service) Execute(ctx context.Context, statement string) (result sharedsql.Result, err error) {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return sharedsql.Result{}, err
	}

	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("acquiring sqlite connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing sqlite connection: %w", closeErr))
				return
			}
			result = sharedsql.Result{}
			err = fmt.Errorf("closing sqlite connection: %w", closeErr)
		}
	}()

	before, err := totalChanges(ctx, conn)
	if err != nil {
		return sharedsql.Result{}, err
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("executing statement: %w", err)
	}
	result, err = sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	after, err := totalChanges(ctx, conn)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.RowsAffected = after - before
	result.Duration = time.Since(started)
	return result, nil
}

func totalChanges(ctx context.Context, conn *stdsql.Conn) (int64, error) {
	var changes int64
	if err := conn.QueryRowContext(ctx, "SELECT total_changes()").Scan(&changes); err != nil {
		return 0, fmt.Errorf("reading total changes: %w", err)
	}
	return changes, nil
}
