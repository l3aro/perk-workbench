package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
)

const (
	assistantToolPhaseTimeout        = 2 * time.Minute
	assistantFinalizationTimeout     = 20 * time.Second
	assistantMaxToolCalls            = 64
	assistantRepeatedToolResultLimit = 3
	chatSpinnerInterval              = 80 * time.Millisecond
)

// chatSpinnerTick re-arms the assistant progress spinner tick.
func (m Model) chatSpinnerTick() tea.Cmd {
	return tea.Tick(chatSpinnerInterval, func(time.Time) tea.Msg { return chatSpinnerTickMsg{} })
}

func (m *Model) startChat() tea.Cmd {
	prompt := strings.TrimSpace(m.chat.input.Value())
	if prompt == "" {
		return nil
	}
	switch strings.ToLower(prompt) {
	case "/new":
		m.newChatConversation()
		m.setStatus("new conversation")
		return nil
	case "/history":
		return m.loadChatHistory()
	case "/yolo-on":
		m.chat.yoloWrites = true
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.setStatus("AI writes: on")
		return nil
	case "/yolo-off":
		m.chat.yoloWrites = false
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.setStatus("AI writes: off")
		return nil
	case "/share-results":
		m.chat.shareResults = true
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.setStatus("AI result sharing: on")
		return nil
	case "/unshare-results":
		m.chat.shareResults = false
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.setStatus("AI result sharing: off")
		return nil
	}
	if m.chat.client == nil {
		m.setStatus("AI is not configured")
		return nil
	}
	// A chat already running in the visible conversation swallows new input;
	// other conversations keep running independently in the background.
	if m.chat.activeRun().loading {
		return nil
	}

	userMessage := ai.Message{Role: ai.RoleUser, Content: prompt}
	conversationID := m.chat.activeID
	fresh := conversationID == ""
	// Capture the connection scope for the whole turn: a background run keeps
	// persisting to the connection it started on after a disconnect.
	connectionID := m.connectionID
	// History persistence is connection-scoped: without a profile scope there
	// is nothing safe to write, so the chat stays usable in memory only.
	historyAvailable := m.chat.history != nil && connectionID != ""
	if m.chat.history != nil && !historyAvailable {
		m.setStatus("AI conversation history is unavailable")
	}
	if fresh && historyAvailable {
		conversation, err := m.chat.history.NewConversation(m.appContext, connectionID, truncateChatTitle(prompt))
		if err != nil {
			m.setStatus(safeText("AI history: " + err.Error()))
			return nil
		}
		conversationID = conversation.ID
		m.chat.activeID = conversationID
	}
	run := m.chat.runs[conversationID]
	if run == nil {
		run = &chatRun{conversationID: conversationID}
		m.chat.runs[conversationID] = run
	}
	run.connectionID = connectionID
	// Record only once the prompt is accepted: a failed conversation creation
	// must not pollute recall history.
	m.chat.recordPromptHistory(prompt)
	run.messages = append(run.messages, userMessage)
	m.chat.input.Reset()
	m.chat.completion = completion{}
	run.loading = true
	run.canceled = false
	run.streamBuffer = ""
	run.gen = m.chat.nextGen
	m.chat.nextGen++
	run.pendingWrite = nil
	run.roundState = nil
	m.refreshChatView()

	gen := run.gen
	client, history := m.chat.client, m.chat.history
	agentID := client.AgentForPrompt(prompt)
	toolsDefs := m.databaseTools()
	baseMessages := append([]ai.Message(nil), run.messages...)

	rootContext, cancel := context.WithCancel(m.appContext)
	run.cancel = cancel

	contextText := m.chatContext()

	send := func() tea.Msg {
		if historyAvailable {
			if err := history.AppendMessage(rootContext, connectionID, conversationID, userMessage); err != nil {
				cancel()
				return chatStreamMsg{conversationID: conversationID, err: err}
			}
		}

		supportTools := len(toolsDefs) > 0 && client.SupportsTools(agentID)

		if !supportTools {
			contextText += m.chatResultsContext()
			contextText = truncateChatContext(contextText)
			eventCh, err := client.ChatStream(rootContext, ai.Request{
				AgentID:  agentID,
				Messages: baseMessages,
				Context:  contextText,
			})
			if err != nil {
				cancel()
				return chatStreamMsg{conversationID: conversationID, err: err}
			}
			return readStreamEvent(eventCh, conversationID)
		}
		toolDeadline := time.Now().Add(assistantToolPhaseTimeout)
		toolContext, toolCancel := context.WithDeadline(rootContext, toolDeadline)
		toolState := toolRoundState{
			gen:            gen,
			messages:       baseMessages,
			agentID:        agentID,
			client:         client,
			history:        history,
			rootContext:    rootContext,
			chatContext:    toolContext,
			cancel:         cancel,
			toolCancel:     toolCancel,
			contextText:    contextText,
			toolsDefs:      toolsDefs,
			conversationID: conversationID,
			toolDeadline:   toolDeadline,
		}

		// Tool calls run until the phase deadline, then one tools-disabled
		// finalization request gets the answer from gathered results.
		turn, err := client.Complete(toolContext, ai.Request{
			AgentID:  agentID,
			Messages: baseMessages,
			Context:  contextText,
			Tools:    toolsDefs,
		})
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return assistantToolPhaseExpiredMsg{gen: gen, state: &toolState}
			}
			cancel()
			return chatStreamMsg{conversationID: conversationID, err: err}
		}

		if len(turn.ToolCalls) == 0 {
			// No tool calls — deliver as final answer.
			ch := make(chan ai.StreamEvent, 2)
			ch <- ai.StreamEvent{Kind: ai.EventDelta, Delta: turn.Content}
			ch <- ai.StreamEvent{Kind: ai.EventDone, Response: &turn}
			close(ch)
			return readStreamEvent(ch, conversationID)
		}

		toolState.messages = append(append([]ai.Message(nil), baseMessages...), ai.Message{
			Role:      ai.RoleAssistant,
			Content:   turn.Content,
			ToolCalls: turn.ToolCalls,
		})
		toolState.toolCalls = turn.ToolCalls
		return assistantToolStartMsg{gen: gen, state: toolState}
	}
	// Title generation runs in parallel with the chat request, only for a
	// brand-new conversation, and never blocks the stream on the model call.
	titleCmd := func() tea.Msg {
		title, err := client.GenerateTitle(m.appContext, prompt)
		if err != nil {
			return chatTitleMsg{connectionID: connectionID, conversationID: conversationID, err: err}
		}
		if title == "" {
			return chatTitleMsg{connectionID: connectionID, conversationID: conversationID}
		}
		if err := history.RenameConversation(m.appContext, connectionID, conversationID, title); err != nil {
			return chatTitleMsg{connectionID: connectionID, conversationID: conversationID, err: err}
		}
		return chatTitleMsg{connectionID: connectionID, conversationID: conversationID, title: title}
	}
	cmds := []tea.Cmd{send, m.chatSpinnerTick()}
	if fresh && historyAvailable {
		cmds = append(cmds, titleCmd)
	}
	return tea.Batch(cmds...)
}

