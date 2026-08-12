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
	items := []CompletionItem{
		{Label: "SELECT", InsertText: "SELECT"},
		{Label: "sessions", InsertText: "sessions"},
		{Label: "status", InsertText: "status"},
	}
	completion := newCompletion(items)

	// When
	completion.filter("se")
	inserted := completion.accept()

	// Then
	if got, want := inserted.Label, "SELECT"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
}

func TestCompletionSuggestions_refilterAfterTyping(t *testing.T) {
	// Given
	items := []CompletionItem{
		{Label: "SELECT", InsertText: "SELECT"},
		{Label: "sessions", InsertText: "sessions"},
		{Label: "status", InsertText: "status"},
	}
	completion := newCompletion(items)
	completion.filter("s")

	// When
	completion.filter("st")

	// Then
	if got, want := completion.accept().Label, "status"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
}

func TestEditor_completionAfterQualifiedTableInsertsColumn(t *testing.T) {
	// Given
	editor := newEditor()
	editor.setValue("SELECT orders.")
	editor.showCompletionFor("", []CompletionItem{{Label: "id", InsertText: "id"}})

	// When
	editor.acceptCompletion()

	// Then
	if got, want := editor.value, "SELECT orders.id"; got != want {
		t.Fatalf("editor value = %q, want %q", got, want)
	}
}

func TestModel_completionKeyShowsKeywordAndTableSuggestions(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus, model.Tab = stateReady, focusWorkspace, tabSQL
	model.schema.objects = []sharedsql.SchemaObject{{Name: "sessions", Type: "table"}}
	model.queryLog.editor.setValue("S")
	// Enter insert mode first (pressing 'i' in normal mode does this).
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	// When: Ctrl+Space triggers completion.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if !model.queryLog.editor.completion.visible() {
		t.Fatal("completion should be visible after Ctrl+Space")
	}
	if got, want := model.queryLog.editor.completion.accept().Label, "SELECT"; got != want {
		t.Fatalf("top completion = %q, want %q", got, want)
	}
}

func TestModel_completionColumnsCachesQualifiedTableResult(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.queryLog.completionRequestTag = 1
	model.queryLog.completionTable = "orders"
	model.queryLog.editor.setValue("SELECT orders.")

	// When
	updated, _ := model.updateCompletionColumns(completionColumnsMsg{
		tag: 1, table: "orders", columns: []sharedsql.ColumnInfo{{Name: "id"}},
	})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.editor.completion.accept().Label, "id"; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
	if got, want := model.queryLog.completionColumns["orders"], []string{"id"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("completionColumns = %q, want %q", got, want)
	}
}
