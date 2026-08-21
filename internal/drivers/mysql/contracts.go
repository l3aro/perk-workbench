package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

const (
	maxRows  = 500
	maxRunes = 300
)

const (
	browseFilterLike         = "LIKE"
	browseFilterNotLike      = "NOT LIKE"
	browseFilterPattern      = "PATTERN"
	browseFilterNotPattern   = "NOT PATTERN"
	browseFilterEqual        = "="
	browseFilterNotEqual     = "!="
	browseFilterLess         = "<"
	browseFilterLessEqual    = "<="
	browseFilterGreater      = ">"
	browseFilterGreaterEqual = ">="
	browseFilterIsNull       = "IS NULL"
	browseFilterIsNotNull    = "IS NOT NULL"
)

func sanitizeDisplay(input string, limits ...int) string {
	limit := 0
	if len(limits) > 0 {
		limit = limits[0]
	}
	var display strings.Builder
	emitted := 0
	lastWasSpace := false
	truncated := false
	for index := 0; index < len(input); {
		runeValue, size := rune(input[index]), 1
		if runeValue >= utf8.RuneSelf {
			runeValue, size = utf8.DecodeRuneInString(input[index:])
		}
		if runeValue == '\x1b' {
			index += ansiSequenceLen(input[index:])
			continue
		}
		if runeValue == '\r' || runeValue == '\n' {
			if !lastWasSpace {
				display.WriteByte(' ')
				emitted++
				lastWasSpace = true
				if limit > 0 && emitted >= limit {
					truncated = true
					break
				}
			}
		} else if !unicode.IsControl(runeValue) {
			display.WriteRune(runeValue)
			emitted++
			lastWasSpace = runeValue == ' '
			if limit > 0 && emitted >= limit {
				truncated = true
				break
			}
		}
		index += size
	}
	if truncated {
		display.WriteString("…")
	}
	return display.String()
}

func ansiSequenceLen(input string) int {
	if len(input) == 1 {
		return 1
	}
	switch input[1] {
	case '[':
		for index := 2; index < len(input); index++ {
			if input[index] >= 0x40 && input[index] <= 0x7e {
				return index + 1
			}
		}
	case ']', 'P', '^', '_':
		for index := 2; index < len(input); index++ {
			if input[index] == '\a' {
				return index + 1
			}
			if input[index] == '\x1b' && index+1 < len(input) && input[index+1] == '\\' {
				return index + 2
			}
		}
		return len(input)
	default:
		return 2
	}
	return len(input)
}

func collectRow(values []any) (display, raw []*string) {
	display = make([]*string, len(values))
	raw = make([]*string, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		text := fmt.Sprint(value)
		if bytes, ok := value.([]byte); ok {
			text = string(bytes)
		}
		raw[index] = &text
		sanitized := sanitizeDisplay(text, maxRunes)
		display[index] = &sanitized
	}
	return display, raw
}