// readStreamEvent reads one event from the stream channel and returns it as a chatStreamMsg.
func readStreamEvent(ch <-chan ai.StreamEvent, conversationID string) tea.Msg {
	ev, ok := <-ch
	if !ok {
		return chatStreamMsg{ch: ch, conversationID: conversationID, done: true}
	}
	msg := chatStreamMsg{ch: ch, conversationID: conversationID, delta: ev.Delta}
	if ev.Response != nil {
		msg.done = true
		msg.response = *ev.Response
	}
	if ev.Err != nil {
		msg.err = ev.Err
	}
	return msg
}

func (m *Model) loadChatHistory() tea.Cmd {
	if m.chat.history == nil {
		m.setStatus("AI conversation history is unavailable")
		return nil
	}
	history := m.chat.history
	connectionID := m.connectionID
	return func() tea.Msg {
		conversations, err := history.Conversations(m.appContext, connectionID)
		return chatHistoryLoadedMsg{connectionID: connectionID, conversations: conversations, err: err}
	}
}

func (m *Model) loadChatMessages(conversationID string) tea.Cmd {
	history := m.chat.history
	connectionID := m.connectionID
	m.chat.loadSeq++
	seq := m.chat.loadSeq
	return func() tea.Msg {
		messages, err := history.Messages(m.appContext, connectionID, conversationID)
		return chatMessagesLoadedMsg{connectionID: connectionID, conversationID: conversationID, messages: messages, seq: seq, err: err}
	}
}

