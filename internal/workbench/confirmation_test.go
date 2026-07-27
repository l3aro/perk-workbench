package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestConfirmationDialog_clickingOptionCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Delete row?", "DELETE FROM projects", []confirmationOption{
		{label: "Yes, delete", action: "delete"},
		{label: "Cancel", action: "cancel"},
	})

	// When
	layout := dialog.layout(40, 20)
	completed, action := dialog.Update(tea.MouseClickMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseLeft}, 40, 20)

	// Then
	if !completed || action != "delete" {
		t.Fatalf("click result = completed:%t action:%q, want true/delete", completed, action)
	}
}

func TestConfirmationDialog_yAndEscapeConfirmAndCancel(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Save changes?", "", []confirmationOption{
		{label: "Yes", action: "save"},
		{label: "No", action: "cancel"},
	})

	// When
	confirmed, action := dialog.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}, 40, 20)

	// Then
	if !confirmed || action != "save" {
		t.Fatalf("y result = completed:%t action:%q, want true/save", confirmed, action)
	}

	// Given
	dialog = newConfirmationDialog("Save changes?", "", []confirmationOption{
		{label: "Yes", action: "save"},
		{label: "No", action: "cancel"},
	})

	// When
	canceled, action := dialog.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, 40, 20)

	// Then
	if !canceled || action != "cancel" {
		t.Fatalf("escape result = completed:%t action:%q, want true/cancel", canceled, action)
	}
}

func TestConfirmationDialog_shortTerminalKeepsEveryOptionClickable(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Run destructive SQL?", "CREATE TABLE projects (id INTEGER)", []confirmationOption{
		{label: "Yes", action: "run"},
		{label: "No", action: "cancel"},
	})
	layout := dialog.layout(100, 5)

	// When
	completed, action := dialog.Update(tea.MouseClickMsg{X: layout.buttonX[1], Y: layout.buttonY[1], Button: tea.MouseLeft}, 100, 5)

	// Then
	if layout.buttonY[0] >= 5 || layout.buttonY[1] >= 5 || !completed || action != "cancel" {
		t.Fatalf("short layout = buttons:%v completed:%t action:%q, want visible buttons and cancel", layout.buttonY, completed, action)
	}
}

func TestConfirmationDialog_helpTextFitsInsideCard(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard row changes?", "", []confirmationOption{
		{label: "Yes", action: "discard"},
		{label: "No", action: "cancel"},
	})

	// When
	layout := dialog.layout(80, 20)

	// Then
	if !layout.showHelp || layout.contentWidth < ansi.StringWidth("←/→ toggle • enter select") {
		t.Fatalf("help layout = show:%t width:%d, want visible help with its full width", layout.showHelp, layout.contentWidth)
	}
}

func TestConfirmationDialog_buttonsAreCenteredAndEqualWidth(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard row changes?", "", []confirmationOption{
		{label: "Yes", action: "discard"},
		{label: "No", action: "cancel"},
	})

	// When
	layout := dialog.layout(80, 20)

	// Then
	if layout.buttonWidth[0] != layout.buttonWidth[1] {
		t.Fatalf("button widths = %v, want equal widths", layout.buttonWidth)
	}
	groupCenter := layout.buttonX[0] + layout.buttonX[1] + layout.buttonWidth[1]
	cardCenter := (layout.x - 2) + (layout.x + layout.contentWidth + 6)
	if groupCenter < cardCenter-1 || groupCenter > cardCenter+1 {
		t.Fatalf("button center = %d, card center = %d, want centered", groupCenter, cardCenter)
	}
}

func TestConfirmationDialog_mouseReleaseCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard changes?", "", []confirmationOption{
		{label: "Yes", action: "discard"},
		{label: "No", action: "cancel"},
	})
	layout := dialog.layout(40, 20)

	// When
	completed, action := dialog.Update(tea.MouseReleaseMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseLeft}, 40, 20)

	// Then
	if !completed || action != "discard" {
		t.Fatalf("mouse release = completed:%t action:%q, want true/discard", completed, action)
	}
}

func TestConfirmationDialog_buttonlessMouseReleaseCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard changes?", "", []confirmationOption{
		{label: "Yes", action: "discard"},
		{label: "No", action: "cancel"},
	})
	layout := dialog.layout(40, 20)

	// When
	completed, action := dialog.Update(tea.MouseReleaseMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseNone}, 40, 20)

	// Then
	if !completed || action != "discard" {
		t.Fatalf("buttonless mouse release = completed:%t action:%q, want true/discard", completed, action)
	}
}

func TestConfirmationDialog_mouseMotionCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard changes?", "", []confirmationOption{
		{label: "Yes", action: "discard"},
		{label: "No", action: "cancel"},
	})
	layout := dialog.layout(40, 20)

	// When
	completed, action := dialog.Update(tea.MouseMotionMsg{X: layout.buttonX[0], Y: layout.buttonY[0], Button: tea.MouseLeft}, 40, 20)

	// Then
	if !completed || action != "discard" {
		t.Fatalf("mouse motion = completed:%t action:%q, want true/discard", completed, action)
	}
}
