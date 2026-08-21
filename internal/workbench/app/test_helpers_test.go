package app

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	_ "modernc.org/sqlite"
)

type appRegistryShim struct {
	caps  database.Capabilities
	build func(database.FormValues) (string, bool)
}

func (s appRegistryShim) Capabilities() database.Capabilities { return s.caps }
func (s appRegistryShim) BuildTarget(values database.FormValues) (string, bool) {
	return s.build(values)
}
func (s appRegistryShim) Open(ctx context.Context, target string) (sharedsql.Service, error) {
	return openTestSQLite(ctx, target)
}

func init() {
	sqlLanguage := sharedsql.SQLQueryLanguage
	shims := []database.Shim{
		appRegistryShim{
			caps: database.Capabilities{
				Name: "sqlite", Display: "SQLite", QueryLanguage: &sqlLanguage,
				Form: &database.FormSpec{Fields: []database.FormField{{Key: "target", Title: "Target*", Kind: database.FormInput, Placeholder: "path/to/database.db or :memory:", Validate: database.FormRequired, Error: "target is required"}}},
			},
			build: func(values database.FormValues) (string, bool) { return strings.TrimSpace(values.Database), true },
		},
		appRegistryShim{
			caps: database.Capabilities{
				Name: "mysql", Display: "MySQL", Targets: []database.TargetPattern{{Prefix: "mysql:"}}, QueryLanguage: &sqlLanguage,
				Form: &database.FormSpec{Prefix: "mysql:", Fields: []database.FormField{
					{Key: "host", Title: "Host", Kind: database.FormInput, Placeholder: "localhost"},
					{Key: "port", Title: "Port", Kind: database.FormInput, Default: "3306", Validate: database.FormPort},
					{Key: "username", Title: "Username*", Kind: database.FormInput, Validate: database.FormRequired},
					{Key: "password", Title: "Password", Kind: database.FormPassword},
					{Key: "database", Title: "Database", Kind: database.FormInput},
					{Key: "tls", Title: "TLS", Kind: database.FormSelect, Options: []database.FormOption{{Label: "Verify certificate", Value: "true"}, {Label: "Encrypt, don't verify certificate", Value: "skip-verify"}, {Label: "Don't encrypt", Value: "false"}}},
				}},
			},
			build: func(values database.FormValues) (string, bool) {
				config := mysql.NewConfig()
				config.User, config.Passwd, config.Net = strings.TrimSpace(values.User), values.Pass, "tcp"
				config.Addr, config.DBName, config.TLSConfig = net.JoinHostPort(values.Host, values.Port), strings.TrimSpace(values.Database), values.TLS
				if config.TLSConfig == "" {
					config.TLSConfig = "false"
				}
				return config.FormatDSN(), true
			},
		},
		appRegistryShim{
			caps: database.Capabilities{
				Name: "postgres", Display: "PostgreSQL", Targets: []database.TargetPattern{{Prefix: "postgres://", KeepTarget: true}, {Prefix: "postgresql://", KeepTarget: true}, {Prefix: "postgres:"}}, QueryLanguage: &sqlLanguage,
				Form: &database.FormSpec{Prefix: "postgres:", Fields: []database.FormField{
					{Key: "host", Title: "Host", Kind: database.FormInput, Placeholder: "localhost"},
					{Key: "port", Title: "Port", Kind: database.FormInput, Default: "5432", Validate: database.FormPort},
					{Key: "username", Title: "Username*", Kind: database.FormInput, Validate: database.FormRequired},
					{Key: "password", Title: "Password", Kind: database.FormPassword},
					{Key: "database", Title: "Database", Kind: database.FormInput},
					{Key: "tls", Title: "TLS", Kind: database.FormSelect, Options: []database.FormOption{{Label: "Verify certificate", Value: "verify-full"}, {Label: "Encrypt, don't verify certificate", Value: "require"}, {Label: "Don't encrypt", Value: "disable"}}},
				}},
			},
			build: func(values database.FormValues) (string, bool) {
				target := &url.URL{Scheme: "postgres", User: url.UserPassword(strings.TrimSpace(values.User), values.Pass), Host: net.JoinHostPort(values.Host, values.Port), Path: strings.TrimSpace(values.Database)}
				query := target.Query()
				query.Set("sslmode", values.TLS)
				if values.TLS == "" {
					query.Set("sslmode", "disable")
				}
				target.RawQuery = query.Encode()
				return target.String(), true
			},
		},
	}
	for _, shim := range shims {
		if err := database.RegisterShim(shim); err != nil {
			panic(err)
		}
	}
}

type testSQLiteService struct {
	db          *stdsql.DB
	foreignKeys map[string][]sharedsql.ForeignKeyInfo
	indexes     map[string][]sharedsql.IndexInfo
}

