// Package uikit holds the shared UI contracts and primitives of the
// workbench shell: keybinding scopes and the key matcher interface, the
// screen-layout snapshot passed to feature components, the typed events
// features emit for the root to act on, the theme palette, and the
// stateless table/viewport primitives shared by the root and the feature
// packages.
//
// Feature packages under internal/workbench import uikit but never the
// root workbench package; the root satisfies uikit.KeyMatcher through
// its Keybindings type.
package uikit

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// Scope is the routing scope of a command binding: global bindings apply
// everywhere, view bindings inside a pane, form bindings inside modal
// forms.
type Scope int

const (
	ScopeGlobal Scope = iota
	ScopeView
	ScopeForm
	// ScopeEditor is the query editor's insert-mode scope: bindings that
	// act on the focused statement editor (e.g. query.complete).
	ScopeEditor
)

func (s Scope) String() string {
	switch s {
	case ScopeGlobal:
		return "global"
	case ScopeView:
		return "view"
	case ScopeForm:
		return "form"
	case ScopeEditor:
		return "editor"
	default:
		return "unknown"
	}
}

// CommandID is a stable identifier for an application keyboard command.
type CommandID string

// PreparedKeyStroke contains the two canonical representations of one key
// press. It is immutable after construction and lets dispatchers reuse the
// computed strings without constructing a temporary stroke slice.
type PreparedKeyStroke struct {
	msg       tea.KeyPressMsg
	text      string
	keystroke string
}

// PrepareKeyStroke snapshots the representations used by key binding
// matching. The original message is retained solely for compatibility with
// matchers that have not adopted the prepared API.
func PrepareKeyStroke(msg tea.KeyPressMsg) PreparedKeyStroke {
	return PreparedKeyStroke{
		msg:       msg,
		text:      msg.String(),
		keystroke: msg.Keystroke(),
	}
}

// Message returns the original key press for compatibility fallbacks.
func (p PreparedKeyStroke) Message() tea.KeyPressMsg {
	return p.msg
}

// String returns the text representation of the prepared key press.
func (p PreparedKeyStroke) String() string {
	return p.text
}

// Keystroke returns the canonical keystroke representation of the prepared
// key press.
func (p PreparedKeyStroke) Keystroke() string {
	return p.keystroke
}

// KeyMatcher matches a key press against configured command bindings.
// Root's Keybindings satisfies it structurally.
type KeyMatcher interface {
	Match(msg tea.KeyPressMsg, id CommandID, scopes []Scope) bool
}

// PreparedKeyMatcher is the allocation-free extension implemented by
// matchers that can consume a prepared key press. KeyMatcher intentionally
// remains small so existing fakes and third-party implementations continue
// to satisfy it.
type PreparedKeyMatcher interface {
	MatchPrepared(key PreparedKeyStroke, id CommandID, scopes []Scope) bool
}

// MatchPrepared dispatches through the prepared matcher when available and
// falls back to the original message-taking API for existing matchers.
func MatchPrepared(keybindings KeyMatcher, key PreparedKeyStroke, id CommandID, scopes []Scope) bool {
	if prepared, ok := keybindings.(PreparedKeyMatcher); ok {
		return prepared.MatchPrepared(key, id, scopes)
	}
	return keybindings.Match(key.Message(), id, scopes)
}

// Layout is the root-owned screen snapshot handed to a feature component
// for one update or render. Width and Height are the full screen size;
// ViewportWidth is the content width inside the component's pane and
// PaneHeight the pane body height. The root keeps pane hit-testing and
// focus routing; components never see screen origins.
type Layout struct {
	Width         int
	Height        int
	ViewportWidth int
	PaneHeight    int
}

// Event is a typed request from a feature component to the root shell.
// The root applies the side effect; the component never touches root
// state.
type Event interface{ isEvent() }

// StatusChanged asks the root to record a status line transition.
type StatusChanged struct{ Text string }

func (StatusChanged) isEvent() {}

// ClipboardRequested asks the root to write text to both clipboards.
type ClipboardRequested struct{ Text string }

func (ClipboardRequested) isEvent() {}

// ExplainRequested asks the root to open the EXPLAIN picker for a
// statement.
type ExplainRequested struct{ Statement string }

func (ExplainRequested) isEvent() {}

// SpaceCompact is the single-cell horizontal padding of table cells and
// header buttons.
const SpaceCompact = 1