func (m *Model) deleteChatHistory(clear bool) tea.Cmd {
	if m.chat.history == nil {
		return nil
	}
	history, conversationID := m.chat.history, m.chat.activeID
	connectionID := m.connectionID
	return func() tea.Msg {
		var err error
		if clear {
			err = history.Clear(m.appContext, connectionID)
		} else if conversationID != "" {
			err = history.DeleteConversation(m.appContext, connectionID, conversationID)
		}
		return chatHistoryDeletedMsg{err: err, connectionID: connectionID, conversationID: conversationID, clear: clear}
	}
}

func (m *Model) newChatConversation() {
	if run := m.chat.runs[""]; run != nil {
		if run.cancel != nil {
			run.cancel()
		}
		delete(m.chat.runs, "")
	}
	m.chat.activeID = ""
	m.chat.input.Reset()
	m.chat.completion = completion{}
	m.refreshChatView()
}

func (m Model) updateChat(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case chatSpinnerTickMsg:
		run := m.chat.activeRun()
		if !run.loading {
			return m, nil
		}
		run.spinnerFrame++
		return m, m.chatSpinnerTick()
	case chatHistoryLoadedMsg:
		if message.connectionID != m.connectionID {
			return m, nil // stale: started before a disconnect/reconnect
		}
		if message.err != nil {
			m.setStatus(safeText("AI history: " + message.err.Error()))
			return m, nil
		}
		if len(message.conversations) == 0 {
			m.setStatus("no saved AI conversations")
			return m, nil
		}
		choices := make([]huh.Option[string], len(message.conversations))
		for index, conversation := range message.conversations {
			choices[index] = huh.NewOption(chatHistoryOptionLabel(m.chat.runs[conversation.ID], conversation.Title), conversation.ID)
		}
		m.chat.historyChoice = message.conversations[0].ID
		m.chat.historyPicker = newForm(huh.NewGroup(huh.NewSelect[string]().Key("conversation").Title("AI conversations").Options(choices...).Value(&m.chat.historyChoice))).WithWidth(max(m.layout.tableViewportWidth, 1))
		return m, m.chat.historyPicker.Init()
	case chatMessagesLoadedMsg:
		if message.connectionID != m.connectionID {
			return m, nil // stale: started before a disconnect/reconnect
		}
		if message.seq != m.chat.loadSeq {
			return m, nil // stale selection load
		}
		if message.err != nil {
			m.setStatus(safeText("AI history: " + message.err.Error()))
			return m, nil
		}
		m.chat.activeID = message.conversationID
		run := m.chat.runs[message.conversationID]
		if run == nil {
			run = &chatRun{conversationID: message.conversationID, messages: message.messages}
			m.chat.runs[message.conversationID] = run
		}
		m.refreshChatView()
		return m, nil
	case chatHistoryDeletedMsg:
		if message.connectionID != m.connectionID {
			return m, nil // stale: started before a disconnect/reconnect
		}
		if message.err != nil {
			m.setStatus(safeText("AI history: " + message.err.Error()))
			return m, nil
		}
		if message.clear {
			for _, run := range m.chat.runs {
				if run.roundState != nil {
					run.roundState.releaseContexts()
				}
				if run.cancel != nil {
					run.cancel()
				}
			}
			m.chat.runs = map[string]*chatRun{}
		} else if run := m.chat.runs[message.conversationID]; run != nil {
			if run.roundState != nil {
				run.roundState.releaseContexts()
			}
			if run.cancel != nil {
				run.cancel()
			}
			delete(m.chat.runs, message.conversationID)
		}
		if message.clear || m.chat.activeID == message.conversationID {
			m.newChatConversation()
		}
		m.setStatus("AI conversation history cleared")
		return m, nil
	case chatPersistMsg:
		if message.err != nil {
			m.setStatus(safeText("AI history: " + message.err.Error()))
		}
		return m, nil
	case chatTitleMsg:
		if message.connectionID != m.connectionID {
			return m, nil // stale: started before a disconnect/reconnect
		}
		if message.err != nil {
			if run := m.chat.runs[message.conversationID]; run != nil && m.chat.isActive(run) {
				m.setStatus(safeText("AI title: " + message.err.Error()))
			}
		}
		return m, nil
	case chatStreamMsg:
		run := m.chat.runs[message.conversationID]
		if run == nil {
			return m, nil // conversation deleted or never registered
		}
		if message.err != nil {
			if run.roundState != nil {
				run.roundState.releaseContexts()
				run.roundState = nil
			}
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			canceled := run.canceled
			run.loading = false
			run.canceled = false
			run.streamBuffer = ""
			if m.chat.isActive(run) {
				m.refreshChatView()
			}
			if canceled {
				if m.chat.isActive(run) {
					m.setStatus("AI request canceled")
				}
				return m, nil
			}
			if m.chat.isActive(run) {
				m.setStatus(safeText("AI: " + message.err.Error()))
			}
			return m, nil
		}
		if message.done {
			// If canceled, discard partial content.
			if run.canceled {
				if run.roundState != nil {
					run.roundState.releaseContexts()
					run.roundState = nil
				}
				if run.cancel != nil {
					run.cancel()
					run.cancel = nil
				}
				run.loading = false
				run.canceled = false
				run.streamBuffer = ""
				if m.chat.isActive(run) {
					m.refreshChatView()
					m.setStatus("AI request canceled")
				}
				return m, nil
			}
			wasFinalizing := run.roundState != nil && run.roundState.finalizing
			if run.roundState != nil {
				run.roundState.releaseContexts()
			}
			// Stream completed successfully — cancel the context.
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			run.loading = false
			run.canceled = false
			content := run.streamBuffer
			run.streamBuffer = ""
			run.roundState = nil
			if content == "" {
				if m.chat.isActive(run) {
					m.refreshChatView()
					m.setStatus("AI returned empty response")
				}
				return m, nil
			}
			response := message.response
			run.messages = append(run.messages, ai.Message{
				Role:    ai.RoleAssistant,
				Agent:   response.Agent,
				Content: content,
			})
			if m.chat.isActive(run) {
				m.refreshChatView()
			}
			if wasFinalizing && m.chat.isActive(run) {
				m.setStatus("Assistant response complete")
			}
			// Persist to history asynchronously; report errors regardless of
			// which conversation is visible. The run keeps its captured scope
			// even if the model has since disconnected.
			if m.chat.history != nil && run.connectionID != "" && run.conversationID != "" {
				history, cid, scope := m.chat.history, run.conversationID, run.connectionID
				appCtx := m.appContext
				historyMsg := ai.Message{
					Role:    ai.RoleAssistant,
					Agent:   response.Agent,
					Content: content,
				}
				return m, func() tea.Msg {
					err := history.AppendMessage(appCtx, scope, cid, historyMsg)
					return chatPersistMsg{err: err}
				}
			}
			return m, nil
		}
		// Delta: accumulate and render, then await the next event.
		run.streamBuffer += message.delta
		if m.chat.isActive(run) {
			m.refreshChatView()
		}
		ch := message.ch
		cid := run.conversationID
		return m, func() tea.Msg {
			return readStreamEvent(ch, cid)
		}
	case chatResponseMsg:
		run := m.chat.runs[message.conversationID]
		if run == nil {
			return m, nil
		}
		canceled := run.canceled
		run.loading = false
		run.cancel = nil
		run.canceled = false
		run.streamBuffer = ""
		if message.err != nil {
			if m.chat.isActive(run) {
				if canceled {
					m.setStatus("AI request canceled")
				} else {
					m.setStatus(safeText("AI: " + message.err.Error()))
				}
			}
			return m, nil
		}
		run.messages = append(run.messages, ai.Message{Role: ai.RoleAssistant, Agent: message.response.Agent, Content: message.response.Content})
		if m.chat.isActive(run) {
			m.refreshChatView()
		}
		return m, nil

	case assistantToolStartMsg:
		run := m.chat.runs[message.state.conversationID]
		if run == nil || message.gen != run.gen {
			return m, nil // stale or deleted
		}
		run.roundState = &message.state
		run.messages = message.state.messages
		run.cancel = message.state.cancel
		if m.chat.isActive(run) {
			m.refreshChatView()
		}
		return m, func() tea.Msg {
			return assistantToolContinueMsg{gen: message.gen}
		}

	case assistantToolPhaseExpiredMsg:
		run := m.chat.runByGen(message.gen)
		if run == nil {
			return m, nil // stale
		}
		if message.state != nil {
			run.roundState = message.state
			run.messages = message.state.messages
			run.cancel = message.state.cancel
		}
		if run.roundState == nil {
			return m, nil
		}
		return m.startToolFinalization(run, "tool time budget reached")

	case assistantToolContinueMsg:
		run := m.chat.runByGen(message.gen)
		if run == nil || run.roundState == nil {
			return m, nil
		}
		return m.processNextToolCall(run)

	case assistantWriteResultMsg:
		run := m.chat.runByGen(message.gen)
		if run == nil {
			return m, nil // stale
		}
		return m.handleWriteResult(run, message)

	case assistantToolResultMsg:
		run := m.chat.runByGen(message.gen)
		if run == nil {
			return m, nil // stale
		}
		return m.handleToolResult(run, message)
	}
	if keyPress, ok := message.(tea.KeyPressMsg); ok {
		run := m.chat.activeRun()
		if run.loading && m.chat.chatMode != formModeInsert && keyPress.Key().Code == tea.KeyEscape {
			if run.roundState != nil {
				run.roundState.releaseContexts()
			}
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			run.canceled = true
			run.roundState = nil
			run.pendingWrite = nil
			return m, nil
		}
		switch {
		case m.keybindings.Match(keyPress, "chat.delete", []scope{scopeView}):
			return m, m.deleteChatHistory(false)
		case m.keybindings.Match(keyPress, "chat.clear", []scope{scopeView}):
			return m, m.deleteChatHistory(true)
		case m.keybindings.Match(keyPress, "chat.apply_sql", []scope{scopeView}):
			return m, m.applyChatSQL()
		case keyPress.Key().Code == tea.KeyPgUp:
			m.chat.viewport.PageUp()
			return m, nil
		case keyPress.Key().Code == tea.KeyPgDown:
			m.chat.viewport.PageDown()
			return m, nil
		}

		if m.chat.chatMode == formModeNormal {
			if keyPress.Key().Code == 'i' || keyPress.Key().Code == tea.KeyEnter {
				m.chat.chatMode = formModeInsert
				return m, m.chat.input.Focus()
			}
			return m, nil
		}

		// Insert mode
		if keyPress.Key().Code == tea.KeyEscape {
			m.chat.completion.dismiss()
			m.chat.chatMode = formModeNormal
			m.chat.input.Blur()
			return m, nil
		}
		if m.chat.completion.visible() {
			key := keyPress.Key()
			switch {
			case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == tea.ModCtrl):
				m.chat.completion.move(-1)
				return m, nil
			case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == tea.ModCtrl):
				m.chat.completion.move(1)
				return m, nil
			case key.Code == tea.KeyTab:
				m.acceptChatCompletion()
				return m, nil
			case key.Code == tea.KeyEnter:
				item := m.chat.completion.accept()
				m.chat.completion = completion{}
				if item.InsertText == "" || item.InsertText == m.chat.input.Value() {
					// Already complete (e.g. "/new"): Enter runs it.
					return m, m.startChat()
				}
				m.chat.input.SetValue(item.InsertText)
				return m, nil
			}
		}
		key := keyPress.Key()
		if (key.Code == tea.KeyUp && (m.chat.input.Value() == "" || m.chat.historyIndex >= 0)) ||
			(key.Code == tea.KeyDown && m.chat.historyIndex >= 0) {
			direction := 1
			if key.Code == tea.KeyDown {
				direction = -1
			}
			if value, ok := m.chat.recallPromptHistory(direction); ok {
				m.chat.input.SetValue(value)
				return m, nil
			}
		}
		if keyPress.Key().Code == tea.KeyEnter {
			return m, m.startChat()
		}
	}

	if m.chat.chatMode == formModeInsert {
		previous := m.chat.input.Value()
		input, command := m.chat.input.Update(message)
		m.chat.input = input
		if m.chat.input.Value() != previous {
			m.chat.historyIndex = -1
		}
		m.updateChatCompletion()
		return m, command
	}
	return m, nil
}

