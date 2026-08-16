package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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
			// Pane keys route into the schema component, which owns the
			// filter input, the tree navigation and expansion, and the
			// add/rename/delete-table keys; the root applies the returned
			// events (table selection, reconnects, context menus, popup
			// requests) through its own overlays and DB flows.
			component, event, cmd := m.schema.component.Update(message, m.schemaLayout(), m.keybindings, m.schemaSnapshot())
			m.schema.component = component
			return m.applySchemaEvent(event, cmd)
		case focusWorkspace:
			if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.formActive() && !(m.Tab == tabQuery && m.overlay.formMode.Editing()) &&
				m.keybindings.Match(keyPress, "workspace.escape_to_schema", []scope{scopeView, scopeGlobal}) {
				m.Focus = focusSchema
				m.queryLog.editor.text.Blur()
				m.blurTables()
				return m, nil
			}
			switch m.Tab {
			case tabStructure:
				if m.schema.component.Structure.ColumnForm.Active() {
					component, action, cmd := m.schema.component.UpdateColumnForm(message, m.schemaLayout(), m.overlay.formMode)
					m.schema.component = component
					switch action {
					case schema.ColumnFormSave:
						if m.schema.component.Structure.ColumnForm.IsNew {
							return m, m.addColumn()
						}
						return m, m.alterColumn()
					case schema.ColumnFormDelete:
						return m, m.deleteColumn()
					}
					return m, cmd
				}
				// Pane keys and the table passthrough route into the
				// component; the root applies the events (filter/edit/
				// delete requests) and keeps the horizontal pan offset.
				component, event, cmd := m.schema.component.UpdateWorkspace(message, m.workspaceLayout(), m.keybindings, tabStructure, m.schemaSnapshot(), &m.layout.structureOffset)
				m.schema.component = component
				return m.applySchemaEvent(event, cmd)
			case tabBrowse:
				if m.browse.component.FilterForm != nil {
					component, result, cmd := m.browse.component.UpdateFilterForm(message, m.keybindings, m.overlay.formMode)
					m.browse.component = component
					switch result.Action {
					case browse.FilterApply:
						if result.Err != nil {
							m.setStatus(uikit.SafeText(result.Err.Error()))
							return m, nil
						}
						m.BrowsePage, m.browse.component.Loading = 0, true
						return m, m.loadBrowse()
					}
					return m, cmd
				}
				if m.browse.component.Form.Active() {
					component, action, cmd := m.browse.component.UpdateForm(message, browseLayout(m), m.overlay.formMode)
					m.browse.component = component
					switch action {
					case browse.FormSave:
						if m.browse.component.Form.Inserting {
							return m, m.insertBrowseRow()
						}
						return m, m.updateBrowseRow()
					}
					return m, cmd
				}
				// Object-list mode: a/e/d run the selected object's
				// add/edit/delete table actions directly, mirroring the
				// "," object menu. Row actions apply to table rows only.
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.add_table", []scope{scopeView, scopeGlobal}) {
					return m, m.browseObjectAction("add_table")
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.rename_table", []scope{scopeView, scopeGlobal}) {
					return m, m.browseObjectAction("rename_table")
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.delete_table", []scope{scopeView, scopeGlobal}) {
					return m, m.browseObjectAction("delete_table")
				}
				// Row actions (filter/edit/insert/view/context menu) apply
				// to table rows only; the object-list mode routes its own
				// keys (Enter opens, "," asks for the object menu) through
				// the component below.
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.refine", []scope{scopeView, scopeGlobal}) {
					return m, m.openBrowseFilterForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.edit_cell", []scope{scopeView, scopeGlobal}) {
					if m.writeCapabilities().RowWriter {
						return m, m.openCellEditor()
					}
					return m, m.openEditDocument()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.edit", []scope{scopeView, scopeGlobal}) {
					if m.writeCapabilities().RowWriter {
						return m, m.openBrowseForm()
					}
					return m, m.openEditDocument()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.insert_row", []scope{scopeView, scopeGlobal}) {
					return m, m.openInsertRowForm()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.delete_row", []scope{scopeView, scopeGlobal}) {
					if !m.browseWriteAvailable() {
						return m, nil
					}
					m.confirmBrowseRowDelete()
					return m, nil
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "cell.view", []scope{scopeView, scopeGlobal}) {
					row := m.browse.component.Table.Cursor()
					col := m.browse.component.SelectedColumn
					display := ""
					if row >= 0 && row < len(m.browse.component.Table.Rows()) && col >= 0 && col < len(m.browse.component.Table.Rows()[row]) {
						display = m.browse.component.Table.Rows()[row][col]
					}
					raw := m.rawCellValue("browse", row, col, display)
					return m, m.openCellViewer(m.browse.component.Table, col, raw)
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.browse.component.ObjectListMode() && m.keybindings.Match(keyPress, "browse.context_menu", []scope{scopeView, scopeGlobal}) {
					row := m.browse.component.Table.Cursor()
					if row < 0 || row >= len(m.browse.component.Result.Rows) {
						return m, nil
					}
					rows := m.browse.component.Table.Rows()
					rowHeight := m.browse.component.Table.Height()
					start := min(max(row-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
					menuX := m.layout.schemaWidth + 1 - m.browse.component.Offset
					for _, column := range m.browse.component.Table.Columns()[:m.browse.component.SelectedColumn] {
						menuX += column.Width + 2*spaceCompact
					}
					m.overlay.contextMenu = &contextMenuModel{
						options: m.browseRowMenuOptions(),
						visible: true,
						x:       menuX,
						y:       row - start + 6,
					}
					return m, nil
				}
				// Pane keys: route into the component, which owns the
				// sort/reset/copy/paging keys and the table passthrough;
				// the root applies the events (reloads, page ticks,
				// clipboard) and refreshes the status after navigation.
				component, event, cmd := m.browse.component.Update(message, browseLayout(m), m.keybindings, m.browseBackend())
				m.browse.component = component
				model, cmd := m.applyBrowseEvent(event, cmd)
				m = model.(Model)
				m.refreshBrowseStatus()
				return m, cmd
			case tabQuery:
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.overlay.formMode.Editing() && m.queryLog.results.Focused() && m.keybindings.Match(keyPress, "cell.view", []scope{scopeView, scopeGlobal}) {
					row := m.queryLog.results.Cursor()
					col := m.layout.resultsColumn
					display := ""
					if row >= 0 && row < len(m.queryLog.results.Rows()) && col >= 0 && col < len(m.queryLog.results.Rows()[row]) {
						display = m.queryLog.results.Rows()[row][col]
					}
					raw := m.rawCellValue("results", row, col, display)
					return m, m.openCellViewer(m.queryLog.results, col, raw)
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.overlay.formMode.Editing() && m.queryLog.results.Focused() && m.keybindings.Match(keyPress, "cell.yank", []scope{scopeView, scopeGlobal}) {
					return m, m.copySQLCell()
				}
				if keyPress, ok := message.(tea.KeyPressMsg); ok && !m.overlay.formMode.Editing() && moveTableCell(&m.queryLog.results, &m.layout.resultsColumn, &m.layout.resultsOffset, m.layout.tableViewportWidth, keyPress) {
					return m, nil
				}
				if m.queryLog.results.Focused() {
					m.queryLog.results, command = m.queryLog.results.Update(message)
				}
			case tabIndexes:
				if m.schema.component.Structure.IndexForm.Active() {
					component, action, cmd := m.schema.component.UpdateIndexForm(message, m.schemaLayout(), m.overlay.formMode)
					m.schema.component = component
					switch action {
					case schema.IndexFormSave:
						return m, m.saveIndex()
					case schema.IndexFormDelete:
						return m, m.deleteIndex()
					}
					return m, cmd
				}
				// Pane keys and the table passthrough route into the
				// component; the root applies the events (filter/edit/
				// delete requests) and keeps the horizontal pan offset.
				component, event, cmd := m.schema.component.UpdateWorkspace(message, m.workspaceLayout(), m.keybindings, tabIndexes, m.schemaSnapshot(), &m.layout.indexesOffset)
				m.schema.component = component
				return m.applySchemaEvent(event, cmd)
			case tabForeignKeys:
				if m.schema.component.Structure.ForeignKeyForm.Active() {
					component, action, cmd := m.schema.component.UpdateForeignKeyForm(message, m.schemaLayout(), m.overlay.formMode)
					m.schema.component = component
					switch action {
					case schema.ForeignKeyFormSave:
						return m, m.saveForeignKey()
					case schema.ForeignKeyFormDelete:
						return m, m.deleteForeignKey()
					}
					return m, cmd
				}
				// Pane keys and the table passthrough route into the
				// component; the root applies the events (filter/edit/
				// delete requests) and keeps the horizontal pan offset.
				component, event, cmd := m.schema.component.UpdateWorkspace(message, m.workspaceLayout(), m.keybindings, tabForeignKeys, m.schemaSnapshot(), &m.layout.foreignKeysOffset)
				m.schema.component = component
				return m.applySchemaEvent(event, cmd)
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
		entry, ok := m.queryLog.component.SelectedEntry()
		if !ok {
			return m, cmd
		}
		text, ok := m.queryLogYankText(entry)
		if !ok {
			m.setStatus("not replayable")
			return m, cmd
		}
		m.setStatus("copied to clipboard")
		if cmd == nil {
			return m, copyQueryLogStatement(text)
		}
		return m, tea.Batch(cmd, copyQueryLogStatement(text))
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
	return m.schema.component.Structure.TableFiltering || m.schema.component.Structure.ColumnForm.Active() || m.tableFormOpen() || m.browse.component.DocumentEditor != nil || m.browse.component.Form.Active() || m.browse.component.FilterForm != nil || m.schema.component.Structure.IndexForm.Active() || m.schema.component.Structure.ForeignKeyForm.Active()
}
