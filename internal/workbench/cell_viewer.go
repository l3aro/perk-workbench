package workbench

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

// cellViewer is a read-only viewport overlay for inspecting a table cell value.
// It wraps the bubbles viewport for vertical scrolling with h/j/k/l, arrow
// keys, paging, mouse wheel, a soft-wrap toggle (w), and horizontal scrolling
// when word wrap is off. Content is word-wrapped by default.
type cellViewer struct {
	column   string
	viewport viewport.Model
}

// newCellViewer creates a cellViewer with the given column name and cell value.
// width and height define the viewport interior (not including the dialog frame).
// The value is sanitized to strip ANSI escape codes and unsafe control characters
// while preserving newlines and full length. Content wraps by default; press w
// to toggle word wrap off for horizontal scrolling.
func newCellViewer(column, value string, width, height int) *cellViewer {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.SetContent(chrome.DetailValue(sanitizeCellViewer(value)))
	vp.FillHeight = false
	vp.MouseWheelEnabled = true
	vp.SoftWrap = true
	return &cellViewer{column: column, viewport: vp}
}

// resize updates the viewport dimensions while retaining content and scroll position.
func (cv *cellViewer) resize(width, height int) {
	cv.viewport.SetWidth(width)
	cv.viewport.SetHeight(height)
}

// update handles messages for the viewer. Intercepts 'w' to toggle soft wrap;
// all other messages are delegated to the viewport (h/j/k/l, arrows, paging,
// mouse wheel).
func (cv *cellViewer) update(msg tea.Msg) tea.Cmd {
	if keyPress, ok := msg.(tea.KeyPressMsg); ok {
		switch keyPress.Key().Code {
		case 'w':
			cv.viewport.SoftWrap = !cv.viewport.SoftWrap
			if cv.viewport.SoftWrap {
				cv.viewport.SetXOffset(0)
			}
			return nil
		}
	}
	var cmd tea.Cmd
	cv.viewport, cmd = cv.viewport.Update(msg)
	return cmd
}

// content returns the title line and the viewport render, including scroll percentages.
func (cv *cellViewer) content() string {
	vPct := int(cv.viewport.ScrollPercent() * 100)
	hPct := int(cv.viewport.HorizontalScrollPercent() * 100)
	return fmt.Sprintf("View %s \u2014 V:%d%% H:%d%% | w wrap | Esc close\n%s",
		cv.column, vPct, hPct, cv.viewport.View())
}

// sanitizeCellViewer strips ANSI escape codes and unsafe control characters
// from a raw DB value while preserving newlines and full length. This prevents
// terminal injection via stored text while keeping the content readable.
func sanitizeCellViewer(input string) string {
	var display strings.Builder
	display.Grow(len(input))
	for i := 0; i < len(input); {
		r, size := rune(input[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(input[i:])
		}
		if r == '\x1b' {
			i += ansiSequenceLen(input[i:])
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

// ansiSequenceLen returns the length of an ANSI escape sequence starting at
// the beginning of input.
func ansiSequenceLen(input string) int {
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

// rawCellValue returns the untruncated value for the selected cell, falling
// back to the table's display value when raw data is unavailable.
func (m *Model) rawCellValue(tableType string, row, col int, displayValue string) string {
	var source [][]*string
	switch tableType {
	case "browse":
		source = m.browse.result.UntruncatedRows
	case "results":
		source = m.queryLog.resultsRaw
	}
	if row >= 0 && row < len(source) && col >= 0 && col < len(source[row]) {
		if cell := source[row][col]; cell != nil {
			return *cell
		}
		return "NULL"
	}
	return displayValue
}

// openCellViewer creates a cellViewer for the selected cell in the given result
// table. Returns nil if there is no selection or column is out of range.
func (m *Model) openCellViewer(resultTable table.Model, selectedColumn int, rawValue string) tea.Cmd {
	row := resultTable.Cursor()
	if row < 0 || row >= len(resultTable.Rows()) {
		return nil
	}
	columns := resultTable.Columns()
	if selectedColumn < 0 || selectedColumn >= len(columns) {
		return nil
	}

	columnTitle := columns[selectedColumn].Title

	m.browse.cellViewer = newCellViewer(columnTitle, rawValue, max(m.layout.width-8, 1), max(m.layout.height-10, 1))
	return nil
}
