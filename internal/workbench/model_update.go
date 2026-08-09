package workbench

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

func isIdentStart(text string) bool {
	if text == "" {
		return false
	}
	r := []rune(text)[0]
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

const browseDebounceDuration = 150 * time.Millisecond

type browseDebounceMsg struct {
	tag   uint64
	delta int
	table string
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	previousStatus := m.Status
	previousCoreRevision := m.StatusRevision()
	previousRevision := m.statusRevision
	updated, cmd := m.updateCore(message)
	model := updated.(Model)
	statusWritten := model.Status != previousStatus ||
		model.StatusRevision() != previousCoreRevision ||
		model.statusRevision != previousRevision
	if statusWritten && model.Status != "" {
		notifyCmd := model.notify(model.Status)
		if cmd == nil {
			return model, notifyCmd
		}
		return model, tea.Batch(cmd, notifyCmd)
	}
	return model, cmd
}

func (m Model) updateCore(message tea.Msg) (tea.Model, tea.Cmd) {
	if window, ok := message.(tea.WindowSizeMsg); ok {
		m.layout(window.Width, window.Height)
		if m.cellViewer != nil {
			m.cellViewer.resize(max(m.width-8, 1), max(m.height-10, 1))
		}
		if m.notificationHistory != nil {
			m.notificationHistory.resize(window.Width, window.Height)
		}
		return m, nil
	}

	// Route streaming and persistence messages early to prevent modal branches
	// from consuming them and stalling the stream.
	if _, ok := message.(chatStreamMsg); ok {
		return m.updateChat(message)
	}
	if _, ok := message.(chatPersistMsg); ok {
		return m.updateChat(message)
	}
	// Route tool-round messages early for the same reason.
	if _, ok := message.(assistantToolStartMsg); ok {
		return m.updateChat(message)
	}
	if _, ok := message.(assistantToolPhaseExpiredMsg); ok {
		return m.updateChat(message)
	}
	if _, ok := message.(assistantToolContinueMsg); ok {
		return m.updateChat(message)
	}
	if _, ok := message.(assistantWriteResultMsg); ok {
		return m.updateChat(message)
	}
	if _, ok := message.(assistantToolResultMsg); ok {
		return m.updateChat(message)
	}
	if dismiss, ok := message.(notificationDismissMsg); ok {
		if dismiss.generation == m.notificationGeneration {
			m.notificationPopup = nil
		}
		return m, nil
	}
	// Consume the release trailing a popup click before the modal branches
	// swallow it; otherwise the flag stays armed and eats a later release.
	if _, ok := message.(tea.MouseReleaseMsg); ok && m.notificationPopupSwallowRelease {
		m.notificationPopupSwallowRelease = false
		return m, nil
	}
	// A click on the popup opens the notification history (or, without a
	// connection scope, a single-entry detail view) and swallows the
	// trailing release so it cannot click the pane underneath.
	if click, ok := message.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		if bounds, hit := m.notificationPopupBounds(); hit && click.X >= bounds.Min.X && click.X < bounds.Max.X && click.Y >= bounds.Min.Y && click.Y < bounds.Max.Y {
			m.notificationPopupSwallowRelease = true
			if m.connectionID != "" && m.notificationPopup.id != 0 {
				m.notificationHistory = newNotificationHistory(m.notificationEntries, m.notificationPopup.id, m.width, m.height)
			} else {
				m.notificationDetail = m.notificationPopup
			}
			return m, nil
		}
	}
	if m.notificationHistory != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok {
			// The filter's first Escape only blurs the filter; a second
			// Escape closes the modal.
			if keyPress.Key().Code == tea.KeyEscape && !m.notificationHistory.filterFocused {
				m.notificationHistory = nil
				return m, nil
			}
			m.notificationHistory.handleKey(keyPress)
		}
		return m, nil
	}
	if m.notificationDetail != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.notificationDetail = nil
			return m, nil
		}
		return m, nil
	}

	if m.themePicker != nil {
		keyPress, ok := message.(tea.KeyPressMsg)
		if !ok {
			return m, nil
		}
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			m.applyTheme(m.themePicker.original)
			m.themePicker = nil
			return m, nil
		case tea.KeyEnter:
			m.commitTheme(m.themePicker.theme())
			m.themePicker = nil
			return m, nil
		case tea.KeyUp:
			m.themePicker.move(-1)
		case tea.KeyDown:
			m.themePicker.move(1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				m.themePicker.move(1)
			case "k":
				m.themePicker.move(-1)
			default:
				return m, nil
			}
		}
		m.applyTheme(m.themePicker.theme())
		return m, nil
	}
	if m.tableTargetPicker != nil {
		keyPress, ok := message.(tea.KeyPressMsg)
		if !ok {
			return m, nil
		}
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			m.tableTargetPicker = nil
			return m, nil
		case tea.KeyEnter:
			m.commitTableOpenTarget(m.tableTargetPicker.tab())
			m.tableTargetPicker = nil
			return m, nil
		case tea.KeyUp:
			m.tableTargetPicker.move(-1)
		case tea.KeyDown:
			m.tableTargetPicker.move(1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				m.tableTargetPicker.move(1)
			case "k":
				m.tableTargetPicker.move(-1)
			default:
				return m, nil
			}
		}
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
		if wheel, ok := message.(tea.MouseWheelMsg); ok {
			m.commandPalette.handleWheel(wheel)
			return m, nil
		}
		if click, ok := message.(tea.MouseClickMsg); ok {
			selectMsg, consumed := m.commandPalette.handleClick(click, m.width, m.height)
			if !consumed {
				return m, nil
			}
			if selectMsg.id != "" {
				return m.handlePaletteCommand(selectMsg.id)
			}
			return m, nil
		}
	}
	if m.cellViewer != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.cellViewer = nil
			return m, nil
		}
		cmd := m.cellViewer.update(message)
		return m, cmd
	}
	if m.contextMenu != nil && m.contextMenu.visible {
		return m.updateContextMenu(message)
	}
	if m.quitDialog != nil {
		completed, action := m.quitDialog.Update(message, m.width, m.height)
		if !completed {
			return m, nil
		}
		m.quitDialog = nil
		switch action {
		case "quit":
			return m, tea.Quit
		case "disconnect":
			m.disconnect()
		}
		return m, nil
	}
	if mouse, ok := message.(tea.MouseMsg); ok {
		if dialog := m.activeConfirmation(); dialog != nil {
			// A release consumed by an open dialog is not the trailing
			// release of a form-button press; it must not count toward the
			// one-shot swallow below.
			m.formButtonHit = false
			completed, _ := dialog.Update(mouse, m.width, m.height)
			if !completed {
				return m, nil
			}
			message = tea.KeyPressMsg{Code: tea.KeyEnter}
		}
	}
	if m.queryConfirmation != nil {
		completed, action := m.queryConfirmation.dialog.Update(message, m.width, m.height)
		if !completed {
			return m, nil
		}
		statement := m.queryConfirmation.statement
		m.queryConfirmation = nil
		if action != "run" {
			return m, nil
		}
		return m.startQueryStatement(statement, false)
	}
	if run := m.chat.activeRun(); run.pendingWrite != nil && run.pendingWrite.dialog != nil {
		// While in chat insert mode, Escape exits insert mode first so the
		// write confirmation cannot interrupt the agent on the first press;
		// a second Escape (now in normal mode) declines the write.
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape && m.chat.chatMode == formModeInsert {
			m.chat.chatMode = formModeNormal
			m.chat.input.Blur()
			return m, nil
		}
		completed, action := run.pendingWrite.dialog.Update(message, m.width, m.height)
		if !completed {
			return m, nil
		}
		call := run.pendingWrite.call
		statement := run.pendingWrite.statement
		gen := run.pendingWrite.generation

		if action != "run" {
			// Decline — send error result and stop round.
			run.pendingWrite = nil
			return m, func() tea.Msg {
				return assistantWriteResultMsg{
					gen: gen, callID: call.ID, callName: call.Name,
					err: "execution canceled by user", declined: true,
				}
			}
		}

		// Approved — execute write asynchronously.
		run.pendingWrite = nil
		rs := run.roundState
		if rs == nil || gen != rs.gen {
			return m, nil
		}
		if time.Now().After(rs.toolDeadline) {
			return m, func() tea.Msg {
				return assistantWriteResultMsg{
					gen: gen, callID: call.ID, callName: call.Name,
					err: "tool time budget ended before write approval",
				}
			}
		}
		chatContext := rs.chatContext
		db := m.Database
		return m, func() tea.Msg {
			res, err := db.Execute(chatContext, statement)
			content := ""
			errStr := ""
			if err != nil {
				errStr = err.Error()
			} else {
				content = formatResult(res)
			}
			return assistantWriteResultMsg{
				gen: gen, callID: call.ID, callName: call.Name,
				content: content, err: errStr,
			}
		}
	}
	if m.cellEditor != nil {
		keyPress, isKeyPress := message.(tea.KeyPressMsg)
		if isKeyPress && keyPress.Key().Code == tea.KeyEscape {
			if m.cellEditor.confirming {
				m.cellEditor.confirming = false
				m.cellEditor.confirm = nil
				m.formMode.mode = formModeNormal
				return m, m.cellEditor.input.Init()
			}
			m.cellEditor = nil
			return m, nil
		}
		if m.cellEditor.confirming {
			completed, action := m.cellEditor.confirm.Update(message, m.width, m.height)
			if !completed {
				return m, nil
			}
			if action != "save" {
				m.cellEditor = nil
				return m, nil
			}
			cmd := m.executeCellUpdate()
			m.cellEditor = nil
			return m, cmd
		}

		// Save/Cancel buttons on the dialog's bottom row. The release that
		// trails the press is swallowed via formButtonHit.
		if mouse, ok := message.(tea.MouseClickMsg); ok && mouse.Button == tea.MouseLeft {
			switch m.cellEditorButtonAt(mouse.X, mouse.Y) {
			case "save":
				m.formButtonHit = true
				return m, m.cellEditor.beginConfirmation()
			case "cancel":
				m.formButtonHit = true
				m.cellEditor = nil
				return m, nil
			}
		}

		// Only Ctrl+S (form.save) submits the cell editor; Enter neither
		// submits nor advances. Without this guard, a single-field form
		// transitions to StateCompleted on Enter.
		if isKeyPress && keyPress.Key().Code == tea.KeyEnter {
			return m, nil
		}
		if isKeyPress && m.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}) {
			return m, m.cellEditor.beginConfirmation()
		}
		model, command := m.cellEditor.input.Update(message)
		m.cellEditor.input = model.(*huh.Form)
		if m.cellEditor.input.State != huh.StateCompleted {
			return m, command
		}
		return m, m.cellEditor.beginConfirmation()
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
		m.editorValidity = sqlValidityPending
		return m, tea.Batch(m.formMode.beginInsert(m.editor), m.scheduleSQLValidation())
	}
	if m.chatHistoryPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.chatHistoryPicker = nil
			return m, nil
		}
		form, command := m.chatHistoryPicker.Update(message)
		m.chatHistoryPicker = form.(*huh.Form)
		if m.chatHistoryPicker.State != huh.StateCompleted {
			return m, command
		}
		conversationID := m.chat.historyChoice
		m.chatHistoryPicker = nil
		return m, m.loadChatMessages(conversationID)
	}
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.layout(message.Width, message.Height)
		return m, nil
	case browseDebounceMsg:
		if message.tag != m.browsePageTag || message.table != m.SelectedTable || m.browseLoading {
			return m, nil
		}
		if message.delta > 0 && !m.browseResult.HasMore {
			return m, nil
		}
		if !m.ChangeBrowsePage(message.delta) {
			return m, nil
		}
		m.browseLoading = true
		return m, m.loadBrowse()
	case tea.KeyPressMsg:
		if m.tableFiltering {
			return m, m.updateTableFilter(message)
		}
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
		if quit && !m.formActive() && !m.schemaFilter.Focused() &&
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
			return m.openQuitDialog(), nil
		}

		// Route chat Escape before global keybindings so chat's
		// insert→normal→cancel precedence wins over query.cancel etc.
		if m.Focus == focusChat && message.Key().Code == tea.KeyEscape {
			return m.updateChat(message)
		}

		if m.State == stateReady && !m.formActive() && !m.schemaFilter.Focused() && !(m.Focus == focusWorkspace && m.Tab == tabSQL && m.formMode.editing()) && !(m.Focus == focusChat && m.chat.chatMode == formModeInsert) {
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
			case m.keybindings.Match(message, "focus.chat", []scope{scopeGlobal}):
				if !m.chat.visible {
					return m, nil
				}
				m.Focus = focusChat
				m.queryLogPendingG = false
				m.editor.text.Blur()
				m.blurTables()
				if !m.vimMode {
					m.chat.chatMode = formModeInsert
					return m, m.chat.input.Focus()
				}
				m.chat.chatMode = formModeNormal
				return m, nil
			case m.keybindings.Match(message, "ai.toggle", []scope{scopeGlobal}):
				m.toggleAI()
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
		if m.State == stateReady && !m.formActive() && m.keybindings.Match(message, "query.history", []scope{scopeGlobal}) && m.recallQueryHistory(1) {
			m.Focus, m.Tab = focusWorkspace, tabSQL
			m.blurTables()
			return m, m.formMode.beginInsert(m.editor)
		}
		if m.sqlEditorActive() && !m.tableFormOpen() {
			if m.editor.completionVisible() {
				key := message.Key()
				completionHandled := true
				switch {
				case key.Code == tea.KeyEscape:
					m.editor.completion = completion{}
					completionHandled = false // let escape also exit insert mode
				case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == tea.ModCtrl):
					m.editor.completion.move(-1)
				case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == tea.ModCtrl):
					m.editor.completion.move(1)
				case key.Code == tea.KeyEnter || key.Code == tea.KeyTab:
					m.editor.acceptCompletion()
				default:
					completionHandled = false
				}
				if completionHandled {
					return m, nil
				}
			}
			if m.formMode.editing() && m.keybindings.Match(message, "editor.complete", []scope{scopeForm, scopeView, scopeGlobal}) {
				return m, m.startCompletion()
			}
			if m.formMode.editing() {
				key := message.Key()
				if (key.Code == tea.KeyUp && (m.editor.value == "" || m.historyIndex >= 0)) ||
					(key.Code == tea.KeyDown && m.historyIndex >= 0) {
					direction := 1
					if key.Code == tea.KeyDown {
						direction = -1
					}
					if m.recallQueryHistory(direction) {
						// setValue replaces the textarea, dropping focus; re-focus so
						// subsequent typing in insert mode still lands.
						m.editorValidity = sqlValidityPending
						return m, tea.Batch(m.editor.Focus(), m.scheduleSQLValidation())
					}
				}
			}
			switch m.formMode.route(message, m.editor) {
			case formRouteConsumed:
				return m, nil
			case formRouteHuh:
				previous := m.editor.value
				command := m.editor.update(message)
				if m.editor.value != previous {
					m.historyIndex = -1
					m.editorValidity = sqlValidityPending
					command = tea.Batch(command, m.scheduleSQLValidation())
				}
				if message.Text == "." {
					m.editor.completion = completion{}
					return m, tea.Batch(command, m.startCompletion())
				}
				if m.editor.completionVisible() {
					m.editor.completion.filter(sharedsql.CompletionPrefix(m.editor.value))
				} else if isIdentStart(message.Text) {
					m.editor.completion = completion{}
					return m, tea.Batch(command, m.startCompletion())
				}
				return m, command
			case formRouteParent:
				if isInsertModeKey(message) || m.keybindings.Match(message, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}) {
					return m, m.formMode.beginInsert(m.editor)
				}
			}
		}
		if m.State == stateReady && !m.formActive() && m.Focus == focusWorkspace {
			switch {
			case m.keybindings.Match(message, "workspace.tab_next", []scope{scopeView}):
				return m, m.toggleTab(true)
			case m.keybindings.Match(message, "workspace.tab_prev", []scope{scopeView}):
				return m, m.toggleTab(false)
			}
		}
	}
	switch message := message.(type) {
	case tea.MouseClickMsg:
		if m.tableFiltering {
			m.closeTableFilter()
		}
		cmds := []tea.Cmd{}
		if m.contextMenu == nil && !m.hasOverlay() && message.Button == tea.MouseLeft {
			model, cmd := m.handleLeftClick(message.X, message.Y)
			m = model.(Model)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			var m2 tea.Model
			m2, maybeCmd := m.handleBrowseClick(message.X, message.Y)
			m = m2.(Model)
			if maybeCmd != nil {
				cmds = append(cmds, maybeCmd)
			}
			var m3 tea.Model
			m3, maybeCmd = m.handleSchemaTableClick(message.X, message.Y)
			m = m3.(Model)
			if maybeCmd != nil {
				cmds = append(cmds, maybeCmd)
			}
			var m4 tea.Model
			m4, maybeCmd = m.handleFormClick(message.X, message.Y)
			m = m4.(Model)
			if maybeCmd != nil {
				cmds = append(cmds, maybeCmd)
			}
			if len(cmds) > 0 || m.contextMenu != nil {
				return m, tea.Batch(cmds...)
			}
		}
		if message.Button == tea.MouseRight && !m.hasOverlay() {
			return m.handleRightClick(message.X, message.Y)
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
	case tea.MouseWheelMsg:
		if !m.hasOverlay() && m.contextMenu == nil {
			if (m.formActive() || m.State == stateConnection) && !m.tableFiltering {
				return m.scrollForm(message)
			}
			return m.handleMouseWheel(message)
		}
	case tea.MouseReleaseMsg:
		if m.notificationPopupSwallowRelease {
			m.notificationPopupSwallowRelease = false
			return m, nil
		}
		if m.commandPalette != nil && m.commandPalette.swallowRelease {
			m.commandPalette.swallowRelease = false
			return m, nil
		}
		if m.formButtonHit {
			// The release trailing a form-button press must not also act on
			// the pane underneath (field focus, table selection). One-shot:
			// a later real release clicks normally.
			m.formButtonHit = false
			return m, nil
		}
		if m.contextMenu == nil && !m.hasOverlay() && message.Button == tea.MouseLeft {
			return m.handleLeftClick(message.X, message.Y)
		}
	}

	if m.deleteConfirm != nil {
		completed, action := m.deleteConfirm.Update(message, m.width, m.height)
		if !completed {
			return m, nil
		}
		m.deleteConfirm = nil
		pending := m.deletePending
		m.deletePending = ""
		if action != "delete" && action != "delete_table" {
			m.deletePendingName = ""
			m.deletePendingDatabase = ""
			return m, nil
		}
		switch pending {
		case "column":
			cmd := m.deleteColumn()
			m.deletePendingName = ""
			return m, cmd
		case "index":
			cmd := m.deleteIndex()
			m.deletePendingName = ""
			return m, cmd
		case "foreign_key":
			cmd := m.deleteForeignKey()
			m.deletePendingName = ""
			return m, cmd
		case "table":
			database, table := m.deletePendingDatabase, m.deletePendingName
			m.deletePendingDatabase, m.deletePendingName = "", ""
			statement := "DROP TABLE " + m.actionIdentifier(m.qualifiedTableName(database, table))
			return m.startQueryStatement(statement, true)
		case "schema":
			schema := m.deletePendingName
			m.deletePendingName, m.deletePendingDatabase = "", ""
			return m.startQueryStatement("DROP SCHEMA "+m.quoteIdentifier(schema)+" RESTRICT", true)
		case "database":
			database := m.deletePendingDatabase
			m.deletePendingDatabase, m.deletePendingName = "", ""
			return m.startQueryStatement("DROP DATABASE "+m.quoteIdentifier(database), true)
		default:
			m.deletePendingName = ""
			return m, m.deleteRow()
		}
	}

	switch message := message.(type) {
	case databaseOpenedMsg:
		return m.updateOpen(message)
	case directoryReadMsg:
		m.pickerDir = message.dir
		if message.err != nil {
			log.Error("reading directory", message.err)
			m.setStatus(safeText(fmt.Sprintf("unable to read directory: %v", message.err)))
			return m, nil
		}
		m.setStatus("choose a database")
		items := make([]list.Item, len(message.items))
		for index, item := range message.items {
			items[index] = item
		}
		return m, m.picker.SetItems(items)
	case pickerSelectionMsg:
		if message.err != nil {
			log.Error("opening selection", message.err)
			m.setStatus(safeText(fmt.Sprintf("unable to open selection: %v", message.err)))
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
	case schemaLoadedMsg:
		return m.updateSchemaLoaded(message)
	case queryCanceledMsg:
		return m.updateQueryCanceled(message)
	case sqlValidationTickMsg:
		return m.updateSQLValidationTick(message)
	case sqlValidationMsg:
		return m.updateSQLValidation(message)
	case tableInfoMsg:
		return m.updateTableInfo(message)
	case completionColumnsMsg:
		return m.updateCompletionColumns(message)
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
	case columnDeletedMsg:
		return m.updateColumnDeleted(message)
	case browseRowUpdatedMsg:
		return m.updateBrowseRowUpdated(message)
	case deleteRowMsg:
		return m.updateDeleteRowMsg(message)
	case cellEditorUpdatedMsg:
		return m.updateCellEditorUpdated(message)
	case sqlEditorFinishedMsg:
		return m.updateExternalEditor(message)
	case chatResponseMsg, chatStreamMsg, chatPersistMsg, chatHistoryLoadedMsg, chatMessagesLoadedMsg, chatHistoryDeletedMsg,
		assistantToolStartMsg, assistantToolContinueMsg, assistantWriteResultMsg, chatSpinnerTickMsg:
		return m.updateChat(message)
	}

	// Route the table popup before ordinary active dispatch; like the other
	// forms it receives every unconsumed message (huh init, mouse, keys).
	// On execute the form is retained (but hidden) until the query resolves:
	// a rejected DDL restores it, a success closes it and refreshes the
	// sidebar.
	if m.tableFormOpen() {
		command, action := m.tableForm.Update(message, m.formMode)
		switch action {
		case tableFormClose:
			m.tableForm = tableForm{}
		case tableFormSave:
			m.tableForm.confirmation.description = m.tableForm.statement(m)
		case tableFormExecute:
			statement := m.tableForm.statement(m)
			m.tableFormRunning = true
			return m.startQueryStatement(statement, true)
		}
		return m, command
	}
	if m.sqlEditorActive() && m.formMode.editing() {
		previous := m.editor.value
		command := m.editor.update(message)
		if m.editor.value != previous {
			m.editorValidity = sqlValidityPending
			command = tea.Batch(command, m.scheduleSQLValidation())
		}
		return m, command
	}

	return m.updateActive(message)
}

func (m Model) executeQuery() (tea.Model, tea.Cmd) {
	if requiresQueryConfirmation(m.editor.value) {
		m.queryConfirmation = newQueryConfirmation(m.editor.value)
		return m, nil
	}
	return m.startQuery()
}

func (m Model) sqlEditorActive() bool {
	return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL
}
