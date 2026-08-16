package sql

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// QueryCommand is one static command the query editor may complete from
// a driver's language advertisement: the canonical command name, a
// Redis-native-style usage line, and a concise summary. All three are
// required, nonblank, bounded, and control-free; names must be unique
// case-insensitively and the list is capped so a plugin can never force
// an unbounded completion list or handshake frame.
type QueryCommand struct {
	Name    string `json:"name"`
	Usage   string `json:"usage"`
	Summary string `json:"summary"`
}

// QueryLanguage is the serializable advertisement of how the query
// editor presents one driver's statements: the language name, the
// editor tab label, the input placeholder, an optional lexer hint,
// optional example statements the driver's parser already accepts, and
// an optional static command catalog for completion. It crosses the
// plugin DTO boundary unchanged.
type QueryLanguage struct {
	Name        string         `json:"name"`
	EditorLabel string         `json:"editor_label"`
	Placeholder string         `json:"placeholder"`
	Lexer       string         `json:"lexer,omitempty"`
	Examples    []string       `json:"examples,omitempty"`
	Commands    []QueryCommand `json:"commands,omitempty"`
}

// Bounds every nonzero query language advertisement must respect, so a
// plugin can never force an unbounded completion list, overlay, or
// handshake frame.
const (
	// MaxQueryCommands caps the advertised command catalog.
	MaxQueryCommands = 512
	// MaxQueryCommandNameRunes caps one command name.
	MaxQueryCommandNameRunes = 64
	// MaxQueryCommandUsageRunes caps one usage line.
	MaxQueryCommandUsageRunes = 256
	// MaxQueryCommandSummaryRunes caps one summary line.
	MaxQueryCommandSummaryRunes = 256
)

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
// all — every field blank and no examples or commands.
func IsZeroQueryLanguage(ql QueryLanguage) bool {
	return ql.Name == "" && ql.EditorLabel == "" && ql.Placeholder == "" &&
		ql.Lexer == "" && len(ql.Examples) == 0 && len(ql.Commands) == 0
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

// ValidateQueryLanguage checks the invariant set every nonzero query
// language advertisement must hold: name, editor label, and placeholder
// must be nonblank after trimming, every example must be nonblank, and
// every optional command entry must carry nonblank bounded control-free
// name/usage/summary, with names unique case-insensitively within the
// capped list. A zero value is not an advertisement and passes. This is
// the single Go invariant set shared by driver registration and the
// plugin conformance runner.
func ValidateQueryLanguage(ql QueryLanguage) error {
	if IsZeroQueryLanguage(ql) {
		return nil
	}
	switch {
	case strings.TrimSpace(ql.Name) == "":
		return errors.New("query language needs a name")
	case strings.TrimSpace(ql.EditorLabel) == "":
		return fmt.Errorf("query language %q needs an editor label", ql.Name)
	case strings.TrimSpace(ql.Placeholder) == "":
		return fmt.Errorf("query language %q needs a placeholder", ql.Name)
	}
	for i, example := range ql.Examples {
		if strings.TrimSpace(example) == "" {
			return fmt.Errorf("query language %q example %d must not be blank", ql.Name, i)
		}
	}
	if err := ValidateQueryCommands(ql.Name, ql.Commands); err != nil {
		return err
	}
	return nil
}

// ValidateQueryCommands checks the static command catalog of one query
// language advertisement: the list is capped, every entry carries
// nonblank bounded control-free name/usage/summary, names are ASCII
// letters/digits/underscores (the exact charset the editor tokenizes,
// so every advertised name is completable), and names are unique
// case-insensitively — exact for ASCII, where lowercase folding is
// total.
func ValidateQueryCommands(language string, commands []QueryCommand) error {
	if len(commands) > MaxQueryCommands {
		return fmt.Errorf("query language %q advertises %d commands, cap is %d", language, len(commands), MaxQueryCommands)
	}
	seen := make(map[string]bool, len(commands))
	for i, command := range commands {
		if err := validateCommandName(language, i, command.Name); err != nil {
			return err
		}
		if err := validateCommandField(language, "usage", i, command.Usage, MaxQueryCommandUsageRunes); err != nil {
			return err
		}
		if err := validateCommandField(language, "summary", i, command.Summary, MaxQueryCommandSummaryRunes); err != nil {
			return err
		}
		key := strings.ToLower(command.Name)
		if seen[key] {
			return fmt.Errorf("query language %q command name %q is repeated case-insensitively", language, command.Name)
		}
		seen[key] = true
	}
	return nil
}

func validateCommandName(language string, index int, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("query language %q command %d name must not be blank", language, index)
	}
	if runes := len([]rune(name)); runes > MaxQueryCommandNameRunes {
		return fmt.Errorf("query language %q command %d name is %d runes, cap is %d", language, index, runes, MaxQueryCommandNameRunes)
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return fmt.Errorf("query language %q command %d name %q must be ASCII letters, digits, or underscores", language, index, name)
		}
	}
	return nil
}

func validateCommandField(language, field string, index int, value string, maxRunes int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("query language %q command %d %s must not be blank", language, index, field)
	}
	if runes := len([]rune(value)); runes > maxRunes {
		return fmt.Errorf("query language %q command %d %s is %d runes, cap is %d", language, index, field, runes, maxRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("query language %q command %d %s contains a control character", language, index, field)
		}
	}
	return nil
}
