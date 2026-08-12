package chat

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Update handles the chat messages and pane input. The root routes every
// chat-owned message and every key press while the chat pane is focused
// here; the component emits events for root side effects (status, writes,
// schema refresh, clipboard, editor application).
func (cm *Model) Update(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher, ctx Context) (Model, Event, tea.Cmd) {
	switch message := msg.(type) {
	case SpinnerTickMsg:
		run := cm.ActiveRun()
		if !run.Loading {
			return *cm, nil, nil
		}
		run.SpinnerFrame++
		return *cm, nil, SpinnerTick()
	case HistoryLoadedMsg:
		if message.ConnectionID != ctx.ConnectionID {
			return *cm, nil, nil // stale: started before a disconnect/reconnect
		}
		if message.Err != nil {
			return *cm, StatusChanged{Text: uikit.SafeText("AI history: " + message.Err.Error())}, nil
		}
		if len(message.Conversations) == 0 {
			return *cm, StatusChanged{Text: "no saved AI conversations"}, nil
		}
		choices := make([]huh.Option[string], len(message.Conversations))
		for index, conversation := range message.Conversations {
			choices[index] = huh.NewOption(HistoryOptionLabel(cm.Runs[conversation.ID], conversation.Title), conversation.ID)
		}
		cm.HistoryChoice = message.Conversations[0].ID
		cm.HistoryPicker = uikit.NewForm(huh.NewGroup(huh.NewSelect[string]().Key("conversation").Title("AI conversations").Options(choices...).Value(&cm.HistoryChoice))).WithWidth(max(layout.ViewportWidth, 1))
		return *cm, nil, cm.HistoryPicker.Init()
	case MessagesLoadedMsg:
		if message.ConnectionID != ctx.ConnectionID {
			return *cm, nil, nil // stale: started before a disconnect/reconnect
		}
		if message.Seq != cm.LoadSeq {
			return *cm, nil, nil // stale selection load
		}
		if message.Err != nil {
			return *cm, StatusChanged{Text: uikit.SafeText("AI history: " + message.Err.Error())}, nil
		}
		cm.ActiveID = message.ConversationID
		run := cm.Runs[message.ConversationID]
		if run == nil {
			run = &Run{ConversationID: message.ConversationID, Messages: message.Messages}
			cm.Runs[message.ConversationID] = run
		}
		cm.RefreshView()
		return *cm, nil, nil
	case HistoryDeletedMsg:
		if message.ConnectionID != ctx.ConnectionID {
			return *cm, nil, nil // stale: started before a disconnect/reconnect
		}
		if message.Err != nil {
			return *cm, StatusChanged{Text: uikit.SafeText("AI history: " + message.Err.Error())}, nil
		}
		if message.Clear {
			for _, run := range cm.Runs {
				if run.RoundState != nil {
					run.RoundState.ReleaseContexts()
				}
				if run.Cancel != nil {
					run.Cancel()
				}
			}
			cm.Runs = map[string]*Run{}
		} else if run := cm.Runs[message.ConversationID]; run != nil {
			if run.RoundState != nil {
				run.RoundState.ReleaseContexts()
			}
			if run.Cancel != nil {
				run.Cancel()
			}
			delete(cm.Runs, message.ConversationID)
		}
		if message.Clear || cm.ActiveID == message.ConversationID {
			cm.NewConversation()
		}
		return *cm, StatusChanged{Text: "AI conversation history cleared"}, nil
	case PersistMsg:
		if message.Err != nil {
			return *cm, StatusChanged{Text: uikit.SafeText("AI history: " + message.Err.Error())}, nil
		}
		return *cm, nil, nil
	case TitleMsg:
		if message.ConnectionID != ctx.ConnectionID {
			return *cm, nil, nil // stale: started before a disconnect/reconnect
		}
		if message.Err != nil {
			if run := cm.Runs[message.ConversationID]; run != nil && cm.IsActive(run) {
				return *cm, StatusChanged{Text: uikit.SafeText("AI title: " + message.Err.Error())}, nil
			}
		}
		return *cm, nil, nil
	case StreamMsg:
		run := cm.Runs[message.ConversationID]
		if run == nil {
			return *cm, nil, nil // conversation deleted or never registered
		}
		if message.Err != nil {
			if run.RoundState != nil {
				run.RoundState.ReleaseContexts()
				run.RoundState = nil
			}
			if run.Cancel != nil {
				run.Cancel()
				run.Cancel = nil
			}
			canceled := run.Canceled
			run.Loading = false
			run.Canceled = false
			run.StreamBuffer = ""
			if cm.IsActive(run) {
				cm.RefreshView()
			}
			if canceled {
				if cm.IsActive(run) {
					return *cm, StatusChanged{Text: "AI request canceled"}, nil
				}
				return *cm, nil, nil
			}
			if cm.IsActive(run) {
				return *cm, StatusChanged{Text: uikit.SafeText("AI: " + message.Err.Error())}, nil
			}
			return *cm, nil, nil
		}
		if message.Done {
			// If canceled, discard partial content.
			if run.Canceled {
				if run.RoundState != nil {
					run.RoundState.ReleaseContexts()
					run.RoundState = nil
				}
				if run.Cancel != nil {
					run.Cancel()
					run.Cancel = nil
				}
				run.Loading = false
				run.Canceled = false
				run.StreamBuffer = ""
				if cm.IsActive(run) {
					cm.RefreshView()
					return *cm, StatusChanged{Text: "AI request canceled"}, nil
				}
				return *cm, nil, nil
			}
			wasFinalizing := run.RoundState != nil && run.RoundState.Finalizing
			if run.RoundState != nil {
				run.RoundState.ReleaseContexts()
			}
			// Stream completed successfully — cancel the context.
			if run.Cancel != nil {
				run.Cancel()
				run.Cancel = nil
			}
			run.Loading = false
			run.Canceled = false
			content := run.StreamBuffer
			run.StreamBuffer = ""
			run.RoundState = nil
			if content == "" {
				if cm.IsActive(run) {
					cm.RefreshView()
					return *cm, StatusChanged{Text: "AI returned empty response"}, nil
				}
				return *cm, nil, nil
			}
			response := message.Response
			run.Messages = append(run.Messages, ai.Message{
				Role:    ai.RoleAssistant,
				Agent:   response.Agent,
				Content: content,
			})
			if cm.IsActive(run) {
				cm.RefreshView()
			}
			var event Event
			if wasFinalizing && cm.IsActive(run) {
				event = StatusChanged{Text: "Assistant response complete"}
			}
			// Persist to history asynchronously; report errors regardless
			// of which conversation is visible. The run keeps its captured
			// scope even if the model has since disconnected.
			if cm.History != nil && run.ConnectionID != "" && run.ConversationID != "" {
				history, cid, scope := cm.History, run.ConversationID, run.ConnectionID
				historyMsg := ai.Message{
					Role:    ai.RoleAssistant,
					Agent:   response.Agent,
					Content: content,
				}
				cmd := func() tea.Msg {
					err := history.AppendMessage(cm.baseContext(), scope, cid, historyMsg)
					return PersistMsg{Err: err}
				}
				return *cm, event, cmd
			}
			return *cm, event, nil
		}
		// Delta: accumulate and render, then await the next event.
		run.StreamBuffer += message.Delta
		if cm.IsActive(run) {
			cm.RefreshView()
		}
		ch := message.Ch
		cid := run.ConversationID
		return *cm, nil, func() tea.Msg {
			return ReadStreamEvent(ch, cid)
		}
	case ResponseMsg:
		run := cm.Runs[message.ConversationID]
		if run == nil {
			return *cm, nil, nil
		}
		canceled := run.Canceled
		run.Loading = false
		run.Cancel = nil
		run.Canceled = false
		run.StreamBuffer = ""
		if message.Err != nil {
			if cm.IsActive(run) {
				if canceled {
					return *cm, StatusChanged{Text: "AI request canceled"}, nil
				}
				return *cm, StatusChanged{Text: uikit.SafeText("AI: " + message.Err.Error())}, nil
			}
			return *cm, nil, nil
		}
		run.Messages = append(run.Messages, ai.Message{Role: ai.RoleAssistant, Agent: message.Response.Agent, Content: message.Response.Content})
		if cm.IsActive(run) {
			cm.RefreshView()
		}
		return *cm, nil, nil

	case ToolStartMsg:
		run := cm.Runs[message.State.ConversationID]
		if run == nil || message.Gen != run.Gen {
			return *cm, nil, nil // stale or deleted
		}
		run.RoundState = &message.State
		run.Messages = message.State.Messages
		run.Cancel = message.State.Cancel
		if cm.IsActive(run) {
			cm.RefreshView()
		}
		return *cm, nil, func() tea.Msg {
			return ToolContinueMsg{Gen: message.Gen}
		}

	case ToolPhaseExpiredMsg:
		run := cm.RunByGen(message.Gen)
		if run == nil {
			return *cm, nil, nil // stale
		}
		if message.State != nil {
			run.RoundState = message.State
			run.Messages = message.State.Messages
			run.Cancel = message.State.Cancel
		}
		if run.RoundState == nil {
			return *cm, nil, nil
		}
		return cm.startToolFinalization(ctx, run, "tool time budget reached")

	case ToolContinueMsg:
		run := cm.RunByGen(message.Gen)
		if run == nil || run.RoundState == nil {
			return *cm, nil, nil
		}
		return cm.ProcessNextToolCall(ctx, run)

	case WriteResultMsg:
		run := cm.RunByGen(message.Gen)
		if run == nil {
			return *cm, nil, nil // stale
		}
		return cm.HandleWriteResult(ctx, run, message)

	case ToolResultMsg:
		run := cm.RunByGen(message.Gen)
		if run == nil {
			return *cm, nil, nil // stale
		}
		return cm.HandleToolResult(ctx, run, message)
	}

	keyPress, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if cm.ChatMode == ModeInsert {
			previous := cm.Input.Value()
			input, command := cm.Input.Update(msg)
			cm.Input = input
			if cm.Input.Value() != previous {
				cm.HistoryIndex = -1
			}
			cm.UpdateChatCompletion()
			return *cm, nil, command
		}
		return *cm, nil, nil
	}
	run := cm.ActiveRun()
	if run.Loading && cm.ChatMode != ModeInsert && keyPress.Key().Code == tea.KeyEscape {
		if run.RoundState != nil {
			run.RoundState.ReleaseContexts()
		}
		if run.Cancel != nil {
			run.Cancel()
			run.Cancel = nil
		}
		run.Canceled = true
		run.RoundState = nil
		run.PendingWrite = nil
		return *cm, nil, nil
	}
	switch {
	case keys.Match(keyPress, "chat.delete", []uikit.Scope{uikit.ScopeView}):
		return *cm, nil, cm.DeleteHistory(ctx, false)
	case keys.Match(keyPress, "chat.clear", []uikit.Scope{uikit.ScopeView}):
		return *cm, nil, cm.DeleteHistory(ctx, true)
	case keys.Match(keyPress, "chat.apply_sql", []uikit.Scope{uikit.ScopeView}):
		if statement := SQL(run.Messages); statement != "" {
			return *cm, SQLRequested{Statement: statement, Source: "editor"}, nil
		}
		return *cm, nil, nil
	case keyPress.Key().Code == tea.KeyPgUp:
		cm.Viewport.PageUp()
		return *cm, nil, nil
	case keyPress.Key().Code == tea.KeyPgDown:
		cm.Viewport.PageDown()
		return *cm, nil, nil
	}

	if cm.ChatMode == ModeNormal {
		if keyPress.Key().Code == 'i' || keyPress.Key().Code == tea.KeyEnter {
			cm.ChatMode = ModeInsert
			return *cm, nil, cm.Input.Focus()
		}
		return *cm, nil, nil
	}

	// Insert mode
	if keyPress.Key().Code == tea.KeyEscape {
		cm.Completion.Dismiss()
		cm.ChatMode = ModeNormal
		cm.Input.Blur()
		return *cm, nil, nil
	}
	if cm.Completion.Visible() {
		key := keyPress.Key()
		switch {
		case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == tea.ModCtrl):
			cm.Completion.Move(-1)
			return *cm, nil, nil
		case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == tea.ModCtrl):
			cm.Completion.Move(1)
			return *cm, nil, nil
		case key.Code == tea.KeyTab:
			cm.AcceptChatCompletion()
			return *cm, nil, nil
		case key.Code == tea.KeyEnter:
			item := cm.Completion.Accept()
			cm.Completion = Completion{}
			if item.InsertText == "" || item.InsertText == cm.Input.Value() {
				// Already complete (e.g. "/new"): Enter runs it.
				return cm.StartChat(ctx)
			}
			cm.Input.SetValue(item.InsertText)
			return *cm, nil, nil
		}
	}
	key := keyPress.Key()
	if (key.Code == tea.KeyUp && (cm.Input.Value() == "" || cm.HistoryIndex >= 0)) ||
		(key.Code == tea.KeyDown && cm.HistoryIndex >= 0) {
		direction := 1
		if key.Code == tea.KeyDown {
			direction = -1
		}
		if value, ok := cm.RecallPromptHistory(direction); ok {
			cm.Input.SetValue(value)
			return *cm, nil, nil
		}
	}
	if keyPress.Key().Code == tea.KeyEnter {
		return cm.StartChat(ctx)
	}

	if cm.ChatMode == ModeInsert {
		previous := cm.Input.Value()
		input, command := cm.Input.Update(msg)
		cm.Input = input
		if cm.Input.Value() != previous {
			cm.HistoryIndex = -1
		}
		cm.UpdateChatCompletion()
		return *cm, nil, command
	}
	return *cm, nil, nil
}