func openTestSQLite(ctx context.Context, target string) (*testSQLiteService, error) {
	dsn := target
	if target != ":memory:" {
		absolute, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		dsn = (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=rwc"}).String()
	}
	db, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if target == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &testSQLiteService{db: db, foreignKeys: make(map[string][]sharedsql.ForeignKeyInfo), indexes: make(map[string][]sharedsql.IndexInfo)}, nil
}

func openTestDatabase(ctx context.Context, _ string, target string) (sharedsql.Opened, error) {
	if target != ":memory:" {
		if _, err := filepath.EvalSymlinks(target); err != nil {
			return sharedsql.Opened{}, err
		}
	}
	service, err := openTestSQLite(ctx, target)
	if err != nil {
		return sharedsql.Opened{}, fmt.Errorf("opening database: %w", err)
	}
	objects, err := service.ListSchema(ctx)
	if err != nil {
		_ = service.Close()
		return sharedsql.Opened{}, err
	}
	return sharedsql.Opened{
		Target: target, Service: service, Info: service.Info(), Objects: objects,
		QueryLanguage: sharedsql.SQLQueryLanguage,
	}, nil
}

func (s *testSQLiteService) Close() error { return s.db.Close() }

func (s *testSQLiteService) Info() sharedsql.DatabaseInfo {
	return sharedsql.DatabaseInfo{Product: "SQLite", Version: "test"}
}

func (s *testSQLiteService) Execute(ctx context.Context, statement string) (sharedsql.Result, error) {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return sharedsql.Result{}, err
	}
	started := time.Now()
	first := strings.ToUpper(strings.Fields(statement)[0])
	if first != "SELECT" && first != "WITH" && first != "PRAGMA" && first != "EXPLAIN" {
		result, err := s.db.ExecContext(ctx, statement)
		if err != nil {
			return sharedsql.Result{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return sharedsql.Result{}, err
		}
		return sharedsql.Result{RowsAffected: affected, Duration: time.Since(started)}, nil
	}
	return s.query(ctx, statement, started)
}

func (s *testSQLiteService) ExecuteReadOnly(ctx context.Context, statement string) (sharedsql.Result, error) {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return sharedsql.Result{}, err
	}
	return s.query(ctx, statement, time.Now())
}

func (s *testSQLiteService) query(ctx context.Context, statement string, started time.Time, args ...any) (sharedsql.Result, error) {
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result, err := sharedsql.CollectRows(rows)
	if err != nil {
		return sharedsql.Result{}, err
	}
	result.Duration = time.Since(started)
	return result, nil
}

func (s *testSQLiteService) Validate(ctx context.Context, statement string) error {
	if err := sharedsql.ValidateStatement(statement); err != nil {
		return err
	}
	prepared, err := s.db.PrepareContext(ctx, statement)
	if err != nil {
		return err
	}
	return prepared.Close()
}

