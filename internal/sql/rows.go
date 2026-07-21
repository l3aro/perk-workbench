package sql

import (
	"database/sql"
	"errors"
	"fmt"
)

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

	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return Result{}, CloseRows(rows, "scanning result row", err)
		}
		if len(result.Rows) < MaxRows {
			result.Rows = append(result.Rows, DisplayRow(values))
		} else {
			result.Truncated = true
		}
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
