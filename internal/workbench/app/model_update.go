package app

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
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
		m.browse.component.Resize(window.Width, window.Height)
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

	// Route streaming, persistence, and tool-round messages early so modal
	// branches never consume them and stall the stream.
	if chat.OwnsMessage(message) {
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
	if m.browse.component.CellViewer != nil {
		component, cmd := m.browse.component.UpdateCellViewer(message)
		m.browse.component = component
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
	if m.chat.writeConfirmation != nil {
		// While in chat insert mode, Escape exits insert mode first so the
		// write confirmation cannot interrupt the agent on the first press;
		// a second Escape (now in normal mode) declines the write.
		if keyPress, ok := message.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape && m.chat.component.InsertMode() {
			m.chat.component.ExitInsertMode()
			return m, nil
		}
		completed, action := m.chat.writeConfirmation.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		pending := *m.chat.writePending
		m.chat.writeConfirmation = nil
		m.chat.writePending = nil
		m.overlay.formMode.Mode = formModeNormal

		if action != "run" {
			// Decline — send error result and stop round.
			return m, func() tea.Msg {
				return chat.WriteResultMsg{
					Gen: pending.Generation,
					Err: "execution canceled by user", Declined: true,
				}
			}
		}

		// Approved — execute write asynchronously through the root-owned
		// database path.
		if time.Now().After(pending.Deadline) {
			return m, func() tea.Msg {
				return chat.WriteResultMsg{
					Gen: pending.Generation,
					Err: "tool time budget ended before write approval",
				}
			}
		}
		statement := pending.Statement
		db := m.Database
		ctx, cancel := context.WithDeadline(m.appContext, pending.Deadline)
		return m, func() tea.Msg {
			defer cancel()
			res, err := db.Execute(ctx, statement)
			content := ""
			errStr := ""
			if err != nil {
				errStr = err.Error()
			} else {
				content = chat.FormatResult(res)
			}
			return chat.WriteResultMsg{
				Gen: pending.Generation, Content: content, Err: errStr,
			}
		}
	}
	if m.browse.component.DocumentEditor != nil {
		component, result, cmd := m.browse.component.UpdateDocumentEditor(message, browseLayout(m), m.keybindings)
		m.browse.component = component
		switch {
		case result.Save:
			return m, m.executeDocumentSave()
		case result.CancelConfirmation:
			m.overlay.formMode.Mode = formModeNormal
		case result.Status != "":
			m.setStatus(result.Status)
			return m, nil
		}
		return m, cmd
	}
	if m.browse.component.CellEditor != nil {
		component, result, cmd := m.browse.component.UpdateCellEditor(message, browseLayout(m), m.keybindings)
		m.browse.component = component
		switch {
		case result.Save:
			// executeCellUpdate captures the editor state synchronously;
			// the editor closes immediately, before the async result
			// lands, matching the pre-refactor behavior.
			cmd := m.executeCellUpdate()
			m.browse.component.CloseCellEditor()
			return m, cmd
		case result.CancelConfirmation:
			m.overlay.formMode.Mode = formModeNormal
		}
		if result.ButtonHit {
			// The release trailing a form-button press must not also act
			// on the pane underneath (field focus, table selection).
			// One-shot: a later real release clicks normally.
			m.layout.formButtonHit = true
		}
		return m, cmd
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
		return m, tea.Batch(beginInsert(m.overlay.formMode, m.queryLog.editor), m.scheduleSQLValidation())
	}
	if m.chat.component.HistoryPicker != nil {
		component, outcome, cmd := m.chat.component.UpdateHistoryPicker(message)
		m.chat.component = component
		if outcome.Picked != "" {
			return m, m.chat.component.LoadMessages(m.chatLayout(), outcome.Picked)
		}
		return m, cmd
	}
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.applyLayout(message.Width, message.Height)
		return m, nil
	case browseDebounceMsg:
		if message.tag != m.browse.component.PageTag || message.table != m.SelectedTable || m.browse.component.Loading {
			return m, nil
		}
		if message.delta > 0 && !m.browse.component.Result.HasMore {
			return m, nil
		}
		if !m.ChangeBrowsePage(message.delta) {
			return m, nil
		}
		m.browse.component.SetPage(m.BrowsePage)
		m.browse.component.Loading = true
		return m, m.loadBrowse()
	case tea.KeyPressMsg:
		if m.schema.component.Structure.TableFiltering {
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
		if quit && !m.formActive() && !m.schema.component.Filter.Focused() &&
			!(m.State == stateConnection && (m.connection.component.RecentFilter.Focused() || (m.connection.component.Form.Focus == connectionFocusForm && m.overlay.formMode.Editing()))) &&
			!(m.sqlEditorActive() && m.overlay.formMode.Editing()) &&
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
		// insert→normal→cancel precedence wins over query.cancel etc. An
		// open write confirmation owns the key instead.
		if m.Focus == focusChat && message.Key().Code == tea.KeyEscape && m.chat.writeConfirmation == nil {
			return m.updateChat(message)
		}

		if m.State == stateReady && !m.formActive() && !m.schema.component.Filter.Focused() && !(m.Focus == focusWorkspace && m.Tab == tabSQL && m.overlay.formMode.Editing()) && !(m.Focus == focusChat && m.chat.component.ChatMode == chat.ModeInsert) {
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
				if !m.chat.component.Visible {
					return m, nil
				}
				m.Focus = focusChat
				m.queryLog.component.ClearPendingG()
				m.queryLog.editor.text.Blur()
				m.blurTables()
				if !m.vimMode {
					return m, m.chat.component.EnterInsertMode()
				}
				m.chat.component.EnterNormalMode()
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
			return m, beginInsert(m.overlay.formMode, m.queryLog.editor)
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
			if m.overlay.formMode.Editing() && m.keybindings.Match(message, "editor.complete", []scope{scopeForm, scopeView, scopeGlobal}) {
				return m, m.startCompletion()
			}
			if m.overlay.formMode.Editing() {
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
			switch formModeRoute(m.overlay.formMode, message, m.queryLog.editor) {
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
					return m, beginInsert(m.overlay.formMode, m.queryLog.editor)
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
		if m.schema.component.Structure.TableFiltering {
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
			if (m.formActive() || m.State == stateConnection) && !m.schema.component.Structure.TableFiltering {
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
	case schemaForeignKeysAllLoadedMsg:
		return m.updateSchemaForeignKeysAll(message)
	case schemaIndexesAllLoadedMsg:
		return m.updateSchemaIndexesAll(message)
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

	case schema.TreeAnimTickMsg:
		component, _, cmd := m.schema.component.Update(message, m.schemaLayout(), m.keybindings, m.schemaSnapshot())
		m.schema.component = component
		return m, cmd
	}

	// Route the table popup before ordinary active dispatch; like the other
	// forms it receives every unconsumed message (huh init, mouse, keys).
	// On execute the form is retained (but hidden) until the query resolves:
	// a rejected DDL restores it, a success closes it and refreshes the
	// sidebar.
	if m.tableFormOpen() {
		command, action := m.schema.component.Structure.TableForm.Update(message, m.overlay.formMode)
		switch action {
		case schema.TableFormClose:
			m.schema.component.Structure.TableForm = schema.TableForm{}
		case schema.TableFormSave:
			m.schema.component.Structure.TableForm.Confirmation.Description = m.tableFormStatement()
		case schema.TableFormExecute:
			statement := m.tableFormStatement()
			m.schema.component.Structure.TableFormRunning = true
			return m.startQueryStatement(statement, true)
		}
		return m, command
	}
	if m.sqlEditorActive() && m.overlay.formMode.Editing() {
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

// openTableFilter starts editing the active tab's table filter.
func (m *Model) openTableFilter() tea.Cmd {
	return m.schema.component.OpenTableFilter(m.Tab)
}

// closeTableFilter ends table-filter editing.
func (m *Model) closeTableFilter() {
	m.schema.component.CloseTableFilter()
}

// updateTableFilter routes one key through the active table filter input.
func (m *Model) updateTableFilter(message tea.KeyPressMsg) tea.Cmd {
	return m.schema.component.UpdateTableFilter(message)
}

// resetTableFilter clears the active tab's table filter.
func (m *Model) resetTableFilter() {
	m.schema.component.ResetTableFilter(m.Tab)
}

// openTableForm opens the create/rename table popup through the component;
// the form-mode controller enters insert mode on the name field, matching
// the pre-refactor openPopup.
func (m *Model) openTableForm(database, table string) tea.Cmd {
	component, _ := m.schema.component.OpenTableForm(database, table, m.workspaceLayout(), m.keybindings)
	m.schema.component = component
	return m.overlay.formMode.BeginHuh(component.Structure.TableForm.Focus())
}

// openDatabaseForm opens the create/rename database popup; only PostgreSQL
// renames databases.
func (m *Model) openDatabaseForm(originalName string) tea.Cmd {
	if originalName != "" && m.databaseInfo.Product != "PostgreSQL" {
		return nil
	}
	component, _ := m.schema.component.OpenDatabaseForm(originalName, m.workspaceLayout(), m.keybindings)
	m.schema.component = component
	return m.overlay.formMode.BeginHuh(component.Structure.TableForm.Focus())
}

// openSchemaForm opens the create/rename schema popup; only PostgreSQL has
// schemas.
func (m *Model) openSchemaForm(originalName string) tea.Cmd {
	if !m.supportsSchemas() {
		return nil
	}
	component, _ := m.schema.component.OpenSchemaForm(originalName, m.workspaceLayout(), m.keybindings)
	m.schema.component = component
	return m.overlay.formMode.BeginHuh(component.Structure.TableForm.Focus())
}

// supportsSchemas reports whether the connected product nests tables under
// schemas (PostgreSQL only).
func (m Model) supportsSchemas() bool { return m.databaseInfo.Product == "PostgreSQL" }

// tableFormStatement returns the DDL for the pending table-popup create or
// rename, quoting identifiers with the active product's rules and keeping
// the typed name verbatim. MySQL qualifies names with the selected database
// so the ALTER targets the right schema; PostgreSQL table creates carry the
// target schema.
func (m Model) tableFormStatement() string {
	form := m.schema.component.Structure.TableForm
	switch form.ObjectKind {
	case schema.TableFormDatabase:
		if form.OriginalName == "" {
			return "CREATE DATABASE " + m.quoteIdentifier(form.Name)
		}
		if m.databaseInfo.Product == "PostgreSQL" {
			return "ALTER DATABASE " + m.quoteIdentifier(form.OriginalName) + " RENAME TO " + m.quoteIdentifier(form.Name)
		}
		return ""
	case schema.TableFormSchema:
		if m.databaseInfo.Product != "PostgreSQL" {
			return ""
		}
		if form.OriginalName == "" {
			return "CREATE SCHEMA " + m.quoteIdentifier(form.Name)
		}
		return "ALTER SCHEMA " + m.quoteIdentifier(form.OriginalName) + " RENAME TO " + m.quoteIdentifier(form.Name)
	}
	if form.OriginalName != "" {
		oldName := form.Table
		if m.databaseInfo.Product == "MySQL" {
			oldName = m.qualifiedTableName(form.Database, form.OriginalName)
		}
		newName := form.Name
		if m.databaseInfo.Product == "MySQL" {
			newName = m.qualifiedTableName(form.Database, form.Name)
		}
		return "ALTER TABLE " + m.actionIdentifier(oldName) + " RENAME TO " + m.actionIdentifier(newName)
	}
	createName := form.Name
	switch m.databaseInfo.Product {
	case "MySQL":
		createName = m.qualifiedTableName(form.Database, form.Name)
	case "PostgreSQL":
		// form.Database carries the target schema from the sidebar item.
		createName = form.Database + "." + form.Name
	}
	return "CREATE TABLE " + m.actionIdentifier(createName) + " (id INTEGER PRIMARY KEY)"
}

// qualifiedTableName returns name for SQLite and PostgreSQL (whose sidebar
// tables already carry schema.table) and database.name for MySQL.
func (m Model) qualifiedTableName(database, name string) string {
	if m.databaseInfo.Product == "MySQL" {
		return database + "." + name
	}
	return name
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
