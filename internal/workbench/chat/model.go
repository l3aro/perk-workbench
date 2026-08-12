// Package chat owns the AI assistant feature: the chat pane state (input,
// viewport, runs, tool rounds, slash completions, prompt history),
// rendering, context building, and read-only tool execution against the
// session service. The root shell supplies an immutable context snapshot
// (connection scope, database info, schema, query, results) before every
// update and applies the component's request events; write confirmations
// and interactive query execution stay root-owned, and root replies with
// the exported chat messages the component consumes.
package chat

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Client is the AI provider the assistant talks to. The root injects it
// through SetAI; the component never touches the provider package's
// internals beyond this interface.
type Client interface {
	AgentForPrompt(string) string
	Chat(context.Context, ai.Request) (ai.Response, error)
	Complete(context.Context, ai.Request) (ai.Response, error)
	ChatStream(context.Context, ai.Request) (<-chan ai.StreamEvent, error)
	SupportsTools(string) bool
	GenerateTitle(context.Context, string) (string, error)
}

// Executor is the root-implemented read-only database boundary: the
// component requests tool queries through it, and the root owns the
// service call. The write path stays event-based (ConfirmationRequested
// plus root execution).
type Executor interface {
	ExecuteReadOnly(ctx context.Context, statement string) (sharedsql.Result, error)
}

// History persists conversations per connection scope. The component
// reads and writes it through this interface only.
type History interface {
	NewConversation(context.Context, string, string) (ai.Conversation, error)
	AppendMessage(context.Context, string, string, ai.Message) error
	Conversations(context.Context, string) ([]ai.Conversation, error)
	Messages(context.Context, string, string) ([]ai.Message, error)
	DeleteConversation(context.Context, string, string) error
	Clear(context.Context, string) error
	RenameConversation(context.Context, string, string, string) error
}

// Context is the immutable snapshot the root hands to the component for
// one update: the connection scope, the database product/version, the
// schema objects, the current SQL editor value, and the visible results.
// The component never reaches into root state beyond this snapshot.
type Context struct {
	ConnectionID string
	Database     sharedsql.DatabaseInfo
	Schema       []sharedsql.SchemaObject
	Query        string
	Results      sharedsql.Result
}

// Mode is the chat pane's modal input mode (insert/normal), mirroring the
// root's form-mode semantics for the chat input only.
type Mode uint8

const (
	ModeNormal Mode = iota
	ModeInsert
)

// CompletionItem is one slash-command suggestion.
type CompletionItem struct {
	Label      string
	InsertText string
	Detail     string
	Kind       string // display category, e.g. "command"
}

// Completion is the slash-command suggestion dropdown while typing.
type Completion struct {
	Items    []CompletionItem
	Matches  []CompletionItem
	Prefix   string
	Selected int
}

// NewCompletion builds a completion list from command items.
func NewCompletion(items []CompletionItem) Completion {
	for i := range items {
		if items[i].InsertText == "" {
			items[i].InsertText = items[i].Label
		}
	}
	return Completion{Items: items}
}

// Filter narrows matches to those with a case-insensitive prefix match.
func (c *Completion) Filter(prefix string) {
	c.Prefix = prefix
	lower := strings.ToLower(prefix)
	c.Matches = c.Matches[:0]
	for _, item := range c.Items {
		if strings.HasPrefix(strings.ToLower(item.Label), lower) {
			c.Matches = append(c.Matches, item)
		}
	}
	c.Selected = 0
}

// Accept returns the selected match.
func (c Completion) Accept() CompletionItem {
	if len(c.Matches) == 0 {
		return CompletionItem{}
	}
	return c.Matches[c.Selected]
}

// Move shifts the selection by delta, wrapping.
func (c *Completion) Move(delta int) {
	if len(c.Matches) == 0 {
		return
	}
	c.Selected = (c.Selected + delta + len(c.Matches)) % len(c.Matches)
}

// Dismiss closes the dropdown but remembers the prefix.
func (c *Completion) Dismiss() {
	c.Matches = c.Matches[:0]
	c.Selected = 0
}

// Visible reports whether the dropdown has matches.
func (c Completion) Visible() bool { return len(c.Matches) > 0 }

