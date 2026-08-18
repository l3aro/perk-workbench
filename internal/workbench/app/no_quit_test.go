package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestNoQuit_ctrlCDoesNotQuit locks Ctrl+C in a locked session, then
// unlocks and proves the same key quits again.
func TestNoQuit_ctrlCDoesNotQuit(t *testing.T) {
	model := readyModel(t)
	model.SetNoQuit(true)

	updated, command := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)
	if model.State != stateReady {
		t.Fatalf("state = %v, want stateReady", model.State)
	}
	if commandQuits(command) {
		t.Fatal("Ctrl+C quit in a locked session")
	}
	if model.overlay.quitDialog != nil {
		t.Fatal("Ctrl+C opened the quit dialog in a locked session")
	}

	model.SetNoQuit(false)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)
	if !commandQuits(command) {
		t.Fatal("Ctrl+C did not quit after unlocking")
	}
}

// TestNoQuit_ctrlQDoesNotOpenDialog locks Ctrl+Q, then unlocks and proves
// the same key opens the quit dialog again.
func TestNoQuit_ctrlQDoesNotOpenDialog(t *testing.T) {
	model := readyModel(t)
	model.SetNoQuit(true)

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	model = updated.(Model)
	if model.overlay.quitDialog != nil {
		t.Fatal("Ctrl+Q opened the quit dialog in a locked session")
	}

	model.SetNoQuit(false)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	model = updated.(Model)
	if model.overlay.quitDialog == nil {
		t.Fatal("Ctrl+Q did not open the quit dialog after unlocking")
	}
}

// TestNoQuit_paletteHidesQuit proves a locked session never lists the
// quit command in the palette and never quits when asked to dispatch it.
func TestNoQuit_paletteHidesQuit(t *testing.T) {
	model := readyModel(t)
	model.SetNoQuit(true)

	for _, item := range model.overlay.commandPalette.items {
		if item.id == "app.quit" {
			t.Fatal("locked session palette still lists app.quit")
		}
	}

	updated, command := model.handlePaletteCommand("app.quit")
	if commandQuits(command) {
		t.Fatal("palette app.quit quit in a locked session")
	}
	if got := updated.(Model).State; got != stateReady {
		t.Fatalf("state = %v, want stateReady", got)
	}
}

// TestNoQuit_headerHidesQuitButton proves the locked header keeps the
// palette button but drops the quit button.
func TestNoQuit_headerHidesQuitButton(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.SetNoQuit(true)

	header := model.headerView()
	if strings.Contains(header, headerQuitButtonLabel) {
		t.Fatal("locked header still shows the quit button")
	}
	if !strings.Contains(header, headerButtonLabel) {
		t.Fatal("locked header lost the palette button")
	}
}

// TestNoQuit_headerClicksCannotOpenQuitDialog proves the locked header's
// rightmost slot opens the palette at most, and the old two-button slots
// are inert: no click can reach the quit dialog.
func TestNoQuit_headerClicksCannotOpenQuitDialog(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.SetNoQuit(true)

	// In a locked header the palette button sits at the right edge inside
	// the border margin; click inside it and expect the palette.
	width := ansi.StringWidth(headerButtonStyle.Render(headerButtonLabel))
	quitX := model.layout.width - headerRightMargin - width

	updated, _ := model.Update(tea.MouseClickMsg{X: quitX + 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.overlay.quitDialog != nil {
		t.Fatal("locked header click opened the quit dialog")
	}
	if !model.overlay.commandPalette.visible {
		t.Fatal("locked header rightmost click did not open the palette")
	}

	// The old palette slot (left of the rightmost button, separated by the
	// button gap) is inert in a locked header.
	model.overlay.commandPalette.visible = false
	oldPaletteX := quitX - headerButtonGap - width
	updated, _ = model.Update(tea.MouseClickMsg{X: oldPaletteX + 1, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.overlay.quitDialog != nil {
		t.Fatal("locked header old-slot click opened the quit dialog")
	}
	if model.overlay.commandPalette.visible {
		t.Fatal("locked header old-slot click opened the palette")
	}
}

// TestNoQuit_footerOmitsQuitHint proves the locked footer never advertises
// a quit key, on the ready screen or the connection screen.
func TestNoQuit_footerOmitsQuitHint(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	model.SetNoQuit(true)
	if got := model.footer(); strings.Contains(got, "quit") {
		t.Fatalf("locked ready footer = %q, want no quit hint", got)
	}

	connectionModel := resizeModel(New("", context.Background(), testOpen, false), 100, 24)
	connectionModel.SetNoQuit(true)
	if got := connectionModel.footer(); strings.Contains(got, "quit") {
		t.Fatalf("locked connection footer = %q, want no quit hint", got)
	}
}

// TestNoQuit_quitDialogConfirmedDoesNotQuit is the defensive backstop: a
// quit dialog that somehow opened in a locked session cannot quit.
func TestNoQuit_quitDialogConfirmedDoesNotQuit(t *testing.T) {
	model := readyModel(t)
	model.SetNoQuit(true)
	model = model.openQuitDialog()
	if model.overlay.quitDialog == nil {
		t.Fatal("openQuitDialog did not open a dialog")
	}

	// Select the "Quit" option (option index 1) and confirm.
	model.overlay.quitDialog.Selected = 1
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if commandQuits(command) {
		t.Fatal("locked session quit dialog confirmed a quit")
	}
	if model.overlay.quitDialog != nil {
		t.Fatal("locked session quit dialog was not consumed")
	}
}