// Icon glyphs (Nerd Font codepoints; terminals without the font render
// the geometric fallbacks chosen at call sites).
const (
	IconPrimaryKey = "\uf084" // nf-fa-key
	IconUnique     = "\uee40" // nf-fa-fingerprint
	IconRegular    = "\uf0cb" // nf-fa-list_ol
	IconSuccess    = "\uf00c" // nf-fa-check
	IconFailed     = "\uf00d" // nf-fa-times
	IconCanceled   = "\uf05e" // nf-fa-ban
)

// The theme palette. Colors are mutable package state swapped by
// SetTheme; every style derives from them, so rendering always sees the
// current theme. Root's theme code snapshots these into its own style
// registry on every SetTheme.
var (
	ColorCanvas, ColorPanel, ColorStripe                    string
	ColorInk, ColorMuted, ColorPrimary                      string
	ColorSecondary, ColorDanger, ColorFocused, ColorSuccess string
	ColorBorder, ColorModeNormal                            string
	ColorModeInsert                                         string
	ColorWarn                                               string
)

// Styles derived from the palette; rebuilt by SetTheme.
var (
	HeaderStyle, StatusStyle              lipgloss.Style
	StatusSuccessStyle, StatusFailedStyle lipgloss.Style
	StatusCanceledStyle                   lipgloss.Style
	SelectedCellStyle                     lipgloss.Style
	selectedRowStyle                      lipgloss.Style
	selectedTableStyleCache               map[int]lipgloss.Style
	cellStyleCache                        map[int]lipgloss.Style
	cellStyleCacheRight                   map[int]lipgloss.Style
	ButtonSaveStyle, ButtonCancelStyle    lipgloss.Style
	ActionStyle, ActionSelectedStyle      lipgloss.Style
	ActionFocusedStyle                    lipgloss.Style
	ThinkingStyle                         lipgloss.Style
	UserMessageStyle                      lipgloss.Style
	UserMessageAccentStyle                lipgloss.Style
	CompletionItemStyle                   lipgloss.Style
	CompletionDetailStyle                 lipgloss.Style
)

