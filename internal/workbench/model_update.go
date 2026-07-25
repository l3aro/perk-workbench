package workbench

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
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
	if m.commandPalette.visible {
		if keyPress, ok := message.(tea.KeyPressMsg); ok {
			selectMsg, close, consumed := m.commandPalette.handleKey(keyPress)
			if consumed && !close && selectMsg.id == "" {
				return m, nil
			}
			if close && selectMsg.id == "" {
				return m, nil
			}
			if selectMsg.id != "" {
				return m.handlePaletteCommand(selectMsg.id)
			}
		}
	}
	if m.queryConfirmation != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.queryConfirmation = nil
			return m, nil
		}
		form, command := m.queryConfirmation.form.Update(message)
		m.queryConfirmation.form = form.(*huh.Form)
		if m.queryConfirmation.form.State != huh.StateCompleted {
			return m, command
		}
		statement, confirmed := m.queryConfirmation.statement, m.queryConfirmation.confirmed || m.queryConfirmation.form.GetBool("confirm")
		m.queryConfirmation = nil
		if !confirmed {
			return m, nil
		}
		return m.startQueryStatement(statement)
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
	if m.savedQueryPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.savedQueryPicker = nil
			return m, nil
		}
		form, command := m.savedQueryPicker.form.Update(message)
		m.savedQueryPicker.form = form.(*huh.Form)
		if m.savedQueryPicker.form.State != huh.StateCompleted {
			return m, command
		}
		m.editor.setValue(m.savedQueryPicker.selection)
		m.savedQueryPicker = nil
		m.Status = "loaded saved query"
		m.Focus, m.Tab = focusWorkspace, tabSQL
		m.blurTables()
		return m, m.formMode.beginInsert(m.editor)
	}
	if m.savedQueryPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.savedQueryPicker = nil
			return m, nil
		}
		form, command := m.savedQueryPicker.form.Update(message)
		m.savedQueryPicker.form = form.(*huh.Form)
		if m.savedQueryPicker.form.State != huh.StateCompleted {
			return m, command
		}
		m.editor.setValue(m.savedQueryPicker.selection)
		m.savedQueryPicker = nil
		m.Status = "loaded saved query"
		m.Focus, m.Tab = focusWorkspace, tabSQL
		m.blurTables()
		return m, m.formMode.beginInsert(m.editor)
	}
	if m.savedQueryPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.savedQueryPicker = nil
			return m, nil
		}
		form, command := m.savedQueryPicker.form.Update(message)
		m.savedQueryPicker.form = form.(*huh.Form)
		if m.savedQueryPicker.form.State != huh.StateCompleted {
			return m, command
		}
		m.editor.setValue(m.savedQueryPicker.selection)
		m.savedQueryPicker = nil
		m.Status = "loaded saved query"
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
		if m.keybindings.Match(message, "editor.external", []scope{scopeGlobal}) {
			if command, handled := m.openExternalEditor(); handled {
				return m, command
			}
		}
		if m.keybindings.Match(message, "app.palette", []scope{scopeGlobal}) && !m.hasOverlay() {
			m.commandPalette = newCommandPalette(m)
			m.commandPalette.visible = true
			return m, nil
		}
		quit := m.keybindings.Match(message, "app.quit", []scope{scopeGlobal})
		if quit && !m.formActive() && !m.schema.SettingFilter() &&
			!(m.State == stateConnection && (m.recent.SettingFilter() || (m.connection.focus == connectionFocusForm && m.formMode.editing()))) &&
			!(m.sqlEditorActive() && m.formMode.editing()) &&
			(m.Running() || m.State != stateReady || m.Focus != focusWorkspace || m.Tab != tabSQL || m.editor.value == "") {
			if m.Running() {
				m.RequestQuit()
				m.cancelQuery()
				return m, nil
			}
			return m, tea.Quit
		}
		if m.State == stateReady && m.keybindings.Match(message, "app.quit_dialog", []scope{scopeGlobal}) &&
			!m.hasOverlay() && !m.formActive() && !m.Running() {
			m.quitDialog = huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Key("action").
					Title("Quit?").
					Options(
						huh.NewOption("Disconnect", "disconnect"),
						huh.NewOption("Quit", "quit"),
						huh.NewOption("Cancel", "cancel"),
					),
			)).WithShowHelp(false).WithWidth(max(m.width/2, 40))
			return m, m.quitDialog.Init()
		}
		if m.State == stateReady && !m.formActive() && !m.schema.SettingFilter() && !(m.Focus == focusWorkspace && m.Tab == tabSQL && m.formMode.editing()) {
			switch {
			case m.keybindings.Match(message, "focus.schema", []scope{scopeGlobal}):
				m.Focus = focusSchema
				m.queryLogPendingG = false
				m.editor.text.Blur()
				m.blurTables()
				return m, nil
			case m.keybindings.Match(message, "focus.workspace", []scope{scopeGlobal}):
				m.Focus = focusWorkspace
				m.queryLogPendingG = false
				m.focusActiveTable()
				return m, nil
			case m.keybindings.Match(message, "focus.query_log", []scope{scopeGlobal}):
				m.Focus = focusQueryLog
				m.queryLogPendingG = false
				m.editor.text.Blur()
				m.blurTables()
				m.queryLog.Focus()
				if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
					m.queryLog.SetCursor(0)
				}
				return m, nil
			case m.keybindings.Match(message, "focus.toggle_fullscreen", []scope{scopeGlobal}):
				m.fullscreen = !m.fullscreen
				m.layout(m.width, m.height)
				return m, nil
			case m.keybindings.Match(message, "focus.cycle_forward", []scope{scopeGlobal}):
				m.cycleFocus(true)
				return m, nil
			case m.keybindings.Match(message, "focus.cycle_backward", []scope{scopeGlobal}):
				m.cycleFocus(false)
				return m, nil
			}
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL &&
			m.keybindings.Match(message, "query.execute", []scope{scopeGlobal}) {
			return m.executeQuery()
		}
		if m.Running() && m.keybindings.Match(message, "query.cancel", []scope{scopeGlobal}) {
			m.cancelQuery()
			return m, nil
		}
		if m.State == stateReady && !m.formActive() && m.keybindings.Match(message, "query.history", []scope{scopeGlobal}) && m.recallQueryHistory() {
			m.Focus, m.Tab = focusWorkspace, tabSQL
			m.blurTables()
			return m, m.formMode.beginInsert(m.editor)
		}
		if m.State == stateReady && !m.formActive() && m.keybindings.Match(message, "query.save", []scope{scopeGlobal}) {
			if saved, err := m.saveQuery(); err != nil {
				m.Status = safeText(fmt.Sprintf("saving query: %v", err))
			} else if saved {
				m.Status = "saved query"
			}
			return m, nil
		}
		if m.State == stateReady && !m.formActive() && m.keybindings.Match(message, "query.saved", []scope{scopeGlobal}) {
			m.savedQueryPicker = newSavedQueryPicker(m.savedQueries, m.tableViewportWidth)
			if m.savedQueryPicker != nil {
				return m, m.savedQueryPicker.form.Init()
			}
			return m, nil
		}
		if m.sqlEditorActive() {
			switch m.formMode.route(message, m.editor) {
			case formRouteConsumed:
				return m, nil
			case formRouteHuh:
				return m, m.editor.update(message)
			case formRouteParent:
				if m.keybindings.Match(message, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}) {
					return m, m.formMode.beginInsert(m.editor)
				}
			}
		}
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			switch {
			case m.keybindings.Match(message, "workspace.tab_next", []scope{scopeView}):
				m.toggleTab(true)
				return m, nil
			case m.keybindings.Match(message, "workspace.tab_prev", []scope{scopeView}):
				m.toggleTab(false)
				return m, nil
			}
		}
	}
	switch message := message.(type) {
	case tea.MouseClickMsg:
		if message.Button == tea.MouseLeft && !m.hasOverlay() {
			return m.handleLeftClick(message.X, message.Y)
		}
	case tea.MouseWheelMsg:
		if !m.hasOverlay() {
			return m.handleMouseWheel(message)
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
	if m.quitDialog != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.quitDialog = nil
			return m, nil
		}
		model, command := m.quitDialog.Update(message)
		m.quitDialog = model.(*huh.Form)
		if m.quitDialog.State != huh.StateCompleted {
			return m, command
		}
		action := m.quitDialog.GetString("action")
		m.quitDialog = nil
		switch action {
		case "quit":
			return m, tea.Quit
		case "disconnect":
			m.disconnect()
		}
		return m, nil
	}

	if m.queryLogDetail != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok {
			switch {
			case m.keybindings.Match(keyPress, "detail.yank", []scope{scopeView, scopeGlobal}):
				m.yankPicker = newYankPicker(*m.queryLogDetail, m.tableViewportWidth)
				m.queryLogDetail = nil
				return m, m.yankPicker.form.Init()
			case m.keybindings.Match(keyPress, "detail.explain", []scope{scopeView, scopeGlobal}):
				explain := newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, m.queryLogDetail.statement, m.tableViewportWidth)
				if explain == nil {
					return m, nil
				}
				m.explainPicker = explain
				m.queryLogDetail = nil
				return m, m.explainPicker.form.Init()
			case m.keybindings.Match(keyPress, "detail.close", []scope{scopeView, scopeGlobal}):
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
		if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "picker.reload", []scope{scopeView, scopeGlobal}) {
			m.Status = "reloading picker"
			return m, readDirectory(m.pickerDir)
		}
		if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "picker.select", []scope{scopeView, scopeGlobal}) {
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
			if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "schema.select_table", []scope{scopeView, scopeGlobal}) {
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
			if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formActive() && !(m.Tab == tabSQL && m.formMode.editing()) &&
				m.keybindings.Match(keyPress, "workspace.escape_to_schema", []scope{scopeView, scopeGlobal}) {
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
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "structure.edit", []scope{scopeView, scopeGlobal}) {
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
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.edit", []scope{scopeView, scopeGlobal}) {
					return m, m.openBrowseForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch {
					case m.keybindings.Match(keyPress, "browse.next_page", []scope{scopeView, scopeGlobal}):
						if m.browseLoading {
							return m, nil
						}
						m.browsePageTag++
						tag := m.browsePageTag
						table := m.SelectedTable
						return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
							return browseDebounceMsg{tag: tag, delta: 1, table: table}
						})
					case m.keybindings.Match(keyPress, "browse.prev_page", []scope{scopeView, scopeGlobal}):
						if m.browseLoading {
							return m, nil
						}
						m.browsePageTag++
						tag := m.browsePageTag
						table := m.SelectedTable
						return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
							return browseDebounceMsg{tag: tag, delta: -1, table: table}
						})
					}
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
					switch {
					case m.keybindings.Match(keyPress, "indexes.create", []scope{scopeView, scopeGlobal}):
						return m, m.openIndexForm(nil)
					case m.keybindings.Match(keyPress, "indexes.edit", []scope{scopeView, scopeGlobal}):
						row := m.indexes.Cursor()
						if row >= 0 && row < len(m.indexInfo) {
							return m, m.openIndexForm(&m.indexInfo[row])
						}
						return m, nil
					case m.keybindings.Match(keyPress, "indexes.delete", []scope{scopeView, scopeGlobal}):
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
					switch {
					case m.keybindings.Match(keyPress, "foreign_keys.toggle_diagram", []scope{scopeView, scopeGlobal}):
						m.relationshipDiagram = !m.relationshipDiagram
						return m, nil
					case m.keybindings.Match(keyPress, "foreign_keys.create", []scope{scopeView, scopeGlobal}):
						return m, m.openForeignKeyForm(nil)
					case m.keybindings.Match(keyPress, "foreign_keys.edit", []scope{scopeView, scopeGlobal}):
						row := m.foreignKeys.Cursor()
						if row >= 0 && row < len(m.foreignKeyInfo) {
							return m, m.openForeignKeyForm(&m.foreignKeyInfo[row])
						}
						return m, nil
					case m.keybindings.Match(keyPress, "foreign_keys.delete", []scope{scopeView, scopeGlobal}):
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
				if !m.keybindings.Match(keyPress, "query_log.top_first", []scope{scopeView, scopeGlobal}) {
					m.queryLogPendingG = false
				}
				if scrollTable(&m.queryLog, &m.queryLogOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				rows := m.queryLog.Rows()
				if len(rows) == 0 {
					return m, nil
				}
				switch {
				case m.keybindings.Match(keyPress, "query_log.yank", []scope{scopeView, scopeGlobal}):
					cursor := m.queryLog.Cursor()
					if cursor < 0 || cursor >= len(m.queryLogEntries) {
						return m, nil
					}
					m.yankPicker = newYankPicker(m.queryLogEntries[cursor], m.tableViewportWidth)
					return m, m.yankPicker.form.Init()
				case m.keybindings.Match(keyPress, "query_log.explain", []scope{scopeView, scopeGlobal}):
					cursor := m.queryLog.Cursor()
					if cursor < 0 || cursor >= len(m.queryLogEntries) {
						return m, nil
					}
					m.explainPicker = newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, m.queryLogEntries[cursor].statement, m.tableViewportWidth)
					if m.explainPicker == nil {
						return m, nil
					}
					return m, m.explainPicker.form.Init()
				case m.keybindings.Match(keyPress, "query_log.cursor_down", []scope{scopeView, scopeGlobal}):
					m.queryLog.SetCursor(min(m.queryLog.Cursor()+1, len(rows)-1))
					return m, nil
				case m.keybindings.Match(keyPress, "query_log.cursor_up", []scope{scopeView, scopeGlobal}):
					m.queryLog.SetCursor(max(m.queryLog.Cursor()-1, 0))
					return m, nil
				case m.keybindings.Match(keyPress, "query_log.top_first", []scope{scopeView, scopeGlobal}):
					if m.queryLogPendingG {
						m.queryLog.SetCursor(0)
						m.queryLogPendingG = false
					} else {
						m.queryLogPendingG = true
					}
					return m, nil
				case m.keybindings.Match(keyPress, "query_log.top_last", []scope{scopeView, scopeGlobal}):
					m.queryLog.SetCursor(len(rows) - 1)
					return m, nil
				case m.keybindings.Match(keyPress, "query_log.detail", []scope{scopeView, scopeGlobal}):
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
		if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "failure.return_to_picker", []scope{scopeView, scopeGlobal}) {
			m.RecoverToPicker("choose another database")
			return m, readDirectory(m.pickerDir)
		}
	}
	return m, nil
}

