package workbench

import (
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// confirmationOption is one selectable action of a confirmation dialog;
// the dialog widget lives in the shared UI contract layer so every form
// (connection, browse, structure) and root overlay share one type.
type confirmationOption = uikit.ConfirmationOption

type confirmationDialog = uikit.ConfirmationDialog

type confirmationLayout = uikit.ConfirmationLayout

func newConfirmationDialog(title, description string, options []confirmationOption) *confirmationDialog {
	return uikit.NewConfirmationDialog(title, description, options)
}

// openQuitDialog opens the quit confirmation dialog, shared by the Ctrl+Q
// keybinding and the header quit button.
func (m Model) openQuitDialog() Model {
	m.overlay.quitDialog = newConfirmationDialog("Quit?", "", []confirmationOption{
		{Label: "Disconnect", Action: "disconnect"},
		{Label: "Quit", Action: "quit"},
		{Label: "Cancel", Action: "cancel"},
	})
	return m
}

func yesNoConfirmation(title, description, action string) *confirmationDialog {
	return uikit.YesNoConfirmation(title, description, action)
}
