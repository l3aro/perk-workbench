package mysql

import "testing"

func TestPrimaryKeyStatements(t *testing.T) {
	// Given
	columns := []string{"id", "code"}

	// When
	statement := mysqlAddPrimaryKeyStatement("items", columns)

	// Then
	if statement != "ALTER TABLE `items` ADD PRIMARY KEY (`id`, `code`)" {
		t.Fatalf("mysqlAddPrimaryKeyStatement() = %q", statement)
	}
}
