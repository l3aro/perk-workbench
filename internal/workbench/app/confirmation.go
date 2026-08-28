package app

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
// keybinding and the header quit button. Each option carries a shortcut key
// (d/q/c) so the dialog completes on a single press.
func (m Model) openQuitDialog() Model {
	m.overlay.quitDialog = newConfirmationDialog("Quit?", "", []confirmationOption{
		{Label: "Disconnect", Action: "disconnect", Key: 'd'},
		{Label: "Quit", Action: "quit", Key: 'q'},
		{Label: "Cancel", Action: "cancel", Key: 'c'},
	})
	return m
}

func yesNoConfirmation(title, description, action string) *confirmationDialog {
	return uikit.YesNoConfirmation(title, description, action)
}
