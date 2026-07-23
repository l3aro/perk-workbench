package workbench

import (
	"fmt"
	"strings"
)

func (f browseForm) updateStatement(table string) (string, error) {
	if !f.active() || len(f.primary) == 0 {
		return "", fmt.Errorf("selected row cannot be updated")
	}
	var sets []string
	for index, column := range f.columns {
		if f.isDirty(index) {
			sets = append(sets, quoteBrowseIdentifier(column)+" = "+f.value(index))
		}
	}
	if len(sets) == 0 {
		return "", nil
	}
	where := make([]string, len(f.primary))
	for index, primary := range f.primary {
		if f.original[primary] == nil {
			where[index] = quoteBrowseIdentifier(f.columns[primary]) + " IS NULL"
		} else {
			where[index] = quoteBrowseIdentifier(f.columns[primary]) + " = " + quoteBrowseValue(*f.original[primary])
		}
	}
	return "UPDATE " + quoteBrowseIdentifier(table) + " SET " + strings.Join(sets, ", ") + " WHERE " + strings.Join(where, " AND "), nil
}

func (f browseForm) isDirty(index int) bool {
	if f.original[index] == nil {
		return !f.values.nulls[index] // was NULL, now has a value → dirty
	}
	if f.values.nulls[index] {
		return true // had a value, now NULL → dirty
	}
	return f.values.fields[index] != *f.original[index] // value changed
}

func (f browseForm) value(index int) string {
	if f.values.nulls[index] {
		return "NULL"
	}
	return quoteBrowseValue(f.values.fields[index])
}

func quoteBrowseIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quoteBrowseValue(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
