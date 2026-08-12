package chat

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
)

// Event is a typed request from the chat component to the root shell.
// The root applies the side effect; the component never touches root
// state.
type Event interface{ isEvent() }

// StatusChanged asks the root to record a status line transition.
type StatusChanged struct{ Text string }

func (StatusChanged) isEvent() {}

// ClipboardRequested asks the root to write text to both clipboards.
type ClipboardRequested struct{ Text string }

func (ClipboardRequested) isEvent() {}

// SQLRequested asks the root to run a statement through its execution
// paths: Source "editor" applies the statement to the SQL editor (the
// assistant's apply-SQL command); other sources run through the
// interactive startQueryStatement path.
type SQLRequested struct {
	Statement string
	ReadOnly  bool
	Source    string
}

func (SQLRequested) isEvent() {}

// SchemaRequested asks the root to refresh the schema sidebar.
type SchemaRequested struct{}

func (SchemaRequested) isEvent() {}

// ConfirmationRequested asks the root to show the write-confirmation
// dialog for one assistant sql_write call. Generation is the turn id the
// root reports back through WriteResultMsg.
type ConfirmationRequested struct {
	Statement  string
	Generation int64
}

func (ConfirmationRequested) isEvent() {}

// StreamMsg is sent for each streaming delta from the AI.
type StreamMsg struct {
	Ch             <-chan ai.StreamEvent
	ConversationID string
	Delta          string
	Response       ai.Response
	Done           bool
	Err            error
}

// ResponseMsg carries a non-streaming completion.
type ResponseMsg struct {
	ConversationID string
	Response       ai.Response
	Err            error
}

// HistoryLoadedMsg carries the /history conversation list.
type HistoryLoadedMsg struct {
	ConnectionID  string
	Conversations []ai.Conversation
	Err           error
}

// MessagesLoadedMsg carries one conversation's messages.
type MessagesLoadedMsg struct {
	ConnectionID   string
	ConversationID string
	Messages       []ai.Message
	Seq            int64
	Err            error
}

// HistoryDeletedMsg reports a conversation delete or clear.
type HistoryDeletedMsg struct {
	Err            error
	ConnectionID   string
	ConversationID string // conversation the delete targeted ("" for clear-all)
	Clear          bool
}

// PersistMsg reports an error persisting an AI message to history.
type PersistMsg struct{ Err error }

// TitleMsg reports completion of asynchronous conversation title
// generation.
type TitleMsg struct {
	ConnectionID   string
	ConversationID string
	Title          string
	Err            error
}

// SpinnerTickMsg advances the assistant progress spinner while loading.
type SpinnerTickMsg struct{}

// ToolStartMsg is sent by StartChat's closure with the first Complete
// response containing tool calls. Update stores the state.
type ToolStartMsg struct {
	Gen   int64
	State ToolRoundState
}

// ToolPhaseExpiredMsg switches an expired tool phase to finalization.
// State is provided only when the initial tool request expired.
type ToolPhaseExpiredMsg struct {
	Gen   int64
	State *ToolRoundState
}

// ToolContinueMsg signals Update to resume tool-round processing.
type ToolContinueMsg struct{ Gen int64 }

// ToolResultMsg carries the result of an async read-only tool execution.
type ToolResultMsg struct {
	Gen      int64
	CallID   string
	CallName string
	Content  string
	Err      string
}

// WriteResultMsg carries the result of an async sql_write execution.
type WriteResultMsg struct {
	Gen      int64
	CallID   string
	CallName string
	Content  string
	Err      string
	Declined bool // user declined the write; stop round
}

// WriteRequest is the root-side record of a pending assistant write
// awaiting confirmation: the statement, the turn id to report back, and
// the tool-phase deadline the execution must honor.
type WriteRequest struct {
	Statement  string
	Generation int64
	Deadline   time.Time
}

// OwnsMessage reports whether msg belongs to the chat feature. The root
// routes every owned message into the component's Update.
func OwnsMessage(msg tea.Msg) bool {
	switch msg.(type) {
	case StreamMsg, ResponseMsg, HistoryLoadedMsg, MessagesLoadedMsg, HistoryDeletedMsg,
		PersistMsg, TitleMsg, SpinnerTickMsg, ToolStartMsg, ToolPhaseExpiredMsg,
		ToolContinueMsg, ToolResultMsg, WriteResultMsg:
		return true
	}
	return false
}

// ReadStreamEvent reads one event from the stream channel and returns it
// as a StreamMsg.
func ReadStreamEvent(ch <-chan ai.StreamEvent, conversationID string) tea.Msg {
	ev, ok := <-ch
	if !ok {
		return StreamMsg{Ch: ch, ConversationID: conversationID, Done: true}
	}
	msg := StreamMsg{Ch: ch, ConversationID: conversationID, Delta: ev.Delta}
	if ev.Response != nil {
		msg.Done = true
		msg.Response = *ev.Response
	}
	if ev.Err != nil {
		msg.Err = ev.Err
	}
	return msg
}

// SpinnerTick re-arms the assistant progress spinner tick.
func SpinnerTick() tea.Cmd {
	return tea.Tick(SpinnerInterval, func(time.Time) tea.Msg { return SpinnerTickMsg{} })
}

// SpinnerInterval is the progress spinner frame period.
const SpinnerInterval = 80 * time.Millisecond

// Phase timeouts and budgets for assistant tool rounds.
const (
	ToolPhaseTimeout        = 2 * time.Minute
	FinalizationTimeout     = 20 * time.Second
	MaxToolCalls            = 64
	RepeatedToolResultLimit = 3
)
