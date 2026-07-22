package sqlite

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	sharedsql "github.com/l3aro/perk/internal/sql"
)

type missingReferenceColumn struct {
	foreignKey, column, sequence int
	referencedTable              string
}

func (s *Service) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("reading foreign keys: %w", err)
	}
	foreignKeys := []sharedsql.ForeignKeyInfo{}
	positions := map[int]int{}
	missingColumns := []missingReferenceColumn{}
	for rows.Next() {
		var id, sequence int
		var referencedTable, from, onUpdate, onDelete, match string
		var to stdsql.NullString
		if err := rows.Scan(&id, &sequence, &referencedTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning foreign keys", err)
		}
		position, exists := positions[id]
		if !exists {
			position = len(foreignKeys)
			positions[id] = position
			foreignKeys = append(foreignKeys, sharedsql.ForeignKeyInfo{ID: strconv.Itoa(id), ReferenceTable: sharedsql.SanitizeDisplay(referencedTable), OnDelete: sharedsql.SanitizeDisplay(onDelete), OnUpdate: sharedsql.SanitizeDisplay(onUpdate)})
		}
		foreignKeys[position].Columns = append(foreignKeys[position].Columns, sharedsql.SanitizeDisplay(from))
		foreignKeys[position].ReferenceColumns = append(foreignKeys[position].ReferenceColumns, sharedsql.SanitizeDisplay(to.String))
		if !to.Valid {
			missingColumns = append(missingColumns, missingReferenceColumn{foreignKey: position, column: len(foreignKeys[position].ReferenceColumns) - 1, sequence: sequence, referencedTable: referencedTable})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing foreign-key rows: %w", err)
	}
	if err := s.resolveMissingReferenceColumns(ctx, foreignKeys, missingColumns); err != nil {
		return nil, err
	}
	return foreignKeys, nil
}

