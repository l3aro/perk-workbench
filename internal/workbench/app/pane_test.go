package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

func TestTitledPane_replacesTopBorderWithPlainTitle(t *testing.T) {
	// Given
	style := panelStyle.Width(20).Height(3)

	// When
	pane := titledPane("Databases", "items", style)
	plain := ansi.Strip(pane)
	firstLine, _, _ := strings.Cut(plain, "\n")

	// Then
	if !strings.HasPrefix(firstLine, "╭ Databases ") {
		t.Fatalf("top border = %q, want a plain Databases title", firstLine)
	}
	if got, want := lipgloss.Width(pane), lipgloss.Width(style.Render("items")); got != want {
		t.Fatalf("pane width = %d, want %d", got, want)
	}
	if got, want := lipgloss.Height(pane), lipgloss.Height(style.Render("items")); got != want {
		t.Fatalf("pane height = %d, want %d", got, want)
	}
}

func TestAbbreviateCount(t *testing.T) {
	// Given
	cases := map[int64]string{
		0:          "0",
		999:        "999",
		1000:       "1k",
		10420:      "10.42k",
		490000:     "490k",
		1000000:    "1M",
		1234567:    "1.23M",
		1230000000: "1.23B",
	}

	// When/Then
	for input, want := range cases {
		if got := schema.AbbreviateCount(input); got != want {
			t.Errorf("schema.AbbreviateCount(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestSchemaItemDelegate_rightAlignsBadgeAndTruncatesLongName(t *testing.T) {
	count := int64(331_600)
	model := schema.NewSchemaList()
	model.SetSize(40, 10)
	model.SetItems([]list.Item{schema.Item{
		Name:     "department_employee_that_have_a_very_long_name",
		Kind:     "table",
		Table:    "x",
		RowCount: &count,
	}})

	var buf strings.Builder
	schema.ItemDelegate{}.Render(&buf, model, 0, model.Items()[0])
	plain := ansi.Strip(buf.String())

	// One line of exactly the list width; name truncated with an ellipsis
	// so the count badge stays flush against the right edge.
	if strings.Contains(plain, "\n") {
		t.Fatalf("row wrapped across lines: %q", plain)
	}
	if got := lipgloss.Width(plain); got != 40 {
		t.Fatalf("rendered row width = %d, want 40: %q", got, plain)
	}
	if !strings.HasSuffix(plain, " (331.6k)") {
		t.Fatalf("row = %q, want count badge at the right edge", plain)
	}
	if !strings.Contains(plain, "…") || strings.Contains(plain, "long_name") {
		t.Fatalf("row = %q, want the long name truncated with an ellipsis", plain)
	}
}

func TestNewSchemaList_hidesInternalTitle(t *testing.T) {
	// Given

	// When
	model := schema.NewSchemaList()

	// Then
	if model.ShowTitle() {
		t.Fatal("schema list shows an internal title below the pane title")
	}
}
