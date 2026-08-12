package workbench

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// handlePaletteCommand dispatches a command selected from the palette.
// It returns a (potentially mutated) model and any command to run.
func (m Model) handlePaletteCommand(id CommandID) (tea.Model, tea.Cmd) {
	m.overlay.commandPalette.visible = false

	switch id {
	case "notifications.show":
		m.notifications.component.OpenHistory(0, m.layout.width, m.layout.height)
		return m, nil
	case "theme.select":
		m.overlay.themePicker = newThemePicker()
		return m, nil
	case "vim.toggle":
		return m, m.toggleVimMode()
	case "table.open_target":
		m.overlay.tableTargetPicker = newTableTargetPicker()
		return m, nil
	case "theme.ocean":
		m.commitTheme(themeOcean)
		return m, nil
	case "theme.dracula":
		m.commitTheme(themeDracula)
		return m, nil
	case "theme.catppuccin":
		m.commitTheme(themeCatppuccin)
		return m, nil
	case "theme.nord":
		m.commitTheme(themeNord)
		return m, nil
	case "theme.monokai":
		m.commitTheme(themeMonokai)
		return m, nil
	case "theme.solarized":
		m.commitTheme(themeSolarized)
		return m, nil
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
			m.queryLog.component.PendingG = false
			m.queryLog.editor.text.Blur()
			m.blurTables()
		}
		return m, nil
	case "focus.workspace":
		if m.State == stateReady {
			m.Focus = focusWorkspace
			m.queryLog.component.PendingG = false
			m.focusActiveTable()
		}
		return m, nil
	case "focus.query_log":
		if m.State == stateReady {
			m.Focus = focusQueryLog
			m.queryLog.component.PendingG = false
			m.queryLog.editor.text.Blur()
			m.blurTables()
			m.queryLog.component.Table.Focus()
			if len(m.queryLog.component.Table.Rows()) > 0 && m.queryLog.component.Table.Cursor() < 0 {
				m.queryLog.component.Table.SetCursor(0)
			}
		}
		return m, nil
	case "focus.chat":
		if m.State == stateReady && m.chat.visible {
			m.Focus = focusChat
			m.queryLog.component.PendingG = false
			m.queryLog.editor.text.Blur()
			m.blurTables()
			if !m.vimMode {
				m.chat.chatMode = formModeInsert
				return m, m.chat.input.Focus()
			}
			m.chat.chatMode = formModeNormal
			return m, nil
		}
		return m, nil
	case "ai.toggle":
		m.toggleAI()
		return m, nil
	case "chat.delete":
		if m.State == stateReady && m.Focus == focusChat {
			return m, m.deleteChatHistory(false)
		}
		return m, nil
	case "chat.clear":
		if m.State == stateReady && m.Focus == focusChat {
			return m, m.deleteChatHistory(true)
		}
		return m, nil
	case "chat.apply_sql":
		if m.State == stateReady && m.Focus == focusChat {
			return m, m.applyChatSQL()
		}
		return m, nil
	case "focus.toggle_fullscreen":
		m.layout.fullscreen = !m.layout.fullscreen
		m.applyLayout(m.layout.width, m.layout.height)
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
			m.queryLog.editor.text.Blur()
			m.blurTables()
		}
		return m, nil
	case "workspace.tab_next":
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			return m, m.toggleTab(true)
		}
		return m, nil
	case "workspace.tab_prev":
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			return m, m.toggleTab(false)
		}
		return m, nil
	case "schema.select_table":
		if m.State == stateReady && m.Focus == focusSchema {
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
		}
		return m, nil
	case "schema.expand":
		if m.State == stateReady && m.Focus == focusSchema {
			return m.expandSchemaLevel()
		}
		return m, nil
	case "schema.collapse":
		if m.State == stateReady && m.Focus == focusSchema {
			return m.collapseSchemaLevel()
		}
		return m, nil
	case "schema.add_table":
		if m.State == stateReady && m.Focus == focusSchema {
			if item, ok := m.schema.list.SelectedItem().(schemaItem); ok {
				if target, ok := m.schemaAddTarget(item); ok {
					return m, m.openTableForm(target, "")
				}
			}
		}
		return m, nil
	case "schema.rename_table":
		if m.State == stateReady && m.Focus == focusSchema {
			if item, ok := m.schema.list.SelectedItem().(schemaItem); ok && !item.root && item.kind == "table" {
				return m, m.openTableForm(item.database, item.table)
			}
		}
		return m, nil
	case "schema.delete_table":
		if m.State == stateReady && m.Focus == focusSchema {
			if item, ok := m.schema.list.SelectedItem().(schemaItem); ok && !item.root && item.kind == "table" {
				m.confirmTableDelete(item.database, item.table)
			}
		}
		return m, nil
	case "picker.reload":
		if m.State == statePicking {
			m.setStatus("reloading picker")
			return m, readDirectory(m.connection.pickerDir)
		}
		return m, nil
	case "picker.select":
		if m.State == statePicking {
			if item, ok := m.connection.picker.SelectedItem().(pickerItem); ok {
				return m, selectPickerItem(item.raw)
			}
		}
		return m, nil
	case "failure.return_to_picker":
		if m.State == stateFailure {
			m.RecoverToPicker("choose another database")
			return m, readDirectory(m.connection.pickerDir)
		}
		return m, nil
	case "structure.filter", "indexes.filter", "foreign_keys.filter":
		if m.State == stateReady && m.Focus == focusWorkspace && !m.formActive() {
			return m, m.openTableFilter()
		}
		return m, nil
	case "structure.reset", "indexes.reset", "foreign_keys.reset":
		if m.State == stateReady && m.Focus == focusWorkspace && !m.formActive() {
			m.resetTableFilter()
		}
		return m, nil
	case "browse.refine":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
			return m, m.openBrowseFilterForm()
		}
		return m, nil
	case "browse.reset":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
			return m, m.resetBrowseFilters()
		}
		return m, nil
	case "browse.sort":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
			return m, m.cycleBrowseSort()
		}
		return m, nil
	case "cell.view":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
			row := m.browse.table.Cursor()
			col := m.layout.browseColumn
			display := ""
			if row >= 0 && row < len(m.browse.table.Rows()) && col >= 0 && col < len(m.browse.table.Rows()[row]) {
				display = m.browse.table.Rows()[row][col]
			}
			raw := m.rawCellValue("browse", row, col, display)
			return m, m.openCellViewer(m.browse.table, col, raw)
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && !m.overlay.formMode.editing() && m.queryLog.results.Focused() {
			row := m.queryLog.results.Cursor()
			col := m.layout.resultsColumn
			display := ""
			if row >= 0 && row < len(m.queryLog.results.Rows()) && col >= 0 && col < len(m.queryLog.results.Rows()[row]) {
				display = m.queryLog.results.Rows()[row][col]
			}
			raw := m.rawCellValue("results", row, col, display)
			return m, m.openCellViewer(m.queryLog.results, col, raw)
		}
		return m, nil
	case "cell.yank":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
			return m, m.copyBrowseCell()
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && !m.overlay.formMode.editing() && m.queryLog.results.Focused() {
			return m, m.copySQLCell()
		}
		return m, nil
	case "browse.next_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
			if m.browse.loading {
				return m, nil
			}
			m.browse.pageTag++
			tag := m.browse.pageTag
			table := m.SelectedTable
			return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
				return browseDebounceMsg{tag: tag, delta: 1, table: table}
			})
		}
		return m, nil
	case "browse.prev_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.form.active() && m.browse.filterForm == nil {
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
		return m, nil
	case "foreign_keys.toggle_diagram":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys {
			m.structure.relationshipDiagram = !m.structure.relationshipDiagram
		}
		return m, nil
	case "indexes.create":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabIndexes && !m.structure.indexForm.active() {
			return m, m.openIndexForm(nil)
		}
		return m, nil
	case "foreign_keys.create":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys && !m.structure.foreignKeyForm.active() {
			return m, m.openForeignKeyForm(nil)
		}
		return m, nil
	case "structure.add":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabStructure && !m.structure.columnForm.active() {
			return m, m.openNewColumnForm()
		}
		return m, nil
	case "connection.switch_to_form":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusRecent {
			m.connection.form.focus = connectionFocusForm
		}
		return m, nil
	case "connection.switch_to_list":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusForm {
			m.connection.form.setFocus(connectionFocusRecent)
		}
		return m, nil
	case "connection.execute":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusForm && m.connection.form.confirmation == nil {
			if err := m.connection.form.validate(); err != nil {
				m.setStatus(safeText(err.Error()))
				return m, m.connection.form.showValidationError()
			}
			m.overlay.formMode.beginConfirm()
			return m, m.connection.form.beginConfirmation()
		}
		return m, nil
	case "connection.action_enter":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusForm && m.connection.form.confirmation == nil && m.connectionActionFocused() {
			m.overlay.formMode.mode = formModeNormal
			m.connection.form.blur()
			if m.connection.form.values.action == connectionActionTest {
				return m, m.testConnection()
			}
			return m.openConnection()
		}
		return m, nil
	case "connection.edit_field":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusForm && m.connection.form.confirmation == nil && !m.connectionActionFocused() {
			return m, m.overlay.formMode.beginHuh(m.connection.form.focusForm())
		}
		return m, nil
	case "connection.field_next":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusForm && m.connection.form.confirmation == nil {
			return m, m.connection.form.form.NextField()
		}
		return m, nil
	case "connection.field_prev":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusForm && m.connection.form.confirmation == nil {
			return m, m.connection.form.form.PrevField()
		}
		return m, nil
	case "connection.add":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusRecent {
			return m, m.newConnection()
		}
		return m, nil
	case "connection.edit":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusRecent {
			return m, m.editSelectedRecentConnection()
		}
		return m, nil
	case "connection.delete":
		if m.State == stateConnection && m.connection.form.focus == connectionFocusRecent {
			m.confirmDeleteRecentConnection()
			return m, nil
		}
		return m, nil
	default:
		return m, nil
	}
}
