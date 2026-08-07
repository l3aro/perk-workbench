package mysql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
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
		SELECT schemata.schema_name, tables.table_type, tables.table_name, tables.table_rows
		FROM information_schema.schemata AS schemata
		LEFT JOIN information_schema.tables AS tables
			ON tables.table_schema = schemata.schema_name
			AND tables.table_type IN ('BASE TABLE', 'VIEW')
		WHERE schemata.schema_name NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
		ORDER BY schemata.schema_name, tables.table_type, tables.table_name`)
	if err != nil {
		return nil, fmt.Errorf("listing schema: %w", err)
	}
	objects := []sharedsql.SchemaObject{}
	lastDatabase := ""
	for rows.Next() {
		var database string
		var tableType, tableName stdsql.NullString
		var tableRows stdsql.NullInt64
		if err := rows.Scan(&database, &tableType, &tableName, &tableRows); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning schema", err)
		}
		database = sharedsql.SanitizeDisplay(database)
		if database != lastDatabase {
			objects = append(objects, sharedsql.SchemaObject{Database: database, Type: "database", Name: database})
			lastDatabase = database
		}
		if tableName.Valid {
			objectType := "view"
			if tableType.String == "BASE TABLE" {
				objectType = "table"
			}
			object := sharedsql.SchemaObject{
				Database: database,
				Type:     objectType,
				Name:     sharedsql.SanitizeDisplay(tableName.String),
			}
			// table_rows is exact for MyISAM, an estimate for InnoDB;
			// NULL for views.
			if tableType.String == "BASE TABLE" && tableRows.Valid {
				count := tableRows.Int64
				object.RowCount = &count
			}
			objects = append(objects, object)
		}
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

func (s *Service) ExecuteReadOnly(ctx context.Context, statement string) (result sharedsql.Result, err error) {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return sharedsql.Result{}, err
	}

	started := time.Now()
	tx, err := s.db.BeginTx(ctx, &stdsql.TxOptions{ReadOnly: true})
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("beginning read-only transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, stdsql.ErrTxDone) && err == nil {
			err = fmt.Errorf("rolling back read-only transaction: %w", rollbackErr)
		}
	}()

	rows, err := tx.QueryContext(ctx, statement)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("executing read-only statement: %w", err)
	}
	result, err = sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.Duration = time.Since(started)
	return result, nil
}

// Validate prepares the statement against the open database without executing
// it, so syntax and schema errors surface without any side effects.
func (s *Service) Validate(ctx context.Context, statement string) error {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return err
	}
	prepared, err := s.db.PrepareContext(ctx, statement)
	if err != nil {
		return fmt.Errorf("validating statement: %w", err)
	}
	return prepared.Close()
}

func mysqlTableParts(table string) (database, name string) {
	database, name, found := strings.Cut(table, ".")
	if !found {
		return "", table
	}
	return database, name
}

func mysqlTableIdentifier(table string) string {
	database, name := mysqlTableParts(table)
	if database == "" {
		return quoteIdentifier(name)
	}
	return quoteIdentifier(database) + "." + quoteIdentifier(name)
}

func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	database, table := mysqlTableParts(name)
	rows, err := s.db.QueryContext(ctx, `
		SELECT column_name, column_type, is_nullable, column_default, column_key, extra
		FROM information_schema.columns
		WHERE table_schema = COALESCE(NULLIF(?, ''), DATABASE()) AND table_name = ?
		ORDER BY ordinal_position`, database, table)
	if err != nil {
		return nil, fmt.Errorf("reading table info: %w", err)
	}
	columns := []sharedsql.ColumnInfo{}
	for rows.Next() {
		var column sharedsql.ColumnInfo
		var nullable, key string
		var defaultValue stdsql.NullString
		if err := rows.Scan(&column.Name, &column.Type, &nullable, &defaultValue, &key, &column.Attributes); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning table info", err)
		}
		column.Name = sharedsql.SanitizeDisplay(column.Name)
		column.Type = sharedsql.SanitizeDisplay(column.Type)
		column.Attributes = sharedsql.SanitizeDisplay(column.Attributes)
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
	if change.Name == change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && mysqlDefaultsEqual(change.DefaultValue, current.DefaultValue) && (change.Attributes == nil || *change.Attributes == current.Attributes) {
		return nil
	}
	if current.PrimaryKey > 0 {
		if change.Name != change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && mysqlDefaultsEqual(change.DefaultValue, current.DefaultValue) && (change.Attributes == nil || *change.Attributes == current.Attributes) {
			_, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(change.PreviousName)+" TO "+quoteIdentifier(change.Name))
			return err
		}
		return errors.New("primary-key columns can only be renamed without other changes")
	}
	if change.Name != change.PreviousName && change.Type == current.Type && change.Nullable == current.Nullable && mysqlDefaultsEqual(change.DefaultValue, current.DefaultValue) && (change.Attributes == nil || *change.Attributes == current.Attributes) {
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" RENAME COLUMN "+quoteIdentifier(change.PreviousName)+" TO "+quoteIdentifier(change.Name)); err != nil {
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
	statement := "ALTER TABLE " + mysqlTableIdentifier(table) + " CHANGE COLUMN " + quoteIdentifier(change.PreviousName) + " " + quoteIdentifier(change.Name) + " " + mysqlColumnDeclaration(change, attributes)
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("altering column: %w", err)
	}
	return nil
}

func (s *Service) AddColumn(ctx context.Context, table string, col sharedsql.ColumnDef) error {
	if err := sharedsql.ValidateColumnDef(col); err != nil {
		return err
	}
	statement := "ALTER TABLE " + mysqlTableIdentifier(table) + " ADD COLUMN " + quoteIdentifier(col.Name) + " " + strings.TrimSpace(col.Type)
	if !col.Nullable {
		statement += " NOT NULL"
	}
	if col.Nullable {
		statement += " NULL"
	}
	if col.DefaultValue != nil {
		statement += " DEFAULT " + mysqlDefault(*col.DefaultValue)
	}
	if col.Attributes != nil && *col.Attributes != "" {
		statement += " " + *col.Attributes
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("adding column: %w", err)
	}
	return nil
}

func (s *Service) DropColumn(ctx context.Context, table, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("column name is required")
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+mysqlTableIdentifier(table)+" DROP COLUMN "+quoteIdentifier(name)); err != nil {
		return fmt.Errorf("dropping column: %w", err)
	}
	return nil
}

func mysqlColumnDeclaration(change sharedsql.ColumnChange, attributes mysqlColumnAttributes) string {
	declaration := strings.TrimSpace(change.Type)
	if attributes.characterSet.Valid {
		declaration += " CHARACTER SET " + attributes.characterSet.String
	}
	if attributes.collation.Valid {
		declaration += " COLLATE " + attributes.collation.String
	}
	if change.Nullable {
		declaration += " NULL"
	} else {
		declaration += " NOT NULL"
	}
	if change.DefaultValue != nil {
		declaration += " DEFAULT " + mysqlDefault(*change.DefaultValue)
	}
	if change.Attributes != nil && *change.Attributes != "" {
		declaration += " " + *change.Attributes
	} else if attributes.comment.Valid && attributes.comment.String != "" {
		declaration += " COMMENT " + mysqlDefault(attributes.comment.String)
	}
	return declaration
}

type mysqlColumnAttributes struct {
	extra                            string
	comment, characterSet, collation stdsql.NullString
}

func (s *Service) columnAttributes(ctx context.Context, table, column string) (mysqlColumnAttributes, error) {
	database, name := mysqlTableParts(table)
	var attributes mysqlColumnAttributes
	err := s.db.QueryRowContext(ctx, `
		SELECT extra, column_comment, character_set_name, collation_name
		FROM information_schema.columns
		WHERE table_schema = COALESCE(NULLIF(?, ''), DATABASE()) AND table_name = ? AND column_name = ?`, database, name, column).Scan(&attributes.extra, &attributes.comment, &attributes.characterSet, &attributes.collation)
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

func (s *Service) BrowseTable(ctx context.Context, name string, options sharedsql.BrowseOptions) (sharedsql.Result, error) {
	if options.Offset < 0 || options.Limit < 1 || options.Limit > sharedsql.MaxRows {
		return sharedsql.Result{}, fmt.Errorf("invalid browse range: offset=%d limit=%d", options.Offset, options.Limit)
	}
	statement := "SELECT * FROM " + mysqlTableIdentifier(name)
	args := make([]any, 0, len(options.Filters)+2)
	valid := make(map[string]bool, len(options.Columns))
	for _, column := range options.Columns {
		valid[column] = true
	}
	if len(options.Filters) > 0 {
		terms := make([]string, 0, len(options.Filters))
		for _, filter := range options.Filters {
			if !valid[filter.Column] {
				return sharedsql.Result{}, fmt.Errorf("invalid browse filter column: %s", filter.Column)
			}
			column := quoteIdentifier(filter.Column)
			switch filter.Operator {
			case sharedsql.BrowseFilterLike, sharedsql.BrowseFilterNotLike:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterLess, sharedsql.BrowseFilterLessEqual, sharedsql.BrowseFilterGreater, sharedsql.BrowseFilterGreaterEqual:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull:
				terms = append(terms, column+" "+string(filter.Operator))
			default:
				return sharedsql.Result{}, fmt.Errorf("invalid browse filter operator: %q", filter.Operator)
			}
		}
		statement += " WHERE " + strings.Join(terms, " AND ")
	}
	if len(options.Sorts) > 0 {
		orders := make([]string, 0, len(options.Sorts))
		for _, sort := range options.Sorts {
			if !valid[sort.Column] {
				continue
			}
			order := quoteIdentifier(sort.Column)
			if sort.Descending {
				order += " DESC"
			}
			orders = append(orders, order)
		}
		if len(orders) > 0 {
			statement += " ORDER BY " + strings.Join(orders, ", ")
		}
	}
	args = append(args, options.Limit+1, options.Offset)
	rows, err := s.db.QueryContext(ctx, statement+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return sharedsql.Result{}, fmt.Errorf("browsing table: %w", err)
	}
	result, err := sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.HasMore = len(result.Rows) > options.Limit
	if result.HasMore {
		result.Rows = result.Rows[:options.Limit]
		result.UntruncatedRows = result.UntruncatedRows[:options.Limit]
	}
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
