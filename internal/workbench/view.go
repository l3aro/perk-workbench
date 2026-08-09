package workbench

import (
	"fmt"
	"image"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	view.KeyboardEnhancements.ReportEventTypes = true
	view.MouseMode = tea.MouseModeCellMotion
	if m.height < 4 || m.width < 1 {
		view.SetContent(headerStyle.Render("PERK WORKBENCH"))
		return view
	}
	content := m.contentView()
	fullContent := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), content, footerStyle.Render(m.footer()))
	if m.commandPalette.visible || m.themePicker != nil || m.tableTargetPicker != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.themePicker != nil {
			m.drawConfirmDialog(canvas, m.themePicker.content())
		} else if m.tableTargetPicker != nil {
			m.drawConfirmDialog(canvas, m.tableTargetPicker.content())
		} else {
			m.commandPalette.paletteDraw(canvas, m.width, m.height)
		}
		m.drawNotificationPopup(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.notificationHistory != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		m.drawNotificationHistory(canvas)
		m.drawNotificationPopup(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.notificationDetail != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		m.drawNotificationDetail(canvas)
		m.drawNotificationPopup(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.queryLogDetail != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		m.drawQueryLogDetail(canvas)
		m.drawNotificationPopup(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.cellEditor != nil || m.cellViewer != nil || m.explainPicker != nil || m.chatHistoryPicker != nil || m.quitDialog != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() ||
		m.connection.confirmation != nil || m.contextMenu != nil || m.deleteConfirm != nil || m.queryConfirmation != nil || m.hasConfirming() {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.contextMenu != nil {
			m.drawContextMenu(canvas)
		} else if dialog := m.activeConfirmation(); dialog != nil {
			dialog.draw(canvas)
		} else if dialog := m.confirmContent(); dialog != "" {
			m.drawConfirmDialog(canvas, dialog)
		} else if m.cellViewer != nil {
			m.drawCellViewer(canvas)
		}
		m.drawNotificationPopup(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.notificationPopup != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		m.drawNotificationPopup(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	view.SetContent(fullContent)
	return view
}

// headerButtonLabel is the command-palette button pinned to the right side
// of the header row, next to the quit button.
const headerButtonLabel = ">_ Command"

// headerButtonWidth returns the common rendered width of the two header
// buttons: both are padded to the wider one so they render identically.
func headerButtonWidth() int {
	pal := ansi.StringWidth(headerButtonStyle.Render(headerButtonLabel))
	quit := ansi.StringWidth(headerQuitButtonStyle.Render(headerQuitButtonLabel))
	return max(pal, quit)
}

// headerQuitButtonLabel is the I/O power-symbol button pinned to the far
// right of the header row: it opens the quit confirmation dialog (Ctrl+Q).
const headerQuitButtonLabel = "⏻ Quit"

// headerButtonGap is the fixed padding between the palette and quit buttons,
// kept even when the terminal is too narrow for a leading gap.
const headerButtonGap = 1

// headerRightMargin is the fixed padding between the quit button and the
// right edge of the header row.
const headerRightMargin = 1

// renderHeaderButton renders a header button at the given width with its
// symbol and label centered.
func renderHeaderButton(style lipgloss.Style, label string, width int) string {
	free := width - ansi.StringWidth(style.Render(label))
	if free <= 0 {
		return style.Render(label)
	}
	return style.Render(strings.Repeat(" ", free/2) + label + strings.Repeat(" ", free-free/2))
}

// headerView renders the header row: the logo on the left, then the
// command-palette button, then the quit button (equal width, symbol and
// label centered), with fixed padding between the two buttons and a fixed
// margin before the right edge.
func (m Model) headerView() string {
	logo := headerStyle.Render("PERK WORKBENCH")
	width := headerButtonWidth()
	button := renderHeaderButton(headerButtonStyle, headerButtonLabel, width)
	quitButton := renderHeaderButton(headerQuitButtonStyle, headerQuitButtonLabel, width)
	buttons := button + strings.Repeat(" ", headerButtonGap) + quitButton + strings.Repeat(" ", headerRightMargin)
	gap := max(m.width-ansi.StringWidth(logo)-ansi.StringWidth(buttons), 0)
	return logo + strings.Repeat(" ", gap) + buttons
}

// tableFormOpen reports whether the table popup is visible: open and not
// mid-execution (the retained form hides while its DDL query runs).
func (m Model) tableFormOpen() bool { return m.tableForm.active() && !m.tableFormRunning }

func (m Model) hasConfirming() bool {
	return m.explainPicker != nil || m.quitDialog != nil || m.queryConfirmation != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() || m.connection.confirmation != nil || m.tableFormOpen() ||
		(m.cellEditor != nil && m.cellEditor.confirming) ||
		(m.chat.activeRun().pendingWrite != nil && m.chat.activeRun().pendingWrite.dialog != nil)
}

func (m Model) activeConfirmation() *confirmationDialog {
	switch {
	case m.queryConfirmation != nil:
		return m.queryConfirmation.dialog
	case m.quitDialog != nil:
		return m.quitDialog
	case m.columnForm.confirming():
		return m.columnForm.confirmation
	case m.browseForm.confirming():
		return m.browseForm.confirmation
	case m.indexForm.confirming():
		return m.indexForm.confirmation
	case m.foreignKeyForm.confirming():
		return m.foreignKeyForm.confirmation
	case m.connection.confirmation != nil:
		return m.connection.confirmation
	case m.tableFormOpen() && m.tableForm.confirming():
		return m.tableForm.confirmation
	case m.deleteConfirm != nil:
		return m.deleteConfirm
	case m.cellEditor != nil && m.cellEditor.confirming:
		return m.cellEditor.confirm
	case m.chat.activeRun().pendingWrite != nil && m.chat.activeRun().pendingWrite.dialog != nil:
		return m.chat.activeRun().pendingWrite.dialog
	default:
		return nil
	}
}

func (m Model) hasOverlay() bool {
	return m.commandPalette.visible || m.themePicker != nil || m.tableTargetPicker != nil || m.queryLogDetail != nil || m.notificationHistory != nil || m.notificationDetail != nil || m.explainPicker != nil || m.chatHistoryPicker != nil || m.quitDialog != nil || m.cellEditor != nil || m.cellViewer != nil || m.contextMenu != nil || m.deleteConfirm != nil || m.hasConfirming()
}

func (m Model) confirmContent() string {
	var raw string
	switch {
	case m.cellEditor != nil:
		return m.cellEditor.confirmContent()
	case m.queryConfirmation != nil:
		raw = m.queryConfirmation.dialog.content(m.width)
	case m.explainPicker != nil:
		raw = m.explainPicker.form.View()
	case m.tableFormOpen():
		raw = m.tableForm.View()
	case m.chatHistoryPicker != nil:
		raw = m.chatHistoryPicker.View()
	case m.quitDialog != nil:
		raw = m.quitDialog.content(m.width)
	case m.columnForm.confirming():
		raw = m.columnForm.confirmation.content(m.width)
	case m.browseForm.confirming():
		raw = m.browseForm.confirmation.content(m.width)
	case m.indexForm.confirming():
		raw = m.indexForm.confirmation.content(m.width)
	case m.foreignKeyForm.confirming():
		raw = m.foreignKeyForm.confirmation.content(m.width)
	case m.connection.confirmation != nil:
		raw = m.connection.confirmation.content(m.width)
	case m.deleteConfirm != nil:
		raw = m.deleteConfirm.content(m.width)
	case m.chat.activeRun().pendingWrite != nil:
		return m.chat.activeRun().pendingWrite.dialog.content(m.width)
	}
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for i, line := range strings.Split(raw, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " "))
	}
	return b.String()
}

func (m Model) drawCellViewer(canvas uv.ScreenBuffer) {
	cv := m.cellViewer
	if cv == nil {
		return
	}
	bounds := canvas.Bounds()

	padX := 2
	title := "View " + cv.column
	titleWidth := ansi.StringWidth(title)

	// Body height = actual rendered lines, capped at scroll window.
	// Short content makes a compact dialog.
	vpContent := cv.viewport.View()
	vpLines := strings.Split(vpContent, "\n")
	bodyRows := max(min(len(vpLines), cv.viewport.Height()), 1)

	viewW := cv.viewport.Width()
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
	panelStyle := uv.Style{Bg: chrome.ParseHex(colorPanel)}
	bgCell := uv.Cell{Content: " ", Width: 1, Style: panelStyle}
	canvas.FillArea(&bgCell, image.Rect(x, y, x+borderW, y+borderH))

	// Border
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
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
	titleStyle := uv.Style{Fg: chrome.ParseHex(colorSecondary), Bg: chrome.ParseHex(colorPanel), Attrs: uv.AttrBold}
	ink := uv.Style{Fg: chrome.ParseHex(colorInk), Bg: chrome.ParseHex(colorPanel)}
	muted := uv.Style{Fg: chrome.ParseHex(colorMuted), Bg: chrome.ParseHex(colorPanel)}

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

	vPct := int(cv.viewport.ScrollPercent() * 100)
	hPct := int(cv.viewport.HorizontalScrollPercent() * 100)
	pctStr := fmt.Sprintf("V:%d%% H:%d%%", vPct, hPct)
	pctWidth := ansi.StringWidth(pctStr)
	pctX := cx0 + innerW - padX - pctWidth
	if pctX < cx0+padX {
		pctX = cx0 + padX
	}
	drawConfirmationText(canvas, pctStr, pctX, footerY, muted)
}

func (m Model) drawConfirmDialog(canvas uv.ScreenBuffer, dialog string) {
	dialogStr := uv.NewStyledString(dialog)
	dialogW := min(dialogStr.UnicodeWidth(), canvas.Bounds().Dx()-6)
	dialogH := min(dialogStr.Height(), canvas.Bounds().Dy()-6)
	if dialogW <= 0 || dialogH <= 0 {
		return
	}
	bounds := canvas.Bounds()
	borderW := dialogW + 2
	borderH := dialogH + 2
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

	dialogStr.Draw(canvas, image.Rect(x+1, y+1, x+1+dialogW, y+1+dialogH))
}

func (m Model) drawContextMenu(canvas uv.ScreenBuffer) {
	menu := m.contextMenu
	if menu == nil || !menu.visible || len(menu.options) == 0 {
		return
	}

	maxLabel := 0
	maxKeys := 0
	for _, opt := range menu.options {
		if len(opt.label) > maxLabel {
			maxLabel = len(opt.label)
		}
		if len(opt.keys) > maxKeys {
			maxKeys = len(opt.keys)
		}
	}
	const title = "Row actions"
	pad := 2
	keyGap := 2
	optWidth := maxLabel + pad
	keyColWidth := maxKeys + keyGap
	contentWidth := max(optWidth+keyColWidth, len(title)+pad, 24)
	borderW := contentWidth + 2

	bounds := canvas.Bounds()
	menuX := menu.x
	menuY := menu.y
	if menuX+borderW > bounds.Dx() {
		menuX = bounds.Dx() - borderW
	}
	totalH := 4 + len(menu.options)
	if menuY+totalH > bounds.Dy() {
		menuY = bounds.Dy() - totalH
	}
	menuX = max(0, menuX)
	menuY = max(0, menuY)

	bg := uv.Style{Bg: chrome.ParseHex(colorPanel)}
	selectedBg := uv.Style{Bg: chrome.ParseHex(colorPrimary), Fg: chrome.ParseHex(colorCanvas)}
	titleFg := uv.Style{Fg: chrome.ParseHex(colorSecondary)}
	inkFg := uv.Style{Fg: chrome.ParseHex(colorInk)}
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}

	bgCell := uv.Cell{Content: " ", Width: 1, Style: bg}
	canvas.FillArea(&bgCell, image.Rect(menuX, menuY, menuX+borderW, menuY+totalH))

	canvas.SetCell(menuX, menuY, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX+borderW-1, menuY, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX, menuY+totalH-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX+borderW-1, menuY+totalH-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})
	for cx := menuX + 1; cx < menuX+borderW-1; cx++ {
		canvas.SetCell(cx, menuY, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, menuY+totalH-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for cy := menuY + 1; cy < menuY+totalH-1; cy++ {
		canvas.SetCell(menuX, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(menuX+borderW-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}

	cx0 := menuX + 1
	titleLine := " " + title + strings.Repeat(" ", contentWidth-len(title)-1)
	for i, ch := range titleLine {
		canvas.SetCell(cx0+i, menuY+1, &uv.Cell{Content: string(ch), Width: 1, Style: titleFg})
	}

	for cx := cx0; cx < menuX+borderW-1; cx++ {
		canvas.SetCell(cx, menuY+2, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}

	mutedFg := uv.Style{Fg: chrome.ParseHex(colorMuted)}

	for idx, opt := range menu.options {
		optY := menuY + 3 + idx
		labelWidth := maxLabel + pad - 1
		labelPart := " " + opt.label + strings.Repeat(" ", labelWidth-len(opt.label))
		keyPart := strings.Repeat(" ", keyColWidth-len(opt.keys)) + opt.keys
		line := labelPart + keyPart
		if len(line) < contentWidth {
			line += strings.Repeat(" ", contentWidth-len(line))
		}
		optStyle := inkFg
		keyStyle := mutedFg
		if idx == menu.selected {
			optStyle = selectedBg
			keyStyle = selectedBg
		}
		for i, ch := range line {
			style := optStyle
			if i >= len(labelPart) && i < len(labelPart)+len(keyPart) {
				style = keyStyle
			}
			canvas.SetCell(cx0+i, optY, &uv.Cell{Content: string(ch), Width: 1, Style: style})
		}
	}
}

func (m Model) drawQueryLogDetail(canvas uv.ScreenBuffer) {
	d := m.queryLogDetail
	if d == nil {
		return
	}
	var statusStr, iconStr string
	switch d.status {
	case "failed":
		statusStr = "Failed"
		iconStr = statusFailedStyle.Render(iconFailed)
	case "canceled":
		statusStr = "Canceled"
		iconStr = statusCanceledStyle.Render(iconCanceled)
	default:
		statusStr = "Success"
		iconStr = statusSuccessStyle.Render(iconSuccess)
	}

	innerW := m.width - 4

	var b strings.Builder
	b.WriteString(headerStyle.Render("  \uf0ca Query Log Detail  "))
	b.WriteString("\n\n")
	b.WriteString("  Time:     ")
	b.WriteString(d.startedAt.Format("2006-01-02 15:04:05"))
	b.WriteString("\n")
	b.WriteString("  Status:   ")
	b.WriteString(iconStr)
	b.WriteString(" ")
	b.WriteString(statusStr)
	b.WriteString("\n")
	b.WriteString("  Duration: ")
	b.WriteString(d.duration.Round(time.Microsecond).String())
	b.WriteString("\n")
	b.WriteString("  Statement:\n    ")
	b.WriteString(ansi.Wordwrap(safeText(chrome.DetailValue(d.statement)), innerW-4, "\n    "))
	b.WriteString("\n")
	b.WriteString("  Message:  ")
	b.WriteString(ansi.Wordwrap(safeText(chrome.DetailValue(d.message)), innerW-14, " "))
	b.WriteString("\n\n  y copy | e explain | enter/esc close")

	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(1, 1, m.width-1, m.height-1))

	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
	for x := 1; x < m.width-1; x++ {
		canvas.SetCell(x, 0, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(x, m.height-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for y := 1; y < m.height-1; y++ {
		canvas.SetCell(0, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(m.width-1, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(0, 0, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(m.width-1, 0, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(0, m.height-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(m.width-1, m.height-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})

	uv.NewStyledString(b.String()).Draw(canvas, image.Rect(1, 1, m.width-1, m.height-1))
}

func (m Model) contentView() string {
	switch m.State {
	case stateConnection:
		if m.compact {
			title, content := "Connection <2>", m.connectionPaneView(max(m.height-6, 0))
			if m.connection.focus == connectionFocusRecent {
				title, content = "Profiles <1>", m.recentPaneView()
			}
			return titledPane(title, content, paneStyle(true).Width(max(m.width-2, 0)).MaxWidth(max(m.width-2, 0)).Height(max(m.height-4, 0)).MaxHeight(max(m.height-4, 0)))
		}
		left := titledPane("Profiles <1>", m.recentPaneView(), paneStyle(m.connection.focus == connectionFocusRecent).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-4, 0)))
		right := titledPane("Connection <2>", m.connectionPaneView(max(m.height-6, 0)), paneStyle(m.connection.focus != connectionFocusRecent).Width(max(m.width-m.schemaWidth, 0)).Height(max(m.height-4, 0)))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	case statePicking:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(m.picker.View())
	case stateOpening:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(statusStyle.Render("opening database"))
	case stateFailure:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(statusStyle.Render(m.Status + "\npress enter to return to the picker"))
	}
	if m.compact {
		width, height := max(1, m.width-2), max(1, m.height-4)
		switch m.Focus {
		case focusSchema:
			return titledPane("Databases <1>", m.schemaPaneBody(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusWorkspace:
			return titledPane("Workspace <2>", m.workspaceView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusQueryLog:
			return titledPane("Query Log <3>", m.queryLogContentView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusChat:
			return titledPane("Assistant <4>", m.chatContentView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		}
	}
	left := titledPane("Databases <1>", m.schemaPaneBody(), paneStyle(m.Focus == focusSchema).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-2, 0)))
	center := lipgloss.JoinVertical(lipgloss.Left, m.rightView(), m.queryLogPaneView())
	if !m.chat.visible {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, center)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, center, m.chatPaneView())
}

// schemaPaneBody is the sidebar body: the persistent filter row above the
// schema list. The row is omitted when the pane is too narrow to show it.
func (m Model) schemaPaneBody() string {
	if row := m.schemaFilterRow(); row != "" {
		return row + "\n" + m.schema.View()
	}
	return m.schema.View()
}

func (m Model) rightView() string {
	return titledPane("Workspace <2>", m.workspaceView(), paneStyle(m.Focus == focusWorkspace).Width(max(m.editorWidth-2, 0)).Height(max(m.workspaceHeight, 0)))
}

func (m Model) queryLogPaneView() string {
	return titledPane("Query Log <3>", m.queryLogContentView(), paneStyle(m.Focus == focusQueryLog).Width(max(m.editorWidth-2, 0)).Height(max(m.queryLogHeight, 0)))
}

func (m Model) chatPaneView() string {
	return titledPane("Assistant <4>", m.chatContentView(), paneStyle(m.Focus == focusChat).Width(max(m.chatWidth-2, 0)).Height(max(m.height-2, 0)))
}

func (m Model) chatContentView() string {
	view := m.chat.viewport.View()
	if dropdown := m.chatCompletionOverlay(); dropdown != "" {
		lines := strings.Split(view, "\n")
		overlayLines := strings.Split(dropdown, "\n")
		start := len(lines) - len(overlayLines)
		if start < 0 {
			overlayLines = overlayLines[len(overlayLines)-len(lines):]
			start = 0
		}
		copy(lines[start:], overlayLines)
		view = strings.Join(lines, "\n")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		view,
		lipgloss.NewStyle().Padding(1, 0).Render(m.chat.input.View()),
		m.chatModeBadge(),
	)
}

func (m Model) queryLogContentView() string {
	content := tableViewportViewWithAlignment(m.queryLog, nil, m.queryLogOffset, m.tableViewportWidth, m.queryLogColumn)
	summary := m.queryLogSummary() + colsHint(m.queryLog.Columns(), m.tableViewportWidth)
	padding := max(m.queryLogHeight-1-lipgloss.Height(content)-1, 0)
	return content + strings.Repeat("\n", padding+1) +
		chrome.PaneStatus(statusStyle.Render("n/p page"), statusStyle.Render(summary), m.tableViewportWidth)
}

func (m Model) workspaceView() string {
	tabs := []string{"SQL", "Browse", "Columns", "Indexes", "Foreign Keys"}
	for index := range tabs {
		if workspaceTab(index) == m.Tab {
			tabs[index] = connectionActionSelectedStyle.Render(tabs[index])
		} else {
			tabs[index] = statusStyle.Render(tabs[index])
		}
	}
	var content string
	switch m.Tab {
	case tabStructure:
		content = m.structureView()
	case tabBrowse:
		content = m.browseView()
	case tabSQL:
		content = m.sqlPaneView()
	case tabIndexes:
		content = m.indexesView()
	case tabForeignKeys:
		content = m.foreignKeysView()
	}
	modeLine := m.modeBadge()
	if m.compact && m.SelectedTable != "" {
		modeLine += "  " + statusStyle.Render(m.SelectedTable)
	}
	footer := modeLine + " " + statusStyle.Render("L/H tabs")
	if m.formTabActive() {
		return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, formButtonsBar(m.formMode.buttonsFocused, m.formMode.buttonChoice), "", footer)
	}
	// A blank line separates the tab's status line from the mode/tab-hint
	// footer; the browse tab renders that gap again between its status
	// line and the pager button row (see browseView).
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, "", footer)
}

func (m Model) sqlPaneView() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		sqlEditorBox(m.editor.View(), m.editorBorderColor()),
		tableViewportViewWithAlignment(m.results, m.resultsNumericColumns, m.resultsOffset, m.tableViewportWidth, m.resultsColumn),
	)

	if dropdown := m.completionOverlay(); dropdown != "" {
		lines := strings.Split(content, "\n")
		overlayLines := strings.Split(dropdown, "\n")
		// +2: dropdown sits below the cursor line, plus the editor's own border row.
		startLine := max(m.completionCursorOffset()+2, 1)
		for i, ol := range overlayLines {
			if startLine+i < len(lines) {
				lines[startLine+i] = ol
			}
		}
		content = strings.Join(lines, "\n")
	}

	return content + "\n" + chrome.PaneStatus("", m.resultsStatus, m.tableViewportWidth)
}

// sqlEditorBox frames the SQL input; its border color mirrors the live
// validity of the current statement.
func sqlEditorBox(view, borderColor string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(borderColor)).Render(view)
}

func (m Model) editorBorderColor() string {
	switch m.editorValidity {
	case sqlValidityValid:
		return colorSuccess
	case sqlValidityInvalid:
		return colorDanger
	default:
		return colorBorder
	}
}

func (m Model) structureView() string {
	if m.columnForm.active() {
		return m.formViewport(m.columnForm.View(), m.columnForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.structure, nil, m.structureOffset, m.tableViewportWidth, -1) + "\n" + chrome.PaneStatus(m.tableFilterStatus(tabStructure), "", m.tableViewportWidth)
}

func (m Model) browseView() string {
	if m.browseFilterForm != nil {
		// The filter view is already windowed at its scroll offset (it
		// renders at most one screenful per frame), so the viewport slice
		// must not re-apply the offset.
		return m.formViewport(m.browseFilterForm.View(), 0)
	}
	if m.browseForm.active() {
		return m.formViewport(m.browseForm.View(), m.browseForm.scrollOffset)
	}
	view := tableViewportViewWithAlignment(m.browse, m.browseNumericColumns, m.browseOffset, m.tableViewportWidth, m.browseColumn) + "\n" + m.browseStatusLine() + "\n\n" + m.browsePagerLine()
	return view
}

// browseStatusLine renders the browse status line: the keyboard hints on
// the left, the row-range summary on the right. Both segments are
// truncated so the line always fits the viewport width: PaneStatus wraps
// overflowing text, which would push the pager button row below the fixed
// row the click handler tests. The n/p page hint is not offered because
// the pager button row below always renders that affordance.
func (m Model) browseStatusLine() string {
	width := m.tableViewportWidth
	left := "/ filter | r reset | s sort column"
	if ansi.StringWidth(left)+2 > width {
		left = ansi.Truncate(left, max(width-2, 0), "…")
	}
	right := m.browseStatus
	remaining := width - ansi.StringWidth(left) - 2
	if remaining < 2 {
		// Fewer than two cells left: even the styled empty segment would
		// overflow, so drop the summary entirely.
		return statusStyle.Render(left)
	}
	if ansi.StringWidth(right)+2 > remaining {
		right = ansi.Truncate(right, remaining-2, "…")
	}
	return chrome.PaneStatus(statusStyle.Render(left), statusStyle.Render(right), width)
}

// browsePrevLabel and browseNextLabel are the pager button labels on the
// browse button row.
const (
	browsePrevLabel = "◀ Prev"
	browseNextLabel = "Next ▶"
)

// browsePager describes the pager button row under the browse status line:
// the rendered row, each button's content-x span (pane content coordinates,
// including the row's single-cell left padding), and whether each button is
// enabled. Prev is pinned to the left edge and Next to the right edge of
// the row, so enabling or disabling a button never moves the other. A
// disabled button renders in the secondary color and ignores clicks; an
// enabled one renders in the primary color and pages on click. The view
// and the click hit-test share this one source of truth.
type browsePager struct {
	line                 string
	prev, next           string
	prevStart, nextStart int
	prevEnabled          bool
	nextEnabled          bool
}

func (m Model) browsePager() browsePager {
	pager := browsePager{
		prev:        formCancelButtonStyle.Render(browsePrevLabel),
		next:        formCancelButtonStyle.Render(browseNextLabel),
		prevEnabled: m.BrowsePage > 0,
		nextEnabled: m.browseResult.HasMore,
	}
	if pager.prevEnabled {
		pager.prev = formSaveButtonStyle.Render(browsePrevLabel)
	}
	if pager.nextEnabled {
		pager.next = formSaveButtonStyle.Render(browseNextLabel)
	}
	gap := max(m.tableViewportWidth-2-ansi.StringWidth(pager.prev)-ansi.StringWidth(pager.next), 0)
	pager.prevStart = 1 // statusStyle pads the row by one cell on each side
	pager.nextStart = 1 + ansi.StringWidth(pager.prev) + gap
	pager.line = statusStyle.Render(pager.prev + strings.Repeat(" ", gap) + pager.next)
	return pager
}

// browsePagerLine returns the pager button row.
func (m Model) browsePagerLine() string {
	return m.browsePager().line
}

func (m Model) formViewport(view string, offset int) string {
	height := m.formViewportHeight()
	lines := strings.Split(view, "\n")
	if len(lines) <= height {
		return view
	}
	offset = min(max(offset, 0), len(lines)-height)
	return strings.Join(lines[offset:offset+height], "\n")
}

func (m Model) indexesView() string {
	if m.indexForm.active() {
		return m.formViewport(m.indexForm.View(), m.indexForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.indexes, nil, m.indexesOffset, m.tableViewportWidth, -1) + "\n" + chrome.PaneStatus(m.tableFilterStatus(tabIndexes), "", m.tableViewportWidth)
}

func (m Model) foreignKeysView() string {
	if m.foreignKeyForm.active() {
		return m.formViewport(m.foreignKeyForm.View(), m.foreignKeyForm.scrollOffset)
	}
	if m.relationshipDiagram {
		return m.relationshipView()
	}
	return tableViewportViewWithAlignment(m.foreignKeys, nil, m.foreignKeysOffset, m.tableViewportWidth, -1) + "\n" + chrome.PaneStatus(m.tableFilterStatus(tabForeignKeys), "", m.tableViewportWidth)
}

func (m Model) footer() string {
	if m.State == stateConnection {
		quitKey := m.keybindings.DisplayKey("app.quit")
		quitHint := chrome.FormatFooterKey(quitKey) + " quit"
		return safeText("1 profiles | 2 form | tab controls | " + quitHint)
	}
	if m.State == stateReady {
		quitKey := m.keybindings.DisplayKey("app.quit_dialog")
		quitHint := chrome.FormatFooterKey(quitKey) + " quit"
		parts := []string{}
		if m.ReadOnly {
			parts = append(parts, "READONLY")
		}
		if m.databaseInfo.Product != "" && m.databaseInfo.Version != "" {
			parts = append(parts, m.databaseInfo.Product+" "+m.databaseInfo.Version)
		}
		parts = append(parts, "f fullscreen", "^p palette")
		parts = append(parts, quitHint)
		return safeText(strings.Join(parts, " | "))
	}
	quitKey := m.keybindings.DisplayKey("app.quit")
	quitHint := chrome.FormatFooterKey(quitKey) + " quit"
	return safeText(quitHint)
}

func (m Model) modeBadge() string {
	badge := ""
	if m.vimMode {
		// The modal INSERT/NORMAL state only exists in vim mode.
		if m.formMode.editing() {
			badge = modeInsertStyle.Render("INSERT")
		} else {
			badge = modeNormalStyle.Render("NORMAL")
		}
	}
	if m.ReadOnly && m.State == stateReady {
		if badge != "" {
			badge += " "
		}
		badge += readOnlyStyle.Render("READONLY")
	}
	return badge
}

func (m Model) chatModeBadge() string {
	left := ""
	if m.vimMode {
		// The modal INSERT/NORMAL state only exists in vim mode.
		if m.chat.chatMode == formModeInsert {
			left = modeInsertStyle.Render("INSERT")
		} else {
			left = modeNormalStyle.Render("NORMAL")
		}
	}
	right := ""
	run := m.chat.activeRun()
	if run.loading {
		right = chatSpinnerFrames[run.spinnerFrame%len(chatSpinnerFrames)]
	}
	if m.chat.yoloWrites {
		if right != "" {
			right += " "
		}
		right += statusFailedStyle.Render("YOLO")
	}
	if right == "" {
		return left
	}
	return chrome.PaneStatus(left, right, m.chat.viewport.Width())
}
