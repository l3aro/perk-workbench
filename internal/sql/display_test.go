package sql

import (
	"strings"
	"testing"
)

func TestMaxRunesIs300(t *testing.T) {
	input := strings.Repeat("x", MaxRunes+1)
	if got, want := SanitizeDisplay(input, MaxRunes), strings.Repeat("x", 300)+"…"; got != want {
		t.Fatalf("SanitizeDisplay() = %q, want %q", got, want)
	}
}
