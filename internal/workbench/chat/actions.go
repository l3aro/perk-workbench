package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// StartChat begins an assistant turn for the current input value. It
// returns the batch of commands that stream the response; status
// transitions surface as StatusChanged events.
func (cm *Model) StartChat(ctx Context) (Model, Event, tea.Cmd) {
	prompt := strings.TrimSpace(cm.Input.Value())
	if prompt == "" {
		return *cm, nil, nil
	}
	switch strings.ToLower(prompt) {
	case "/new":
		cm.NewConversation()
		return *cm, StatusChanged{Text: "new conversation"}, nil
	case "/history":
		return *cm, nil, cm.LoadHistory(ctx)
	case "/yolo-on":
		cm.YoloWrites = true
		cm.Input.Reset()
		cm.Completion = Completion{}
		return *cm, StatusChanged{Text: "AI writes: on"}, nil
	case "/yolo-off":
		cm.YoloWrites = false
		cm.Input.Reset()
		cm.Completion = Completion{}
		return *cm, StatusChanged{Text: "AI writes: off"}, nil
	case "/share-results":
		cm.ShareResults = true
		cm.Input.Reset()
		cm.Completion = Completion{}
		return *cm, StatusChanged{Text: "AI result sharing: on"}, nil
	case "/unshare-results":
		cm.ShareResults = false
		cm.Input.Reset()
		cm.Completion = Completion{}
		return *cm, StatusChanged{Text: "AI result sharing: off"}, nil
	}
	if cm.Client == nil {
		return *cm, StatusChanged{Text: "AI is not configured"}, nil
	}
	// A chat already running in the visible conversation swallows new
	// input; other conversations keep running independently in the
	// background.
	if cm.ActiveRun().Loading {
		return *cm, nil, nil
	}

	userMessage := ai.Message{Role: ai.RoleUser, Content: prompt}
	conversationID := cm.ActiveID
	fresh := conversationID == ""
	// Capture the connection scope for the whole turn: a background run
	// keeps persisting to the connection it started on after a disconnect.
	connectionID := ctx.ConnectionID
	// History persistence is connection-scoped: without a profile scope
	// there is nothing safe to write, so the chat stays usable in memory
	// only.
	historyAvailable := cm.History != nil && connectionID != ""
	var notice Event
	if cm.History != nil && !historyAvailable {
		cm.History = nil // in-memory only: nothing safe to persist
		notice = StatusChanged{Text: "AI conversation history is unavailable"}
	}
	if fresh && historyAvailable {
		conversation, err := cm.History.NewConversation(context.Background(), connectionID, TruncateTitle(prompt))
		if err != nil {
			return *cm, StatusChanged{Text: uikit.SafeText("AI history: " + err.Error())}, nil
		}
		conversationID = conversation.ID
		cm.ActiveID = conversationID
	}
	run := cm.Runs[conversationID]
	if run == nil {
		run = &Run{ConversationID: conversationID}
		cm.Runs[conversationID] = run
	}
	run.ConnectionID = connectionID
	// Record only once the prompt is accepted: a failed conversation
	// creation must not pollute recall history.
	cm.RecordPromptHistory(prompt)
	run.Messages = append(run.Messages, userMessage)
	cm.Input.Reset()
	cm.Completion = Completion{}
	run.Loading = true
	run.Canceled = false
	run.StreamBuffer = ""
	run.Gen = cm.NextGen
	cm.NextGen++
	run.PendingWrite = nil
	run.RoundState = nil
	run.resetRenderCache()
	run.resetStreamCache()
	cm.RefreshView()

	gen := run.Gen
	client, history := cm.Client, cm.History
	agentID := client.AgentForPrompt(prompt)
	toolsDefs := cm.DatabaseTools(ctx)
	baseMessages := append([]ai.Message(nil), run.Messages...)

	rootContext, cancel := context.WithCancel(cm.baseContext())
	run.Cancel = cancel

	contextText := cm.ContextText(ctx)

	send := func() tea.Msg {
		if historyAvailable {
			if err := history.AppendMessage(rootContext, connectionID, conversationID, userMessage); err != nil {
				cancel()
				return StreamMsg{ConversationID: conversationID, Err: err}
			}
		}

		supportTools := len(toolsDefs) > 0 && client.SupportsTools(agentID)

		if !supportTools {
			contextText += ResultsContext(ctx)
			contextText = TruncateContext(contextText)
			eventCh, err := client.ChatStream(rootContext, ai.Request{
				AgentID:  agentID,
				Messages: baseMessages,
				Context:  contextText,
			})
			if err != nil {
				cancel()
				return StreamMsg{ConversationID: conversationID, Err: err}
			}
			return ReadStreamEvent(eventCh, conversationID)
		}
		toolDeadline := time.Now().Add(ToolPhaseTimeout)
		toolContext, toolCancel := context.WithDeadline(rootContext, toolDeadline)
		toolState := ToolRoundState{
			Gen:            gen,
			Messages:       baseMessages,
			AgentID:        agentID,
			Client:         client,
			History:        history,
			RootContext:    rootContext,
			ChatContext:    toolContext,
			Cancel:         cancel,
			ToolCancel:     toolCancel,
			ContextText:    contextText,
			ToolsDefs:      toolsDefs,
			ConversationID: conversationID,
			ToolDeadline:   toolDeadline,
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
				return ToolPhaseExpiredMsg{Gen: gen, State: &toolState}
			}
			cancel()
			return StreamMsg{ConversationID: conversationID, Err: err}
		}

		if len(turn.ToolCalls) == 0 {
			// No tool calls — deliver as final answer.
			ch := make(chan ai.StreamEvent, 2)
			ch <- ai.StreamEvent{Kind: ai.EventDelta, Delta: turn.Content}
			ch <- ai.StreamEvent{Kind: ai.EventDone, Response: &turn}
			close(ch)
			return ReadStreamEvent(ch, conversationID)
		}

		toolState.Messages = append(append([]ai.Message(nil), baseMessages...), ai.Message{
			Role:      ai.RoleAssistant,
			Content:   turn.Content,
			ToolCalls: turn.ToolCalls,
		})
		toolState.ToolCalls = turn.ToolCalls
		return ToolStartMsg{Gen: gen, State: toolState}
	}
	// Title generation runs in parallel with the chat request, only for a
	// brand-new conversation, and never blocks the stream on the model
	// call.
	titleCmd := func() tea.Msg {
		title, err := client.GenerateTitle(rootContext, prompt)
		if err != nil {
			return TitleMsg{ConnectionID: connectionID, ConversationID: conversationID, Err: err}
		}
		if title == "" {
			return TitleMsg{ConnectionID: connectionID, ConversationID: conversationID}
		}
		if err := history.RenameConversation(rootContext, connectionID, conversationID, title); err != nil {
			return TitleMsg{ConnectionID: connectionID, ConversationID: conversationID, Err: err}
		}
		return TitleMsg{ConnectionID: connectionID, ConversationID: conversationID, Title: title}
	}
	cmds := []tea.Cmd{send, SpinnerTick()}
	if fresh && historyAvailable {
		cmds = append(cmds, titleCmd)
	}
	return *cm, notice, tea.Batch(cmds...)
}

