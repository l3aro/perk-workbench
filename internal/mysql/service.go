package mysql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type Service struct {
	db   *stdsql.DB
	info sharedsql.DatabaseInfo
}

func Open(ctx context.Context, dsn string) (*Service, error) {
	db, err := stdsql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening mysql database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging mysql database: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("pinging mysql database: %w", err)
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("reading mysql version: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("reading mysql version: %w", err)
	}
	return &Service{db: db, info: sharedsql.DatabaseInfo{Product: "MySQL", Version: version}}, nil
}

func (s *Service) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing mysql database: %w", err)
	}
	return nil
}

func (s *Service) Info() sharedsql.DatabaseInfo { return s.info }

func (s *Service) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT CASE table_type WHEN 'BASE TABLE' THEN 'table' ELSE 'view' END, table_name
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_type, table_name`)
	if err != nil {
		return nil, fmt.Errorf("listing schema: %w", err)
	}
	objects := []sharedsql.SchemaObject{}
	for rows.Next() {
		var object sharedsql.SchemaObject
		if err := rows.Scan(&object.Type, &object.Name); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning schema", err)
		}
		object.Type = sharedsql.SanitizeDisplay(object.Type)
		object.Name = sharedsql.SanitizeDisplay(object.Name)
		objects = append(objects, object)
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
		return sharedsql.Result{}, fmt.Errorf("acquiring mysql connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("closing mysql connection: %w", closeErr))
				return
			}
			result = sharedsql.Result{}
			err = fmt.Errorf("closing mysql connection: %w", closeErr)
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

func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT column_name, column_type, is_nullable, column_default, column_key
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, name)
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}
	columns := []sharedsql.ColumnInfo{}
	for rows.Next() {
		var column sharedsql.ColumnInfo
		var nullable, key string
		var defaultValue stdsql.NullString
		if err := rows.Scan(&column.Name, &column.Type, &nullable, &defaultValue, &key); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		column.Name = sharedsql.SanitizeDisplay(column.Name)
		column.Type = sharedsql.SanitizeDisplay(column.Type)
		column.Nullable = nullable == "YES"
		switch key {
		case "PRI":
			column.PrimaryKey = 1
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexPrimaryKey}
		case "UNI":
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexUnique}
		case "MUL":
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexRegular}
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
	if change.Name == change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && mysqlDefaultsEqual(change.DefaultValue, current.DefaultValue) {
		return nil
	}
	if current.PrimaryKey > 0 {
		if change.Name != change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && mysqlDefaultsEqual(change.DefaultValue, current.DefaultValue) {
			_, err := s.db.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(change.PreviousName)+" TO "+quoteIdentifier(change.Name))
			return err
		}
		return errors.New("primary-key columns can only be renamed without other changes")
	}
	if change.Name != change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && mysqlDefaultsEqual(change.DefaultValue, current.DefaultValue) {
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+quoteIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(change.PreviousName)+" TO "+quoteIdentifier(change.Name)); err != nil {
			return fmt.Errorf("renaming column: %w", err)
		}
		return nil
	}
	attributes, err := s.columnAttributes(ctx, table, change.PreviousName)
	if err != nil {
		return err
	}
	if attributes.extra != "" {
		return fmt.Errorf("column %q has unsupported attributes: %s", change.PreviousName, attributes.extra)
	}
	statement := "ALTER TABLE " + quoteIdentifier(table) + " CHANGE COLUMN " + quoteIdentifier(change.PreviousName) + " " + quoteIdentifier(change.Name) + " " + strings.TrimSpace(change.Type)
	if change.Nullable {
		statement += " NULL"
	} else {
		statement += " NOT NULL"
	}
	if change.DefaultValue != nil {
		statement += " DEFAULT " + mysqlDefault(*change.DefaultValue)
	}
	if attributes.characterSet.Valid {
		statement += " CHARACTER SET " + attributes.characterSet.String
	}
	if attributes.collation.Valid {
		statement += " COLLATE " + attributes.collation.String
	}
	if attributes.comment.Valid && attributes.comment.String != "" {
		statement += " COMMENT " + mysqlDefault(attributes.comment.String)
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("altering column: %w", err)
	}
	return nil
}

type mysqlColumnAttributes struct {
	extra                            string
	comment, characterSet, collation stdsql.NullString
}

func (s *Service) columnAttributes(ctx context.Context, table, column string) (mysqlColumnAttributes, error) {
	var attributes mysqlColumnAttributes
	err := s.db.QueryRowContext(ctx, `
		SELECT extra, column_comment, character_set_name, collation_name
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&attributes.extra, &attributes.comment, &attributes.characterSet, &attributes.collation)
	if err != nil {
		return mysqlColumnAttributes{}, fmt.Errorf("reading column attributes: %w", err)
	}
	return attributes, nil
}

func mysqlDefault(value string) string {
	trimmed := strings.TrimSpace(value)
	switch strings.ToUpper(trimmed) {
	case "NULL", "CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP":
		return trimmed
	}
	if numericDefault(trimmed) {
		return trimmed
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
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

func (s *Service) BrowseTable(ctx context.Context, name string, offset, limit int) (sharedsql.Result, error) {
	if offset < 0 || limit < 1 {
		return sharedsql.Result{}, fmt.Errorf("invalid page: offset=%d limit=%d", offset, limit)
	}
	var totalRows int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdentifier(name)).Scan(&totalRows); err != nil {
		return sharedsql.Result{}, fmt.Errorf("counting table rows: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+quoteIdentifier(name)+" LIMIT ? OFFSET ?", limit, offset)
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
	if index := strings.IndexAny(statement, " \t\n\r("); index >= 0 {
		statement = statement[:index]
	}
	switch strings.ToUpper(statement) {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH":
		return true
	default:
		return false
	}
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func mysqlDefaultsEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
