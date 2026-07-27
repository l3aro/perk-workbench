package workbench

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
)

type chatClient interface {
	AgentForPrompt(string) string
	Chat(context.Context, ai.Request) (ai.Response, error)
	Complete(context.Context, ai.Request) (ai.Response, error)
	ChatStream(context.Context, ai.Request) (<-chan ai.StreamEvent, error)
	SupportsTools(string) bool
}

type chatHistory interface {
	NewConversation(context.Context, string) (ai.Conversation, error)
	AppendMessage(context.Context, string, ai.Message) error
	Conversations(context.Context) ([]ai.Conversation, error)
	Messages(context.Context, string) ([]ai.Message, error)
	DeleteConversation(context.Context, string) error
	Clear(context.Context) error
}

type chatModel struct {
	input          textarea.Model
	viewport       viewport.Model
	messages       []ai.Message
	client         chatClient
	history        chatHistory
	conversationID string
	cancel         context.CancelFunc
	loading        bool
	canceled       bool
	enabled        bool
	visible        bool
	shareResults   bool
	historyChoice  string
	chatMode       formMode
	glamour        *glamour.TermRenderer
	streamBuffer   string // accumulated streaming content
}

type chatResponseMsg struct {
	conversationID string
	response       ai.Response
	err            error
}

// chatStreamMsg is sent for each streaming delta from the AI.
type chatStreamMsg struct {
	ch             <-chan ai.StreamEvent
	conversationID string
	delta          string
	response       ai.Response
	done           bool
	err            error
}

type chatHistoryLoadedMsg struct {
	conversations []ai.Conversation
	err           error
}

type chatMessagesLoadedMsg struct {
	conversationID string
	messages       []ai.Message
	err            error
}

type chatHistoryDeletedMsg struct{ err error }

// chatPersistMsg reports an error persisting an AI message to history.
type chatPersistMsg struct{ err error }

func newChatModel() chatModel {
	input := textarea.New()
	input.Placeholder = "Ask about this database..."
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.SetVirtualCursor(false)
	input.SetHeight(1)
	input.KeyMap.InsertNewline.SetEnabled(false)
	styles := input.Styles()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)
	input.Blur()

	viewport := viewport.New()
	viewport.SoftWrap = true
	viewport.FillHeight = true
	viewport.MouseWheelEnabled = true
	return chatModel{input: input, viewport: viewport, chatMode: formModeNormal}
}

func (m *Model) SetAI(client chatClient, history chatHistory) {
	m.chat.client = client
	m.chat.history = history
	m.chat.enabled = client != nil
	m.chat.visible = m.chat.enabled
}

func (m *Model) toggleAI() {
	if !m.chat.enabled {
		return
	}
	m.chat.visible = !m.chat.visible
	if !m.chat.visible && m.Focus == focusChat {
		m.Focus = focusWorkspace
		m.focusActiveTable()
	}
	m.layout(m.width, m.height)
}

func (m *Model) resizeChat() {
	width := max(m.chatWidth-6, 1)
	height := max(m.height-4, 1)
	if m.compact {
		width = max(m.width-6, 1)
	}
	m.chat.input.SetWidth(width)
	m.chat.input.SetHeight(1)
	m.chat.viewport.SetWidth(width)
	m.chat.viewport.SetHeight(max(height-5, 1))
	m.chat.initGlamour(width)
	m.refreshChatView()
}

func (m *Model) refreshChatView() {
	blocks := make([]string, 0, len(m.chat.messages)+1)
	for _, message := range m.chat.messages {
		var block string
		content := safeText(message.Content)
		if message.Role == ai.RoleAssistant && m.chat.glamour != nil {
			if rendered, err := m.chat.glamour.Render(content); err == nil {
				content = strings.TrimRight(rendered, "\n")
			}
		}
		if message.Role == ai.RoleUser {
			contentWidth := max(m.chat.viewport.Width()-2, 1)
			lines := strings.Split(lipgloss.Wrap(content, contentWidth, ""), "\n")
			for i, line := range lines {
				line = " " + line + strings.Repeat("\u00a0", max(contentWidth-lipgloss.Width(line), 0))
				lines[i] = userMessageAccentStyle.Render("\u258c") + userMessageStyle.Render(line)
			}
			block += strings.Join(lines, "\n")
		} else {
			block += content
		}
		blocks = append(blocks, block)
	}

	// Append streaming content as the last assistant message.
	if m.chat.loading {
		// Adaptive label: "thinking..." before content, "streaming..." during.
		label := "\u2022 thinking..."
		if m.chat.streamBuffer != "" {
			label = "\u2022 streaming..."
		}
		blocks = append(blocks, thinkingStyle.Render(label))

		if m.chat.streamBuffer != "" {
			content := safeText(m.chat.streamBuffer)
			if m.chat.glamour != nil {
				if rendered, err := m.chat.glamour.Render(content); err == nil {
					content = strings.TrimRight(rendered, "\n")
				}
			}
			blocks = append(blocks, content)
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, statusStyle.Render("Ask about the selected database, query, or results."))
	}
	m.chat.viewport.SetContent(strings.Join(blocks, "\n\n"))
	m.chat.viewport.GotoBottom()
}

func (cm *chatModel) initGlamour(width int) {
	if width < 1 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		cm.glamour = nil
		return
	}
	cm.glamour = r
}
