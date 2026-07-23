package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEditor_huhTextBindsSQLValue(t *testing.T) {
	// Given
	editor := newEditor()
	editor.text.Focus()

	// When
	_ = editor.update(tea.KeyPressMsg{Code: 'S', Text: "S"})

	// Then
	if got := editor.value; got != "S" {
		t.Fatalf("Huh Text value = %q, want S", got)
	}
}
