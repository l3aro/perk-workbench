package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"regexp"
	"strings"
)

var inlinePrimaryKey = regexp.MustCompile(`(?i)\s+PRIMARY\s+KEY(?:\s+(?:ASC|DESC))?(?:\s+ON\s+CONFLICT\s+(?:ROLLBACK|ABORT|FAIL|IGNORE|REPLACE))?(?:\s+AUTOINCREMENT)?`)

func (s *Service) changePrimaryKey(ctx context.Context, table string, columns []string, requireExisting bool) error {
	if err := s.primaryKeyDependencies(ctx, table); err != nil {
		return err
	}
	current, err := s.primaryKeyColumns(ctx, table)
	if err != nil {
		return err
	}
	if requireExisting && len(current) == 0 {
		return fmt.Errorf("primary key was not found")
	}
	if !requireExisting && len(current) > 0 {
		return fmt.Errorf("table already has a primary key")
	}
	return s.rebuildTableWithSQL(ctx, table, func(*stdsql.Tx) error { return nil }, func(createSQL string) (string, error) {
		return rewritePrimaryKey(createSQL, "__perk_workbench_column_edit", columns)
	})
}

func (s *Service) primaryKeyColumns(ctx context.Context, table string) ([]string, error) {
	columns, err := s.TableInfo(ctx, table)
	if err != nil {
		return nil, err
	}
	primary := make([]string, 0, len(columns))
	for position := 1; position <= len(columns); position++ {
		for _, column := range columns {
			if column.PrimaryKey == position {
				primary = append(primary, column.Name)
				break
			}
		}
	}
	return primary, nil
}

func (s *Service) primaryKeyDependencies(ctx context.Context, table string) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT m.name
		FROM sqlite_schema AS m
		JOIN pragma_foreign_key_list(m.name) AS fk ON fk."table" = ?
		WHERE m.type = 'table' AND m.name <> ?`, table, table)
	if err != nil {
		return fmt.Errorf("listing foreign-key dependencies: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scanning foreign-key dependency: %w", err)
		}
		_ = rows.Close()
		return fmt.Errorf("primary key changes are unsupported while table %q is referenced by foreign key in %q", table, name)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterating foreign-key dependencies: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("closing foreign-key dependencies: %w", err)
	}
	return nil
}

func rewritePrimaryKey(createSQL, temporary string, columns []string) (string, error) {
	open, close, err := tableDefinitionBounds(createSQL)
	if err != nil {
		return "", err
	}
	definitions, err := splitDefinitions(createSQL[open+1 : close])
	if err != nil {
		return "", err
	}
	filtered := definitions[:0]
	for _, definition := range definitions {
		if tablePrimaryKeyDefinition(definition) {
			continue
		}
		filtered = append(filtered, inlinePrimaryKey.ReplaceAllString(definition, ""))
	}
	if len(columns) == 0 && strings.Contains(strings.ToUpper(createSQL[close+1:]), "WITHOUT ROWID") {
		return "", fmt.Errorf("cannot remove the primary key from a WITHOUT ROWID table")
	}
	if len(columns) > 0 {
		names := make([]string, len(columns))
		for index, column := range columns {
			names[index] = quoteIdentifier(strings.TrimSpace(column))
		}
		filtered = append(filtered, "PRIMARY KEY ("+strings.Join(names, ", ")+")")
	}
	return "CREATE TABLE " + quoteIdentifier(temporary) + " (" + strings.Join(filtered, ", ") + ")" + createSQL[close+1:], nil
}

func tablePrimaryKeyDefinition(definition string) bool {
	words := strings.Fields(strings.ToUpper(definition))
	if len(words) >= 2 && words[0] == "PRIMARY" && words[1] == "KEY" {
		return true
	}
	return len(words) >= 4 && words[0] == "CONSTRAINT" && words[2] == "PRIMARY" && words[3] == "KEY"
}