func (m *Model) applyChatSQL() tea.Cmd {
	statement := chatSQL(m.chat.activeRun().messages)
	if statement == "" {
		return nil
	}
	m.queryLog.editor.setValue(statement)
	m.Focus, m.Tab = focusWorkspace, tabSQL
	m.setStatus("AI SQL added to editor")
	m.queryLog.editorValidity = sqlValidityPending
	return m.scheduleSQLValidation()
}

// processNextToolCall executes the next pending tool call in the current round.
// sql_write with YOLO off creates a pendingWrite modal instead of executing.
func (m Model) processNextToolCall(run *chatRun) (tea.Model, tea.Cmd) {
	rs := run.roundState
	if rs == nil {
		return m, nil
	}

	if rs.nextCall >= len(rs.toolCalls) {
		return m.startNextToolRound(run)
	}

	if !rs.finalizing && time.Now().After(rs.toolDeadline) {
		return m.startToolFinalization(run, "tool time budget reached")
	}
	if !rs.finalizing && rs.toolCallCount >= assistantMaxToolCalls {
		return m.startToolFinalization(run, "tool-call budget reached")
	}
	rs.toolCallCount++

	call := rs.toolCalls[rs.nextCall]

	switch call.Name {
	case "sql_write":
		query, _ := call.Input["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			rs.nextCall++
			content := "Error: query argument is required"
			rs.messages = append(rs.messages, ai.Message{
				Role: ai.RoleTool, ToolID: call.ID, ToolName: call.Name, Content: content,
			})
			run.messages = rs.messages
			if rs.recordToolResult(call, content) {
				return m.startToolFinalization(run, "repeated tool result")
			}
			return m, func() tea.Msg { return assistantToolContinueMsg{gen: rs.gen} }
		}
		if m.chat.yoloWrites {
			// Execute async through the same result path as approved writes.
			db := m.Database
			chatCtx := rs.chatContext
			gen := rs.gen
			callID := call.ID
			callName := call.Name
			return m, func() tea.Msg {
				res, err := db.Execute(chatCtx, query)
				content := ""
				errStr := ""
				if err != nil {
					errStr = "executing statement: " + err.Error()
				} else {
					content = formatResult(res)
				}
				return assistantWriteResultMsg{
					gen: gen, callID: callID, callName: callName,
					content: content, err: errStr,
				}
			}
		}
		// Show confirmation modal.
		run.pendingWrite = &pendingWrite{
			generation: rs.gen,
			call:       call,
			statement:  query,
			dialog:     yesNoConfirmation("Run assistant SQL write?", query, "run"),
		}
		return m, nil

	default:
		ctx := rs.chatContext
		gen := rs.gen
		callID := call.ID
		callName := call.Name
		return m, func() tea.Msg {
			result := m.executeTool(ctx, call)
			content := result.Content
			errStr := ""
			if result.Error != "" {
				errStr = result.Error
			}
			return assistantToolResultMsg{
				gen: gen, callID: callID, callName: callName,
				content: content, err: errStr,
			}
		}
	}
}

