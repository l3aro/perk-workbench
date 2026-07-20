package workbench

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.layout(message.Width, message.Height)
		return m, nil
	case tea.KeyPressMsg:
		if message.String() == "ctrl+c" || (message.String() == "q" && (m.running || m.state != stateReady || m.focus != focusEditor || m.editor.textarea.Value() == "")) {
			if m.running {
				m.pendingQuit = true
				m.cancelQuery()
				return m, nil
			}
			return m, tea.Quit
		}
		if m.state == stateReady && m.focus == focusEditor && m.executeKey(message) {
			return m.startQuery()
		}
		if m.running && message.Key().Code == tea.KeyEscape {
			m.cancelQuery()
			return m, nil
		}
		if m.state == stateReady && (message.String() == "tab" || message.String() == "shift+tab") {
			m.toggleFocus(message.String() == "tab")
			return m, nil
		}
	}

	switch message := message.(type) {
	case databaseOpenedMsg:
		return m.updateOpen(message)
	case directoryReadMsg:
		m.pickerDir = message.dir
		if message.err != nil {
			m.status = safeText(fmt.Sprintf("unable to read directory: %v", message.err))
			return m, nil
		}
		m.status = "choose a database"
		items := make([]list.Item, len(message.items))
		for index, item := range message.items {
			items[index] = item
		}
		return m, m.picker.SetItems(items)
	case pickerSelectionMsg:
		if message.err != nil {
			m.status = safeText(fmt.Sprintf("unable to open selection: %v", message.err))
			return m, nil
		}
		if message.dir {
			return m, readDirectory(message.target)
		}
		m.state = stateOpening
		m.status = "opening database"
		return m, m.openTarget(message.target)
	case querySucceededMsg:
		return m.updateQuerySuccess(message)
	case queryFailedMsg:
		return m.updateQueryFailure(message)
	case queryCanceledMsg:
		return m.updateQueryCanceled(message)
	}

	return m.updateActive(message)
}

func (m Model) updateOpen(message databaseOpenedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.state = stateFailure
		m.status = safeText(fmt.Sprintf("database unavailable: %v", message.err))
		return m, nil
	}
	m.state, m.target, m.service = stateReady, message.target, message.service
	m.status = safeText("ready: " + filepath.Base(message.target))
	items := make([]list.Item, len(message.objects))
	for index, object := range message.objects {
		items[index] = schemaItem{title: safeText(object.Name), description: safeText(object.Type)}
	}
	return m, m.schema.SetItems(items)
}

func (m Model) updateActive(message tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case statePicking:
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "r" {
			m.status = "reloading picker"
			return m, readDirectory(m.pickerDir)
		}
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "enter" {
			if item, ok := m.picker.SelectedItem().(pickerItem); ok {
				return m, selectPickerItem(item.raw)
			}
		}
		var command tea.Cmd
		m.picker, command = m.picker.Update(message)
		return m, command
	case stateReady:
		var command tea.Cmd
		switch m.focus {
		case focusSchema:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "enter" {
				if item, ok := m.schema.SelectedItem().(schemaItem); ok {
					m.editor.textarea.SetValue(fmt.Sprintf("SELECT sql FROM sqlite_schema WHERE type = '%s' AND name = '%s'", strings.ReplaceAll(item.description, "'", "''"), strings.ReplaceAll(item.title, "'", "''")))
					return m.startQuery()
				}
			}
			m.schema, command = m.schema.Update(message)
			return m, command
		case focusEditor:
			m.editor, command = m.editor.Update(message)
			return m, command
		case focusResults:
			m.results, command = m.results.Update(message)
			return m, command
		}
	case stateFailure:
		if keyPress, ok := message.(tea.KeyPressMsg); ok && (keyPress.String() == "enter" || keyPress.String() == "esc") {
			m.state, m.status = statePicking, "choose another database"
			return m, readDirectory(m.pickerDir)
		}
	}
	return m, nil
}

func (m Model) executeKey(key tea.KeyPressMsg) bool {
	return (key.Key().Code == tea.KeyEnter && key.Key().Mod == tea.ModCtrl) || key.Key().Code == tea.KeyF5
}

func (m *Model) toggleFocus(forward bool) {
	step := focus(1)
	if !forward {
		step = 2
	}
	m.focus = (m.focus + step) % 3
	m.editor.textarea.Blur()
	m.results.Blur()
	switch m.focus {
	case focusEditor:
		m.editor.textarea.Focus()
	case focusResults:
		m.results.Focus()
	}
}
