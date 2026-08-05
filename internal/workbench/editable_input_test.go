package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// Regression: huh v2.0.0's group update delivered PasteMsg to the focused
// field twice, pasting clipboard content twice. The fix (v2.0.3) delivers
// it once.
func TestFormPasteInsertsOnce(t *testing.T) {
	var value string
	form := huh.NewForm(huh.NewGroup(
		newEditableInput(huh.NewInput().Key("value").Value(&value), &value),
	))
	_ = form.Init()

	updated, _ := form.Update(tea.PasteMsg{Content: "clipboard"})
	form = updated.(*huh.Form)

	if value != "clipboard" {
		t.Fatalf("value after one paste = %q, want %q", value, "clipboard")
	}
}
