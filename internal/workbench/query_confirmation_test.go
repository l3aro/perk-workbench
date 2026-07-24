package workbench

import "testing"

func TestRequiresQueryConfirmation_statement_classes(t *testing.T) {
	tests := []struct {
		statement string
		want      bool
	}{
		{statement: "SELECT 1"},
		{statement: "-- inspect\nSELECT 1"},
		{statement: "UPDATE projects SET name = 'next'", want: true},
		{statement: "DELETE FROM projects", want: true},
		{statement: "CREATE TABLE projects (id INTEGER)", want: true},
		{statement: "ALTER TABLE projects ADD COLUMN name TEXT", want: true},
		{statement: "DROP TABLE projects", want: true},
		{statement: "BEGIN", want: true},
		{statement: "COMMIT", want: true},
		{statement: "ROLLBACK", want: true},
	}
	for _, test := range tests {
		t.Run(test.statement, func(t *testing.T) {
			if got := requiresQueryConfirmation(test.statement); got != test.want {
				t.Fatalf("requiresQueryConfirmation(%q) = %t, want %t", test.statement, got, test.want)
			}
		})
	}
}
