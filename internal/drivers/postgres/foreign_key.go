package postgres

import (
	"context"
	"fmt"
	"strings"

	driver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func (s *Service) ListForeignKeys(ctx context.Context, table string) ([]driver.ForeignKeyInfo, error) {
	schema, name := postgresTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT constraints.constraint_name, key_columns.column_name, referenced_columns.table_schema, referenced_columns.table_name,
			referenced_columns.column_name, refs.delete_rule, refs.update_rule
		FROM information_schema.table_constraints AS constraints
		JOIN information_schema.key_column_usage AS key_columns
			ON key_columns.constraint_catalog = constraints.constraint_catalog
			AND key_columns.constraint_schema = constraints.constraint_schema
			AND key_columns.constraint_name = constraints.constraint_name
		JOIN information_schema.referential_constraints AS refs
			ON refs.constraint_catalog = constraints.constraint_catalog
			AND refs.constraint_schema = constraints.constraint_schema
			AND refs.constraint_name = constraints.constraint_name
		JOIN information_schema.key_column_usage AS referenced_columns
			ON referenced_columns.constraint_catalog = refs.unique_constraint_catalog
			AND referenced_columns.constraint_schema = refs.unique_constraint_schema
			AND referenced_columns.constraint_name = refs.unique_constraint_name
			AND referenced_columns.ordinal_position = key_columns.position_in_unique_constraint
		WHERE constraints.constraint_type = 'FOREIGN KEY' AND constraints.table_schema = $1 AND constraints.table_name = $2
		ORDER BY constraints.constraint_name, key_columns.ordinal_position`, schema, name)
	if err != nil {
		return nil, fmt.Errorf("reading foreign keys: %w", err)
	}
	foreignKeys := []driver.ForeignKeyInfo{}
	for rows.Next() {
		var id, column, referenceSchema, referenceTable, referenceColumn, onDelete, onUpdate string
		if err := rows.Scan(&id, &column, &referenceSchema, &referenceTable, &referenceColumn, &onDelete, &onUpdate); err != nil {
			return nil, closeRows(rows, "scanning foreign keys", err)
		}
		if len(foreignKeys) == 0 || foreignKeys[len(foreignKeys)-1].ID != id {
			foreignKeys = append(foreignKeys, driver.ForeignKeyInfo{ID: sanitizeDisplay(id), ReferenceTable: sanitizeDisplay(referenceSchema + "." + referenceTable), OnDelete: onDelete, OnUpdate: onUpdate})
		}
		foreignKeys[len(foreignKeys)-1].Columns = append(foreignKeys[len(foreignKeys)-1].Columns, sanitizeDisplay(column))
		foreignKeys[len(foreignKeys)-1].ReferenceColumns = append(foreignKeys[len(foreignKeys)-1].ReferenceColumns, sanitizeDisplay(referenceColumn))
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing foreign-key rows: %w", err)
	}
	return foreignKeys, nil
}

func (s *Service) ListReferencingForeignKeys(ctx context.Context, table string) ([]driver.ReferencingForeignKeyInfo, error) {
	schema, name := postgresTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT constraints.table_schema, constraints.table_name, constraints.constraint_name, key_columns.column_name,
			referenced_columns.column_name, refs.delete_rule, refs.update_rule
		FROM information_schema.table_constraints AS constraints
		JOIN information_schema.key_column_usage AS key_columns
			ON key_columns.constraint_catalog = constraints.constraint_catalog
			AND key_columns.constraint_schema = constraints.constraint_schema
			AND key_columns.constraint_name = constraints.constraint_name
		JOIN information_schema.referential_constraints AS refs
			ON refs.constraint_catalog = constraints.constraint_catalog
			AND refs.constraint_schema = constraints.constraint_schema
			AND refs.constraint_name = constraints.constraint_name
		JOIN information_schema.key_column_usage AS referenced_columns
			ON referenced_columns.constraint_catalog = refs.unique_constraint_catalog
			AND referenced_columns.constraint_schema = refs.unique_constraint_schema
			AND referenced_columns.constraint_name = refs.unique_constraint_name
			AND referenced_columns.ordinal_position = key_columns.position_in_unique_constraint
		WHERE constraints.constraint_type = 'FOREIGN KEY' AND referenced_columns.table_schema = $1 AND referenced_columns.table_name = $2
		ORDER BY constraints.table_schema, constraints.table_name, constraints.constraint_name, key_columns.ordinal_position`, schema, name)
	if err != nil {
		return nil, fmt.Errorf("reading referencing foreign keys: %w", err)
	}
	references := []driver.ReferencingForeignKeyInfo{}
	for rows.Next() {
		var tableSchema, tableName, id, column, referenceColumn, onDelete, onUpdate string
		if err := rows.Scan(&tableSchema, &tableName, &id, &column, &referenceColumn, &onDelete, &onUpdate); err != nil {
			return nil, closeRows(rows, "scanning referencing foreign keys", err)
		}
		foreignTable := sanitizeDisplay(tableSchema + "." + tableName)
		if len(references) == 0 || references[len(references)-1].Table != foreignTable || references[len(references)-1].ID != id {
			references = append(references, driver.ReferencingForeignKeyInfo{Table: foreignTable, ForeignKeyInfo: driver.ForeignKeyInfo{ID: sanitizeDisplay(id), ReferenceTable: schema + "." + name, OnDelete: onDelete, OnUpdate: onUpdate}})
		}
		references[len(references)-1].Columns = append(references[len(references)-1].Columns, sanitizeDisplay(column))
		references[len(references)-1].ReferenceColumns = append(references[len(references)-1].ReferenceColumns, sanitizeDisplay(referenceColumn))
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating referencing foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing referencing-foreign-key rows: %w", err)
	}
	return references, nil
}

