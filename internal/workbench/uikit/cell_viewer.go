package uikit

import (
	"fmt"
	"image"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

// CellViewer is a read-only viewport overlay for inspecting a table cell
// value. It wraps the bubbles viewport for vertical scrolling with
// h/j/k/l, arrow keys, paging, mouse wheel, a soft-wrap toggle (w), and
// horizontal scrolling when word wrap is off. Content is word-wrapped by
// default. Shared by the browse pane and the notification history modal.
type CellViewer struct {
	Column   string
	Viewport viewport.Model
}

// NewCellViewer creates a CellViewer with the given column name and cell
// value. width and height define the viewport interior (not including the
// dialog frame). The value is sanitized to strip ANSI escape codes and
// unsafe control characters while preserving newlines and full length.
// Content wraps by default; press w to toggle word wrap off for horizontal
// scrolling.
func NewCellViewer(column, value string, width, height int) *CellViewer {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.SetContent(chrome.DetailValue(sanitizeCellViewer(value)))
	vp.FillHeight = false
	vp.MouseWheelEnabled = true
	vp.SoftWrap = true
	return &CellViewer{Column: column, Viewport: vp}
}

// Resize updates the viewport dimensions while retaining content and
// scroll position.
func (cv *CellViewer) Resize(width, height int) {
	cv.Viewport.SetWidth(width)
	cv.Viewport.SetHeight(height)
}

// Update handles messages for the viewer. Intercepts 'w' to toggle soft
// wrap; all other messages are delegated to the viewport (h/j/k/l, arrows,
// paging, mouse wheel).
func (cv *CellViewer) Update(msg tea.Msg) tea.Cmd {
	if keyPress, ok := msg.(tea.KeyPressMsg); ok {
		switch keyPress.Key().Code {
		case 'w':
			cv.Viewport.SoftWrap = !cv.Viewport.SoftWrap
			if cv.Viewport.SoftWrap {
				cv.Viewport.SetXOffset(0)
			}
			return nil
		}
	}
	var cmd tea.Cmd
	cv.Viewport, cmd = cv.Viewport.Update(msg)
	return cmd
}

// Content returns the title line and the viewport render, including scroll
// percentages.
func (cv *CellViewer) Content() string {
	vPct := int(cv.Viewport.ScrollPercent() * 100)
	hPct := int(cv.Viewport.HorizontalScrollPercent() * 100)
	return fmt.Sprintf("View %s \u2014 V:%d%% H:%d%% | w wrap | Esc close\n%s",
		cv.Column, vPct, hPct, cv.Viewport.View())
}

// sanitizeCellViewer strips ANSI escape codes and unsafe control characters
// from a raw DB value while preserving newlines and full length. This
// prevents terminal injection via stored text while keeping the content
// readable.
func sanitizeCellViewer(input string) string {
	var display strings.Builder
	display.Grow(len(input))
	for i := 0; i < len(input); {
		r, size := rune(input[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(input[i:])
		}
		if r == '\x1b' {
			i += AnsiSequenceLen(input[i:])
			continue
		}
		// Preserve newlines and tabs, strip other controls.
		if r == '\n' || r == '\t' || (!unicode.IsControl(r) && r < '\U0010FFFF') {
			display.WriteRune(r)
		}
		i += size
	}
	return display.String()
}

// DrawCellViewerBox draws one cell viewer card on the canvas. Shared by
// the workspace overlay and the notification history modal.
func DrawCellViewerBox(canvas uv.ScreenBuffer, cv *CellViewer) {
	if cv == nil {
		return
	}
	bounds := canvas.Bounds()

	padX := 2
	title := "View " + cv.Column
	titleWidth := ansi.StringWidth(title)

	// Body height = actual rendered lines, capped at scroll window.
	// Short content makes a compact dialog.
	vpContent := cv.Viewport.View()
	vpLines := strings.Split(vpContent, "\n")
	bodyRows := max(min(len(vpLines), cv.Viewport.Height()), 1)

	viewW := cv.Viewport.Width()
	contentW := max(viewW, titleWidth)
	innerW := contentW + padX*2
	innerH := 1 + 1 + bodyRows + 1 + 1 // title + top pad + body + bottom pad + footer

	borderW := innerW + 2
	borderH := innerH + 2
	if borderW > bounds.Dx() || borderH > bounds.Dy() {
		return
	}

	x := max(0, (bounds.Dx()-borderW)/2)
	y := max(0, (bounds.Dy()-borderH)/2)

	// Fill panel background
	panelStyle := uv.Style{Bg: chrome.ParseHex(ColorPanel)}
	bgCell := uv.Cell{Content: " ", Width: 1, Style: panelStyle}
	canvas.FillArea(&bgCell, image.Rect(x, y, x+borderW, y+borderH))

	// Border
	borderStyle := uv.Style{Fg: chrome.ParseHex(ColorBorder)}
	canvas.SetCell(x, y, &uv.Cell{Content: "\u256d", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y, &uv.Cell{Content: "\u256e", Width: 1, Style: borderStyle})
	canvas.SetCell(x, y+borderH-1, &uv.Cell{Content: "\u2570", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y+borderH-1, &uv.Cell{Content: "\u256f", Width: 1, Style: borderStyle})
	for cx := x + 1; cx < x+borderW-1; cx++ {
		canvas.SetCell(cx, y, &uv.Cell{Content: "\u2500", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, y+borderH-1, &uv.Cell{Content: "\u2500", Width: 1, Style: borderStyle})
	}
	for cy := y + 1; cy < y+borderH-1; cy++ {
		canvas.SetCell(x, cy, &uv.Cell{Content: "\u2502", Width: 1, Style: borderStyle})
		canvas.SetCell(x+borderW-1, cy, &uv.Cell{Content: "\u2502", Width: 1, Style: borderStyle})
	}

	// Styles matching confirmation dialog
	titleStyle := uv.Style{Fg: chrome.ParseHex(ColorSecondary), Bg: chrome.ParseHex(ColorPanel), Attrs: uv.AttrBold}
	ink := uv.Style{Fg: chrome.ParseHex(ColorInk), Bg: chrome.ParseHex(ColorPanel)}
	muted := uv.Style{Fg: chrome.ParseHex(ColorMuted), Bg: chrome.ParseHex(ColorPanel)}

	// Content area starts at (x+1, y+1)
	cx0 := x + 1
	cy0 := y + 1

	// Row 0: title (title color/bold, matching confirmation dialog style)
	drawConfirmationText(canvas, title, cx0+padX, cy0, titleStyle)

	// Row 1: blank padding

	// Rows 2..2+bodyRows-1: viewport content
	vpStartY := cy0 + 2
	for i := 0; i < bodyRows; i++ {
		drawConfirmationText(canvas, vpLines[i], cx0+padX, vpStartY+i, ink)
	}

	// Last row: footer with bindings left, V%/H% right
	footerY := cy0 + innerH - 1

	bindings := "w wrap | Esc close"
	drawConfirmationText(canvas, bindings, cx0+padX, footerY, muted)

	vPct := int(cv.Viewport.ScrollPercent() * 100)
	hPct := int(cv.Viewport.HorizontalScrollPercent() * 100)
	pctStr := fmt.Sprintf("V:%d%% H:%d%%", vPct, hPct)
	pctWidth := ansi.StringWidth(pctStr)
	pctX := cx0 + innerW - padX - pctWidth
	if pctX < cx0+padX {
		pctX = cx0 + padX
	}
	drawConfirmationText(canvas, pctStr, pctX, footerY, muted)
}

// drawConfirmationText writes one text line onto the canvas at (x, y) in
// the given style, measuring rune widths so wide glyphs advance correctly.
// It mirrors the root confirmation dialog's renderer so the viewer card
// draws identically wherever it appears.
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