func (s *Service) ListReferencingForeignKeys(ctx context.Context, table string) ([]sharedsql.ReferencingForeignKeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.name, f.id, f.seq, f."table", f."from", f."to", f.on_update, f.on_delete
		FROM sqlite_schema AS m
		JOIN pragma_foreign_key_list(m.name) AS f
		WHERE m.type = 'table' AND lower(f."table") = lower(?)
		ORDER BY m.name, f.id, f.seq`, table)
	if err != nil {
		return nil, fmt.Errorf("reading referencing foreign keys: %w", err)
	}
	references := []sharedsql.ReferencingForeignKeyInfo{}
	positions := map[string]int{}
	missingColumns := []missingReferenceColumn{}
	for rows.Next() {
		var sourceTable, referencedTable, from, onUpdate, onDelete string
		var id, sequence int
		var to stdsql.NullString
		if err := rows.Scan(&sourceTable, &id, &sequence, &referencedTable, &from, &to, &onUpdate, &onDelete); err != nil {
			return nil, sharedsql.CloseRows(rows, "scanning referencing foreign keys", err)
		}
		key := sourceTable + "\x00" + strconv.Itoa(id)
		position, exists := positions[key]
		if !exists {
			position = len(references)
			positions[key] = position
			references = append(references, sharedsql.ReferencingForeignKeyInfo{Table: sharedsql.SanitizeDisplay(sourceTable), ForeignKeyInfo: sharedsql.ForeignKeyInfo{ID: strconv.Itoa(id), ReferenceTable: sharedsql.SanitizeDisplay(referencedTable), OnDelete: sharedsql.SanitizeDisplay(onDelete), OnUpdate: sharedsql.SanitizeDisplay(onUpdate)}})
		}
		references[position].Columns = append(references[position].Columns, sharedsql.SanitizeDisplay(from))
		references[position].ReferenceColumns = append(references[position].ReferenceColumns, sharedsql.SanitizeDisplay(to.String))
		if !to.Valid {
			missingColumns = append(missingColumns, missingReferenceColumn{foreignKey: position, column: len(references[position].ReferenceColumns) - 1, sequence: sequence, referencedTable: referencedTable})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, sharedsql.CloseRows(rows, "iterating referencing foreign keys", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing referencing-foreign-key rows: %w", err)
	}
	foreignKeys := make([]sharedsql.ForeignKeyInfo, len(references))
	for index := range references {
		foreignKeys[index] = references[index].ForeignKeyInfo
	}
	if err := s.resolveMissingReferenceColumns(ctx, foreignKeys, missingColumns); err != nil {
		return nil, err
	}
	for index := range references {
		references[index].ForeignKeyInfo = foreignKeys[index]
	}
	return references, nil
}

func (s *Service) resolveMissingReferenceColumns(ctx context.Context, foreignKeys []sharedsql.ForeignKeyInfo, missingColumns []missingReferenceColumn) error {
	primaryKeys := map[string][]string{}
	for _, missing := range missingColumns {
		primary, exists := primaryKeys[missing.referencedTable]
		if !exists {
			var err error
			primary, err = s.primaryKeyColumns(ctx, missing.referencedTable)
			if err != nil {
				return fmt.Errorf("reading referenced primary key: %w", err)
			}
			primaryKeys[missing.referencedTable] = primary
		}
		if missing.sequence >= len(primary) {
			return fmt.Errorf("referenced primary key for %q has no column at position %d", missing.referencedTable, missing.sequence)
		}
		foreignKeys[missing.foreignKey].ReferenceColumns[missing.column] = sharedsql.SanitizeDisplay(primary[missing.sequence])
	}
	return nil
}

func (s *Service) CreateForeignKey(ctx context.Context, table string, change sharedsql.ForeignKeyChange) error {
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return err
	}
	foreignKeys, err := s.ListForeignKeys(ctx, table)
	if err != nil {
		return err
	}
	foreignKeys = append(foreignKeys, foreignKeyInfo(change))
	return s.replaceForeignKeys(ctx, table, foreignKeys)
}

func (s *Service) ReplaceForeignKey(ctx context.Context, table, previous string, change sharedsql.ForeignKeyChange) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("foreign key is required")
	}
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return err
	}
	foreignKeys, err := s.ListForeignKeys(ctx, table)
	if err != nil {
		return err
	}
	for index := range foreignKeys {
		if foreignKeys[index].ID == previous {
			foreignKeys[index] = foreignKeyInfo(change)
			return s.replaceForeignKeys(ctx, table, foreignKeys)
		}
	}
	return fmt.Errorf("foreign key %q was not found", previous)
}

func (s *Service) DropForeignKey(ctx context.Context, table, previous string) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("foreign key is required")
	}
	foreignKeys, err := s.ListForeignKeys(ctx, table)
	if err != nil {
		return err
	}
	filtered := foreignKeys[:0]
	found := false
	for _, foreignKey := range foreignKeys {
		if foreignKey.ID == previous {
			found = true
			continue
		}
		filtered = append(filtered, foreignKey)
	}
	if !found {
		return fmt.Errorf("foreign key %q was not found", previous)
	}
	return s.replaceForeignKeys(ctx, table, filtered)
}

func (s *Service) replaceForeignKeys(ctx context.Context, table string, foreignKeys []sharedsql.ForeignKeyInfo) error {
	return s.rebuildTableWithSQL(ctx, table, func(*stdsql.Tx) error { return nil }, func(createSQL string) (string, error) {
		return rewriteForeignKeys(createSQL, "__perk_column_edit", foreignKeys)
	})
}

func foreignKeyInfo(change sharedsql.ForeignKeyChange) sharedsql.ForeignKeyInfo {
	return sharedsql.ForeignKeyInfo{Columns: change.Columns, ReferenceTable: strings.TrimSpace(change.ReferenceTable), ReferenceColumns: change.ReferenceColumns, OnDelete: strings.ToUpper(strings.TrimSpace(change.OnDelete)), OnUpdate: strings.ToUpper(strings.TrimSpace(change.OnUpdate))}
}

func rewriteForeignKeys(createSQL, temporary string, foreignKeys []sharedsql.ForeignKeyInfo) (string, error) {
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
		if tableForeignKeyDefinition(definition) {
			continue
		}
		if start := inlineForeignKeyStart(definition); start >= 0 {
			definition = strings.TrimSpace(definition[:start])
			if definition == "" {
				return "", fmt.Errorf("foreign key definition cannot be safely rewritten")
			}
		}
		filtered = append(filtered, definition)
	}
	for _, foreignKey := range foreignKeys {
		filtered = append(filtered, foreignKeyDefinition(foreignKey))
	}
	return "CREATE TABLE " + quoteIdentifier(temporary) + " (" + strings.Join(filtered, ", ") + ")" + createSQL[close+1:], nil
}

func foreignKeyDefinition(foreignKey sharedsql.ForeignKeyInfo) string {
	columns := make([]string, len(foreignKey.Columns))
	references := make([]string, len(foreignKey.ReferenceColumns))
	for index := range foreignKey.Columns {
		columns[index] = quoteIdentifier(strings.TrimSpace(foreignKey.Columns[index]))
		references[index] = quoteIdentifier(strings.TrimSpace(foreignKey.ReferenceColumns[index]))
	}
	return "FOREIGN KEY (" + strings.Join(columns, ", ") + ") REFERENCES " + quoteIdentifier(foreignKey.ReferenceTable) + " (" + strings.Join(references, ", ") + ") ON DELETE " + foreignKey.OnDelete + " ON UPDATE " + foreignKey.OnUpdate
}

func tableForeignKeyDefinition(definition string) bool {
	words := topLevelWords(definition)
	if len(words) >= 2 && words[0].text == "FOREIGN" && words[1].text == "KEY" {
		return true
	}
	return len(words) >= 4 && words[0].text == "CONSTRAINT" && words[2].text == "FOREIGN" && words[3].text == "KEY"
}

func inlineForeignKeyStart(definition string) int {
	words := topLevelWords(definition)
	for index, word := range words {
		if word.text != "REFERENCES" {
			continue
		}
		if index >= 2 && words[index-2].text == "CONSTRAINT" {
			return words[index-2].start
		}
		return word.start
	}
	return -1
}

type sqlWord struct {
	text  string
	start int
}

func topLevelWords(input string) []sqlWord {
	words := []sqlWord{}
	quote, depth, start := rune(0), 0, -1
	for index, character := range input {
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"', '`':
			quote = character
		case '[':
			quote = ']'
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && unicode.IsLetter(character) {
				if start < 0 {
					start = index
				}
			} else if start >= 0 {
				words = append(words, sqlWord{text: strings.ToUpper(input[start:index]), start: start})
				start = -1
			}
		}
	}
	if start >= 0 {
		words = append(words, sqlWord{text: strings.ToUpper(input[start:]), start: start})
	}
	return words
}
