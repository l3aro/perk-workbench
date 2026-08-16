package querylog

import (
	"strings"
	"testing"
)

// TestAdvisoryBlock_rendersLabeledGuidanceOnlyWhenPresent proves the
// detail overlay advisory block: each advisory renders on its own
// visibly labeled line (Hint:/Try:), absent advisories render nothing,
// and the guidance never merges into the message.
func TestAdvisoryBlock_rendersLabeledGuidanceOnlyWhenPresent(t *testing.T) {
	for _, test := range []struct {
		name    string
		entry   Entry
		want    []string
		missing []string
	}{
		{
			name:    "no advisories",
			entry:   Entry{Message: "boom"},
			missing: []string{"Hint:", "Try:"},
		},
		{
			name:    "hint only",
			entry:   Entry{Message: "boom", Hint: "GET accepts strings, but user:1 is a hash"},
			want:    []string{"  Hint:     GET accepts strings, but user:1 is a hash"},
			missing: []string{"Try:"},
		},
		{
			name:    "suggested statement only",
			entry:   Entry{Message: "boom", SuggestedStatement: "HGETALL user:1"},
			want:    []string{"  Try:      HGETALL user:1"},
			missing: []string{"Hint:"},
		},
		{
			name: "both",
			entry: Entry{
				Message:            "redis: WRONGTYPE Operation against a key holding the wrong kind of value",
				Hint:               "GET accepts strings, but user:1 is a hash",
				SuggestedStatement: "HGETALL user:1",
			},
			want: []string{
				"  Hint:     GET accepts strings, but user:1 is a hash",
				"  Try:      HGETALL user:1",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := advisoryBlock(test.entry, 80)
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Fatalf("advisoryBlock = %q, want it to contain %q", got, want)
				}
			}
			for _, missing := range test.missing {
				if strings.Contains(got, missing) {
					t.Fatalf("advisoryBlock = %q, must not contain %q", got, missing)
				}
			}
			if strings.Contains(got, test.entry.Message) && test.entry.Message != "" && test.entry.Message != "boom" {
				t.Fatalf("advisoryBlock = %q must not repeat the raw error message", got)
			}
		})
	}
}

// TestAdvisoryBlock_wrapsLongGuidance proves long advisory text wraps at
// the same width budget as the message instead of overflowing the
// dialog.
func TestAdvisoryBlock_wrapsLongGuidance(t *testing.T) {
	block := advisoryBlock(Entry{Hint: strings.Repeat("word ", 60), SuggestedStatement: strings.Repeat("token ", 60)}, 40)
	lines := strings.Split(block, "\n")
	if len(lines) < 3 {
		t.Fatalf("long guidance did not wrap: %q", block)
	}
	for _, label := range []string{"Hint:", "Try:"} {
		if !strings.Contains(block, label) {
			t.Fatalf("wrapped block = %q, missing %q", block, label)
		}
	}
}
