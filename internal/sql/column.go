package sql

import (
	"errors"
	"strings"
	"unicode"
)

func ValidateColumnChange(change ColumnChange) error {
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
	return nil
}