// LoadHistory loads the /history conversation list.
func (cm *Model) LoadHistory(ctx Context) tea.Cmd {
	if cm.History == nil {
		return nil
	}
	history := cm.History
	connectionID := ctx.ConnectionID
	return func() tea.Msg {
		conversations, err := history.Conversations(cm.baseContext(), connectionID)
		return HistoryLoadedMsg{ConnectionID: connectionID, Conversations: conversations, Err: err}
	}
}

// LoadMessages loads one conversation's messages and selects it.
func (cm *Model) LoadMessages(ctx Context, conversationID string) tea.Cmd {
	history := cm.History
	connectionID := ctx.ConnectionID
	cm.LoadSeq++
	seq := cm.LoadSeq
	return func() tea.Msg {
		messages, err := history.Messages(cm.baseContext(), connectionID, conversationID)
		return MessagesLoadedMsg{ConnectionID: connectionID, ConversationID: conversationID, Messages: messages, Seq: seq, Err: err}
	}
}

// DeleteHistory deletes the visible conversation, or clears every
// conversation when clear is set.
func (cm *Model) DeleteHistory(ctx Context, clear bool) tea.Cmd {
	if cm.History == nil {
		return nil
	}
	history, conversationID := cm.History, cm.ActiveID
	connectionID := ctx.ConnectionID
	return func() tea.Msg {
		var err error
		if clear {
			err = history.Clear(cm.baseContext(), connectionID)
		} else if conversationID != "" {
			err = history.DeleteConversation(cm.baseContext(), connectionID, conversationID)
		}
		return HistoryDeletedMsg{Err: err, ConnectionID: connectionID, ConversationID: conversationID, Clear: clear}
	}
}

