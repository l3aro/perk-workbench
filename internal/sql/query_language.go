package sql

// QueryLanguage is the serializable advertisement of how the query
// editor presents one driver's statements: the language name, the
// editor tab label, the input placeholder, an optional lexer hint, and
// optional example statements the driver's parser already accepts. It
// crosses the plugin DTO boundary unchanged.
type QueryLanguage struct {
	Name        string   `json:"name"`
	EditorLabel string   `json:"editor_label"`
	Placeholder string   `json:"placeholder"`
	Lexer       string   `json:"lexer,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// SQLQueryLanguage is the legacy SQL default every driver without an
// explicit query language advertisement gets: the query editor presents
// SQL with the conventional placeholder and lexer.
var SQLQueryLanguage = QueryLanguage{
	Name:        "SQL",
	EditorLabel: "SQL",
	Placeholder: "Enter a query…",
	Lexer:       "sql",
}

// IsZeroQueryLanguage reports whether ql carries no advertisement at
// all — every field blank and no examples.
func IsZeroQueryLanguage(ql QueryLanguage) bool {
	return ql.Name == "" && ql.EditorLabel == "" && ql.Placeholder == "" &&
		ql.Lexer == "" && len(ql.Examples) == 0
}

// NormalizeQueryLanguage resolves a query language advertisement for
// presentation: an absent or all-zero advertisement falls back to the
// legacy SQL default.
func NormalizeQueryLanguage(ql QueryLanguage) QueryLanguage {
	if IsZeroQueryLanguage(ql) {
		return SQLQueryLanguage
	}
	return ql
}
