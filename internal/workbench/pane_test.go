package workbench

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
		if got := abbreviateCount(input); got != want {
			t.Errorf("abbreviateCount(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestNewSchemaList_hidesInternalTitle(t *testing.T) {
	// Given

	// When
	model := newSchemaList()

	// Then
	if model.ShowTitle() {
		t.Fatal("schema list shows an internal title below the pane title")
	}
}