func (s *testSQLiteService) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'main', type, name
		FROM sqlite_schema
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	objects := []sharedsql.SchemaObject{{Database: "main", Type: "database", Name: "main"}}
	for rows.Next() {
		var object sharedsql.SchemaObject
		if err := rows.Scan(&object.Database, &object.Type, &object.Name); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

func (s *testSQLiteService) TableInfo(ctx context.Context, table string) ([]sharedsql.ColumnInfo, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_xinfo("`+strings.ReplaceAll(table, `"`, `""`)+`")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []sharedsql.ColumnInfo
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var column sharedsql.ColumnInfo
		var defaultValue stdsql.NullString
		if err := rows.Scan(&cid, &column.Name, &column.Type, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return nil, err
		}
		column.Nullable = notNull == 0
		column.PrimaryKey = primaryKey
		if defaultValue.Valid {
			value := defaultValue.String
			column.DefaultValue = &value
		}
		if primaryKey > 0 {
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexPrimaryKey}
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func (s *testSQLiteService) BrowseTable(ctx context.Context, table string, options sharedsql.BrowseOptions) (sharedsql.Result, error) {
	if options.Offset < 0 || options.Limit < 1 || options.Limit > sharedsql.MaxRows {
		return sharedsql.Result{}, errors.New("invalid browse range")
	}
	quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	statement := "SELECT * FROM " + quote(table)
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
			column := quote(filter.Column)
			switch filter.Operator {
			case sharedsql.BrowseFilterLike, sharedsql.BrowseFilterNotLike,
				sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual,
				sharedsql.BrowseFilterLess, sharedsql.BrowseFilterLessEqual,
				sharedsql.BrowseFilterGreater, sharedsql.BrowseFilterGreaterEqual:
				terms = append(terms, column+" "+string(filter.Operator)+" ?")
				args = append(args, filter.Value)
			case sharedsql.BrowseFilterPattern, sharedsql.BrowseFilterNotPattern:
				operator := "LIKE"
				if filter.Operator == sharedsql.BrowseFilterNotPattern {
					operator = "NOT LIKE"
				}
				terms = append(terms, column+" "+operator+" ? ESCAPE '\\'")
				args = append(args, sharedsql.GlobToLike(filter.Value))
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
			order := quote(sort.Column)
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
	result, err := s.query(ctx, statement+" LIMIT ? OFFSET ?", time.Now(), args...)
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

func (s *testSQLiteService) WriteCapabilities() sharedsql.WriteCapabilities {
	return sharedsql.WriteCapabilities{RowWriter: true}
}

func testRowValue(value sharedsql.Value) (any, error) {
	switch value.Kind {
	case sharedsql.ValueNull:
		return nil, nil
	case sharedsql.ValueString:
		return value.String, nil
	default:
		return nil, fmt.Errorf("unsupported row value kind %s", value.Kind)
	}
}

func (s *testSQLiteService) InsertRow(ctx context.Context, table string, values []sharedsql.RowValue) (sharedsql.Result, error) {
	columns := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, row := range values {
		if row.Value.Kind == sharedsql.ValueDefault {
			continue
		}
		value, err := testRowValue(row.Value)
		if err != nil {
			return sharedsql.Result{}, err
		}
		columns = append(columns, `"`+strings.ReplaceAll(row.Name, `"`, `""`)+`"`)
		args = append(args, value)
	}
	quote := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	statement := "INSERT INTO " + quote + " DEFAULT VALUES"
	if len(columns) > 0 {
		names := strings.Join(columns, ", ")
		statement = "INSERT INTO " + quote + " (" + names + ") VALUES (" + strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + ")"
	}
	result, err := s.db.ExecContext(ctx, statement, args...)
	if err != nil {
		return sharedsql.Result{}, err
	}
	affected, err := result.RowsAffected()
	return sharedsql.Result{RowsAffected: affected}, err
}

func (s *testSQLiteService) UpdateRow(ctx context.Context, table string, key []sharedsql.RowValue, values []sharedsql.RowValue) (sharedsql.Result, error) {
	if len(key) == 0 || len(values) == 0 {
		return sharedsql.Result{}, errors.New("row key and values are required")
	}
	quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	sets, args := make([]string, 0, len(values)), make([]any, 0, len(values)+len(key))
	for _, row := range values {
		value, err := testRowValue(row.Value)
		if err != nil {
			return sharedsql.Result{}, err
		}
		sets = append(sets, quote(row.Name)+" = ?")
		args = append(args, value)
	}
	where := make([]string, 0, len(key))
	for _, row := range key {
		if row.Value.Kind == sharedsql.ValueNull {
			where = append(where, quote(row.Name)+" IS NULL")
			continue
		}
		value, err := testRowValue(row.Value)
		if err != nil {
			return sharedsql.Result{}, err
		}
		where = append(where, quote(row.Name)+" = ?")
		args = append(args, value)
	}
	result, err := s.db.ExecContext(ctx, "UPDATE "+quote(table)+" SET "+strings.Join(sets, ", ")+" WHERE "+strings.Join(where, " AND "), args...)
	if err != nil {
		return sharedsql.Result{}, err
	}
	affected, err := result.RowsAffected()
	return sharedsql.Result{RowsAffected: affected}, err
}

func (s *testSQLiteService) DeleteRow(ctx context.Context, table string, key []sharedsql.RowValue) (sharedsql.Result, error) {
	if len(key) == 0 {
		return sharedsql.Result{}, errors.New("row key is required")
	}
	quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	where, args := make([]string, 0, len(key)), make([]any, 0, len(key))
	for _, row := range key {
		if row.Value.Kind == sharedsql.ValueNull {
			where = append(where, quote(row.Name)+" IS NULL")
			continue
		}
		value, err := testRowValue(row.Value)
		if err != nil {
			return sharedsql.Result{}, err
		}
		where = append(where, quote(row.Name)+" = ?")
		args = append(args, value)
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM "+quote(table)+" WHERE "+strings.Join(where, " AND "), args...)
	if err != nil {
		return sharedsql.Result{}, err
	}
	affected, err := result.RowsAffected()
	return sharedsql.Result{RowsAffected: affected}, err
}

func testQuote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func (s *testSQLiteService) ListIndexes(_ context.Context, table string) ([]sharedsql.IndexInfo, error) {
	return append([]sharedsql.IndexInfo(nil), s.indexes[table]...), nil
}

func (s *testSQLiteService) ListForeignKeys(_ context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	return append([]sharedsql.ForeignKeyInfo(nil), s.foreignKeys[table]...), nil
}

func (s *testSQLiteService) ListReferencingForeignKeys(context.Context, string) ([]sharedsql.ReferencingForeignKeyInfo, error) {
	return nil, nil
}

func (s *testSQLiteService) ListForeignKeysAll(context.Context) (map[string][]sharedsql.ForeignKeyInfo, error) {
	all := make(map[string][]sharedsql.ForeignKeyInfo, len(s.foreignKeys))
	for table, keys := range s.foreignKeys {
		all[table] = append([]sharedsql.ForeignKeyInfo(nil), keys...)
	}
	return all, nil
}

func (s *testSQLiteService) ListIndexesAll(ctx context.Context) (map[string][]sharedsql.IndexInfo, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			_ = rows.Close()
			return nil, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	all := make(map[string][]sharedsql.IndexInfo, len(tables))
	for _, table := range tables {
		indexes, err := s.ListIndexes(ctx, table)
		if err != nil {
			return nil, err
		}
		all[table] = indexes
	}
	return all, nil
}

func (s *testSQLiteService) CreateIndex(_ context.Context, table string, change sharedsql.IndexChange) error {
	name := change.Name
	if change.PrimaryKey {
		name = "PRIMARY"
	}
	s.indexes[table] = append(s.indexes[table], sharedsql.IndexInfo{Name: name, Unique: change.Unique, PrimaryKey: change.PrimaryKey, Columns: append([]string(nil), change.Columns...)})
	return nil
}

func (s *testSQLiteService) ReplaceIndex(ctx context.Context, table, previous string, change sharedsql.IndexChange) error {
	if err := s.DropIndex(ctx, table, previous); err != nil {
		return err
	}
	return s.CreateIndex(ctx, table, change)
}

func (s *testSQLiteService) DropIndex(_ context.Context, table, name string) error {
	indexes := s.indexes[table]
	for index := range indexes {
		if indexes[index].Name == name {
			s.indexes[table] = append(indexes[:index], indexes[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("index %q not found", name)
}

func (s *testSQLiteService) CreateForeignKey(_ context.Context, table string, change sharedsql.ForeignKeyChange) error {
	keys := s.foreignKeys[table]
	keys = append(keys, sharedsql.ForeignKeyInfo{ID: fmt.Sprintf("%d", len(keys)+1), Columns: append([]string(nil), change.Columns...), ReferenceTable: change.ReferenceTable, ReferenceColumns: append([]string(nil), change.ReferenceColumns...), OnDelete: change.OnDelete, OnUpdate: change.OnUpdate})
	s.foreignKeys[table] = keys
	return nil
}

func (s *testSQLiteService) ReplaceForeignKey(_ context.Context, table, id string, change sharedsql.ForeignKeyChange) error {
	keys := s.foreignKeys[table]
	for index := range keys {
		if keys[index].ID == id {
			keys[index].Columns = append([]string(nil), change.Columns...)
			keys[index].ReferenceTable = change.ReferenceTable
			keys[index].ReferenceColumns = append([]string(nil), change.ReferenceColumns...)
			keys[index].OnDelete, keys[index].OnUpdate = change.OnDelete, change.OnUpdate
			s.foreignKeys[table] = keys
			return nil
		}
	}
	return fmt.Errorf("foreign key %q not found", id)
}

func (s *testSQLiteService) DropForeignKey(_ context.Context, table, id string) error {
	keys := s.foreignKeys[table]
	for index := range keys {
		if keys[index].ID == id {
			s.foreignKeys[table] = append(keys[:index], keys[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("foreign key %q not found", id)
}

func (s *testSQLiteService) AlterColumn(ctx context.Context, table string, change sharedsql.ColumnChange) error {
	if change.Name == change.PreviousName {
		return nil
	}
	_, err := s.db.ExecContext(ctx, "ALTER TABLE "+testQuote(table)+" RENAME COLUMN "+testQuote(change.PreviousName)+" TO "+testQuote(change.Name))
	return err
}

func (s *testSQLiteService) DropColumn(ctx context.Context, table, name string) error {
	_, err := s.db.ExecContext(ctx, "ALTER TABLE "+testQuote(table)+" DROP COLUMN "+testQuote(name))
	return err
}

func (s *testSQLiteService) AddColumn(ctx context.Context, table string, column sharedsql.ColumnDef) error {
	statement := "ALTER TABLE " + testQuote(table) + " ADD COLUMN " + testQuote(column.Name) + " " + column.Type
	if !column.Nullable {
		statement += " NOT NULL"
	}
	_, err := s.db.ExecContext(ctx, statement)
	return err
}

var _ sharedsql.Service = (*testSQLiteService)(nil)
