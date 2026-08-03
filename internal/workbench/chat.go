package workbench

import (
	"context"
	"strings"
	"time"

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
	GenerateTitle(context.Context, string) (string, error)
}

type chatHistory interface {
	NewConversation(context.Context, string, string) (ai.Conversation, error)
	AppendMessage(context.Context, string, string, ai.Message) error
	Conversations(context.Context, string) ([]ai.Conversation, error)
	Messages(context.Context, string, string) ([]ai.Message, error)
	DeleteConversation(context.Context, string, string) error
	Clear(context.Context, string) error
	RenameConversation(context.Context, string, string, string) error
}

type chatModel struct {
	input    textarea.Model
	viewport viewport.Model
	client   chatClient
	history  chatHistory
	activeID string // conversation shown; "" is the fresh unsent view
	runs     map[string]*chatRun
	nextGen  int64 // globally unique turn ids across concurrent runs
	loadSeq  int64 // bumps on each /history selection; drops stale loads
	enabled  bool
	visible  bool

	shareResults  bool
	historyChoice string
	chatMode      formMode
	glamour       *glamour.TermRenderer
	completion    completion // slash-command suggestions while typing

	// promptHistory is the newest-first list of accepted user prompts for
	// this process; historyIndex == -1 means not browsing recall.
	promptHistory []string
	historyIndex  int

	// YOLO writes: when true, sql_write executes without per-statement modal.
	yoloWrites bool
}

// chatRun is the full state of one conversation — the active one or any
// conversation still executing in the background. Runs are independent:
// switching the visible conversation never interrupts another run. connectionID
// is the profile scope captured when the turn began; a run keeps persisting to
// it even if the model later disconnects.
type chatRun struct {
	conversationID string
	connectionID   string
	messages       []ai.Message
	streamBuffer   string // accumulated streaming content
	loading        bool
	canceled       bool
	cancel         context.CancelFunc
	gen            int64 // turn id; checked on async completions
	spinnerFrame   int   // progress spinner frame while loading
	roundState     *toolRoundState
	pendingWrite   *pendingWrite
}

// activeRun returns the run backing the visible conversation, creating an
// empty run for the fresh (unsent) view when needed.
func (cm *chatModel) activeRun() *chatRun {
	run := cm.runs[cm.activeID]
	if run == nil {
		run = &chatRun{conversationID: cm.activeID}
		cm.runs[cm.activeID] = run
	}
	return run
}

// runByGen finds the run that owns the given turn id.
func (cm *chatModel) runByGen(gen int64) *chatRun {
	for _, run := range cm.runs {
		if run.gen == gen {
			return run
		}
	}
	return nil
}

// isActive reports whether run backs the visible conversation.
func (cm *chatModel) isActive(run *chatRun) bool {
	return cm.activeID == run.conversationID
}

type chatResponseMsg struct {
	conversationID string
	response       ai.Response
	err            error
}

// toolRoundState holds resumable state for one assistant run with tools.
// It is set by startChat and cleared when the run ends.
type toolRoundState struct {
	gen                     int64
	messages                []ai.Message
	agentID                 string
	client                  chatClient
	history                 chatHistory
	rootContext             context.Context
	chatContext             context.Context
	cancel                  context.CancelFunc
	toolCancel              context.CancelFunc
	finalizationCancel      context.CancelFunc
	contextText             string
	toolsDefs               []ai.ToolDefinition
	conversationID          string
	toolCalls               []ai.ToolCall
	nextCall                int
	toolCallCount           int
	toolDeadline            time.Time
	finalizing              bool
	lastToolResultSignature string
	repeatedToolResults     int
}

// pendingWrite holds state for a sql_write call awaiting user confirmation.
type pendingWrite struct {
	generation int64
	call       ai.ToolCall
	statement  string
	dialog     *confirmationDialog
}

// assistantToolStartMsg is sent by startChat's closure with the first
// Complete response containing tool calls. updateChat stores the state.
type assistantToolStartMsg struct {
	gen   int64
	state toolRoundState
}

// assistantToolPhaseExpiredMsg switches an expired tool phase to finalization.
// state is provided only when the initial tool request expired.
type assistantToolPhaseExpiredMsg struct {
	gen   int64
	state *toolRoundState
}

