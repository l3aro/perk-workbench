package workbench

import (
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// CompletionKind classifies the type of suggestion.
type CompletionKind int

const (
	KindKeyword CompletionKind = iota
	KindTable
	KindView
	KindColumn
	KindSchema
	KindFunction
	KindBufferWord
)

func (k CompletionKind) String() string {
	switch k {
	case KindKeyword:
		return "keyword"
	case KindTable:
		return "table"
	case KindView:
		return "view"
	case KindColumn:
		return "column"
	case KindSchema:
		return "schema"
	case KindFunction:
		return "function"
	case KindBufferWord:
		return "buffer"
	default:
		return ""
	}
}

// CompletionItem is a single suggestion candidate.
type CompletionItem struct {
	Label      string         // display text
	InsertText string         // text inserted on accept (defaults to Label)
	Kind       CompletionKind // for display/label
	Detail     string         // extra info shown alongside the label
}

type completion struct {
	items    []CompletionItem // all candidates
	matches  []CompletionItem // filtered subset
	prefix   string
	selected int
}

func newCompletion(items []CompletionItem) completion {
	// Ensure InsertText defaults to Label.
	for i := range items {
		if items[i].InsertText == "" {
			items[i].InsertText = items[i].Label
		}
	}
	return completion{items: items}
}

// filter narrows matches to those with a case-insensitive prefix match.
func (c *completion) filter(prefix string) {
	c.prefix = prefix
	lower := strings.ToLower(prefix)
	c.matches = c.matches[:0]
	for _, item := range c.items {
		if strings.HasPrefix(strings.ToLower(item.Label), lower) {
			c.matches = append(c.matches, item)
		}
	}
	c.selected = 0
}

func (c completion) accept() CompletionItem {
	if len(c.matches) == 0 {
		return CompletionItem{}
	}
	return c.matches[c.selected]
}

func (c *completion) move(delta int) {
	if len(c.matches) == 0 {
		return
	}
	c.selected = (c.selected + delta + len(c.matches)) % len(c.matches)
}

func (c completion) visible() bool { return len(c.matches) > 0 }

// completionItemForColumn creates a CompletionItem from a column name with type info.
func completionItemForColumn(name, typeName, tableRef string) CompletionItem {
	detail := tableRef
	if typeName != "" {
		detail = typeName + " · " + tableRef
	}
	return CompletionItem{
		Label:      name,
		InsertText: name,
		Kind:       KindColumn,
		Detail:     detail,
	}
}

// completionItemForObject creates a CompletionItem from a schema object.
func completionItemForObject(object sharedsql.SchemaObject) CompletionItem {
	kind := KindTable
	if object.Type == "view" {
		kind = KindView
	}
	return CompletionItem{
		Label:      object.Name,
		InsertText: object.Name,
		Kind:       kind,
		Detail:     object.Database,
	}
}

// keywordItem creates a keyword CompletionItem.
func keywordItem(keyword string) CompletionItem {
	return CompletionItem{Label: keyword, InsertText: keyword, Kind: KindKeyword}
}

// bufferWordItem creates a buffer-word CompletionItem.
func bufferWordItem(word string) CompletionItem {
	return CompletionItem{Label: word, InsertText: word, Kind: KindBufferWord, Detail: "buffer"}
}

// BuiltinFunctions returns SQL built-in function names keyed by product name.
var BuiltinFunctions = map[string][]string{
	"SQLite": {
		"ABS", "AVG", "CAST", "CHAR", "COALESCE", "COUNT", "CURRENT_DATE",
		"CURRENT_TIME", "CURRENT_TIMESTAMP", "DATE", "DATETIME", "GROUP_CONCAT",
		"IFNULL", "INSTR", "JSON_EXTRACT", "JSON_OBJECT", "JSON_ARRAY",
		"JULIANDAY", "LENGTH", "LIKE", "LOWER", "LTRIM", "MAX", "MIN", "NULLIF",
		"PRINTF", "RANDOM", "REPLACE", "ROUND", "RTRIM", "STRFTIME", "SUBSTR",
		"SUM", "TIME", "TRIM", "TYPEOF", "UNICODE", "UPPER", "ZEROBLOB",
	},
	"MySQL": {
		"AVG", "CAST", "CHAR_LENGTH", "COALESCE", "CONCAT", "COUNT", "CURDATE",
		"CURRENT_DATE", "CURRENT_TIMESTAMP", "CURTIME", "DATE", "DATE_FORMAT",
		"DATEDIFF", "DAY", "FIND_IN_SET", "FORMAT", "FROM_UNIXTIME", "GROUP_CONCAT",
		"IFNULL", "INSTR", "JSON_EXTRACT", "JSON_OBJECT", "JSON_ARRAY",
		"LENGTH", "LOCATE", "LOWER", "LPAD", "LTRIM", "MAX", "MD5", "MIN",
		"MONTH", "NOW", "NULLIF", "RAND", "REGEXP", "REPLACE", "REVERSE",
		"ROUND", "RPAD", "RTRIM", "SHA1", "SHA2", "SUBSTRING", "SUM",
		"TIMESTAMP", "TRIM", "TRUNCATE", "UNIX_TIMESTAMP", "UPPER", "UUID",
		"VERSION", "WEEK", "YEAR",
	},
	"PostgreSQL": {
		"ABS", "AGE", "ARRAY_AGG", "AVG", "CAST", "CEIL", "CHAR_LENGTH",
		"COALESCE", "CONCAT", "COUNT", "CURRENT_DATE", "CURRENT_TIME",
		"CURRENT_TIMESTAMP", "DATE", "DATE_PART", "DATE_TRUNC", "DATEDIFF",
		"EXTRACT", "FLOOR", "GREATEST", "JSON_AGG", "JSON_BUILD_OBJECT",
		"JSON_EXTRACT_PATH", "LATERAL", "LEAST", "LENGTH", "LOWER", "LPAD",
		"LTRIM", "MAX", "MIN", "NOW", "NULLIF", "POSITION", "POW",
		"RANDOM", "REGEXP_MATCHES", "REGEXP_REPLACE", "REPLACE", "REVERSE",
		"ROUND", "RPAD", "RTRIM", "SPLIT_PART", "STRING_AGG", "SUBSTRING",
		"SUM", "TO_CHAR", "TO_DATE", "TO_NUMBER", "TO_TIMESTAMP", "TRIM",
		"TRUNC", "UPPER", "UUID_GENERATE_V4", "WIDTH_BUCKET",
	},
}
