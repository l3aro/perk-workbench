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
	if change.Attributes != nil && (strings.IndexFunc(*change.Attributes, unicode.IsControl) >= 0 || strings.Contains(*change.Attributes, ";")) {
		return errors.New("column attributes contain unsupported characters")
	}
	return nil
}

func ValidateColumnDef(col ColumnDef) error {
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

// ValidateColumnAttributeChange returns an error when the caller does not
// support column-level attribute changes and a non-nil, differing value is
// requested.
func ValidateColumnAttributeChange(changeAttributes *string, currentAttributes string) error {
	if changeAttributes != nil && *changeAttributes != currentAttributes {
		return errors.New("column attributes change is not supported for this database")
	}
	return nil
}
