package workbench

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type externalEditorTargetKind uint8

const (
	externalEditorTargetSQL externalEditorTargetKind = iota
	externalEditorTargetConnection
	externalEditorTargetColumn
	externalEditorTargetBrowse
	externalEditorTargetIndex
	externalEditorTargetForeignKey
)

type externalEditorLocation struct {
	kind externalEditorTargetKind
	key  string
}

type externalEditorTarget interface {
	externalEditorValue() string
	setExternalEditorValue(string)
	Focus() tea.Cmd
}

type sqlEditorFinishedMsg struct {
	tag      uint64
	location externalEditorLocation
	value    string
	err      error
}

func (m *Model) openExternalEditor() (tea.Cmd, bool) {
	target, location, ok := m.focusedExternalEditor()
	if !ok {
		return nil, false
	}
	m.editorEditTag++
	command, err := externalEditorCommand(target.externalEditorValue(), m.editorEditTag, location)
	if err != nil {
		m.Status = safeText(fmt.Sprintf("opening editor: %v", err))
		return nil, true
	}
	return command, true
}

func (m *Model) focusedExternalEditor() (externalEditorTarget, externalEditorLocation, bool) {
	if m.sqlEditorActive() && m.formMode.editing() {
		return m.editor, externalEditorLocation{kind: externalEditorTargetSQL}, true
	}
	if !m.formMode.editing() {
		return nil, externalEditorLocation{}, false
	}

	var (
		form     *huh.Form
		location externalEditorLocation
	)
	switch {
	case m.cellEditor != nil:
		return nil, externalEditorLocation{}, false
	case m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.confirmation == nil:
		form, location.kind = m.connection.form, externalEditorTargetConnection
	case m.columnForm.active() && !m.columnForm.confirming():
		form, location.kind = m.columnForm.form, externalEditorTargetColumn
	case m.browseForm.active() && !m.browseForm.confirming():
		form, location.kind = m.browseForm.form, externalEditorTargetBrowse
	case m.indexForm.active() && !m.indexForm.confirming():
		form, location.kind = m.indexForm.form, externalEditorTargetIndex
	case m.foreignKeyForm.active() && !m.foreignKeyForm.confirming():
		form, location.kind = m.foreignKeyForm.form, externalEditorTargetForeignKey
	default:
		return nil, externalEditorLocation{}, false
	}
	if form == nil {
		return nil, externalEditorLocation{}, false
	}
	target, ok := form.GetFocusedField().(externalEditorTarget)
	if !ok {
		return nil, externalEditorLocation{}, false
	}
	location.key = form.GetFocusedField().GetKey()
	return target, location, true
}

func externalEditorCommand(value string, tag uint64, location externalEditorLocation) (tea.Cmd, error) {
	command, complete, err := externalEditorProcess(value, tag, location)
	if err != nil {
		return nil, err
	}
	return tea.ExecProcess(command, complete), nil
}

func sqlEditorProcess(value string, tag uint64) (*exec.Cmd, tea.ExecCallback, error) {
	return externalEditorProcess(value, tag, externalEditorLocation{kind: externalEditorTargetSQL})
}

func externalEditorProcess(value string, tag uint64, location externalEditorLocation) (*exec.Cmd, tea.ExecCallback, error) {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		return nil, nil, fmt.Errorf("$EDITOR is not set")
	}
	extension := "txt"
	if location.kind == externalEditorTargetSQL {
		extension = "sql"
	}
	file, err := os.CreateTemp("", "perk-workbench-editor-*."+extension)
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
			return sqlEditorFinishedMsg{tag: tag, location: location, err: runErr}
		}
		if readErr != nil {
			return sqlEditorFinishedMsg{tag: tag, location: location, err: fmt.Errorf("reading editor file: %w", readErr)}
		}
		if removeErr != nil {
			return sqlEditorFinishedMsg{tag: tag, location: location, err: fmt.Errorf("removing editor file: %w", removeErr)}
		}
		return sqlEditorFinishedMsg{tag: tag, location: location, value: string(updated)}
	}, nil
}

func (m Model) updateExternalEditor(message sqlEditorFinishedMsg) (tea.Model, tea.Cmd) {
	target, location, ok := m.focusedExternalEditor()
	if message.tag != m.editorEditTag || !ok || message.location != location {
		m.Status = "editor target is no longer focused"
		return m, nil
	}
	if message.err != nil {
		m.Status = safeText(fmt.Sprintf("editor failed: %v", message.err))
		return m, nil
	}
	target.setExternalEditorValue(message.value)
	if target == m.editor && m.editor.value != message.value {
		m.editorValidity = sqlValidityPending
		return m, tea.Batch(target.Focus(), m.scheduleSQLValidation())
	}
	return m, target.Focus()
}