// ReleaseContexts cancels the tool-phase and finalization contexts.
func (rs *ToolRoundState) ReleaseContexts() {
	if rs.ToolCancel != nil {
		rs.ToolCancel()
	}
	if rs.FinalizationCancel != nil {
		rs.FinalizationCancel()
	}
}

// RecordToolResult tracks repeated identical tool results and reports
// whether the repetition limit was reached.
func (rs *ToolRoundState) RecordToolResult(call ai.ToolCall, content string) bool {
	input, err := json.Marshal(call.Input)
	if err != nil {
		rs.LastToolResultSignature = ""
		rs.RepeatedToolResults = 0
		return false
	}
	signature := call.Name + "\x00" + string(input) + "\x00" + content
	if signature == rs.LastToolResultSignature {
		rs.RepeatedToolResults++
	} else {
		rs.LastToolResultSignature = signature
		rs.RepeatedToolResults = 1
	}
	return rs.RepeatedToolResults >= RepeatedToolResultLimit
}

// SkipRemainingToolCalls appends skip results for the calls left in the
// round (finalization path).
func (rs *ToolRoundState) SkipRemainingToolCalls(reason string) {
	for rs.NextCall < len(rs.ToolCalls) {
		call := rs.ToolCalls[rs.NextCall]
		rs.Messages = append(rs.Messages, ai.Message{
			Role: ai.RoleTool, ToolID: call.ID, ToolName: call.Name, Content: "Skipped: " + reason,
		})
		rs.NextCall++
	}
}

// ProcessNextToolCall executes the next pending tool call in the current
// round. sql_write with YOLO off emits ConfirmationRequested; read-only
// calls execute against the session service; sql_write executes through
// the root-owned write path (SQLRequested/ConfirmationRequested) and the
// result arrives as WriteResultMsg.
func (cm *Model) ProcessNextToolCall(ctx Context, run *Run) (Model, Event, tea.Cmd) {
	rs := run.RoundState
	if rs == nil {
		return *cm, nil, nil
	}

	if rs.NextCall >= len(rs.ToolCalls) {
		return cm.startNextToolRound(ctx, run)
	}

	if !rs.Finalizing && time.Now().After(rs.ToolDeadline) {
		return cm.startToolFinalization(ctx, run, "tool time budget reached")
	}
	if !rs.Finalizing && rs.ToolCallCount >= MaxToolCalls {
		return cm.startToolFinalization(ctx, run, "tool-call budget reached")
	}
	rs.ToolCallCount++

	call := rs.ToolCalls[rs.NextCall]

	switch call.Name {
	case "sql_write":
		query, _ := call.Input["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			rs.NextCall++
			content := "Error: query argument is required"
			rs.Messages = append(rs.Messages, ai.Message{
				Role: ai.RoleTool, ToolID: call.ID, ToolName: call.Name, Content: content,
			})
			run.Messages = rs.Messages
			if rs.RecordToolResult(call, content) {
				return cm.startToolFinalization(ctx, run, "repeated tool result")
			}
			return *cm, nil, func() tea.Msg { return ToolContinueMsg{Gen: rs.Gen} }
		}
		// The root executes every write (YOLO skips the dialog there); the
		// pending record routes the WriteResultMsg back to this call.
		run.PendingWrite = &PendingWrite{
			Generation: rs.Gen,
			Call:       call,
			Statement:  query,
		}
		return *cm, ConfirmationRequested{Statement: query, Generation: rs.Gen}, nil

	default:
		chatCtx := rs.ChatContext
		gen := rs.Gen
		callID := call.ID
		callName := call.Name
		snapshot := ctx
		return *cm, nil, func() tea.Msg {
			result := cm.ExecuteTool(chatCtx, call, snapshot)
			content := result.Content
			errStr := ""
			if result.Error != "" {
				errStr = result.Error
			}
			return ToolResultMsg{Gen: gen, CallID: callID, CallName: callName, Content: content, Err: errStr}
		}
	}
}