// SetTheme applies the named theme palette and rebuilds the derived
// styles. Names match the workbench appTheme values.
func SetTheme(name string) {
	switch name {
	case "dracula":
		ColorCanvas, ColorPanel, ColorStripe = "#282a36", "#343746", "#44475a"
		ColorInk, ColorMuted, ColorPrimary = "#f8f8f2", "#b1b2c7", "#bd93f9"
		ColorSecondary, ColorDanger, ColorFocused = "#ff79c6", "#ff5555", "#50fa7b"
		ColorSuccess = "#50fa7b"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#6272a4", "#8be9fd", "#50fa7b"
		ColorWarn = "#f1fa8c"
	case "nord":
		ColorCanvas, ColorPanel, ColorStripe = "#2e3440", "#3b4252", "#434c5e"
		ColorInk, ColorMuted, ColorPrimary = "#eceff4", "#d8dee9", "#88c0d0"
		ColorSecondary, ColorDanger, ColorFocused = "#ebcb8b", "#bf616a", "#a3be8c"
		ColorSuccess = "#a3be8c"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#4c566a", "#81a1c1", "#a3be8c"
		ColorWarn = "#ebcb8b"
	case "monokai":
		ColorCanvas, ColorPanel, ColorStripe = "#272822", "#2f302a", "#3e3d32"
		ColorInk, ColorMuted, ColorPrimary = "#f8f8f2", "#75715e", "#a6e22e"
		ColorSecondary, ColorDanger, ColorFocused = "#f92672", "#f92672", "#a6e22e"
		ColorSuccess = "#a6e22e"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#49483e", "#66d9ef", "#fd971f"
		ColorWarn = "#e6db74"
	case "catppuccin":
		ColorCanvas, ColorPanel, ColorStripe = "#1e1e2e", "#313244", "#45475a"
		ColorInk, ColorMuted, ColorPrimary = "#cdd6f4", "#a6adc8", "#cba6f7"
		ColorSecondary, ColorDanger, ColorFocused = "#f9e2af", "#f38ba8", "#a6e3a1"
		ColorSuccess = "#a6e3a1"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#6c7086", "#89b4fa", "#a6e3a1"
		ColorWarn = "#f9e2af"
	case "solarized":
		ColorCanvas, ColorPanel, ColorStripe = "#002b36", "#073642", "#123f4a"
		ColorInk, ColorMuted, ColorPrimary = "#839496", "#657b83", "#268bd2"
		ColorSecondary, ColorDanger, ColorFocused = "#d33682", "#dc322f", "#859900"
		ColorSuccess = "#859900"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#0e5553", "#268bd2", "#859900"
		ColorWarn = "#b58900"
	case "light-ocean":
		ColorCanvas, ColorPanel, ColorStripe = "#f6f8fa", "#eef1f4", "#e3e8ef"
		ColorInk, ColorMuted, ColorPrimary = "#24292f", "#57606a", "#107894"
		ColorSecondary, ColorDanger, ColorFocused = "#2f6fbf", "#cf222e", "#1a7f37"
		ColorSuccess = "#1a7f37"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#cfd6e0", "#0969da", "#1a7f37"
		ColorWarn = "#9a6700"
	case "light-nord":
		ColorCanvas, ColorPanel, ColorStripe = "#eceff4", "#e5e9f0", "#d8dee9"
		ColorInk, ColorMuted, ColorPrimary = "#2e3440", "#4c566a", "#5e81ac"
		ColorSecondary, ColorDanger, ColorFocused = "#8a6d3b", "#bf616a", "#3b7a57"
		ColorSuccess = "#3b7a57"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#b8c0cc", "#3b6ea5", "#3b7a57"
		ColorWarn = "#8a6d3b"
	case "light-monokai":
		ColorCanvas, ColorPanel, ColorStripe = "#f7f7f2", "#efece2", "#e2ded0"
		ColorInk, ColorMuted, ColorPrimary = "#272822", "#6f6f5e", "#718c00"
		ColorSecondary, ColorDanger, ColorFocused = "#c9185a", "#f92672", "#5a8c00"
		ColorSuccess = "#5a8c00"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#cfcbdb", "#4271ae", "#5a8c00"
		ColorWarn = "#a37500"
	case "light-dracula":
		ColorCanvas, ColorPanel, ColorStripe = "#f5f5f7", "#ecebf0", "#dedce4"
		ColorInk, ColorMuted, ColorPrimary = "#2f2d43", "#6e6c86", "#7a5cd6"
		ColorSecondary, ColorDanger, ColorFocused = "#b45a8c", "#e04f6c", "#2a9d6d"
		ColorSuccess = "#2a9d6d"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#cfcdd9", "#4870c8", "#2a9d6d"
		ColorWarn = "#b08800"
	case "light-catppuccin":
		ColorCanvas, ColorPanel, ColorStripe = "#eff1f5", "#e6e9ef", "#dce0e8"
		ColorInk, ColorMuted, ColorPrimary = "#4c4f69", "#6c6f85", "#1e66f5"
		ColorSecondary, ColorDanger, ColorFocused = "#df8e1d", "#d20f39", "#40a02b"
		ColorSuccess = "#40a02b"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#ccd0da", "#1e66f5", "#40a02b"
		ColorWarn = "#df8e1d"
	case "light-solarized":
		ColorCanvas, ColorPanel, ColorStripe = "#fdf6e3", "#eee8d5", "#e3dcbe"
		ColorInk, ColorMuted, ColorPrimary = "#586e75", "#657b83", "#0077aa"
		ColorSecondary, ColorDanger, ColorFocused = "#d33682", "#dc322f", "#2aa198"
		ColorSuccess = "#859900"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#cdbf9f", "#268bd2", "#2aa198"
		ColorWarn = "#b58900"
	default:
		ColorCanvas, ColorPanel, ColorStripe = "#10151f", "#17202e", "#1c2838"
		ColorInk, ColorMuted, ColorPrimary = "#e6edf3", "#8b9bb4", "#94e2d5"
		ColorSecondary, ColorDanger, ColorFocused = "#89b4fa", "#f38ba8", "#f9e2af"
		ColorSuccess = "#3fb950"
		ColorBorder, ColorModeNormal, ColorModeInsert = "#324155", "#58a6ff", "#3fb950"
		ColorWarn = "#f9e2af"
	}
	resetStyles()
}

func init() { SetTheme("ocean") }

