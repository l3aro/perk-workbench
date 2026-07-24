package workbench

import (
	"image"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
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
	visible      bool
	contextTitle string
}

// newCommandPalette builds the palette items from the keybinding registry.
func newCommandPalette(m Model) *commandPalette {
	keybindings := m.keybindings
	items := make([]commandPaletteItem, 0, len(keybindings.commands))
	for id, def := range keybindings.commands {
		if !commandAvailable(id, def, m) {
			continue
		}
		shortcut := ""
		if len(def.keys) > 0 {
			shortcut = displayKey(def.keys[0])
		}
		label := commandLabel(id, def.label)
		items = append(items, commandPaletteItem{
			id:       id,
			label:    label,
			shortcut: shortcut,
			scope:    def.scope,
		})
	}

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
		filtered:     items,
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
				return "Structure"
			case tabBrowse:
				return "Browse"
			case tabSQL:
				return "SQL"
			case tabIndexes:
				return "Indexes"
			case tabForeignKeys:
				return "Foreign Keys"
			}
		case focusQueryLog:
			return "Query Log"
		}
	}
	return ""
}

// commandAvailable returns true if the given command can be executed in the
// current model context. Global commands are always shown. View/form commands
// are shown only when their pane matches the current state.
// commandLabel returns a disambiguated display label for the palette.
// Commands sharing the same raw label get a pane prefix.
func commandLabel(id CommandID, raw string) string {
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
	case "structure.edit":
		return "edit column"
	case "browse.edit":
		return "edit row"
	case "browse.next_page":
		return "next page"
	case "browse.prev_page":
		return "prev page"
	case "indexes.create":
		return "new index"
	case "indexes.edit":
		return "edit index"
	case "indexes.delete":
		return "delete index"
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
		return "connection recent"
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
	if def.scope == scopeGlobal {
		return true
	}
	switch id {
	case "workspace.escape_to_schema", "workspace.tab_next", "workspace.tab_prev":
		return m.State == stateReady && m.Focus == focusWorkspace && !m.formActive()
	case "schema.select_table":
		return m.State == stateReady && m.Focus == focusSchema
	case "structure.edit":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabStructure && !m.formActive()
	case "browse.edit", "browse.next_page", "browse.prev_page":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active()
	case "indexes.create", "indexes.edit", "indexes.delete":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabIndexes && !m.indexForm.active()
	case "foreign_keys.toggle_diagram", "foreign_keys.create", "foreign_keys.edit", "foreign_keys.delete":
		return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys && !m.foreignKeyForm.active()
	case "query_log.yank", "query_log.explain", "query_log.cursor_down", "query_log.cursor_up",
		"query_log.top_first", "query_log.top_last", "query_log.detail":
		return m.State == stateReady && m.Focus == focusQueryLog
	case "detail.yank", "detail.explain", "detail.close":
		return m.queryLogDetail != nil
	case "picker.reload", "picker.select":
		return m.State == statePicking
	case "failure.return_to_picker":
		return m.State == stateFailure
	case "connection.switch_to_form", "connection.switch_to_list",
		"connection.add", "connection.edit", "connection.delete",
		"connection.action_enter", "connection.execute",
		"connection.edit_field", "connection.field_next", "connection.field_prev":
		return m.State == stateConnection
	case "form.edit":
		return m.formActive()
	case "form.save", "form.discard", "form.field_next", "form.field_prev":
		return m.formActive()
	case "form.delete":
		return m.indexForm.active() || m.foreignKeyForm.active()
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
	case tea.KeyBackspace:
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.applyFilter()
		}
		return commandPaletteSelectMsg{}, false, true
	default:
		stroke := msg.Keystroke()
		if len(stroke) == 1 && stroke[0] >= ' ' && stroke[0] <= '~' {
			p.query = append(p.query, rune(stroke[0]))
			p.applyFilter()
		}
		return commandPaletteSelectMsg{}, false, true
	}
}

// paletteDraw draws the palette overlay onto an existing screen buffer.
func (p *commandPalette) paletteDraw(canvas uv.ScreenBuffer, width, height int) {
	if !p.visible {
		return
	}

	// Palette dimensions: ~60% of terminal, centered, capped.
	palW := min(width*6/10, 80)
	palH := min(height*6/10, len(p.filtered)+6)
	palW = max(palW, 40)
	palH = max(palH, 8)
	palH = min(palH, height-4)

	// Build the list content.
	var listLines []string
	scopeNames := map[scope]string{
		scopeGlobal: "Global",
		scopeView:   "View",
		scopeForm:   "Form",
	}
	lastScope := scope(-1)
	for i, item := range p.filtered {
		if item.scope != lastScope {
			if i > 0 {
				listLines = append(listLines, "")
			}
			listLines = append(listLines, headerStyle.Render("  "+scopeNames[item.scope]+"  "))
			lastScope = item.scope
		}

		label := item.label
		if i == p.cursor {
			label = selectedItemStyle.Render(label)
		}
		spacer := strings.Repeat(" ", max(1, 24-ansi.StringWidth(label)))
		line := " " + label + spacer + mutedStyle.Render(item.shortcut)
		listLines = append(listLines, line)
	}
	if len(p.filtered) == 0 {
		listLines = append(listLines, mutedStyle.Render("  no matching commands"))
	}

	// Center the palette box.
	boxX := (width - palW) / 2
	boxY := (height - palH) / 2

	// Fill background.
	dialogBg := uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: parseHex(colorPanel)}}
	canvas.FillArea(&dialogBg, image.Rect(boxX, boxY, boxX+palW, boxY+palH))

	// Draw border.
	borderStyle := uv.Style{Fg: parseHex(colorBorder)}
	for cx := boxX + 1; cx < boxX+palW-1; cx++ {
		canvas.SetCell(cx, boxY, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
		canvas.SetCell(cx, boxY+palH-1, &uv.Cell{Content: "─", Width: 1, Style: borderStyle})
	}
	// Title line with context + filter.
	title := headerStyle.Render(" Commands ")
	ctx := p.contextTitle
	if ctx != "" {
		title += mutedStyle.Render(" [" + ctx + "]")
	}
	if len(p.query) > 0 {
		title += " " + mutedStyle.Render("/"+string(p.query)+" ")
	} else {
		title += " " + mutedStyle.Render(" / filter... ")
	}

	// Help line.
	helpLine := mutedStyle.Render(" \uf0a8\uf0a7 navigate | enter select | esc close")
	for cy := boxY + 1; cy < boxY+palH-1; cy++ {
		canvas.SetCell(boxX, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
		canvas.SetCell(boxX+palW-1, cy, &uv.Cell{Content: "│", Width: 1, Style: borderStyle})
	}
	canvas.SetCell(boxX, boxY, &uv.Cell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.SetCell(boxX+palW-1, boxY, &uv.Cell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.SetCell(boxX, boxY+palH-1, &uv.Cell{Content: "└", Width: 1, Style: borderStyle})
	canvas.SetCell(boxX+palW-1, boxY+palH-1, &uv.Cell{Content: "┘", Width: 1, Style: borderStyle})

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
	Foreground(lipgloss.Color(colorInk)).
	Background(lipgloss.Color(colorStripe)).
	Bold(true).
	Padding(0, 0)

// Muted style for hints, shortcuts, and status.
var mutedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(colorMuted)).
	Padding(0, 0)
