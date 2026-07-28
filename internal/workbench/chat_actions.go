package workbench

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
)

func (m *Model) startChat() tea.Cmd {
	prompt := strings.TrimSpace(m.chat.input.Value())
	if prompt == "" || m.chat.loading {
		return nil
	}
	if m.chat.client == nil {
		m.Status = "AI is not configured"
		return nil
	}

	userMessage := ai.Message{Role: ai.RoleUser, Content: prompt}
	m.chat.messages = append(m.chat.messages, userMessage)
	m.chat.input.Reset()
	m.chat.loading = true
	m.chat.canceled = false
	m.chat.streamBuffer = ""
	m.refreshChatView()

	// Clone the model state needed inside the closure.
	client, history, conversationID := m.chat.client, m.chat.history, m.chat.conversationID
	agentID := client.AgentForPrompt(prompt)
	toolsDefs := m.databaseTools()

	// Build the message list. Internal tool-round messages are kept separate
	// from the visible transcript.
	baseMessages := append([]ai.Message(nil), m.chat.messages...)

	chatContext, cancel := context.WithCancel(m.appContext)
	m.chat.cancel = cancel

	// contextText is captured at send time; tool results extend the message list, not context.
	contextText := m.chatContext()

	return func() tea.Msg {
		if history != nil {
			if conversationID == "" {
				conversation, err := history.NewConversation(chatContext, truncateChatTitle(prompt))
				if err != nil {
					cancel()
					return chatStreamMsg{conversationID: "", err: err}
				}
				conversationID = conversation.ID
			}
			if err := history.AppendMessage(chatContext, conversationID, userMessage); err != nil {
				cancel()
				return chatStreamMsg{conversationID: conversationID, err: err}
			}
		}

		// Check whether tools are available and supported.
		supportTools := len(toolsDefs) > 0 && client.SupportsTools(agentID)

		if !supportTools {
			// No tools — use the original streaming path unchanged.
			eventCh, err := client.ChatStream(chatContext, ai.Request{
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

		// --- Tool round: non-streaming request with tools. ---
		messages := append([]ai.Message(nil), baseMessages...)
		const maxToolRounds = 4
		toolRound := 0
		for ; toolRound < maxToolRounds; toolRound++ {
			turn, err := client.Complete(chatContext, ai.Request{
				AgentID:  agentID,
				Messages: messages,
				Context:  contextText,
				Tools:    toolsDefs,
			})
			if err != nil {
				cancel()
				return chatStreamMsg{conversationID: conversationID, err: err}
			}

			if len(turn.ToolCalls) == 0 {
				// Final answer ready. Deliver through a synthetic channel so the
				// existing UI/persistence path works correctly.
				ch := make(chan ai.StreamEvent, 2)
				ch <- ai.StreamEvent{Kind: ai.EventDelta, Delta: turn.Content}
				ch <- ai.StreamEvent{Kind: ai.EventDone, Response: &turn}
				close(ch)
				return readStreamEvent(ch, conversationID)
			}

			// Append assistant message with tool calls.
			messages = append(messages, ai.Message{
				Role:      ai.RoleAssistant,
				Content:   turn.Content,
				ToolCalls: turn.ToolCalls,
			})

			// Execute each tool call and append results.
			for _, call := range turn.ToolCalls {
				result := m.executeTool(chatContext, call)
				resultMsg := ai.Message{
					Role:     ai.RoleTool,
					ToolID:   call.ID,
					ToolName: call.Name,
					Content:  result.Content,
				}
				if result.Error != "" {
					resultMsg.Content = "Error: " + result.Error
				}
				messages = append(messages, resultMsg)
			}
		}

		// Fallback: all maxToolRounds consumed with tool calls still returned.
		cancel()
		return chatStreamMsg{
			conversationID: conversationID,
			err:            fmt.Errorf("maximum tool rounds exceeded"),
		}
	}
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
	m.refreshChatView()
}

func (m Model) updateChat(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
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
			// Stream completed successfully — cancel the context.
			if m.chat.cancel != nil {
				m.chat.cancel()
			}
			m.chat.loading = false
			m.chat.cancel = nil
			m.chat.canceled = false
			content := m.chat.streamBuffer
			m.chat.streamBuffer = ""
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
	}
	if keyPress, ok := message.(tea.KeyPressMsg); ok {
		if m.chat.loading && m.chat.chatMode != formModeInsert && keyPress.Key().Code == tea.KeyEscape {
			if m.chat.cancel != nil {
				m.chat.cancel()
				m.chat.cancel = nil
				m.chat.canceled = true
			}
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
			m.chat.chatMode = formModeNormal
			m.chat.input.Blur()
			return m, nil
		}
		if keyPress.Key().Code == tea.KeyEnter {
			return m, m.startChat()
		}
	}

	if m.chat.chatMode == formModeInsert {
		input, command := m.chat.input.Update(message)
		m.chat.input = input
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
