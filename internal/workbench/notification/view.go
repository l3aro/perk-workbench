package notification

import (
	"image"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// View renders no pane content: notifications are pure overlays. The root
// draws them through Draw at its overlay precedence slots.
func (m Model) View(layout uikit.Layout) string { return "" }

// Draw renders the component's open overlays onto the canvas: the history
// modal, then the single-entry detail, then the popup last so it stays
// readable above everything. Root calls Draw from each overlay precedence
// slot and draws whatever is open.
func (m Model) Draw(canvas uv.ScreenBuffer, layout uikit.Layout) {
	if m.History != nil {
		drawHistory(canvas, m.History, layout)
	}
	if m.Detail != nil {
		drawDetail(canvas, m.Detail, layout)
	}
	if m.Popup != nil {
		drawPopup(canvas, m.Popup, layout)
	}
}

// drawPopup draws the top-right popup card. It is drawn last so it stays
// readable above any overlay.
func drawPopup(canvas uv.ScreenBuffer, popup *Entry, layout uikit.Layout) {
	bounds, ok := popupBounds(popup, layout)
	if !ok {
		return
	}
	// The level symbol sits on the title row; title text and description
	// start after it so the whole body is indented together.
	iconIndent := iconIndent(popup.Level)
	bodyX := bounds.Min.X + 1 + iconIndent
	width := max(bounds.Dx()-2-iconIndent, 1)
	lines := strings.Split(ansi.Wordwrap(popup.Description, width, "\n"), "\n")
	innerH := min(len(lines), max(bounds.Dy()-3, 0))
	start := max(len(lines)-innerH, 0)

	panelBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(uikit.ColorPanel)}}
	canvas.FillArea(&panelBg, bounds)

	borderStyle := uv.Style{Fg: chrome.ParseHex(borderColor(popup.Level))}
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
		Fg:    chrome.ParseHex(levelColor(popup.Level)),
		Bg:    chrome.ParseHex(uikit.ColorPanel),
		Attrs: uv.AttrBold,
	}
	tx := bounds.Min.X + 1
	if level, ok := logLevelOf(popup.Level); ok {
		icon := logLevelIcon(level)
		canvas.SetCell(tx, bounds.Min.Y+1, &uv.Cell{Content: icon, Width: max(ansi.StringWidth(icon), 1), Style: titleStyle})
		tx += max(ansi.StringWidth(icon), 1) + 1 // symbol + gap
	}
	titleText := popup.Title
	if level, ok := logLevelOf(popup.Level); ok {
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

	ink := uv.Style{Fg: chrome.ParseHex(uikit.ColorInk), Bg: chrome.ParseHex(uikit.ColorPanel)}
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

// popupBounds returns the screen rectangle of the visible popup: a
// bordered card anchored to the top-right corner.
func popupBounds(popup *Entry, layout uikit.Layout) (image.Rectangle, bool) {
	if popup == nil {
		return image.Rectangle{}, false
	}
	width := min(50, layout.Width-4)
	if width < 4 || layout.Height < 4 {
		return image.Rectangle{}, false
	}
	lines := strings.Split(ansi.Wordwrap(popup.Description, max(width-4-iconIndent(popup.Level), 1), "\n"), "\n")
	cardW := width + 2
	cardH := len(lines) + 3 // title row + description + top/bottom border
	if cardH > layout.Height-4 {
		cardH = layout.Height - 4
	}
	x := layout.Width - cardW - 1
	y := 1
	return image.Rect(x, y, x+cardW, y+cardH), true
}

// drawDetail draws the single-entry detail card opened from a popup click
// when no connection scope exists.
func drawDetail(canvas uv.ScreenBuffer, entry *Entry, layout uikit.Layout) {
	if entry == nil {
		return
	}
	innerW := layout.Width - 8
	description := chrome.DetailValue(entry.Description)
	body := "Title:\n  " + entry.Title + "\n\nDescription:\n  " +
		ansi.Wordwrap(uikit.SafeText(description), innerW-2, "\n  ") + "\n\nTime:\n  " +
		entry.CreatedAt.Format("2006-01-02 15:04:05") + "\n\n  esc close"
	drawCard(canvas, layout.Width, layout.Height, " Notification ", body, innerW)
}

// drawHistory draws the modal: a filter input, a sortable table with cell
// travel, and a status row with Prev/Next pager buttons. An open cell
// viewer overlays the modal.
func drawHistory(canvas uv.ScreenBuffer, h *history, layout uikit.Layout) {
	if h == nil {
		return
	}
	if layout.Width < 40 || layout.Height < 14 {
		drawCard(canvas, layout.Width, layout.Height, " Notifications ", "terminal too small", layout.Width-8)
		return
	}
	boxW := layout.Width - 2
	innerW := max(boxW-4, 1)

	// Panel background so the modal reads above the panes underneath.
	panelBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(uikit.ColorPanel)}}
	canvas.FillArea(&panelBg, image.Rect(1, 1, layout.Width-1, layout.Height-1))

	filter := h.filter.View()
	filterBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(uikit.ColorBorder)).
		Padding(0, 1).
		Width(innerW)
	tableLines := strings.Split(uikit.TableViewportViewWithAlignment(h.table, nil, h.offset, innerW, h.selectedCol), "\n")
	pager := h.pager()
	var b strings.Builder
	b.WriteString(filterBox.Render(ansi.Truncate(filter, max(innerW-6, 1), "")))
	b.WriteString("\n\n")
	b.WriteString(strings.Join(tableLines, "\n"))
	b.WriteString("\n\n")
	b.WriteString(pager.Line)
	// Width is the physical box width in lipgloss v2 (content is wrapped
	// at Width-frame), so Width(boxW) leaves exactly innerW cells for the
	// content lines.
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(uikit.ColorPrimary)).
		Padding(0, 1).
		Width(boxW).
		Render(b.String())
	uv.NewStyledString(box).Draw(canvas, image.Rect(1, 1, layout.Width-1, layout.Height-1))

	drawLabel(canvas, 2, layout.Height-2, "h/j/k/l move | s sort | / filter | y copy | v view | n/p page | esc close", uikit.ColorMuted, uikit.ColorCanvas)
	if h.viewer != nil {
		uikit.DrawCellViewerBox(canvas, h.viewer)
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
	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(uikit.ColorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(x, y, x+borderW, y+borderH))
	borderStyle := uv.Style{Fg: chrome.ParseHex(uikit.ColorBorder)}
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
	titleStyle := uv.Style{Fg: chrome.ParseHex(uikit.ColorSecondary), Bg: chrome.ParseHex(uikit.ColorPanel), Attrs: uv.AttrBold}
	for offset, r := range title {
		if x+1+offset >= x+borderW-1 {
			break
		}
		canvas.SetCell(x+1+offset, y, &uv.Cell{Content: string(r), Width: 1, Style: titleStyle})
	}
	ink := uv.Style{Fg: chrome.ParseHex(uikit.ColorInk), Bg: chrome.ParseHex(uikit.ColorPanel)}
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
