package workbench

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type editor struct {
	value         string
	text          *huh.Text
	width, height int
}

func newEditor() *editor {
	editor := &editor{}
	editor.resetText()
	return editor
}

func (e *editor) resetText() {
	e.text = huh.NewText().Value(&e.value).ExternalEditor(false).EditorExtension("sql")
	e.text.WithKeyMap(huh.NewDefaultKeyMap())
	e.text.WithWidth(max(e.width, 1))
	e.text.WithHeight(max(e.height, 1))
}

func (e *editor) setValue(value string) {
	e.value = value
	e.resetText()
}

func (e *editor) setSize(width, height int) {
	e.width, e.height = width, height
	e.text.WithWidth(max(width, 1))
	e.text.WithHeight(max(height, 1))
}

func (e *editor) update(message tea.Msg) tea.Cmd {
	_, command := e.text.Update(message)
	return command
}
