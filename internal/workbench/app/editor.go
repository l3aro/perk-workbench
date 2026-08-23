package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type editor struct {
	value         string
	text          *queryTextarea
	completion    completion
	language      sharedsql.QueryLanguage
	width, height int
}

func newEditor() *editor {
	editor := &editor{language: sharedsql.SQLQueryLanguage}
	editor.resetText()
	return editor
}

// setLanguage switches the editor's query language: the placeholder and
// the syntax lexer follow the advertisement. The textarea is rebuilt so
// the resolved lexer and placeholder apply atomically, and any visible
// completion overlay (SQL-only) is dropped.
func (e *editor) setLanguage(language sharedsql.QueryLanguage) {
	e.language = sharedsql.NormalizeQueryLanguage(language)
	e.completion = completion{}
	e.resetText()
}

func (e *editor) resetText() {
	e.text = newQueryTextarea(max(e.width, 1), max(e.height, 1), e.language)
	e.text.SetValue(e.value)
}

func (e *editor) setValue(value string) {
	e.value = value
	e.resetText()
}

func (e *editor) ExternalEditorValue() string { return e.value }

func (e *editor) SetExternalEditorValue(value string) { e.setValue(value) }

func (e *editor) Focus() tea.Cmd { return e.text.Focus() }

func (e *editor) setSize(width, height int) {
	e.width, e.height = width, height
	e.text.SetWidth(max(width, 1))
	e.text.SetHeight(max(height, 1))
}

func (e *editor) update(message tea.Msg) tea.Cmd {
	command := e.text.Update(message)
	e.value = e.text.Value()
	return command
}

func (e *editor) showCompletion(items []CompletionItem) {
	e.showCompletionFor(sharedsql.CompletionPrefix(e.value), items)
}

func (e *editor) showCompletionFor(prefix string, items []CompletionItem) {
	e.completion = newCompletion(items)
	e.completion.filter(prefix)
}

// commandCompletionActive reports whether the active language advertises
// a static command catalog the editor can complete from.
func (e *editor) commandCompletionActive() bool {
	return len(e.language.Commands) > 0
}

// showCommandCompletion opens the command completion overlay for the
// language's advertised catalog, filtered by the command token at the
// cursor, and reports whether it opened. It is a no-op (false) when the
// language advertises no commands, so the invoking key falls through to
// the SQL completion binding. The overlay is pure editor state:
// accepting never executes anything and never captures or persists
// values.
func (e *editor) showCommandCompletion() bool {
	if !e.commandCompletionActive() {
		return false
	}
	start, end := commandTokenSpan(e.value, e.text.input.Line(), e.text.input.Column())
	items := commandCompletionItems(e.language.Commands)
	e.showCompletionFor(string([]rune(e.value)[start:end]), items)
	return true
}

// commandTokenSpan returns the half-open rune span [start, end) of the
// command token at the cursor within value: the maximal run of letters,
// digits, and underscores around (line, col). The cursor may sit
// anywhere inside the token — including its middle — and the span is
// absolute so replacement survives multiline values.
func commandTokenSpan(value string, line, col int) (start, end int) {
	runes := []rune(value)
	offset := 0
	for l := 0; l < line; l++ {
		for offset < len(runes) && runes[offset] != '\n' {
			offset++
		}
		offset++ // the newline
	}
	cursor := offset + col
	start = cursor
	for start > 0 && isCommandTokenRune(runes[start-1]) {
		start--
	}
	end = cursor
	for end < len(runes) && isCommandTokenRune(runes[end]) {
		end++
	}
	return start, end
}

func isCommandTokenRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_'
}

// acceptVisibleCompletion accepts the selected completion with the
// accept rule of the active overlay: command completion replaces the
// whole command token at the cursor (the rest of the statement and the
// cursor position are preserved), SQL completion replaces the typed
// prefix. Nothing is ever executed.
func (e *editor) acceptVisibleCompletion() {
	if e.commandCompletionActive() {
		e.acceptCommandCompletion()
		return
	}
	e.acceptCompletion()
}

