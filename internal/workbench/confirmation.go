package workbench

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

type confirmationOption struct {
	label  string
	action string
}

type confirmationDialog struct {
	title, description string
	options            []confirmationOption
	selected           int
}

type confirmationLayout struct {
	x, y         int
	buttonX      []int
	buttonY      []int
	buttonWidth  []int
	description  []string
	contentWidth int
	showHelp     bool
}

func newConfirmationDialog(title, description string, options []confirmationOption) *confirmationDialog {
	return &confirmationDialog{title: title, description: description, options: options}
}

func yesNoConfirmation(title, description, action string) *confirmationDialog {
	return newConfirmationDialog(title, description, []confirmationOption{
		{label: "Yes", action: action},
		{label: "No", action: "cancel"},
	})
}

func (d confirmationDialog) content(width int) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(d.title)
	if description := strings.TrimSpace(d.description); description != "" {
		b.WriteString("\n\n  ")
		b.WriteString(ansi.Wordwrap(safeText(description), max(min(width-8, 72)-2, 1), "\n  "))
	}
	b.WriteString("\n\n")
	for index, option := range d.options {
		if index == d.selected {
			b.WriteString("  > ")
		} else {
			b.WriteString("    ")
		}
		b.WriteString(option.label)
		if index+1 < len(d.options) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (d confirmationDialog) layout(width, height int) confirmationLayout {
	layout := confirmationLayout{}
	if description := strings.TrimSpace(d.description); description != "" {
		layout.description = strings.Split(ansi.Wordwrap(safeText(description), max(min(width-10, 72), 1), "\n"), "\n")
	}
	buttonRowWidth := 0
	for _, option := range d.options {
		width := ansi.StringWidth(option.label) + 4
		layout.buttonWidth = append(layout.buttonWidth, width)
		buttonRowWidth += width
	}
	buttonRowWidth += max(len(d.options)-1, 0) * 2
	stackButtons := buttonRowWidth > max(width-5, 1)
	buttonHeight := 1
	if stackButtons {
		buttonHeight = len(d.options)
	}
	if height < len(layout.description)+buttonHeight+5 {
		layout.description = nil
	}
	layout.showHelp = height >= len(layout.description)+buttonHeight+6
	contentWidth := max(ansi.StringWidth(d.title), buttonRowWidth)
	if stackButtons {
		contentWidth = ansi.StringWidth(d.title)
		for _, buttonWidth := range layout.buttonWidth {
			contentWidth = max(contentWidth, buttonWidth)
		}
	}
	for _, line := range layout.description {
		contentWidth = max(contentWidth, ansi.StringWidth(line))
	}
	layout.contentWidth = contentWidth
	layout.x = max(0, (width-contentWidth-5)/2)
	contentHeight := len(layout.description) + buttonHeight + 3
	if layout.showHelp {
		contentHeight += 3
	}
	layout.y = max(0, (height-contentHeight)/2)
	buttonY := layout.y + len(layout.description) + 2
	if stackButtons {
		for index := range d.options {
			buttonX := max(0, min(layout.x+4, max(width-layout.buttonWidth[index], 0)))
			layout.buttonX = append(layout.buttonX, buttonX)
			layout.buttonWidth[index] = min(layout.buttonWidth[index], max(width-buttonX, 1))
			layout.buttonY = append(layout.buttonY, buttonY+index)
		}
		return layout
	}
	buttonX := layout.x + 4 + max((contentWidth-buttonRowWidth)/2, 0)
	for _, buttonWidth := range layout.buttonWidth {
		layout.buttonX = append(layout.buttonX, buttonX)
		layout.buttonY = append(layout.buttonY, buttonY)
		buttonX += buttonWidth + 2
	}
	return layout
}

func drawConfirmationText(canvas uv.ScreenBuffer, text string, x, y int, style uv.Style) {
	bounds := canvas.Bounds()
	if y < 0 || y >= bounds.Dy() {
		return
	}
	for _, character := range text {
		width := max(ansi.StringWidth(string(character)), 1)
		if x < 0 {
			x += width
			continue
		}
		if x+width > bounds.Dx() {
			return
		}
		canvas.SetCell(x, y, &uv.Cell{Content: string(character), Width: width, Style: style})
		x += width
	}
}

func (d confirmationDialog) draw(canvas uv.ScreenBuffer) {
	bounds := canvas.Bounds()
	layout := d.layout(bounds.Dx(), bounds.Dy())
	accent := uv.Style{Fg: chrome.ParseHex(colorAccent), Attrs: uv.AttrBold}
	border := uv.Style{Fg: chrome.ParseHex(colorAccent)}
	muted := uv.Style{Fg: chrome.ParseHex(colorMuted)}
	selected := uv.Style{Fg: chrome.ParseHex(colorCanvas), Bg: chrome.ParseHex(colorAccent)}
	unselected := uv.Style{Fg: chrome.ParseHex(colorInk), Bg: chrome.ParseHex(colorStripe)}

	lastButtonY := layout.y
	for _, buttonY := range layout.buttonY {
		lastButtonY = max(lastButtonY, buttonY)
	}
	cardX := max(0, layout.x-2)
	cardY := max(0, layout.y-1)
	cardRight := min(bounds.Dx(), layout.x+layout.contentWidth+6)
	cardBottom := min(bounds.Dy(), lastButtonY+2)
	if layout.showHelp {
		cardBottom = min(bounds.Dy(), lastButtonY+5)
	}
	for x := cardX; x < cardRight; x++ {
		canvas.SetCell(x, cardY, &uv.Cell{Content: "─", Width: 1, Style: border})
		canvas.SetCell(x, cardBottom-1, &uv.Cell{Content: "─", Width: 1, Style: border})
	}
	for y := cardY; y < cardBottom; y++ {
		canvas.SetCell(cardX, y, &uv.Cell{Content: "│", Width: 1, Style: border})
		canvas.SetCell(cardRight-1, y, &uv.Cell{Content: "│", Width: 1, Style: border})
	}
	canvas.SetCell(cardX, cardY, &uv.Cell{Content: "┌", Width: 1, Style: border})
	canvas.SetCell(cardRight-1, cardY, &uv.Cell{Content: "┐", Width: 1, Style: border})
	canvas.SetCell(cardX, cardBottom-1, &uv.Cell{Content: "└", Width: 1, Style: border})
	canvas.SetCell(cardRight-1, cardBottom-1, &uv.Cell{Content: "┘", Width: 1, Style: border})
	for row := layout.y; row <= lastButtonY; row++ {
		canvas.SetCell(layout.x, row, &uv.Cell{Content: "┃", Width: 1, Style: border})
	}
	drawConfirmationText(canvas, d.title, layout.x+4, layout.y, accent)
	for index, line := range layout.description {
		drawConfirmationText(canvas, line, layout.x+4, layout.y+index+1, muted)
	}
	for index, option := range d.options {
		label := "  " + option.label + "  "
		style := unselected
		if index == d.selected {
			style = selected
		}
		drawConfirmationText(canvas, label, layout.buttonX[index], layout.buttonY[index], style)
	}
	if layout.showHelp {
		drawConfirmationText(canvas, "←/→ toggle • enter select", layout.x+1, lastButtonY+3, muted)
	}
}

func (d *confirmationDialog) selectOption(x, y, width, height int) (bool, string) {
	layout := d.layout(width, height)
	for index, buttonX := range layout.buttonX {
		if y == layout.buttonY[index] && x >= buttonX && x < buttonX+layout.buttonWidth[index] {
			d.selected = index
			return true, d.options[index].action
		}
	}
	return false, ""
}

func (d *confirmationDialog) Update(message tea.Msg, width, height int) (bool, string) {
	if len(d.options) == 0 {
		return false, ""
	}
	switch message := message.(type) {
	case tea.KeyPressMsg:
		switch message.Key().Code {
		case tea.KeyEscape:
			return true, d.options[len(d.options)-1].action
		case 'n':
			if len(d.options) == 2 {
				return true, d.options[1].action
			}
		case 'y':
			if len(d.options) == 2 {
				return true, d.options[0].action
			}
		case tea.KeyLeft, 'h', tea.KeyUp, 'k':
			d.selected = max(d.selected-1, 0)
		case tea.KeyRight, 'l', tea.KeyDown, 'j':
			d.selected = min(d.selected+1, len(d.options)-1)
		case tea.KeyEnter:
			return true, d.options[d.selected].action
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
