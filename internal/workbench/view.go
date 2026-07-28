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
	fullContent := lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render("PERK WORKBENCH"), content, footerStyle.Render(m.footer()))
	if m.commandPalette.visible || m.themePicker != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.themePicker != nil {
			m.drawConfirmDialog(canvas, m.themePicker.content())
		} else {
			m.commandPalette.paletteDraw(canvas, m.width, m.height)
		}
		view.SetContent(canvas.Render())
		return view
	}
	if m.queryLogDetail != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		m.drawQueryLogDetail(canvas)
		view.SetContent(canvas.Render())
		return view
	}
	if m.cellEditor != nil || m.explainPicker != nil || m.savedQueryPicker != nil || m.chatHistoryPicker != nil || m.quitDialog != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() ||
		m.connection.confirmation != nil || m.contextMenu != nil || m.deleteConfirm != nil || m.queryConfirmation != nil {
		canvas := uv.NewScreenBuffer(m.width, m.height)
		screen.Clear(canvas)
		uv.NewStyledString(fullContent).Draw(canvas, canvas.Bounds())
		if m.contextMenu != nil {
			m.drawContextMenu(canvas)
		} else if dialog := m.activeConfirmation(); dialog != nil {
			dialog.draw(canvas)
		} else if dialog := m.confirmContent(); dialog != "" {
			m.drawConfirmDialog(canvas, dialog)
		}
		view.SetContent(canvas.Render())
		return view
	}
	view.SetContent(fullContent)
	return view
}

func (m Model) hasConfirming() bool {
	return m.explainPicker != nil || m.quitDialog != nil || m.queryConfirmation != nil || m.columnForm.confirming() || m.indexForm.confirming() ||
		m.foreignKeyForm.confirming() || m.browseForm.confirming() || m.connection.confirmation != nil ||
		(m.cellEditor != nil && m.cellEditor.confirming)
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
	case m.deleteConfirm != nil:
		return m.deleteConfirm
	case m.cellEditor != nil && m.cellEditor.confirming:
		return m.cellEditor.confirm
	default:
		return nil
	}
}

func (m Model) hasOverlay() bool {
	return m.commandPalette.visible || m.themePicker != nil || m.queryLogDetail != nil || m.explainPicker != nil || m.chatHistoryPicker != nil || m.quitDialog != nil || m.cellEditor != nil || m.contextMenu != nil || m.deleteConfirm != nil || m.hasConfirming()
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
	case m.savedQueryPicker != nil:
		raw = m.savedQueryPicker.form.View()
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
	canvas.SetCell(x, y, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(x, y+borderH-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(x+borderW-1, y+borderH-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})
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
	selectedBg := uv.Style{Bg: chrome.ParseHex(colorAccent), Fg: chrome.ParseHex(colorCanvas)}
	inkFg := uv.Style{Fg: chrome.ParseHex(colorInk)}
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}

	bgCell := uv.Cell{Content: " ", Width: 1, Style: bg}
	canvas.FillArea(&bgCell, image.Rect(menuX, menuY, menuX+borderW, menuY+totalH))

	canvas.SetCell(menuX, menuY, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX+borderW-1, menuY, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX, menuY+totalH-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(menuX+borderW-1, menuY+totalH-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})
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
		canvas.SetCell(cx0+i, menuY+1, &uv.Cell{Content: string(ch), Width: 1, Style: inkFg})
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
	canvas.SetCell(0, 0, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(m.width-1, 0, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(0, m.height-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(m.width-1, m.height-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})

	uv.NewStyledString(b.String()).Draw(canvas, image.Rect(1, 1, m.width-1, m.height-1))
}

func (m Model) contentView() string {
	switch m.State {
	case stateConnection:
		if m.compact {
			content := m.connectionPaneView(max(m.height-6, 0))
			if m.connection.focus == connectionFocusRecent {
				content = m.recent.View()
			}
			return compactPane(content, max(m.width-2, 0), max(m.height-4, 0))
		}
		left := paneStyle(m.connection.focus == connectionFocusRecent).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.recent.View())
		right := paneStyle(m.connection.focus != connectionFocusRecent).Width(max(m.editorWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.connectionPaneView(max(m.height-6, 0)))
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
			return titledPane("Databases", m.schema.View(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusWorkspace:
			return titledPane("Workspace", m.workspaceView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusQueryLog:
			return titledPane("Query Log", m.queryLogContentView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		case focusChat:
			return titledPane("AI", m.chatContentView(), paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height))
		}
	}
	left := titledPane("Databases", m.schema.View(), paneStyle(m.Focus == focusSchema).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-2, 0)))
	center := lipgloss.JoinVertical(lipgloss.Left, m.rightView(), m.queryLogPaneView())
	if !m.chat.visible {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, center)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, center, m.chatPaneView())
}

func (m Model) rightView() string {
	return titledPane("Workspace", m.workspaceView(), paneStyle(m.Focus == focusWorkspace).Width(max(m.editorWidth-2, 0)).Height(max(m.workspaceHeight, 0)))
}

func (m Model) queryLogPaneView() string {
	return titledPane("Query Log", m.queryLogContentView(), paneStyle(m.Focus == focusQueryLog).Width(max(m.editorWidth-2, 0)).Height(max(m.queryLogHeight, 0)))
}

func (m Model) chatPaneView() string {
	return titledPane("AI", m.chatContentView(), paneStyle(m.Focus == focusChat).Width(max(m.chatWidth-2, 0)).Height(max(m.height-2, 0)))
}

func (m Model) chatContentView() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.chat.viewport.View(),
		m.chat.input.View(),
		m.chatModeBadge(),
	)
}

