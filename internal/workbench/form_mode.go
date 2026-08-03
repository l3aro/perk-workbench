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
	case keyPress.Key().Code == tea.KeyUp, keyPress.Key().Code == 'k':
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
