package postgres

import (
	"context"
	"fmt"
	"strings"

	driver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func (s *Service) ListIndexes(ctx context.Context, table string) ([]driver.IndexInfo, error) {
	schema, name := postgresTableParts(table)
	rows, err := s.db.QueryContext(ctx, `
		SELECT index_relation.relname, indexes.indisunique, indexes.indisprimary, attributes.attname
		FROM pg_index AS indexes
		JOIN pg_class AS relation ON relation.oid = indexes.indrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		JOIN pg_class AS index_relation ON index_relation.oid = indexes.indexrelid
		CROSS JOIN LATERAL unnest(indexes.indkey) WITH ORDINALITY AS keys(attribute_number, ordinality)
		JOIN pg_attribute AS attributes ON attributes.attrelid = relation.oid AND attributes.attnum = keys.attribute_number
		WHERE namespace.nspname = $1 AND relation.relname = $2
		ORDER BY index_relation.relname, keys.ordinality`, schema, name)
	if err != nil {
		return nil, fmt.Errorf("reading indexes: %w", err)
	}
	indexes := []driver.IndexInfo{}
	for rows.Next() {
		var indexName, column string
		var unique, primaryKey bool
		if err := rows.Scan(&indexName, &unique, &primaryKey, &column); err != nil {
			return nil, closeRows(rows, "scanning indexes", err)
		}
		if len(indexes) == 0 || indexes[len(indexes)-1].Name != indexName {
			indexes = append(indexes, driver.IndexInfo{Name: sanitizeDisplay(indexName), Unique: unique, PrimaryKey: primaryKey})
		}
		indexes[len(indexes)-1].Columns = append(indexes[len(indexes)-1].Columns, sanitizeDisplay(column))
	}
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating indexes", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing indexes: %w", err)
	}
	return indexes, nil
}

// ListIndexesAll returns every index in the connected database, keyed by
// the indexed table's qualified name (schema.table).
func (s *Service) ListIndexesAll(ctx context.Context) (map[string][]driver.IndexInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT namespace.nspname, relation.relname, index_relation.relname, indexes.indisunique, indexes.indisprimary, attributes.attname
		FROM pg_index AS indexes
		JOIN pg_class AS relation ON relation.oid = indexes.indrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		JOIN pg_class AS index_relation ON index_relation.oid = indexes.indexrelid
		CROSS JOIN LATERAL unnest(indexes.indkey) WITH ORDINALITY AS keys(attribute_number, ordinality)
		JOIN pg_attribute AS attributes ON attributes.attrelid = relation.oid AND attributes.attnum = keys.attribute_number
		WHERE namespace.nspname NOT IN ('pg_catalog', 'pg_toast')
		ORDER BY namespace.nspname, relation.relname, index_relation.relname, keys.ordinality`)
	if err != nil {
		return nil, fmt.Errorf("reading indexes: %w", err)
	}
	indexes := map[string][]driver.IndexInfo{}
	var lastTable, lastName string
	var info *driver.IndexInfo
	finish := func() {
		if info != nil {
			indexes[lastTable] = append(indexes[lastTable], *info)
		}
	}
	for rows.Next() {
		var schema, table, name, column string
		var unique, primaryKey bool
		if err := rows.Scan(&schema, &table, &name, &unique, &primaryKey, &column); err != nil {
			return nil, closeRows(rows, "scanning indexes", err)
		}
		table = schema + "." + table
		if info == nil || table != lastTable || name != lastName {
			finish()
			lastTable, lastName = table, name
			info = &driver.IndexInfo{Name: sanitizeDisplay(name), Unique: unique, PrimaryKey: primaryKey}
		}
		info.Columns = append(info.Columns, sanitizeDisplay(column))
	}
	finish()
	if err := rows.Err(); err != nil {
		return nil, closeRows(rows, "iterating indexes", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing indexes: %w", err)
	}
	return indexes, nil
}

func (s *Service) CreateIndex(ctx context.Context, table string, change driver.IndexChange) error {
	if err := validateIndexChange(change); err != nil {
		return validationError(err)
	}
	statement := postgresCreateIndexStatement(table, change)
	if change.PrimaryKey {
		statement = "ALTER TABLE " + postgresTableIdentifier(table) + " ADD PRIMARY KEY (" + indexColumns(change.Columns) + ")"
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("creating index: %w", err)
	}
	return nil
}

func (s *Service) ReplaceIndex(ctx context.Context, table, previous string, change driver.IndexChange) error {
	if err := validateIndexChange(change); err != nil {
		return validationError(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return connectionError(fmt.Errorf("starting index replacement: %w", err))
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, postgresDropIndexStatement(table, previous)); err != nil {
		return fmt.Errorf("dropping previous index: %w", err)
	}
	statement := postgresCreateIndexStatement(table, change)
	if change.PrimaryKey {
		statement = "ALTER TABLE " + postgresTableIdentifier(table) + " ADD PRIMARY KEY (" + indexColumns(change.Columns) + ")"
	}
	if _, err := tx.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("creating replacement index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing index replacement: %w", err)
	}
	return nil
}

func (s *Service) DropIndex(ctx context.Context, table, name string) error {
	if _, err := s.db.ExecContext(ctx, postgresDropIndexStatement(table, name)); err != nil {
		return fmt.Errorf("dropping index: %w", err)
	}
	return nil
}

func postgresCreateIndexStatement(table string, change driver.IndexChange) string {
	prefix := "CREATE INDEX "
	if change.Unique {
		prefix = "CREATE UNIQUE INDEX "
	}
	return prefix + quoteIdentifier(change.Name) + " ON " + postgresTableIdentifier(table) + " (" + indexColumns(change.Columns) + ")"
}

func postgresDropIndexStatement(table, name string) string {
	if strings.HasSuffix(name, "_pkey") {
		return "ALTER TABLE " + postgresTableIdentifier(table) + " DROP CONSTRAINT " + quoteIdentifier(name)
	}
	schema, _ := postgresTableParts(table)
	return "DROP INDEX " + quoteIdentifier(schema) + "." + quoteIdentifier(name)
}
