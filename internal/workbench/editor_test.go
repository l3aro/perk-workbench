package workbench

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestEditor_textareaBindsSQLValue(t *testing.T) {
	// Given
	editor := newEditor()
	editor.text.Focus()

	// When
	_ = editor.update(tea.KeyPressMsg{Code: 'S', Text: "S"})

	// Then
	if got := editor.value; got != "S" {
		t.Fatalf("SQL editor value = %q, want S", got)
	}
}

func TestCompletionSuggestions_filterAndInsertSelectedValue(t *testing.T) {
	// Given
	completion := newCompletion([]string{"SELECT", "sessions", "status"})

	// When
	completion.filter("se")
	inserted := completion.accept()

	// Then
	if got, want := inserted, "SELECT"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
}

func TestCompletionSuggestions_refilterAfterTyping(t *testing.T) {
	// Given
	completion := newCompletion([]string{"SELECT", "sessions", "status"})
	completion.filter("s")

	// When
	completion.filter("st")

	// Then
	if got, want := completion.accept(), "status"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
}

func TestEditor_completionAfterQualifiedTableInsertsColumn(t *testing.T) {
	// Given
	editor := newEditor()
	editor.setValue("SELECT orders.")
	editor.showCompletionFor("", []string{"id"})

	// When
	editor.acceptCompletion()

	// Then
	if got, want := editor.value, "SELECT orders.id"; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
}

func TestModel_completionKeyShowsKeywordAndTableSuggestions(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
	model.schemaObjects = []sharedsql.SchemaObject{{Name: "sessions", Type: "table"}}
	model.editor.setValue("se")

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if got, want := model.editor.completion.accept(), "SELECT"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
}

func TestModel_completionColumnsCachesQualifiedTableResult(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.completionRequestTag = 1
	model.completionTable = "orders"
	model.editor.setValue("SELECT orders.")

	// When
	updated, _ := model.updateCompletionColumns(completionColumnsMsg{
		tag: 1, table: "orders", columns: []sharedsql.ColumnInfo{{Name: "id"}},
	})
	model = updated.(Model)

	// Then
	if got, want := model.editor.completion.accept(), "id"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
	if got, want := model.completionColumns["orders"], []string{"id"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("cached columns = %q, want %q", got, want)
	}
}
