package workbench

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// handlePaletteCommand dispatches a command selected from the palette.
// It returns a (potentially mutated) model and any command to run.
func (m Model) handlePaletteCommand(id CommandID) (tea.Model, tea.Cmd) {
	m.commandPalette.visible = false

	switch id {
	case "theme.select":
		m.themePicker = newThemePicker()
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
	case "focus.chat":
		if m.State == stateReady && m.chat.visible {
			m.Focus = focusChat
			m.chat.chatMode = formModeNormal
			m.queryLogPendingG = false
			m.editor.text.Blur()
			m.blurTables()
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
			if item, ok := m.schema.SelectedItem().(schemaItem); ok {
				if item.root {
					m.expandedDatabases[item.database] = !m.expandedDatabases[item.database]
					return m, m.rebuildSchemaTree()
				}
				return m, m.selectSchemaTable(item)
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
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
			return m, m.openBrowseFilterForm()
		}
		return m, nil
	case "browse.reset":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
			return m, m.resetBrowseFilters()
		}
		return m, nil
	case "browse.sort":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
			return m, m.cycleBrowseSort()
		}
		return m, nil
	case "cell.view":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
			row := m.browse.Cursor()
			col := m.browseColumn
			display := ""
			if row >= 0 && row < len(m.browse.Rows()) && col >= 0 && col < len(m.browse.Rows()[row]) {
				display = m.browse.Rows()[row][col]
			}
			raw := m.rawCellValue("browse", row, col, display)
			return m, m.openCellViewer(m.browse, col, raw)
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && !m.formMode.editing() && m.results.Focused() {
			row := m.results.Cursor()
			col := m.resultsColumn
			display := ""
			if row >= 0 && row < len(m.results.Rows()) && col >= 0 && col < len(m.results.Rows()[row]) {
				display = m.results.Rows()[row][col]
			}
			raw := m.rawCellValue("results", row, col, display)
			return m, m.openCellViewer(m.results, col, raw)
		}
		return m, nil
	case "cell.yank":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
			return m, m.copyBrowseCell()
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && !m.formMode.editing() && m.results.Focused() {
			return m, m.copySQLCell()
		}
		return m, nil
	case "browse.next_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
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
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browseForm.active() && m.browseFilterForm == nil {
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
	case "structure.add":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabStructure && !m.columnForm.active() {
			return m, m.openNewColumnForm()
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
	case "connection.execute":
		if m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.confirmation == nil {
			if err := m.connection.validate(); err != nil {
				m.Status = safeText(err.Error())
				return m, m.connection.showValidationError()
			}
			m.formMode.beginConfirm()
			return m, m.connection.beginConfirmation()
		}
		return m, nil
	case "connection.action_enter":
		if m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.confirmation == nil && m.connectionActionFocused() {
			m.formMode.mode = formModeNormal
			m.connection.blur()
			if m.connection.values.action == connectionActionTest {
				return m, m.testConnection()
			}
			return m.openConnection()
		}
		return m, nil
	case "connection.edit_field":
		if m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.confirmation == nil && !m.connectionActionFocused() {
			return m, m.formMode.beginHuh(m.connection.focusForm())
		}
		return m, nil
	case "connection.field_next":
		if m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.confirmation == nil {
			return m, m.connection.form.NextField()
		}
		return m, nil
	case "connection.field_prev":
		if m.State == stateConnection && m.connection.focus == connectionFocusForm && m.connection.confirmation == nil {
			return m, m.connection.form.PrevField()
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
