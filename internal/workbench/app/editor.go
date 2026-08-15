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
		e.text.input.SetValue(strings.Join(lines, "\n"))
		e.value = e.text.Value()
	}
	e.completion = completion{}
}
func (e editor) completionVisible() bool { return e.completion.visible() }

func (e editor) View() string { return e.text.View() }

func (m Model) completionOverlay() string {
	matches := m.queryLog.editor.completion.matches
	if len(matches) < 1 {
		return ""
	}

	// Scrollable viewport of up to 5 items, keeping selection visible.
	const viewSize = 5
	selected := m.queryLog.editor.completion.selected

	offset := selected - viewSize/2
	offset = max(offset, 0)
	offset = min(offset, max(len(matches)-viewSize, 0))

	end := min(offset+viewSize, len(matches))
	visible := matches[offset:end]

	var items []string
	for i, match := range visible {
		idx := offset + i
		if idx == selected {
			items = append(items, selectedCellStyle.Render(match.Label))
		} else {
			items = append(items, completionItemStyle.Render(match.Label))
		}
		// Detail shown in secondary muted style.
		if match.Detail != "" {
			detail := completionDetailStyle.Render(match.Detail)
			items[len(items)-1] = items[len(items)-1] + " " + detail
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
