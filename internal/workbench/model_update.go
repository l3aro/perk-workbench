package workbench

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

const browseDebounceDuration = 150 * time.Millisecond

type browseDebounceMsg struct {
	tag   uint64
	delta int
	table string
}

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
	case browseDebounceMsg:
		if message.tag != m.browsePageTag || message.table != m.SelectedTable || m.browseLoading {
			return m, nil
		}
		if message.delta > 0 && int64((m.BrowsePage+1)*browsePageSize) >= m.browseResult.TotalRows {
			return m, nil
		}
		if !m.ChangeBrowsePage(message.delta) {
			return m, nil
		}
		m.browseLoading = true
		return m, m.loadBrowse()
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
		if m.State == stateReady && !m.formActive() && !m.schema.SettingFilter() && !(m.Focus == focusWorkspace && m.Tab == tabSQL && m.editor.insert) {
			switch message.String() {
			case "1":
				m.Focus = focusSchema
				m.queryLogPendingG = false
				m.editor.textarea.Blur()
				m.blurTables()
				return m, nil
			case "2":
				m.Focus = focusWorkspace
				m.queryLogPendingG = false
				m.focusActiveTable()
				return m, nil
			case "3":
				m.Focus = focusQueryLog
				m.queryLogPendingG = false
				m.editor.textarea.Blur()
				m.blurTables()
				m.queryLog.Focus()
				if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
					m.queryLog.SetCursor(0)
				}
				return m, nil
			case "f":
				m.fullscreen = !m.fullscreen
				m.layout(m.width, m.height)
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
	case indexesLoadedMsg:
		return m.updateIndexes(message)
	case foreignKeysLoadedMsg:
		return m.updateForeignKeys(message)
	case indexChangedMsg:
		return m.updateIndexChanged(message)
	case indexDeletedMsg:
		return m.updateIndexDeleted(message)
	case foreignKeyChangedMsg:
		return m.updateForeignKeyChanged(message)
	case foreignKeyDeletedMsg:
		return m.updateForeignKeyDeleted(message)
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
					return m, tea.Batch(m.loadTableInfo(), m.loadBrowse(), m.loadIndexes(), m.loadForeignKeys())
				}
			}
			m.schema, command = m.schema.Update(message)
			return m, command
		case focusWorkspace:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formActive() && !(m.Tab == tabSQL && m.editor.insert) && keyPress.String() == "esc" {
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
					if m.browseLoading {
						return m, nil
					}
					delta := 1
					if keyPress.String() == "p" {
						delta = -1
					}
					m.browsePageTag++
					tag := m.browsePageTag
					table := m.SelectedTable
					return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
						return browseDebounceMsg{tag: tag, delta: delta, table: table}
					})
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && scrollTable(&m.browse, &m.browseOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.browse, command = m.browse.Update(message)
			case tabSQL:
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.editor.insert && scrollTable(&m.results, &m.resultsOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				if m.results.Focused() {
					m.results, command = m.results.Update(message)
				} else {
					m.editor, command = m.editor.Update(message)
				}
			case tabIndexes:
				if m.indexForm.active() {
					command, action := m.indexForm.Update(message)
					switch action {
					case indexFormSave:
						m.indexForm.saving = true
						return m, m.saveIndex()
					case indexFormDelete:
						m.indexForm.saving = true
						return m, m.deleteIndex()
					case indexFormDiscard:
						m.indexForm.close()
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch keyPress.String() {
					case "n":
						m.openIndexForm(nil)
						return m, nil
					case "enter", "i":
						row := m.indexes.Cursor()
						if row >= 0 && row < len(m.indexInfo) {
							m.openIndexForm(&m.indexInfo[row])
						}
						return m, nil
					case "d":
						row := m.indexes.Cursor()
						if row >= 0 && row < len(m.indexInfo) {
							m.openIndexForm(&m.indexInfo[row])
							m.indexForm.mode = indexFormConfirmDelete
						}
						return m, nil
					}
					if scrollTable(&m.indexes, &m.indexesOffset, m.tableViewportWidth, keyPress) {
						return m, nil
					}
				}
				m.indexes, command = m.indexes.Update(message)
			case tabForeignKeys:
				if m.foreignKeyForm.active() {
					command, action := m.foreignKeyForm.Update(message)
					switch action {
					case foreignKeyFormSave:
						m.foreignKeyForm.saving = true
						return m, m.saveForeignKey()
					case foreignKeyFormDelete:
						m.foreignKeyForm.saving = true
						return m, m.deleteForeignKey()
					case foreignKeyFormDiscard:
						m.foreignKeyForm.close()
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch keyPress.String() {
					case "n":
						m.openForeignKeyForm(nil)
						return m, nil
					case "enter", "i":
						row := m.foreignKeys.Cursor()
						if row >= 0 && row < len(m.foreignKeyInfo) {
							m.openForeignKeyForm(&m.foreignKeyInfo[row])
						}
						return m, nil
					case "d":
						row := m.foreignKeys.Cursor()
						if row >= 0 && row < len(m.foreignKeyInfo) {
							m.openForeignKeyForm(&m.foreignKeyInfo[row])
							m.foreignKeyForm.mode = foreignKeyFormConfirmDelete
						}
						return m, nil
					}
					if scrollTable(&m.foreignKeys, &m.foreignKeysOffset, m.tableViewportWidth, keyPress) {
						return m, nil
					}
				}
				m.foreignKeys, command = m.foreignKeys.Update(message)
			}
			return m, command
		case focusQueryLog:
			if keyPress, ok := message.(tea.KeyPressMsg); ok {
				if keyPress.String() != "g" {
					m.queryLogPendingG = false
				}
				if scrollTable(&m.queryLog, &m.queryLogOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				rows := m.queryLog.Rows()
				if len(rows) == 0 {
					return m, nil
				}
				switch keyPress.String() {
				case "j":
					m.queryLog.SetCursor(min(m.queryLog.Cursor()+1, len(rows)-1))
					return m, nil
				case "k":
					m.queryLog.SetCursor(max(m.queryLog.Cursor()-1, 0))
					return m, nil
				case "g":
					if m.queryLogPendingG {
						m.queryLog.SetCursor(0)
						m.queryLogPendingG = false
					} else {
						m.queryLogPendingG = true
					}
					return m, nil
				case "G":
					m.queryLog.SetCursor(len(rows) - 1)
					return m, nil
				}
			}
			m.queryLog, command = m.queryLog.Update(message)
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

func (m Model) formActive() bool {
	return m.columnForm.active() || m.browseForm.active() || m.indexForm.active() || m.foreignKeyForm.active()
}

func scrollTable(resultTable *table.Model, offset *int, viewportWidth int, keyPress tea.KeyPressMsg) bool {
	step := max(viewportWidth/2, 1)
	next := *offset
	switch keyPress.Key().Code {
	case tea.KeyLeft, 'h':
		next = tableOffset(*resultTable, *offset-step, viewportWidth)
	case tea.KeyRight, 'l':
		next = tableOffset(*resultTable, *offset+step, viewportWidth)
	default:
		return false
	}
	if next == *offset {
		return false
	}
	*offset = next
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
		if len(m.results.Rows()) > 0 {
			m.results.Focus()
		} else {
			m.editor.textarea.Focus()
		}
	case tabIndexes:
		m.indexes.Focus()
	case tabForeignKeys:
		m.foreignKeys.Focus()
	}
}

func (m *Model) blurTables() {
	m.structure.Blur()
	m.browse.Blur()
	m.results.Blur()
	m.indexes.Blur()
	m.foreignKeys.Blur()
	m.queryLog.Blur()
}
