package workbench

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExplainOptions_matchesDriverAndStatementCapabilities(t *testing.T) {
	tests := []struct {
		name      string
		product   string
		version   string
		statement string
		want      []string
	}{
		{name: "SQLite query", product: "SQLite", statement: "SELECT 1", want: []string{"EXPLAIN", "EXPLAIN QUERY PLAN"}},
		{name: "SQLite ignores existing explain", product: "SQLite", statement: "EXPLAIN SELECT 1"},
		{name: "MySQL data mutation", product: "MySQL", version: "8.0.36", statement: "UPDATE projects SET name = 'next'", want: []string{"EXPLAIN"}},
		{name: "MySQL analyze", product: "MySQL", version: "8.0.18", statement: "SELECT 1", want: []string{"EXPLAIN", "EXPLAIN ANALYZE"}},
		{name: "older MySQL", product: "MySQL", version: "8.0.17", statement: "SELECT 1", want: []string{"EXPLAIN"}},
		{name: "MariaDB", product: "MySQL", version: "10.11.4-MariaDB", statement: "SELECT 1", want: []string{"EXPLAIN"}},
		{name: "unsupported MySQL statement", product: "MySQL", version: "8.0.36", statement: "CREATE TABLE projects (id INTEGER)"},
		{name: "PostgreSQL query", product: "PostgreSQL", statement: "SELECT 1", want: []string{"EXPLAIN", "EXPLAIN ANALYZE"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := explainOptions(test.product, test.version, test.statement); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("explain options = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestExplainShortcut_prefillsSelectedCommandAndFocusesSQLEditor(t *testing.T) {
	// Given
	model := readyModel(t)
	model.databaseInfo.Product = "SQLite"
	model.editor.setValue("SELECT current editor")
	model.appendQueryLog(queryLogEntry{statement: "SELECT 1"})
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)
	if command == nil || model.explainPicker == nil {
		t.Fatal("e did not open the explain command picker")
	}
	model = resolveExplainCommand(model, command)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = resolveExplainCommand(model, command)

	// Then
	if model.explainPicker != nil {
		t.Fatal("selection did not close the explain command picker")
	}
	if model.Focus != focusWorkspace || model.Tab != tabSQL || !model.formMode.editing() {
		t.Fatalf("SQL editor focus = focus:%v tab:%v editing:%t", model.Focus, model.Tab, model.formMode.editing())
	}
	if got, want := model.editor.value, "EXPLAIN QUERY PLAN\nSELECT 1"; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	if command == nil || !model.Running() {
		t.Fatal("prefilled explain query did not start")
	}
	model = driveCommand(model, command)
	if model.Running() || len(model.results.Rows()) == 0 {
		t.Fatalf("explain query did not complete with results: running=%t rows=%#v", model.Running(), model.results.Rows())
	}
}

func TestExplainShortcut_discardsUnsupportedStatements(t *testing.T) {
	// Given
	model := readyModel(t)
	model.databaseInfo.Product, model.databaseInfo.Version = "MySQL", "8.0.36"
	model.editor.setValue("SELECT current editor")
	model.appendQueryLog(queryLogEntry{statement: "CREATE TABLE projects (id INTEGER)"})
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	model = updated.(Model)

	// Then
	if command != nil || model.explainPicker != nil {
		t.Fatalf("unsupported statement opened picker: command=%t picker=%t", command != nil, model.explainPicker != nil)
	}
	if got, want := model.editor.value, "SELECT current editor"; got != want {
		t.Fatalf("editor value = %q, want unchanged %q", got, want)
	}
}

func resolveExplainCommand(model Model, command tea.Cmd) Model {
	return driveCommand(model, command)
}
