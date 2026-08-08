package workbench

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

type connectionActionButtons struct {
	selector      *huh.Select[string]
	value         *string
	width, height int
	focused       bool
}

func newConnectionActionButtons(value *string) *connectionActionButtons {
	return &connectionActionButtons{
		selector: huh.NewSelect[string]().
			Key("action").
			Options(
				huh.NewOption(connectionActionTest, connectionActionTest),
				huh.NewOption(connectionActionConnect, connectionActionConnect),
			).
			Value(value).
			Inline(true),
		value: value,
	}
}

func (b *connectionActionButtons) Init() tea.Cmd { return b.selector.Init() }

func (b *connectionActionButtons) Update(message tea.Msg) (huh.Model, tea.Cmd) {
	model, command := b.selector.Update(message)
	b.selector = model.(*huh.Select[string])
	return b, command
}

// connectionActionWidth returns the common rendered width of the Test
// connection and Connect buttons; the shorter label is centered into it.
func connectionActionWidth() int {
	return max(
		lipgloss.Width(connectionActionStyle.Render(connectionActionTest)),
		lipgloss.Width(connectionActionStyle.Render(connectionActionConnect)),
	)
}

// connectionActionRender renders an action button at the common width with
// its label centered.
func connectionActionRender(style lipgloss.Style, label string) string {
	free := connectionActionWidth() - lipgloss.Width(style.Render(label))
	if free <= 0 {
		return style.Render(label)
	}
	return style.Render(strings.Repeat(" ", free/2) + label + strings.Repeat(" ", free-free/2))
}

func (b *connectionActionButtons) View() string {
	// Mirrors huh's own focus convention: the selected option renders with
	// the focus color while the field is focused and the selection color
	// (primary) when blurred.
	selectedStyle := connectionActionSelectedStyle
	if b.focused {
		selectedStyle = connectionActionFocusedStyle
	}
	testStyle, connectStyle := connectionActionStyle, connectionActionStyle
	if *b.value == connectionActionTest {
		testStyle = selectedStyle
	} else {
		connectStyle = selectedStyle
	}
	testButton := connectionActionRender(testStyle, connectionActionTest)
	connectButton := connectionActionRender(connectStyle, connectionActionConnect)
	buttons := lipgloss.JoinHorizontal(lipgloss.Left, testButton, " ", connectButton)
	if b.width > 0 && lipgloss.Width(buttons) > b.width {
		buttons = lipgloss.JoinVertical(lipgloss.Left, testButton, connectButton)
	}
	return "Action\n" + buttons
}

func (b *connectionActionButtons) Blur() tea.Cmd {
	b.focused = false
	return b.selector.Blur()
}
func (b *connectionActionButtons) Focus() tea.Cmd {
	b.focused = true
	return b.selector.Focus()
}
func (b *connectionActionButtons) Error() error { return b.selector.Error() }
func (b *connectionActionButtons) Run() error   { return b.selector.Run() }
func (b *connectionActionButtons) RunAccessible(writer io.Writer, reader io.Reader) error {
	return b.selector.RunAccessible(writer, reader)
}
func (b *connectionActionButtons) Skip() bool              { return false }
func (b *connectionActionButtons) Zoom() bool              { return false }
func (b *connectionActionButtons) KeyBinds() []key.Binding { return b.selector.KeyBinds() }
func (b *connectionActionButtons) WithTheme(theme huh.Theme) huh.Field {
	b.selector.WithTheme(theme)
	return b
}
func (b *connectionActionButtons) WithKeyMap(keyMap *huh.KeyMap) huh.Field {
	b.selector.WithKeyMap(keyMap)
	return b
}
func (b *connectionActionButtons) WithWidth(width int) huh.Field {
	b.width = width
	b.selector.WithWidth(width)
	return b
}
func (b *connectionActionButtons) WithHeight(height int) huh.Field {
	b.height = height
	b.selector.WithHeight(height)
	return b
}
func (b *connectionActionButtons) WithPosition(position huh.FieldPosition) huh.Field {
	b.selector.WithPosition(position)
	return b
}
func (b *connectionActionButtons) GetKey() string { return b.selector.GetKey() }
func (b *connectionActionButtons) GetValue() any  { return b.selector.GetValue() }
