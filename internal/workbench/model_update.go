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
	if window, ok := message.(tea.WindowSizeMsg); ok {
		m.layout(window.Width, window.Height)
		return m, nil
	}
	if m.explainPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.explainPicker = nil
			return m, nil
		}
		command := m.explainPicker.Update(message)
		if !m.explainPicker.completed() {
			return m, command
		}
		m.editor.setValue(m.explainPicker.query())
		m.explainPicker = nil
		m.Focus, m.Tab = focusWorkspace, tabSQL
		m.blurTables()
		return m, m.formMode.beginInsert(m.editor)
	}
	if m.yankPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.yankPicker = nil
			return m, nil
		}
		command := m.yankPicker.Update(message)
		if !m.yankPicker.completed() {
			return m, command
		}
		content := m.yankPicker.value()
		m.yankPicker = nil
		m.Status = "copied to clipboard"
		return m, copyQueryLogStatement(content)
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
		if message.String() == "ctrl+c" || (message.String() == "q" && !m.formActive() && !m.schema.SettingFilter() && !(m.State == stateConnection && (m.recent.SettingFilter() || (m.connection.focus == connectionFocusForm && m.formMode.editing()))) && !(m.sqlEditorActive() && m.formMode.editing()) && (m.Running() || m.State != stateReady || m.Focus != focusWorkspace || m.Tab != tabSQL || m.editor.value == "")) {
			if m.Running() {
				m.RequestQuit()
				m.cancelQuery()
				return m, nil
			}
			return m, tea.Quit
		}
		if m.State == stateReady && !m.formActive() && !m.schema.SettingFilter() && !(m.Focus == focusWorkspace && m.Tab == tabSQL && m.formMode.editing()) {
			switch message.String() {
			case "1":
				m.Focus = focusSchema
				m.queryLogPendingG = false
				m.editor.text.Blur()
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
				m.editor.text.Blur()
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
		if m.Running() && message.Key().Code == tea.KeyEscape {
			m.cancelQuery()
			return m, nil
		}
		if m.sqlEditorActive() {
			switch m.formMode.route(message, m.editor) {
			case formRouteConsumed:
				return m, nil
			case formRouteHuh:
				return m, m.editor.update(message)
			case formRouteParent:
				if message.String() == "i" {
					return m, m.formMode.beginInsert(m.editor)
				}
			}
		}
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			switch message.String() {
			case "tab", "]":
				m.toggleTab(true)
				return m, nil
			case "shift+tab", "[":
				m.toggleTab(false)
				return m, nil
			}
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
	case referencingForeignKeysLoadedMsg:
		return m.updateReferencingForeignKeys(message)
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
	case sqlEditorFinishedMsg:
		return m.updateExternalEditor(message)
	}

	if m.sqlEditorActive() && m.formMode.editing() {
		return m, m.editor.update(message)
	}

	return m.updateActive(message)
}

func (m Model) updateOpen(message databaseOpenedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		if m.connection.focus == connectionFocusForm {
			m.State = stateConnection
			m.Status = safeText(fmt.Sprintf("database unavailable: %v", message.err))
			m.formMode.mode = formModeNormal
			return m, nil
		}
		m.Fail(safeText(fmt.Sprintf("database unavailable: %v", message.err)))
		return m, nil
	}
	m.Opened(message.target, message.service, "")
	m.databaseInfo = message.info
	m.Focus = focusSchema
	m.editor.text.Blur()
	m.blurTables()
	m.recordConnection()
	name := filepath.Base(message.target)
	if configured := strings.TrimSpace(m.connection.values.name); configured != "" {
		name = configured
	}
	m.Status = safeText("ready: " + name)
	return m, m.setSchemaObjects(message.objects)
}