func collectRows(rows *sql.Rows) (driver.Result, error) {
	columns, err := rows.Columns()
	if err != nil {
		return driver.Result{}, closeRows(rows, "reading result columns", err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return driver.Result{}, closeRows(rows, "reading result column types", err)
	}
	result := driver.Result{Columns: make([]string, len(columns)), ColumnTypes: make([]string, len(columnTypes)), Rows: [][]*string{}}
	for index, column := range columns {
		result.Columns[index] = sanitizeDisplay(column)
	}
	for index, columnType := range columnTypes {
		result.ColumnTypes[index] = columnType.DatabaseTypeName()
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	for rows.Next() {
		if len(result.Rows) == maxRows {
			result.Truncated = true
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			return driver.Result{}, closeRows(rows, "scanning result row", err)
		}
		display, raw := collectRow(values)
		result.Rows = append(result.Rows, display)
		result.UntruncatedRows = append(result.UntruncatedRows, raw)
	}
	if err := rows.Err(); err != nil {
		return driver.Result{}, closeRows(rows, "iterating result rows", err)
	}
	if err := rows.Close(); err != nil {
		return driver.Result{}, fmt.Errorf("closing result rows: %w", err)
	}
	return result, nil
}

func closeRows(rows *sql.Rows, action string, err error) error {
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("%s: %w", action, errors.Join(err, closeErr))
	}
	return fmt.Errorf("%s: %w", action, err)
}

func validateStatement(input string) error {
	const (
		normal = iota
		lineComment
		blockComment
		singleQuote
		doubleQuote
		backtick
		bracket
	)
	state := normal
	seenToken, trailingSemicolon := false, false
	triggerState := 0
	var token strings.Builder
	consumeToken := func() error {
		if token.Len() == 0 {
			return nil
		}
		word := strings.ToUpper(token.String())
		token.Reset()
		switch triggerState {
		case 0:
			if word == "CREATE" {
				triggerState = 1
			}
		case 1:
			switch word {
			case "TRIGGER":
				return errors.New("trigger statements are not supported")
			case "TEMP", "TEMPORARY":
				triggerState = 2
			case "OR":
				triggerState = 4
			case "IF":
				triggerState = 5
			default:
				triggerState = 3
			}
		case 2:
			if word == "TRIGGER" {
				return errors.New("trigger statements are not supported")
			}
			triggerState = 3
		case 4:
			if word == "REPLACE" {
				triggerState = 1
			} else {
				triggerState = 3
			}
		case 5:
			if word == "NOT" {
				triggerState = 6
			} else {
				triggerState = 3
			}
		case 6:
			if word == "EXISTS" {
				triggerState = 1
			} else {
				triggerState = 3
			}
		}
		return nil
	}
	runes := []rune(input)
	for index := 0; index < len(runes); index++ {
		current := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		switch state {
		case lineComment:
			if current == '\n' || current == '\r' {
				state = normal
			}
			continue
		case blockComment:
			if current == '*' && next == '/' {
				state = normal
				index++
			}
			continue
		case singleQuote:
			if current == '\'' {
				if next == '\'' {
					index++
				} else {
					state = normal
				}
			}
			continue
		case doubleQuote:
			if current == '"' {
				if next == '"' {
					index++
				} else {
					state = normal
				}
			}
			continue
		case backtick:
			if current == '`' {
				if next == '`' {
					index++
				} else {
					state = normal
				}
			}
			continue
		case bracket:
			if current == ']' {
				state = normal
			}
			continue
		}
		if unicode.IsSpace(current) {
			if err := consumeToken(); err != nil {
				return err
			}
			continue
		}
		if current == '-' && next == '-' {
			if err := consumeToken(); err != nil {
				return err
			}
			state, index = lineComment, index+1
			continue
		}
		if current == '/' && next == '*' {
			if err := consumeToken(); err != nil {
				return err
			}
			state, index = blockComment, index+1
			continue
		}
		if current == ';' {
			if err := consumeToken(); err != nil {
				return err
			}
			if !seenToken || trailingSemicolon {
				return errors.New("only one statement is allowed")
			}
			trailingSemicolon = true
			continue
		}
		if trailingSemicolon {
			return errors.New("only one statement is allowed")
		}
		seenToken = true
		switch current {
		case '\'':
			if err := consumeToken(); err != nil {
				return err
			}
			state = singleQuote
		case '"':
			if err := consumeToken(); err != nil {
				return err
			}
			state = doubleQuote
		case '`':
			if err := consumeToken(); err != nil {
				return err
			}
			state = backtick
		case '[':
			if err := consumeToken(); err != nil {
				return err
			}
			state = bracket
		default:
			if unicode.IsLetter(current) || unicode.IsDigit(current) || current == '_' {
				token.WriteRune(current)
			} else if err := consumeToken(); err != nil {
				return err
			}
		}
	}
	if err := consumeToken(); err != nil {
		return err
	}
	if !seenToken {
		return errors.New("statement is empty")
	}
	return nil
}

func validateColumnChange(change driver.ColumnChange) error {
	if strings.TrimSpace(change.PreviousName) == "" || strings.TrimSpace(change.Name) == "" {
		return errors.New("column name is required")
	}
	if strings.TrimSpace(change.Type) == "" {
		return errors.New("column type is required")
	}
	for _, value := range []string{change.PreviousName, change.Name} {
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return errors.New("column name contains control characters")
		}
	}
	for _, value := range change.Type {
		if !(unicode.IsLetter(value) || unicode.IsDigit(value) || unicode.IsSpace(value) || strings.ContainsRune("_(),'", value)) {
			return errors.New("column type contains unsupported characters")
		}
	}
	if change.DefaultValue != nil && (strings.Contains(*change.DefaultValue, ";") || strings.IndexFunc(*change.DefaultValue, unicode.IsControl) >= 0) {
		return errors.New("column default contains unsupported characters")
	}
	if change.Attributes != nil && (strings.IndexFunc(*change.Attributes, unicode.IsControl) >= 0 || strings.Contains(*change.Attributes, ";")) {
		return errors.New("column attributes contain unsupported characters")
	}
	return nil
}