func (m Model) queryLogContentView() string {
	content := tableViewportViewWithAlignment(m.queryLog, nil, m.queryLogOffset, m.tableViewportWidth, m.queryLogColumn)
	summary := m.queryLogSummary() + colsHint(m.queryLog.Columns(), m.tableViewportWidth)
	padding := max(m.queryLogHeight-1-lipgloss.Height(content)-1, 0)
	return content + strings.Repeat("\n", padding+1) +
		chrome.PaneStatus(statusStyle.Render("y copy cell | enter detail | e explain"), statusStyle.Render(summary), m.tableViewportWidth)
}

func (m Model) workspaceView() string {
	tabs := []string{"SQL", "Columns", "Browse", "Indexes", "Foreign Keys"}
	for index := range tabs {
		if workspaceTab(index) == m.Tab {
			tabs[index] = headerStyle.Render(tabs[index])
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
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), "", content, modeLine+" "+statusStyle.Render("L/H tabs"))
}

func (m Model) sqlPaneView() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.editor.View(),
		tableViewportViewWithAlignment(m.results, m.resultsNumericColumns, m.resultsOffset, m.tableViewportWidth, m.resultsColumn),
	)

	if dropdown := m.completionOverlay(); dropdown != "" {
		lines := strings.Split(content, "\n")
		overlayLines := strings.Split(dropdown, "\n")
		startLine := max(m.completionCursorOffset()+1, 1)
		for i, ol := range overlayLines {
			if startLine+i < len(lines) {
				lines[startLine+i] = ol
			}
		}
		content = strings.Join(lines, "\n")
	}

	return content + "\n" + chrome.PaneStatus("", m.resultsStatus, m.tableViewportWidth)
}

func (m Model) structureView() string {
	if m.columnForm.active() {
		return m.formViewport(m.columnForm.View(), m.columnForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.structure, nil, m.structureOffset, m.tableViewportWidth, m.structureColumn)
}

func (m Model) browseView() string {
	if m.browseForm.active() {
		return m.formViewport(m.browseForm.View(), m.browseForm.scrollOffset)
	}
	return tableViewportViewWithAlignment(m.browse, m.browseNumericColumns, m.browseOffset, m.tableViewportWidth, m.browseColumn) + "\n" + chrome.PaneStatus("", m.browseStatus, m.tableViewportWidth)
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
		return m.indexForm.View()
	}
	return tableViewportViewWithAlignment(m.indexes, nil, m.indexesOffset, m.tableViewportWidth, m.indexesColumn)
}

func (m Model) foreignKeysView() string {
	if m.foreignKeyForm.active() {
		return m.foreignKeyForm.View()
	}
	if m.relationshipDiagram {
		return m.relationshipView()
	}
	return tableViewportViewWithAlignment(m.foreignKeys, nil, m.foreignKeysOffset, m.tableViewportWidth, m.foreignKeysColumn)
}

func (m Model) footer() string {
	if m.State == stateConnection {
		quitKey := m.keybindings.DisplayKey("app.quit")
		quitHint := chrome.FormatFooterKey(quitKey) + " quit"
		return safeText(m.Status + " | 1 profiles | 2 form | tab controls | a add | e edit | d delete | / filter | " + quitHint)
	}
	if m.State == stateReady {
		quitKey := m.keybindings.DisplayKey("app.quit_dialog")
		quitHint := chrome.FormatFooterKey(quitKey) + " quit"
		parts := []string{}
		if m.Status != "" {
			parts = append(parts, m.Status)
		}
		if m.databaseInfo.Product != "" && m.databaseInfo.Version != "" {
			parts = append(parts, m.databaseInfo.Product+" "+m.databaseInfo.Version)
		}
		parts = append(parts, "1 tables", "2 tabs", "3 history")
		if m.chat.visible {
			parts = append(parts, "4 AI", "^g toggle AI")
		}
		parts = append(parts, "f fullscreen", "^p palette")
		parts = append(parts, quitHint)
		return safeText(strings.Join(parts, " | "))
	}
	quitKey := m.keybindings.DisplayKey("app.quit")
	quitHint := chrome.FormatFooterKey(quitKey) + " quit"
	return safeText(m.Status + " | " + quitHint)
}

func (m Model) modeBadge() string {
	if m.formMode.editing() {
		return modeInsertStyle.Render("INSERT")
	}
	return modeNormalStyle.Render("NORMAL")
}

func (m Model) chatModeBadge() string {
	if m.chat.chatMode == formModeInsert {
		return modeInsertStyle.Render("INSERT")
	}
	return modeNormalStyle.Render("NORMAL")
}
