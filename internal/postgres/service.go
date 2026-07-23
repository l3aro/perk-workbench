package postgres

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type Service struct {
	db   *stdsql.DB
	info sharedsql.DatabaseInfo
}

func Open(ctx context.Context, dsn string) (*Service, error) {
	db, err := stdsql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgresql database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging postgresql database: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("pinging postgresql database: %w", err)
	}
	var version string
	if err := db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("reading postgresql version: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("reading postgresql version: %w", err)
	}
	return &Service{db: db, info: sharedsql.DatabaseInfo{Product: "PostgreSQL", Version: version}}, nil
}

func (s *Service) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing postgresql database: %w", err)
	}
	return nil
}

func (s *Service) Info() sharedsql.DatabaseInfo { return s.info }

func (s *Service) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT table_schema, table_type, table_name
		FROM information_schema.tables
		WHERE table_schema NOT IN ('information_schema', 'pg_catalog')
			AND table_schema NOT LIKE 'pg_toast%'
		ORDER BY table_schema, table_type, table_name`)
	if err != nil {
		return nil, fmt.Errorf("listing schema: %w", err)
	}
	objects := []sharedsql.SchemaObject{}
	lastSchema := ""
	for rows.Next() {
		var schema, tableType, tableName string
		if err := rows.Scan(&schema, &tableType, &tableName); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning schema", err)
		}
		schema = sharedsql.SanitizeDisplay(schema)
		if schema != lastSchema {
			objects = append(objects, sharedsql.SchemaObject{Database: schema, Type: "database", Name: schema})
			lastSchema = schema
		}
		objectType := "view"
		if tableType == "BASE TABLE" {
			objectType = "table"
		}
		objects = append(objects, sharedsql.SchemaObject{Database: schema, Type: objectType, Name: sharedsql.SanitizeDisplay(tableName)})
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating schema", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing schema rows: %w", err)
	}
	return objects, nil
}

func (s *Service) Execute(ctx context.Context, statement string) (result sharedsql.Result, err error) {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return sharedsql.Result{}, err
	}
	started := time.Now()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("acquiring postgresql connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing postgresql connection: %w", closeErr))
				return
			}
			result = sharedsql.Result{}
			err = fmt.Errorf("closing postgresql connection: %w", closeErr)
		}
	}()
	if !ReturnsRows(statement) {
		execution, err := conn.ExecContext(ctx, statement)
		if err != nil {
			return sharedsql.Result{}, fmt.Errorf("executing statement: %w", err)
		}
		result.RowsAffected, err = execution.RowsAffected()
		if err != nil {
			return sharedsql.Result{}, fmt.Errorf("reading affected rows: %w", err)
		}
		result.Duration = time.Since(started)
		return result, nil
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("executing statement: %w", err)
	}
	result, err = sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.Duration = time.Since(started)
	return result, nil
}

func postgresTableParts(table string) (schema, name string) {
	schema, name, found := strings.Cut(table, ".")
	if !found {
		return "public", table
	}
	return schema, name
}

func postgresTableIdentifier(table string) string {
	schema, name := postgresTableParts(table)
	return quoteIdentifier(schema) + "." + quoteIdentifier(name)
}

func quoteIdentifier(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func indexColumns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quoteIdentifier(strings.TrimSpace(column))
	}
	return strings.Join(quoted, ", ")
}

func ReturnsRows(statement string) bool {
	for {
		statement = strings.TrimSpace(strings.TrimLeft(statement, "("))
		switch {
		case strings.HasPrefix(statement, "--"):
			if index := strings.IndexByte(statement, '\n'); index >= 0 {
				statement = statement[index+1:]
				continue
			}
			return false
		case strings.HasPrefix(statement, "/*"):
			index := strings.Index(statement[2:], "*/")
			if index < 0 {
				return false
			}
			statement = statement[index+4:]
			continue
		}
		break
	}
	keyword := statement
	if index := strings.IndexAny(keyword, " \t\n\r("); index >= 0 {
		keyword = keyword[:index]
	}
	switch strings.ToUpper(keyword) {
	case "SELECT", "SHOW", "EXPLAIN", "WITH", "VALUES", "TABLE":
		return true
	case "INSERT", "UPDATE", "DELETE", "MERGE":
		return strings.Contains(strings.ToUpper(statement), "RETURNING")
	default:
		return false
	}
}

func (s *Service) BrowseTable(ctx context.Context, name string, offset, limit int) (sharedsql.Result, error) {
	if offset < 0 || limit < 1 {
		return sharedsql.Result{}, fmt.Errorf("invalid page: offset=%d limit=%d", offset, limit)
	}
	identifier := postgresTableIdentifier(name)
	var totalRows int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+identifier).Scan(&totalRows); err != nil {
		return sharedsql.Result{}, fmt.Errorf("counting table rows: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+identifier+" LIMIT $1 OFFSET $2", limit, offset)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("browsing table: %w", err)
	}
	result, err := sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.TotalRows = totalRows
	return result, nil
}

func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	schema, table := postgresTableParts(name)
	rows, err := s.db.QueryContext(ctx, `
		SELECT attributes.attname, pg_catalog.format_type(attributes.atttypid, attributes.atttypmod),
			NOT attributes.attnotnull, pg_get_expr(defaults.adbin, defaults.adrelid),
			COALESCE(primary_key.ordinality, 0), attributes.attidentity, attributes.attgenerated
		FROM pg_attribute AS attributes
		JOIN pg_class AS relation ON relation.oid = attributes.attrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		LEFT JOIN pg_attrdef AS defaults ON defaults.adrelid = relation.oid AND defaults.adnum = attributes.attnum
		LEFT JOIN LATERAL (
			SELECT keys.ordinality
			FROM pg_index AS indexes
			CROSS JOIN LATERAL unnest(indexes.indkey) WITH ORDINALITY AS keys(attribute_number, ordinality)
			WHERE indexes.indrelid = relation.oid AND indexes.indisprimary AND keys.attribute_number = attributes.attnum
		) AS primary_key ON true
		WHERE namespace.nspname = $1 AND relation.relname = $2 AND attributes.attnum > 0 AND NOT attributes.attisdropped
		ORDER BY attributes.attnum`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}
	columns := []sharedsql.ColumnInfo{}
	for rows.Next() {
		var column sharedsql.ColumnInfo
		var defaultValue stdsql.NullString
		var identity, generated string
		if err := rows.Scan(&column.Name, &column.Type, &column.Nullable, &defaultValue, &column.PrimaryKey, &identity, &generated); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		column.Name, column.Type = sharedsql.SanitizeDisplay(column.Name), sharedsql.SanitizeDisplay(column.Type)
		switch identity {
		case "a":
			column.Attributes = "IDENTITY ALWAYS"
		case "d":
			column.Attributes = "IDENTITY BY DEFAULT"
		}
		if generated == "s" {
			column.Attributes = strings.TrimSpace(column.Attributes + " GENERATED STORED")
		}
		if column.PrimaryKey > 0 {
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexPrimaryKey}
		}
		if defaultValue.Valid {
			value := sharedsql.SanitizeDisplay(defaultValue.String)
			column.DefaultValue = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating table info", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing table info rows: %w", err)
	}
	return columns, nil
}
func (s *Service) AlterColumn(ctx context.Context, table string, change sharedsql.ColumnChange) error {
	if err := sharedsql.ValidateColumnChange(change); err != nil {
		return err
	}
	columns, err := s.TableInfo(ctx, table)
	if err != nil {
		return err
	}
	var currentInfo sharedsql.ColumnInfo
	for _, column := range columns {
		if column.Name == change.PreviousName {
			currentInfo = column
			break
		}
	}
	if currentInfo.Name == "" {
		return fmt.Errorf("column %q was not found", change.PreviousName)
	}
	typeChanged := !strings.EqualFold(strings.TrimSpace(change.Type), strings.TrimSpace(currentInfo.Type))
	defaultChanged := !postgresDefaultsEqual(change.DefaultValue, currentInfo.DefaultValue)
	if change.Name == change.PreviousName && !typeChanged && change.Nullable == currentInfo.Nullable && !defaultChanged {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting column alteration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	identifier := postgresTableIdentifier(table)
	previous, current := quoteIdentifier(change.PreviousName), quoteIdentifier(change.Name)
	if change.Name != change.PreviousName {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+identifier+" RENAME COLUMN "+previous+" TO "+current); err != nil {
			return fmt.Errorf("renaming column: %w", err)
		}
	}
	if typeChanged {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+identifier+" ALTER COLUMN "+current+" TYPE "+strings.TrimSpace(change.Type)+" USING "+current+"::"+strings.TrimSpace(change.Type)); err != nil {
			return fmt.Errorf("changing column type: %w", err)
		}
	}
	if change.Nullable != currentInfo.Nullable {
		nullability := "SET NOT NULL"
		if change.Nullable {
			nullability = "DROP NOT NULL"
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+identifier+" ALTER COLUMN "+current+" "+nullability); err != nil {
			return fmt.Errorf("changing column nullability: %w", err)
		}
	}
	if defaultChanged {
		defaultStatement := "DROP DEFAULT"
		if change.DefaultValue != nil {
			defaultStatement = "SET DEFAULT " + postgresDefault(*change.DefaultValue)
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+identifier+" ALTER COLUMN "+current+" "+defaultStatement); err != nil {
			return fmt.Errorf("changing column default: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing column alteration: %w", err)
	}
	return nil
}

func postgresDefault(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "''"
	}
	if strings.EqualFold(trimmed, "NULL") || strings.EqualFold(trimmed, "CURRENT_DATE") || strings.EqualFold(trimmed, "CURRENT_TIME") || strings.EqualFold(trimmed, "CURRENT_TIMESTAMP") || numericDefault(trimmed) {
		return trimmed
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func postgresDefaultsEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func numericDefault(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character == '+' || character == '-' {
			if index == 0 {
				continue
			}
			return false
		}
		if character == '.' {
			continue
		}
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