// handleWriteResult processes the outcome of an async sql_write execution.
func (m Model) handleWriteResult(run *chatRun, msg assistantWriteResultMsg) (tea.Model, tea.Cmd) {
	rs := run.roundState
	if rs == nil || msg.gen != rs.gen {
		return m, nil
	}

	run.pendingWrite = nil

	if msg.declined {
		// User declined — append the result and end this run explicitly.
		rs.messages = append(rs.messages, ai.Message{
			Role: ai.RoleTool, ToolID: msg.callID, ToolName: msg.callName,
			Content: "Error: " + msg.err,
		})
		run.messages = rs.messages
		return m.stopToolRound(run, "Assistant write canceled")
	}

	content := msg.content
	if msg.err != "" {
		content = "Error: " + msg.err
	}
	call := rs.toolCalls[rs.nextCall]
	rs.messages = append(rs.messages, ai.Message{
		Role: ai.RoleTool, ToolID: msg.callID, ToolName: msg.callName, Content: content,
	})
	rs.nextCall++
	run.messages = rs.messages
	if rs.recordToolResult(call, content) {
		return m.startToolFinalization(run, "repeated tool result")
	}

	if rs.nextCall < len(rs.toolCalls) {
		return m, func() tea.Msg { return assistantToolContinueMsg{gen: rs.gen} }
	}
	return m.startNextToolRound(run)
}