// acceptCommandCompletion inserts the selected command's canonical name
// plus a trailing space in place of the command token at the cursor —
// whether the cursor is at the token's start, middle, or end — and
// restores the cursor immediately after the inserted name on the same
// multiline statement. Escape/cancel leaves the statement untouched.
func (e *editor) acceptCommandCompletion() {
	item := e.completion.accept()
	e.completion = completion{}
	if item.Label == "" {
		return
	}
	value := e.text.Value()
	start, end := commandTokenSpan(value, e.text.input.Line(), e.text.input.Column())
	insertion := item.InsertText + " "
	runes := []rune(value)
	newValue := string(runes[:start]) + insertion + string(runes[end:])
	// SetValue leaves the cursor at the value end; walk it back to the
	// absolute insertion offset (the rune right after "NAME ") so the
	// rest of a multiline statement and the cursor position survive.
	newRunes := []rune(newValue)
	target := start + len([]rune(insertion))
	targetRow, targetCol := 0, 0
	for i := 0; i < target; i++ {
		if newRunes[i] == '\n' {
			targetRow++
			targetCol = 0
		} else {
			targetCol++
		}
	}
	lastRow := 0
	for _, r := range newRunes {
		if r == '\n' {
			lastRow++
		}
	}
	e.text.SetValue(newValue)
	for range lastRow - targetRow {
		e.text.input.CursorUp()
	}
	e.text.input.SetCursorColumn(targetCol)
	e.value = e.text.Value()
}

// refilter recomputes the completion match set after the value changed:
// command completion filters by the command token at the cursor, SQL
// completion by the word ending at the value's end.
func (e *editor) refilter() {
	if e.commandCompletionActive() {
		start, end := commandTokenSpan(e.value, e.text.input.Line(), e.text.input.Column())
		e.completion.filter(string([]rune(e.value)[start:end]))
		return
	}
	e.completion.filter(sharedsql.CompletionPrefix(e.value))
}

func (e *editor) acceptCompletion() {
	item := e.completion.accept()
	prefix := e.completion.prefix
	if item.Label != "" && strings.HasPrefix(strings.ToLower(item.Label), strings.ToLower(prefix)) {
		// Replace the typed prefix with the full completion text.
		// We manually splice the value: delete |prefix| chars before cursor
		// and insert the full InsertText.
		lines := strings.Split(e.text.Value(), "\n")
		line := e.text.input.Line()
		col := e.text.input.Column()
		prefixRunes := len([]rune(prefix))
		lineRunes := []rune(lines[line])
		newLine := string(lineRunes[:col-prefixRunes]) + item.InsertText + string(lineRunes[col:])
		lines[line] = newLine
		e.text.SetValue(strings.Join(lines, "\n"))
		e.value = e.text.Value()
	}
	e.completion = completion{}
}
func (e editor) completionVisible() bool { return e.completion.visible() }

func (e editor) View() string { return e.text.View() }

// completionView returns the visible completion window and the index of
// its first match within the full match set — the shared geometry of the
// overlay renderer and the mouse hit-test.
func (m Model) completionView() ([]CompletionItem, int) {
	matches := m.queryLog.editor.completion.matches
	if len(matches) < 1 {
		return nil, 0
	}
	// Scrollable viewport of up to 5 items, keeping selection visible.
	const viewSize = 5
	selected := m.queryLog.editor.completion.selected
	offset := selected - viewSize/2
	offset = max(offset, 0)
	offset = min(offset, max(len(matches)-viewSize, 0))
	return matches[offset:min(offset+viewSize, len(matches))], offset
}

func (m Model) completionOverlay() string {
	visible, offset := m.completionView()
	if len(visible) < 1 {
		return ""
	}

	var items []string
	for i, match := range visible {
		idx := offset + i
		if idx == m.queryLog.editor.completion.selected {
			items = append(items, selectedCellStyle.Render(match.Label))
		} else {
			items = append(items, completionItemStyle.Render(match.Label))
		}
		// Usage shown in secondary muted style, summary after it.
		if match.Detail != "" {
			detail := completionDetailStyle.Render(match.Detail)
			items[len(items)-1] = items[len(items)-1] + " " + detail
		}
		if match.Summary != "" {
			summary := completionDetailStyle.Render(match.Summary)
			items[len(items)-1] = items[len(items)-1] + " " + summary
		}
	}

	return completionBoxStyle.
		MaxWidth(m.queryLog.editor.width).
		Render(lipgloss.JoinVertical(lipgloss.Left, items...))
}
func (m Model) completionCursorOffset() int {
	styledLines := m.queryLog.editor.text.styledLines(m.queryLog.editor.value, max(m.queryLog.editor.width, 1))
	cursorLine := m.queryLog.editor.text.input.Line()
	cursorInfo := m.queryLog.editor.text.input.LineInfo()
	start := min(m.queryLog.editor.text.input.ScrollYOffset(), len(styledLines))

	for i, sl := range styledLines {
		if sl.hardLine == cursorLine && sl.subLine == cursorInfo.RowOffset {
			return i - start
		}
	}
	return -1
}