func (m Model) formActive() bool {
	return m.columnForm.active() || m.browseForm.active() || m.indexForm.active() || m.foreignKeyForm.active()
}

// handlePaletteCommand dispatches a command selected from the palette.
// It returns a (potentially mutated) model and any command to run.
func (m Model) handlePaletteCommand(id CommandID) (tea.Model, tea.Cmd) {
	m.commandPalette.visible = false

	switch id {
	case "app.quit":
		return m, tea.Quit
	case "editor.external":
		if cmd, handled := m.openExternalEditor(); handled {
			return m, cmd
		}
		return m, nil
	case "query.execute":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL {
			return m.executeQuery()
		}
		return m, nil
	case "query.cancel":
		if m.Running() {
			m.cancelQuery()
		}
		return m, nil
	case "focus.schema":
		if m.State == stateReady {
			m.Focus = focusSchema
			m.queryLogPendingG = false
			m.editor.text.Blur()
			m.blurTables()
		}
		return m, nil
	case "focus.workspace":
		if m.State == stateReady {
			m.Focus = focusWorkspace
			m.queryLogPendingG = false
			m.focusActiveTable()
		}
		return m, nil
	case "focus.query_log":
		if m.State == stateReady {
			m.Focus = focusQueryLog
			m.queryLogPendingG = false
			m.editor.text.Blur()
			m.blurTables()
			m.queryLog.Focus()
			if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
				m.queryLog.SetCursor(0)
			}
		}
		return m, nil
	case "focus.toggle_fullscreen":
		m.fullscreen = !m.fullscreen
		m.layout(m.width, m.height)
		return m, nil
	case "focus.cycle_forward":
		m.cycleFocus(true)
		return m, nil
	case "focus.cycle_backward":
		m.cycleFocus(false)
		return m, nil
	case "workspace.escape_to_schema":
		if m.State == stateReady && m.Focus == focusWorkspace && !m.formActive() {
			m.Focus = focusSchema
			m.editor.text.Blur()
			m.blurTables()
		}
		return m, nil
	case "workspace.tab_next":
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			m.toggleTab(true)
		}
		return m, nil
	case "workspace.tab_prev":
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			m.toggleTab(false)
		}
		return m, nil
	case "schema.select_table":
		if m.State == stateReady && m.Focus == focusSchema {
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
		return m, nil
	case "picker.reload":
		if m.State == statePicking {
			m.Status = "reloading picker"
			return m, readDirectory(m.pickerDir)
		}
		return m, nil
	case "picker.select":
		if m.State == statePicking {
			if item, ok := m.picker.SelectedItem().(pickerItem); ok {
				return m, selectPickerItem(item.raw)
			}
		}
		return m, nil
	case "failure.return_to_picker":
		if m.State == stateFailure {
			m.RecoverToPicker("choose another database")
			return m, readDirectory(m.pickerDir)
		}
		return m, nil
	case "browse.next_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() {
			if m.browseLoading {
				return m, nil
			}
			m.browsePageTag++
			tag := m.browsePageTag
			table := m.SelectedTable
			return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
				return browseDebounceMsg{tag: tag, delta: 1, table: table}
			})
		}
		return m, nil
	case "browse.prev_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() {
			if m.browseLoading {
				return m, nil
			}
			m.browsePageTag++
			tag := m.browsePageTag
			table := m.SelectedTable
			return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
				return browseDebounceMsg{tag: tag, delta: -1, table: table}
			})
		}
		return m, nil
	case "foreign_keys.toggle_diagram":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys {
			m.relationshipDiagram = !m.relationshipDiagram
		}
		return m, nil
	case "indexes.create":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabIndexes && !m.indexForm.active() {
			return m, m.openIndexForm(nil)
		}
		return m, nil
	case "foreign_keys.create":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys && !m.foreignKeyForm.active() {
			return m, m.openForeignKeyForm(nil)
		}
		return m, nil
	case "connection.switch_to_form":
		if m.State == stateConnection && m.connection.focus == connectionFocusRecent {
			m.connection.focus = connectionFocusForm
		}
		return m, nil
	case "connection.switch_to_list":
		if m.State == stateConnection && m.connection.focus == connectionFocusForm {
			m.connection.setFocus(connectionFocusRecent)
		}
		return m, nil
	case "connection.add":
		if m.State == stateConnection && m.connection.focus == connectionFocusRecent {
			return m, m.newConnection()
		}
		return m, nil
	case "connection.edit":
		if m.State == stateConnection && m.connection.focus == connectionFocusRecent {
			return m, m.editSelectedRecentConnection()
		}
		return m, nil
	case "connection.delete":
		if m.State == stateConnection && m.connection.focus == connectionFocusRecent {
			m.deleteSelectedRecentConnection()
			return m, nil
		}
		return m, nil
	default:
		return m, nil
	}
}
func (m Model) executeQuery() (tea.Model, tea.Cmd) {
	if requiresQueryConfirmation(m.editor.value) {
		m.queryConfirmation = newQueryConfirmation(m.editor.value, m.width)
		return m, m.queryConfirmation.form.Init()
	}
	return m.startQuery()
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

func (m *Model) cycleFocus(forward bool) {
	m.editor.text.Blur()
	m.blurTables()
	m.queryLogPendingG = false

	if forward {
		m.Focus = (m.Focus + 1) % 3
	} else {
		m.Focus = (m.Focus + 2) % 3
	}

	switch m.Focus {
	case focusSchema:
	case focusWorkspace:
		m.focusActiveTable()
	case focusQueryLog:
		m.queryLog.Focus()
		if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
			m.queryLog.SetCursor(0)
		}
	}
}

