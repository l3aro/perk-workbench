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
	// The level symbol sits on the title row; title text and description
	// start after it so the whole body is indented together.
	iconIndent := notificationIconIndent(m.notificationPopup.level)
	bodyX := bounds.Min.X + 1 + iconIndent
	width := max(bounds.Dx()-2-iconIndent, 1)
	lines := strings.Split(ansi.Wordwrap(m.notificationPopup.description, width, "\n"), "\n")
	innerH := min(len(lines), max(bounds.Dy()-3, 0))
	start := max(len(lines)-innerH, 0)

	panelBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&panelBg, bounds)

	borderStyle := uv.Style{Fg: chrome.ParseHex(notificationBorderColor(m.notificationPopup.level))}
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

	// Title row: level symbol then level title, both in the severity color,
	// bold. The symbol may be a double-width glyph, so advance by measured
	// width like drawConfirmationText does.
	titleStyle := uv.Style{
		Fg:    chrome.ParseHex(notificationLevelColor(m.notificationPopup.level)),
		Bg:    chrome.ParseHex(colorPanel),
		Attrs: uv.AttrBold,
	}
	tx := bounds.Min.X + 1
	if level, ok := logLevelOf(m.notificationPopup.level); ok {
		icon := logLevelIcon(level)
		canvas.SetCell(tx, bounds.Min.Y+1, &uv.Cell{Content: icon, Width: max(ansi.StringWidth(icon), 1), Style: titleStyle})
		tx += max(ansi.StringWidth(icon), 1) + 1 // symbol + gap
	}
	titleText := m.notificationPopup.title
	if level, ok := logLevelOf(m.notificationPopup.level); ok {
		titleText = level.Title()
	}
	for _, r := range titleText {
		runeWidth := max(ansi.StringWidth(string(r)), 1)
		if tx+runeWidth > bounds.Max.X-1 {
			break
		}
		canvas.SetCell(tx, bounds.Min.Y+1, &uv.Cell{Content: string(r), Width: runeWidth, Style: titleStyle})
		tx += runeWidth
	}

	ink := uv.Style{Fg: chrome.ParseHex(colorInk), Bg: chrome.ParseHex(colorPanel)}
	for row := 0; row < innerH; row++ {
		cx := bodyX
		for _, r := range lines[start+row] {
			runeWidth := max(ansi.StringWidth(string(r)), 1)
			if cx+runeWidth > bounds.Max.X-1 {
				break
			}
			canvas.SetCell(cx, bounds.Min.Y+2+row, &uv.Cell{Content: string(r), Width: runeWidth, Style: ink})
			cx += runeWidth
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

// drawNotificationHistory draws the modal: a filter input, a sortable
// table with cell travel, and a status row with Prev/Next pager buttons.
// An open cell viewer overlays the modal.
func (m Model) drawNotificationHistory(canvas uv.ScreenBuffer) {
	h := m.notificationHistory
	if h == nil {
		return
	}
	if m.width < 40 || m.height < 14 {
		drawCard(canvas, m.width, m.height, " Notifications ", "terminal too small", m.width-8)
		return
	}
	boxW := m.width - 2
	innerW := max(boxW-4, 1)

	// Panel background so the modal reads above the panes underneath.
	panelBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&panelBg, image.Rect(1, 1, m.width-1, m.height-1))

	filter := h.filter.View()
	filterBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Padding(0, 1).
		Width(innerW)
	tableLines := strings.Split(tableViewportViewWithAlignment(h.table, nil, h.offset, innerW, h.selectedCol), "\n")
	pager := h.pager()
	var b strings.Builder
	b.WriteString(filterBox.Render(ansi.Truncate(filter, max(innerW-6, 1), "")))
	b.WriteString("\n\n")
	b.WriteString(strings.Join(tableLines, "\n"))
	b.WriteString("\n\n")
	b.WriteString(pager.line)
	// Width is the physical box width in lipgloss v2 (content is wrapped
	// at Width-frame), so Width(boxW) leaves exactly innerW cells for the
	// content lines.
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorPrimary)).
		Padding(0, 1).
		Width(boxW).
		Render(b.String())
	uv.NewStyledString(box).Draw(canvas, image.Rect(1, 1, m.width-1, m.height-1))

	drawLabel(canvas, 2, m.height-2, "h/j/k/l move | s sort | / filter | y copy | v view | n/p page | esc close", colorMuted, colorCanvas)
	if h.viewer != nil {
		drawCellViewerBox(canvas, h.viewer)
	}
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