// Model is the chat feature component: the input, the message viewport,
// the conversation runs, tool-round state, and the pane interactions.
type Model struct {
	Input    textarea.Model
	Viewport viewport.Model
	Client   Client
	History  History
	ActiveID string // conversation shown; "" is the fresh unsent view
	Runs     map[string]*Run
	NextGen  int64 // globally unique turn ids across concurrent runs
	LoadSeq  int64 // bumps on each /history selection; drops stale loads
	Enabled  bool
	Visible  bool

	ShareResults  bool
	HistoryChoice string
	ChatMode      Mode
	glamour       *glamour.TermRenderer
	Completion    Completion // slash-command suggestions while typing
	// HistoryPicker is the /history conversation picker overlay.
	HistoryPicker *huh.Form
	// KeepInsert keeps the chat input in insert mode after a release
	// click inside the chat pane.
	KeepInsert bool

	// PromptHistory is the newest-first list of accepted user prompts for
	// this process; HistoryIndex == -1 means not browsing recall.
	PromptHistory []string
	HistoryIndex  int

	// YoloWrites: when true, sql_write executes without per-statement modal.
	YoloWrites bool

	// AppContext is the root application context run commands derive
	// from, so quitting cancels in-flight streams and history writes.
	AppContext context.Context

	// Session state the root keeps current (the chat-scoped mirror of the
	// connection): the executor read-only tools query through, the
	// read-only flag (gates the sql_write tool), the opened target
	// (connection info), and the last failed statement the assistant
	// context summarizes.
	Executor        Executor
	ReadOnly        bool
	Target          string
	LastFailedQuery string
	LastFailedError string
}

// Run is the full state of one conversation — the active one or any
// conversation still executing in the background. Runs are independent:
// switching the visible conversation never interrupts another run.
// ConnectionID is the profile scope captured when the turn began; a run
// keeps persisting to it even if the model later disconnects.
type Run struct {
	ConversationID string
	ConnectionID   string
	Messages       []ai.Message
	// BlockCache holds rendered viewport blocks per message so
	// RefreshView re-renders only appended/replaced messages instead of
	// the whole conversation on every stream delta. StreamBlock caches
	// the streaming tail so unchanged buffers are not re-rendered.
	BlockCache   []Block
	CachedWidth  int
	StreamBlock  string
	StreamSource string
	StreamBuffer string // accumulated streaming content
	Loading      bool
	Canceled     bool
	Cancel       context.CancelFunc
	Gen          int64 // turn id; checked on async completions
	SpinnerFrame int   // progress spinner frame while loading
	RoundState   *ToolRoundState
	PendingWrite *PendingWrite
}

// Block is one cached rendered viewport block for a chat message.
type Block struct {
	Content string // source message content the block was rendered from
	Block   string
}

// PendingWrite holds state for a sql_write call awaiting user
// confirmation. The dialog itself is a root overlay; the component
// reports the request through ConfirmationRequested and waits for the
// write result message.
type PendingWrite struct {
	Generation int64
	Call       ai.ToolCall
	Statement  string
}

// ToolRoundState holds resumable state for one assistant run with tools.
// It is set by StartChat and cleared when the run ends.
type ToolRoundState struct {
	Gen                     int64
	Messages                []ai.Message
	AgentID                 string
	Client                  Client
	History                 History
	RootContext             context.Context
	ChatContext             context.Context
	Cancel                  context.CancelFunc
	ToolCancel              context.CancelFunc
	FinalizationCancel      context.CancelFunc
	ContextText             string
	ToolsDefs               []ai.ToolDefinition
	ConversationID          string
	ToolCalls               []ai.ToolCall
	NextCall                int
	ToolCallCount           int
	ToolDeadline            time.Time
	Finalizing              bool
	LastToolResultSignature string
	RepeatedToolResults     int
}

// ActiveRun returns the run backing the visible conversation, creating an
// empty run for the fresh (unsent) view when needed.
func (cm *Model) ActiveRun() *Run {
	run := cm.Runs[cm.ActiveID]
	if run == nil {
		run = &Run{ConversationID: cm.ActiveID}
		cm.Runs[cm.ActiveID] = run
	}
	return run
}

