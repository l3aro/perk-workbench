package connection

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// ActionButtons is the Test/Connect huh field at the bottom of the
// connection form.
type ActionButtons struct {
	selector      *huh.Select[string]
	value         *string
	width, height int
	focused       bool
}

// NewActionButtons builds the inline Test/Connect selector bound to value.
func NewActionButtons(value *string) *ActionButtons {
	return &ActionButtons{
		selector: huh.NewSelect[string]().
			Key("action").
			Options(
				huh.NewOption(ActionTest, ActionTest),
				huh.NewOption(ActionConnect, ActionConnect),
			).
			Value(value).
			Inline(true),
		value: value,
	}
}

func (b *ActionButtons) Init() tea.Cmd { return b.selector.Init() }

func (b *ActionButtons) Update(message tea.Msg) (huh.Model, tea.Cmd) {
	model, command := b.selector.Update(message)
	b.selector = model.(*huh.Select[string])
	return b, command
}

// ActionWidth returns the common rendered width of the Test connection
// and Connect buttons; the shorter label is centered into it.
func ActionWidth() int {
	return max(
		lipgloss.Width(uikit.ActionStyle.Render(ActionTest)),
		lipgloss.Width(uikit.ActionStyle.Render(ActionConnect)),
	)
}

// ActionRender renders an action button at the common width with its
// label centered.
func ActionRender(style lipgloss.Style, label string) string {
	free := ActionWidth() - lipgloss.Width(style.Render(label))
	if free <= 0 {
		return style.Render(label)
	}
	return style.Render(strings.Repeat(" ", free/2) + label + strings.Repeat(" ", free-free/2))
}

func (b *ActionButtons) View() string {
	// Mirrors huh's own focus convention: the selected option renders with
	// the focus color while the field is focused and the selection color
	// (primary) when blurred.
	selectedStyle := uikit.ActionSelectedStyle
	if b.focused {
		selectedStyle = uikit.ActionFocusedStyle
	}
	testStyle, connectStyle := uikit.ActionStyle, uikit.ActionStyle
	if *b.value == ActionTest {
		testStyle = selectedStyle
	} else {
		connectStyle = selectedStyle
	}
	testButton := ActionRender(testStyle, ActionTest)
	connectButton := ActionRender(connectStyle, ActionConnect)
	buttons := lipgloss.JoinHorizontal(lipgloss.Left, testButton, " ", connectButton)
	if b.width > 0 && lipgloss.Width(buttons) > b.width {
		buttons = lipgloss.JoinVertical(lipgloss.Left, testButton, connectButton)
	}
	return "Action\n" + buttons
}

func (b *ActionButtons) Blur() tea.Cmd {
	b.focused = false
	return b.selector.Blur()
}
func (b *ActionButtons) Focus() tea.Cmd {
	b.focused = true
	return b.selector.Focus()
}
func (b *ActionButtons) Error() error { return b.selector.Error() }
func (b *ActionButtons) Run() error   { return b.selector.Run() }
func (b *ActionButtons) RunAccessible(writer io.Writer, reader io.Reader) error {
	return b.selector.RunAccessible(writer, reader)
}
func (b *ActionButtons) Skip() bool              { return false }
func (b *ActionButtons) Zoom() bool              { return false }
func (b *ActionButtons) KeyBinds() []key.Binding { return b.selector.KeyBinds() }
func (b *ActionButtons) WithTheme(theme huh.Theme) huh.Field {
	b.selector.WithTheme(theme)
	return b
}
func (b *ActionButtons) WithKeyMap(keyMap *huh.KeyMap) huh.Field {
	b.selector.WithKeyMap(keyMap)
	return b
}
func (b *ActionButtons) WithWidth(width int) huh.Field {
	b.width = width
	b.selector.WithWidth(width)
	return b
}
func (b *ActionButtons) WithHeight(height int) huh.Field {
	b.height = height
	b.selector.WithHeight(height)
	return b
}
func (b *ActionButtons) WithPosition(position huh.FieldPosition) huh.Field {
	b.selector.WithPosition(position)
	return b
}
func (b *ActionButtons) GetKey() string { return b.selector.GetKey() }
func (b *ActionButtons) GetValue() any  { return b.selector.GetValue() }
