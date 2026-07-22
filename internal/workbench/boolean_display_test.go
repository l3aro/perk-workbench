package workbench

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBooleanValue_renders_colored_nerd_font_symbols(t *testing.T) {
	// Given
	tests := []struct {
		name  string
		value bool
		want  string
	}{
		{name: "true", value: true, want: iconBooleanTrue},
		{name: "false", value: false, want: iconBooleanFalse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			got := booleanValue(test.value)

			// Then
			if ansi.Strip(got) != test.want || !strings.ContainsRune(got, '\x1b') {
				t.Fatalf("booleanValue(%t) = %q, want colored %q", test.value, got, test.want)
			}
		})
	}
}
