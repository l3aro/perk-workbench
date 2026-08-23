package sql

import "strings"

var Keywords = []string{
	"ALTER", "AND", "AS", "ASC", "BETWEEN", "BY", "CASE", "CREATE", "DELETE", "DESC", "DISTINCT", "DROP",
	"FROM", "GROUP", "HAVING", "INSERT", "INTO", "JOIN", "LEFT", "LIMIT", "NOT", "NULL", "ON", "OR",
	"ORDER", "RIGHT", "SELECT", "SET", "UPDATE", "VALUES", "WHERE", "WITH",
}

// CompletionPrefix returns the word (identifier) at the end of the SQL value.
// This is used for real-time prefix filtering as the user types.
func CompletionPrefix(value string) string {
	for index := len(value); index > 0; index-- {
		character := value[index-1]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '.' {
			return value[index:]
		}
	}
	return value
}

// CompletionContext classifies the cursor position for context-aware suggestions.
type CompletionContext int

const (
	CtxGeneric    CompletionContext = iota // fallback — suggest everything
	CtxTable                               // after FROM/JOIN/INTO — suggest tables, views, schemas, keywords
	CtxExpression                          // after SELECT/WHERE/ON — suggest columns, functions, keywords
	CtxQualified                           // schema. or table. prefix — suggest objects or columns
)

// SQLAnalysis holds the result of analyzing cursor position within SQL text.
type SQLAnalysis struct {
	Context          CompletionContext
	Prefix           string
	Qualifier        string            // for CtxQualified: schema or table name before the dot
	Aliases          map[string]string // alias → table name (lowercase)
	ReferencedTables []string          // table names mentioned in the query
	Words            []string          // uppercased tokens of the whole value, shared with callers
}

// tableContextWords are keywords after which we expect a table/object name.
var tableContextWords = []string{"FROM", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS",
	"INTO", "TABLE", "UPDATE", "INDEX", "NATURAL", "OUTER"}

// isTableContextWord checks if word (uppercased) is a table-context keyword.
func isTableContextWord(word string) bool {
	for _, kw := range tableContextWords {
		if word == kw {
			return true
		}
	}
	return false
}

// AnalyzeSQL analyzes the SQL text and cursor column position to determine
// the completion context.
//
// row and col are 0-indexed cursor position within the text.
func AnalyzeSQL(value string, row, col int) SQLAnalysis {
	lines := strings.Split(value, "\n")
	if row >= len(lines) {
		row = len(lines) - 1
	}
	if row < 0 {
		row = 0
	}
	line := lines[row]
	if col > len(line) {
		col = len(line)
	}
	if col < 0 {
		col = 0
	}

	cursor := col
	for index := range row {
		cursor += len(lines[index]) + 1
	}
	words, beforeCursor := tokenizeUpperAt(value, cursor)

	// Get the identifier being typed (may include a dot for qualified names).
	prefix, _ := completionPrefixAt(line, col)

	// Detect qualifier from a dot inside the prefix (e.g. "office." or "office.o").
	qualifier := ""
	if dotInPrefix := strings.LastIndex(prefix, "."); dotInPrefix >= 0 {
		qualifier = prefix[:dotInPrefix]
		prefix = prefix[dotInPrefix+1:]
	}

	referenced, aliases := extractTableReferencesTokens(words)
	if qualifier != "" {
		return SQLAnalysis{
			Context:          CtxQualified,
			Prefix:           prefix,
			Qualifier:        qualifier,
			Aliases:          aliases,
			ReferencedTables: referenced,
			Words:            words,
		}
	}

	return SQLAnalysis{
		Context:          classifyContextTokens(beforeCursor),
		Prefix:           prefix,
		Aliases:          aliases,
		ReferencedTables: referenced,
		Words:            words,
	}
}

// completionPrefixAt extracts the identifier (or dotted identifier) at the cursor.
// Returns the prefix and its start column.
func completionPrefixAt(line string, col int) (string, int) {
	if col > len(line) {
		col = len(line)
	}
	start := col
	for start > 0 && isIdentChar(rune(line[start-1])) {
		start--
	}
	if start < col {
		return line[start:col], start
	}
	return "", col
}