// HandleWriteResult processes the outcome of an async sql_write
// execution (YOLO or confirmed).
func (cm *Model) HandleWriteResult(ctx Context, run *Run, msg WriteResultMsg) (Model, Event, tea.Cmd) {
	rs := run.RoundState
	if rs == nil || msg.Gen != rs.Gen {
		return *cm, nil, nil
	}

	// The call info comes from the pending write (confirmation path) or
	// the current tool call (YOLO path).
	callID, callName := msg.CallID, msg.CallName
	if callID == "" && run.PendingWrite != nil {
		callID, callName = run.PendingWrite.Call.ID, run.PendingWrite.Call.Name
	}
	run.PendingWrite = nil

	if msg.Declined {
		// User declined — append the result and end this run explicitly.
		rs.Messages = append(rs.Messages, ai.Message{
			Role: ai.RoleTool, ToolID: callID, ToolName: callName,
			Content: "Error: " + msg.Err,
		})
		run.Messages = rs.Messages
		return cm.stopToolRound(ctx, run, "Assistant write canceled")
	}

	content := msg.Content
	if msg.Err != "" {
		content = "Error: " + msg.Err
	}
	call := rs.ToolCalls[rs.NextCall]
	rs.Messages = append(rs.Messages, ai.Message{
		Role: ai.RoleTool, ToolID: callID, ToolName: callName, Content: content,
	})
	rs.NextCall++
	run.Messages = rs.Messages
	if rs.RecordToolResult(call, content) {
		return cm.startToolFinalization(ctx, run, "repeated tool result")
	}

	if rs.NextCall < len(rs.ToolCalls) {
		return *cm, nil, func() tea.Msg { return ToolContinueMsg{Gen: rs.Gen} }
	}
	return cm.startNextToolRound(ctx, run)
}

// HandleToolResult processes the outcome of an async read-only tool
// execution.
func (cm *Model) HandleToolResult(ctx Context, run *Run, msg ToolResultMsg) (Model, Event, tea.Cmd) {
	rs := run.RoundState
	if rs == nil || msg.Gen != rs.Gen {
		return *cm, nil, nil
	}

	content := msg.Content
	if msg.Err != "" {
		content = "Error: " + msg.Err
	}

	call := rs.ToolCalls[rs.NextCall]
	rs.Messages = append(rs.Messages, ai.Message{
		Role: ai.RoleTool, ToolID: msg.CallID, ToolName: msg.CallName, Content: content,
	})
	rs.NextCall++
	run.Messages = rs.Messages
	if rs.RecordToolResult(call, content) {
		return cm.startToolFinalization(ctx, run, "repeated tool result")
	}

	if rs.NextCall < len(rs.ToolCalls) {
		return *cm, nil, func() tea.Msg { return ToolContinueMsg{Gen: rs.Gen} }
	}
	return cm.startNextToolRound(ctx, run)
}

// startNextToolRound issues the next Complete call from a command closure.
func (cm *Model) startNextToolRound(ctx Context, run *Run) (Model, Event, tea.Cmd) {
	rs := run.RoundState
	if rs == nil {
		return *cm, nil, nil
	}
	if !rs.Finalizing && time.Now().After(rs.ToolDeadline) {
		return cm.startToolFinalization(ctx, run, "tool time budget reached")
	}
	if !rs.Finalizing && rs.ToolCallCount >= MaxToolCalls {
		return cm.startToolFinalization(ctx, run, "tool-call budget reached")
	}

	rs.NextCall = 0
	rs.ToolCalls = nil

	client := rs.Client
	chatContext := rs.ChatContext
	agentID := rs.AgentID
	contextText := rs.ContextText
	toolsDefs := rs.ToolsDefs
	msgs := rs.Messages
	gen := rs.Gen
	history := rs.History
	cancel := rs.Cancel
	conversationID := rs.ConversationID
	finalizing := rs.Finalizing

	return *cm, nil, func() tea.Msg {
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
				return ToolPhaseExpiredMsg{Gen: gen}
			}
			return StreamMsg{ConversationID: conversationID, Err: err}
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
			return ReadStreamEvent(ch, conversationID)
		}

		newMsgs := append(msgs, ai.Message{
			Role: ai.RoleAssistant, Content: turn.Content, ToolCalls: turn.ToolCalls,
		})
		return ToolStartMsg{
			Gen: gen,
			State: ToolRoundState{
				Gen:                     gen,
				Messages:                newMsgs,
				AgentID:                 agentID,
				Client:                  client,
				History:                 history,
				RootContext:             rs.RootContext,
				ChatContext:             chatContext,
				ToolCancel:              rs.ToolCancel,
				Cancel:                  cancel,
				ContextText:             rs.ContextText,
				ToolsDefs:               toolsDefs,
				ConversationID:          conversationID,
				ToolCalls:               turn.ToolCalls,
				ToolCallCount:           rs.ToolCallCount,
				ToolDeadline:            rs.ToolDeadline,
				LastToolResultSignature: rs.LastToolResultSignature,
				RepeatedToolResults:     rs.RepeatedToolResults,
			},
		}
	}
}

