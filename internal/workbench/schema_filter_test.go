package workbench

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// officeDemoItems mirrors the sidebar shape of the MySQL office demo:
// database roots plus tables under them.
func officeDemoItems() []list.Item {
	return []list.Item{
		schemaItem{title: "information_schema", root: true},
		schemaItem{title: "mysql", root: true},
		schemaItem{title: "office", root: true},
		schemaItem{title: "customers", database: "office", table: "customers", kind: "table"},
		schemaItem{title: "offices", database: "office", table: "offices", kind: "table"},
		schemaItem{title: "rez_abc_", database: "office", table: "rez_abc_", kind: "table"},
	}
}

func schemaFilterTargets(items []list.Item) []string {
	targets := make([]string, len(items))
	for index, item := range items {
		targets[index] = item.FilterValue()
	}
	return targets
}

func schemaFilterMatches(term string, targets []string) []string {
	ranks := schemaListFilter(term, targets)
	got := make([]string, 0, len(ranks))
	for _, rank := range ranks {
		got = append(got, targets[rank.Index])
	}
	return got
}

func TestSchemaListFilter_globPatterns(t *testing.T) {
	targets := schemaFilterTargets(officeDemoItems())

	if got := schemaFilterMatches("of*", targets); len(got) != 2 || !strings.Contains(got[0], "office") || !strings.Contains(got[1], "offices") {
		t.Fatalf("of* matches = %#v, want office and offices", got)
	}
	if got := schemaFilterMatches("OF*", targets); len(got) != 2 {
		t.Fatalf("OF* matches = %#v, want case-insensitive office and offices", got)
	}
	if got := schemaFilterMatches("off?ce", targets); len(got) != 1 || !strings.Contains(got[0], "office") {
		t.Fatalf("off?ce matches = %#v, want only office (anchored)", got)
	}
	// _ is literal: rez_*_ matches the rez_abc_ table only.
	if got := schemaFilterMatches("rez_*_", targets); len(got) != 1 || !strings.Contains(got[0], "rez_abc_") {
		t.Fatalf("rez_*_ matches = %#v, want only rez_abc_", got)
	}
	if got := schemaFilterMatches("*", targets); len(got) != len(targets) {
		t.Fatalf("* matches = %d, want all %d", len(got), len(targets))
	}
}

func TestSchemaListFilter_plainTermKeepsFuzzyMatching(t *testing.T) {
	targets := schemaFilterTargets(officeDemoItems())
	// "ffice" is a fuzzy (subsequence) match for office/offices, not a
	// prefix glob; the plain-term path must still run the default filter.
	got := schemaFilterMatches("ffice", targets)
	found := false
	for _, match := range got {
		if strings.Contains(match, "office") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ffice matches = %#v, want fuzzy office match", got)
	}
	// Multi-word fuzzy terms behave as before the separator change.
	if got := schemaFilterMatches("office table", targets); len(got) == 0 {
		t.Fatal("office table matches = none, want fuzzy matches")
	}
}

func TestFocus_schema_filter_globPattern(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	if err := model.schema.SetItems(officeDemoItems()); err != nil {
		t.Fatalf("setting schema items: %v", err)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	for _, key := range []rune{'r', 'e', 'z', '_', '*', '_'} {
		updated, _ = model.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		model = updated.(Model)
	}
	if got := model.schema.VisibleItems(); len(got) != 1 || got[0].(schemaItem).title != "rez_abc_" {
		t.Fatalf("visible items = %#v, want only rez_abc_", got)
	}
}