func (m Model) updateActive(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.queryLogDetail != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok {
			switch keyPress.String() {
			case "y":
				m.yankPicker = newYankPicker(*m.queryLogDetail, m.tableViewportWidth)
				m.queryLogDetail = nil
				return m, m.yankPicker.form.Init()
			case "e":
				explain := newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, m.queryLogDetail.statement, m.tableViewportWidth)
				if explain == nil {
					return m, nil
				}
				m.explainPicker = explain
				m.queryLogDetail = nil
				return m, m.explainPicker.form.Init()
			case "enter", "esc":
				m.queryLogDetail = nil
				return m, nil
			}
		}
		return m, nil
	}

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
					if item.root {
						m.expandedDatabases[item.database] = !m.expandedDatabases[item.database]
						return m, m.rebuildSchemaTree()
					}
					m.SelectTable(m.schemaTable(item))
					m.structureColumns = nil
					m.foreignKeyInfo = nil
					m.referencingForeignKeyInfo = nil
					m.relationshipDiagram = false
					m.focusActiveTable()
					return m, tea.Batch(m.loadTableInfo(), m.loadBrowse(), m.loadIndexes(), m.loadForeignKeys(), m.loadReferencingForeignKeys())
				}
			}
			m.schema, command = m.schema.Update(message)
			return m, command
		case focusWorkspace:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formActive() && !(m.Tab == tabSQL && m.formMode.editing()) && keyPress.String() == "esc" {
				m.Focus = focusSchema
				m.editor.text.Blur()
				m.blurTables()
				return m, nil
			}
			switch m.Tab {
			case tabStructure:
				if m.columnForm.active() {
					command, action := m.columnForm.Update(message, m.formMode)
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
					return m, m.openColumnForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && scrollTable(&m.structure, &m.structureOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.structure, command = m.structure.Update(message)
			case tabBrowse:
				if m.browseForm.active() {
					command, action := m.browseForm.Update(message, m.formMode)
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
					return m, m.openBrowseForm()
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
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formMode.editing() && scrollTable(&m.results, &m.resultsOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				if m.results.Focused() {
					m.results, command = m.results.Update(message)
				}
			case tabIndexes:
				if m.indexForm.active() {
					command, action := m.indexForm.Update(message, m.formMode)
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
						return m, m.openIndexForm(nil)
					case "enter", "i":
						row := m.indexes.Cursor()
						if row >= 0 && row < len(m.indexInfo) {
							return m, m.openIndexForm(&m.indexInfo[row])
						}
						return m, nil
					case "d":
						row := m.indexes.Cursor()
						if row >= 0 && row < len(m.indexInfo) {
							_ = m.openIndexForm(&m.indexInfo[row])
							m.indexForm.beginConfirmation(false, true)
							m.formMode.beginConfirm()
							return m, m.indexForm.confirmation.Init()
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
					command, action := m.foreignKeyForm.Update(message, m.formMode)
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
					case "g":
						m.relationshipDiagram = !m.relationshipDiagram
						return m, nil
					case "n":
						return m, m.openForeignKeyForm(nil)
					case "enter", "i":
						row := m.foreignKeys.Cursor()
						if row >= 0 && row < len(m.foreignKeyInfo) {
							return m, m.openForeignKeyForm(&m.foreignKeyInfo[row])
						}
						return m, nil
					case "d":
						row := m.foreignKeys.Cursor()
						if row >= 0 && row < len(m.foreignKeyInfo) {
							_ = m.openForeignKeyForm(&m.foreignKeyInfo[row])
							m.foreignKeyForm.beginConfirmation(false, true)
							m.formMode.beginConfirm()
							return m, m.foreignKeyForm.confirmation.Init()
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
				case "y":
					cursor := m.queryLog.Cursor()
					if cursor < 0 || cursor >= len(m.queryLogEntries) {
						return m, nil
					}
					m.yankPicker = newYankPicker(m.queryLogEntries[cursor], m.tableViewportWidth)
					return m, m.yankPicker.form.Init()
				case "e":
					cursor := m.queryLog.Cursor()
					if cursor < 0 || cursor >= len(m.queryLogEntries) {
						return m, nil
					}
					m.explainPicker = newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, m.queryLogEntries[cursor].statement, m.tableViewportWidth)
					if m.explainPicker == nil {
						return m, nil
					}
					return m, m.explainPicker.form.Init()
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
				case "enter":
					cursor := m.queryLog.Cursor()
					if cursor >= 0 && cursor < len(m.queryLogEntries) {
						entry := m.queryLogEntries[cursor]
						m.queryLogDetail = &entry
					}
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

func (m Model) sqlEditorActive() bool {
	return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL
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
	return (key.Key().Code == tea.KeyEnter && key.Key().Mod == tea.ModCtrl) ||
		(key.Key().Code == 's' && key.Key().Mod == tea.ModCtrl) ||
		key.Key().Code == tea.KeyF5
}

func (m *Model) toggleTab(forward bool) {
	m.Workflow.ToggleTab(forward)
	m.focusActiveTable()
}

func (m *Model) focusActiveTable() {
	m.editor.text.Blur()
	m.blurTables()
	switch m.Tab {
	case tabStructure:
		m.structure.Focus()
	case tabBrowse:
		m.browse.Focus()
	case tabSQL:
		if len(m.results.Rows()) > 0 {
			m.results.Focus()
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
