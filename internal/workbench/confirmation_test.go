package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestConfirmationDialog_clickingOptionCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Delete row?", "DELETE FROM projects", []confirmationOption{
		{Label: "Yes, delete", Action: "delete"},
		{Label: "Cancel", Action: "cancel"},
	})

	// When
	layout := dialog.Layout(40, 20)
	completed, action := dialog.Update(tea.MouseClickMsg{X: layout.ButtonX[0], Y: layout.ButtonY[0], Button: tea.MouseLeft}, 40, 20)

	// Then
	if !completed || action != "delete" {
		t.Fatalf("click result = completed:%t action:%q, want true/delete", completed, action)
	}
}

func TestConfirmationDialog_yAndEscapeConfirmAndCancel(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Save changes?", "", []confirmationOption{
		{Label: "Yes", Action: "save"},
		{Label: "No", Action: "cancel"},
	})

	// When
	confirmed, action := dialog.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}, 40, 20)

	// Then
	if !confirmed || action != "save" {
		t.Fatalf("y result = completed:%t action:%q, want true/save", confirmed, action)
	}

	// Given
	dialog = newConfirmationDialog("Save changes?", "", []confirmationOption{
		{Label: "Yes", Action: "save"},
		{Label: "No", Action: "cancel"},
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
		{Label: "Yes", Action: "run"},
		{Label: "No", Action: "cancel"},
	})
	layout := dialog.Layout(100, 5)

	// When
	completed, action := dialog.Update(tea.MouseClickMsg{X: layout.ButtonX[1], Y: layout.ButtonY[1], Button: tea.MouseLeft}, 100, 5)

	// Then
	if layout.ButtonY[0] >= 5 || layout.ButtonY[1] >= 5 || !completed || action != "cancel" {
		t.Fatalf("short layout = buttons:%v completed:%t action:%q, want visible buttons and cancel", layout.ButtonY, completed, action)
	}
}

func TestConfirmationDialog_helpTextFitsInsideCard(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard row changes?", "", []confirmationOption{
		{Label: "Yes", Action: "discard"},
		{Label: "No", Action: "cancel"},
	})

	// When
	layout := dialog.Layout(80, 20)

	// Then
	if !layout.ShowHelp || layout.ContentWidth < ansi.StringWidth("←/→ toggle • enter select") {
		t.Fatalf("help layout = show:%t width:%d, want visible help with its full width", layout.ShowHelp, layout.ContentWidth)
	}
}

func TestConfirmationDialog_buttonsAreCenteredAndEqualWidth(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard row changes?", "", []confirmationOption{
		{Label: "Yes", Action: "discard"},
		{Label: "No", Action: "cancel"},
	})

	// When
	layout := dialog.Layout(80, 20)

	// Then
	if layout.ButtonWidth[0] != layout.ButtonWidth[1] {
		t.Fatalf("button widths = %v, want equal widths", layout.ButtonWidth)
	}
	groupCenter := layout.ButtonX[0] + layout.ButtonX[1] + layout.ButtonWidth[1]
	cardCenter := (layout.X - 2) + (layout.X + layout.ContentWidth + 6)
	if groupCenter < cardCenter-1 || groupCenter > cardCenter+1 {
		t.Fatalf("button center = %d, card center = %d, want centered", groupCenter, cardCenter)
	}
}

func TestConfirmationDialog_mouseReleaseCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard changes?", "", []confirmationOption{
		{Label: "Yes", Action: "discard"},
		{Label: "No", Action: "cancel"},
	})
	layout := dialog.Layout(40, 20)

	// When
	completed, action := dialog.Update(tea.MouseReleaseMsg{X: layout.ButtonX[0], Y: layout.ButtonY[0], Button: tea.MouseLeft}, 40, 20)

	// Then
	if !completed || action != "discard" {
		t.Fatalf("mouse release = completed:%t action:%q, want true/discard", completed, action)
	}
}

func TestConfirmationDialog_buttonlessMouseReleaseCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard changes?", "", []confirmationOption{
		{Label: "Yes", Action: "discard"},
		{Label: "No", Action: "cancel"},
	})
	layout := dialog.Layout(40, 20)

	// When
	completed, action := dialog.Update(tea.MouseReleaseMsg{X: layout.ButtonX[0], Y: layout.ButtonY[0], Button: tea.MouseNone}, 40, 20)

	// Then
	if !completed || action != "discard" {
		t.Fatalf("buttonless mouse release = completed:%t action:%q, want true/discard", completed, action)
	}
}

func TestConfirmationDialog_mouseMotionCompletesWithItsAction(t *testing.T) {
	// Given
	dialog := newConfirmationDialog("Discard changes?", "", []confirmationOption{
		{Label: "Yes", Action: "discard"},
		{Label: "No", Action: "cancel"},
	})
	layout := dialog.Layout(40, 20)

	// When
	completed, action := dialog.Update(tea.MouseMotionMsg{X: layout.ButtonX[0], Y: layout.ButtonY[0], Button: tea.MouseLeft}, 40, 20)

	// Then
	if !completed || action != "discard" {
		t.Fatalf("mouse motion = completed:%t action:%q, want true/discard", completed, action)
	}
}
