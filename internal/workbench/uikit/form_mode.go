package uikit

import (
	tea "charm.land/bubbletea/v2"
)

// FormMode is a modal form's input mode: normal (keys route to the form's
// command bindings), insert (keys route to the focused field), or confirm
// (a save/discard confirmation is open and swallows input).
type FormMode uint8

const (
	FormModeNormal FormMode = iota
	FormModeInsert
	FormModeConfirm
)

// FormMessageRoute tells a form what to do after routing a message through
// the mode controller.
type FormMessageRoute uint8

const (
	FormRouteParent FormMessageRoute = iota
	FormRouteHuh
	FormRouteConsumed
)

// FormButtonRoute tells a form what to do after a key press while the
// Save/Cancel bar may be focused.
type FormButtonRoute uint8

const (
	FormButtonContinue FormButtonRoute = iota // not focused, or key passes through
	FormButtonHandled                         // consumed; run the returned command
	FormButtonReplay                          // consumed; process the synthesized key instead
)

// FormModeController routes keys through the modal form modes shared by
// every editable form (connection, browse, structure, index, foreign key).
// The root owns one controller per session; feature components drive it
// through the exported methods and read Mode for the mode badge.
type FormModeController struct {
	Mode FormMode
	// ButtonsFocused reports whether the Save/Cancel bar is the form's
	// navigation target; ButtonChoice selects which button Enter activates.
	ButtonsFocused bool
	ButtonChoice   int
}

// Editing reports whether the controller is in insert mode.
func (c FormModeController) Editing() bool { return c.Mode == FormModeInsert }

// RouteFormButtons handles keys while the Save/Cancel bar is focused:
// Enter activates the chosen button, h/l switch the choice, k returns to
// the last field. The form's own save/discard bindings still pass through;
// every other key is swallowed so the bar is a real focus target and never
// misfires on the field underneath.
func (c *FormModeController) RouteFormButtons(keyPress tea.KeyPressMsg, keybindings KeyMatcher, focusLast func() tea.Cmd) (FormButtonRoute, tea.KeyPressMsg, tea.Cmd) {
	if !c.ButtonsFocused {
		return FormButtonContinue, tea.KeyPressMsg{}, nil
	}
	switch {
	case keyPress.Key().Code == tea.KeyEnter:
		// Synthesize the button's default key so activation runs the exact
		// same path as the keyboard. ponytail: literal default keys only,
		// matching the save key press.
		if c.ButtonChoice == 0 {
			return FormButtonReplay, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl, Text: "s"}, nil
		}
		return FormButtonReplay, tea.KeyPressMsg{Code: tea.KeyEscape}, nil
	case keyPress.Key().Code == tea.KeyLeft, keyPress.Key().Code == 'h':
		c.ButtonChoice = 0
		return FormButtonHandled, tea.KeyPressMsg{}, nil
	case keyPress.Key().Code == tea.KeyRight, keyPress.Key().Code == 'l':
		c.ButtonChoice = 1
		return FormButtonHandled, tea.KeyPressMsg{}, nil
	case keyPress.Key().Code == tea.KeyUp, keyPress.Key().Code == 'k',
		keyPress.Key().Code == tea.KeyTab && keyPress.Key().Mod&tea.ModShift != 0:
		c.ButtonsFocused = false
		return FormButtonHandled, tea.KeyPressMsg{}, focusLast()
	case keyPress.Key().Code == tea.KeyDown, keyPress.Key().Code == 'j':
		return FormButtonHandled, tea.KeyPressMsg{}, nil
	}
	prepared := PrepareKeyStroke(keyPress)
	if MatchPrepared(keybindings, prepared, "form.save", []Scope{ScopeForm, ScopeView, ScopeGlobal}) ||
		MatchPrepared(keybindings, prepared, "form.discard", []Scope{ScopeForm, ScopeView, ScopeGlobal}) {
		return FormButtonContinue, tea.KeyPressMsg{}, nil
	}
	return FormButtonHandled, tea.KeyPressMsg{}, nil
}

// FocusButtons makes the Save/Cancel bar the form's navigation target.
func (c *FormModeController) FocusButtons() {
	c.ButtonsFocused = true
	c.ButtonChoice = 0
}

// RouteToBar moves focus to the Save/Cancel bar when Tab is pressed on the
// form's last field, so the bar is reachable from insert mode without
// leaving it (vim mode off never needs Escape). Only Tab is intercepted:
// j/k are content keys on insert-mode inputs, and Down is option navigation
// on selects. Returns true when the key was consumed.
func (c *FormModeController) RouteToBar(keyPress tea.KeyPressMsg, atLastField bool, blur func()) bool {
	if c.ButtonsFocused || !atLastField || keyPress.Key().Code != tea.KeyTab {
		return false
	}
	c.FocusButtons()
	blur()
	return true
}

// BeginHuh enters insert mode on a huh form field.
func (c *FormModeController) BeginHuh(focus tea.Cmd) tea.Cmd {
	c.Mode = FormModeInsert
	c.ButtonsFocused = false
	return focus
}

// BeginConfirm moves the controller into the confirmation mode; the
// Save/Cancel bar focus survives so a bar-initiated confirmation that is
// dismissed returns to the bar, whose field underneath stays blurred.
func (c *FormModeController) BeginConfirm() {
	c.Mode = FormModeConfirm
}

// RouteHuh routes a message for a huh-backed form through the modal
// modes. Escape exits insert mode (blurring the field) or confirmation
// mode; normal mode passes the message to the parent.
func (c *FormModeController) RouteHuh(message tea.Msg, blur func()) FormMessageRoute {
	keyPress, ok := message.(tea.KeyPressMsg)
	if ok && keyPress.Key().Code == tea.KeyEscape {
		switch c.Mode {
		case FormModeInsert:
			c.Mode = FormModeNormal
			blur()
			return FormRouteConsumed
		case FormModeConfirm:
			c.Mode = FormModeNormal
			return FormRouteConsumed
		}
	}
	if c.Mode == FormModeNormal {
		return FormRouteParent
	}
	return FormRouteHuh
}
