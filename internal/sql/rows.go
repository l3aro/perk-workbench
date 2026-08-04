package sql

import (
	"database/sql"
	"errors"
	"fmt"
)

// collectRow converts scanned database values into a display row (sanitized
// and capped at MaxRunes) and a raw row (full value, preserved for the cell
// viewer). The cell is converted to a string exactly once and both rows share
// the derived display string.
func collectRow(values []any) (display, raw []*string) {
	display = make([]*string, len(values))
	raw = make([]*string, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		var text string
		if bytes, ok := value.([]byte); ok {
			text = string(bytes)
		} else {
			text = fmt.Sprint(value)
		}
		raw[index] = &text
		sanitized := SanitizeDisplay(text, MaxRunes)
		display[index] = &sanitized
	}
	return display, raw
}

func CollectRows(rows *sql.Rows) (Result, error) {
	columns, err := rows.Columns()
	if err != nil {
		return Result{}, CloseRows(rows, "reading result columns", err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return Result{}, CloseRows(rows, "reading result column types", err)
	}
	result := Result{Columns: make([]string, len(columns)), ColumnTypes: make([]string, len(columnTypes)), Rows: [][]*string{}}
	for index, column := range columns {
		result.Columns[index] = SanitizeDisplay(column)
	}
	for index, columnType := range columnTypes {
		result.ColumnTypes[index] = columnType.DatabaseTypeName()
	}

	// Scan destinations are reused across rows: database/sql copies values
	// into them and collectRow converts to strings immediately.
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	for rows.Next() {
		if len(result.Rows) == MaxRows {
			result.Truncated = true
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			return Result{}, CloseRows(rows, "scanning result row", err)
		}
		display, raw := collectRow(values)
		result.Rows = append(result.Rows, display)
		result.UntruncatedRows = append(result.UntruncatedRows, raw)
	}
	if err := rows.Err(); err != nil {
		return Result{}, CloseRows(rows, "iterating result rows", err)
	}
	if err := rows.Close(); err != nil {
		return Result{}, fmt.Errorf("closing result rows: %w", err)
	}
	return result, nil
}

func CloseRows(rows *sql.Rows, action string, err error) error {
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("%s: %w", action, errors.Join(err, closeErr))
	}
	return fmt.Errorf("%s: %w", action, err)
}
