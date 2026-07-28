package workbench

import (
	"testing"

	"charm.land/bubbles/v2/table"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestTableFilters_filterAndResetEachWorkspaceTable(t *testing.T) {
	tests := []struct {
		name  string
		tab   workspaceTab
		load  func(Model) Model
		query string
		want  string
	}{
		{
			name: "columns", tab: tabStructure, query: "name", want: "name",
			load: func(model Model) Model {
				updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}}})
				return updated.(Model)
			},
		},
		{
			name: "indexes", tab: tabIndexes, query: "category", want: "items_category",
			load: func(model Model) Model {
				updated, _ := model.Update(indexesLoadedMsg{table: "items", indexes: []sharedsql.IndexInfo{{Name: "items_name", Columns: []string{"name"}}, {Name: "items_category", Columns: []string{"category"}}}})
				return updated.(Model)
			},
		},
		{
			name: "foreign keys", tab: tabForeignKeys, query: "parents", want: "parent_id",
			load: func(model Model) Model {
				updated, _ := model.Update(foreignKeysLoadedMsg{table: "items", foreignKeys: []sharedsql.ForeignKeyInfo{{ID: "0", Columns: []string{"parent_id"}, ReferenceTable: "parents", ReferenceColumns: []string{"id"}}, {ID: "1", Columns: []string{"owner_id"}, ReferenceTable: "owners", ReferenceColumns: []string{"id"}}}})
				return updated.(Model)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := readyModel(t)
			model.SelectedTable, model.Tab, model.Focus = "items", test.tab, focusWorkspace
			model = test.load(model)

			updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
			model = updated.(Model)
			for _, character := range test.query {
				updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
				model = updated.(Model)
			}

			var rows []table.Row
			switch test.tab {
			case tabStructure:
				rows = model.structure.Rows()
			case tabIndexes:
				rows = model.indexes.Rows()
			case tabForeignKeys:
				rows = model.foreignKeys.Rows()
			}
			if !model.tableFiltering || len(rows) != 1 || !containsRowValue(rows[0], test.want) {
				t.Fatalf("filtered state/rows = %t/%#v, want one row containing %q", model.tableFiltering, rows, test.want)
			}

			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
			model = updated.(Model)
			if model.tableFilterValue(test.tab) != "" || len(rowsForTab(model, test.tab)) != 2 {
				t.Fatalf("filter/rows after reset = %q/%#v, want empty/two rows", model.tableFilterValue(test.tab), rowsForTab(model, test.tab))
			}
		})
	}
}

func containsRowValue(row table.Row, want string) bool {
	for _, value := range row {
		if value == want {
			return true
		}
	}
	return false
}

func rowsForTab(model Model, tab workspaceTab) []table.Row {
	switch tab {
	case tabStructure:
		return model.structure.Rows()
	case tabIndexes:
		return model.indexes.Rows()
	case tabForeignKeys:
		return model.foreignKeys.Rows()
	default:
		return nil
	}
}
