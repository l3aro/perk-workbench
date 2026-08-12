package workbench

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

func (m Model) updateActive(message tea.Msg) (tea.Model, tea.Cmd) {
	// The query-log detail overlay replaces normal content and consumes
	// every message while open.
	if m.queryLog.component.DetailOpen() {
		model, event, cmd := m.queryLog.component.Update(message, queryLogLayout(m), m.keybindings)
		m.queryLog.component = model
		return m.applyQueryLogEvent(event, cmd)
	}
	switch m.State {
	case stateConnection:
		return m.updateConnection(message)
	case statePicking:
		if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "picker.reload", []scope{scopeView, scopeGlobal}) {
			m.setStatus("reloading picker")
			return m, readDirectory(m.connection.pickerDir)
		}
		if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "picker.select", []scope{scopeView, scopeGlobal}) {
			if item, ok := m.connection.picker.SelectedItem().(pickerItem); ok {
				return m, selectPickerItem(item.raw)
			}
		}
		var command tea.Cmd
		m.connection.picker, command = m.connection.picker.Update(message)
		return m, command
	case stateReady:
		var command tea.Cmd
		switch m.Focus {
		case focusSchema:
			if m.queryLog.results.Focused() {
				m.queryLog.results, command = m.queryLog.results.Update(message)
				return m, command
			}
			if m.schema.filter.Focused() {
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch keyPress.Code {
					case tea.KeyEscape, tea.KeyEnter:
						// Exit editing, keeping the applied filter.
						m.schema.filter.Blur()
						return m, nil
					}
				}
				before := m.schema.filter.Value()
				var filterCommand tea.Cmd
				m.schema.filter, filterCommand = m.schema.filter.Update(message)
				if m.schema.filter.Value() != before {
					m.applySchemaFilter()
				}
				return m, filterCommand
			}
			if keyPress, ok := message.(tea.KeyPressMsg); ok {
				switch {
				case m.keybindings.Match(keyPress, "schema.filter", []scope{scopeView, scopeGlobal}):
					m.schema.filter.Focus()
					return m, nil
				case m.keybindings.Match(keyPress, "schema.context_menu", []scope{scopeView, scopeGlobal}):
					if item, ok := m.schema.list.SelectedItem().(schemaItem); ok {
						m.openSchemaItemMenu(item, m.layout.schemaWidth/2, m.schemaRowY(m.schema.list.Index())+1)
					} else if m.supportsCreateDatabase() {
						m.openBlankServerMenu(m.layout.schemaWidth/2, m.schemaRowY(0)+1)
					}
					return m, nil
				case m.keybindings.Match(keyPress, "schema.add_table", []scope{scopeView, scopeGlobal}):
					if item, ok := m.schema.list.SelectedItem().(schemaItem); ok {
						if target, ok := m.schemaAddTarget(item); ok {
							return m, m.openTableForm(target, "")
						}
					}
					return m, nil
				case m.keybindings.Match(keyPress, "schema.create_database", []scope{scopeView, scopeGlobal}):
					if m.supportsCreateDatabase() {
						return m, m.openDatabaseForm("")
					}
					return m, nil
				case m.keybindings.Match(keyPress, "schema.rename_table", []scope{scopeView, scopeGlobal}):
					if item, ok := m.schema.list.SelectedItem().(schemaItem); ok && !item.root && item.kind == "table" {
						return m, m.openTableForm(item.database, item.table)
					}
					return m, nil
				case m.keybindings.Match(keyPress, "schema.delete_table", []scope{scopeView, scopeGlobal}):
					if item, ok := m.schema.list.SelectedItem().(schemaItem); ok && !item.root && item.kind == "table" {
						m.confirmTableDelete(item.database, item.table)
					}
					return m, nil
				case m.keybindings.Match(keyPress, "schema.select_table", []scope{scopeView, scopeGlobal}):
					if item, ok := m.schema.list.SelectedItem().(schemaItem); ok {
						if item.kind == "schema" {
							return m, treeToggleCmd(m.toggleSchema(item.database, item.schema), m.rebuildSchemaTree())
						}
						if item.root {
							if m.databaseInfo.Product == "PostgreSQL" && !m.databaseRootConnected(item.database) {
								return m.reconnectDatabase(item.database)
							}
							return m, treeToggleCmd(m.toggleDatabase(item.database), m.rebuildSchemaTree())
						}
						return m, m.selectSchemaTable(item)
					}
				case m.keybindings.Match(keyPress, "schema.expand", []scope{scopeView, scopeGlobal}):
					return m.expandSchemaLevel()
				case m.keybindings.Match(keyPress, "schema.collapse", []scope{scopeView, scopeGlobal}):
					return m.collapseSchemaLevel()
				}
			}
			m.schema.list, command = m.schema.list.Update(message)
			// The list's own keymap can clear the filter (esc in tree
			// navigation); keep the visible input in sync.
			if !m.schema.list.IsFiltered() && m.schema.filter.Value() != "" {
				m.schema.filter.SetValue("")
			}
			return m, command
		case focusWorkspace:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formActive() && !(m.Tab == tabSQL && m.overlay.formMode.editing()) &&
				m.keybindings.Match(keyPress, "workspace.escape_to_schema", []scope{scopeView, scopeGlobal}) {
				m.Focus = focusSchema
				m.queryLog.editor.text.Blur()
				m.blurTables()
				return m, nil
			}
			switch m.Tab {
			case tabStructure:
				if m.structure.columnForm.active() {
					m.structure.columnForm.height = m.layout.height
					command, action := m.structure.columnForm.Update(message, m.overlay.formMode)
					switch action {
					case columnFormSave:
						m.structure.columnForm.saving = true
						if m.structure.columnForm.isNew {
							return m, m.addColumn()
						}
						return m, m.alterColumn()
					case columnFormDiscard:
						m.structure.columnForm = columnForm{}
					case columnFormDelete:
						m.structure.columnForm.saving = true
						return m, m.deleteColumn()
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch {
					case m.keybindings.Match(keyPress, "structure.filter", []scope{scopeView, scopeGlobal}):
						return m, m.openTableFilter()
					case m.keybindings.Match(keyPress, "structure.reset", []scope{scopeView, scopeGlobal}):
						m.resetTableFilter()
						return m, nil
					case m.keybindings.Match(keyPress, "structure.edit", []scope{scopeView, scopeGlobal}):
						return m, m.openColumnForm()
					case m.keybindings.Match(keyPress, "structure.add", []scope{scopeView, scopeGlobal}):
						return m, m.openNewColumnForm()
					case m.keybindings.Match(keyPress, "structure.delete", []scope{scopeView, scopeGlobal}):
						if column := m.selectedColumn(); column != nil {
							m.overlay.deletePending = "column"
							m.overlay.deletePendingName = column.Name
							m.overlay.deleteConfirm = newConfirmationDialog("Delete column?", "", []confirmationOption{
								{label: "Yes, delete", action: "delete"},
								{label: "Cancel", action: "cancel"},
							})
						}
						return m, nil
					}
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && moveTableRow(&m.structure.table, &m.layout.structureOffset, m.layout.tableViewportWidth, keyPress) {
					return m, nil
				}
				m.structure.table, command = m.structure.table.Update(message)
			case tabBrowse:
				if m.browse.filterForm != nil {
					command, action := m.browse.filterForm.Update(message, m.keybindings)
					if m.browse.filterForm.editing {
						m.overlay.formMode.mode = formModeInsert
					} else {
						m.overlay.formMode.mode = formModeNormal
					}
					switch action {
					case browseFilterDiscard:
						m.browse.filterForm = nil
					case browseFilterApply:
						settings, err := m.browse.filterForm.apply()
						if err != nil {
							m.setStatus(safeText(err.Error()))
							return m, nil
						}
						m.browse.settings = settings
						m.browse.filterForm = nil
						m.BrowsePage, m.browse.loading = 0, true
						m.browse.pageTag++
						return m, m.loadBrowse()
					}
					return m, command
				}
				if m.browse.form.active() {
					m.browse.form.height = m.layout.height
					command, action := m.browse.form.Update(message, m.overlay.formMode)
					switch action {
					case browseFormSave:
						m.browse.form.saving = true
						if m.browse.form.inserting {
							return m, m.insertBrowseRow()
						}
						return m, m.updateBrowseRow()
					case browseFormDiscard:
						m.browse.form = browseForm{}
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.refine", []scope{scopeView, scopeGlobal}) {
					return m, m.openBrowseFilterForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.reset", []scope{scopeView, scopeGlobal}) {
					return m, m.resetBrowseFilters()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.sort", []scope{scopeView, scopeGlobal}) {
					return m, m.cycleBrowseSort()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.edit_cell", []scope{scopeView, scopeGlobal}) {
					if m.writeCapabilities().RowWriter {
						return m, m.openCellEditor()
					}
					return m, m.openEditDocument()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.edit", []scope{scopeView, scopeGlobal}) {
					if m.writeCapabilities().RowWriter {
						return m, m.openBrowseForm()
					}
					return m, m.openEditDocument()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "browse.insert_row", []scope{scopeView, scopeGlobal}) {
					return m, m.openInsertRowForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "cell.view", []scope{scopeView, scopeGlobal}) {
					row := m.browse.table.Cursor()
					col := m.layout.browseColumn
					display := ""
					if row >= 0 && row < len(m.browse.table.Rows()) && col >= 0 && col < len(m.browse.table.Rows()[row]) {
						display = m.browse.table.Rows()[row][col]
					}
					raw := m.rawCellValue("browse", row, col, display)
					return m, m.openCellViewer(m.browse.table, col, raw)
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch {
					case m.keybindings.Match(keyPress, "cell.yank", []scope{scopeView, scopeGlobal}):
						return m, m.copyBrowseCell()
					case m.keybindings.Match(keyPress, "browse.context_menu", []scope{scopeView, scopeGlobal}):
						row := m.browse.table.Cursor()
						if row < 0 || row >= len(m.browse.result.Rows) {
							return m, nil
						}
						rows := m.browse.table.Rows()
						rowHeight := m.browse.table.Height()
						start := min(max(row-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
						menuX := m.layout.schemaWidth + 1 - m.layout.browseOffset
						for _, column := range m.browse.table.Columns()[:m.layout.browseColumn] {
							menuX += column.Width + 2*spaceCompact
						}
						m.overlay.contextMenu = &contextMenuModel{
							options: m.browseRowMenuOptions(),
							visible: true,
							x:       menuX,
							y:       row - start + 6,
						}
						return m, nil
					case m.keybindings.Match(keyPress, "browse.next_page", []scope{scopeView, scopeGlobal}):
						if m.browse.loading {
							return m, nil
						}
						m.browse.pageTag++
						tag := m.browse.pageTag
						table := m.SelectedTable
						return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
							return browseDebounceMsg{tag: tag, delta: 1, table: table}
						})
					case m.keybindings.Match(keyPress, "browse.prev_page", []scope{scopeView, scopeGlobal}):
						if m.browse.loading {
							return m, nil
						}
						m.browse.pageTag++
						tag := m.browse.pageTag
						table := m.SelectedTable
						return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
							return browseDebounceMsg{tag: tag, delta: -1, table: table}
						})
					}
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && moveTableCell(&m.browse.table, &m.layout.browseColumn, &m.layout.browseOffset, m.layout.tableViewportWidth, keyPress) {
					m.refreshBrowseStatus()
					return m, nil
				}
				m.browse.table, command = m.browse.table.Update(message)
				m.refreshBrowseStatus()
			case tabSQL:
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.overlay.formMode.editing() && m.queryLog.results.Focused() && m.keybindings.Match(keyPress, "cell.view", []scope{scopeView, scopeGlobal}) {
					row := m.queryLog.results.Cursor()
					col := m.layout.resultsColumn
					display := ""
					if row >= 0 && row < len(m.queryLog.results.Rows()) && col >= 0 && col < len(m.queryLog.results.Rows()[row]) {
						display = m.queryLog.results.Rows()[row][col]
					}
					raw := m.rawCellValue("results", row, col, display)
					return m, m.openCellViewer(m.queryLog.results, col, raw)
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.overlay.formMode.editing() && m.queryLog.results.Focused() && m.keybindings.Match(keyPress, "cell.yank", []scope{scopeView, scopeGlobal}) {
					return m, m.copySQLCell()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.overlay.formMode.editing() && moveTableCell(&m.queryLog.results, &m.layout.resultsColumn, &m.layout.resultsOffset, m.layout.tableViewportWidth, keyPress) {
					return m, nil
				}
				if m.queryLog.results.Focused() {
					m.queryLog.results, command = m.queryLog.results.Update(message)
				}
			case tabIndexes:
				if m.structure.indexForm.active() {
					m.structure.indexForm.height = m.layout.height
					command, action := m.structure.indexForm.Update(message, m.overlay.formMode)
					switch action {
					case indexFormSave:
						m.structure.indexForm.saving = true
						return m, m.saveIndex()
					case indexFormDelete:
						m.structure.indexForm.saving = true
						return m, m.deleteIndex()
					case indexFormDiscard:
						m.structure.indexForm.close()
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch {
					case m.keybindings.Match(keyPress, "indexes.filter", []scope{scopeView, scopeGlobal}):
						return m, m.openTableFilter()
					case m.keybindings.Match(keyPress, "indexes.reset", []scope{scopeView, scopeGlobal}):
						m.resetTableFilter()
						return m, nil
					case m.keybindings.Match(keyPress, "indexes.create", []scope{scopeView, scopeGlobal}):
						return m, m.openIndexForm(nil)
					case m.keybindings.Match(keyPress, "indexes.edit", []scope{scopeView, scopeGlobal}):
						if index := m.selectedIndex(); index != nil {
							return m, m.openIndexForm(index)
						}
						return m, nil
					case m.keybindings.Match(keyPress, "indexes.delete", []scope{scopeView, scopeGlobal}):
						if index := m.selectedIndex(); index != nil {
							m.overlay.deletePending = "index"
							m.overlay.deletePendingName = index.Name
							m.overlay.deleteConfirm = newConfirmationDialog("Delete index?", "", []confirmationOption{
								{label: "Yes, delete", action: "delete"},
								{label: "Cancel", action: "cancel"},
							})
						}
						return m, nil
					}
					if moveTableRow(&m.structure.indexes, &m.layout.indexesOffset, m.layout.tableViewportWidth, keyPress) {
						return m, nil
					}
				}
				m.structure.indexes, command = m.structure.indexes.Update(message)
			case tabForeignKeys:
				if m.structure.foreignKeyForm.active() {
					m.structure.foreignKeyForm.height = m.layout.height
					command, action := m.structure.foreignKeyForm.Update(message, m.overlay.formMode)
					switch action {
					case foreignKeyFormSave:
						m.structure.foreignKeyForm.saving = true
						return m, m.saveForeignKey()
					case foreignKeyFormDelete:
						m.structure.foreignKeyForm.saving = true
						return m, m.deleteForeignKey()
					case foreignKeyFormDiscard:
						m.structure.foreignKeyForm.close()
					}
					return m, command
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok {
					switch {
					case m.keybindings.Match(keyPress, "foreign_keys.filter", []scope{scopeView, scopeGlobal}):
						return m, m.openTableFilter()
					case m.keybindings.Match(keyPress, "foreign_keys.reset", []scope{scopeView, scopeGlobal}):
						m.resetTableFilter()
						return m, nil
					case m.keybindings.Match(keyPress, "foreign_keys.toggle_diagram", []scope{scopeView, scopeGlobal}):
						m.structure.relationshipDiagram = !m.structure.relationshipDiagram
						return m, nil
					case m.keybindings.Match(keyPress, "foreign_keys.create", []scope{scopeView, scopeGlobal}):
						return m, m.openForeignKeyForm(nil)
					case m.keybindings.Match(keyPress, "foreign_keys.edit", []scope{scopeView, scopeGlobal}):
						if foreignKey := m.selectedForeignKey(); foreignKey != nil {
							return m, m.openForeignKeyForm(foreignKey)
						}
						return m, nil
					case m.keybindings.Match(keyPress, "foreign_keys.delete", []scope{scopeView, scopeGlobal}):
						if foreignKey := m.selectedForeignKey(); foreignKey != nil {
							m.overlay.deletePending = "foreign_key"
							m.overlay.deletePendingName = foreignKey.ID
							m.overlay.deleteConfirm = newConfirmationDialog("Delete foreign key?", "", []confirmationOption{
								{label: "Yes, delete", action: "delete"},
								{label: "Cancel", action: "cancel"},
							})
						}
						return m, nil
					}
					if moveTableRow(&m.structure.foreignKeys, &m.layout.foreignKeysOffset, m.layout.tableViewportWidth, keyPress) {
						return m, nil
					}
				}
				m.structure.foreignKeys, command = m.structure.foreignKeys.Update(message)
			}
			return m, command
		case focusQueryLog:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "query_log.context_menu", []scope{scopeView, scopeGlobal}) {
				// The pending top mark gates the first press of any key:
				// an armed mark clears and stops without opening the menu.
				// The menu itself needs screen geometry, so root builds it.
				if m.queryLog.component.PendingG {
					m.queryLog.component.ClearPendingG()
					return m, nil
				}
				m.queryLog.component.ClearPendingG()
				if !m.queryLog.component.HasRows() {
					return m, nil
				}
				options := []menuOption{
					{label: "Detail", action: "query_log_detail", keys: "enter"},
					{label: "Copy cell", action: "query_log_yank", keys: "y"},
					{label: "Explain", action: "query_log_explain", keys: "e"},
				}
				maxLabel, maxKeys := 0, 0
				for _, option := range options {
					maxLabel = max(maxLabel, len(option.label))
					maxKeys = max(maxKeys, len(option.keys))
				}
				contentWidth := max(maxLabel+2+maxKeys+2, len("Row actions")+2, 24)
				menuWidth := contentWidth + 2
				menuX := min(max(m.layout.schemaWidth+1, 0), max(m.layout.width-menuWidth, 0))
				menuY := min(max(m.layout.workspaceHeight+4, 0), max(m.layout.height-(4+len(options)), 0))
				m.overlay.contextMenu = &contextMenuModel{
					options:  options,
					selected: 0,
					visible:  true,
					x:        menuX,
					y:        menuY,
				}
				return m, nil
			}
			model, event, cmd := m.queryLog.component.Update(message, queryLogLayout(m), m.keybindings)
			m.queryLog.component = model
			return m.applyQueryLogEvent(event, cmd)
		case focusChat:
			return m.updateChat(message)
		}
	case stateFailure:
		if keyPress, ok := message.(tea.KeyPressMsg); ok && m.keybindings.Match(keyPress, "failure.return_to_picker", []scope{scopeView, scopeGlobal}) {
			m.RecoverToPicker("choose another database")
			return m, readDirectory(m.connection.pickerDir)
		}
	}
	return m, nil
}

// queryLogLayout builds the layout snapshot root hands to the query-log
// component.
func queryLogLayout(m Model) uikit.Layout {
	return uikit.Layout{
		Width:         m.layout.width,
		Height:        m.layout.height,
		ViewportWidth: m.layout.tableViewportWidth,
		PaneHeight:    m.layout.queryLogHeight,
	}
}

// applyQueryLogEvent applies one query-log event: status transitions,
// clipboard copies, and EXPLAIN picker construction all stay root-owned.
func (m Model) applyQueryLogEvent(event uikit.Event, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch e := event.(type) {
	case nil:
		return m, cmd
	case uikit.StatusChanged:
		m.setStatus(uikit.SafeText(e.Text))
		return m, cmd
	case uikit.ClipboardRequested:
		m.setStatus("copied to clipboard")
		if cmd == nil {
			return m, copyQueryLogStatement(e.Text)
		}
		return m, tea.Batch(cmd, copyQueryLogStatement(e.Text))
	case uikit.ExplainRequested:
		picker := newExplainPicker(m.databaseInfo.Product, m.databaseInfo.Version, e.Statement, m.layout.tableViewportWidth)
		if picker == nil {
			// Unsupported product: keep whatever overlay state exists.
			return m, cmd
		}
		m.overlay.explainPicker = picker
		// A detail-triggered explain closes the detail only when the
		// picker was built, matching the original behavior.
		m.queryLog.component.CloseDetail()
		if cmd == nil {
			return m, picker.form.Init()
		}
		return m, tea.Batch(cmd, picker.form.Init())
	}
	return m, cmd
}

func (m Model) formActive() bool {
	return m.structure.tableFiltering || m.structure.columnForm.active() || m.tableFormOpen() || m.browse.documentEditor != nil || m.browse.form.active() || m.browse.filterForm != nil || m.structure.indexForm.active() || m.structure.foreignKeyForm.active()
}
