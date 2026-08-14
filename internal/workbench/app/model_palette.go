package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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
		if m.State == stateReady && m.chat.component.Visible {
			m.Focus = focusChat
			m.queryLog.component.PendingG = false
			m.queryLog.editor.text.Blur()
			m.blurTables()
			if !m.vimMode {
				return m, m.chat.component.EnterInsertMode()
			}
			m.chat.component.EnterNormalMode()
			return m, nil
		}
		return m, nil
	case "ai.toggle":
		m.toggleAI()
		return m, nil
	case "chat.delete":
		if m.State == stateReady && m.Focus == focusChat {
			return m, m.chat.component.DeleteHistory(m.chatLayout(), false)
		}
		return m, nil
	case "chat.clear":
		if m.State == stateReady && m.Focus == focusChat {
			return m, m.chat.component.DeleteHistory(m.chatLayout(), true)
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
			component, event, cmd := m.schema.component.SchemaSelect(m.schemaSnapshot())
			m.schema.component = component
			return m.applySchemaEvent(event, cmd)
		}
		return m, nil
	case "schema.expand":
		if m.State == stateReady && m.Focus == focusSchema {
			component, cmd := m.schema.component.SchemaExpand(m.schemaSnapshot())
			m.schema.component = component
			return m, cmd
		}
		return m, nil
	case "schema.collapse":
		if m.State == stateReady && m.Focus == focusSchema {
			component, cmd := m.schema.component.SchemaCollapse(m.schemaSnapshot())
			m.schema.component = component
			return m, cmd
		}
		return m, nil
	case "schema.add_table":
		if m.State == stateReady && m.Focus == focusSchema {
			component, event, cmd := m.schema.component.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}, m.schemaLayout(), m.keybindings, m.schemaSnapshot())
			m.schema.component = component
			return m.applySchemaEvent(event, cmd)
		}
		return m, nil
	case "schema.rename_table":
		if m.State == stateReady && m.Focus == focusSchema {
			component, event, cmd := m.schema.component.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}, m.schemaLayout(), m.keybindings, m.schemaSnapshot())
			m.schema.component = component
			return m.applySchemaEvent(event, cmd)
		}
		return m, nil
	case "schema.delete_table":
		if m.State == stateReady && m.Focus == focusSchema {
			component, event, cmd := m.schema.component.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}, m.schemaLayout(), m.keybindings, m.schemaSnapshot())
			m.schema.component = component
			return m.applySchemaEvent(event, cmd)
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
	case "browse.edit":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.browseWriteAvailable() {
			if m.writeCapabilities().RowWriter {
				return m, m.openBrowseForm()
			}
			return m, m.openEditDocument()
		}
		return m, nil
	case "browse.insert_row":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.browseWriteAvailable() {
			return m, m.openInsertRowForm()
		}
		return m, nil
	case "browse.delete_row":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.ObjectListMode() && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil && m.browseWriteAvailable() {
			m.confirmBrowseRowDelete()
			return m, nil
		}
		return m, nil
	case "browse.add_table":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && m.browse.component.ObjectListMode() {
			return m, m.browseObjectAction("add_table")
		}
		return m, nil
	case "browse.rename_table":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && m.browse.component.ObjectListMode() {
			return m, m.browseObjectAction("rename_table")
		}
		return m, nil
	case "browse.delete_table":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && m.browse.component.ObjectListMode() {
			return m, m.browseObjectAction("delete_table")
		}
		return m, nil
	case "browse.refine":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			return m, m.openBrowseFilterForm()
		}
		return m, nil
	case "browse.reset":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			return m, m.resetBrowseFilters()
		}
		return m, nil
	case "browse.sort":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			return m, m.cycleBrowseSort()
		}
		return m, nil
	case "cell.view":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			row := m.browse.component.Table.Cursor()
			col := m.browse.component.SelectedColumn
			display := ""
			if row >= 0 && row < len(m.browse.component.Table.Rows()) && col >= 0 && col < len(m.browse.component.Table.Rows()[row]) {
				display = m.browse.component.Table.Rows()[row][col]
			}
			raw := m.rawCellValue("browse", row, col, display)
			return m, m.openCellViewer(m.browse.component.Table, col, raw)
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && !m.overlay.formMode.Editing() && m.queryLog.results.Focused() {
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
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			return m, m.copyBrowseCell()
		}
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL && !m.overlay.formMode.Editing() && m.queryLog.results.Focused() {
			return m, m.copySQLCell()
		}
		return m, nil
	case "browse.next_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			if m.browse.component.Loading {
				return m, nil
			}
			m.browse.component.PageTag++
			tag := m.browse.component.PageTag
			table := m.SelectedTable
			return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
				return browseDebounceMsg{tag: tag, delta: 1, table: table}
			})
		}
		return m, nil
	case "browse.prev_page":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabBrowse && !m.browse.component.Form.Active() && m.browse.component.FilterForm == nil {
			if m.browse.component.Loading {
				return m, nil
			}
			m.browse.component.PageTag++
			tag := m.browse.component.PageTag
			table := m.SelectedTable
			return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
				return browseDebounceMsg{tag: tag, delta: -1, table: table}
			})
		}
		return m, nil
	case "foreign_keys.toggle_diagram":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys {
			m.schema.component.Structure.RelationshipDiagram = !m.schema.component.Structure.RelationshipDiagram
			if m.schema.component.Structure.RelationshipDiagram {
				m.schema.component.Structure.IndexDiagram = false
			}
		}
		return m, nil
	case "indexes.toggle_diagram":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabIndexes && !m.schema.component.Structure.IndexForm.Active() {
			m.schema.component.Structure.IndexDiagram = !m.schema.component.Structure.IndexDiagram
			if m.schema.component.Structure.IndexDiagram {
				m.schema.component.Structure.RelationshipDiagram = false
			}
		}
		return m, nil
	case "diagram.depth_up", "diagram.depth_down":
		if m.State == stateReady && m.Focus == focusWorkspace {
			diagram := (m.Tab == tabForeignKeys && m.schema.component.Structure.RelationshipDiagram) || (m.Tab == tabIndexes && m.schema.component.Structure.IndexDiagram)
			if diagram {
				if id == "diagram.depth_up" {
					m.schema.component.Structure.DiagramDepth = min(m.schema.component.Structure.DiagramDepth+1, schema.MaxDiagramDepth)
				} else {
					m.schema.component.Structure.DiagramDepth = max(m.schema.component.Structure.DiagramDepth-1, 1)
				}
			}
		}
		return m, nil
	case "indexes.create":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabIndexes && !m.schema.component.Structure.IndexForm.Active() {
			return m, m.openIndexForm(nil)
		}
		return m, nil
	case "foreign_keys.create":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabForeignKeys && !m.schema.component.Structure.ForeignKeyForm.Active() {
			return m, m.openForeignKeyForm(nil)
		}
		return m, nil
	case "structure.add":
		if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabStructure && !m.schema.component.Structure.ColumnForm.Active() {
			return m, m.openNewColumnForm()
		}
		return m, nil
	case "connection.switch_to_form":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusRecent {
			m.connection.component.Form.Focus = connectionFocusForm
		}
		return m, nil
	case "connection.switch_to_list":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm {
			m.connection.component.Form.SetFocus(connectionFocusRecent)
		}
		return m, nil
	case "connection.execute":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil {
			if err := m.connection.component.Form.Validate(); err != nil {
				m.setStatus(safeText(err.Error()))
				return m, m.connection.component.Form.ShowValidationError()
			}
			m.overlay.formMode.BeginConfirm()
			m.connection.component.Form.BeginConfirmation()
			return m, nil
		}
		return m, nil
	case "connection.action_enter":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil && m.connectionActionFocused() {
			m.overlay.formMode.Mode = formModeNormal
			m.connection.component.Form.Blur()
			if m.connection.component.Form.Values.Action == connectionActionTest {
				return m, m.testConnection()
			}
			return m.openConnection()
		}
		return m, nil
	case "connection.edit_field":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil && !m.connectionActionFocused() {
			return m, m.overlay.formMode.BeginHuh(m.connection.component.Form.FocusForm())
		}
		return m, nil
	case "connection.field_next":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil {
			return m, m.connection.component.Form.Huh.NextField()
		}
		return m, nil
	case "connection.field_prev":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusForm && m.connection.component.Form.Confirmation == nil {
			return m, m.connection.component.Form.Huh.PrevField()
		}
		return m, nil
	case "connection.add":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusRecent {
			return m, m.newConnection()
		}
		return m, nil
	case "connection.edit":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusRecent {
			return m, m.editSelectedRecentConnection()
		}
		return m, nil
	case "connection.delete":
		if m.State == stateConnection && m.connection.component.Form.Focus == connectionFocusRecent {
			m.confirmDeleteRecentConnection()
			return m, nil
		}
		return m, nil
	default:
		return m, nil
	}
}
