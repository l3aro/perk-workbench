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
	if prompt == "" || m.chat.loading {
		return nil
	}
	switch strings.ToLower(prompt) {
	case "/new":
		m.newChatConversation()
		m.Status = "new conversation"
		return nil
	case "/yolo-on":
		m.chat.yoloWrites = true
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.Status = "AI writes: on"
		return nil
	case "/yolo-off":
		m.chat.yoloWrites = false
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.Status = "AI writes: off"
		return nil
	case "/share-results":
		m.chat.shareResults = true
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.Status = "AI result sharing: on"
		return nil
	case "/unshare-results":
		m.chat.shareResults = false
		m.chat.input.Reset()
		m.chat.completion = completion{}
		m.Status = "AI result sharing: off"
		return nil
	}
	if m.chat.client == nil {
		m.Status = "AI is not configured"
		return nil
	}

	userMessage := ai.Message{Role: ai.RoleUser, Content: prompt}
	m.chat.messages = append(m.chat.messages, userMessage)
	m.chat.input.Reset()
	m.chat.completion = completion{}
	m.chat.loading = true
	m.chat.canceled = false
	m.chat.streamBuffer = ""
	m.chat.gen++
	m.chat.pendingWrite = nil
	m.chat.roundState = nil
	m.refreshChatView()

	gen := m.chat.gen
	client, history, conversationID := m.chat.client, m.chat.history, m.chat.conversationID
	agentID := client.AgentForPrompt(prompt)
	toolsDefs := m.databaseTools()
	baseMessages := append([]ai.Message(nil), m.chat.messages...)

	rootContext, cancel := context.WithCancel(m.appContext)
	m.chat.cancel = cancel

	contextText := m.chatContext()

	send := func() tea.Msg {
		if history != nil {
			if conversationID == "" {
				conversation, err := history.NewConversation(rootContext, truncateChatTitle(prompt))
				if err != nil {
					cancel()
					return chatStreamMsg{conversationID: "", err: err}
				}
				conversationID = conversation.ID
			}
			if err := history.AppendMessage(rootContext, conversationID, userMessage); err != nil {
				cancel()
				return chatStreamMsg{conversationID: conversationID, err: err}
			}
		}

		supportTools := len(toolsDefs) > 0 && client.SupportsTools(agentID)

		if !supportTools {
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
	return tea.Batch(send, m.chatSpinnerTick())
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
		m.Status = "AI conversation history is unavailable"
		return nil
	}
	history := m.chat.history
	return func() tea.Msg {
		conversations, err := history.Conversations(m.appContext)
		return chatHistoryLoadedMsg{conversations: conversations, err: err}
	}
}

func (m *Model) loadChatMessages(conversationID string) tea.Cmd {
	history := m.chat.history
	return func() tea.Msg {
		messages, err := history.Messages(m.appContext, conversationID)
		return chatMessagesLoadedMsg{conversationID: conversationID, messages: messages, err: err}
	}
}

func (m *Model) deleteChatHistory(clear bool) tea.Cmd {
	if m.chat.history == nil {
		return nil
	}
	history, conversationID := m.chat.history, m.chat.conversationID
	return func() tea.Msg {
		var err error
		if clear {
			err = history.Clear(m.appContext)
		} else if conversationID != "" {
			err = history.DeleteConversation(m.appContext, conversationID)
		}
		return chatHistoryDeletedMsg{err: err}
	}
}

func (m *Model) newChatConversation() {
	m.chat.messages = nil
	m.chat.conversationID = ""
	m.chat.input.Reset()
	m.chat.completion = completion{}
	m.refreshChatView()
}