func resetStyles() {
	HeaderStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCanvas)).
		Background(lipgloss.Color(ColorSecondary)).
		Bold(true).
		Padding(0, SpaceCompact)
	StatusStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorMuted)).
		Padding(0, SpaceCompact)
	StatusSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess))
	StatusFailedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDanger))
	StatusCanceledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	SelectedCellStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCanvas)).
		Background(lipgloss.Color(ColorPrimary)).
		Bold(true)
	selectedRowStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorPrimary)).
		Background(lipgloss.Color(ColorStripe))
	selectedTableStyleCache = make(map[int]lipgloss.Style)
	cellStyleCache = make(map[int]lipgloss.Style)
	cellStyleCacheRight = make(map[int]lipgloss.Style)
	clearFilterInputRowCache()
	ButtonSaveStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCanvas)).
		Background(lipgloss.Color(ColorPrimary)).
		Bold(true).
		Padding(0, SpaceCompact)
	ButtonCancelStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorInk)).
		Background(lipgloss.Color(ColorStripe)).
		Padding(0, SpaceCompact)
	ActionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorInk)).
		Background(lipgloss.Color(ColorStripe)).
		Padding(0, SpaceCompact)
	ActionSelectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCanvas)).
		Background(lipgloss.Color(ColorPrimary)).
		Bold(true).
		Padding(0, SpaceCompact)
	ActionFocusedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCanvas)).
		Background(lipgloss.Color(ColorFocused)).
		Bold(true).
		Padding(0, SpaceCompact)
}

// SafeText strips ANSI escape codes and unsafe control characters from
// display input.
func SafeText(input string) string { return sharedsql.SanitizeDisplay(input) }

// CellText truncates a cell value to MaxRunes runes and appends "…" for
// the table display. The original full value remains in the raw result
// for editing.
func CellText(input string) string {
	return sharedsql.SanitizeDisplay(input, sharedsql.MaxRunes)
}

// NewResultsTable builds a fresh results-style table.
func NewResultsTable() table.Model {
	return table.New(
		table.WithColumns([]table.Column{{Title: "Results", Width: 1}}),
		table.WithFocused(true),
		table.WithWidth(1),
		table.WithHeight(2),
		table.WithStyles(table.Styles{
			Header:   HeaderStyle,
			Cell:     lipgloss.NewStyle().Padding(0, SpaceCompact),
			Selected: selectedTableStyle(0),
		}),
	)
}

// ResizeResultsTable fits a results table to the viewport with the shared
// cell styles.
func ResizeResultsTable(resultTable *table.Model, width, height int) {
	tableWidth := max(width, TableContentWidth(resultTable.Columns()))
	resultTable.SetWidth(tableWidth)
	resultTable.SetHeight(height)
	resultTable.SetStyles(table.Styles{
		Header:   HeaderStyle,
		Cell:     lipgloss.NewStyle().Padding(0, SpaceCompact),
		Selected: selectedTableStyle(tableWidth),
	})
}

// TableContentWidth sums column widths plus cell padding.
func TableContentWidth(columns []table.Column) int {
	width := 0
	for _, column := range columns {
		width += column.Width + 2*SpaceCompact
	}
	return width
}

// TableColumns sizes columns from titles and row content.
func TableColumns(titles []string, rows []table.Row) []table.Column {
	if len(titles) == 0 {
		titles = []string{"Results"}
	}

	columns := make([]table.Column, len(titles))
	for index, title := range titles {
		columns[index] = table.Column{Title: title, Width: max(ansi.StringWidth(title), 1)}
	}
	for _, row := range rows {
		for index, value := range row {
			if index < len(columns) {
				columns[index].Width = max(columns[index].Width, ansi.StringWidth(value))
			}
		}
	}
	return columns
}

// TableViewportView renders a table viewport without a selected column.
func TableViewportView(resultTable table.Model, offset, width int) string {
	return TableViewportViewWithAlignment(resultTable, nil, offset, width, -1)
}

