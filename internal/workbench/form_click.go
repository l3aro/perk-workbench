package workbench

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// formFieldIndexAt maps a click on a rendered form view to a field index.
// titles lists every field's rendered title in render order. The click lands
// on viewLine of the visible viewport, which shows the full view scrolled by
// scrollOffset lines. It returns -1 when the click misses every field or the
// layout cannot be determined (e.g. the form is mid-save and renders status
// text instead of fields).
func formFieldIndexAt(view string, scrollOffset, viewLine int, titles []string) int {
	if viewLine < 0 || scrollOffset < 0 || len(titles) == 0 {
		return -1
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	target := viewLine + scrollOffset
	if target < 0 || target >= len(lines) {
		return -1
	}
	field, searchFrom := -1, 0
	for index, title := range titles {
		titleLine := -1
		for line := searchFrom; line < len(lines); line++ {
			if formLineIsTitle(lines[line], title) {
				titleLine = line
				break
			}
		}
		if titleLine < 0 {
			// The view is a focus-scrolled window (huh group viewport):
			// titles outside the visible region are absent. Skip them; the
			// first visible title anchors the mapping below.
			continue
		}
		if target < titleLine {
			// The click is above this title: it lands in the block of the
			// field right before it, whose title may be scrolled out.
			return index - 1
		}
		field, searchFrom = index, titleLine+1
	}
	return field
}

// formLineIsTitle reports whether a stripped form line is a field title.
// Huh frames field titles with a "┃ " gutter; custom fields may not.
// Titles must not collide with select option text rendered below them.
func formLineIsTitle(line, title string) bool {
	clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "┃"))
	return strings.HasPrefix(clean, title)
}

