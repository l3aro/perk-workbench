package uikit

import (
	"strings"
	"testing"
)

func TestSanitizeCellViewer_strips_ansi_preserves_newlines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "preserves plain text", input: "hello world", want: "hello world"},
		{name: "strips ANSI escape", input: "\x1b[31mred\x1b[0m", want: "red"},
		{name: "strips OSC sequence", input: "a\x1b]0;title\x07b", want: "ab"},
		{name: "preserves newlines", input: "line1\nline2", want: "line1\nline2"},
		{name: "strips carriage return", input: "a\rb", want: "ab"},
		{name: "preserves tabs", input: "a\tb", want: "a\tb"},
		{name: "strips BEL", input: "a\x07b", want: "ab"},
		{name: "preserves full length", input: "abc " + strings.Repeat("x", 500), want: "abc " + strings.Repeat("x", 500)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeCellViewer(test.input)
			if got != test.want {
				t.Fatalf("sanitizeCellViewer(%q) = %q (len %d), want %q (len %d)",
					test.input, got, len(got), test.want, len(test.want))
			}
		})
	}
}
