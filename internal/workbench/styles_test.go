package workbench

import (
	"strings"
	"testing"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func TestCellTextCapsAtSharedRuneLimit(t *testing.T) {
	input := strings.Repeat("x", sharedsql.MaxRunes+1)
	if got, want := cellText(input), strings.Repeat("x", 300)+"…"; got != want {
		t.Fatalf("cellText() = %q, want %q", got, want)
	}
}
