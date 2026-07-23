package postgres

import (
	"context"
	"fmt"
	"strings"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

func (s *Service) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	schema, name := postgresTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT constraints.constraint_name, key_columns.column_name, referenced_columns.table_schema, referenced_columns.table_name,
			referenced_columns.column_name, references.delete_rule, references.update_rule
		FROM information_schema.table_constraints AS constraints
		JOIN information_schema.key_column_usage AS key_columns
			ON key_columns.constraint_catalog = constraints.constraint_catalog
			AND key_columns.constraint_schema = constraints.constraint_schema
			AND key_columns.constraint_name = constraints.constraint_name
		JOIN information_schema.referential_constraints AS references
			ON references.constraint_catalog = constraints.constraint_catalog
			AND references.constraint_schema = constraints.constraint_schema
			AND references.constraint_name = constraints.constraint_name
		JOIN information_schema.key_column_usage AS referenced_columns
			ON referenced_columns.constraint_catalog = references.unique_constraint_catalog
			AND referenced_columns.constraint_schema = references.unique_constraint_schema
			AND referenced_columns.constraint_name = references.unique_constraint_name
			AND referenced_columns.ordinal_position = key_columns.position_in_unique_constraint
		WHERE constraints.constraint_type = 'FOREIGN KEY' AND constraints.table_schema = $1 AND constraints.table_name = $2
		ORDER BY constraints.constraint_name, key_columns.ordinal_position`, schema, name)
	if err != nil {
		return nil, fmt.Errorf("reading foreign keys: %w", err)
	}
	foreignKeys := []sharedsql.ForeignKeyInfo{}
	for rows.Next() {
		var id, column, referenceSchema, referenceTable, referenceColumn, onDelete, onUpdate string
		if err := rows.Scan(&id, &column, &referenceSchema, &referenceTable, &referenceColumn, &onDelete, &onUpdate); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning foreign keys", err)
		}
		if len(foreignKeys) == 0 || foreignKeys[len(foreignKeys)-1].ID != id {
			foreignKeys = append(foreignKeys, sharedsql.ForeignKeyInfo{ID: sharedsql.SanitizeDisplay(id), ReferenceTable: sharedsql.SanitizeDisplay(referenceSchema + "." + referenceTable), OnDelete: onDelete, OnUpdate: onUpdate})
		}
		foreignKeys[len(foreignKeys)-1].Columns = append(foreignKeys[len(foreignKeys)-1].Columns, sharedsql.SanitizeDisplay(column))
		foreignKeys[len(foreignKeys)-1].ReferenceColumns = append(foreignKeys[len(foreignKeys)-1].ReferenceColumns, sharedsql.SanitizeDisplay(referenceColumn))
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
	schema, name := postgresTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT constraints.table_schema, constraints.table_name, constraints.constraint_name, key_columns.column_name,
			referenced_columns.column_name, references.delete_rule, references.update_rule
		FROM information_schema.table_constraints AS constraints
		JOIN information_schema.key_column_usage AS key_columns
			ON key_columns.constraint_catalog = constraints.constraint_catalog
			AND key_columns.constraint_schema = constraints.constraint_schema
			AND key_columns.constraint_name = constraints.constraint_name
		JOIN information_schema.referential_constraints AS references
			ON references.constraint_catalog = constraints.constraint_catalog
			AND references.constraint_schema = constraints.constraint_schema
			AND references.constraint_name = constraints.constraint_name
		JOIN information_schema.key_column_usage AS referenced_columns
			ON referenced_columns.constraint_catalog = references.unique_constraint_catalog
			AND referenced_columns.constraint_schema = references.unique_constraint_schema
			AND referenced_columns.constraint_name = references.unique_constraint_name
			AND referenced_columns.ordinal_position = key_columns.position_in_unique_constraint
		WHERE constraints.constraint_type = 'FOREIGN KEY' AND referenced_columns.table_schema = $1 AND referenced_columns.table_name = $2
		ORDER BY constraints.table_schema, constraints.table_name, constraints.constraint_name, key_columns.ordinal_position`, schema, name)
	if err != nil {
		return nil, fmt.Errorf("reading referencing foreign keys: %w", err)
	}
	references := []sharedsql.ReferencingForeignKeyInfo{}
	for rows.Next() {
		var tableSchema, tableName, id, column, referenceColumn, onDelete, onUpdate string
		if err := rows.Scan(&tableSchema, &tableName, &id, &column, &referenceColumn, &onDelete, &onUpdate); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning referencing foreign keys", err)
		}
		foreignTable := sharedsql.SanitizeDisplay(tableSchema + "." + tableName)
		if len(references) == 0 || references[len(references)-1].Table != foreignTable || references[len(references)-1].ID != id {
			references = append(references, sharedsql.ReferencingForeignKeyInfo{Table: foreignTable, ForeignKeyInfo: sharedsql.ForeignKeyInfo{ID: sharedsql.SanitizeDisplay(id), ReferenceTable: schema + "." + name, OnDelete: onDelete, OnUpdate: onUpdate}})
		}
		references[len(references)-1].Columns = append(references[len(references)-1].Columns, sharedsql.SanitizeDisplay(column))
		references[len(references)-1].ReferenceColumns = append(references[len(references)-1].ReferenceColumns, sharedsql.SanitizeDisplay(referenceColumn))
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating referencing foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing referencing-foreign-key rows: %w", err)
	}
	return references, nil
}

func (s *Service) CreateForeignKey(ctx context.Context, table string, change sharedsql.ForeignKeyChange) error {
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+postgresTableIdentifier(table)+" ADD "+postgresForeignKeyClause(change)); err != nil {
		return fmt.Errorf("creating foreign key: %w", err)
	}
	return nil
}

func (s *Service) ReplaceForeignKey(ctx context.Context, table, previous string, change sharedsql.ForeignKeyChange) error {
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting foreign-key replacement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "ALTER TABLE "+postgresTableIdentifier(table)+" DROP CONSTRAINT "+quoteIdentifier(previous)); err != nil {
		return fmt.Errorf("dropping foreign key: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE "+postgresTableIdentifier(table)+" ADD "+postgresForeignKeyClause(change)); err != nil {
		return fmt.Errorf("creating replacement foreign key: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing foreign-key replacement: %w", err)
	}
	return nil
}

func (s *Service) DropForeignKey(ctx context.Context, table, previous string) error {
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+postgresTableIdentifier(table)+" DROP CONSTRAINT "+quoteIdentifier(previous)); err != nil {
		return fmt.Errorf("dropping foreign key: %w", err)
	}
	return nil
}

func postgresForeignKeyClause(change sharedsql.ForeignKeyChange) string {
	return "FOREIGN KEY (" + indexColumns(change.Columns) + ") REFERENCES " + postgresTableIdentifier(change.ReferenceTable) + " (" + indexColumns(change.ReferenceColumns) + ") ON DELETE " + strings.ToUpper(strings.TrimSpace(change.OnDelete)) + " ON UPDATE " + strings.ToUpper(strings.TrimSpace(change.OnUpdate))
}
