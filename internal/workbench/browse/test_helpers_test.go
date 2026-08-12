package browse

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// keyMatcherFunc adapts a function to uikit.KeyMatcher for tests.
type keyMatcherFunc func(tea.KeyPressMsg, uikit.CommandID, []uikit.Scope) bool

func (f keyMatcherFunc) Match(msg tea.KeyPressMsg, id uikit.CommandID, scopes []uikit.Scope) bool {
	return f(msg, id, scopes)
}

// testKeybindings matches the named command for any key press, mirroring
// the root's default binding registry for the commands under test.
func testKeybindings() uikit.KeyMatcher {
	return keyMatcherFunc(func(msg tea.KeyPressMsg, id uikit.CommandID, scopes []uikit.Scope) bool {
		stroke := msg.Keystroke()
		switch id {
		case "browse_filter.apply":
			return stroke == "f5" || stroke == "ctrl+s"
		case "browse.sort":
			return stroke == "s"
		case "browse.reset":
			return stroke == "r"
		case "cell.yank":
			return stroke == "y"
		case "browse.next_page":
			return stroke == "n"
		case "browse.prev_page":
			return stroke == "p"
		case "form.edit":
			return stroke == "i" || stroke == "enter"
		case "form.save":
			return stroke == "ctrl+s" || stroke == "f5" || stroke == "ctrl+enter"
		case "form.discard":
			return stroke == "esc"
		case "form.field_next":
			return stroke == "j" || stroke == "down"
		case "form.field_prev":
			return stroke == "k" || stroke == "up"
		case "browse_form.set_null":
			return stroke == "n"
		case "browse_form.set_default":
			return stroke == "N"
		case "browse_form.field_top":
			return stroke == "g"
		case "browse_form.field_bottom":
			return stroke == "G"
		}
		return false
	})
}
