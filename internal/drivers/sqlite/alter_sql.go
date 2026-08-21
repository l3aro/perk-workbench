package sqlite

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	plugindriver "github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func rewriteCreateTable(createSQL, temporary string, change plugindriver.ColumnChange) (string, error) {
	open, close, err := tableDefinitionBounds(createSQL)
	if err != nil {
		return "", err
	}
	definitions, err := splitDefinitions(createSQL[open+1 : close])
	if err != nil {
		return "", err
	}
	replaced := false
	for index, definition := range definitions {
		name, remainder := definitionName(definition)
		if name != change.PreviousName {
			continue
		}
		if unsupportedColumnConstraint(remainder) {
			return "", fmt.Errorf("column %q has constraints that cannot be safely rewritten", name)
		}
		definition := quoteIdentifier(change.Name) + " " + strings.TrimSpace(change.Type)
		if !change.Nullable {
			definition += " NOT NULL"
		}
		if change.DefaultValue != nil {
			definition += " DEFAULT " + *change.DefaultValue
		}
		definitions[index] = definition
		replaced = true
		break
	}
	if !replaced {
		return "", fmt.Errorf("column %q was not found in the table definition", change.PreviousName)
	}
	return "CREATE TABLE " + quoteIdentifier(temporary) + " (" + strings.Join(definitions, ", ") + ")" + createSQL[close+1:], nil
}

func tableDefinitionBounds(statement string) (int, int, error) {
	open := strings.IndexByte(statement, '(')
	if open < 0 {
		return 0, 0, errors.New("table definition is missing an opening parenthesis")
	}
	depth := 0
	quote := byte(0)
	for index := open; index < len(statement); index++ {
		current := statement[index]
		if quote != 0 {
			if current == quote {
				if quote != ']' && index+1 < len(statement) && statement[index+1] == quote {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"', '`':
			quote = current
		case '[':
			quote = ']'
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return open, index, nil
			}
		}
	}
	return 0, 0, errors.New("table definition is missing a closing parenthesis")
}

func splitDefinitions(input string) ([]string, error) {
	definitions := []string{}
	start, depth := 0, 0
	quote := byte(0)
	for index := 0; index < len(input); index++ {
		current := input[index]
		if quote != 0 {
			if current == quote {
				if quote != ']' && index+1 < len(input) && input[index+1] == quote {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"', '`':
			quote = current
		case '[':
			quote = ']'
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return nil, errors.New("unbalanced column definition")
			}
			depth--
		case ',':
			if depth == 0 {
				definitions = append(definitions, strings.TrimSpace(input[start:index]))
				start = index + 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, errors.New("unbalanced table definition")
	}
	definitions = append(definitions, strings.TrimSpace(input[start:]))
	return definitions, nil
}

func definitionName(definition string) (string, string) {
	definition = strings.TrimSpace(definition)
	if definition == "" {
		return "", ""
	}
	switch definition[0] {
	case '"', '`', '[':
		end := definition[0]
		if end == '[' {
			end = ']'
		}
		for index := 1; index < len(definition); index++ {
			if definition[index] == end {
				if end != ']' && index+1 < len(definition) && definition[index+1] == end {
					index++
					continue
				}
				return strings.ReplaceAll(definition[1:index], string([]byte{end, end}), string(end)), definition[index+1:]
			}
		}
		return "", definition
	default:
		for index, current := range definition {
			if current == ' ' || current == '\t' || current == '\n' || current == '\r' {
				return definition[:index], definition[index:]
			}
		}
		return definition, ""
	}
}

func unsupportedColumnConstraint(definition string) bool {
	for _, word := range strings.FieldsFunc(strings.ToUpper(definition), func(character rune) bool { return !unicode.IsLetter(character) }) {
		switch word {
		case "PRIMARY", "KEY", "UNIQUE", "CHECK", "REFERENCES", "COLLATE", "GENERATED", "CONSTRAINT", "ON", "CONFLICT":
			return true
		}
	}
	return false
}

func rewriteDropColumn(createSQL, temporary, name string) (string, error) {
	open, close, err := tableDefinitionBounds(createSQL)
	if err != nil {
		return "", err
	}
	definitions, err := splitDefinitions(createSQL[open+1 : close])
	if err != nil {
		return "", err
	}
	filtered := make([]string, 0, len(definitions))
	removed := false
	for _, definition := range definitions {
		defName, _ := definitionName(definition)
		if strings.EqualFold(defName, name) {
			removed = true
			continue
		}
		filtered = append(filtered, definition)
	}
	if !removed {
		return "", fmt.Errorf("column %q was not found in the table definition", name)
	}
	return "CREATE TABLE " + quoteIdentifier(temporary) + " (" + strings.Join(filtered, ", ") + ")" + createSQL[close+1:], nil
}
