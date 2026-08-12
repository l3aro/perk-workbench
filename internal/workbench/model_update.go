package workbench

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
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
	previousRevision := m.notifications.statusRevision
	updated, cmd := m.updateCore(message)
	model := updated.(Model)
	var cmds []tea.Cmd
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	statusWritten := model.Status != previousStatus ||
		model.StatusRevision() != previousCoreRevision ||
		model.notifications.statusRevision != previousRevision
	if statusWritten && model.Status != "" && !model.notifications.skipStatusPopup {
		cmds = append(cmds, model.notify(model.Status))
	}
	model.notifications.skipStatusPopup = false
	// Logged events surface as popups after the status one so the level
	// title, icon, and color win the visible slot. A transition that logs
	// before its profile scope exists marks its entries transient so they
	// never bind history to the wrong connection.
	transient := model.notifications.skipNotificationPersist
	model.notifications.skipNotificationPersist = false
	for _, entry := range notification.DrainLogEntries() {
		if transient {
			cmds = append(cmds, model.notifyLogTransient(entry))
		} else {
			cmds = append(cmds, model.notifyLog(entry))
		}
	}
	if len(cmds) == 0 {
		return model, nil
	}
	if len(cmds) == 1 {
		return model, cmds[0]
	}
	return model, tea.Batch(cmds...)
}

