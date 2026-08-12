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
				rows = model.structure.table.Rows()
			case tabIndexes:
				rows = model.structure.indexes.Rows()
			case tabForeignKeys:
				rows = model.structure.foreignKeys.Rows()
			}
			if !model.structure.tableFiltering || len(rows) != 1 || !containsRowValue(rows[0], test.want) {
				t.Fatalf("filtered state/rows = %t/%#v, want one row containing %q", model.structure.tableFiltering, rows, test.want)
			}

			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			model = updated.(Model)
			if model.structure.tableFiltering || model.tableFilterValue(test.tab) != test.query || len(rowsForTab(model, test.tab)) != 1 {
				t.Fatalf("filter after escape = active %t/value %q/rows %#v, want inactive/%q/one row",
					model.structure.tableFiltering, model.tableFilterValue(test.tab), rowsForTab(model, test.tab), test.query)
			}

			updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			if model.structure.tableFiltering || model.tableFilterValue(test.tab) != test.query {
				t.Fatalf("filter after enter = active %t/value %q, want inactive/%q",
					model.structure.tableFiltering, model.tableFilterValue(test.tab), test.query)
			}

			updated, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
			model = updated.(Model)
			if model.tableFilterValue(test.tab) != "" || len(rowsForTab(model, test.tab)) != 2 {
				t.Fatalf("filter/rows after reset = %q/%#v, want empty/two rows", model.tableFilterValue(test.tab), rowsForTab(model, test.tab))
			}
		})
	}
}

func TestTableFilters_mouseTabSwitchClosesSession(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab, model.Focus = "items", tabStructure, focusWorkspace
	updated, _ := model.Update(tableInfoMsg{table: "items", columns: []sharedsql.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}})
	model = updated.(Model)
	model.structure.indexRows = []table.Row{{"items_name"}, {"items_category"}}
	model.structure.indexes.SetRows(model.structure.indexRows)
	model.applyLayout(100, 24)

	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	if !model.structure.tableFiltering || model.tableFilterValue(tabStructure) != "i" {
		t.Fatalf("filter session = %t/query %q, want active/i", model.structure.tableFiltering, model.tableFilterValue(tabStructure))
	}

	updated, _ = model.Update(tea.MouseClickMsg{
		X:      model.layout.schemaWidth + 25,
		Y:      2,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if model.structure.tableFiltering {
		t.Fatal("table filter remained active after tab click")
	}
	if model.Tab != tabIndexes {
		t.Fatalf("active tab = %v, want indexes", model.Tab)
	}
	if model.tableFilterValue(tabStructure) != "i" || len(model.structure.table.Rows()) != 1 {
		t.Fatalf("columns filter = %q/rows %#v, want i/one row", model.tableFilterValue(tabStructure), model.structure.table.Rows())
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if model.structure.indexes.Cursor() != 1 {
		t.Fatalf("indexes cursor = %d, want 1 after j", model.structure.indexes.Cursor())
	}
	if model.tableFilterValue(tabStructure) != "i" {
		t.Fatalf("hidden columns filter changed to %q after indexes navigation", model.tableFilterValue(tabStructure))
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
		return model.structure.table.Rows()
	case tabIndexes:
		return model.structure.indexes.Rows()
	case tabForeignKeys:
		return model.structure.foreignKeys.Rows()
	default:
		return nil
	}
}
