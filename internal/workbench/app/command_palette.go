package app

import (
	"image"
	"image/color"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

type commandPaletteItem struct {
	id       CommandID
	label    string
	shortcut string
	scope    scope
}

// commandPaletteSelectMsg is sent when the user selects a command from the palette.
type commandPaletteSelectMsg struct {
	id CommandID
}

// commandPalette is a command palette overlay: type to filter, navigate, select.
type commandPalette struct {
	items        []commandPaletteItem
	filtered     []commandPaletteItem
	cursor       int
	query        []rune
	filtering    bool
	visible      bool
	contextTitle string
	// swallowRelease eats the MouseReleaseMsg that trails a click-select, so
	// the release does not click the pane underneath the closed palette.
	swallowRelease bool
}

// newCommandPalette builds the palette items from the keybinding registry.
func newCommandPalette(m Model) *commandPalette {
	keybindings := m.keybindings
	items := make([]commandPaletteItem, 0, len(keybindings.commands))
	for id, def := range keybindings.commands {
		if id == "app.palette" {
			continue
		}
		if !commandAvailable(id, def, m) {
			continue
		}
		shortcut := ""
		if len(def.keys) > 0 {
			shortcut = displayKey(def.keys[0])
		}
		label := commandLabel(m, id, def.label)
		items = append(items, commandPaletteItem{
			id:       id,
			label:    label,
			shortcut: shortcut,
			scope:    def.scope,
		})
	}
	items = append(items, commandPaletteItem{id: "notifications.show", label: "notifications", scope: scopeGlobal})
	items = append(items, commandPaletteItem{id: "theme.select", label: "theme", scope: scopeGlobal})
	items = append(items, commandPaletteItem{id: "plugin.manage", label: "plugins", scope: scopeGlobal})

	vimLabel := "vim mode: off"
	if m.vimMode {
		vimLabel = "vim mode: on"
	}
	items = append(items, commandPaletteItem{id: "vim.toggle", label: vimLabel, scope: scopeGlobal})
	items = append(items, commandPaletteItem{id: "table.open_target", label: "open table → " + m.tableTargetName(tableOpenTargetTab()), scope: scopeGlobal})

	// Compute context title from current model state
	contextTitle := contextLabel(m)
	sort.Slice(items, func(i, j int) bool {
		if items[i].scope != items[j].scope {
			return items[i].scope < items[j].scope
		}
		return items[i].label < items[j].label
	})

	return &commandPalette{
		items:        items,
		filtered:     append([]commandPaletteItem{}, items...),
		cursor:       0,
		query:        nil,
		visible:      false,
		contextTitle: contextTitle,
	}
}

// contextLabel returns a human-readable label for the current state/focus/tab.
func contextLabel(m Model) string {
	switch m.State {
	case stateConnection:
		return "Connection"
	case statePicking:
		return "Picker"
	case stateOpening:
		return "Opening"
	case stateFailure:
		return "Failure"
	case stateReady:
		switch m.Focus {
		case focusSchema:
			return "Schema"
		case focusWorkspace:
			switch m.Tab {
			case tabStructure:
				return "Columns"
			case tabBrowse:
				return "Browse"
			case tabQuery:
				return m.editorLanguage().EditorLabel
			case tabIndexes:
				return "Indexes"
			case tabForeignKeys:
				return "Foreign Keys"
			case tabCustom:
				if label := m.activeWorkspaceViewLabel(); label != "" {
					return label
				}
			}
		case focusQueryLog:
			return "Query Log"
		case focusChat:
			return "AI Chat"
		}
	}
	return ""
}

// commandLabel returns a disambiguated display label for the palette.
// Commands sharing the same raw label get a pane prefix. On document
// stores the row actions relabel: edit and insert act on the whole
// document.
func commandLabel(m Model, id CommandID, raw string) string {
	// On document stores the row actions relabel: edit and insert act on
	// the whole document.
	if capabilities := m.writeCapabilities(); !capabilities.RowWriter && capabilities.Document != nil {
		switch id {
		case "browse.edit":
			return "edit document"
		case "browse.insert_row":
			return "insert document"
		case "browse.delete_row":
			return "delete document"
		}
	}
	// These labels are unique across scopes — pass through unchanged.
	switch raw {
	case "next tab", "prev tab", "fullscreen", "palette",
		"run", "cancel", "edit column", "edit row", "new index",
		"delete index", "diagram", "new FK", "edit FK", "delete FK",
		"null", "reload", "connect", "action",
		"save", "discard", "delete":
		return raw
	}
	// Disambiguate shared labels by pane context.
	switch id {
	case "focus.schema":
		return "focus schema"
	case "focus.workspace":
		return "focus workspace"
	case "focus.query_log":
		return "focus log"
	case "focus.chat":
		return "focus AI chat"
	case "chat.delete", "chat.clear", "chat.apply_sql":
		return raw
	case "focus.cycle_forward":
		return "cycle focus →"
	case "focus.cycle_backward":
		return "cycle focus ←"
	case "workspace.tab_next":
		return "tab →"
	case "workspace.tab_prev":
		return "tab ←"
	case "schema.select_table":
		return "open table"
	case "schema.expand":
		return "expand level"
	case "schema.collapse":
		return "collapse level"
	case "structure.filter":
		return "filter columns"
	case "structure.reset":
		return "reset column filter"
	case "structure.edit":
		return "edit column"
	case "structure.add":
		return "add column"
	case "structure.delete":
		return "delete column"
	case "browse.edit":
		return "edit row"
	case "browse.insert_row":
		return "insert row"
	case "browse.refine":
		return "filter and row limit"
	case "browse.reset":
		return "reset filters"
	case "browse.sort":
		return "sort column"
	case "browse.next_page":
		return "next page"
	case "browse.prev_page":
		return "prev page"
	case "indexes.filter":
		return "filter indexes"
	case "indexes.reset":
		return "reset index filter"
	case "indexes.toggle_diagram":
		return "diagram"
	case "indexes.create":
		return "new index"
	case "indexes.edit":
		return "edit index"
	case "indexes.delete":
		return "delete index"
	case "diagram.depth_up":
		return "focus depth +"
	case "diagram.depth_down":
		return "focus depth -"
	case "foreign_keys.filter":
		return "filter foreign keys"
	case "foreign_keys.reset":
		return "reset foreign key filter"
	case "foreign_keys.create":
		return "new FK"
	case "foreign_keys.edit":
		return "edit FK"
	case "foreign_keys.delete":
		return "delete FK"
	case "query_log.cursor_down":
		return "↓"
	case "query_log.cursor_up":
		return "↑"
	case "query_log.top_first":
		return "top"
	case "query_log.top_last":
		return "bottom"
	case "query_log.next_page":
		return "next page"
	case "query_log.prev_page":
		return "prev page"
	case "detail.close":
		return "close detail"
	case "picker.reload":
		return "reload dir"
	case "picker.select":
		return "open"
	case "failure.return_to_picker":
		return "back to picker"
	case "workspace.escape_to_schema":
		return "back to schema"
	case "connection.switch_to_form":
		return "connection form"
	case "connection.switch_to_list":
		return "connection profiles"
	case "connection.edit_field":
		return "edit field"
	case "connection.field_next":
		return "↓"
	case "connection.field_prev":
		return "↑"
	default:
		return raw
	}
}
func commandAvailable(id CommandID, def commandDef, m Model) bool {
	switch id {
	case "app.quit":
		return !m.formActive() && !m.schema.component.Filter.Focused() &&
			!(m.State == stateConnection && (m.connection.component.RecentFilter.Focused() || (m.connection.component.Form.Focus == connectionFocusForm && m.overlay.formMode.Editing()))) &&
			!(m.queryEditorActive() && m.overlay.formMode.Editing())
	case "editor.external":
		return m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil
	case "query.cancel":
		return m.Running()
	case "ai.toggle":
		return m.State == stateReady && m.chat.component.Enabled
	case "focus.chat":
		return m.State == stateReady && m.chat.component.Visible
	case "focus.schema", "focus.workspace", "focus.query_log",
		"focus.toggle_fullscreen", "focus.cycle_forward", "focus.cycle_backward":
		return m.State == stateReady && !m.formActive() && !m.schema.component.Filter.Focused()
	case "query.execute":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabQuery
	case "query.history", "app.quit_dialog":
		return false
	}
	if def.scope == scopeGlobal {
		return false
	}
	switch id {
	case "workspace.escape_to_schema", "workspace.tab_next", "workspace.tab_prev":
		return m.State == stateReady && m.Focus == focusWorkspace && !m.formActive()
	case "schema.select_table", "schema.expand", "schema.collapse", "schema.add_table", "schema.rename_table", "schema.delete_table":
		return m.State == stateReady && m.Focus == focusSchema
	case "structure.filter", "structure.reset", "structure.edit", "structure.add", "structure.delete":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabStructure && !m.formActive()
	case "browse.edit":
		// Row actions never apply to the scope object list.
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.browseWriteAvailable()
	case "browse.edit_cell":
		// On document stores the cell binding edits the whole document, so
		// it merges into "edit document" and is not offered separately.
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.writeCapabilities().RowWriter
	case "browse.refine", "browse.reset", "browse.sort", "browse.next_page", "browse.prev_page":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil
	case "browse.insert_row":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.browseWriteAvailable()
	case "browse.delete_row":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.browseWriteAvailable()
	case "browse.add_table", "browse.rename_table", "browse.delete_table":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil
	case "cell.view":
		return (m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil) ||
			(m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabQuery && !m.overlay.formMode.Editing() && m.queryLog.results.Focused()) ||
			(m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabCustom && m.workspace.table.Focused())
	case "cell.yank":
		return (m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil) ||
			(m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabQuery && !m.overlay.formMode.Editing() && m.queryLog.results.Focused()) ||
			(m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabCustom && m.workspace.table.Focused())
	case "workspace.view_reload":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabCustom && !m.formActive()
	case "indexes.filter", "indexes.reset", "indexes.toggle_diagram", "indexes.create", "indexes.edit", "indexes.delete":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabIndexes && !m.schema.component.Structure.IndexForm.Active()
	case "diagram.depth_up", "diagram.depth_down":
		return m.State == stateReady && m.Focus == focusWorkspace && ((m.Tab == tabForeignKeys && m.schema.component.Structure.RelationshipDiagram) || (m.Tab == tabIndexes && m.schema.component.Structure.IndexDiagram))
	case "foreign_keys.filter", "foreign_keys.reset", "foreign_keys.toggle_diagram", "foreign_keys.create", "foreign_keys.edit", "foreign_keys.delete":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys && !m.schema.component.Structure.ForeignKeyForm.Active()
	case "query_log.yank", "query_log.explain", "query_log.detail", "query_log.context_menu",
		"query_log.cursor_down", "query_log.cursor_up", "query_log.top_first", "query_log.top_last", "query_log.next_page", "query_log.prev_page":
		return m.State == stateReady && m.Focus == focusQueryLog
	case "chat.delete", "chat.clear", "chat.apply_sql":
		return m.State == stateReady && m.Focus == focusChat
	case "detail.explain", "detail.close":
		return m.queryLog.component.Detail != nil
	case "picker.reload", "picker.select":
		return m.State == statePicking
	case "failure.return_to_picker":
		return m.State == stateFailure
	case "connection.switch_to_form", "connection.add", "connection.edit", "connection.delete":
		return m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusRecent
	case "connection.switch_to_list", "connection.execute", "connection.field_next", "connection.field_prev":
		return m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil
	case "connection.action_enter":
		return m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil && m.connectionActionFocused()
	case "connection.edit_field":
		return m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil && !m.connectionActionFocused()
	case "form.edit":
		return m.formActive()
	case "form.save", "form.discard", "form.field_next", "form.field_prev":
		return m.formActive()
	case "form.delete":
		return m.schema.component.Structure.IndexForm.Active() || m.schema.component.Structure.ForeignKeyForm.Active() || m.schema.component.Structure.ColumnForm.Active()
	default:
		return false
	}
}

// applyFilter re-filters items by the current query (case-insensitive substring match).
func (p *commandPalette) applyFilter() {
	q := strings.ToLower(string(p.query))
	if q == "" {
		p.filtered = make([]commandPaletteItem, len(p.items))
		copy(p.filtered, p.items)
	} else {
		p.filtered = p.filtered[:0]
		for _, item := range p.items {
			if strings.Contains(strings.ToLower(item.label), q) {
				p.filtered = append(p.filtered, item)
			}
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

// handleKey processes a key press while the palette is visible.
// Returns (selectMsg, close, consumed).
func (p *commandPalette) handleKey(msg tea.KeyPressMsg) (commandPaletteSelectMsg, bool, bool) {
	stroke := msg.Keystroke()
	if p.filtering {
		switch msg.Key().Code {
		case tea.KeyEscape, tea.KeyEnter:
			p.filtering = false
			return commandPaletteSelectMsg{}, false, true
		case tea.KeyBackspace:
			if len(p.query) > 0 {
				p.query = p.query[:len(p.query)-1]
				p.applyFilter()
			}
			return commandPaletteSelectMsg{}, false, true
		}
		if len(stroke) == 1 && stroke[0] >= ' ' && stroke[0] <= '~' {
			p.query = append(p.query, rune(stroke[0]))
			p.applyFilter()
		}
		return commandPaletteSelectMsg{}, false, true
	}

	switch msg.Key().Code {
	case tea.KeyEscape:
		p.visible = false
		return commandPaletteSelectMsg{}, true, true
	case tea.KeyEnter:
		if len(p.filtered) > 0 && p.cursor >= 0 && p.cursor < len(p.filtered) {
			item := p.filtered[p.cursor]
			p.visible = false
			return commandPaletteSelectMsg{id: item.id}, false, true
		}
		return commandPaletteSelectMsg{}, false, true
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return commandPaletteSelectMsg{}, false, true
	case tea.KeyDown:
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return commandPaletteSelectMsg{}, false, true
	default:
		if len(stroke) == 1 {
			switch stroke[0] {
			case '/':
				p.filtering = true
			case 'j':
				if p.cursor < len(p.filtered)-1 {
					p.cursor++
				}
			case 'k':
				if p.cursor > 0 {
					p.cursor--
				}
			}
		}
		return commandPaletteSelectMsg{}, false, true
	}
}

// move shifts the selection cursor by step, clamped to the filtered items.
func (p *commandPalette) move(step int) {
	if len(p.filtered) == 0 {
		p.cursor = 0
		return
	}
	p.cursor = min(max(p.cursor+step, 0), len(p.filtered)-1)
}

// handleWheel moves the selection on mouse wheel events.
func (p *commandPalette) handleWheel(wheel tea.MouseWheelMsg) {
	switch wheel.Button {
	case tea.MouseWheelUp:
		p.move(-1)
	case tea.MouseWheelDown:
		p.move(1)
	}
}

// layout returns the palette box geometry for the given screen size.
func (p *commandPalette) layout(width, height int) (palW, palH, boxX, boxY int) {
	palW = min(width*6/10, 80)
	palH = min(height*6/10, len(p.filtered)+6)
	palW = max(palW, 40)
	palH = max(palH, 8)
	palH = min(palH, height-4)
	return palW, palH, (width - palW) / 2, (height - palH) / 2
}

// listContent builds the scrolled list lines and maps each visible line to
// its filtered item index (-1 for scope headers and separators).
func (p *commandPalette) listContent(palH int) (lines []string, itemAtLine []int) {
	scopeNames := map[scope]string{
		scopeGlobal: "Global",
		scopeView:   "View",
		scopeForm:   "Form",
	}
	lastScope := scope(-1)
	selectedLine := 0
	for i, item := range p.filtered {
		if item.scope != lastScope {
			if i > 0 {
				lines = append(lines, "")
				itemAtLine = append(itemAtLine, -1)
			}
			lines = append(lines, mutedStyle.Render("  "+scopeNames[item.scope]+"  "))
			itemAtLine = append(itemAtLine, -1)
			lastScope = item.scope
		}

		prefix := "  "
		label := item.label
		if i == p.cursor {
			selectedLine = len(lines)
			prefix = "> "
			label = selectedItemStyle.Render(label)
		}
		spacer := strings.Repeat(" ", max(1, 24-ansi.StringWidth(label)))
		lines = append(lines, prefix+label+spacer+mutedStyle.Render(item.shortcut))
		itemAtLine = append(itemAtLine, i)
	}
	if len(p.filtered) == 0 {
		lines = append(lines, mutedStyle.Render("  no matching commands"))
		itemAtLine = append(itemAtLine, -1)
		return lines, itemAtLine
	}
	visibleLines := max(1, palH-6)
	start := max(0, min(selectedLine-visibleLines/2, len(lines)-visibleLines))
	end := min(start+visibleLines, len(lines))
	return lines[start:end], itemAtLine[start:end]
}

// handleClick selects the command under a left click on the palette box.
// Returns (selectMsg, consumed); consumed is false for clicks outside the box.
func (p *commandPalette) handleClick(msg tea.MouseClickMsg, width, height int) (commandPaletteSelectMsg, bool) {
	if !p.visible || msg.Button != tea.MouseLeft {
		return commandPaletteSelectMsg{}, false
	}
	palW, palH, boxX, boxY := p.layout(width, height)
	if msg.X < boxX || msg.X >= boxX+palW || msg.Y < boxY || msg.Y >= boxY+palH {
		// Click outside the box dismisses the palette without dispatching.
		p.visible = false
		p.swallowRelease = true
		return commandPaletteSelectMsg{}, true
	}
	lines, itemAtLine := p.listContent(palH)
	// Inner area starts at boxY+1: title, blank, then list lines from boxY+3.
	lineIdx := msg.Y - boxY - 3
	if lineIdx < 0 || lineIdx >= len(lines) {
		return commandPaletteSelectMsg{}, true
	}
	idx := itemAtLine[lineIdx]
	if idx < 0 {
		return commandPaletteSelectMsg{}, true
	}
	p.cursor = idx
	p.visible = false
	p.swallowRelease = true
	return commandPaletteSelectMsg{id: p.filtered[idx].id}, true
}

// paletteDimKeep is the fraction of each backdrop color that survives when
// the palette is open: the app behind reads as a dimmed scrim (0 = black,
// 1 = unchanged).
const paletteDimKeep = 0.55

// dimColor returns c darkened by keeping the given fraction of each channel.
func dimColor(c color.Color, keep float64) color.Color {
	rgba := color.RGBAModel.Convert(c).(color.RGBA)
	return color.RGBA{
		R: uint8(float64(rgba.R) * keep),
		G: uint8(float64(rgba.G) * keep),
		B: uint8(float64(rgba.B) * keep),
		A: 255,
	}
}

// dimPaletteBackdrop faints every cell already drawn on the canvas so the
// palette box pops against a muted backdrop. Cells without a background get
// a dim canvas fill so the scrim is uniform; the box is drawn over it next.
func dimPaletteBackdrop(canvas uv.ScreenBuffer) {
	bounds := canvas.Bounds()
	fallback := dimColor(chrome.ParseHex(colorCanvas), paletteDimKeep)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		line := canvas.Line(y)
		if line == nil {
			continue
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cell := line.At(x)
			if cell == nil {
				canvas.SetCell(x, y, &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: fallback}})
				continue
			}
			if cell.Style.Bg != nil {
				cell.Style.Bg = dimColor(cell.Style.Bg, paletteDimKeep)
			} else {
				cell.Style.Bg = fallback
			}
			if cell.Style.Fg != nil {
				cell.Style.Fg = dimColor(cell.Style.Fg, paletteDimKeep)
			}
		}
	}
}

// paletteDraw draws the palette overlay onto an existing screen buffer.
func (p *commandPalette) paletteDraw(canvas uv.ScreenBuffer, width, height int) {
	if !p.visible {
		return
	}

	dimPaletteBackdrop(canvas)

	// Palette dimensions: ~60% of terminal, centered, capped.
	palW := min(width*6/10, 80)
	palH := min(height*6/10, len(p.filtered)+6)
	palW = max(palW, 40)
	palH = max(palH, 8)
	palH = min(palH, height-4)

	// Build the list content.
	listLines, _ := p.listContent(palH)

	// Center the palette box.
	boxX := (width - palW) / 2
	boxY := (height - palH) / 2

	// Fill background.
	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: chrome.ParseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(boxX, boxY, boxX+palW, boxY+palH))

	// Draw border.
	borderStyle := uv.Style{Fg: chrome.ParseHex(colorBorder)}
	for cx := boxX + 1; cx < boxX+palW-1; cx++ {
		canvas.SetCell(cx, boxY, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, boxY+palH-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	// First inner line: context + filter prompt; the title lives in the top border.
	title := " "
	ctx := p.contextTitle
	if ctx != "" {
		title += mutedStyle.Render("[" + ctx + "]")
	}
	if len(p.query) > 0 {
		title += " " + mutedStyle.Render("/"+string(p.query)+" ")
	} else {
		title += " " + mutedStyle.Render(" / filter... ")
	}

	// Help line.
	helpLine := mutedStyle.Render(" / filter | j/k or arrows navigate | enter select | esc close")
	if p.filtering {
		helpLine = mutedStyle.Render(" enter/esc stop filtering | backspace erase")
	}
	for cy := boxY + 1; cy < boxY+palH-1; cy++ {
		canvas.SetCell(boxX, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(boxX+palW-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(boxX, boxY, &uv.Cell{Content: "╭", Width: 1, Style: borderStyle})
	canvas.SetCell(boxX+palW-1, boxY, &uv.Cell{Content: "╮", Width: 1, Style: borderStyle})
	canvas.SetCell(boxX, boxY+palH-1, &uv.Cell{Content: "╰", Width: 1, Style: borderStyle})
	canvas.SetCell(boxX+palW-1, boxY+palH-1, &uv.Cell{Content: "╯", Width: 1, Style: borderStyle})

	// Title overlay in the top border, replacing the ─ run drawn above.
	// Title color like the pane overlays, but no background badge.
	titleCells := " Command Palette "
	titleStyle := uv.Style{Fg: chrome.ParseHex(colorSecondary), Attrs: uv.AttrBold}
	for offset, r := range titleCells {
		canvas.SetCell(boxX+1+offset, boxY, &uv.Cell{Content: string(r), Width: 1, Style: titleStyle})
	}

	// Build full text.
	innerW := palW - 2
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString("\n")
	for _, line := range listLines {
		trimmed := strings.TrimRight(line, " ")
		b.WriteString(" ")
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpLine)

	innerArea := image.Rect(boxX+1, boxY+1, boxX+1+innerW, boxY+1+palH-2)
	uv.NewStyledString(b.String()).Draw(canvas, innerArea)
}

// Selected/focused item style.
var selectedItemStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorPrimary)).
	Background(lipgloss.Color(colorStripe)).
	Bold(true).
	Padding(0, 0)

// Muted style for hints, shortcuts, and status.
var mutedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorMuted)).
	Padding(0, 0)
