package mysql

import (
	"context"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func (s *Service) ListIndexes(ctx context.Context, table string) ([]sharedsql.IndexInfo, error) {
	database, name := mysqlTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT index_name, non_unique, column_name
		FROM information_schema.statistics
		WHERE table_schema = COALESCE(NULLIF(?, ''), DATABASE()) AND table_name = ?
		ORDER BY index_name, seq_in_index`, database, name)
	if err != nil {
		return nil, fmt.Errorf("reading indexes: %w", err)
	}
	indexes := []sharedsql.IndexInfo{}
	for rows.Next() {
		var name, column string
		var nonUnique int
		if err := rows.Scan(&name, &nonUnique, &column); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning indexes", err)
		}
		if len(indexes) == 0 || indexes[len(indexes)-1].Name != name {
			indexes = append(indexes, sharedsql.IndexInfo{Name: sharedsql.SanitizeDisplay(name), Unique: nonUnique == 0 && name != "PRIMARY", PrimaryKey: name == "PRIMARY"})
		}
		last := len(indexes) - 1
		indexes[last].Columns = append(indexes[last].Columns, sharedsql.SanitizeDisplay(column))
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating indexes", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing indexes: %w", err)
	}
	return indexes, nil
}

func (s *Service) CreateIndex(ctx context.Context, table string, change sharedsql.IndexChange) error {
	if err := sharedsql.ValidateIndexChange(change); err != nil {
		return err
	}
	if change.PrimaryKey {
		if _, err := s.db.ExecContext(ctx, mysqlAddPrimaryKeyStatement(table, change.Columns)); err != nil {
			return fmt.Errorf("creating primary key: %w", err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, mysqlCreateIndexStatement(table, change)); err != nil {
		return fmt.Errorf("creating index: %w", err)
	}
	return nil
}

func (s *Service) ReplaceIndex(ctx context.Context, table, previous string, change sharedsql.IndexChange) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("previous index name is required")
	}
	if err := sharedsql.ValidateIndexChange(change); err != nil {
		return err
	}
	if previous == "PRIMARY" {
		if !change.PrimaryKey {
			return fmt.Errorf("replace a primary key with another primary key, or delete it first")
		}
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" DROP PRIMARY KEY, ADD PRIMARY KEY ("+indexColumns(change.Columns)+")"); err != nil {
			return fmt.Errorf("replacing primary key: %w", err)
		}
		return nil
	}
	if change.PrimaryKey {
		return fmt.Errorf("create a primary key separately before replacing this index")
	}
	if _, err := s.db.ExecContext(ctx, "DROP INDEX "+quoteIdentifier(previous)+" ON "+mysqlTableIdentifier(table)); err != nil {
		return fmt.Errorf("dropping index: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, mysqlCreateIndexStatement(table, change)); err != nil {
		return fmt.Errorf("creating replacement index: %w", err)
	}
	return nil
}

func (s *Service) DropIndex(ctx context.Context, table, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("index name is required")
	}
	if name == "PRIMARY" {
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" DROP PRIMARY KEY"); err != nil {
			return fmt.Errorf("dropping primary key: %w", err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "DROP INDEX "+quoteIdentifier(name)+" ON "+mysqlTableIdentifier(table)); err != nil {
		return fmt.Errorf("dropping index: %w", err)
	}
	return nil
}

func mysqlCreateIndexStatement(table string, change sharedsql.IndexChange) string {
	prefix := "CREATE INDEX "
	if change.Unique {
		prefix = "CREATE UNIQUE INDEX "
	}
	return prefix + quoteIdentifier(change.Name) + " ON " + mysqlTableIdentifier(table) + " (" + indexColumns(change.Columns) + ")"
}

func mysqlAddPrimaryKeyStatement(table string, columns []string) string {
	return "ALTER TABLE " + mysqlTableIdentifier(table) + " ADD PRIMARY KEY (" + indexColumns(columns) + ")"
}

func indexColumns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quoteIdentifier(strings.TrimSpace(column))
	}
	return strings.Join(quoted, ", ")
}
