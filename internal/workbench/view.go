package workbench

import (
	"image"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	view.KeyboardEnhancements.ReportEventTypes = true
	view.MouseMode = tea.MouseModeCellMotion
	if m.layout.height < 4 || m.layout.width < 1 {
		view.SetContent(headerStyle.Render("PERK WORKBENCH"))
		return view
	}
	content := m.contentView()
	fullContent := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), content, footerStyle.Render(m.footer()))
	if m.overlay.commandPalette.visible || m.overlay.themePicker != nil || m.overlay.tableTargetPicker != nil {
		canvas := uv.NewScreenBuffer(m.layout.width, m.layout.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.overlay.themePicker != nil {
			m.drawConfirmDialog(canvas, m.overlay.themePicker.content())
		} else if m.overlay.tableTargetPicker != nil {
			m.drawConfirmDialog(canvas, m.overlay.tableTargetPicker.content())
		} else {
			m.overlay.commandPalette.paletteDraw(canvas, m.layout.width, m.layout.height)
		}
		m.notifications.component.Draw(canvas, notificationLayout(m))
		view.SetContent(canvas.Render())
		return view
	}
	if m.notifications.component.HistoryOpen() {
		canvas := uv.NewScreenBuffer(m.layout.width, m.layout.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		m.notifications.component.Draw(canvas, notificationLayout(m))
		view.SetContent(canvas.Render())
		return view
	}
	if m.notifications.component.DetailOpen() {
		canvas := uv.NewScreenBuffer(m.layout.width, m.layout.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		m.notifications.component.Draw(canvas, notificationLayout(m))
		view.SetContent(canvas.Render())
		return view
	}
	if m.queryLog.component.DetailOpen() {
		canvas := uv.NewScreenBuffer(m.layout.width, m.layout.height)
		screen.Clear(canvas)
		m.queryLog.component.Draw(canvas, uikit.Layout{Width: m.layout.width, Height: m.layout.height})
		m.notifications.component.Draw(canvas, notificationLayout(m))
		view.SetContent(canvas.Render())
		return view
	}
	if m.browse.component.CellEditor != nil || m.browse.component.CellViewer != nil || m.overlay.explainPicker != nil || m.chat.component.HistoryPicker != nil || m.overlay.quitDialog != nil || m.structure.columnForm.confirming() || m.structure.indexForm.confirming() ||
		m.structure.foreignKeyForm.confirming() || m.browse.component.Form.Confirming() ||
		m.connection.component.Form.Confirmation != nil || m.overlay.contextMenu != nil || m.overlay.deleteConfirm != nil || m.overlay.queryConfirmation != nil || m.hasConfirming() {
		canvas := uv.NewScreenBuffer(m.layout.width, m.layout.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.overlay.contextMenu != nil {
			m.drawContextMenu(canvas)
		} else if dialog := m.activeConfirmation(); dialog != nil {
			dialog.Draw(canvas)
		} else if dialog := m.confirmContent(); dialog != "" {
			m.drawConfirmDialog(canvas, dialog)
		} else if m.browse.component.CellViewer != nil {
			m.drawCellViewer(canvas)
		}
		m.notifications.component.Draw(canvas, notificationLayout(m))
		view.SetContent(canvas.Render())
		return view
	}
	if m.notifications.component.PopupOpen() {
		canvas := uv.NewScreenBuffer(m.layout.width, m.layout.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		m.notifications.component.Draw(canvas, notificationLayout(m))
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
	gap := max(m.layout.width-ansi.StringWidth(logo)-ansi.StringWidth(buttons), 0)
	return logo + strings.Repeat(" ", gap) + buttons
}

// tableFormOpen reports whether the table popup is visible: open and not
// mid-execution (the retained form hides while its DDL query runs).
func (m Model) tableFormOpen() bool {
	return m.structure.tableForm.active() && !m.structure.tableFormRunning
}

func (m Model) hasConfirming() bool {
	return m.overlay.explainPicker != nil || m.overlay.quitDialog != nil || m.overlay.queryConfirmation != nil || m.structure.columnForm.confirming() || m.structure.indexForm.confirming() ||
		m.structure.foreignKeyForm.confirming() || m.browse.component.Form.Confirming() || m.connection.component.Form.Confirmation != nil || m.tableFormOpen() ||
		(m.browse.component.CellEditor != nil && m.browse.component.CellEditor.Confirming) ||
		(m.browse.component.DocumentEditor != nil && m.browse.component.DocumentEditor.Confirming) ||
		(m.chat.writeConfirmation != nil)
}

func (m Model) activeConfirmation() *confirmationDialog {
	switch {
	case m.overlay.queryConfirmation != nil:
		return m.overlay.queryConfirmation.dialog
	case m.overlay.quitDialog != nil:
		return m.overlay.quitDialog
	case m.structure.columnForm.confirming():
		return m.structure.columnForm.confirmation
	case m.browse.component.Form.Confirming():
		return m.browse.component.Form.Confirmation
	case m.structure.indexForm.confirming():
		return m.structure.indexForm.confirmation
	case m.structure.foreignKeyForm.confirming():
		return m.structure.foreignKeyForm.confirmation
	case m.connection.component.Form.Confirmation != nil:
		return m.connection.component.Form.Confirmation
	case m.tableFormOpen() && m.structure.tableForm.confirming():
		return m.structure.tableForm.confirmation
	case m.overlay.deleteConfirm != nil:
		return m.overlay.deleteConfirm
	case m.browse.component.CellEditor != nil && m.browse.component.CellEditor.Confirming:
		return m.browse.component.CellEditor.Confirm
	case m.browse.component.DocumentEditor != nil && m.browse.component.DocumentEditor.Confirming:
		return m.browse.component.DocumentEditor.Confirmation
	case m.chat.writeConfirmation != nil:
		return m.chat.writeConfirmation
	default:
		return nil
	}
}

func (m Model) hasOverlay() bool {
	return m.overlay.commandPalette.visible || m.overlay.themePicker != nil || m.overlay.tableTargetPicker != nil || m.queryLog.component.Detail != nil || m.notifications.component.HistoryOpen() || m.notifications.component.DetailOpen() || m.overlay.explainPicker != nil || m.chat.component.HistoryPicker != nil || m.overlay.quitDialog != nil || m.browse.component.CellEditor != nil || m.browse.component.DocumentEditor != nil || m.browse.component.CellViewer != nil || m.overlay.contextMenu != nil || m.overlay.deleteConfirm != nil || m.hasConfirming()
}

func (m Model) confirmContent() string {
	var raw string
	switch {
	case m.browse.component.CellEditor != nil:
		return m.browse.component.CellEditor.ConfirmContent()
	case m.overlay.queryConfirmation != nil:
		raw = m.overlay.queryConfirmation.dialog.Content(m.layout.width)
	case m.overlay.explainPicker != nil:
		raw = m.overlay.explainPicker.form.View()
	case m.tableFormOpen():
		raw = m.structure.tableForm.View()
	case m.chat.component.HistoryPicker != nil:
		raw = m.chat.component.HistoryPicker.View()
	case m.overlay.quitDialog != nil:
		raw = m.overlay.quitDialog.Content(m.layout.width)
	case m.structure.columnForm.confirming():
		raw = m.structure.columnForm.confirmation.Content(m.layout.width)
	case m.browse.component.Form.Confirming():
		raw = m.browse.component.Form.Confirmation.Content(m.layout.width)
	case m.structure.indexForm.confirming():
		raw = m.structure.indexForm.confirmation.Content(m.layout.width)
	case m.structure.foreignKeyForm.confirming():
		raw = m.structure.foreignKeyForm.confirmation.Content(m.layout.width)
	case m.connection.component.Form.Confirmation != nil:
		raw = m.connection.component.Form.Confirmation.Content(m.layout.width)
	case m.overlay.deleteConfirm != nil:
		raw = m.overlay.deleteConfirm.Content(m.layout.width)
	case m.chat.writeConfirmation != nil:
		return m.chat.writeConfirmation.Content(m.layout.width)
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
	if m.browse.component.CellViewer != nil {
		uikit.DrawCellViewerBox(canvas, m.browse.component.CellViewer)
	}
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
	menu := m.overlay.contextMenu
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
	title := menu.title
	if title == "" {
		title = "Row actions"
	}
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
	d := m.queryLog.component.Detail
	if d == nil {
		return
	}
	var statusStr, iconStr string
	switch d.Status {
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

	innerW := m.layout.width - 4

	var b strings.Builder
	b.WriteString(headerStyle.Render("  \uf0ca Query Log Detail  "))
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
	b.WriteString(ansi.Wordwrap(safeText(chrome.DetailValue(d.Statement)), innerW-4, "\n    "))
	b.WriteString("\n")
	b.WriteString("  Message:  ")
	b.WriteString(ansi.Wordwrap(safeText(chrome.DetailValue(d.Message)), innerW-14, " "))
	b.WriteString("\n\n  y copy | e explain | enter/esc close")

	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(1, 1, m.layout.width-1, m.layout.height-1))

	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
	for x := 1; x < m.layout.width-1; x++ {
		canvas.SetCell(x, 0, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(x, m.layout.height-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	for y := 1; y < m.layout.height-1; y++ {
		canvas.SetCell(0, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(m.layout.width-1, y, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(0, 0, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(m.layout.width-1, 0, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(0, m.layout.height-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(m.layout.width-1, m.layout.height-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})

	uv.NewStyledString(b.String()).Draw(canvas, image.Rect(1, 1, m.layout.width-1, m.layout.height-1))
}

func (m Model) contentView() string {
	switch m.State {
	case stateConnection:
		if m.layout.compact {
			title, content := "Connection <2>", m.connectionPaneView(max(m.layout.height-6, 0))
			if m.connection.component.Form.Focus == connectionFocusRecent {
				title, content = "Profiles <1>", m.recentPaneView()
			}
			return titledPane(title, content, paneStyle(true).Width(max(m.layout.width-2, 0)).MaxWidth(max(m.layout.width-2, 0)).Height(max(m.layout.height-4, 0)).MaxHeight(max(m.layout.height-4, 0)))
		}
		left := titledPane("Profiles <1>", m.recentPaneView(), paneStyle(m.connection.component.Form.Focus == connectionFocusRecent).Width(max(m.layout.schemaWidth-2, 0)).Height(max(m.layout.height-4, 0)))
		right := titledPane("Connection <2>", m.connectionPaneView(max(m.layout.height-6, 0)), paneStyle(m.connection.component.Form.Focus != connectionFocusRecent).Width(max(m.layout.width-m.layout.schemaWidth, 0)).Height(max(m.layout.height-4, 0)))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	case statePicking:
		return paneStyle(true).Width(max(m.layout.width-2, 0)).Height(max(m.layout.height-4, 0)).Render(m.connection.picker.View())
	case stateOpening:
		return paneStyle(true).Width(max(m.layout.width-2, 0)).Height(max(m.layout.height-4, 0)).Render(statusStyle.Render("opening database"))
	case stateFailure:
		return paneStyle(true).Width(max(m.layout.width-2, 0)).Height(max(m.layout.height-4, 0)).Render(statusStyle.Render(m.Status + "\npress enter to return to the picker"))
	}
	if m.layout.compact {
		width, height := max(1, m.layout.width-2), max(1, m.layout.height-4)
		switch m.Focus {
		case focusSchema:
			return titledPane("Databases <1>", m.schemaPaneBody(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusWorkspace:
			return titledPane("Workspace <2>", m.workspaceView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusQueryLog:
			return titledPane("Query Log <3>", m.queryLog.component.View(queryLogLayout(m)), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusChat:
			return titledPane("Assistant <4>", m.chatContentView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		}
	}
	left := titledPane("Databases <1>", m.schemaPaneBody(), paneStyle(m.Focus == focusSchema).Width(max(m.layout.schemaWidth-2, 0)).Height(max(m.layout.height-2, 0)))
	center := lipgloss.JoinVertical(lipgloss.Left, m.rightView(), m.queryLogPaneView())
	if !m.chat.component.Visible {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, center)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, center, m.chatPaneView())
}

// schemaPaneBody is the sidebar body: the persistent filter row above the
// schema list. The row is omitted when the pane is too narrow to show it.
func (m Model) schemaPaneBody() string {
	if row := m.schemaFilterRow(); row != "" {
		return row + "\n" + m.schema.list.View()
	}
	return m.schema.list.View()
}

func (m Model) rightView() string {
	return titledPane("Workspace <2>", m.workspaceView(), paneStyle(m.Focus == focusWorkspace).Width(max(m.layout.editorWidth-2, 0)).Height(max(m.layout.workspaceHeight, 0)))
}

func (m Model) queryLogPaneView() string {
	return titledPane("Query Log <3>", m.queryLog.component.View(queryLogLayout(m)), paneStyle(m.Focus == focusQueryLog).Width(max(m.layout.editorWidth-2, 0)).Height(max(m.layout.queryLogHeight, 0)))
}

func (m Model) chatPaneView() string {
	return titledPane("Assistant <4>", m.chatContentView(), paneStyle(m.Focus == focusChat).Width(max(m.layout.chatWidth-2, 0)).Height(max(m.layout.height-2, 0)))
}

func (m Model) chatContentView() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.chat.component.View(uikit.Layout{
			Width:         max(m.layout.chatWidth-6, 1),
			Height:        max(m.layout.height-4, 1),
			ViewportWidth: max(m.layout.chatWidth-6, 1),
		}),
		m.chatModeBadge(),
	)
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
	if m.layout.compact && m.SelectedTable != "" {
		modeLine += "  " + statusStyle.Render(m.SelectedTable)
	}
	footer := modeLine + " " + statusStyle.Render("L/H tabs")
	if m.formTabActive() {
		return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, formButtonsBar(m.overlay.formMode.ButtonsFocused, m.overlay.formMode.ButtonChoice), "", footer)
	}
	// A blank line separates the tab's status line from the mode/tab-hint
	// footer; the browse tab renders that gap again between its status
	// line and the pager button row (see browseView).
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, "", footer)
}

func (m Model) sqlPaneView() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		sqlEditorBox(m.queryLog.editor.View(), m.editorBorderColor()),
		tableViewportViewWithAlignment(m.queryLog.results, m.queryLog.resultsNumericColumns, m.layout.resultsOffset, m.layout.tableViewportWidth, m.layout.resultsColumn),
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

	return content + "\n" + chrome.PaneStatus("", m.queryLog.resultsStatus, m.layout.tableViewportWidth)
}

// sqlEditorBox frames the SQL input; its border color mirrors the live
// validity of the current statement.
func sqlEditorBox(view, borderColor string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(borderColor)).Render(view)
}

func (m Model) editorBorderColor() string {
	switch m.queryLog.editorValidity {
	case sqlValidityValid:
		return colorSuccess
	case sqlValidityInvalid:
		return colorDanger
	default:
		return colorBorder
	}
}

func (m Model) structureView() string {
	if m.structure.columnForm.active() {
		return m.formViewport(m.structure.columnForm.View(), m.structure.columnForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.structure.table, nil, m.layout.structureOffset, m.layout.tableViewportWidth, -1) + "\n" + chrome.PaneStatus(m.tableFilterStatus(tabStructure), "", m.layout.tableViewportWidth)
}

func (m Model) browseView() string {
	m.browse.component.Page = m.BrowsePage
	return m.browse.component.View(browseLayout(m))
}

// browseStatusSplit reports whether the browse status line renders on two
// lines. The layout decision lives in the browse component, the single
// source of truth the table height and the pager click hit-test mirror;
// the root wrapper keeps the name for the pager hit-test.
func (m Model) browseStatusSplit() bool {
	return m.browse.component.StatusSplit(browseLayout(m))
}

// browseFooterRows is the number of workspace rows the browse view
// reserves below its data rows; see the component for the exact equation.
func (m Model) browseFooterRows() int {
	return m.browse.component.FooterRows(browseLayout(m))
}

func (m Model) browseStatusLine() string {
	return m.browse.component.StatusLine(browseLayout(m))
}

// browsePager is the shared pager button row type from the UI contract
// layer: the rendered line, each button's content-x span, and whether each
// button is enabled. The browse pane and the notification history modal
// both lay their rows out from it.
type browsePager = uikit.Pager

func (m Model) browsePager() browsePager {
	m.browse.component.Page = m.BrowsePage
	return m.browse.component.Pager(browseLayout(m))
}

// browsePagerLine returns the pager button row.
func (m Model) browsePagerLine() string {
	return m.browse.component.PagerLine(browseLayout(m))
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
	if m.structure.indexForm.active() {
		return m.formViewport(m.structure.indexForm.View(), m.structure.indexForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.structure.indexes, nil, m.layout.indexesOffset, m.layout.tableViewportWidth, -1) + "\n" + chrome.PaneStatus(m.tableFilterStatus(tabIndexes), "", m.layout.tableViewportWidth)
}

func (m Model) foreignKeysView() string {
	if m.structure.foreignKeyForm.active() {
		return m.formViewport(m.structure.foreignKeyForm.View(), m.structure.foreignKeyForm.scrollOffset)
	}
	if m.structure.relationshipDiagram {
		return m.relationshipView()
	}
	return tableViewportViewWithAlignment(m.structure.foreignKeys, nil, m.layout.foreignKeysOffset, m.layout.tableViewportWidth, -1) + "\n" + chrome.PaneStatus(m.tableFilterStatus(tabForeignKeys), "", m.layout.tableViewportWidth)
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
		if m.overlay.formMode.Editing() {
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
