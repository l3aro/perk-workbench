package workbench

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type externalEditorFinishedMsg struct {
	value string
	err   error
}

func (m *Model) openExternalEditor() (tea.Cmd, bool) {
	value, ok := m.focusedTextValue()
	if !ok {
		return nil, false
	}
	command, err := externalEditorCommand(value)
	if err != nil {
		m.Status = safeText(fmt.Sprintf("opening editor: %v", err))
		return nil, true
	}
	return command, true
}

func externalEditorCommand(value string) (tea.Cmd, error) {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return nil, fmt.Errorf("$EDITOR is not set")
	}
	file, err := os.CreateTemp("", "perk-editor-*")
	if err != nil {
		return nil, fmt.Errorf("creating editor file: %w", err)
	}
	name := file.Name()
	if _, err := file.WriteString(value); err != nil {
		file.Close()
		os.Remove(name)
		return nil, fmt.Errorf("writing editor file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return nil, fmt.Errorf("closing editor file: %w", err)
	}
	return tea.ExecProcess(exec.Command("sh", "-c", "exec $EDITOR \"$1\"", "--", name), func(runErr error) tea.Msg {
		value, readErr := os.ReadFile(name)
		removeErr := os.Remove(name)
		if runErr != nil {
			return externalEditorFinishedMsg{err: runErr}
		}
		if readErr != nil {
			return externalEditorFinishedMsg{err: fmt.Errorf("reading editor file: %w", readErr)}
		}
		if removeErr != nil {
			return externalEditorFinishedMsg{err: fmt.Errorf("removing editor file: %w", removeErr)}
		}
		return externalEditorFinishedMsg{value: string(value)}
	}), nil
}

func (m Model) focusedTextValue() (string, bool) {
	switch m.State {
	case stateConnection:
		if m.recent.SettingFilter() {
			return m.recent.FilterValue(), true
		}
		switch m.connection.focus {
		case connectionFocusName:
			return m.connection.name.Value(), true
		case connectionFocusTarget:
			return m.connection.target.Value(), true
		case connectionFocusHost:
			return m.connection.host.Value(), true
		case connectionFocusPort:
			return m.connection.port.Value(), true
		case connectionFocusUsername:
			return m.connection.user.Value(), true
		case connectionFocusPassword:
			return m.connection.pass.Value(), true
		}
	case statePicking:
		if m.picker.SettingFilter() {
			return m.picker.FilterValue(), true
		}
	case stateReady:
		if m.Focus == focusSchema && m.schema.SettingFilter() {
			return m.schema.FilterValue(), true
		}
		if m.Focus != focusWorkspace {
			return "", false
		}
		switch m.Tab {
		case tabSQL:
			if m.editor.textarea.Focused() {
				return m.editor.textarea.Value(), true
			}
		case tabStructure:
			if m.columnForm.mode == columnFormInsert {
				if m.columnForm.name.Focused() {
					return m.columnForm.name.Value(), true
				}
				if m.columnForm.preset.Focused() {
					return m.columnForm.preset.Value(), true
				}
				for _, input := range m.columnForm.parameters {
					if input.Focused() {
						return input.Value(), true
					}
				}
			}
		case tabBrowse:
			if m.browseForm.mode == browseFormInsert {
				for _, input := range m.browseForm.inputs {
					if input.Focused() {
						return input.Value(), true
					}
				}
			}
		}
	}
	return "", false
}

func (m *Model) setFocusedTextValue(value string) bool {
	switch m.State {
	case stateConnection:
		if m.recent.SettingFilter() {
			m.recent.SetFilterText(value)
			return true
		}
		switch m.connection.focus {
		case connectionFocusName:
			m.connection.name.SetValue(value)
		case connectionFocusTarget:
			m.connection.target.SetValue(value)
		case connectionFocusHost:
			m.connection.host.SetValue(value)
		case connectionFocusPort:
			m.connection.port.SetValue(value)
		case connectionFocusUsername:
			m.connection.user.SetValue(value)
		case connectionFocusPassword:
			m.connection.pass.SetValue(value)
		default:
			return false
		}
		return true
	case statePicking:
		if m.picker.SettingFilter() {
			m.picker.SetFilterText(value)
			return true
		}
	case stateReady:
		if m.Focus == focusSchema && m.schema.SettingFilter() {
			m.schema.SetFilterText(value)
			return true
		}
		if m.Focus != focusWorkspace {
			return false
		}
		switch m.Tab {
		case tabSQL:
			if m.editor.textarea.Focused() {
				m.editor.textarea.SetValue(value)
				return true
			}
		case tabStructure:
			if m.columnForm.name.Focused() {
				m.columnForm.name.SetValue(value)
				return true
			}
			if m.columnForm.preset.Focused() {
				m.columnForm.preset.SetValue(value)
				return true
			}
			for index := range m.columnForm.parameters {
				if m.columnForm.parameters[index].Focused() {
					m.columnForm.parameters[index].SetValue(value)
					return true
				}
			}
		case tabBrowse:
			for index := range m.browseForm.inputs {
				if m.browseForm.inputs[index].Focused() {
					m.browseForm.inputs[index].SetValue(value)
					return true
				}
			}
		}
	}
	return false
}
