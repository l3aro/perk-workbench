package workbench

import (
	"fmt"
	"path/filepath"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.layout(message.Width, message.Height)
		return m, nil
	case tea.KeyPressMsg:
		if message.String() == "ctrl+c" || (message.String() == "q" && (m.running || m.state != stateReady || m.focus != focusWorkspace || m.tab != tabSQL || m.editor.textarea.Value() == "")) {
			if m.running {
				m.pendingQuit = true
				m.cancelQuery()
				return m, nil
			}
			return m, tea.Quit
		}
		if m.state == stateReady {
			switch message.String() {
			case "1":
				m.focus = focusSchema
				m.editor.textarea.Blur()
				return m, nil
			case "2":
				m.focus = focusWorkspace
				if m.tab == tabSQL {
					m.editor.textarea.Focus()
				}
				return m, nil
			}
		}
		if m.state == stateReady && m.focus == focusWorkspace && m.tab == tabSQL && m.executeKey(message) {
			return m.startQuery()
		}
		if m.running && message.Key().Code == tea.KeyEscape {
			m.cancelQuery()
			return m, nil
		}
		if m.state == stateReady && m.focus == focusWorkspace && (message.String() == "tab" || message.String() == "shift+tab") {
			m.toggleTab(message.String() == "tab")
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
	case tableInfoMsg:
		return m.updateTableInfo(message)
	case browseTableMsg:
		return m.updateBrowse(message)
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
					m.selectedTable, m.browsePage, m.tab, m.focus = item.title, 0, tabStructure, focusWorkspace
					return m, tea.Batch(m.loadTableInfo(), m.loadBrowse())
				}
			}
			m.schema, command = m.schema.Update(message)
			return m, command
		case focusWorkspace:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "esc" {
				m.focus = focusSchema
				m.editor.textarea.Blur()
				return m, nil
			}
			switch m.tab {
			case tabStructure:
				m.structure, command = m.structure.Update(message)
			case tabBrowse:
				if keyPress, ok := message.(tea.KeyPressMsg); ok && (keyPress.String() == "n" || keyPress.String() == "p") {
					if keyPress.String() == "n" {
						m.browsePage++
					} else if m.browsePage > 0 {
						m.browsePage--
					}
					return m, m.loadBrowse()
				}
				m.browse, command = m.browse.Update(message)
			case tabSQL:
				m.editor, command = m.editor.Update(message)
			}
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

func (m *Model) toggleTab(forward bool) {
	step := workspaceTab(1)
	if !forward {
		step = 2
	}
	m.tab = (m.tab + step) % 3
	if m.tab == tabSQL {
		m.editor.textarea.Focus()
	} else {
		m.editor.textarea.Blur()
	}
}
