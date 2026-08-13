package browse

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// View renders the browse pane body: the filter/row forms, the scope
// object list, or the result table with its status line and pager row.
// The root frames the pane and routes overlay rendering.
func (m Model) View(layout uikit.Layout) string {
	if m.FilterForm != nil {
		// The filter view is already windowed at its scroll offset (it
		// renders at most one screenful per frame), so the viewport slice
		// must not re-apply the offset.
		return viewportSlice(m.FilterForm.View(), 0, layout)
	}
	if m.DocumentEditor != nil {
		return viewportSlice(m.DocumentEditor.View(), m.DocumentEditor.ScrollOffset, layout)
	}
	if m.Form.Active() {
		return viewportSlice(m.Form.View(), m.Form.ScrollOffset, layout)
	}
	if m.Objects != nil {
		return m.objectsView(layout)
	}
	view := uikit.TableViewportViewWithAlignment(m.Table, m.NumericColumns, m.Offset, layout.ViewportWidth, m.SelectedColumn) + "\n" + m.StatusLine(layout) + "\n\n" + m.PagerLine(layout)
	return view
}

// objectsView renders the scope object list: the name/kind/rows table
// with a status line and no pager (object lists are not paged).
func (m Model) objectsView(layout uikit.Layout) string {
	view := uikit.TableViewportViewWithAlignment(m.Table, nil, m.Offset, layout.ViewportWidth, m.SelectedColumn)
	return view + "\n" + m.ObjectsStatusLine(layout)
}

// ObjectsStatusHints is the keyboard-hint segment of the object-list
// status line; the object count is the other segment.
const ObjectsStatusHints = "enter open | , context menu"

// ObjectsStatusLine renders the object-list status line: the keyboard
// hints on the left, the object count on the right. Both segments are
// truncated so the line always fits the viewport width, mirroring the
// browse status line.
func (m Model) ObjectsStatusLine(layout uikit.Layout) string {
	width := layout.ViewportWidth
	left, right := ObjectsStatusHints, m.ObjectsStatus()
	if ansi.StringWidth(left)+2 > width {
		left = ansi.Truncate(left, max(width-2, 0), "…")
	}
	remaining := width - ansi.StringWidth(left) - 2
	if remaining < 2 {
		// Fewer than two cells left: drop the count entirely.
		return uikit.StatusStyle.Render(left)
	}
	if ansi.StringWidth(right)+2 > remaining {
		right = ansi.Truncate(right, remaining-2, "…")
	}
	return chrome.PaneStatus(uikit.StatusStyle.Render(left), uikit.StatusStyle.Render(right), width)
}

// ObjectsStatus is the right segment of the object-list status line: the
// number of objects in the scope.
func (m Model) ObjectsStatus() string {
	switch len(m.Objects) {
	case 0:
		return "no objects"
	case 1:
		return "1 object"
	default:
		return fmt.Sprintf("%d objects", len(m.Objects))
	}
}

// Draw renders nothing: the browse pane is a lipgloss pane; its canvas
// overlays (the cell viewer) are drawn by the root from the shared
// uikit.CellViewer. The contract mirrors the other feature components.
func (m Model) Draw(canvas uv.ScreenBuffer, layout uikit.Layout) {}

// viewportSlice clips a form view to the pane body height at the given
// scroll offset, matching the root formViewport helper.
func viewportSlice(view string, offset int, layout uikit.Layout) string {
	height := layout.PaneHeight
	lines := strings.Split(view, "\n")
	if len(lines) <= height {
		return view
	}
	offset = uikit.Clamp(offset, 0, len(lines)-height)
	return strings.Join(lines[offset:offset+height], "\n")
}

// StatusHints is the keyboard-hint segment of the browse status line; the
// row-range summary (Status) is the other segment.
const StatusHints = "/ filter | r reset | s sort column"

// StatusSplit reports whether the browse status line renders on two
// lines: the keyboard hints on the first, the row-range summary
// right-aligned on the second. It splits exactly when the single-line
// layout would truncate the summary (left + 4 = the two segments plus the
// two cells each reserves for its padding). The browse table height, the
// pager row's y position, and the pager click hit-test all mirror this
// choice, so it is the single source of truth.
func (m Model) StatusSplit(layout uikit.Layout) bool {
	return m.Status != "" && ansi.StringWidth(StatusHints)+4+ansi.StringWidth(m.Status) > layout.ViewportWidth
}

// FooterRows is the number of workspace rows the browse view reserves
// below its data rows: the status line, the footer gap, the pager button
// row, plus the pane chrome. A narrow viewport splits the status line
// onto two rows (StatusSplit), reserving one more.
func (m Model) FooterRows(layout uikit.Layout) int {
	if m.StatusSplit(layout) {
		return 9
	}
	return 8
}

// StatusLine renders the browse status line: the keyboard hints on the
// left, the row-range summary on the right. Both segments are truncated
// so the line always fits the viewport width: PaneStatus wraps
// overflowing text, which would push the pager button row below the fixed
// row the click handler tests. The n/p page hint is not offered because
// the pager button row below always renders that affordance.
//
// On narrow viewports where the segments would collide (StatusSplit) they
// move onto two lines, each keeping as much width as the viewport allows;
// large screens keep the single-line layout unchanged.
func (m Model) StatusLine(layout uikit.Layout) string {
	width := layout.ViewportWidth
	left, right := StatusHints, m.Status
	if m.StatusSplit(layout) {
		if ansi.StringWidth(left) > max(width-2, 0) {
			left = ansi.Truncate(left, max(width-2, 0), "…")
		}
		if ansi.StringWidth(right) > max(width-2, 0) {
			right = ansi.Truncate(right, max(width-2, 0), "…")
		}
		return uikit.StatusStyle.Render(left) + "\n" + chrome.PaneStatus("", uikit.StatusStyle.Render(right), width)
	}
	if ansi.StringWidth(left)+2 > width {
		left = ansi.Truncate(left, max(width-2, 0), "…")
	}
	remaining := width - ansi.StringWidth(left) - 2
	if remaining < 2 {
		// Fewer than two cells left: even the styled empty segment would
		// overflow, so drop the summary entirely.
		return uikit.StatusStyle.Render(left)
	}
	if ansi.StringWidth(right)+2 > remaining {
		right = ansi.Truncate(right, remaining-2, "…")
	}
	return chrome.PaneStatus(uikit.StatusStyle.Render(left), uikit.StatusStyle.Render(right), width)
}

// Pager is the shared pager button row: the rendered line, each button's
// content-x span, and whether each button is enabled. Prev is enabled
// from page 1 on; Next while the loaded page has more rows.
func (m Model) Pager(layout uikit.Layout) uikit.Pager {
	pager := uikit.NewPager(m.Page > 0, m.Result.HasMore)
	gap := max(layout.ViewportWidth-2-ansi.StringWidth(pager.Prev)-ansi.StringWidth(pager.Next), 0)
	pager.PrevStart = 1 // statusStyle pads the row by one cell on each side
	pager.NextStart = 1 + ansi.StringWidth(pager.Prev) + gap
	pager.Line = uikit.StatusStyle.Render(pager.Prev + strings.Repeat(" ", gap) + pager.Next)
	return pager
}

// PagerLine returns the pager button row.
func (m Model) PagerLine(layout uikit.Layout) string {
	return m.Pager(layout).Line
}
