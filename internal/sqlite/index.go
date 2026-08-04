package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"slices"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func (s *Service) ListIndexes(ctx context.Context, table string) ([]sharedsql.IndexInfo, error) {
	// One query instead of 1+N: join pragma_index_list with
	// pragma_index_info via the table-valued pragma functions.
	rows, err := s.db.QueryContext(ctx,
		"SELECT il.name, il.\"unique\", il.origin, ii.name FROM pragma_index_list("+sqlLiteral(table)+") il JOIN pragma_index_info(il.name) ii ORDER BY il.seq, ii.seqno")
	if err != nil {
		return nil, fmt.Errorf("reading indexes: %w", err)
	}
	type listedIndex struct {
		name       string
		unique     bool
		primaryKey bool
	}
	indexes := make([]sharedsql.IndexInfo, 0, 8)
	var primary sharedsql.IndexInfo
	var current *listedIndex
	columns := []string{}
	flush := func() {
		if current == nil {
			return
		}
		info := sharedsql.IndexInfo{
			Name:       sharedsql.SanitizeDisplay(current.name),
			Unique:     current.unique,
			PrimaryKey: current.primaryKey,
			Columns:    columns,
		}
		if info.PrimaryKey {
			info.Name = "PRIMARY"
			primary = info
		} else {
			indexes = append(indexes, info)
		}
	}
	for rows.Next() {
		var unique int
		var indexName, origin string
		var column stdsql.NullString
		if err := rows.Scan(&indexName, &unique, &origin, &column); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning indexes", err)
		}
		if origin != "c" && origin != "pk" {
			continue
		}
		if current == nil || current.name != indexName {
			flush()
			current = &listedIndex{name: indexName, unique: unique != 0, primaryKey: origin == "pk"}
			columns = []string{}
		}
		if column.Valid {
			columns = append(columns, sharedsql.SanitizeDisplay(column.String))
		}
	}
	flush()
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating indexes", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing indexes: %w", err)
	}

	if len(primary.Columns) == 0 {
		pk, err := tablePKColumns(ctx, s.db, table)
		if err != nil {
			return nil, err
		}
		if len(pk) > 0 {
			primary = sharedsql.IndexInfo{Name: "PRIMARY", PrimaryKey: true, Columns: pk}
		}
	}
	if len(primary.Columns) > 0 {
		indexes = append([]sharedsql.IndexInfo{primary}, indexes...)
	}
	return indexes, nil
}

func tablePKColumns(ctx context.Context, queryer tableInfoQuerier, table string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, "PRAGMA table_xinfo("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}
	type pkCol struct {
		name     string
		position int
	}
	var pks []pkCol
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var name, colType string
		var defaultValue stdsql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		if primaryKey > 0 {
			pks = append(pks, pkCol{name: sharedsql.SanitizeDisplay(name), position: primaryKey})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating table info", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table info rows: %w", err)
	}
	slices.SortFunc(pks, func(a, b pkCol) int { return a.position - b.position })
	columns := make([]string, len(pks))
	for index, primaryKey := range pks {
		columns[index] = primaryKey.name
	}
	return columns, nil
}

func sqliteIndexColumns(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
}, name string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, "PRAGMA index_info("+quoteIdentifier(name)+")")
	if err != nil {
		return nil, fmt.Errorf("reading index %q columns: %w", name, err)
	}
	columns := []string{}
	for rows.Next() {
		var sequence, columnID int
		var column stdsql.NullString
		if err := rows.Scan(&sequence, &columnID, &column); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning index columns", err)
		}
		if !column.Valid {
			return nil, sharedsql.CloseRows(rows, "scanning index columns", fmt.Errorf("index %q uses an expression", name))
		}
		columns = append(columns, sharedsql.SanitizeDisplay(column.String))
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating index columns", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing index columns: %w", err)
	}
	return columns, nil
}

func (s *Service) CreateIndex(ctx context.Context, table string, change sharedsql.IndexChange) error {
	if err := sharedsql.ValidateIndexChange(change); err != nil {
		return err
	}
	if change.PrimaryKey {
		return s.changePrimaryKey(ctx, table, change.Columns, false)
	}
	if _, err := s.db.ExecContext(ctx, sqliteCreateIndexStatement(table, change)); err != nil {
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
		return s.changePrimaryKey(ctx, table, change.Columns, true)
	}
	if change.PrimaryKey {
		return fmt.Errorf("create a primary key separately before replacing this index")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting index replacement: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DROP INDEX "+quoteIdentifier(previous)); err != nil {
		return fmt.Errorf("dropping index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, sqliteCreateIndexStatement(table, change)); err != nil {
		return fmt.Errorf("creating replacement index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing index replacement: %w", err)
	}
	return nil
}

func (s *Service) DropIndex(ctx context.Context, table, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("index name is required")
	}
	if name == "PRIMARY" {
		return s.changePrimaryKey(ctx, table, nil, true)
	}
	if _, err := s.db.ExecContext(ctx, "DROP INDEX "+quoteIdentifier(name)); err != nil {
		return fmt.Errorf("dropping index: %w", err)
	}
	return nil
}

func sqliteCreateIndexStatement(table string, change sharedsql.IndexChange) string {
	prefix := "CREATE INDEX "
	if change.Unique {
		prefix = "CREATE UNIQUE INDEX "
	}
	columns := make([]string, len(change.Columns))
	for index, column := range change.Columns {
		columns[index] = quoteIdentifier(strings.TrimSpace(column))
	}
	return prefix + quoteIdentifier(change.Name) + " ON " + quoteIdentifier(table) + " (" + strings.Join(columns, ", ") + ")"
}
