package querylog

import (
	"image"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Draw renders the detail overlay when open. The root calls Draw at the
// query-log detail precedence slot and draws the notification popup over
// the result.
func (m Model) Draw(canvas uv.ScreenBuffer, layout uikit.Layout) {
	if m.Detail == nil {
		return
	}
	d := m.Detail
	var statusStr, iconStr string
	switch d.Status {
	case "failed":
		statusStr = "Failed"
		iconStr = uikit.StatusFailedStyle.Render(uikit.IconFailed)
	case "canceled":
		statusStr = "Canceled"
		iconStr = uikit.StatusCanceledStyle.Render(uikit.IconCanceled)
	default:
		statusStr = "Success"
		iconStr = uikit.StatusSuccessStyle.Render(uikit.IconSuccess)
	}

	innerW := layout.Width - 4

	var b strings.Builder
	b.WriteString(uikit.HeaderStyle.Render("  \uf0ca Query Log Detail  "))
	b.WriteString("\n\n")
	b.WriteString("  Time:     ")
	b.WriteString(d.StartedAt.Format("2006-01-02 15:04:05"))
	b.WriteString("\n")
	b.WriteString("  Status:   ")
	b.WriteString(iconStr)
	b.WriteString(" ")
	b.WriteString(statusStr)
	b.WriteString("\n")
	b.WriteString("  Duration: ")
	b.WriteString(d.Duration.Round(time.Microsecond).String())
	b.WriteString("\n")
	b.WriteString("  Statement:\n    ")
	b.WriteString(ansi.Wordwrap(uikit.SafeText(chrome.DetailValue(d.Statement)), innerW-4, "\n    "))
	b.WriteString("\n")
	b.WriteString("  Message:  ")
	b.WriteString(ansi.Wordwrap(uikit.SafeText(chrome.DetailValue(d.Message)), innerW-14, " "))
	b.WriteString(advisoryBlock(*d, innerW))
	b.WriteString("\n  y copy | e explain | enter/esc close")

	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(uikit.ColorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(1, 1, layout.Width-1, layout.Height-1))

	borderStyle := uv.Style{Fg: chrome.ParseHex(uikit.ColorBorder)}
	for x := 1; x < layout.Width-1; x++ {
		canvas.SetCell(x, 0, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(x, layout.Height-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for y := 1; y < layout.Height-1; y++ {
		canvas.SetCell(0, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(layout.Width-1, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(0, 0, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(layout.Width-1, 0, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(0, layout.Height-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(layout.Width-1, layout.Height-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})

	uv.NewStyledString(b.String()).Draw(canvas, image.Rect(1, 1, layout.Width-1, layout.Height-1))
}

// advisoryBlock renders the labeled advisory guidance lines of a detail
// entry: "  Hint:     …" and "  Try:      …", each word-wrapped like the
// message, ending in a trailing newline. It is empty when neither
// advisory is present, so the detail view shows no advisory labels for
// entries without backend guidance. Advisories are backend text rendered
// separately from the raw error message; the workbench never executes a
// suggested statement.
func advisoryBlock(d Entry, innerW int) string {
	var b strings.Builder
	if d.Hint != "" {
		b.WriteString("\n")
		b.WriteString("  Hint:     ")
		b.WriteString(ansi.Wordwrap(uikit.SafeText(chrome.DetailValue(d.Hint)), innerW-14, " "))
	}
	if d.SuggestedStatement != "" {
		b.WriteString("\n")
		b.WriteString("  Try:      ")
		b.WriteString(ansi.Wordwrap(uikit.SafeText(chrome.DetailValue(d.SuggestedStatement)), innerW-14, " "))
	}
	return b.String()
}