// HistoryPickerOutcome is one root-applied outcome of a history-picker
// update: the picker closed without a selection, or a conversation was
// picked and the root loads it through the component.
type HistoryPickerOutcome struct {
	Picked string // selected conversation id; empty when the picker closed
	Closed bool
}

// UpdateHistoryPicker drives the open /history conversation picker: Escape
// closes it, every other message passes through to the form, and a
// completed selection emits the picked conversation. The root loads the
// conversation through LoadMessages.
func (cm Model) UpdateHistoryPicker(msg tea.Msg) (Model, HistoryPickerOutcome, tea.Cmd) {
	if keyPress, ok := msg.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
		cm.HistoryPicker = nil
		return cm, HistoryPickerOutcome{Closed: true}, nil
	}
	form, command := cm.HistoryPicker.Update(msg)
	cm.HistoryPicker = form.(*huh.Form)
	if cm.HistoryPicker.State != huh.StateCompleted {
		return cm, HistoryPickerOutcome{}, command
	}
	conversationID := cm.HistoryChoice
	cm.HistoryPicker = nil
	return cm, HistoryPickerOutcome{Picked: conversationID}, command
}

// HistoryPickerContent renders the open /history conversation picker, or
// "" when none is open. The root draws the picker overlay; the component
// renders its body.
func (cm Model) HistoryPickerContent() string {
	if cm.HistoryPicker == nil {
		return ""
	}
	return cm.HistoryPicker.View()
}
