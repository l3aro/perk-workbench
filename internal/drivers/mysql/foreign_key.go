package mysql

import (
	"context"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (s *Service) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	database, name := mysqlTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT key_column_usage.constraint_name, key_column_usage.column_name,
			key_column_usage.referenced_table_name, key_column_usage.referenced_column_name,
			referential_constraints.update_rule, referential_constraints.delete_rule
		FROM information_schema.key_column_usage
		JOIN information_schema.referential_constraints
			ON BINARY referential_constraints.constraint_schema = BINARY key_column_usage.constraint_schema
			AND BINARY referential_constraints.table_name = BINARY key_column_usage.table_name
			AND BINARY referential_constraints.constraint_name = BINARY key_column_usage.constraint_name
		WHERE BINARY key_column_usage.table_schema = BINARY COALESCE(NULLIF(?, ''), DATABASE()) AND BINARY key_column_usage.table_name = BINARY ?
			AND key_column_usage.referenced_table_name IS NOT NULL
		ORDER BY key_column_usage.constraint_name, key_column_usage.ordinal_position`, database, name)
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

func (s *Service) ListReferencingForeignKeys(ctx context.Context, table string) ([]sharedsql.ReferencingForeignKeyInfo, error) {
	database, name := mysqlTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT key_column_usage.table_name, key_column_usage.constraint_name, key_column_usage.column_name,
			key_column_usage.referenced_table_name, key_column_usage.referenced_column_name,
			referential_constraints.update_rule, referential_constraints.delete_rule
		FROM information_schema.key_column_usage
		JOIN information_schema.referential_constraints
			ON BINARY referential_constraints.constraint_schema = BINARY key_column_usage.constraint_schema
			AND BINARY referential_constraints.table_name = BINARY key_column_usage.table_name
			AND BINARY referential_constraints.constraint_name = BINARY key_column_usage.constraint_name
		WHERE BINARY key_column_usage.table_schema = BINARY COALESCE(NULLIF(?, ''), DATABASE())
			AND BINARY key_column_usage.referenced_table_schema = BINARY COALESCE(NULLIF(?, ''), DATABASE())
			AND BINARY key_column_usage.referenced_table_name = BINARY ?
		ORDER BY key_column_usage.table_name, key_column_usage.constraint_name, key_column_usage.ordinal_position`, database, database, name)
	if err != nil {
		return nil, fmt.Errorf("reading referencing foreign keys: %w", err)
	}
	references := []sharedsql.ReferencingForeignKeyInfo{}
	for rows.Next() {
		var sourceTable, id, column, referencedTable, referencedColumn, onUpdate, onDelete string
		if err := rows.Scan(&sourceTable, &id, &column, &referencedTable, &referencedColumn, &onUpdate, &onDelete); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning referencing foreign keys", err)
		}
		if len(references) == 0 || references[len(references)-1].Table != sourceTable || references[len(references)-1].ID != id {
			references = append(references, sharedsql.ReferencingForeignKeyInfo{Table: sharedsql.SanitizeDisplay(sourceTable), ForeignKeyInfo: sharedsql.ForeignKeyInfo{ID: sharedsql.SanitizeDisplay(id), ReferenceTable: sharedsql.SanitizeDisplay(referencedTable), OnDelete: sharedsql.SanitizeDisplay(onDelete), OnUpdate: sharedsql.SanitizeDisplay(onUpdate)}})
		}
		last := len(references) - 1
		references[last].Columns = append(references[last].Columns, sharedsql.SanitizeDisplay(column))
		references[last].ReferenceColumns = append(references[last].ReferenceColumns, sharedsql.SanitizeDisplay(referencedColumn))
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating referencing foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing referencing-foreign-key rows: %w", err)
	}
	return references, nil
}

// ListForeignKeysAll returns every foreign key in the connected server,
// keyed by the declaring table's qualified name (database.table), with
// the referenced table qualified the same way — mirroring PostgreSQL's
// bulk map. Unlike the per-table queries it does not depend on the DSN's
// default database (which may be empty or differ from the tables the
// user browses), so it scans every non-system schema instead.
func (s *Service) ListForeignKeysAll(ctx context.Context) (map[string][]sharedsql.ForeignKeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key_column_usage.table_schema, key_column_usage.table_name, key_column_usage.constraint_name, key_column_usage.column_name,
			key_column_usage.referenced_table_schema, key_column_usage.referenced_table_name, key_column_usage.referenced_column_name,
			referential_constraints.update_rule, referential_constraints.delete_rule
		FROM information_schema.key_column_usage
		JOIN information_schema.referential_constraints
			ON BINARY referential_constraints.constraint_schema = BINARY key_column_usage.constraint_schema
			AND BINARY referential_constraints.table_name = BINARY key_column_usage.table_name
			AND BINARY referential_constraints.constraint_name = BINARY key_column_usage.constraint_name
		WHERE key_column_usage.table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')
			AND key_column_usage.referenced_table_name IS NOT NULL
		ORDER BY key_column_usage.table_schema, key_column_usage.table_name, key_column_usage.constraint_name, key_column_usage.ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("reading foreign keys: %w", err)
	}
	foreignKeys := map[string][]sharedsql.ForeignKeyInfo{}
	var lastTable, lastID string
	var info *sharedsql.ForeignKeyInfo
	finish := func() {
		if info != nil {
			foreignKeys[lastTable] = append(foreignKeys[lastTable], *info)
		}
	}
	for rows.Next() {
		var schema, table, id, column, referenceSchema, referencedTable, referencedColumn, onUpdate, onDelete string
		if err := rows.Scan(&schema, &table, &id, &column, &referenceSchema, &referencedTable, &referencedColumn, &onUpdate, &onDelete); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning foreign keys", err)
		}
		qualified := schema + "." + table
		if info == nil || qualified != lastTable || id != lastID {
			finish()
			lastTable, lastID = qualified, id
			info = &sharedsql.ForeignKeyInfo{ID: sharedsql.SanitizeDisplay(id), ReferenceTable: sharedsql.SanitizeDisplay(referenceSchema + "." + referencedTable), OnDelete: sharedsql.SanitizeDisplay(onDelete), OnUpdate: sharedsql.SanitizeDisplay(onUpdate)}
		}
		info.Columns = append(info.Columns, sharedsql.SanitizeDisplay(column))
		info.ReferenceColumns = append(info.ReferenceColumns, sharedsql.SanitizeDisplay(referencedColumn))
	}
	finish()
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
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" ADD "+mysqlForeignKeyClause(change)); err != nil {
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
	statement := "ALTER TABLE " + mysqlTableIdentifier(table) + " DROP FOREIGN KEY " + quoteIdentifier(previous) + ", ADD " + mysqlForeignKeyClause(change)
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("replacing foreign key: %w", err)
	}
	return nil
}

func (s *Service) DropForeignKey(ctx context.Context, table, previous string) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("foreign key is required")
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" DROP FOREIGN KEY "+quoteIdentifier(previous)); err != nil {
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