// classifyContextTokens determines whether the cursor is in table or expression
// context by scanning the tokens before the cursor for the last significant
// keyword.
func classifyContextTokens(words []string) CompletionContext {
	for i := len(words) - 1; i >= 0; i-- {
		word := words[i]
		switch word {
		case "FROM", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "NATURAL", "OUTER":
			return CtxTable
		case "INTO", "TABLE", "UPDATE":
			return CtxTable
		case "SELECT", "WHERE", "SET", "ON", "HAVING":
			return CtxExpression
		case "AND", "OR", "NOT":
			// Continue searching — these might be in either context, but
			// the clause they belong to determines the context.
			continue
		case "ORDER", "GROUP":
			// Check if followed by BY.
			if i+1 < len(words) && words[i+1] == "BY" {
				return CtxExpression
			}
			continue
		case "BETWEEN", "LIKE", "IN", "IS", "AS", "DISTINCT":
			return CtxExpression
		case "BY":
			// Could be ORDER BY, GROUP BY — check preceding word.
			if i > 0 && (words[i-1] == "ORDER" || words[i-1] == "GROUP") {
				return CtxExpression
			}
		}
	}
	return CtxExpression // default for SELECT/column context
}

// tokenizeUpperAt splits text into uppercased words while also returning the
// token view ending at cursor. Both views are produced by one scan.
func tokenizeUpperAt(text string, cursor int) (words, beforeCursor []string) {
	var current strings.Builder
	var before strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		words = append(words, strings.ToUpper(current.String()))
		if before.Len() > 0 {
			beforeCursor = append(beforeCursor, strings.ToUpper(before.String()))
		}
		current.Reset()
		before.Reset()
	}
	for offset, r := range text {
		if isIdentChar(r) {
			current.WriteRune(r)
			if cursor >= 0 && offset < cursor {
				before.WriteRune(r)
			}
			continue
		}
		flush()
	}
	flush()
	return words, beforeCursor
}

// tokenizeUpper splits text into uppercased words, handling non-identifier chars.
func tokenizeUpper(text string) []string {
	words, _ := tokenizeUpperAt(text, -1)
	return words
}

func isIdentChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '.'
}

// ExtractBufferWords extracts identifiers (>=2 chars) from text that aren't SQL keywords.
func ExtractBufferWords(text string) []string {
	return ExtractBufferWordsTokens(tokenizeUpper(text))
}

// ExtractBufferWordsTokens is ExtractBufferWords over pre-tokenized text, so
// callers that already tokenized the buffer (AnalyzeSQL) avoid a second pass.
func ExtractBufferWordsTokens(words []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, w := range words {
		if len(w) < 2 {
			continue
		}
		if isKeyword(w) {
			continue
		}
		lower := strings.ToLower(w)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, lower)
		}
	}
	return result
}

func isKeyword(word string) bool {
	for _, kw := range Keywords {
		if word == kw {
			return true
		}
	}
	return false
}

// extractTableReferencesTokens finds table names and aliases in pre-tokenized
// SQL text. Uses a simple heuristic: words that are NOT keywords and appear
// after FROM/JOIN context words are potential table references.
func extractTableReferencesTokens(words []string) (tables []string, aliases map[string]string) {
	aliases = make(map[string]string)
	seen := make(map[string]bool)

	for i, word := range words {
		if !isTableContextWord(word) {
			continue
		}
		// Next non-keyword word is a table reference.
		for j := i + 1; j < len(words); j++ {
			next := words[j]
			if isKeyword(next) || next == "AS" {
				if next == "AS" {
					// Table AS alias
					if j+1 < len(words) && !isKeyword(words[j+1]) {
						aliases[strings.ToLower(words[j+1])] = strings.ToLower(words[i+1])
					}
				}
				break
			}
			if !seen[strings.ToLower(next)] {
				seen[strings.ToLower(next)] = true
				tables = append(tables, strings.ToLower(next))
			}
			// Check if followed by AS alias
			if j+1 < len(words) && words[j+1] == "AS" && j+2 < len(words) && !isKeyword(words[j+2]) {
				aliases[strings.ToLower(words[j+2])] = strings.ToLower(next)
				break
			}
			// Check for implicit alias (table word then another non-keyword)
			if j+1 < len(words) && !isKeyword(words[j+1]) && !isTableContextWord(words[j+1]) {
				aliases[strings.ToLower(words[j+1])] = strings.ToLower(next)
			}
			break
		}
	}
	return
}
