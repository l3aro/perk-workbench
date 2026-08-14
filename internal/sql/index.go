package sql

import (
	"fmt"
	"slices"
	"strings"
)

type IndexInfo struct {
	Name       string   `json:"name"`
	Unique     bool     `json:"unique"`
	PrimaryKey bool     `json:"primary_key"`
	Columns    []string `json:"columns"`
}

type IndexChange struct {
	Name       string   `json:"name"`
	Unique     bool     `json:"unique"`
	PrimaryKey bool     `json:"primary_key"`
	Columns    []string `json:"columns"`
}

func ValidateIndexChange(change IndexChange) error {
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

func IndexColumnsEqual(left, right []string) bool { return slices.Equal(left, right) }
