package workbench

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
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
	input.SetHeight(1)
	input.KeyMap.InsertNewline.SetEnabled(false)
	styles := input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk))
	styles.Blurred.Text = styles.Focused.Text
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
	viewportHeight := height - 4
	if m.compact {
		viewportHeight -= 2
	}
	m.chat.viewport.SetHeight(max(viewportHeight, 1))
	m.chat.initGlamour(width)
	m.refreshChatView()
}

func (m *Model) refreshChatView() {
	blocks := make([]string, 0, len(m.chat.messages)+1)
	for _, message := range m.chat.messages {
		var block string
		var content string
		if message.Role == ai.RoleAssistant {
			content = safeMarkdown(message.Content)
		} else {
			content = safeText(message.Content)
		}
		if message.Role == ai.RoleAssistant && m.chat.glamour != nil {
			content = m.renderChatContent(content)
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
			content := safeMarkdown(m.chat.streamBuffer)
			if m.chat.glamour != nil {
				content = m.renderChatContent(content)
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

// renderChatContent renders assistant message content. Non-table markdown goes
// through glamour; GFM table blocks are rendered with lipgloss/v2/table for
// proper column alignment within the chat viewport width.
func (m *Model) renderChatContent(content string) string {
	if m.chat.glamour == nil || !strings.Contains(content, "\n|") && !strings.HasPrefix(content, "|") {
		// No table likely — use glamour directly.
		if m.chat.glamour != nil {
			if rendered, err := m.chat.glamour.Render(content); err == nil {
				return strings.TrimRight(rendered, "\n")
			}
		}
		return content
	}

	// Split by blank lines to find table paragraphs.
	paragraphs := strings.Split(content, "\n\n")
	width := m.chat.viewport.Width()
	if width < 1 {
		width = 1
	}
	var out strings.Builder

	for i, para := range paragraphs {
		if i > 0 {
			out.WriteString("\n\n")
		}

		lines := strings.Split(para, "\n")
		if isGFMTable(lines) {
			out.WriteString(renderTableLines(lines, width))
		} else if m.chat.glamour != nil {
			if rendered, err := m.chat.glamour.Render(para); err == nil {
				out.WriteString(strings.TrimRight(rendered, "\n"))
			} else {
				out.WriteString(para)
			}
		} else {
			out.WriteString(para)
		}
	}
	return out.String()
}

// isGFMTable checks whether a set of lines forms a GFM table block.
func isGFMTable(lines []string) bool {
	if len(lines) < 3 {
		return false
	}
	// All lines must be non-empty and start with '|' (possibly after whitespace).
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed == "" || trimmed[0] != '|' {
			return false
		}
	}
	// Second line must be a separator: |[-: ]+|
	sep := strings.TrimSpace(lines[1])
	withoutPipes := strings.ReplaceAll(sep, "|", "")
	if len(withoutPipes) == 0 {
		return false
	}
	for _, r := range withoutPipes {
		if r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return true
}

// renderTableLines renders a parsed GFM table with lipgloss/v2/table.
func renderTableLines(lines []string, width int) string {
	headCells := parseTableRow(lines[0])
	tbl := table.New().
		Width(width).
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true)
			}
			return lipgloss.NewStyle()
		})

	tbl.Headers(headCells...)
	for _, line := range lines[2:] {
		cells := parseTableRow(line)
		tbl.Row(cells...)
	}

	rendered := tbl.Render()
	return rendered
}

// parseTableRow splits a GFM table row line into cells, stripping outer pipes
// and trimming whitespace.
func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	// Remove leading and trailing pipe.
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}
