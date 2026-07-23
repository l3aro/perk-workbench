package workbench

import (
	"fmt"
	"strings"
)

func (f browseForm) updateStatement(table string) (string, error) {
	if !f.active() || len(f.primary) == 0 {
		return "", fmt.Errorf("selected row cannot be updated")
	}
	sets := make([]string, len(f.columns))
	for index, column := range f.columns {
		sets[index] = quoteBrowseIdentifier(column) + " = " + f.value(index)
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