// startToolFinalization switches an exhausted tool round to the
// finalization request.
func (cm *Model) startToolFinalization(ctx Context, run *Run, reason string) (Model, Event, tea.Cmd) {
	rs := run.RoundState
	if rs == nil || rs.Finalizing {
		return *cm, nil, nil
	}
	rs.SkipRemainingToolCalls("tool budget ended")
	if rs.ToolCancel != nil {
		rs.ToolCancel()
		rs.ToolCancel = nil
	}
	rs.Finalizing = true
	rs.ChatContext, rs.FinalizationCancel = context.WithTimeout(rs.RootContext, FinalizationTimeout)
	run.Messages = rs.Messages
	next, ev, cmd := cm.startNextToolRound(ctx, run)
	if ev == nil && cm.IsActive(run) {
		ev = StatusChanged{Text: "Assistant finalizing: " + reason}
	}
	return next, ev, cmd
}

// stopToolRound cleans up a deliberately stopped tool run.
func (cm *Model) stopToolRound(ctx Context, run *Run, status string) (Model, Event, tea.Cmd) {
	rs := run.RoundState
	if rs == nil {
		return *cm, nil, nil
	}
	rs.ReleaseContexts()
	if rs.Cancel != nil {
		rs.Cancel()
	}
	run.Loading = false
	run.Cancel = nil
	run.RoundState = nil
	run.PendingWrite = nil
	var event Event
	if cm.IsActive(run) {
		cm.RefreshView()
		event = StatusChanged{Text: status}
	}
	return *cm, event, nil
}

// UpdateChatCompletion shows slash-command suggestions while the chat
// input starts with "/", and clears them otherwise. When the input text is
// unchanged it leaves the current matches and selection alone: events that
// carry no text (e.g. key releases echoed through the textarea) must not
// reset the dropdown to its first item.
func (cm *Model) UpdateChatCompletion() {
	value := cm.Input.Value()
	if value == cm.Completion.Prefix {
		return
	}
	if !strings.HasPrefix(value, "/") {
		cm.Completion = Completion{}
		return
	}
	if len(cm.Completion.Items) == 0 {
		cm.Completion = NewCompletion(cm.Commands())
	}
	cm.Completion.Filter(value)
}

// AcceptChatCompletion replaces the chat input with the selected
// suggestion.
func (cm *Model) AcceptChatCompletion() {
	item := cm.Completion.Accept()
	cm.Completion = Completion{}
	if item.InsertText == "" {
		return
	}
	cm.Input.SetValue(item.InsertText)
}

// Commands returns the slash-command completion candidates. The YOLO and
// result-sharing suggestions are state-aware: they offer the action that
// makes sense now.
func (cm Model) Commands() []CompletionItem {
	commands := []CompletionItem{
		{Label: "/new", InsertText: "/new", Kind: "command", Detail: "start a fresh conversation"},
		{Label: "/history", InsertText: "/history", Kind: "command", Detail: "pick a saved conversation"},
	}
	yolo := "/yolo-off"
	if !cm.YoloWrites {
		yolo = "/yolo-on"
	}
	commands = append(commands, CompletionItem{Label: yolo, InsertText: yolo, Kind: "command", Detail: "execute assistant writes without confirmation"})
	share := "/unshare-results"
	if !cm.ShareResults {
		share = "/share-results"
	}
	return append(commands, CompletionItem{Label: share, InsertText: share, Kind: "command", Detail: "share the visible results with the assistant"})
}
