package mysql

import (
	"context"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func (s *Service) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key_column_usage.constraint_name, key_column_usage.column_name,
			key_column_usage.referenced_table_name, key_column_usage.referenced_column_name,
			referential_constraints.update_rule, referential_constraints.delete_rule
		FROM information_schema.key_column_usage
		JOIN information_schema.referential_constraints
			ON referential_constraints.constraint_schema = key_column_usage.constraint_schema
			AND referential_constraints.table_name = key_column_usage.table_name
			AND referential_constraints.constraint_name = key_column_usage.constraint_name
		WHERE key_column_usage.table_schema = DATABASE() AND key_column_usage.table_name = ?
			AND key_column_usage.referenced_table_name IS NOT NULL
		ORDER BY key_column_usage.constraint_name, key_column_usage.ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("reading foreign keys: %w", err)
	}
	foreignKeys := []sharedsql.ForeignKeyInfo{}
	for rows.Next() {
		var id, column, referencedTable, referencedColumn, onUpdate, onDelete string
		if err := rows.Scan(&id, &column, &referencedTable, &referencedColumn, &onUpdate, &onDelete); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning foreign keys", err)
		}
		if len(foreignKeys) == 0 || foreignKeys[len(foreignKeys)-1].ID != id {
			foreignKeys = append(foreignKeys, sharedsql.ForeignKeyInfo{ID: sharedsql.SanitizeDisplay(id), ReferenceTable: sharedsql.SanitizeDisplay(referencedTable), OnDelete: sharedsql.SanitizeDisplay(onDelete), OnUpdate: sharedsql.SanitizeDisplay(onUpdate)})
		}
		last := len(foreignKeys) - 1
		foreignKeys[last].Columns = append(foreignKeys[last].Columns, sharedsql.SanitizeDisplay(column))
		foreignKeys[last].ReferenceColumns = append(foreignKeys[last].ReferenceColumns, sharedsql.SanitizeDisplay(referencedColumn))
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing foreign-key rows: %w", err)
	}
	return foreignKeys, nil
}

func (s *Service) CreateForeignKey(ctx context.Context, table string, change sharedsql.ForeignKeyChange) error {
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" ADD "+mysqlForeignKeyClause(change)); err != nil {
		return fmt.Errorf("creating foreign key: %w", err)
	}
	return nil
}

func (s *Service) ReplaceForeignKey(ctx context.Context, table, previous string, change sharedsql.ForeignKeyChange) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("foreign key is required")
	}
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return err
	}
	statement := "ALTER TABLE " + quoteIdentifier(table) + " DROP FOREIGN KEY " + quoteIdentifier(previous) + ", ADD " + mysqlForeignKeyClause(change)
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("replacing foreign key: %w", err)
	}
	return nil
}

func (s *Service) DropForeignKey(ctx context.Context, table, previous string) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("foreign key is required")
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" DROP FOREIGN KEY "+quoteIdentifier(previous)); err != nil {
		return fmt.Errorf("dropping foreign key: %w", err)
	}
	return nil
}

func mysqlForeignKeyClause(change sharedsql.ForeignKeyChange) string {
	columns := make([]string, len(change.Columns))
	references := make([]string, len(change.ReferenceColumns))
	for index := range change.Columns {
		columns[index] = quoteIdentifier(strings.TrimSpace(change.Columns[index]))
		references[index] = quoteIdentifier(strings.TrimSpace(change.ReferenceColumns[index]))
	}
	return "FOREIGN KEY (" + strings.Join(columns, ", ") + ") REFERENCES " + quoteIdentifier(strings.TrimSpace(change.ReferenceTable)) + " (" + strings.Join(references, ", ") + ") ON DELETE " + strings.ToUpper(strings.TrimSpace(change.OnDelete)) + " ON UPDATE " + strings.ToUpper(strings.TrimSpace(change.OnUpdate))
}
