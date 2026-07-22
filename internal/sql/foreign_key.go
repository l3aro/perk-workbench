package sql

import (
	"fmt"
	"strings"
)

type ForeignKeyInfo struct {
	ID               string
	Columns          []string
	ReferenceTable   string
	ReferenceColumns []string
	OnDelete         string
	OnUpdate         string
}

type ForeignKeyChange struct {
	Columns          []string
	ReferenceTable   string
	ReferenceColumns []string
	OnDelete         string
	OnUpdate         string
}

func ValidateForeignKeyChange(change ForeignKeyChange) error {
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
