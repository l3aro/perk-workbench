package workbench

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type editor struct {
	value         string
	text          *sqlTextarea
	completion    completion
	width, height int
}

func newEditor() *editor {
	editor := &editor{}
	editor.resetText()
	return editor
}

func (e *editor) resetText() {
	e.text = newSQLTextarea(max(e.width, 1), max(e.height, 1))
	e.text.SetValue(e.value)
}

func (e *editor) setValue(value string) {
	e.value = value
	e.resetText()
}

func (e *editor) externalEditorValue() string { return e.value }

func (e *editor) setExternalEditorValue(value string) { e.setValue(value) }

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

func (e *editor) showCompletion(values []string) {
	e.showCompletionFor(sharedsql.CompletionPrefix(e.value), values)
}

func (e *editor) showCompletionFor(prefix string, values []string) {
	e.completion = newCompletion(values)
	e.completion.filter(prefix)
}

func (e *editor) acceptCompletion() {
	value := e.completion.accept()
	prefix := e.completion.prefix
	if value != "" && strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
		e.text.input.InsertString(value[len(prefix):])
		e.value = e.text.Value()
	}
	e.completion = completion{}
}

func (e editor) completionVisible() bool { return len(e.completion.matches) > 0 }

func (e editor) View() string {
	view := e.text.View()
	if !e.completionVisible() {
		return view
	}
	lines := strings.Split(view, "\n")
	lines[len(lines)-1] = selectedCellStyle.Render(e.completion.accept())
	return strings.Join(lines, "\n")
}