// RunByGen finds the run that owns the given turn id.
func (cm *Model) RunByGen(gen int64) *Run {
	for _, run := range cm.Runs {
		if run.Gen == gen {
			return run
		}
	}
	return nil
}

// IsActive reports whether run backs the visible conversation.
func (cm *Model) IsActive(run *Run) bool {
	return cm.ActiveID == run.ConversationID
}

// InsertMode reports whether the chat input is in insert mode (the root
// needs it for the write-confirmation escape precedence).
func (cm Model) InsertMode() bool { return cm.ChatMode == ModeInsert }

// ExitInsertMode leaves insert mode and blurs the input.
func (cm *Model) ExitInsertMode() {
	cm.ChatMode = ModeNormal
	cm.Input.Blur()
}

// SetAI configures the provider and history store and enables the pane.
func (cm *Model) SetAI(client Client, history History) {
	cm.Client = client
	cm.History = history
	cm.Enabled = client != nil
	cm.Visible = cm.Enabled
}

// SetContext records the root application context run commands derive
// from. The root calls it once at construction.
func (cm *Model) SetContext(ctx context.Context) {
	cm.AppContext = ctx
}

// baseContext returns the application context, falling back to
// context.Background() before the root wires one in.
func (cm Model) baseContext() context.Context {
	if cm.AppContext != nil {
		return cm.AppContext
	}
	return context.Background()
}

// New builds the chat component with an empty conversation map.
func New() Model {
	input := textarea.New()
	input.Placeholder = "Ask about this database..."
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.KeyMap.InsertNewline.SetEnabled(false)
	styles := input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(uikit.ColorInk))
	styles.Blurred.Text = styles.Focused.Text
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)
	input.Blur()

	viewport := viewport.New()
	viewport.SoftWrap = true
	viewport.FillHeight = true
	viewport.MouseWheelEnabled = true
	return Model{Input: input, Viewport: viewport, Runs: map[string]*Run{}, NextGen: 1, ChatMode: ModeNormal, HistoryIndex: -1}
}

// RecordPromptHistory stores an accepted user prompt, newest first, capped
// at the shared limit, and exits any active recall.
func (cm *Model) RecordPromptHistory(prompt string) {
	cm.PromptHistory = append([]string{prompt}, cm.PromptHistory...)
	if len(cm.PromptHistory) > queryLogLimit {
		cm.PromptHistory = cm.PromptHistory[:queryLogLimit]
	}
	cm.HistoryIndex = -1
}

// queryLogLimit mirrors the query-log entry cap for prompt recall.
const queryLogLimit = 100

// RecallPromptHistory moves up (direction > 0) or down (direction < 0)
// through PromptHistory, mirroring the query editor recall: recall starts
// only at an index of -1, never wraps, and returning to the newest entry
// clears the recalled value.
func (cm *Model) RecallPromptHistory(direction int) (string, bool) {
	if len(cm.PromptHistory) == 0 {
		return "", false
	}
	if direction > 0 {
		if cm.HistoryIndex == -1 {
			cm.HistoryIndex = 0
		} else if cm.HistoryIndex < len(cm.PromptHistory)-1 {
			cm.HistoryIndex++
		} else {
			return "", false // already at the oldest entry; never wrap
		}
	} else {
		if cm.HistoryIndex <= 0 {
			if cm.HistoryIndex == -1 {
				return "", false // Down outside recall mode
			}
			cm.HistoryIndex = -1
		} else {
			cm.HistoryIndex--
		}
	}
	if cm.HistoryIndex == -1 {
		return "", true
	}
	return cm.PromptHistory[cm.HistoryIndex], true
}

// NewConversation cancels and drops the fresh (unsent) run and resets the
// visible conversation to the fresh view.
func (cm *Model) NewConversation() {
	if run := cm.Runs[""]; run != nil {
		if run.Cancel != nil {
			run.Cancel()
		}
		delete(cm.Runs, "")
	}
	cm.ActiveID = ""
	cm.Input.Reset()
	cm.Completion = Completion{}
	cm.RefreshView()
}

