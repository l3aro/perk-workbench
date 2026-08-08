package sql

import (
	"strings"
	"testing"
)

func TestMaxRunesIs50(t *testing.T) {
	input := strings.Repeat("x", MaxRunes+1)
	if got, want := SanitizeDisplay(input, MaxRunes), strings.Repeat("x", MaxRunes)+"…"; got != want {
		t.Fatalf("SanitizeDisplay() = %q, want %q", got, want)
	}
}