// TableViewportViewWithAlignment renders the table viewport with optional
// numeric alignment and a selected column highlight.
func TableViewportViewWithAlignment(resultTable table.Model, numericColumns []bool, offset, width, selectedColumn int) string {
	offset = min(max(offset, 0), max(resultTable.Width()-width, 0))
	columns := resultTable.Columns()
	lines := []string{HeaderStyle.Padding(0, 0).Render(TableLineWithSelection(columns, nil, numericColumns, offset, width, -1, false))}
	rows, rowHeight := resultTable.Rows(), resultTable.Height()
	start := min(max(resultTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	for rowIndex := start; rowIndex < min(start+rowHeight, len(rows)); rowIndex++ {
		selectedRow := rowIndex == resultTable.Cursor()
		row := TableLineWithSelection(columns, rows[rowIndex], numericColumns, offset, width, selectedColumn, selectedRow)
		lines = append(lines, row)
	}
	for range max(rowHeight-(len(lines)-1), 0) {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func tableLine(columns []table.Column, row table.Row, numericColumns []bool, offset, width int) string {
	return TableLineWithSelection(columns, row, numericColumns, offset, width, -1, false)
}

// cellStyle returns a cached width-fixed table cell style. Styles depend only
// on (width, alignment); distinct widths are bounded by the column count, and
// all access happens on the Bubble Tea UI goroutine.

func selectedTableStyle(width int) lipgloss.Style {
	if style, ok := selectedTableStyleCache[width]; ok {
		return style
	}
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorPrimary)).
		Background(lipgloss.Color(ColorStripe))
	if width > 0 {
		style = style.Width(width)
	}
	selectedTableStyleCache[width] = style
	return style
}

func cellStyle(width int, numeric bool) lipgloss.Style {
	cache := cellStyleCache
	if numeric {
		cache = cellStyleCacheRight
	}
	if style, ok := cache[width]; ok {
		return style
	}
	style := lipgloss.NewStyle().Width(width).MaxWidth(width).Inline(true)
	if numeric {
		style = style.Align(lipgloss.Right)
	}
	cache[width] = style
	return style
}

func TableLineWithSelection(columns []table.Column, row table.Row, numericColumns []bool, offset, width, selectedColumn int, selectedRow bool) string {
	cells := make([]string, len(columns))
	for index, column := range columns {
		value := column.Title
		if row != nil {
			value = ""
			if index < len(row) {
				value = row[index]
			}
		}
		style := cellStyle(column.Width, row != nil && index < len(numericColumns) && numericColumns[index])
		if selectedRow {
			value = ansi.Strip(value)
		}
		cell := strings.Repeat(" ", SpaceCompact) + style.Render(ansi.Truncate(value, column.Width, "…")) + strings.Repeat(" ", SpaceCompact)
		cells[index] = cell
	}
	line := CropTableLine(strings.Join(cells, ""), offset, width)
	if !selectedRow {
		return line
	}
	if selectedColumn < 0 || selectedColumn >= len(columns) {
		return highlightedTableRow(line, 0, 0)
	}
	selectedStart := 0
	for _, column := range columns[:selectedColumn] {
		selectedStart += column.Width + 2*SpaceCompact
	}
	return highlightedTableRow(line, selectedStart-offset, columns[selectedColumn].Width+2*SpaceCompact)
}

func CropTableLine(line string, offset, width int) string {
	visible := tableLineSegment(line, offset, width)
	return visible + strings.Repeat(" ", max(width-ansi.StringWidth(visible), 0))
}

// tableLineSegment returns the visible columns [offset, offset+width) of the
// line. Clusters are included on their start column (a cluster may overhang
// the right edge), matching the crop contract; ANSI sequences count as zero
// width and are kept whole so colors survive cropping.
func tableLineSegment(line string, offset, width int) string {
	var visible strings.Builder
	total := offset + width
	lineWidth := 0
	for len(line) > 0 {
		cluster, _ := ansi.FirstGraphemeCluster(line, ansi.WcWidth)
		if strings.HasPrefix(cluster, "\x1b") {
			n := AnsiSequenceLen(line)
			if lineWidth >= offset && lineWidth < total {
				visible.WriteString(line[:n])
			}
			line = line[n:]
			continue
		}
		clusterWidth := ansi.StringWidth(cluster)
		if lineWidth+clusterWidth > total {
			break
		}
		if lineWidth >= offset {
			visible.WriteString(cluster)
		}
		line = line[len(cluster):]
		lineWidth += clusterWidth
	}
	return visible.String()
}

// AnsiSequenceLen returns the length of an ANSI escape sequence starting
// at the beginning of input.
func AnsiSequenceLen(input string) int {
	if len(input) == 1 {
		return 1
	}
	switch input[1] {
	case '[':
		for i := 2; i < len(input); i++ {
			if input[i] >= 0x40 && input[i] <= 0x7e {
				return i + 1
			}
		}
	case ']', 'P', '^', '_':
		for i := 2; i < len(input); i++ {
			if input[i] == '\a' {
				return i + 1
			}
			if input[i] == '\x1b' && i+1 < len(input) && input[i+1] == '\\' {
				return i + 2
			}
		}
		return len(input)
	default:
		return 2
	}
	return len(input)
}

func highlightedTableRow(line string, selectedStart, selectedWidth int) string {
	lineWidth := ansi.StringWidth(line)
	selectedEnd := min(max(selectedStart+selectedWidth, 0), lineWidth)
	selectedStart = min(max(selectedStart, 0), lineWidth)
	if selectedStart == selectedEnd {
		return selectedRowStyle.Render(line)
	}
	return selectedRowStyle.Render(tableLineSegment(line, 0, selectedStart)) +
		SelectedCellStyle.Render(tableLineSegment(line, selectedStart, selectedEnd-selectedStart)) +
		selectedRowStyle.Render(tableLineSegment(line, selectedEnd, lineWidth-selectedEnd))
}

// TableOffset clamps a horizontal table offset to the viewport range.
func TableOffset(resultTable table.Model, offset, viewportWidth int) int {
	return min(max(offset, 0), max(resultTable.Width()-viewportWidth, 0))
}

// ColsHint reports the hidden-columns hint for a horizontally scrolled
// table, or an empty string when everything fits.
func ColsHint(columns []table.Column, viewportWidth int) string {
	if len(columns) <= 1 {
		return ""
	}
	totalWidth := 0
	for _, c := range columns {
		totalWidth += c.Width + 2*SpaceCompact
	}
	if totalWidth <= viewportWidth {
		return ""
	}
	return fmt.Sprintf(" | %d cols", len(columns))
}

// Clamp bounds v to [lo, hi].
func Clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// MoveTableCell moves the cursor row and selected column for arrow keys.
func MoveTableCell(resultTable *table.Model, selectedColumn, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	switch keyPress.Key().Code {
	case tea.KeyUp, 'k':
		resultTable.SetCursor(max(resultTable.Cursor()-1, 0))
		return true
	case tea.KeyDown, 'j':
		resultTable.SetCursor(min(resultTable.Cursor()+1, max(len(resultTable.Rows())-1, 0)))
		return true
	case tea.KeyLeft, 'h':
		MoveTableColumn(resultTable, selectedColumn, offset, viewportWidth, -1)
	case tea.KeyRight, 'l':
		MoveTableColumn(resultTable, selectedColumn, offset, viewportWidth, 1)
	default:
		return false
	}
	return true
}

// MoveTableColumn moves the selected column one step in dir (-1 left, +1
// right), clamped to the column range, and reveals it in the viewport.
func MoveTableColumn(resultTable *table.Model, selectedColumn, offset *int, viewportWidth, dir int) {
	columns := resultTable.Columns()
	if len(columns) == 0 {
		return
	}
	*selectedColumn = Clamp(*selectedColumn, 0, len(columns)-1)
	*selectedColumn = Clamp(*selectedColumn+dir, 0, len(columns)-1)
	RevealTableColumn(*resultTable, *selectedColumn, offset, viewportWidth)
}

// MoveTableRow moves the cursor row and pans row-based tables.
func MoveTableRow(resultTable *table.Model, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	switch keyPress.Key().Code {
	case tea.KeyUp, 'k':
		resultTable.SetCursor(max(resultTable.Cursor()-1, 0))
	case tea.KeyDown, 'j':
		resultTable.SetCursor(min(resultTable.Cursor()+1, max(len(resultTable.Rows())-1, 0)))
	case tea.KeyLeft, 'h':
		*offset = TableOffset(*resultTable, *offset-max(viewportWidth/2, 1), viewportWidth)
	case tea.KeyRight, 'l':
		*offset = TableOffset(*resultTable, *offset+max(viewportWidth/2, 1), viewportWidth)
	default:
		return false
	}
	return true
}

// RevealTableColumn pans the viewport so the selected column is visible.
func RevealTableColumn(resultTable table.Model, selectedColumn int, offset *int, viewportWidth int) {
	columns := resultTable.Columns()
	if len(columns) == 0 {
		*offset = 0
		return
	}

	selectedColumn = min(max(selectedColumn, 0), len(columns)-1)
	columnStart := 0
	for index, column := range columns {
		columnEnd := columnStart + column.Width + 2*SpaceCompact
		if index == selectedColumn {
			if columnEnd-columnStart >= viewportWidth {
				// The column is wider than the viewport, so it cannot fit:
				// align its start so the view opens at the cell's head
				// instead of pinning the viewport to its tail.
				*offset = columnStart
			} else if columnStart < *offset {
				*offset = columnStart
			} else if columnEnd > *offset+viewportWidth {
				*offset = columnEnd - viewportWidth
			}
			return
		}
		columnStart = columnEnd
	}
}