// Reset stops every run and clears all chat state (disconnect path).
func (cm *Model) Reset() {
	for _, run := range cm.Runs {
		if run.RoundState != nil {
			run.RoundState.ReleaseContexts()
		}
		if run.Cancel != nil {
			run.Cancel()
		}
	}
	cm.Runs = map[string]*Run{}
	cm.ActiveID = ""
	cm.PromptHistory = nil
	cm.HistoryIndex = -1
	cm.YoloWrites = false
	cm.Executor = nil
	cm.ReadOnly = false
	cm.Target = ""
	cm.LastFailedQuery = ""
	cm.LastFailedError = ""
	cm.Input.Reset()
	cm.Completion = Completion{}
	cm.RefreshView()
}

// HistoryOptionLabel renders a conversation title for the /history
// picker, prefixing a spinner glyph while that conversation's agent run
// is active. The glyph is static while the picker is open: the picker is
// not rebuilt on every tick.
func HistoryOptionLabel(run *Run, title string) string {
	if run != nil && run.Loading {
		return SpinnerFrames[run.SpinnerFrame%len(SpinnerFrames)] + " " + TruncateTitle(title)
	}
	return TruncateTitle(title)
}

// SpinnerFrames are the braille frames for the assistant progress spinner.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// TruncateTitle caps a conversation title at 60 runes.
func TruncateTitle(prompt string) string {
	runes := []rune(prompt)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}
	return prompt
}

// SQL extracts the assistant's latest fenced SQL statement, newest first.
func SQL(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != ai.RoleAssistant {
			continue
		}
		parts := strings.Split(message.Content, "```")
		for partIndex := len(parts) - 2; partIndex >= 1; partIndex -= 2 {
			block := strings.TrimSpace(parts[partIndex])
			firstLine, statement, found := strings.Cut(block, "\n")
			if found && (strings.EqualFold(firstLine, "sql") || strings.EqualFold(firstLine, "sqlite") || strings.EqualFold(firstLine, "mysql") || strings.EqualFold(firstLine, "postgresql")) {
				return strings.TrimSpace(statement)
			}
		}
	}
	return ""
}

// TruncateContext caps the assistant context at the shared limit.
func TruncateContext(value string) string {
	const limit = 12_000
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n[context truncated]"
}

// ContextText builds the assistant context from the snapshot plus the
// component's session state: database product/version, the last failed
// query, the schema objects, and the current SQL editor value.
func (cm Model) ContextText(ctx Context) string {
	var context strings.Builder
	if ctx.Database.Product != "" {
		context.WriteString("Database: ")
		context.WriteString(ctx.Database.Product)
		if ctx.Database.Version != "" {
			context.WriteString(" ")
			context.WriteString(ctx.Database.Version)
		}
		context.WriteString("\n")
	}
	if cm.LastFailedQuery != "" {
		context.WriteString("Last failed query:\n")
		context.WriteString(cm.LastFailedQuery)
		context.WriteString("\nError:\n")
		context.WriteString(cm.LastFailedError)
		context.WriteString("\n")
	}
	if len(ctx.Schema) > 0 {
		context.WriteString("Schema:\n")
		for _, object := range ctx.Schema {
			context.WriteString(object.Type)
			context.WriteString(" ")
			context.WriteString(object.Database)
			context.WriteString(".")
			context.WriteString(object.Name)
			context.WriteString("\n")
		}
	}
	if query := strings.TrimSpace(ctx.Query); query != "" {
		context.WriteString("Current SQL:\n")
		context.WriteString(query)
		context.WriteString("\n")
	}
	return TruncateContext(context.String())
}

// ResultsContext returns the visible results block for providers without
// tool support; tool-capable providers get get_visible_results instead.
func ResultsContext(ctx Context) string {
	if len(ctx.Results.Rows) == 0 {
		return ""
	}
	var context strings.Builder
	context.WriteString("Visible results:\n")
	for _, row := range ctx.Results.Rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			if cell != nil {
				cells[i] = *cell
			} else {
				cells[i] = "NULL"
			}
		}
		context.WriteString(strings.Join(cells, " | "))
		context.WriteString("\n")
	}
	return context.String()
}