// scrollToFieldTitle returns the view line of the rendered title of the
// field at index (titles in render order), or (0, false) when the layout
// cannot be determined.
func scrollToFieldTitle(view string, titles []string, field int) (int, bool) {
	if field < 0 || field >= len(titles) {
		return 0, false
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	searchFrom := 0
	for index := 0; index <= field; index++ {
		titleLine := -1
		for line := searchFrom; line < len(lines); line++ {
			if formLineIsTitle(lines[line], titles[index]) {
				titleLine = line
				break
			}
		}
		if titleLine < 0 {
			return 0, false
		}
		searchFrom = titleLine + 1
	}
	return searchFrom - 1, true
}

// recordFormClick tracks form press positions for double-click detection.
// It returns true when the press is a double-click on the same spot.
func (m *Model) recordFormClick(x, y int) bool {
	now := time.Now()
	if !m.lastFormClickTime.IsZero() && now.Sub(m.lastFormClickTime) < doubleClickTimeout && m.lastFormClickX == x && m.lastFormClickY == y {
		m.lastFormClickTime = time.Time{}
		return true
	}
	m.lastFormClickTime, m.lastFormClickX, m.lastFormClickY = now, x, y
	return false
}

// clickFormField focuses the form field under a left-click and enters insert
// mode on a double-click (or on any click when vim mode is off — no mode
// switch needed to type). focus moves the field cursor to the clicked field;
// insert runs afterwards, in insert mode.
func (m Model) clickFormField(x, y int, view string, scrollOffset, viewLine int, titles []string, focus, insert func(int) tea.Cmd) (tea.Model, tea.Cmd) {
	field := formFieldIndexAt(view, scrollOffset, viewLine, titles)
	if field < 0 {
		return m, nil
	}
	if !m.vimMode || m.recordFormClick(x, y) {
		return m, tea.Batch(focus(field), insert(field))
	}
	return m, focus(field)
}

// connectionActionAt returns the connection form action whose rendered
// button the click landed on, or "" when it misses both buttons. viewLine
// is the pane-relative line of the click, relX its pane-relative column.
// The buttons render as "<Test connection> <Connect>" (stacked when the
// pane is narrow). "Connect" is a prefix of "connection", so only tokens
// that are not part of a longer word count as buttons.
func connectionActionAt(view string, viewLine, relX int) string {
	if viewLine < 0 {
		return ""
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	if viewLine >= len(lines) {
		return ""
	}
	line := lines[viewLine]
	for _, action := range []string{connectionActionTest, connectionActionConnect} {
		for offset := 0; offset < len(line); {
			index := strings.Index(line[offset:], action)
			if index < 0 {
				break
			}
			index += offset
			end := index + len(action)
			if end >= len(line) || line[end] == ' ' {
				if relX >= index && relX < end {
					return action
				}
			}
			offset = index + len(action)
		}
	}
	return ""
}

// workspaceLeft returns the screen X where the workspace pane's left border
// starts: 0 in the compact single-pane layout, after the schema pane in wide.
func (m Model) workspaceLeft() int {
	if m.compact {
		return 0
	}
	return m.schemaWidth
}

// handleFormClick handles left-click presses on active forms: a single click
// focuses the clicked field, a double-click enters insert mode on it.
// Presses only: model_update routes mouse releases through handleLeftClick.
func (m Model) handleFormClick(x, y int) (tea.Model, tea.Cmd) {
	if m.contextMenu != nil || m.hasOverlay() {
		return m, nil
	}
	contentY := y - 1
	if contentY < 0 {
		return m, nil
	}
	workspaceLeft := m.workspaceLeft()
	switch m.State {
	case stateReady:
		if m.chat.visible && (!m.compact && x >= m.schemaWidth+m.editorWidth || m.compact && m.Focus == focusChat) {
			// handleLeftClick focuses the chat pane on any click; a
			// double-click (or any click without vim mode) enters insert
			// mode on the input. The keep-insert flag stops the trailing
			// release from resetting the mode.
			if !m.vimMode || m.recordFormClick(x, y) {
				m.chatKeepInsert = true
				m.chat.chatMode = formModeInsert
				return m, m.chat.input.Focus()
			}
			return m, nil
		}
		if m.compact && m.Focus != focusWorkspace {
			return m, nil
		}
		if x < workspaceLeft || contentY < 3 || contentY >= m.workspaceHeight {
			return m, nil
		}
		// The Save/Cancel button bar sits two rows above the workspace footer;
		// it is rendered only while a form owns the tab.
		if m.formTabActive() && contentY == m.workspaceHeight-4 {
			switch formButtonAt(x - workspaceLeft - 1) {
			case "save":
				m.formMode.focusButtons()
				m.formButtonHit = true
				var command tea.Cmd
				m, command = m.formSaveCommand()
				return m, command
			case "cancel":
				m.formMode.focusButtons()
				m.formMode.buttonChoice = 1
				m.formButtonHit = true
				return m, formEscapeKeyPress()
			}
			return m, nil
		}
		// Any other click lands on a form field: leave the button bar.
		m.formMode.buttonsFocused = false
		switch m.Tab {
		case tabStructure:
			if m.columnForm.active() && !m.columnForm.confirming() {
				return m.clickFormField(x, y, m.columnForm.View(), m.columnForm.scrollOffset, contentY-3, m.columnForm.fieldTitles(), func(field int) tea.Cmd {
					m.columnForm.scrollToField(field)
					return m.columnForm.focusField(field)
				}, func(int) tea.Cmd { return m.formMode.beginHuh(m.columnForm.focus()) })
			}
		case tabBrowse:
			if m.browseFilterForm != nil {
				return m.handleFilterFormClick(x, y, contentY-3, workspaceLeft)
			}
			if m.browseForm.active() && !m.browseForm.confirming() {
				return m.clickFormField(x, y, m.browseForm.View(), m.browseForm.scrollOffset, contentY-3, m.browseForm.columns, m.browseForm.focusColumn, func(field int) tea.Cmd {
					m.browseForm.values.nulls[field] = false
					return m.formMode.beginHuh(m.browseForm.focus())
				})
			}
		case tabSQL:
			// The editor box fills the first editorHeight lines of the pane:
			// a single click focuses it, and without vim mode the click
			// also enters insert mode so typing works immediately. With vim
			// mode, a double-click enters insert.
			if !m.formActive() && contentY-3 < m.editorHeight {
				if !m.vimMode || m.recordFormClick(x, y) {
					return m, m.formMode.beginInsert(m.editor)
				}
				m.editor.text.Focus()
				return m, nil
			}
		case tabIndexes:
			if m.indexForm.active() && !m.indexForm.confirming() {
				return m.clickFormField(x, y, m.indexForm.View(), m.indexForm.scrollOffset, contentY-3, m.indexForm.fieldTitles(), m.indexForm.focusField, func(int) tea.Cmd { return m.formMode.beginHuh(m.indexForm.focus()) })
			}
		case tabForeignKeys:
			if m.foreignKeyForm.active() && !m.foreignKeyForm.confirming() && !m.relationshipDiagram {
				return m.clickFormField(x, y, m.foreignKeyForm.View(), m.foreignKeyForm.scrollOffset, contentY-3, m.foreignKeyForm.fieldTitles(), m.foreignKeyForm.focusField, func(int) tea.Cmd { return m.formMode.beginHuh(m.foreignKeyForm.focus()) })
			}
		}
	case stateConnection:
		if m.compact {
			if m.connection.focus == connectionFocusRecent {
				return m.handleRecentClick(x, y)
			}
		} else if x < m.schemaWidth {
			return m.handleRecentClick(x, y)
		}
		if m.connection.focus != connectionFocusForm || m.connection.form == nil || m.connection.confirmation != nil {
			return m, nil
		}
		// A click on the Test connection / Connect buttons executes the
		// action, matching Enter on the focused action field.
		if action := connectionActionAt(m.connection.View(), contentY-1, x-workspaceLeft-1); action != "" {
			m.formMode.mode = formModeNormal
			m.connection.blur()
			if action == connectionActionTest {
				return m, m.testConnection()
			}
			return m.openConnection()
		}
		return m.clickFormField(x, y, m.connection.View(), 0, contentY-1, m.connection.fieldTitles(), m.connection.focusField, func(int) tea.Cmd { return m.formMode.beginHuh(m.connection.focusForm()) })
	}
	return m, nil
}

// handleFilterFormClick focuses the clicked filter row and starts editing it
// on a double-click (or on any click without vim mode). The rendered view is:
// header line, one line per filter row, then the Rows limit row.
func (m Model) handleFilterFormClick(x, y, viewLine, workspaceLeft int) (tea.Model, tea.Cmd) {
	f := m.browseFilterForm
	row := viewLine + f.scrollOffset - 1
	if row < 0 || row > len(f.fields) {
		return m, nil
	}
	relX := x - workspaceLeft - 1 + f.horizontalOffset
	if !m.vimMode || m.recordFormClick(x, y) {
		f.row = row
		if row < len(f.fields) {
			f.cell = f.cellAtX(relX)
		}
		cmd, _ := f.beginEdit()
		return m, cmd
	}
	f.row = row
	f.revealSelection()
	return m, nil
}

// cellAtX maps a click x (relative to the filter form) to the operator or
// value cell of a row. Column and type cells fall back to the value cell.
func (f *browseFilterForm) cellAtX(relX int) browseFilterCell {
	start := 0
	for index, column := range f.columns() {
		end := start + column.Width + 2*spaceCompact
		if index == 2 && relX >= start && relX < end {
			return browseFilterOperatorCell
		}
		start = end
	}
	return browseFilterValueCell
}