func (m Model) handleLeftClick(x, y int) (tea.Model, tea.Cmd) {
	if y == 0 || m.hasOverlay() {
		return m, nil
	}
	switch m.State {
	case stateReady:
		if m.compact {
			return m, nil
		}
		// Content starts at y=1 (after header). Schema on left, workspace+query log on right.
		contentY := y - 1
		if contentY < 0 {
			return m, nil
		}
		if x < m.schemaWidth {
			if m.Focus != focusSchema {
				m.Focus = focusSchema
				m.queryLogPendingG = false
				m.editor.text.Blur()
				m.blurTables()
			}
			return m.schemaClick(contentY)
		}
		if contentY < m.workspaceHeight {
			workspaceX := max(x-m.schemaWidth, 0)
			return m.handleWorkspaceClick(workspaceX, contentY)
		}
		return m.focusQueryLogClick(contentY - m.workspaceHeight)
	case stateConnection:
		if m.compact {
			return m, nil
		}
		if x < m.schemaWidth && m.connection.focus != connectionFocusRecent {
			m.connection.focus = connectionFocusRecent
			return m, nil
		}
		if x >= m.schemaWidth && m.connection.focus != connectionFocusForm {
			m.connection.focus = connectionFocusForm
		}
		return m, nil
	case statePicking:
		// Full-width picker: border(1) + title(1) + status(1), then items starting at contentY=4
		itemY := y - 1 - 4
		if itemY >= 0 {
			items := m.picker.VisibleItems()
			if len(items) > 0 {
				start, end := m.picker.Paginator.GetSliceBounds(len(items))
				visible := end - start
				if itemY < visible {
					m.picker.Select(start + itemY)
					if item, ok := m.picker.SelectedItem().(pickerItem); ok {
						return m, selectPickerItem(item.raw)
					}
				}
			}
		}
		return m, nil
	case stateFailure:
		m.RecoverToPicker("choose another database")
		return m, readDirectory(m.pickerDir)
	}
	return m, nil
}
func (m Model) handleWorkspaceClick(x, y int) (tea.Model, tea.Cmd) {
	if m.Focus != focusWorkspace {
		m.Focus = focusWorkspace
		m.queryLogPendingG = false
		m.focusActiveTable()
	}
	// Tab row is the first content row (y=0 of the workspace). Tab labels with padding:
	//   "Columns"(9) "Browse"(8) "SQL"(5) "Indexes"(9) "Foreign Keys"(15)
	// Total ~46 cells, starting at x=1 (after border + padding).
	if y == 0 {
		tabNames := []workspaceTab{tabStructure, tabBrowse, tabSQL, tabIndexes, tabForeignKeys}
		tabWidths := []int{9, 8, 5, 9, 15}
		cx := 1
		for i, w := range tabWidths {
			if x >= cx && x < cx+w {
				if m.Tab != tabNames[i] {
					m.Tab = tabNames[i]
					m.focusActiveTable()
				}
				return m, nil
			}
			cx += w
		}
	}
	return m, nil
}

