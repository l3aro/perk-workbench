package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

func TestStructureForm_typeListRendersAfterHeightIsSet(t *testing.T) {
	// Regression: rebuildForm used to apply WithHeight(1) while the pane
	// height was still unknown, and huh's group only shrinks fields, so the
	// type select's option list stayed frozen at a single row forever.
	// The list must render once the real height is applied.
	form := schema.NewColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", PrimaryKey: 1}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	form.SetWidth(72)
	form.SetHeight(20)
	for _, message := range executeCommandAll(form.Form.Init()) {
		form.Form.Update(message)
	}
	form.FocusField(1)
	if got := form.Form.GetFocusedField().GetKey(); got != "type" {
		t.Fatalf("focused field = %q, want type", got)
	}
	view := ansi.Strip(form.View())
	for _, label := range []string{"INTEGER — whole number", "VARCHAR — variable-length string"} {
		if !strings.Contains(view, label) {
			t.Fatalf("type select view missing option label %q:\n%s", label, view)
		}
	}
}

func TestStructureForm_typeChoicesShowFriendlyLabels(t *testing.T) {
	// Given
	form := schema.NewColumnForm(sqlite.ColumnInfo{Name: "id", Type: "INTEGER", PrimaryKey: 1}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
	options := form.TypeChoices()

	// When/Then — every option keeps the SQL type as its value but shows a
	// descriptive label that still contains the type name for filtering.
	if len(options) != len(form.TypeOptions) {
		t.Fatalf("type choices = %d, want %d", len(options), len(form.TypeOptions))
	}
	for index, option := range options {
		if option.Value != form.TypeOptions[index].Name {
			t.Errorf("choice %d value = %q, want %q", index, option.Value, form.TypeOptions[index].Name)
		}
		if option.Key == "" || option.Key == option.Value {
			t.Errorf("choice %d key = %q, want descriptive label", index, option.Key)
		}
		if !strings.Contains(option.Key, option.Value) {
			t.Errorf("choice %d key = %q, want it to contain %q", index, option.Key, option.Value)
		}
	}
}

func TestStructureForm_typeChoicesFallBackToNameWithoutLabel(t *testing.T) {
	// Given — an existing column whose type is unknown to the catalog: the
	// form prepends a synthetic option carrying only the raw name.
	types := append([]sharedsql.ColumnType{{Name: "CUSTOM"}}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"})...)
	form := schema.NewColumnForm(sqlite.ColumnInfo{Name: "id", Type: "CUSTOM", PrimaryKey: 1}, types)
	form.SetKeys(DefaultKeybindings())

	// When
	options := form.TypeChoices()

	// Then
	if options[0].Key != "CUSTOM" || options[0].Value != "CUSTOM" {
		t.Fatalf("synthetic type choice = %#v, want key and value CUSTOM", options[0])
	}
}
