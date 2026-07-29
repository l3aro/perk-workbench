package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (s *Service) AlterColumn(ctx context.Context, table string, change sharedsql.ColumnChange) error {
	if err := sharedsql.ValidateColumnChange(change); err != nil {
		return err
	}
	columns, err := s.TableInfo(ctx, table)
	if err != nil {
		return err
	}
	var current sharedsql.ColumnInfo
	found := false
	for _, column := range columns {
		if column.Name == change.PreviousName {
			current, found = column, true
			break
		}
	}
	if !found {
		return fmt.Errorf("column %q was not found", change.PreviousName)
	}
	if current.PrimaryKey > 0 {
		if change.Name != change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && defaultsEqual(change.DefaultValue, current.DefaultValue) && (change.Attributes == nil || *change.Attributes == current.Attributes) {
			_, err := s.db.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(change.PreviousName)+" TO "+quoteIdentifier(change.Name))
			return err
		}
		return errors.New("primary-key columns can only be renamed without other changes")
	}
	if change.Type == current.Type && change.Nullable == current.Nullable && defaultsEqual(change.DefaultValue, current.DefaultValue) && (change.Attributes == nil || *change.Attributes == current.Attributes) {
		if change.Name == change.PreviousName {
			return nil
		}
		_, err := s.db.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(change.PreviousName)+" TO "+quoteIdentifier(change.Name))
		if err != nil {
			return fmt.Errorf("renaming column: %w", err)
		}
		return nil
	}
	if err := sharedsql.ValidateColumnAttributeChange(change.Attributes, current.Attributes); err != nil {
		return err
	}
	return s.rebuildTable(ctx, table, change)
}

func (s *Service) AddColumn(ctx context.Context, table string, col sharedsql.ColumnDef) error {
	if err := sharedsql.ValidateColumnDef(col); err != nil {
		return err
	}
	statement := "ALTER TABLE " + quoteIdentifier(table) + " ADD COLUMN " + quoteIdentifier(col.Name) + " " + strings.TrimSpace(col.Type)
	if !col.Nullable {
		statement += " NOT NULL"
	}
	if col.DefaultValue != nil {
		statement += " DEFAULT " + *col.DefaultValue
	}
	if col.Attributes != nil && *col.Attributes != "" {
		statement += " " + *col.Attributes
	}
	_, err := s.db.ExecContext(ctx, statement)
	if err != nil {
		return fmt.Errorf("adding column: %w", err)
	}
	return nil
}

func (s *Service) rebuildTable(ctx context.Context, table string, change sharedsql.ColumnChange) (err error) {
	prepare := func(*stdsql.Tx) error { return nil }
	if change.Name != change.PreviousName {
		previousName := change.PreviousName
		prepare = func(transaction *stdsql.Tx) error {
			if _, err := transaction.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(previousName)+" TO "+quoteIdentifier(change.Name)); err != nil {
				return fmt.Errorf("renaming column: %w", err)
			}
			return nil
		}
		change.PreviousName = change.Name
	}
	return s.rebuildTableWithSQL(ctx, table, prepare, func(createSQL string) (string, error) {
		return rewriteCreateTable(createSQL, "__perk_workbench_column_edit", change)
	})
}

func (s *Service) rebuildTableWithSQL(ctx context.Context, table string, prepare func(*stdsql.Tx) error, rewrite func(string) (string, error)) (err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring sqlite connection: %w", err)
	}
	defer conn.Close()
	foreignKeys, err := foreignKeysEnabled(ctx, conn)
	if err != nil {
		return err
	}
	if foreignKeys {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("disabling foreign keys: %w", err)
		}
		defer func() {
			if _, restoreErr := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON"); restoreErr != nil && err == nil {
				err = fmt.Errorf("restoring foreign keys: %w", restoreErr)
			}
		}()
	}
	transaction, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting schema migration: %w", err)
	}
	defer transaction.Rollback()
	if err := prepare(transaction); err != nil {
		return err
	}
	createSQL, schemaObjects, columns, err := rebuildInputs(ctx, transaction, table)
	if err != nil {
		return err
	}
	temporary := "__perk_workbench_column_edit"
	rebuiltSQL, err := rewrite(createSQL)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, rebuiltSQL); err != nil {
		return fmt.Errorf("creating replacement table: %w", err)
	}
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = quoteIdentifier(column.Name)
	}
	columnList := strings.Join(names, ", ")
	copySQL := "INSERT INTO " + quoteIdentifier(temporary) + " (" + columnList + ") SELECT " + columnList + " FROM " + quoteIdentifier(table)
	if _, err := transaction.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("copying table rows: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "DROP TABLE "+quoteIdentifier(table)); err != nil {
		return fmt.Errorf("dropping previous table: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(temporary)+" RENAME TO "+quoteIdentifier(table)); err != nil {
		return fmt.Errorf("renaming replacement table: %w", err)
	}
	for _, statement := range schemaObjects {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("restoring table object: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("committing schema migration: %w", err)
	}
	return nil
}

type schemaQuerier interface {
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *stdsql.Row
}

func rebuildInputs(ctx context.Context, queryer schemaQuerier, table string) (string, []string, []sharedsql.ColumnInfo, error) {
	var createSQL string
	if err := queryer.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = ?", table).Scan(&createSQL); err != nil {
		return "", nil, nil, fmt.Errorf("reading table definition: %w", err)
	}
	rows, err := queryer.QueryContext(ctx, "SELECT sql FROM sqlite_schema WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL", table)
	if err != nil {
		return "", nil, nil, fmt.Errorf("reading table objects: %w", err)
	}
	objects := []string{}
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			return "", nil, nil, fmt.Errorf("scanning table object: %w", err)
		}
		objects = append(objects, statement)
	}
	if err := rows.Err(); err != nil {
		return "", nil, nil, fmt.Errorf("iterating table objects: %w", err)
	}
	if err := rows.Close(); err != nil {
		return "", nil, nil, fmt.Errorf("closing table objects: %w", err)
	}
	columns, err := tableInfo(ctx, queryer, table)
	if err != nil {
		return "", nil, nil, err
	}
	return createSQL, objects, columns, nil
}

func foreignKeysEnabled(ctx context.Context, conn *stdsql.Conn) (bool, error) {
	var enabled int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
		return false, fmt.Errorf("reading foreign-key setting: %w", err)
	}
	return enabled != 0, nil
}

func defaultsEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
