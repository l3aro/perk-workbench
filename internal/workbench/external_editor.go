package workbench

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type sqlEditorFinishedMsg struct {
	tag   uint64
	value string
	err   error
}

func (m *Model) openSQLExternalEditor() (tea.Cmd, bool) {
	if !m.sqlEditorActive() || !m.formMode.editing() {
		return nil, false
	}
	m.editorEditTag++
	command, err := sqlEditorCommand(m.editor.value, m.editorEditTag)
	if err != nil {
		m.Status = safeText(fmt.Sprintf("opening editor: %v", err))
		return nil, true
	}
	return command, true
}

func sqlEditorCommand(value string, tag uint64) (tea.Cmd, error) {
	command, complete, err := sqlEditorProcess(value, tag)
	if err != nil {
		return nil, err
	}
	return tea.ExecProcess(command, complete), nil
}

func sqlEditorProcess(value string, tag uint64) (*exec.Cmd, tea.ExecCallback, error) {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		return nil, nil, fmt.Errorf("$EDITOR is not set")
	}
	file, err := os.CreateTemp("", "perk-editor-*.sql")
	if err != nil {
		return nil, nil, fmt.Errorf("creating editor file: %w", err)
	}
	name := file.Name()
	if _, err := file.WriteString(value); err != nil {
		file.Close()
		os.Remove(name)
		return nil, nil, fmt.Errorf("writing editor file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return nil, nil, fmt.Errorf("closing editor file: %w", err)
	}
	return exec.Command(editor[0], append(editor[1:], name)...), func(runErr error) tea.Msg {
		updated, readErr := os.ReadFile(name)
		removeErr := os.Remove(name)
		if runErr != nil {
			return sqlEditorFinishedMsg{tag: tag, err: runErr}
		}
		if readErr != nil {
			return sqlEditorFinishedMsg{tag: tag, err: fmt.Errorf("reading editor file: %w", readErr)}
		}
		if removeErr != nil {
			return sqlEditorFinishedMsg{tag: tag, err: fmt.Errorf("removing editor file: %w", removeErr)}
		}
		return sqlEditorFinishedMsg{tag: tag, value: string(updated)}
	}, nil
}

func (m Model) updateSQLExternalEditor(message sqlEditorFinishedMsg) (tea.Model, tea.Cmd) {
	if message.tag != m.editorEditTag || !m.sqlEditorActive() || !m.formMode.editing() {
		m.Status = "editor target is no longer focused"
		return m, nil
	}
	if message.err != nil {
		m.Status = safeText(fmt.Sprintf("editor failed: %v", message.err))
		return m, nil
	}
	m.editor.setValue(message.value)
	return m, m.editor.text.Focus()
}
