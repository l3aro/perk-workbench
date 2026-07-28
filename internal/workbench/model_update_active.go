package workbench

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

func (m Model) updateActive(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.queryLogDetail != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok {
			switch {
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
			if m.results.Focused() {
				m.results, command = m.results.Update(message)
				return m, command
			}
			if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "schema.select_table", []scope{scopeView, scopeGlobal}) {
				if item, ok := m.schema.SelectedItem().(schemaItem); ok {
					if item.root {
						m.expandedDatabases[item.database] = !m.expandedDatabases[item.database]
						return m, m.rebuildSchemaTree()
					}
					return m, m.selectSchemaTable(item)
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
					m.columnForm.height = m.height
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
				if keyPress, ok := message.(tea.KeyPressMsg); ok && moveTableRow(&m.structure, &m.structureOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.structure, command = m.structure.Update(message)
			case tabBrowse:
				if m.browseForm.active() {
					m.browseForm.height = m.height
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
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.edit_cell", []scope{scopeView, scopeGlobal}) {
					return m, m.openCellEditor()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.edit", []scope{scopeView, scopeGlobal}) {
					return m, m.openBrowseForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch {
					case m.keybindings.Match(keyPress, "browse.yank_cell", []scope{scopeView, scopeGlobal}):
						return m, m.copyBrowseCell()
					case m.keybindings.Match(keyPress, "browse.context_menu", []scope{scopeView, scopeGlobal}):
						row := m.browse.Cursor()
						if row < 0 || row >= len(m.browseResult.Rows) {
							return m, nil
						}
						rows := m.browse.Rows()
						rowHeight := m.browse.Height()
						start := min(max(row-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
						menuX := m.schemaWidth + 1 - m.browseOffset
						for _, column := range m.browse.Columns()[:m.browseColumn] {
							menuX += column.Width + 2*spaceCompact
						}
						m.contextMenu = &contextMenuModel{
							options: []menuOption{
								{label: "Copy cell", action: "copy_cell", keys: "y"},
								{label: "Edit cell", action: "edit_cell", keys: "i"},
								{label: "Edit row", action: "edit_row", keys: "enter"},
								{label: "Delete row", action: "delete_row", keys: "d"},
							},
							visible: true,
							x:       menuX,
							y:       row - start + 6,
						}
						return m, nil
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
				if keyPress, ok := message.(tea.KeyPressMsg); ok && moveTableCell(&m.browse, &m.browseColumn, &m.browseOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.browse, command = m.browse.Update(message)
			case tabSQL:
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formMode.editing() && moveTableCell(&m.results, &m.resultsColumn, &m.resultsOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				if m.results.Focused() {
					m.results, command = m.results.Update(message)
				}
			case tabIndexes:
				if m.indexForm.active() {
					m.indexForm.height = m.height
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
							return m, nil
						}
						return m, nil
					}
					if moveTableRow(&m.indexes, &m.indexesOffset, m.tableViewportWidth, keyPress) {
						return m, nil
					}
				}
				m.indexes, command = m.indexes.Update(message)
			case tabForeignKeys:
				if m.foreignKeyForm.active() {
					m.foreignKeyForm.height = m.height
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
							return m, nil
						}
						return m, nil
					}
					if moveTableRow(&m.foreignKeys, &m.foreignKeysOffset, m.tableViewportWidth, keyPress) {
						return m, nil
					}
				}
				m.foreignKeys, command = m.foreignKeys.Update(message)
			}
			return m, command
		case focusQueryLog:
			if keyPress, ok := message.(tea.KeyPressMsg); ok {
				if !m.keybindings.Match(keyPress, "query_log.top_first", []scope{scopeView, scopeGlobal}) {
					if m.queryLogPendingG {
						m.queryLogPendingG = false
						return m, nil
					}
					m.queryLogPendingG = false
				}
				if m.keybindings.Match(keyPress, "query_log.next_page", []scope{scopeView, scopeGlobal}) {
					if m.queryLogPage+1 < m.queryLogPageCount() {
						m.queryLogPage++
						m.queryLog.SetCursor(0)
						m.renderQueryLog()
					}
					return m, nil
				}
				if m.keybindings.Match(keyPress, "query_log.prev_page", []scope{scopeView, scopeGlobal}) {
					if m.queryLogPage > 0 {
						m.queryLogPage--
						m.queryLog.SetCursor(0)
						m.renderQueryLog()
					}
					return m, nil
				}
				if moveTableCell(&m.queryLog, &m.queryLogColumn, &m.queryLogOffset, m.tableViewportWidth, keyPress) {
					return m, nil
				}
				rows := m.queryLog.Rows()
				if len(rows) == 0 {
					return m, nil
				}
				switch {
				case m.keybindings.Match(keyPress, "query_log.yank", []scope{scopeView, scopeGlobal}):
					entry, ok := m.queryLogSelectedEntry()
					if !ok {
						return m, nil
					}
					m.Status = "copied to clipboard"
					return m, copyQueryLogStatement(queryLogCell(entry, m.queryLogColumn))
				case m.keybindings.Match(keyPress, "query_log.explain", []scope{scopeView, scopeGlobal}):
					entry, ok := m.queryLogSelectedEntry()
					if !ok {
						return m, nil
					}
					m.explainPicker = newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, entry.statement, m.tableViewportWidth)
					if m.explainPicker == nil {
						return m, nil
					}
					return m, m.explainPicker.form.Init()
				case m.keybindings.Match(keyPress, "query_log.top_first", []scope{scopeView, scopeGlobal}):
					if m.queryLogPendingG {
						m.queryLog.SetCursor(0)
						m.queryLogColumn, m.queryLogOffset = 0, 0
						m.queryLogPendingG = false
					} else {
						m.queryLogPendingG = true
					}
					return m, nil
				case m.keybindings.Match(keyPress, "query_log.top_last", []scope{scopeView, scopeGlobal}):
					m.queryLog.SetCursor(len(rows) - 1)
					return m, nil
				case m.keybindings.Match(keyPress, "query_log.detail", []scope{scopeView, scopeGlobal}):
					if entry, ok := m.queryLogSelectedEntry(); ok {
						m.queryLogDetail = &entry
					}
					return m, nil
				}
			}
			m.queryLog, command = m.queryLog.Update(message)
			return m, command
		case focusChat:
			return m.updateChat(message)
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