// ListForeignKeysAll returns every foreign key in the connected database,
// keyed by the declaring table's qualified name (schema.table).
func (s *Service) ListForeignKeysAll(ctx context.Context) (map[string][]driver.ForeignKeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT constraints.table_schema, constraints.table_name, constraints.constraint_name, key_columns.column_name,
			referenced_columns.table_schema, referenced_columns.table_name, referenced_columns.column_name,
			refs.delete_rule, refs.update_rule
		FROM information_schema.table_constraints AS constraints
		JOIN information_schema.key_column_usage AS key_columns
			ON key_columns.constraint_catalog = constraints.constraint_catalog
			AND key_columns.constraint_schema = constraints.constraint_schema
			AND key_columns.constraint_name = constraints.constraint_name
		JOIN information_schema.referential_constraints AS refs
			ON refs.constraint_catalog = constraints.constraint_catalog
			AND refs.constraint_schema = constraints.constraint_schema
			AND refs.constraint_name = constraints.constraint_name
		JOIN information_schema.key_column_usage AS referenced_columns
			ON referenced_columns.constraint_catalog = refs.unique_constraint_catalog
			AND referenced_columns.constraint_schema = refs.unique_constraint_schema
			AND referenced_columns.constraint_name = refs.unique_constraint_name
			AND referenced_columns.ordinal_position = key_columns.position_in_unique_constraint
		WHERE constraints.constraint_type = 'FOREIGN KEY'
		ORDER BY constraints.table_schema, constraints.table_name, constraints.constraint_name, key_columns.ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("reading foreign keys: %w", err)
	}
	foreignKeys := map[string][]driver.ForeignKeyInfo{}
	var lastTable, lastID string
	var info *driver.ForeignKeyInfo
	finish := func() {
		if info != nil {
			foreignKeys[lastTable] = append(foreignKeys[lastTable], *info)
		}
	}
	for rows.Next() {
		var tableSchema, tableName, id, column, referenceSchema, referenceTable, referenceColumn, onDelete, onUpdate string
		if err := rows.Scan(&tableSchema, &tableName, &id, &column, &referenceSchema, &referenceTable, &referenceColumn, &onDelete, &onUpdate); err != nil {
			return nil, closeRows(rows, "scanning foreign keys", err)
		}
		table := tableSchema + "." + tableName
		if info == nil || table != lastTable || id != lastID {
			finish()
			lastTable, lastID = table, id
			info = &driver.ForeignKeyInfo{ID: sanitizeDisplay(id), ReferenceTable: sanitizeDisplay(referenceSchema + "." + referenceTable), OnDelete: onDelete, OnUpdate: onUpdate}
		}
		info.Columns = append(info.Columns, sanitizeDisplay(column))
		info.ReferenceColumns = append(info.ReferenceColumns, sanitizeDisplay(referenceColumn))
	}
	finish()
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing foreign-key rows: %w", err)
	}
	return foreignKeys, nil
}

func (s *Service) CreateForeignKey(ctx context.Context, table string, change driver.ForeignKeyChange) error {
	if err := validateForeignKeyChange(change); err != nil {
		return validationError(err)
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+postgresTableIdentifier(table)+" ADD "+postgresForeignKeyClause(change)); err != nil {
		return fmt.Errorf("creating foreign key: %w", err)
	}
	s.validationCache.clear()
	return nil
}

func (s *Service) ReplaceForeignKey(ctx context.Context, table, previous string, change driver.ForeignKeyChange) error {
	if err := validateForeignKeyChange(change); err != nil {
		return validationError(err)
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
	s.validationCache.clear()
	return nil
}

func (s *Service) DropForeignKey(ctx context.Context, table, previous string) error {
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+postgresTableIdentifier(table)+" DROP CONSTRAINT "+quoteIdentifier(previous)); err != nil {
		return fmt.Errorf("dropping foreign key: %w", err)
	}
	s.validationCache.clear()
	return nil
}

func postgresForeignKeyClause(change driver.ForeignKeyChange) string {
	return "FOREIGN KEY (" + indexColumns(change.Columns) + ") REFERENCES " + postgresTableIdentifier(change.ReferenceTable) + " (" + indexColumns(change.ReferenceColumns) + ") ON DELETE " + strings.ToUpper(strings.TrimSpace(change.OnDelete)) + " ON UPDATE " + strings.ToUpper(strings.TrimSpace(change.OnUpdate))
}
