package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// The modal form-mode controller is a shared UI contract: the type and its
// modes/routes live in uikit so every form (connection, browse, structure,
// index, foreign key) drives one controller. The root owns the session
// controller (overlay.formMode) and routes form messages through it.
type formMode = uikit.FormMode

const (
	formModeNormal  formMode = uikit.FormModeNormal
	formModeInsert           = uikit.FormModeInsert
	formModeConfirm          = uikit.FormModeConfirm
)

type formMessageRoute = uikit.FormMessageRoute

const (
	formRouteParent   formMessageRoute = uikit.FormRouteParent
	formRouteHuh                       = uikit.FormRouteHuh
	formRouteConsumed                  = uikit.FormRouteConsumed
)

type formButtonRoute = uikit.FormButtonRoute

const (
	formButtonContinue formButtonRoute = uikit.FormButtonContinue
	formButtonHandled                  = uikit.FormButtonHandled
	formButtonReplay                   = uikit.FormButtonReplay
)

type formModeController = uikit.FormModeController

// formModeRoute routes a message for the query editor (the one form that
// is not huh-backed) through the shared controller's modal modes.
func formModeRoute(c *formModeController, message tea.Msg, editor *editor) formMessageRoute {
	keyPress, ok := message.(tea.KeyPressMsg)
	if ok && keyPress.Key().Code == tea.KeyEscape {
		switch c.Mode {
		case formModeInsert:
			c.Mode = formModeNormal
			editor.text.Blur()
			return formRouteConsumed
		case formModeConfirm:
			c.Mode = formModeNormal
			return formRouteConsumed
		}
	}
	if c.Mode == formModeNormal {
		return formRouteParent
	}
	if c.Mode == formModeConfirm {
		return formRouteConsumed
	}
	return formRouteHuh
}

func isInsertModeKey(keyPress tea.KeyPressMsg) bool {
	return keyPress.Key().Code == 'i'
}

func beginInsert(c *formModeController, editor *editor) tea.Cmd {
	c.Mode = formModeInsert
	c.ButtonsFocused = false
	return editor.text.Focus()
}

// openForm runs a form's init command and, without vim mode, immediately
// enters insert mode on the focused field so typing works without a mode
// switch.
func (m *Model) openForm(command tea.Cmd, focus func() tea.Cmd) tea.Cmd {
	if m.vimMode {
		return command
	}
	return tea.Batch(command, m.overlay.formMode.BeginHuh(focus()))
}

// beginInsertForCurrentFocus transitions the active text input into insert
// mode. Used when vim mode is switched off mid-session, so typing works
// without a mode switch or a re-click. Widgets that are already editing are
// left alone.
func (m *Model) beginInsertForCurrentFocus() tea.Cmd {
	if m.State == stateReady && m.Focus == focusChat && m.chat.component.Visible {
		if m.chat.component.ChatMode != chat.ModeInsert {
			m.chat.component.ChatMode = chat.ModeInsert
			return m.chat.component.Input.Focus()
		}
		return nil
	}
	if m.overlay.formMode.Editing() {
		return nil
	}
	switch {
	case m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Huh != nil && m.connection.component.Form.Confirmation == nil:
		return m.overlay.formMode.BeginHuh(m.connection.component.Form.FocusForm())
	case m.queryEditorActive() && !m.tableFormOpen():
		return beginInsert(m.overlay.formMode, m.queryLog.editor)
	case m.schema.component.Structure.ColumnForm.Active():
		return m.overlay.formMode.BeginHuh(m.schema.component.Structure.ColumnForm.Focus())
	case m.browse.component.DocumentEditor != nil:
		return m.overlay.formMode.BeginHuh(m.browse.component.DocumentEditor.Focus())
	case m.browse.component.Form.Active():
		return m.overlay.formMode.BeginHuh(m.browse.component.Form.Focus())
	case m.browse.component.FilterForm != nil:
		command, _ := m.browse.component.FilterForm.BeginEdit()
		return command
	case m.schema.component.Structure.IndexForm.Active():
		return m.overlay.formMode.BeginHuh(m.schema.component.Structure.IndexForm.Focus())
	case m.schema.component.Structure.ForeignKeyForm.Active():
		return m.overlay.formMode.BeginHuh(m.schema.component.Structure.ForeignKeyForm.Focus())
	case m.tableFormOpen():
		return m.overlay.formMode.BeginHuh(m.schema.component.Structure.TableForm.Focus())
	}
	return nil
}
