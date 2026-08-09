package workbench

import (
	"image"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

// drawNotificationPopup draws the top-right popup card. It is drawn last so
// it stays readable above any overlay.
func (m Model) drawNotificationPopup(canvas uv.ScreenBuffer) {
	bounds, ok := m.notificationPopupBounds()
	if !ok {
		return
	}
	width := bounds.Dx() - 2
	lines := strings.Split(ansi.Wordwrap(m.notificationPopup.description, width, "\n"), "\n")
	innerH := min(len(lines), bounds.Dy()-2)
	start := max(len(lines)-innerH, 0)

	panelBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&panelBg, bounds)

	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
	canvas.SetCell(bounds.Min.X, bounds.Min.Y, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(bounds.Max.X-1, bounds.Min.Y, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(bounds.Min.X, bounds.Max.Y-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(bounds.Max.X-1, bounds.Max.Y-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})
	for cx := bounds.Min.X + 1; cx < bounds.Max.X-1; cx++ {
		canvas.SetCell(cx, bounds.Min.Y, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, bounds.Max.Y-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for cy := bounds.Min.Y + 1; cy < bounds.Max.Y-1; cy++ {
		canvas.SetCell(bounds.Min.X, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(bounds.Max.X-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}

	ink := uv.Style{Fg: chrome.ParseHex(colorInk), Bg: chrome.ParseHex(colorPanel)}
	for row := 0; row < innerH; row++ {
		line := lines[start+row]
		for col, r := range line {
			if bounds.Min.X+1+col >= bounds.Max.X-1 {
				break
			}
			canvas.SetCell(bounds.Min.X+1+col, bounds.Min.Y+1+row, &uv.Cell{Content: string(r), Width: 1, Style: ink})
		}
	}
}

// drawNotificationDetail draws the single-entry detail card opened from a
// popup click when no connection scope exists.
func (m Model) drawNotificationDetail(canvas uv.ScreenBuffer) {
	entry := m.notificationDetail
	if entry == nil {
		return
	}
	innerW := m.width - 8
	description := chrome.DetailValue(entry.description)
	body := "Title:\n  " + entry.title + "\n\nDescription:\n  " +
		ansi.Wordwrap(safeText(description), innerW-2, "\n  ") + "\n\nTime:\n  " +
		entry.createdAt.Format("2006-01-02 15:04:05") + "\n\n  esc close"
	drawCard(canvas, m.width, m.height, " Notification ", body, innerW)
}

// drawNotificationHistory draws the split modal: a narrow filterable list on
// the left, a full detail viewport on the right.
func (m Model) drawNotificationHistory(canvas uv.ScreenBuffer) {
	h := m.notificationHistory
	if h == nil {
		return
	}
	if m.width < 40 || m.height < 8 {
		drawCard(canvas, m.width, m.height, " Notifications ", "terminal too small", m.width-8)
		return
	}
	leftWidth := clamp(m.width/3, 24, 40)
	rightWidth := m.width - leftWidth - 3

	drawPaneBox(canvas, 1, 1, leftWidth, m.height-2, " Notifications ", h.pane == notificationHistoryListPane)
	filter := h.filter.View()
	filterBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Padding(0, 1).
		Width(max(leftWidth-6, 0))
	filterLines := strings.Split(filterBox.Render(ansi.Truncate(filter, max(leftWidth-8, 0), "")), "\n")
	for row, line := range filterLines {
		drawLabel(canvas, 2, 3+row, safeText(line), colorInk, colorCanvas)
	}
	listView := h.list.View()
	listLines := strings.Split(listView, "\n")
	innerLeft := leftWidth - 4
	if len(h.filtered) == 0 {
		drawLabel(canvas, 2, 4, "No notifications", colorMuted, colorCanvas)
	}
	for row, line := range listLines {
		if row >= m.height-6 {
			break
		}
		drawLabel(canvas, 2, 4+row, ansi.Truncate(safeText(line), innerLeft, ""), colorInk, colorCanvas)
	}

	rightX := leftWidth + 3
	drawPaneBox(canvas, rightX, 1, rightWidth, m.height-2, " Details ", h.pane == notificationHistoryDetailPane)
	innerRight := rightWidth - 4
	if _, ok := h.selected(); !ok {
		drawLabel(canvas, rightX+2, 3, "No notification selected", colorMuted, colorCanvas)
	} else {
		content := h.detail.View()
		detailLines := strings.Split(content, "\n")
		for row, line := range detailLines {
			if row >= m.height-4 {
				break
			}
			drawLabel(canvas, rightX+2, 3+row, ansi.Truncate(safeText(line), innerRight, ""), colorInk, colorCanvas)
		}
	}
	drawLabel(canvas, rightX+2, m.height-2, "h/l panes | j/k move or scroll | / filter | esc close", colorMuted, colorCanvas)
}

// drawPaneBox draws a titled bordered box on the canvas.
func drawPaneBox(canvas uv.ScreenBuffer, x, y, width, height int, title string, focused bool) {
	borderColor := colorBorder
	if focused {
		borderColor = colorPrimary
	}
	borderStyle := uv.Style{Fg: chrome.ParseHex(borderColor)}
	panelBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&panelBg, image.Rect(x, y, x+width, y+height))
	for cx := x + 1; cx < x+width-1; cx++ {
		canvas.SetCell(cx, y, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, y+height-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for cy := y + 1; cy < y+height-1; cy++ {
		canvas.SetCell(x, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(x+width-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(x, y, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(x+width-1, y, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(x, y+height-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(x+width-1, y+height-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})
	titleStyle := uv.Style{Fg: chrome.ParseHex(colorSecondary), Bg: chrome.ParseHex(colorPanel), Attrs: uv.AttrBold}
	for offset, r := range title {
		if x+1+offset >= x+width-1 {
			break
		}
		canvas.SetCell(x+1+offset, y, &uv.Cell{Content: string(r), Width: 1, Style: titleStyle})
	}
}

// drawLabel renders one text line onto the canvas at (x, y).
func drawLabel(canvas uv.ScreenBuffer, x, y int, text string, fg, bg string) {
	style := uv.Style{Fg: chrome.ParseHex(fg), Bg: chrome.ParseHex(bg)}
	for col, r := range text {
		canvas.SetCell(x+col, y, &uv.Cell{Content: string(r), Width: 1, Style: style})
	}
}

// drawCard draws a centered bordered card with the given title and body.
func drawCard(canvas uv.ScreenBuffer, width, height int, title, body string, innerW int) {
	if innerW < 4 {
		return
	}
	bounds := canvas.Bounds()
	bodyW := min(innerW, bounds.Dx()-6)
	bodyH := min(len(strings.Split(body, "\n")), bounds.Dy()-6)
	if bodyW <= 0 || bodyH <= 0 {
		return
	}
	borderW := bodyW + 2
	borderH := bodyH + 2
	x := max(0, (bounds.Dx()-borderW)/2)
	y := max(0, (bounds.Dy()-borderH)/2)
	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(x, y, x+borderW, y+borderH))
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
	canvas.SetCell(x, y, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(x, y+borderH-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y+borderH-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})
	for cx := x + 1; cx < x+borderW-1; cx++ {
		canvas.SetCell(cx, y, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, y+borderH-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for cy := y + 1; cy < y+borderH-1; cy++ {
		canvas.SetCell(x, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(x+borderW-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	titleStyle := uv.Style{Fg: chrome.ParseHex(colorSecondary), Bg: chrome.ParseHex(colorPanel), Attrs: uv.AttrBold}
	for offset, r := range title {
		if x+1+offset >= x+borderW-1 {
			break
		}
		canvas.SetCell(x+1+offset, y, &uv.Cell{Content: string(r), Width: 1, Style: titleStyle})
	}
	ink := uv.Style{Fg: chrome.ParseHex(colorInk), Bg: chrome.ParseHex(colorPanel)}
	for row, line := range strings.Split(body, "\n") {
		if y+1+row >= y+borderH-1 {
			break
		}
		drawStyledLabel(canvas, x+1, y+1+row, line, ink)
	}
}

func drawStyledLabel(canvas uv.ScreenBuffer, x, y int, text string, style uv.Style) {
	for col, r := range text {
		if x+col >= canvas.Bounds().Max.X {
			break
		}
		canvas.SetCell(x+col, y, &uv.Cell{Content: string(r), Width: 1, Style: style})
	}
}