// assistantToolContinueMsg signals updateChat to resume tool-round processing.
type assistantToolContinueMsg struct {
	gen int64
}

// assistantToolResultMsg carries the result of an async read-only tool execution.
type assistantToolResultMsg struct {
	gen      int64
	callID   string
	callName string
	content  string
	err      string
}

// assistantWriteResultMsg carries the result of an async sql_write execution.
type assistantWriteResultMsg struct {
	gen      int64
	callID   string
	callName string
	content  string
	err      string
	declined bool // user declined the write; stop round
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
	connectionID  string
	conversations []ai.Conversation
	err           error
}

type chatMessagesLoadedMsg struct {
	connectionID   string
	conversationID string
	messages       []ai.Message
	seq            int64
	err            error
}

type chatHistoryDeletedMsg struct {
	err            error
	connectionID   string
	conversationID string // conversation the delete targeted ("" for clear-all)
	clear          bool
}

// chatHistoryOptionLabel renders a conversation title for the /history
// picker, prefixing a spinner glyph while that conversation's agent run is
// active. The glyph is static while the picker is open: the picker is not
// rebuilt on every tick.
func chatHistoryOptionLabel(run *chatRun, title string) string {
	if run != nil && run.loading {
		return chatSpinnerFrames[run.spinnerFrame%len(chatSpinnerFrames)] + " " + truncateChatTitle(title)
	}
	return truncateChatTitle(title)
}

// chatPersistMsg reports an error persisting an AI message to history.
type chatPersistMsg struct{ err error }

// chatTitleMsg reports completion of asynchronous conversation title generation.
type chatTitleMsg struct {
	connectionID   string
	conversationID string
	title          string
	err            error
}

// chatSpinnerTickMsg advances the assistant progress spinner while loading.
type chatSpinnerTickMsg struct{}

// chatSpinnerFrames are the braille frames for the assistant progress spinner.
var chatSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	return chatModel{input: input, viewport: viewport, runs: map[string]*chatRun{}, nextGen: 1, chatMode: formModeNormal, historyIndex: -1}
}

// recordPromptHistory stores an accepted user prompt, newest first, capped at
// queryLogLimit, and exits any active recall.
func (cm *chatModel) recordPromptHistory(prompt string) {
	cm.promptHistory = append([]string{prompt}, cm.promptHistory...)
	if len(cm.promptHistory) > queryLogLimit {
		cm.promptHistory = cm.promptHistory[:queryLogLimit]
	}
	cm.historyIndex = -1
}

// recallPromptHistory moves up (direction > 0) or down (direction < 0)
// through promptHistory, mirroring recallQueryHistory: recall starts only at
// an index of -1 (caller decides when the input is blank), never wraps, and
// returning to the newest entry clears the recalled value.
func (cm *chatModel) recallPromptHistory(direction int) (string, bool) {
	if len(cm.promptHistory) == 0 {
		return "", false
	}
	if direction > 0 {
		if cm.historyIndex == -1 {
			cm.historyIndex = 0
		} else if cm.historyIndex < len(cm.promptHistory)-1 {
			cm.historyIndex++
		} else {
			return "", false // already at the oldest entry; never wrap
		}
	} else {
		if cm.historyIndex <= 0 {
			if cm.historyIndex == -1 {
				return "", false // Down outside recall mode
			}
			cm.historyIndex = -1
		} else {
			cm.historyIndex--
		}
	}
	if cm.historyIndex == -1 {
		return "", true
	}
	return cm.promptHistory[cm.historyIndex], true
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
	run := m.chat.activeRun()
	blocks := make([]string, 0, len(run.messages)+1)
	for _, message := range run.messages {
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
	if run.loading {
		// Adaptive label: "thinking..." before content, "streaming..." during.
		label := "\u2022 thinking..."
		if run.streamBuffer != "" {
			label = "\u2022 streaming..."
		}
		blocks = append(blocks, thinkingStyle.Render(label))

		if run.streamBuffer != "" {
			content := safeMarkdown(run.streamBuffer)
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
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(colorSecondary)).Bold(true)
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
