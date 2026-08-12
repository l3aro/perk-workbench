package uikit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

// ConfirmationOption is one selectable action of a confirmation dialog.
type ConfirmationOption struct {
	Label  string
	Action string
}

// ConfirmationDialog is a modal yes/no-style prompt owned by the root
// overlay layer and embedded by the feature forms (connection, browse,
// structure) that confirm before acting.
type ConfirmationDialog struct {
	Title, Description string
	Options            []ConfirmationOption
	Selected           int
}

// ConfirmationLayout is the computed dialog geometry, shared by the
// renderer and the click hit-test.
type ConfirmationLayout struct {
	X, Y         int
	ButtonX      []int
	ButtonY      []int
	ButtonWidth  []int
	Description  []string
	ContentWidth int
	ShowHelp     bool
}

// NewConfirmationDialog builds a dialog with the given options.
func NewConfirmationDialog(title, description string, options []ConfirmationOption) *ConfirmationDialog {
	return &ConfirmationDialog{Title: title, Description: description, Options: options}
}

// YesNoConfirmation builds a two-option dialog: Yes runs action, No cancels.
func YesNoConfirmation(title, description, action string) *ConfirmationDialog {
	return NewConfirmationDialog(title, description, []ConfirmationOption{
		{Label: "Yes", Action: action},
		{Label: "No", Action: "cancel"},
	})
}

