package workbench

import (
	"strings"

	"charm.land/bubbles/v2/cursor"
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
	m.refreshChatView()
	messages := append([]ai.Message(nil), m.chat.messages...)
	client, history, conversationID := m.chat.client, m.chat.history, m.chat.conversationID
	contextText := m.chatContext()
	return func() tea.Msg {
		if history != nil {
			if conversationID == "" {
				conversation, err := history.NewConversation(m.appContext, truncateChatTitle(prompt))
				if err != nil {
					return chatResponseMsg{err: err}
				}
				conversationID = conversation.ID
			}
			if err := history.AppendMessage(m.appContext, conversationID, userMessage); err != nil {
				return chatResponseMsg{err: err}
			}
		}
		response, err := client.Chat(m.appContext, ai.Request{AgentID: client.AgentForPrompt(prompt), Messages: messages, Context: contextText})
		if err != nil {
			return chatResponseMsg{conversationID: conversationID, err: err}
		}
		if history != nil {
			message := ai.Message{Role: ai.RoleAssistant, Agent: response.Agent, Content: response.Content}
			if err := history.AppendMessage(m.appContext, conversationID, message); err != nil {
				return chatResponseMsg{conversationID: conversationID, err: err}
			}
		}
		return chatResponseMsg{conversationID: conversationID, response: response}
	}
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
	case chatResponseMsg:
		m.chat.loading = false
		if message.conversationID != "" {
			m.chat.conversationID = message.conversationID
		}
		if message.err != nil {
			m.Status = safeText("AI: " + message.err.Error())
			return m, nil
		}
		m.chat.messages = append(m.chat.messages, ai.Message{Role: ai.RoleAssistant, Agent: message.response.Agent, Content: message.response.Content})
		m.refreshChatView()
		return m, nil
	}
	if keyPress, ok := message.(tea.KeyPressMsg); ok {
		switch {
		case m.keybindings.Match(keyPress, "chat.new", []scope{scopeView}):
			m.newChatConversation()
			return m, nil
		case m.keybindings.Match(keyPress, "chat.history", []scope{scopeView}):
			return m, m.loadChatHistory()
		case m.keybindings.Match(keyPress, "chat.delete", []scope{scopeView}):
			return m, m.deleteChatHistory(false)
		case m.keybindings.Match(keyPress, "chat.clear", []scope{scopeView}):
			return m, m.deleteChatHistory(true)
		case m.keybindings.Match(keyPress, "chat.apply_sql", []scope{scopeView}):
			m.applyChatSQL()
			return m, nil
		case keyPress.Key().Code == tea.KeyEnter:
			return m, m.startChat()
		case keyPress.Key().Code == tea.KeyPgUp:
			m.chat.viewport.PageUp()
			return m, nil
		case keyPress.Key().Code == tea.KeyPgDown:
			m.chat.viewport.PageDown()
			return m, nil
		}
	}
	if _, ok := message.(cursor.BlinkMsg); ok || m.chat.input.Focused() {
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
