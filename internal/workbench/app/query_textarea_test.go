package app

import (
	"testing"

	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestQueryTextareaTokenizesMultilineSQLState(t *testing.T) {
	value := "/* comment starts\ncontinues here */ SELECT 1"
	lines := queryTokenizeLines(value, queryLexer(sharedsql.SQLQueryLanguage))
	if len(lines) != 2 {
		t.Fatalf("got %d hard lines, want 2", len(lines))
	}
	if len(lines[1]) == 0 || lines[1][0].category != queryStyleComment {
		t.Fatalf("continuation line = %#v, want comment style", lines[1])
	}
}

func TestQueryTextareaCachePersistsAndInvalidates(t *testing.T) {
	text := newQueryTextarea(20, 3, sharedsql.SQLQueryLanguage)
	text.SetValue("SELECT 1")
	first := text.styledLines(text.Value(), 20)
	second := text.styledLines(text.Value(), 20)
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("repeated rendering did not reuse the visual cache")
	}

	text.SetValue("SELECT 2")
	third := text.styledLines(text.Value(), 20)
	if len(third) == 0 || &first[0] == &third[0] {
		t.Fatal("value change reused stale visual cache")
	}
}

func TestQueryTextareaWrapUsesRuneWidths(t *testing.T) {
	style := lipgloss.NewStyle()
	runes := []styledRune{
		{rune: '界', style: style},
		{rune: 'e', style: style},
		{rune: '\u0301', style: style},
	}
	wrapped := queryWrapRunes(runes, 2)
	if len(wrapped) != 2 {
		t.Fatalf("got %d wrapped lines, want 2", len(wrapped))
	}
	if got := string([]rune{wrapped[0][0].rune}); got != "界" {
		t.Fatalf("first line = %q, want wide rune", got)
	}
	if got := string([]rune{wrapped[1][0].rune, wrapped[1][1].rune}); got != "e\u0301" {
		t.Fatalf("second line = %q, want combining sequence", got)
	}
	if queryRuneWidth('\u0301') != 0 || queryRuneWidth('界') != 2 {
		t.Fatal("unexpected combining or wide rune width")
	}
}
