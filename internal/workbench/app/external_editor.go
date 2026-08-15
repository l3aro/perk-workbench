package app

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
	externalEditorTargetQuery externalEditorTargetKind = iota
	externalEditorTargetConnection
	externalEditorTargetColumn
	externalEditorTargetBrowse
	externalEditorTargetIndex
	externalEditorTargetForeignKey
	externalEditorTargetTable
)

type externalEditorLocation struct {
	kind externalEditorTargetKind
	key  string
}

type externalEditorTarget interface {
	ExternalEditorValue() string
	SetExternalEditorValue(string)
	Focus() tea.Cmd
}

type externalEditorFinishedMsg struct {
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
	m.queryLog.editorEditTag++
	command, err := externalEditorCommand(target.ExternalEditorValue(), m.queryLog.editorEditTag, location)
	if err != nil {
		m.setStatus(safeText(fmt.Sprintf("opening editor: %v", err)))
		return nil, true
	}
	return command, true
}

func (m *Model) focusedExternalEditor() (externalEditorTarget, externalEditorLocation, bool) {
	if m.queryEditorActive() && m.overlay.formMode.Editing() {
		return m.queryLog.editor, externalEditorLocation{kind: externalEditorTargetQuery}, true
	}
	if !m.overlay.formMode.Editing() {
		return nil, externalEditorLocation{}, false
	}

	var (
		form     *huh.Form
		location externalEditorLocation
	)
	switch {
	case m.browse.component.CellEditor != nil:
		return nil, externalEditorLocation{}, false
	case m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil:
		form, location.kind = m.connection.component.Form.Huh, externalEditorTargetConnection
	case m.schema.component.Structure.ColumnForm.Active() && !m.schema.component.Structure.ColumnForm.Confirming():
		form, location.kind = m.schema.component.Structure.ColumnForm.Form, externalEditorTargetColumn
	case m.browse.component.Form.Active() && !m.browse.component.Form.Confirming():
		form, location.kind = m.browse.component.Form.Form, externalEditorTargetBrowse
	case m.schema.component.Structure.IndexForm.Active() && !m.schema.component.Structure.IndexForm.Confirming():
		form, location.kind = m.schema.component.Structure.IndexForm.Form, externalEditorTargetIndex
	case m.schema.component.Structure.ForeignKeyForm.Active() && !m.schema.component.Structure.ForeignKeyForm.Confirming():
		form, location.kind = m.schema.component.Structure.ForeignKeyForm.Form, externalEditorTargetForeignKey
	case m.tableFormOpen():
		form, location.kind = m.schema.component.Structure.TableForm.Form, externalEditorTargetTable
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

func queryEditorProcess(value string, tag uint64) (*exec.Cmd, tea.ExecCallback, error) {
	return externalEditorProcess(value, tag, externalEditorLocation{kind: externalEditorTargetQuery})
}

func externalEditorProcess(value string, tag uint64, location externalEditorLocation) (*exec.Cmd, tea.ExecCallback, error) {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		return nil, nil, fmt.Errorf("$EDITOR is not set")
	}
	extension := "txt"
	if location.kind == externalEditorTargetQuery {
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
			return externalEditorFinishedMsg{tag: tag, location: location, err: runErr}
		}
		if readErr != nil {
			return externalEditorFinishedMsg{tag: tag, location: location, err: fmt.Errorf("reading editor file: %w", readErr)}
		}
		if removeErr != nil {
			return externalEditorFinishedMsg{tag: tag, location: location, err: fmt.Errorf("removing editor file: %w", removeErr)}
		}
		return externalEditorFinishedMsg{tag: tag, location: location, value: string(updated)}
	}, nil
}

func (m Model) updateExternalEditor(message externalEditorFinishedMsg) (tea.Model, tea.Cmd) {
	target, location, ok := m.focusedExternalEditor()
	if message.tag != m.queryLog.editorEditTag || !ok || message.location != location {
		m.setStatus("editor target is no longer focused")
		return m, nil
	}
	if message.err != nil {
		m.setStatus(safeText(fmt.Sprintf("editor failed: %v", message.err)))
		return m, nil
	}
	target.SetExternalEditorValue(message.value)
	if target == m.queryLog.editor && m.queryLog.editor.value != message.value {
		m.queryLog.editorValidity = sqlValidityPending
		return m, tea.Batch(target.Focus(), m.scheduleSQLValidation())
	}
	return m, target.Focus()
}