// handleToolResult processes the outcome of an async read-only tool execution.
func (m Model) handleToolResult(run *chatRun, msg assistantToolResultMsg) (tea.Model, tea.Cmd) {
	rs := run.roundState
	if rs == nil || msg.gen != rs.gen {
		return m, nil
	}

	content := msg.content
	if msg.err != "" {
		content = "Error: " + msg.err
	}

	call := rs.toolCalls[rs.nextCall]
	rs.messages = append(rs.messages, ai.Message{
		Role: ai.RoleTool, ToolID: msg.callID, ToolName: msg.callName, Content: content,
	})
	rs.nextCall++
	run.messages = rs.messages
	if rs.recordToolResult(call, content) {
		return m.startToolFinalization(run, "repeated tool result")
	}

	if rs.nextCall < len(rs.toolCalls) {
		return m, func() tea.Msg { return assistantToolContinueMsg{gen: rs.gen} }
	}
	return m.startNextToolRound(run)
}

// startNextToolRound issues the next Complete call from a command closure.
func (m Model) startNextToolRound(run *chatRun) (tea.Model, tea.Cmd) {
	rs := run.roundState
	if rs == nil {
		return m, nil
	}
	if !rs.finalizing && time.Now().After(rs.toolDeadline) {
		return m.startToolFinalization(run, "tool time budget reached")
	}
	if !rs.finalizing && rs.toolCallCount >= assistantMaxToolCalls {
		return m.startToolFinalization(run, "tool-call budget reached")
	}

	rs.nextCall = 0
	rs.toolCalls = nil

	client := rs.client
	chatContext := rs.chatContext
	agentID := rs.agentID
	contextText := rs.contextText
	toolsDefs := rs.toolsDefs
	msgs := rs.messages
	gen := rs.gen
	history := rs.history
	cancel := rs.cancel
	conversationID := rs.conversationID
	finalizing := rs.finalizing

	return m, func() tea.Msg {
		roundTools := toolsDefs
		if finalizing {
			roundTools = nil
			contextText += "\n\nTool use is no longer available. Answer the user now using the gathered results. If evidence is incomplete, state that plainly."
		}
		turn, err := client.Complete(chatContext, ai.Request{
			AgentID:  agentID,
			Messages: msgs,
			Context:  contextText,
			Tools:    roundTools,
		})
		if err != nil {
			if !finalizing && errors.Is(err, context.DeadlineExceeded) {
				return assistantToolPhaseExpiredMsg{gen: gen}
			}
			return chatStreamMsg{conversationID: conversationID, err: err}
		}
		if finalizing && len(turn.ToolCalls) > 0 {
			turn.ToolCalls = nil
			if strings.TrimSpace(turn.Content) == "" {
				turn.Content = "I couldn't produce a final answer before the tool budget ended."
			}
		}
		if len(turn.ToolCalls) == 0 {
			ch := make(chan ai.StreamEvent, 2)
			ch <- ai.StreamEvent{Kind: ai.EventDelta, Delta: turn.Content}
			ch <- ai.StreamEvent{Kind: ai.EventDone, Response: &turn}
			close(ch)
			return readStreamEvent(ch, conversationID)
		}

		newMsgs := append(msgs, ai.Message{
			Role: ai.RoleAssistant, Content: turn.Content, ToolCalls: turn.ToolCalls,
		})
		return assistantToolStartMsg{
			gen: gen,
			state: toolRoundState{
				gen:                     gen,
				messages:                newMsgs,
				agentID:                 agentID,
				client:                  client,
				history:                 history,
				rootContext:             rs.rootContext,
				chatContext:             chatContext,
				toolCancel:              rs.toolCancel,
				cancel:                  cancel,
				contextText:             rs.contextText,
				toolsDefs:               toolsDefs,
				conversationID:          conversationID,
				toolCalls:               turn.ToolCalls,
				toolCallCount:           rs.toolCallCount,
				toolDeadline:            rs.toolDeadline,
				lastToolResultSignature: rs.lastToolResultSignature,
				repeatedToolResults:     rs.repeatedToolResults,
			},
		}
	}
}

