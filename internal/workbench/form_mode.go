package workbench

import tea "charm.land/bubbletea/v2"

type formMode uint8

const (
	formModeNormal formMode = iota
	formModeInsert
	formModeConfirm
)

type formMessageRoute uint8

const (
	formRouteParent formMessageRoute = iota
	formRouteHuh
	formRouteConsumed
)

type formModeController struct {
	mode formMode
	// buttonsFocused reports whether the Save/Cancel bar is the form's
	// navigation target; buttonChoice selects which button Enter activates.
	buttonsFocused bool
	buttonChoice   int
}

func (c formModeController) editing() bool { return c.mode == formModeInsert }

// formButtonRoute tells a form what to do after a key press while the
// Save/Cancel bar may be focused.
type formButtonRoute uint8

const (
	formButtonContinue formButtonRoute = iota // not focused, or key passes through
	formButtonHandled                         // consumed; run the returned command
	formButtonReplay                          // consumed; process the synthesized key instead
)

// routeFormButtons handles keys while the Save/Cancel bar is focused: Enter
// activates the chosen button, h/l switch the choice, k returns to the last
// field. The form's own save/discard bindings still pass through; every
// other key is swallowed so the bar is a real focus target and never
// misfires on the field underneath.
func (c *formModeController) routeFormButtons(keyPress tea.KeyPressMsg, keybindings Keybindings, focusLast func() tea.Cmd) (formButtonRoute, tea.KeyPressMsg, tea.Cmd) {
	if !c.buttonsFocused {
		return formButtonContinue, tea.KeyPressMsg{}, nil
	}
	switch {
	case keyPress.Key().Code == tea.KeyEnter:
		// Synthesize the button's default key so activation runs the exact
		// same path as the keyboard. ponytail: literal default keys only,
		// matching formSaveKeyPress.
		if c.buttonChoice == 0 {
			return formButtonReplay, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl, Text: "s"}, nil
		}
		return formButtonReplay, tea.KeyPressMsg{Code: tea.KeyEscape}, nil
	case keyPress.Key().Code == tea.KeyLeft, keyPress.Key().Code == 'h':
		c.buttonChoice = 0
		return formButtonHandled, tea.KeyPressMsg{}, nil
	case keyPress.Key().Code == tea.KeyRight, keyPress.Key().Code == 'l':
		c.buttonChoice = 1
		return formButtonHandled, tea.KeyPressMsg{}, nil
	case keyPress.Key().Code == tea.KeyUp, keyPress.Key().Code == 'k',
		keyPress.Key().Code == tea.KeyTab && keyPress.Key().Mod&tea.ModShift != 0:
		c.buttonsFocused = false
		return formButtonHandled, tea.KeyPressMsg{}, focusLast()
	case keyPress.Key().Code == tea.KeyDown, keyPress.Key().Code == 'j':
		return formButtonHandled, tea.KeyPressMsg{}, nil
	}
	if keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}) ||
		keybindings.Match(keyPress, "form.discard", []scope{scopeForm, scopeView, scopeGlobal}) {
		return formButtonContinue, tea.KeyPressMsg{}, nil
	}
	return formButtonHandled, tea.KeyPressMsg{}, nil
}

func (c *formModeController) focusButtons() {
	c.buttonsFocused = true
	c.buttonChoice = 0
}

// routeToBar moves focus to the Save/Cancel bar when Tab is pressed on the
// form's last field, so the bar is reachable from insert mode without
// leaving it (vim mode off never needs Escape). Only Tab is intercepted:
// j/k are content keys on insert-mode inputs, and Down is option navigation
// on selects. Returns true when the key was consumed.
func (c *formModeController) routeToBar(keyPress tea.KeyPressMsg, atLastField bool, blur func()) bool {
	if c.buttonsFocused || !atLastField || keyPress.Key().Code != tea.KeyTab {
		return false
	}
	c.focusButtons()
	blur()
	return true
}

// openForm runs a form's init command and, without vim mode, immediately
// enters insert mode on the focused field so typing works without a mode
// switch.
func (m *Model) openForm(command tea.Cmd, focus func() tea.Cmd) tea.Cmd {
	if m.vimMode {
		return command
	}
	return tea.Batch(command, m.formMode.beginHuh(focus()))
}

// beginInsertForCurrentFocus transitions the active text input into insert
// mode. Used when vim mode is switched off mid-session, so typing works
// without a mode switch or a re-click. Widgets that are already editing are
// left alone.
func (m *Model) beginInsertForCurrentFocus() tea.Cmd {
	if m.State == stateReady && m.Focus == focusChat && m.chat.visible {
		if m.chat.chatMode != formModeInsert {
			m.chat.chatMode = formModeInsert
			return m.chat.input.Focus()
		}
		return nil
	}
	if m.formMode.editing() {
		return nil
	}
	switch {
	case m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.form != nil && m.connection.confirmation == nil:
		return m.formMode.beginHuh(m.connection.focusForm())
	case m.sqlEditorActive() && !m.tableFormOpen():
		return m.formMode.beginInsert(m.editor)
	case m.columnForm.active():
		return m.formMode.beginHuh(m.columnForm.focus())
	case m.documentEditor != nil:
		return m.formMode.beginHuh(m.documentEditor.focus())
	case m.browseForm.active():
		return m.formMode.beginHuh(m.browseForm.focus())
	case m.browseFilterForm != nil:
		command, _ := m.browseFilterForm.beginEdit()
		return command
	case m.indexForm.active():
		return m.formMode.beginHuh(m.indexForm.focus())
	case m.foreignKeyForm.active():
		return m.formMode.beginHuh(m.foreignKeyForm.focus())
	case m.tableFormOpen():
		return m.formMode.beginHuh(m.tableForm.focus())
	}
	return nil
}

func isInsertModeKey(keyPress tea.KeyPressMsg) bool {
	return keyPress.Key().Code == 'i'
}

func (c *formModeController) beginInsert(editor *editor) tea.Cmd {
	c.mode = formModeInsert
	c.buttonsFocused = false
	return editor.text.Focus()
}

func (c *formModeController) beginHuh(focus tea.Cmd) tea.Cmd {
	c.mode = formModeInsert
	c.buttonsFocused = false
	return focus
}

func (c *formModeController) beginConfirm() {
	c.mode = formModeConfirm
	// Keep buttonsFocused: a bar-initiated confirmation that is dismissed
	// returns to the bar, whose field underneath stays blurred.
}

func (c *formModeController) route(message tea.Msg, editor *editor) formMessageRoute {
	keyPress, ok := message.(tea.KeyPressMsg)
	if ok && keyPress.Key().Code == tea.KeyEscape {
		switch c.mode {
		case formModeInsert:
			c.mode = formModeNormal
			editor.text.Blur()
			return formRouteConsumed
		case formModeConfirm:
			c.mode = formModeNormal
			return formRouteConsumed
		}
	}
	if c.mode == formModeNormal {
		return formRouteParent
	}
	if c.mode == formModeConfirm {
		return formRouteConsumed
	}
	return formRouteHuh
}

func (c *formModeController) routeHuh(message tea.Msg, blur func()) formMessageRoute {
	keyPress, ok := message.(tea.KeyPressMsg)
	if ok && keyPress.Key().Code == tea.KeyEscape {
		switch c.mode {
		case formModeInsert:
			c.mode = formModeNormal
			blur()
			return formRouteConsumed
		case formModeConfirm:
			c.mode = formModeNormal
			return formRouteConsumed
		}
	}
	if c.mode == formModeNormal {
		return formRouteParent
	}
	return formRouteHuh
}
