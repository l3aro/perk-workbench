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

type formModeController struct{ mode formMode }

func (c formModeController) editing() bool { return c.mode == formModeInsert }

func (c *formModeController) beginInsert(editor *editor) tea.Cmd {
	c.mode = formModeInsert
	return editor.text.Focus()
}

func (c *formModeController) beginHuh(focus tea.Cmd) tea.Cmd {
	c.mode = formModeInsert
	return focus
}

func (c *formModeController) beginConfirm() { c.mode = formModeConfirm }

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
