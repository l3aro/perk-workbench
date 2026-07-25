package chrome

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBooleanValue_rendersColoredSymbols(t *testing.T) {
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
			got := BooleanValue(test.value)

			// Then
			if ansi.Strip(got) != test.want || !strings.ContainsRune(got, '\x1b') {
				t.Fatalf("BooleanValue(%t) = %q, want colored %q", test.value, got, test.want)
			}
		})
	}
}

func TestDetailValue_formatsJSONOnlyWhenValid(t *testing.T) {
	// Given
	valid := `{"customer":{"name":"Ada"},"ids":[1,2]}`
	invalid := `{"customer":}`

	// When
	formatted := DetailValue(valid)
	unchanged := DetailValue(invalid)

	// Then
	if want := "{\n  \"customer\": {\n    \"name\": \"Ada\"\n  },\n  \"ids\": [\n    1,\n    2\n  ]\n}"; formatted != want {
		t.Fatalf("DetailValue(valid) = %q, want %q", formatted, want)
	}
	if unchanged != invalid {
		t.Fatalf("DetailValue(invalid) = %q, want %q", unchanged, invalid)
	}
}