func (d ConfirmationDialog) Content(width int) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(d.Title)
	if description := strings.TrimSpace(d.Description); description != "" {
		b.WriteString("\n\n  ")
		b.WriteString(ansi.Wordwrap(SafeText(description), max(min(width-8, 72)-2, 1), "\n  "))
	}
	b.WriteString("\n\n")
	for index, option := range d.Options {
		if index == d.Selected {
			b.WriteString("  > ")
		} else {
			b.WriteString("    ")
		}
		b.WriteString(option.Label)
		if index+1 < len(d.Options) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (d ConfirmationDialog) Layout(width, height int) ConfirmationLayout {
	layout := ConfirmationLayout{}
	if description := strings.TrimSpace(d.Description); description != "" {
		layout.Description = strings.Split(ansi.Wordwrap(SafeText(description), max(min(width-10, 72), 1), "\n"), "\n")
	}
	buttonLabelWidth := 0
	for _, option := range d.Options {
		buttonLabelWidth = max(buttonLabelWidth, ansi.StringWidth(option.Label))
	}
	buttonRowWidth := 0
	for range d.Options {
		buttonWidth := buttonLabelWidth + 4
		layout.ButtonWidth = append(layout.ButtonWidth, buttonWidth)
		buttonRowWidth += buttonWidth
	}
	buttonRowWidth += max(len(d.Options)-1, 0) * 2
	stackButtons := buttonRowWidth > max(width-5, 1)
	buttonHeight := 1
	if stackButtons {
		buttonHeight = len(d.Options)
	}
	if height < len(layout.Description)+buttonHeight+5 {
		layout.Description = nil
	}
	layout.ShowHelp = height >= len(layout.Description)+buttonHeight+6
	contentWidth := max(ansi.StringWidth(d.Title), buttonRowWidth)
	if layout.ShowHelp {
		contentWidth = max(contentWidth, ansi.StringWidth("←/→ toggle • enter select"))
	}
	if stackButtons {
		contentWidth = ansi.StringWidth(d.Title)
		for _, buttonWidth := range layout.ButtonWidth {
			contentWidth = max(contentWidth, buttonWidth)
		}
	}
	for _, line := range layout.Description {
		contentWidth = max(contentWidth, ansi.StringWidth(line))
	}
	layout.ContentWidth = contentWidth
	layout.X = max(0, (width-contentWidth-5)/2)
	contentHeight := len(layout.Description) + buttonHeight + 3
	if layout.ShowHelp {
		contentHeight += 3
	}
	layout.Y = max(0, (height-contentHeight)/2)
	buttonY := layout.Y + len(layout.Description) + 2
	if stackButtons {
		for index := range d.Options {
			buttonX := max(0, min(layout.X+4, max(width-layout.ButtonWidth[index], 0)))
			layout.ButtonX = append(layout.ButtonX, buttonX)
			layout.ButtonWidth[index] = min(layout.ButtonWidth[index], max(width-buttonX, 1))
			layout.ButtonY = append(layout.ButtonY, buttonY+index)
		}
		return layout
	}
	buttonX := layout.X + 2 + max((contentWidth-buttonRowWidth)/2, 0)
	for _, buttonWidth := range layout.ButtonWidth {
		layout.ButtonX = append(layout.ButtonX, buttonX)
		layout.ButtonY = append(layout.ButtonY, buttonY)
		buttonX += buttonWidth + 2
	}
	return layout
}

func (d ConfirmationDialog) Draw(canvas uv.ScreenBuffer) {
	bounds := canvas.Bounds()
	layout := d.Layout(bounds.Dx(), bounds.Dy())
	panelStyle := uv.Style{Bg: chrome.ParseHex(ColorPanel)}
	title := uv.Style{Fg: chrome.ParseHex(ColorSecondary), Bg: chrome.ParseHex(ColorPanel), Attrs: uv.AttrBold}
	border := uv.Style{Fg: chrome.ParseHex(ColorPrimary), Bg: chrome.ParseHex(ColorPanel)}
	muted := uv.Style{Fg: chrome.ParseHex(ColorMuted), Bg: chrome.ParseHex(ColorPanel)}
	selected := uv.Style{Fg: chrome.ParseHex(ColorCanvas), Bg: chrome.ParseHex(ColorDanger)}
	unselected := uv.Style{Fg: chrome.ParseHex(ColorInk), Bg: chrome.ParseHex(ColorStripe)}

	lastButtonY := layout.Y
	for _, buttonY := range layout.ButtonY {
		lastButtonY = max(lastButtonY, buttonY)
	}
	cardX := max(0, layout.X-2)
	cardY := max(0, layout.Y-1)
	cardRight := min(bounds.Dx(), layout.X+layout.ContentWidth+6)
	cardBottom := min(bounds.Dy(), lastButtonY+2)
	if layout.ShowHelp {
		cardBottom = min(bounds.Dy(), lastButtonY+5)
	}
	for y := cardY; y < cardBottom; y++ {
		for x := cardX; x < cardRight; x++ {
			canvas.SetCell(x, y, &uv.Cell{Content: " ", Width: 1, Style: panelStyle})
		}
	}
	for x := cardX; x < cardRight; x++ {
		canvas.SetCell(x, cardY, &uv.Cell{Content: "─", Width: 1, Style: border})
		canvas.SetCell(x, cardBottom-1, &uv.Cell{Content: "─", Width: 1, Style: border})
	}
	for y := cardY; y < cardBottom; y++ {
		canvas.SetCell(cardX, y, &uv.Cell{Content: "│", Width: 1, Style: border})
		canvas.SetCell(cardRight-1, y, &uv.Cell{Content: "│", Width: 1, Style: border})
	}
	canvas.SetCell(cardX, cardY, &uv.Cell{Content: "╭", Width: 1, Style: border})
	canvas.SetCell(cardRight-1, cardY, &uv.Cell{Content: "╮", Width: 1, Style: border})
	canvas.SetCell(cardX, cardBottom-1, &uv.Cell{Content: "╰", Width: 1, Style: border})
	canvas.SetCell(cardRight-1, cardBottom-1, &uv.Cell{Content: "╯", Width: 1, Style: border})
	for row := layout.Y; row <= lastButtonY; row++ {
		canvas.SetCell(layout.X, row, &uv.Cell{Content: "┃", Width: 1, Style: border})
	}
	drawConfirmationText(canvas, d.Title, layout.X+4, layout.Y, title)
	for index, line := range layout.Description {
		drawConfirmationText(canvas, line, layout.X+4, layout.Y+index+1, muted)
	}
	for index, option := range d.Options {
		extraPadding := max(layout.ButtonWidth[index]-4-ansi.StringWidth(option.Label), 0)
		leftPadding := (extraPadding + 1) / 2
		rightPadding := extraPadding / 2
		label := strings.Repeat(" ", 2+leftPadding) + option.Label + strings.Repeat(" ", 2+rightPadding)
		style := unselected
		if index == d.Selected {
			style = selected
		}
		drawConfirmationText(canvas, label, layout.ButtonX[index], layout.ButtonY[index], style)
	}
	if layout.ShowHelp {
		drawConfirmationText(canvas, "←/→ toggle • enter select", layout.X+1, lastButtonY+3, muted)
	}
}

func (d *ConfirmationDialog) selectOption(x, y, width, height int) (bool, string) {
	layout := d.Layout(width, height)
	for index, buttonX := range layout.ButtonX {
		if y == layout.ButtonY[index] && x >= buttonX && x < buttonX+layout.ButtonWidth[index] {
			d.Selected = index
			return true, d.Options[index].Action
		}
	}
	return false, ""
}

func (d *ConfirmationDialog) Update(message tea.Msg, width, height int) (bool, string) {
	if len(d.Options) == 0 {
		return false, ""
	}
	switch message := message.(type) {
	case tea.KeyPressMsg:
		switch message.Key().Code {
		case tea.KeyEscape:
			return true, d.Options[len(d.Options)-1].Action
		case 'n':
			if len(d.Options) == 2 {
				return true, d.Options[1].Action
			}
		case 'y':
			if len(d.Options) == 2 {
				return true, d.Options[0].Action
			}
		case tea.KeyLeft, 'h', tea.KeyUp, 'k':
			d.Selected = max(d.Selected-1, 0)
		case tea.KeyRight, 'l', tea.KeyDown, 'j':
			d.Selected = min(d.Selected+1, len(d.Options)-1)
		case tea.KeyEnter:
			return true, d.Options[d.Selected].Action
		}
	case tea.MouseClickMsg:
		if message.Button != tea.MouseLeft {
			return false, ""
		}
		return d.selectOption(message.X, message.Y, width, height)
	case tea.MouseReleaseMsg:
		return d.selectOption(message.X, message.Y, width, height)
	case tea.MouseMsg:
		mouse := message.Mouse()
		if mouse.Button != tea.MouseLeft && mouse.Button != tea.MouseNone {
			return false, ""
		}
		return d.selectOption(mouse.X, mouse.Y, width, height)
	}
	return false, ""
}