func (m Model) updateChat(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case chatSpinnerTickMsg:
		if !m.chat.loading {
			return m, nil
		}
		m.chat.spinnerFrame++
		return m, m.chatSpinnerTick()
	case chatHistoryLoadedMsg:
		if message.err != nil {
			m.Status = safeText("AI history: " + message.err.Error())
			return m, nil
		}
		if len(message.conversations) == 0 {
			m.Status = "no saved AI conversations"
			return m, nil
		}
		choices := make([]huh.Option[string], len(message.conversations))
		for index, conversation := range message.conversations {
			choices[index] = huh.NewOption(truncateChatTitle(conversation.Title), conversation.ID)
		}
		m.chat.historyChoice = message.conversations[0].ID
		m.chatHistoryPicker = newForm(huh.NewGroup(huh.NewSelect[string]().Key("conversation").Title("AI conversations").Options(choices...).Value(&m.chat.historyChoice))).WithWidth(max(m.tableViewportWidth, 1))
		return m, m.chatHistoryPicker.Init()
	case chatMessagesLoadedMsg:
		if message.err != nil {
			m.Status = safeText("AI history: " + message.err.Error())
			return m, nil
		}
		m.chat.conversationID = message.conversationID
		m.chat.messages = message.messages
		m.refreshChatView()
		return m, nil
	case chatHistoryDeletedMsg:
		if message.err != nil {
			m.Status = safeText("AI history: " + message.err.Error())
			return m, nil
		}
		m.newChatConversation()
		m.Status = "AI conversation history cleared"
		return m, nil
	case chatPersistMsg:
		if message.err != nil {
			m.Status = safeText("AI history: " + message.err.Error())
		}
		return m, nil
	case chatStreamMsg:
		if message.conversationID != "" {
			m.chat.conversationID = message.conversationID
		}
		if message.err != nil {
			if m.chat.roundState != nil {
				m.chat.roundState.releaseContexts()
				m.chat.roundState = nil
			}
			if m.chat.cancel != nil {
				m.chat.cancel()
			}
			canceled := m.chat.canceled
			m.chat.loading = false
			m.chat.cancel = nil
			m.chat.canceled = false
			m.chat.streamBuffer = ""
			m.refreshChatView()
			if canceled {
				m.Status = "AI request canceled"
				return m, nil
			}
			m.Status = safeText("AI: " + message.err.Error())
			return m, nil
		}
		if message.done {
			// If canceled, discard partial content.
			if m.chat.canceled {
				if m.chat.roundState != nil {
					m.chat.roundState.releaseContexts()
					m.chat.roundState = nil
				}
				if m.chat.cancel != nil {
					m.chat.cancel()
				}
				m.chat.loading = false
				m.chat.cancel = nil
				m.chat.canceled = false
				m.chat.streamBuffer = ""
				m.refreshChatView()
				m.Status = "AI request canceled"
				return m, nil
			}
			wasFinalizing := m.chat.roundState != nil && m.chat.roundState.finalizing
			if m.chat.roundState != nil {
				m.chat.roundState.releaseContexts()
			}
			// Stream completed successfully — cancel the context.
			if m.chat.cancel != nil {
				m.chat.cancel()
			}
			m.chat.loading = false
			m.chat.cancel = nil
			m.chat.canceled = false
			content := m.chat.streamBuffer
			m.chat.streamBuffer = ""
			m.chat.roundState = nil
			if content == "" {
				m.refreshChatView()
				m.Status = "AI returned empty response"
				return m, nil
			}
			response := message.response
			m.chat.messages = append(m.chat.messages, ai.Message{
				Role:    ai.RoleAssistant,
				Agent:   response.Agent,
				Content: content,
			})
			m.refreshChatView()
			if wasFinalizing {
				m.Status = "Assistant response complete"
			}
			// Persist to history asynchronously. Always report result.
			if m.chat.history != nil && m.chat.conversationID != "" {
				history, cid := m.chat.history, m.chat.conversationID
				appCtx := m.appContext
				historyMsg := ai.Message{
					Role:    ai.RoleAssistant,
					Agent:   response.Agent,
					Content: content,
				}
				return m, func() tea.Msg {
					err := history.AppendMessage(appCtx, cid, historyMsg)
					return chatPersistMsg{err: err}
				}
			}
			return m, nil
		}
		// Delta: accumulate and render, then await the next event.
		m.chat.streamBuffer += message.delta
		m.refreshChatView()
		ch := message.ch
		cid := m.chat.conversationID
		return m, func() tea.Msg {
			return readStreamEvent(ch, cid)
		}
	case chatResponseMsg:
		canceled := m.chat.canceled
		m.chat.loading = false
		m.chat.cancel = nil
		m.chat.canceled = false
		m.chat.streamBuffer = ""
		if message.conversationID != "" {
			m.chat.conversationID = message.conversationID
		}
		if message.err != nil {
			if canceled {
				m.Status = "AI request canceled"
				return m, nil
			}
			m.Status = safeText("AI: " + message.err.Error())
			return m, nil
		}
		m.chat.messages = append(m.chat.messages, ai.Message{Role: ai.RoleAssistant, Agent: message.response.Agent, Content: message.response.Content})
		m.refreshChatView()
		return m, nil

	case assistantToolStartMsg:
		if message.gen != m.chat.gen {
			return m, nil // stale
		}
		m.chat.roundState = &message.state
		m.chat.messages = message.state.messages
		m.chat.conversationID = message.state.conversationID
		m.chat.cancel = message.state.cancel
		m.refreshChatView()
		return m, func() tea.Msg {
			return assistantToolContinueMsg{gen: message.gen}
		}

	case assistantToolPhaseExpiredMsg:
		if message.gen != m.chat.gen {
			return m, nil // stale
		}
		if message.state != nil {
			m.chat.roundState = message.state
			m.chat.messages = message.state.messages
			m.chat.conversationID = message.state.conversationID
			m.chat.cancel = message.state.cancel
		}
		if m.chat.roundState == nil {
			return m, nil
		}
		return m.startToolFinalization("tool time budget reached")

	case assistantToolContinueMsg:
		if message.gen != m.chat.gen || m.chat.roundState == nil {
			return m, nil
		}
		return m.processNextToolCall()

	case assistantWriteResultMsg:
		if message.gen != m.chat.gen {
			return m, nil // stale
		}
		return m.handleWriteResult(message)

	case assistantToolResultMsg:
		if message.gen != m.chat.gen {
			return m, nil // stale
		}
		return m.handleToolResult(message)
	}
	if keyPress, ok := message.(tea.KeyPressMsg); ok {
		if m.chat.loading && m.chat.chatMode != formModeInsert && keyPress.Key().Code == tea.KeyEscape {
			if m.chat.roundState != nil {
				m.chat.roundState.releaseContexts()
			}
			if m.chat.cancel != nil {
				m.chat.cancel()
				m.chat.cancel = nil
			}
			m.chat.canceled = true
			m.chat.roundState = nil
			m.chat.pendingWrite = nil
			return m, nil
		}
		switch {
		case m.keybindings.Match(keyPress, "chat.new", []scope{scopeView}):
			if m.chat.loading {
				return m, nil
			}
			m.newChatConversation()
			return m, nil
		case m.keybindings.Match(keyPress, "chat.history", []scope{scopeView}):
			if m.chat.loading {
				return m, nil
			}
			return m, m.loadChatHistory()
		case m.keybindings.Match(keyPress, "chat.delete", []scope{scopeView}):
			if m.chat.loading {
				return m, nil
			}
			return m, m.deleteChatHistory(false)
		case m.keybindings.Match(keyPress, "chat.clear", []scope{scopeView}):
			if m.chat.loading {
				return m, nil
			}
			return m, m.deleteChatHistory(true)
		case m.keybindings.Match(keyPress, "chat.apply_sql", []scope{scopeView}):
			m.applyChatSQL()
			return m, nil
		case m.keybindings.Match(keyPress, "chat.share_results", []scope{scopeView}):
			m.toggleChatResultSharing()
			return m, nil
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
			m.chat.completion = completion{}
			m.chat.chatMode = formModeNormal
			m.chat.input.Blur()
			return m, nil
		}
		if m.chat.completion.visible() {
			key := keyPress.Key()
			switch {
			case key.Code == tea.KeyUp:
				m.chat.completion.move(-1)
				return m, nil
			case key.Code == tea.KeyDown:
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
		if keyPress.Key().Code == tea.KeyEnter {
			return m, m.startChat()
		}
	}

	if m.chat.chatMode == formModeInsert {
		input, command := m.chat.input.Update(message)
		m.chat.input = input
		m.updateChatCompletion()
		return m, command
	}
	return m, nil
}

func (m *Model) applyChatSQL() {
	statement := chatSQL(m.chat.messages)
	if statement == "" {
		return
	}
	m.editor.setValue(statement)
	m.Focus, m.Tab = focusWorkspace, tabSQL
	m.Status = "AI SQL added to editor"
}

func (m *Model) toggleChatResultSharing() {
	m.chat.shareResults = !m.chat.shareResults
	if m.chat.shareResults {
		m.Status = "AI result sharing: on"
		return
	}
	m.Status = "AI result sharing: off"
}

// processNextToolCall executes the next pending tool call in the current round.
// sql_write with YOLO off creates a pendingWrite modal instead of executing.
func (m Model) processNextToolCall() (tea.Model, tea.Cmd) {
	rs := m.chat.roundState
	if rs == nil {
		return m, nil
	}

	if rs.nextCall >= len(rs.toolCalls) {
		return m.startNextToolRound()
	}

	if !rs.finalizing && time.Now().After(rs.toolDeadline) {
		return m.startToolFinalization("tool time budget reached")
	}
	if !rs.finalizing && rs.toolCallCount >= assistantMaxToolCalls {
		return m.startToolFinalization("tool-call budget reached")
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
			m.chat.messages = rs.messages
			if rs.recordToolResult(call, content) {
				return m.startToolFinalization("repeated tool result")
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
		m.chat.pendingWrite = &pendingWrite{
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
func (m Model) handleWriteResult(msg assistantWriteResultMsg) (tea.Model, tea.Cmd) {
	rs := m.chat.roundState
	if rs == nil || msg.gen != rs.gen {
		return m, nil
	}

	m.chat.pendingWrite = nil

	if msg.declined {
		// User declined — append the result and end this run explicitly.
		rs.messages = append(rs.messages, ai.Message{
			Role: ai.RoleTool, ToolID: msg.callID, ToolName: msg.callName,
			Content: "Error: " + msg.err,
		})
		m.chat.messages = rs.messages
		return m.stopToolRound("Assistant write canceled")
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
	m.chat.messages = rs.messages
	if rs.recordToolResult(call, content) {
		return m.startToolFinalization("repeated tool result")
	}

	if rs.nextCall < len(rs.toolCalls) {
		return m, func() tea.Msg { return assistantToolContinueMsg{gen: rs.gen} }
	}
	return m.startNextToolRound()
}

// handleToolResult processes the outcome of an async read-only tool execution.
func (m Model) handleToolResult(msg assistantToolResultMsg) (tea.Model, tea.Cmd) {
	rs := m.chat.roundState
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
	m.chat.messages = rs.messages
	if rs.recordToolResult(call, content) {
		return m.startToolFinalization("repeated tool result")
	}

	if rs.nextCall < len(rs.toolCalls) {
		return m, func() tea.Msg { return assistantToolContinueMsg{gen: rs.gen} }
	}
	return m.startNextToolRound()
}

// startNextToolRound issues the next Complete call from a command closure.
func (m Model) startNextToolRound() (tea.Model, tea.Cmd) {
	rs := m.chat.roundState
	if rs == nil {
		return m, nil
	}
	if !rs.finalizing && time.Now().After(rs.toolDeadline) {
		return m.startToolFinalization("tool time budget reached")
	}
	if !rs.finalizing && rs.toolCallCount >= assistantMaxToolCalls {
		return m.startToolFinalization("tool-call budget reached")
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

func (m Model) startToolFinalization(reason string) (tea.Model, tea.Cmd) {
	rs := m.chat.roundState
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
	m.chat.messages = rs.messages
	m.Status = "Assistant finalizing: " + reason
	return m.startNextToolRound()
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
func (m Model) stopToolRound(status string) (tea.Model, tea.Cmd) {
	rs := m.chat.roundState
	if rs == nil {
		return m, nil
	}
	rs.releaseContexts()
	if rs.cancel != nil {
		rs.cancel()
	}
	m.chat.loading = false
	m.chat.cancel = nil
	m.chat.roundState = nil
	m.chat.pendingWrite = nil
	m.refreshChatView()
	m.Status = status
	return m, nil
}