func (m Model) schemaClick(contentY int) (tea.Model, tea.Cmd) {
	// contentY = terminal Y - 1 (after header).
	// Schema list renders: top border (1), title (1), status bar (1), then items.
	// First item is at contentY=4.
	itemY := contentY - 4
	if itemY < 0 {
		return m, nil
	}
	items := m.schema.VisibleItems()
	if len(items) == 0 {
		return m, nil
	}
	start, end := m.schema.Paginator.GetSliceBounds(len(items))
	visible := end - start
	if itemY >= visible {
		return m, nil
	}
	m.schema.Select(start + itemY)
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
	return m, nil
}
func (m Model) focusQueryLogClick(contentY int) (tea.Model, tea.Cmd) {
	if m.Focus != focusQueryLog {
		m.Focus = focusQueryLog
		m.queryLogPendingG = false
		m.editor.text.Blur()
		m.blurTables()
		m.queryLog.Focus()
		if len(m.queryLog.Rows()) > 0 && m.queryLog.Cursor() < 0 {
			m.queryLog.SetCursor(0)
		}
	}
	return m, nil
}

func (m Model) handleMouseWheel(wheel tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.height < 4 || m.width < 1 {
		return m, nil
	}
	// Forward wheel events to the focused area's table.
	step := 1
	if wheel.Button == tea.MouseWheelDown {
		step = 1
	} else if wheel.Button == tea.MouseWheelUp {
		step = -1
	} else {
		return m, nil
	}

	if m.State != stateReady {
		return m, nil
	}

	switch m.Focus {
	case focusSchema:
		return m, nil
	case focusWorkspace:
		m.scrollActiveWorkspaceTable(step)
	case focusQueryLog:
		rows := m.queryLog.Rows()
		rowCount := len(rows)
		if rowCount == 0 {
			return m, nil
		}
		newCursor := clamp(m.queryLog.Cursor()+step, 0, rowCount-1)
		m.queryLog.SetCursor(newCursor)
	}
	return m, nil
}

func (m *Model) scrollActiveWorkspaceTable(step int) {
	switch m.Tab {
	case tabStructure:
		rows := m.structure.Rows()
		newCursor := clamp(m.structure.Cursor()+step, 0, max(len(rows)-1, 0))
		m.structure.SetCursor(newCursor)
	case tabBrowse:
		rows := m.browse.Rows()
		newCursor := clamp(m.browse.Cursor()+step, 0, max(len(rows)-1, 0))
		m.browse.SetCursor(newCursor)
	case tabSQL:
		rows := m.results.Rows()
		newCursor := clamp(m.results.Cursor()+step, 0, max(len(rows)-1, 0))
		m.results.SetCursor(newCursor)
	case tabIndexes:
		rows := m.indexes.Rows()
		newCursor := clamp(m.indexes.Cursor()+step, 0, max(len(rows)-1, 0))
		m.indexes.SetCursor(newCursor)
	case tabForeignKeys:
		rows := m.foreignKeys.Rows()
		newCursor := clamp(m.foreignKeys.Cursor()+step, 0, max(len(rows)-1, 0))
		m.foreignKeys.SetCursor(newCursor)
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
