package workbench

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.running && !m.Workflow.Running() {
		m.Workflow.RestoreQuery(m.appContext, m.activeRequestID, m.cancelRequested)
		if m.pendingQuit {
			m.Workflow.RequestQuit()
		}
	}
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.layout(message.Width, message.Height)
		return m, nil
	case tea.KeyPressMsg:
		if message.Key().Code == 'e' && message.Key().Mod == tea.ModCtrl {
			if command, handled := m.openExternalEditor(); handled {
				return m, command
			}
		}
		if message.String() == "ctrl+c" || (message.String() == "q" && !m.formActive() && !m.schema.SettingFilter() && !(m.State == stateConnection && (m.recent.SettingFilter() || m.connection.inputFocused())) && (m.Running() || m.State != stateReady || m.Focus != focusWorkspace || m.Tab != tabSQL || m.editor.textarea.Value() == "")) {
			if m.Running() {
				m.RequestQuit()
				m.pendingQuit = true
				m.cancelQuery()
				return m, nil
			}
			return m, tea.Quit
		}
		if m.State == stateReady && !m.columnForm.active() && !m.schema.SettingFilter() {
			switch message.String() {
			case "1":
				m.Focus = focusSchema
				m.editor.textarea.Blur()
				m.blurTables()
				return m, nil
			case "2":
				m.Focus = focusWorkspace
				m.focusActiveTable()
				return m, nil
			}
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && m.executeKey(message) {
			return m.startQuery()
		}
		if m.Running() && !m.formActive() && message.Key().Code == tea.KeyEscape {
			m.cancelQuery()
			return m, nil
		}
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace && (message.String() == "tab" || message.String() == "shift+tab") {
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
			m.Status = safeText(fmt.Sprintf("unable to read directory: %v", message.err))
			return m, nil
		}
		m.Status = "choose a database"
		items := make([]list.Item, len(message.items))
		for index, item := range message.items {
			items[index] = item
		}
		return m, m.picker.SetItems(items)
	case pickerSelectionMsg:
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("unable to open selection: %v", message.err))
			return m, nil
		}
		if message.dir {
			return m, readDirectory(message.target)
		}
		m.BeginOpening(message.target, "opening database")
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
	case connectionTestMsg:
		return m.updateConnection(message)
	case columnAlteredMsg:
		return m.updateColumnAltered(message)
	case browseRowUpdatedMsg:
		return m.updateBrowseRowUpdated(message)
	case externalEditorFinishedMsg:
		if message.err != nil {
			m.Status = safeText(fmt.Sprintf("editor failed: %v", message.err))
			return m, nil
		}
		if !m.setFocusedTextValue(message.value) {
			m.Status = "editor target is no longer focused"
		}
		return m, nil
	}

	return m.updateActive(message)
}

func (m Model) updateOpen(message databaseOpenedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.Fail(safeText(fmt.Sprintf("database unavailable: %v", message.err)))
		return m, nil
	}
	m.Opened(message.target, message.service, "")
	m.databaseInfo = message.info
	m.Focus = focusSchema
	m.editor.textarea.Blur()
	m.blurTables()
	m.recordConnection()
	name := filepath.Base(message.target)
	if configured := strings.TrimSpace(m.connection.name.Value()); configured != "" {
		name = configured
	}
	m.Status = safeText("ready: " + name)
	items := make([]list.Item, len(message.objects))
	for index, object := range message.objects {
		items[index] = schemaItem{title: safeText(object.Name)}
	}
	return m, m.schema.SetItems(items)
}

func (m Model) updateActive(message tea.Msg) (tea.Model, tea.Cmd) {
	switch m.State {
	case stateConnection:
		return m.updateConnection(message)
	case statePicking:
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "r" {
			m.Status = "reloading picker"
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
		switch m.Focus {
		case focusSchema:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.String() == "enter" {
				if item, ok := m.schema.SelectedItem().(schemaItem); ok {
					m.SelectTable(item.title)
					m.focusActiveTable()
					return m, tea.Batch(m.loadTableInfo(), m.loadBrowse())
				}
			}
			m.schema, command = m.schema.Update(message)
			return m, command
		case focusWorkspace:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formActive() && keyPress.String() == "esc" {
				m.Focus = focusSchema
				m.editor.textarea.Blur()
				m.blurTables()
				return m, nil
			}
			switch m.Tab {
			case tabStructure:
				if m.columnForm.active() {
					command, action := m.columnForm.Update(message)
					switch action {
					case columnFormSave:
						m.columnForm.saving = true
						return m, m.alterColumn()
					case columnFormDiscard:
						m.columnForm = columnForm{}
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && (keyPress.String() == "enter" || keyPress.String() == "i") {
					m.openColumnForm()
					return m, nil
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && scrollTable(&m.structure, &m.structureOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.structure, command = m.structure.Update(message)
			case tabBrowse:
				if m.browseForm.active() {
					command, action := m.browseForm.Update(message)
					switch action {
					case browseFormSave:
						m.browseForm.saving = true
						return m, m.updateBrowseRow()
					case browseFormDiscard:
						m.browseForm = browseForm{}
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && (keyPress.String() == "enter" || keyPress.String() == "i") {
					m.openBrowseForm()
					return m, nil
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && (keyPress.String() == "n" || keyPress.String() == "p") {
					if keyPress.String() == "n" {
						m.ChangeBrowsePage(1)
						return m, m.loadBrowse()
					}
					if m.ChangeBrowsePage(-1) {
						return m, m.loadBrowse()
					}
					return m, nil
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && scrollTable(&m.browse, &m.browseOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.browse, command = m.browse.Update(message)
			case tabSQL:
				if m.results.Focused() {
					if keyPress, ok := message.(tea.KeyPressMsg); ok && scrollTable(&m.results, &m.resultsOffset, m.tableViewportWidth, keyPress) {
						return m, nil
					}
					m.results, command = m.results.Update(message)
				} else {
					m.editor, command = m.editor.Update(message)
				}
			}
			return m, command
		}
	case stateFailure:
		if keyPress, ok := message.(tea.KeyPressMsg); ok && (keyPress.String() == "enter" || keyPress.String() == "esc") {
			m.RecoverToPicker("choose another database")
			return m, readDirectory(m.pickerDir)
		}
	}
	return m, nil
}

func (m Model) formActive() bool { return m.columnForm.active() || m.browseForm.active() }

func scrollTable(resultTable *table.Model, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	switch keyPress.Key().Code {
	case tea.KeyLeft, 'h':
		*offset = tableOffset(*resultTable, *offset-1, viewportWidth)
	case tea.KeyRight, 'l':
		*offset = tableOffset(*resultTable, *offset+1, viewportWidth)
	default:
		return false
	}
	return true
}

func (m Model) executeKey(key tea.KeyPressMsg) bool {
	return (key.Key().Code == tea.KeyEnter && key.Key().Mod == tea.ModCtrl) || key.Key().Code == tea.KeyF5
}

func (m *Model) toggleTab(forward bool) {
	m.Workflow.ToggleTab(forward)
	m.focusActiveTable()
}

func (m *Model) focusActiveTable() {
	m.editor.textarea.Blur()
	m.blurTables()
	switch m.Tab {
	case tabStructure:
		m.structure.Focus()
	case tabBrowse:
		m.browse.Focus()
	case tabSQL:
		m.editor.textarea.Focus()
	}
}

func (m *Model) blurTables() {
	m.structure.Blur()
	m.browse.Blur()
	m.results.Blur()
}