func validateColumnDef(col driver.ColumnDef) error {
	if strings.TrimSpace(col.Name) == "" {
		return errors.New("column name is required")
	}
	if strings.TrimSpace(col.Type) == "" {
		return errors.New("column type is required")
	}
	if strings.IndexFunc(col.Name, unicode.IsControl) >= 0 {
		return errors.New("column name contains control characters")
	}
	for _, value := range col.Type {
		if !(unicode.IsLetter(value) || unicode.IsDigit(value) || unicode.IsSpace(value) || strings.ContainsRune("_(),'", value)) {
			return errors.New("column type contains unsupported characters")
		}
	}
	if col.DefaultValue != nil && (strings.Contains(*col.DefaultValue, ";") || strings.IndexFunc(*col.DefaultValue, unicode.IsControl) >= 0) {
		return errors.New("column default contains unsupported characters")
	}
	if col.Attributes != nil && (strings.IndexFunc(*col.Attributes, unicode.IsControl) >= 0 || strings.Contains(*col.Attributes, ";")) {
		return errors.New("column attributes contain unsupported characters")
	}
	return nil
}

func validateIndexChange(change driver.IndexChange) error {
	if !change.PrimaryKey && strings.TrimSpace(change.Name) == "" {
		return fmt.Errorf("index name is required")
	}
	if change.PrimaryKey && change.Unique {
		return fmt.Errorf("primary keys cannot also be unique indexes")
	}
	if len(change.Columns) == 0 {
		return fmt.Errorf("index columns are required")
	}
	seen := make(map[string]struct{}, len(change.Columns))
	for _, column := range change.Columns {
		name := strings.TrimSpace(column)
		if name == "" {
			return fmt.Errorf("index columns cannot be empty")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("index column %q is repeated", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateForeignKeyChange(change driver.ForeignKeyChange) error {
	if len(change.Columns) == 0 {
		return fmt.Errorf("foreign-key columns are required")
	}
	if strings.TrimSpace(change.ReferenceTable) == "" {
		return fmt.Errorf("referenced table is required")
	}
	if len(change.Columns) != len(change.ReferenceColumns) {
		return fmt.Errorf("foreign-key and referenced column counts must match")
	}
	seen := make(map[string]struct{}, len(change.Columns))
	for _, column := range append(append([]string{}, change.Columns...), change.ReferenceColumns...) {
		if strings.TrimSpace(column) == "" {
			return fmt.Errorf("foreign-key columns cannot be empty")
		}
	}
	for _, column := range change.Columns {
		name := strings.TrimSpace(column)
		if _, exists := seen[name]; exists {
			return fmt.Errorf("foreign-key column %q is repeated", name)
		}
		seen[name] = struct{}{}
	}
	for _, action := range []string{change.OnDelete, change.OnUpdate} {
		switch strings.ToUpper(strings.TrimSpace(action)) {
		case "NO ACTION", "RESTRICT", "SET NULL", "SET DEFAULT", "CASCADE":
		default:
			return fmt.Errorf("invalid foreign-key action %q", action)
		}
	}
	return nil
}

func globToLike(pattern string) string {
	var builder strings.Builder
	builder.Grow(len(pattern))
	for _, r := range pattern {
		switch r {
		case '*':
			builder.WriteByte('%')
		case '?':
			builder.WriteByte('_')
		case '\\', '%', '_':
			builder.WriteByte('\\')
			builder.WriteRune(r)
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