func (m Model) updateCore(message tea.Msg) (tea.Model, tea.Cmd) {
	if window, ok := message.(tea.WindowSizeMsg); ok {
		m.applyLayout(window.Width, window.Height)
		if m.browse.cellViewer != nil {
			m.browse.cellViewer.Resize(max(m.layout.width-8, 1), max(m.layout.height-10, 1))
		}
		m.notifications.component.ResizeHistory(window.Width, window.Height)
		return m, nil
	}

	// A log entry arrived from outside an update handler (an async
	// command): the outer Update wrapper drains the queue into a popup.
	if _, ok := message.(notification.LogWakeupMsg); ok {
		return m, nil
	}

	// Route document-editor completions before the modal branch, which
	// otherwise swallows them while the editor is loading/saving.
	if loaded, ok := message.(documentEditorLoadedMsg); ok {
		return m.updateDocumentEditorLoaded(loaded)
	}
	if saved, ok := message.(documentEditorSavedMsg); ok {
		return m.updateDocumentEditorSaved(saved)
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
	// The notification component owns its messages: the popup dismiss
	// timer, the popup click and its trailing release, and the open
	// history/detail overlays (which swallow every input while visible).
	// It consumes only what it owns; everything else passes through to the
	// modal and pane routing below.
	if m.notifications.component.Consumes(message, notificationLayout(m)) {
		model, event, cmd := m.notifications.component.Update(message, notificationLayout(m), m.keybindings)
		m.notifications.component = model
		return m.applyNotificationEvent(event, cmd)
	}

	if m.overlay.themePicker != nil {
		keyPress, ok := message.(tea.KeyPressMsg)
		if !ok {
			return m, nil
		}
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			m.applyTheme(m.overlay.themePicker.original)
			m.overlay.themePicker = nil
			return m, nil
		case tea.KeyEnter:
			m.commitTheme(m.overlay.themePicker.theme())
			m.overlay.themePicker = nil
			return m, nil
		case tea.KeyUp:
			m.overlay.themePicker.move(-1)
		case tea.KeyDown:
			m.overlay.themePicker.move(1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				m.overlay.themePicker.move(1)
			case "k":
				m.overlay.themePicker.move(-1)
			default:
				return m, nil
			}
		}
		m.applyTheme(m.overlay.themePicker.theme())
		return m, nil
	}
	if m.overlay.tableTargetPicker != nil {
		keyPress, ok := message.(tea.KeyPressMsg)
		if !ok {
			return m, nil
		}
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			m.overlay.tableTargetPicker = nil
			return m, nil
		case tea.KeyEnter:
			m.commitTableOpenTarget(m.overlay.tableTargetPicker.tab())
			m.overlay.tableTargetPicker = nil
			return m, nil
		case tea.KeyUp:
			m.overlay.tableTargetPicker.move(-1)
		case tea.KeyDown:
			m.overlay.tableTargetPicker.move(1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				m.overlay.tableTargetPicker.move(1)
			case "k":
				m.overlay.tableTargetPicker.move(-1)
			default:
				return m, nil
			}
		}
		return m, nil
	}
	if m.overlay.commandPalette.visible {
		if keyPress, ok := message.(tea.KeyPressMsg); ok {
			selectMsg, close, consumed := m.overlay.commandPalette.handleKey(keyPress)
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
			m.overlay.commandPalette.handleWheel(wheel)
			return m, nil
		}
		if click, ok := message.(tea.MouseClickMsg); ok {
			selectMsg, consumed := m.overlay.commandPalette.handleClick(click, m.layout.width, m.layout.height)
			if !consumed {
				return m, nil
			}
			if selectMsg.id != "" {
				return m.handlePaletteCommand(selectMsg.id)
			}
			return m, nil
		}
	}
	if m.browse.cellViewer != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.browse.cellViewer = nil
			return m, nil
		}
		cmd := m.browse.cellViewer.Update(message)
		return m, cmd
	}
	if m.overlay.contextMenu != nil && m.overlay.contextMenu.visible {
		return m.updateContextMenu(message)
	}
	if m.overlay.quitDialog != nil {
		completed, action := m.overlay.quitDialog.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		m.overlay.quitDialog = nil
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
			m.layout.formButtonHit = false
			completed, _ := dialog.Update(mouse, m.layout.width, m.layout.height)
			if !completed {
				return m, nil
			}
			message = tea.KeyPressMsg{Code: tea.KeyEnter}
		}
	}
	if m.overlay.queryConfirmation != nil {
		completed, action := m.overlay.queryConfirmation.dialog.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		statement := m.overlay.queryConfirmation.statement
		m.overlay.queryConfirmation = nil
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
		completed, action := run.pendingWrite.dialog.Update(message, m.layout.width, m.layout.height)
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
	if m.browse.documentEditor != nil {
		editor := m.browse.documentEditor
		if editor.saving || editor.loading {
			// The payload arrives via documentEditorLoadedMsg, routed
			// before this branch; ignore user input meanwhile.
			return m, nil
		}
		keyPress, isKeyPress := message.(tea.KeyPressMsg)
		if isKeyPress && keyPress.Key().Code == tea.KeyEscape {
			if editor.confirming {
				editor.confirming = false
				editor.confirmation = nil
				m.overlay.formMode.mode = formModeNormal
				return m, editor.form.Init()
			}
			m.browse.documentEditor = nil
			return m, nil
		}
		if editor.confirming {
			completed, action := editor.confirmation.Update(message, m.layout.width, m.layout.height)
			if !completed {
				return m, nil
			}
			if action != "confirm" {
				m.browse.documentEditor = nil
				return m, nil
			}
			editor.confirming = false
			editor.saving = true
			cmd := m.executeDocumentSave()
			return m, cmd
		}
		if isKeyPress && keyPress.Key().Code == tea.KeyEnter {
			return m, nil
		}
		if isKeyPress && m.keybindings.Match(keyPress, "form.save", []scope{scopeForm, scopeView, scopeGlobal}) {
			cmd, err := editor.beginConfirmation()
			if err != nil {
				m.setStatus(safeText(err.Error()))
				return m, nil
			}
			return m, cmd
		}
		model, command := editor.form.Update(message)
		editor.form = model.(*huh.Form)
		if editor.form.State != huh.StateCompleted {
			return m, command
		}
		cmd, err := editor.beginConfirmation()
		if err != nil {
			m.setStatus(safeText(err.Error()))
			return m, nil
		}
		return m, cmd
	}
	if m.browse.cellEditor != nil {
		keyPress, isKeyPress := message.(tea.KeyPressMsg)
		if isKeyPress && keyPress.Key().Code == tea.KeyEscape {
			if m.browse.cellEditor.confirming {
				m.browse.cellEditor.confirming = false
				m.browse.cellEditor.confirm = nil
				m.overlay.formMode.mode = formModeNormal
				return m, m.browse.cellEditor.input.Init()
			}
			m.browse.cellEditor = nil
			return m, nil
		}
		if m.browse.cellEditor.confirming {
			completed, action := m.browse.cellEditor.confirm.Update(message, m.layout.width, m.layout.height)
			if !completed {
				return m, nil
			}
			if action != "save" {
				m.browse.cellEditor = nil
				return m, nil
			}
			cmd := m.executeCellUpdate()
			m.browse.cellEditor = nil
			return m, cmd
		}

		// Save/Cancel buttons on the dialog's bottom row. The release that
		// trails the press is swallowed via formButtonHit.
		if mouse, ok := message.(tea.MouseClickMsg); ok && mouse.Button == tea.MouseLeft {
			switch m.cellEditorButtonAt(mouse.X, mouse.Y) {
			case "save":
				m.layout.formButtonHit = true
				return m, m.browse.cellEditor.beginConfirmation()
			case "cancel":
				m.layout.formButtonHit = true
				m.browse.cellEditor = nil
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
			return m, m.browse.cellEditor.beginConfirmation()
		}
		model, command := m.browse.cellEditor.input.Update(message)
		m.browse.cellEditor.input = model.(*huh.Form)
		if m.browse.cellEditor.input.State != huh.StateCompleted {
			return m, command
		}
		return m, m.browse.cellEditor.beginConfirmation()
	}
	if m.overlay.explainPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.overlay.explainPicker = nil
			return m, nil
		}
		command := m.overlay.explainPicker.Update(message)
		if !m.overlay.explainPicker.completed() {
			return m, command
		}
		m.queryLog.editor.setValue(m.overlay.explainPicker.query())
		m.overlay.explainPicker = nil
		m.Focus, m.Tab = focusWorkspace, tabSQL
		m.blurTables()
		m.queryLog.editorValidity = sqlValidityPending
		return m, tea.Batch(m.overlay.formMode.beginInsert(m.queryLog.editor), m.scheduleSQLValidation())
	}
	if m.chat.historyPicker != nil {
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
			m.chat.historyPicker = nil
			return m, nil
		}
		form, command := m.chat.historyPicker.Update(message)
		m.chat.historyPicker = form.(*huh.Form)
		if m.chat.historyPicker.State != huh.StateCompleted {
			return m, command
		}
		conversationID := m.chat.historyChoice
		m.chat.historyPicker = nil
		return m, m.loadChatMessages(conversationID)
	}
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.applyLayout(message.Width, message.Height)
		return m, nil
	case browseDebounceMsg:
		if message.tag != m.browse.pageTag || message.table != m.SelectedTable || m.browse.loading {
			return m, nil
		}
		if message.delta > 0 && !m.browse.result.HasMore {
			return m, nil
		}
		if !m.ChangeBrowsePage(message.delta) {
			return m, nil
		}
		m.browse.loading = true
		return m, m.loadBrowse()
	case tea.KeyPressMsg:
		if m.structure.tableFiltering {
			return m, m.updateTableFilter(message)
		}
		if m.keybindings.Match(message, "editor.external", []scope{scopeGlobal}) {
			if command, handled := m.openExternalEditor(); handled {
				return m, command
			}
		}
		if m.keybindings.Match(message, "app.palette", []scope{scopeGlobal}) && !m.hasOverlay() {
			m.overlay.commandPalette = newCommandPalette(m)
			m.overlay.commandPalette.visible = true
			return m, nil
		}
		quit := m.keybindings.Match(message, "app.quit", []scope{scopeGlobal})
		if quit && !m.formActive() && !m.schema.filter.Focused() &&
			!(m.State == stateConnection && (m.connection.component.RecentFilter.Focused() || (m.connection.component.Form.Focus == connectionFocusForm && m.overlay.formMode.editing()))) &&
			!(m.sqlEditorActive() && m.overlay.formMode.editing()) &&
			(m.Running() || m.State != stateReady || m.Focus != focusWorkspace || m.Tab != tabSQL || m.queryLog.editor.value == "") {
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

		if m.State == stateReady && !m.formActive() && !m.schema.filter.Focused() && !(m.Focus == focusWorkspace && m.Tab == tabSQL && m.overlay.formMode.editing()) && !(m.Focus == focusChat && m.chat.chatMode == formModeInsert) {
			switch {
			case m.keybindings.Match(message, "focus.schema", []scope{scopeGlobal}):
				m.Focus = focusSchema
				m.queryLog.component.ClearPendingG()
				m.queryLog.editor.text.Blur()
				m.blurTables()
				return m, nil
			case m.keybindings.Match(message, "focus.workspace", []scope{scopeGlobal}):
				m.Focus = focusWorkspace
				m.queryLog.component.ClearPendingG()
				m.focusActiveTable()
				return m, nil
			case m.keybindings.Match(message, "focus.query_log", []scope{scopeGlobal}):
				m.Focus = focusQueryLog
				m.queryLog.component.ClearPendingG()
				m.queryLog.editor.text.Blur()
				m.blurTables()
				m.queryLog.component.Table.Focus()
				if len(m.queryLog.component.Table.Rows()) > 0 && m.queryLog.component.Table.Cursor() < 0 {
					m.queryLog.component.Table.SetCursor(0)
				}
				return m, nil
			case m.keybindings.Match(message, "focus.chat", []scope{scopeGlobal}):
				if !m.chat.visible {
					return m, nil
				}
				m.Focus = focusChat
				m.queryLog.component.ClearPendingG()
				m.queryLog.editor.text.Blur()
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
				m.layout.fullscreen = !m.layout.fullscreen
				m.applyLayout(m.layout.width, m.layout.height)
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
			return m, m.overlay.formMode.beginInsert(m.queryLog.editor)
		}
		if m.sqlEditorActive() && !m.tableFormOpen() {
			if m.queryLog.editor.completionVisible() {
				key := message.Key()
				completionHandled := true
				switch {
				case key.Code == tea.KeyEscape:
					m.queryLog.editor.completion = completion{}
					completionHandled = false // let escape also exit insert mode
				case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == tea.ModCtrl):
					m.queryLog.editor.completion.move(-1)
				case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == tea.ModCtrl):
					m.queryLog.editor.completion.move(1)
				case key.Code == tea.KeyEnter || key.Code == tea.KeyTab:
					m.queryLog.editor.acceptCompletion()
				default:
					completionHandled = false
				}
				if completionHandled {
					return m, nil
				}
			}
			if m.overlay.formMode.editing() && m.keybindings.Match(message, "editor.complete", []scope{scopeForm, scopeView, scopeGlobal}) {
				return m, m.startCompletion()
			}
			if m.overlay.formMode.editing() {
				key := message.Key()
				if (key.Code == tea.KeyUp && (m.queryLog.editor.value == "" || m.queryLog.historyIndex >= 0)) ||
					(key.Code == tea.KeyDown && m.queryLog.historyIndex >= 0) {
					direction := 1
					if key.Code == tea.KeyDown {
						direction = -1
					}
					if m.recallQueryHistory(direction) {
						// setValue replaces the textarea, dropping focus; re-focus so
						// subsequent typing in insert mode still lands.
						m.queryLog.editorValidity = sqlValidityPending
						return m, tea.Batch(m.queryLog.editor.Focus(), m.scheduleSQLValidation())
					}
				}
			}
			switch m.overlay.formMode.route(message, m.queryLog.editor) {
			case formRouteConsumed:
				return m, nil
			case formRouteHuh:
				previous := m.queryLog.editor.value
				command := m.queryLog.editor.update(message)
				if m.queryLog.editor.value != previous {
					m.queryLog.historyIndex = -1
					m.queryLog.editorValidity = sqlValidityPending
					command = tea.Batch(command, m.scheduleSQLValidation())
				}
				if message.Text == "." {
					m.queryLog.editor.completion = completion{}
					return m, tea.Batch(command, m.startCompletion())
				}
				if m.queryLog.editor.completionVisible() {
					m.queryLog.editor.completion.filter(sharedsql.CompletionPrefix(m.queryLog.editor.value))
				} else if isIdentStart(message.Text) {
					m.queryLog.editor.completion = completion{}
					return m, tea.Batch(command, m.startCompletion())
				}
				return m, command
			case formRouteParent:
				if isInsertModeKey(message) || m.keybindings.Match(message, "form.edit", []scope{scopeForm, scopeView, scopeGlobal}) {
					return m, m.overlay.formMode.beginInsert(m.queryLog.editor)
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
		if m.structure.tableFiltering {
			m.closeTableFilter()
		}
		cmds := []tea.Cmd{}
		if m.overlay.contextMenu == nil && !m.hasOverlay() && message.Button == tea.MouseLeft {
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
			if len(cmds) > 0 || m.overlay.contextMenu != nil {
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
		if !m.hasOverlay() && m.overlay.contextMenu == nil {
			if (m.formActive() || m.State == stateConnection) && !m.structure.tableFiltering {
				return m.scrollForm(message)
			}
			return m.handleMouseWheel(message)
		}
	case tea.MouseReleaseMsg:
		if m.overlay.commandPalette != nil && m.overlay.commandPalette.swallowRelease {
			m.overlay.commandPalette.swallowRelease = false
			return m, nil
		}
		if m.layout.formButtonHit {
			// The release trailing a form-button press must not also act on
			// the pane underneath (field focus, table selection). One-shot:
			// a later real release clicks normally.
			m.layout.formButtonHit = false
			return m, nil
		}
		if m.overlay.contextMenu == nil && !m.hasOverlay() && message.Button == tea.MouseLeft {
			return m.handleLeftClick(message.X, message.Y)
		}
	}

	if m.overlay.deleteConfirm != nil {
		completed, action := m.overlay.deleteConfirm.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		m.overlay.deleteConfirm = nil
		pending := m.overlay.deletePending
		m.overlay.deletePending = ""
		if action != "delete" && action != "delete_table" {
			m.overlay.deletePendingName = ""
			m.overlay.deletePendingDatabase = ""
			m.overlay.deletePendingConnection = nil
			return m, nil
		}
		switch pending {
		case "column":
			cmd := m.deleteColumn()
			m.overlay.deletePendingName = ""
			return m, cmd
		case "index":
			cmd := m.deleteIndex()
			m.overlay.deletePendingName = ""
			return m, cmd
		case "foreign_key":
			cmd := m.deleteForeignKey()
			m.overlay.deletePendingName = ""
			return m, cmd
		case "table":
			database, table := m.overlay.deletePendingDatabase, m.overlay.deletePendingName
			m.overlay.deletePendingDatabase, m.overlay.deletePendingName = "", ""
			statement := "DROP TABLE " + m.actionIdentifier(m.qualifiedTableName(database, table))
			return m.startQueryStatement(statement, true)
		case "schema":
			schema := m.overlay.deletePendingName
			m.overlay.deletePendingName, m.overlay.deletePendingDatabase = "", ""
			return m.startQueryStatement("DROP SCHEMA "+m.quoteIdentifier(schema)+" RESTRICT", true)
		case "database":
			database := m.overlay.deletePendingDatabase
			m.overlay.deletePendingDatabase, m.overlay.deletePendingName = "", ""
			return m.startQueryStatement("DROP DATABASE "+m.quoteIdentifier(database), true)
		case "connection":
			connection := m.overlay.deletePendingConnection
			m.overlay.deletePendingConnection = nil
			if connection != nil {
				m.deleteRecentConnection(*connection)
			}
			return m, nil
		default:
			m.overlay.deletePendingName = ""
			return m, m.deleteRow()
		}
	}

	switch message := message.(type) {
	case databaseOpenedMsg:
		return m.updateOpen(message)
	case directoryReadMsg:
		m.connection.pickerDir = message.dir
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
		return m, m.connection.picker.SetItems(items)
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
		// The opening transition surfaces as a Debug log notification
		// (visible only when log_level allows it), not as a plain status
		// popup. It is also transient: it logs before the connection
		// profile exists, so it never binds history to a scope.
		m.notifications.skipStatusPopup = true
		m.notifications.skipNotificationPersist = true
		log.Debug("opening database")
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
	case insertRowMsg:
		return m.updateInsertRowMsg(message)
	case cellEditorUpdatedMsg:
		return m.updateCellEditorUpdated(message)
	case sqlEditorFinishedMsg:
		return m.updateExternalEditor(message)
	case chatResponseMsg, chatStreamMsg, chatPersistMsg, chatHistoryLoadedMsg, chatMessagesLoadedMsg, chatHistoryDeletedMsg,
		assistantToolStartMsg, assistantToolContinueMsg, assistantWriteResultMsg, chatSpinnerTickMsg:
		return m.updateChat(message)
	case treeAnimTickMsg:
		return m.updateTreeAnim(message)
	}

	// Route the table popup before ordinary active dispatch; like the other
	// forms it receives every unconsumed message (huh init, mouse, keys).
	// On execute the form is retained (but hidden) until the query resolves:
	// a rejected DDL restores it, a success closes it and refreshes the
	// sidebar.
	if m.tableFormOpen() {
		command, action := m.structure.tableForm.Update(message, m.overlay.formMode)
		switch action {
		case tableFormClose:
			m.structure.tableForm = tableForm{}
		case tableFormSave:
			m.structure.tableForm.confirmation.Description = m.structure.tableForm.statement(m)
		case tableFormExecute:
			statement := m.structure.tableForm.statement(m)
			m.structure.tableFormRunning = true
			return m.startQueryStatement(statement, true)
		}
		return m, command
	}
	if m.sqlEditorActive() && m.overlay.formMode.editing() {
		previous := m.queryLog.editor.value
		command := m.queryLog.editor.update(message)
		if m.queryLog.editor.value != previous {
			m.queryLog.editorValidity = sqlValidityPending
			command = tea.Batch(command, m.scheduleSQLValidation())
		}
		return m, command
	}

	return m.updateActive(message)
}

func (m Model) executeQuery() (tea.Model, tea.Cmd) {
	if requiresQueryConfirmation(m.queryLog.editor.value) {
		m.overlay.queryConfirmation = newQueryConfirmation(m.queryLog.editor.value)
		return m, nil
	}
	return m.startQuery()
}

func (m Model) sqlEditorActive() bool {
	return m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL
}