func (m Model) startToolFinalization(run *chatRun, reason string) (tea.Model, tea.Cmd) {
	rs := run.roundState
	if rs == nil || rs.finalizing {
		return m, nil
	}
	rs.skipRemainingToolCalls("tool budget ended")
	if rs.toolCancel != nil {
		rs.toolCancel()
		rs.toolCancel = nil
	}
	rs.finalizing = true
	rs.chatContext, rs.finalizationCancel = context.WithTimeout(rs.rootContext, assistantFinalizationTimeout)
	run.messages = rs.messages
	if m.chat.isActive(run) {
		m.setStatus("Assistant finalizing: " + reason)
	}
	return m.startNextToolRound(run)
}

func (rs *toolRoundState) recordToolResult(call ai.ToolCall, content string) bool {
	input, err := json.Marshal(call.Input)
	if err != nil {
		rs.lastToolResultSignature = ""
		rs.repeatedToolResults = 0
		return false
	}
	signature := call.Name + "\x00" + string(input) + "\x00" + content
	if signature == rs.lastToolResultSignature {
		rs.repeatedToolResults++
	} else {
		rs.lastToolResultSignature = signature
		rs.repeatedToolResults = 1
	}
	return rs.repeatedToolResults >= assistantRepeatedToolResultLimit
}

func (rs *toolRoundState) releaseContexts() {
	if rs.toolCancel != nil {
		rs.toolCancel()
	}
	if rs.finalizationCancel != nil {
		rs.finalizationCancel()
	}
}

func (rs *toolRoundState) skipRemainingToolCalls(reason string) {
	for rs.nextCall < len(rs.toolCalls) {
		call := rs.toolCalls[rs.nextCall]
		rs.messages = append(rs.messages, ai.Message{
			Role: ai.RoleTool, ToolID: call.ID, ToolName: call.Name, Content: "Skipped: " + reason,
		})
		rs.nextCall++
	}
}

// stopToolRound cleans up a deliberately stopped tool run.
func (m Model) stopToolRound(run *chatRun, status string) (tea.Model, tea.Cmd) {
	rs := run.roundState
	if rs == nil {
		return m, nil
	}
	rs.releaseContexts()
	if rs.cancel != nil {
		rs.cancel()
	}
	run.loading = false
	run.cancel = nil
	run.roundState = nil
	run.pendingWrite = nil
	if m.chat.isActive(run) {
		m.refreshChatView()
		m.setStatus(status)
	}
	return m, nil
}
