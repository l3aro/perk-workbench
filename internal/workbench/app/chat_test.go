package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/ai"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
)

// TestChat_spinnerWhileLoading guards the assistant progress spinner: it must
// render right-aligned in the chat mode line only while loading, advance on
// each tick, re-arm while loading, and stop when loading ends.
func TestChat_spinnerWhileLoading(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	if strings.Contains(model.chatModeBadge(), "⠋") {
		t.Fatal("spinner shown while idle")
	}

	model.chat.component.ActiveRun().Loading = true
	model.chat.component.ActiveRun().SpinnerFrame = 1
	badge := model.chatModeBadge()
	if !strings.Contains(badge, "⠙") {
		t.Fatalf("badge = %q, want spinner frame while loading", badge)
	}
	if !strings.Contains(ansi.Strip(badge), "NORMAL") {
		t.Fatalf("badge = %q, want NORMAL badge on the left", badge)
	}

	updated, cmd := model.Update(chat.SpinnerTickMsg{})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("tick while loading should re-arm the spinner")
	}
	if model.chat.component.ActiveRun().SpinnerFrame != 2 {
		t.Fatalf("spinnerFrame = %d, want 2", model.chat.component.ActiveRun().SpinnerFrame)
	}

	model.chat.component.ActiveRun().Loading = false
	updated, cmd = model.Update(chat.SpinnerTickMsg{})
	model = updated.(Model)
	assertOnlyNotificationTick(t, cmd)
	if got := model.chatModeBadge(); strings.Contains(got, "⠋") {
		t.Fatalf("badge = %q, spinner shown after loading finished", got)
	}
}

// TestChat_spinnerWithYOLO guards that loading shows the spinner to the left
// of the YOLO indicator instead of replacing it.
func TestChat_spinnerWithYOLO(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	model.chat.component.YoloWrites = true
	model.chat.component.ActiveRun().Loading = true
	model.chat.component.ActiveRun().SpinnerFrame = 2
	badge := model.chatModeBadge()
	if !strings.Contains(badge, "⠹") {
		t.Fatalf("badge = %q, want spinner while loading with YOLO on", badge)
	}
	spinner, _, found := strings.Cut(ansi.Strip(badge), "YOLO")
	if !found {
		t.Fatalf("badge = %q, want YOLO indicator while loading", badge)
	}
	if !strings.Contains(spinner, "⠹") {
		t.Fatalf("badge = %q, spinner must sit left of YOLO", badge)
	}
}

type fakeChatClient struct{}

func (fakeChatClient) AgentForPrompt(string) string { return "assistant" }

func (fakeChatClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}

func (fakeChatClient) Chat(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Add an index."}, nil
}

func (fakeChatClient) Complete(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Add an index."}, nil
}

func (fakeChatClient) SupportsTools(string) bool { return false }

func (fakeChatClient) ChatStream(_ context.Context, request ai.Request) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 4)
	ch <- ai.StreamEvent{Delta: "Add "}
	ch <- ai.StreamEvent{Delta: "an "}
	ch <- ai.StreamEvent{Delta: "index."}
	responseContent := "Add an index."
	var lastPrompt string
	if len(request.Messages) > 0 {
		lastPrompt = request.Messages[len(request.Messages)-1].Content
	}
	if lastPrompt == "talk like a pirate" {
		responseContent = "Arr, add an index!"
	}
	ch <- ai.StreamEvent{Response: &ai.Response{Agent: "Assistant", Content: responseContent}}
	close(ch)
	return ch, nil
}

type waitingChatClient struct {
	started chan<- struct{}
}

func (client waitingChatClient) AgentForPrompt(string) string { return "assistant" }

func (client waitingChatClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}

func (client waitingChatClient) Chat(ctx context.Context, _ ai.Request) (ai.Response, error) {
	client.started <- struct{}{}
	<-ctx.Done()
	return ai.Response{}, ctx.Err()
}

func (client waitingChatClient) Complete(ctx context.Context, _ ai.Request) (ai.Response, error) {
	client.started <- struct{}{}
	<-ctx.Done()
	return ai.Response{}, ctx.Err()
}

func (waitingChatClient) SupportsTools(string) bool { return false }

func (client waitingChatClient) ChatStream(ctx context.Context, _ ai.Request) (<-chan ai.StreamEvent, error) {
	client.started <- struct{}{}
	<-ctx.Done()
	ch := make(chan ai.StreamEvent)
	close(ch)
	return ch, nil
}

// blockingStreamClient blocks its first ChatStream until release is closed;
// later streams complete immediately. Simulates a slow background agent run
// next to a fast one. started is closed once the first stream is requested.
type blockingStreamClient struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (c *blockingStreamClient) AgentForPrompt(string) string { return "assistant" }

func (c *blockingStreamClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}

func (c *blockingStreamClient) Chat(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "x"}, nil
}

func (c *blockingStreamClient) Complete(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "x"}, nil
}

func (c *blockingStreamClient) SupportsTools(string) bool { return false }

func (c *blockingStreamClient) ChatStream(_ context.Context, _ ai.Request) (<-chan ai.StreamEvent, error) {
	c.mu.Lock()
	c.calls++
	blocking := c.calls == 1
	if blocking {
		close(c.started)
	}
	c.mu.Unlock()
	if !blocking {
		ch := make(chan ai.StreamEvent, 2)
		ch <- ai.StreamEvent{Kind: ai.EventDelta, Delta: "fast answer"}
		ch <- ai.StreamEvent{Kind: ai.EventDone, Response: &ai.Response{Agent: "Assistant", Content: "fast answer"}}
		close(ch)
		return ch, nil
	}
	ch := make(chan ai.StreamEvent)
	go func() {
		<-c.release
		ch <- ai.StreamEvent{Kind: ai.EventDelta, Delta: "slow answer"}
		ch <- ai.StreamEvent{Kind: ai.EventDone, Response: &ai.Response{Agent: "Assistant", Content: "slow answer"}}
		close(ch)
	}()
	return ch, nil
}

func TestChat_streamingRendersPartialContent(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'})
	model = updated.(Model)
	model.chat.component.Input.SetValue("talk like a pirate")

	// Send
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// First delta — assert partial content appears
	model, cmd := resolveChatCommand(model, command)
	stripped := ansi.Strip(model.chat.component.Viewport.GetContent())
	if !strings.Contains(stripped, "Add ") {
		t.Fatalf("after first delta = %q, want \"Add \"", stripped)
	}
	// Label should say "streaming"
	if !strings.Contains(stripped, "streaming") {
		t.Fatalf("viewport = %q, want streaming label", stripped)
	}
	if model.chat.component.ActiveRun().Loading != true {
		t.Fatal("model should still be loading after first delta")
	}
	if model.chat.component.ActiveRun().StreamBuffer != "Add " {
		t.Fatalf("streamBuffer = %q", model.chat.component.ActiveRun().StreamBuffer)
	}

	// Second delta
	model, cmd = resolveChatCommand(model, cmd)
	stripped = ansi.Strip(model.chat.component.Viewport.GetContent())
	if !strings.Contains(stripped, "Add an ") {
		t.Fatalf("after second delta = %q, want \"Add an \"", stripped)
	}

	// Third delta
	model, cmd = resolveChatCommand(model, cmd)

	// Completion
	model, _ = resolveChatCommand(model, cmd)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading after completion")
	}
	if len(model.chat.component.ActiveRun().Messages) != 2 {
		t.Fatalf("messages = %#v, want 2", model.chat.component.ActiveRun().Messages)
	}
	if model.chat.component.ActiveRun().Messages[1].Content != "Add an index." {
		t.Fatalf("response = %q", model.chat.component.ActiveRun().Messages[1].Content)
	}
}

// TestChat_slashNewStartsNewConversation guards the /new slash command: it
// must clear messages and the conversation ID without sending a request.
func TestChat_slashNewStartsNewConversation(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	model.chat.component.ActiveID = "existing"
	model.chat.component.ActiveRun().Messages = []ai.Message{{Role: ai.RoleUser, Content: "old turn"}}

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	model.chat.component.Input.SetValue("/new")

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	assertOnlyNotificationTick(t, command)
	if model.chat.component.ActiveID != "" {
		t.Fatalf("conversation ID = %q, want cleared", model.chat.component.ActiveID)
	}
	if len(model.chat.component.ActiveRun().Messages) != 0 {
		t.Fatalf("messages = %#v, want cleared", model.chat.component.ActiveRun().Messages)
	}
	if got := model.chat.component.Input.Value(); got != "" {
		t.Fatalf("input = %q, want cleared", got)
	}
}

// TestChat_concurrentConversationsRunIndependently guards the core
// concurrency contract: starting a second conversation (or switching to an
// older one via the /history picker) must not interrupt a run that is still
// streaming in the background, and both conversations must persist complete
// histories.
func TestChat_concurrentConversationsRunIndependently(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	client := &blockingStreamClient{started: make(chan struct{}), release: make(chan struct{})}
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(client, history)
	model.connectionID = "test-connection"
	model.applyLayout(140, 32)

	// Conversation A: send a prompt whose stream blocks until released.
	model.chat.component.Input.SetValue("slow question")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmdA := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmdA == nil {
		t.Fatal("sending chat A did not return a command")
	}
	idA := model.chat.component.ActiveID
	if idA == "" {
		t.Fatal("conversation A was not created")
	}
	streamA := make(chan tea.Msg, 1)
	go runChatCommand(cmdA, streamA)
	<-client.started // A's stream is blocked and waiting

	// /new while A runs: fresh view, A keeps running in the background.
	model.chat.component.Input.SetValue("/new")
	updated, cmdNew := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = driveNotificationCommand(t, model, cmdNew)
	if model.chat.component.ActiveID != "" {
		t.Fatalf("active conversation = %q, want fresh view", model.chat.component.ActiveID)
	}
	if !model.chat.component.Runs[idA].Loading {
		t.Fatal("switching to /new interrupted conversation A")
	}

	// Conversation B: send and complete while A is still running.
	model.chat.component.Input.SetValue("fast question")
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmdB := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	idB := model.chat.component.ActiveID
	if idB == "" || idB == idA {
		t.Fatalf("conversation B id = %q, want distinct from A %q", idB, idA)
	}
	model = driveStreamToCompletion(t, model, cmdB)

	if !model.chat.component.Runs[idA].Loading {
		t.Fatal("completing B interrupted conversation A")
	}
	if len(model.chat.component.Runs[idB].Messages) != 2 || model.chat.component.Runs[idB].Messages[1].Content != "fast answer" {
		t.Fatalf("B messages = %#v", model.chat.component.Runs[idB].Messages)
	}

	// Switch back to A: open the /history picker, then drive the selection
	// handoff its submit produces (huh's own key handling is library
	// plumbing; the load command it emits is the product contract).
	model.chat.component.Input.SetValue("/history")
	updated, cmdHistory := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmdHistory == nil {
		t.Fatal("/history sent no command")
	}
	model, cmdHistory = resolveChatCommand(model, cmdHistory)
	if model.chat.component.HistoryPicker == nil {
		t.Fatal("/history did not open the conversation picker")
	}
	model.chat.component.HistoryChoice = idA // what a selection in the form binds
	model.chat.component.HistoryPicker = nil // what the submit handler does first
	cmdLoad := model.chat.component.LoadMessages(model.chatLayout(), idA)
	updated, _ = model.Update(cmdLoad())
	model = updated.(Model)
	if model.chat.component.ActiveID != idA {
		t.Fatalf("active conversation = %q, want A", model.chat.component.ActiveID)
	}
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("A should still be loading when switching back")
	}
	if model.chat.component.ActiveRun().Messages[0].Content != "slow question" {
		t.Fatalf("A view = %#v, want the slow question", model.chat.component.ActiveRun().Messages)
	}

	// Release A's stream and drive it to completion.
	close(client.release)
	model, cmd := resolveChatMessage(model, <-streamA)
	for cmd != nil {
		model, cmd = resolveChatCommand(model, cmd)
	}
	if model.chat.component.ActiveRun().Loading {
		t.Fatal("A still loading after stream completion")
	}
	if len(model.chat.component.ActiveRun().Messages) != 2 || model.chat.component.ActiveRun().Messages[1].Content != "slow answer" {
		t.Fatalf("A messages = %#v", model.chat.component.ActiveRun().Messages)
	}

	// Both conversations persisted with full histories.
	for _, cid := range []string{idA, idB} {
		persisted, err := history.Messages(context.Background(), "test-connection", cid)
		if err != nil {
			t.Fatal(err)
		}
		if len(persisted) != 2 {
			t.Fatalf("persisted messages for %s = %#v, want 2", cid, persisted)
		}
	}
}

// TestChat_staleConversationLoadIsDropped guards rapid /history selections:
// a slow load for an earlier pick must not activate its conversation after a
// newer pick already landed.
func TestChat_staleConversationLoadIsDropped(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	a, err := history.NewConversation(context.Background(), "test-connection", "conv a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := history.NewConversation(context.Background(), "test-connection", "conv b")
	if err != nil {
		t.Fatal(err)
	}
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, history)
	model.connectionID = "test-connection"
	model.applyLayout(140, 32)

	loadA := model.chat.component.LoadMessages(model.chatLayout(), a.ID) // seq 1
	loadB := model.chat.component.LoadMessages(model.chatLayout(), b.ID) // seq 2

	// B's load lands first and activates B.
	updated, _ := model.Update(loadB())
	model = updated.(Model)
	if model.chat.component.ActiveID != b.ID {
		t.Fatalf("active conversation = %q, want B", model.chat.component.ActiveID)
	}

	// A's stale load lands late and must be dropped.
	updated, _ = model.Update(loadA())
	model = updated.(Model)
	if model.chat.component.ActiveID != b.ID {
		t.Fatalf("stale load switched active conversation to %q, want B", model.chat.component.ActiveID)
	}
}

// TestChat_historyOptionLabelPrefixesRunningConversations guards the /history
// picker labels: a conversation whose agent run is active gets a spinner
// glyph prefix, idle ones do not.
func TestChat_historyOptionLabelPrefixesRunningConversations(t *testing.T) {
	if got := chat.HistoryOptionLabel(nil, "old chat"); got != "old chat" {
		t.Fatalf("idle label = %q", got)
	}
	if got := chat.HistoryOptionLabel(&chat.Run{}, "old chat"); got != "old chat" {
		t.Fatalf("idle run label = %q", got)
	}
	run := &chat.Run{Loading: true, SpinnerFrame: 1}
	if got := chat.HistoryOptionLabel(run, "old chat"); got != "⠙ old chat" {
		t.Fatalf("running label = %q", got)
	}
}

// TestChat_slashCompletionSuggestsCommands guards the chat slash-command
// autocomplete: typing "/n" must show a "/new" suggestion of kind command,
// Tab must accept it, and Enter on the complete command runs it.
func TestChat_slashCompletionSuggestsCommands(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	// Type "/n".
	for _, ch := range []rune{'/', 'n'} {
		updated, _ = model.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		model = updated.(Model)
	}

	if !model.chat.component.Completion.Visible() {
		t.Fatal("completion should be visible for \"/n\"")
	}
	if item := model.chat.component.Completion.Accept(); item.Label != "/new" || item.Kind != "command" {
		t.Fatalf("suggestion = %#v, want /new command", item)
	}
	if got := ansi.Strip(model.chatContentView()); !strings.Contains(got, "command") {
		t.Fatal("overlay does not show the command type")
	}
	linesWithOverlay := len(strings.Split(ansi.Strip(model.chatContentView()), "\n"))

	// Tab accepts: input becomes "/new", completion hides.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if got := model.chat.component.Input.Value(); got != "/new" {
		t.Fatalf("input = %q, want /new", got)
	}
	if model.chat.component.Completion.Visible() {
		t.Fatal("completion should hide after accept")
	}
	// The dropdown overlays the viewport: content height must not change.
	if lines := len(strings.Split(ansi.Strip(model.chatContentView()), "\n")); lines != linesWithOverlay {
		t.Fatalf("content lines = %d with overlay, %d after, want equal", linesWithOverlay, lines)
	}

	// Enter runs the /new command: no request, messages cleared.
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	assertOnlyNotificationTick(t, command)
	if len(model.chat.component.ActiveRun().Messages) != 0 {
		t.Fatalf("messages = %#v, want cleared", model.chat.component.ActiveRun().Messages)
	}
}

// TestChat_completionArrowNavigation guards dropdown navigation: Up/Down and
// Ctrl+K/Ctrl+J move the selection (wrapping), typing refilters, and Tab
// accepts the selected item.
func TestChat_completionArrowNavigation(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if got := len(model.chat.component.Completion.Matches); got != 4 {
		t.Fatalf("matches = %d, want 4", got)
	}

	move := func(code rune, mod tea.KeyMod) {
		updated, _ = model.Update(tea.KeyPressMsg{Code: code, Mod: mod})
		model = updated.(Model)
	}
	move(tea.KeyDown, 0)
	if got := model.chat.component.Completion.Selected; got != 1 {
		t.Fatalf("selected after Down = %d, want 1", got)
	}
	// A no-text event (the terminal's key-release echo) must not reset it.
	updated, _ = model.Update(tea.KeyReleaseMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if got := model.chat.component.Completion.Selected; got != 1 {
		t.Fatalf("selected after key release = %d, want 1", got)
	}
	move(tea.KeyDown, 0)
	if got := model.chat.component.Completion.Selected; got != 2 {
		t.Fatalf("selected after 2x Down = %d, want 2", got)
	}
	move(tea.KeyDown, 0)
	if got := model.chat.component.Completion.Selected; got != 3 {
		t.Fatalf("selected after 3x Down = %d, want 3", got)
	}
	// Wraps around the list.
	move(tea.KeyDown, 0)
	if got := model.chat.component.Completion.Selected; got != 0 {
		t.Fatalf("selected after wrap = %d, want 0", got)
	}
	// Up arrow walks back.
	move(tea.KeyUp, 0)
	if got := model.chat.component.Completion.Selected; got != 3 {
		t.Fatalf("selected after Up = %d, want 3", got)
	}
	// Ctrl+J / Ctrl+K behave like Down/Up.
	move('j', tea.ModCtrl)
	if got := model.chat.component.Completion.Selected; got != 0 {
		t.Fatalf("selected after Ctrl+J = %d, want 0", got)
	}
	move('k', tea.ModCtrl)
	if got := model.chat.component.Completion.Selected; got != 3 {
		t.Fatalf("selected after Ctrl+K = %d, want 3", got)
	}

	// Tab accepts the selected item, not the first one.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	want := "/share-results"
	if got := model.chat.component.Input.Value(); got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

// TestChat_historySlashCommand guards the /history slash command: Enter on the
// complete command loads conversations into the history picker instead of
// sending an AI request.
func TestChat_historySlashCommand(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	if _, err := history.NewConversation(context.Background(), "test-connection", "old chat"); err != nil {
		t.Fatal(err)
	}
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, history)
	model.connectionID = "test-connection"
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	model.chat.component.Input.SetValue("/history")
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("/history sent no command")
	}
	msg := command()
	if _, ok := msg.(chat.HistoryLoadedMsg); !ok {
		t.Fatalf("command returned %T, want chat.HistoryLoadedMsg", msg)
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)
	if model.chat.component.HistoryPicker == nil {
		t.Fatal("/history did not open the conversation picker")
	}
}

// TestChat_yoloCommandsToggleWrites guards the YOLO commands: the suggestion
// is state-aware (only the action for the current state), Tab accepts it, and
// Enter runs it without sending a request.
func TestChat_yoloCommandsToggleWrites(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	// Off state suggests only "/yolo-on".
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	matches := model.chat.component.Completion.Matches
	if len(matches) != 1 || matches[0].Label != "/yolo-on" || matches[0].Kind != "command" {
		t.Fatalf("matches = %#v, want only /yolo-on command", matches)
	}

	// Tab accepts; Enter runs it without sending a request.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if got := model.chat.component.Input.Value(); got != "/yolo-on" {
		t.Fatalf("input = %q, want /yolo-on", got)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	assertOnlyNotificationTick(t, command)
	if !model.chat.component.YoloWrites {
		t.Fatal("yoloWrites = false, want true")
	}

	// On state suggests only "/yolo-off".
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	matches = model.chat.component.Completion.Matches
	if len(matches) != 1 || matches[0].Label != "/yolo-off" {
		t.Fatalf("matches = %#v, want only /yolo-off", matches)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	assertOnlyNotificationTick(t, command)
	if model.chat.component.YoloWrites {
		t.Fatal("yoloWrites = true, want false")
	}

	// Prose mentioning YOLO must be sent to the AI, not executed as a command.
	model.chat.component.Input.SetValue("turn on YOLO")
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("prose prompt must be sent to the AI")
	}
	if model.chat.component.YoloWrites {
		t.Fatal("prose prompt toggled yoloWrites")
	}
}

func TestChat_enterSendsPromptAndRendersResponse(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'})
	model = updated.(Model)
	if model.Focus != focusChat {
		t.Fatalf("focus = %v, want chat", model.Focus)
	}
	model.chat.component.Input.SetValue("How should I speed this up?")

	// Enter insert mode before sending
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}
	// Drive the streaming protocol through completion.
	model = driveStreamToCompletion(t, model, command)

	if len(model.chat.component.ActiveRun().Messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant messages", model.chat.component.ActiveRun().Messages)
	}
	if got := model.chat.component.ActiveRun().Messages[1].Content; got != "Add an index." {
		t.Fatalf("assistant response = %q", got)
	}
	if strings.Contains(model.chat.component.Viewport.GetContent(), "Assistant") {
		t.Fatal("chat response renders an assistant label")
	}
}

// resolveChatCommand executes one chat-flow command, expanding tea.BatchMsg
// children in order and dropping the progress-spinner tick chain (the real
// runtime re-arms it; tests drive commands manually and would spin forever).
func resolveChatCommand(model Model, command tea.Cmd) (Model, tea.Cmd) {
	if command == nil {
		return model, nil
	}
	return resolveChatMessage(model, command())
}

// resolveChatMessage feeds one already-produced message through the model,
// expanding tea.BatchMsg children and dropping the spinner tick chain.
func resolveChatMessage(model Model, message tea.Msg) (Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BatchMsg:
		// The real runtime runs batch children concurrently and follows
		// every returned command, so preserve all of them (e.g. the stream
		// continuation plus a notification dismiss tick).
		var nexts []tea.Cmd
		for _, child := range msg {
			var next tea.Cmd
			model, next = resolveChatCommand(model, child)
			if next != nil {
				nexts = append(nexts, next)
			}
		}
		switch len(nexts) {
		case 0:
			return model, nil
		case 1:
			return model, nexts[0]
		default:
			return model, tea.Batch(nexts...)
		}
	case chat.SpinnerTickMsg:
		return model, nil
	default:
		updated, next := model.Update(msg)
		return updated.(Model), next
	}
}

// runChatCommand executes a chat-flow command in a goroutine, expanding
// tea.BatchMsg children (the real runtime runs them concurrently), and reports
// the first non-tick message on ch.
func runChatCommand(command tea.Cmd, ch chan<- tea.Msg) {
	for _, message := range executeCommandAll(command) {
		if _, tick := message.(chat.SpinnerTickMsg); !tick {
			ch <- message
			return
		}
	}
}

// driveStreamToCompletion feeds streaming events until the stream completes.
func driveStreamToCompletion(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		model, cmd = resolveChatCommand(model, cmd)
	}
	return model
}

func TestChat_realProviderResponsePersistsConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"Use \"}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"an \"}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"index.\"}}]}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	client, err := ai.NewClient(ai.Config{
		Providers: map[string]ai.Provider{"test": {
			Name: "Test", API: ai.APIOpenAICompatible, BaseURL: server.URL + "/v1", APIKey: "test", Models: []string{"small"},
		}},
		Agents: map[string]ai.Agent{"assistant": {
			Name: "Assistant", Provider: "test", Model: "small", SystemPrompt: "Help.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(client, history)
	model.connectionID = "test-connection"
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("How should I speed this up?")

	// Enter insert mode before sending
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // ensure normal
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}
	// Drive streaming through completion (including async persistence).
	model = driveStreamToCompletion(t, model, command)

	if len(model.chat.component.ActiveRun().Messages) != 2 || model.chat.component.ActiveRun().Messages[1].Content != "Use an index." {
		t.Fatalf("messages = %#v", model.chat.component.ActiveRun().Messages)
	}
	// Check that the persisted conversation includes both messages.
	persisted, err := history.Messages(context.Background(), "test-connection", model.chat.component.ActiveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 2 {
		t.Fatalf("persisted messages = %#v", persisted)
	}
}

func TestChat_generatesTitleForNewConversation(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, history)
	model.connectionID = "test-connection"
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("How do I add an index?")

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // ensure normal
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}
	model = driveStreamToCompletion(t, model, command)

	conversations, err := history.Conversations(context.Background(), "test-connection")
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 {
		t.Fatalf("conversations = %#v, want one", conversations)
	}
	if conversations[0].Title != "Cheap title" {
		t.Fatalf("conversation title = %q, want generated title", conversations[0].Title)
	}
}

func TestChatSQL_returnsLatestFencedSQLBlock(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleAssistant, Content: "```sql\nSELECT 1;\n```"},
		{Role: ai.RoleAssistant, Content: "```postgresql\nCREATE TABLE projects (id integer);\n```"},
	}

	statement := chat.SQL(messages)
	if statement != "CREATE TABLE projects (id integer);" {
		t.Fatalf("statement = %q", statement)
	}
}

func TestChat_toggleVisibilityChangesPaneLayout(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.applyLayout(140, 32)
	if model.chat.component.Visible {
		t.Fatal("AI pane is visible without an Assistant")
	}

	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)
	if !model.chat.component.Visible || model.layout.editorWidth != 58 {
		t.Fatalf("AI pane = visible:%t editorWidth:%d", model.chat.component.Visible, model.layout.editorWidth)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if model.chat.component.Visible || model.layout.editorWidth != 94 {
		t.Fatalf("AI pane after toggle = visible:%t editorWidth:%d", model.chat.component.Visible, model.layout.editorWidth)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if !model.chat.component.Visible || model.layout.editorWidth != 58 {
		t.Fatalf("AI pane after second toggle = visible:%t editorWidth:%d", model.chat.component.Visible, model.layout.editorWidth)
	}
}

func TestChat_inputPaddingPreservesPaneHeight(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	if got, want := lipgloss.Height(model.chatContentView()), model.layout.height-4; got != want {
		t.Fatalf("chat content height = %d, want %d", got, want)
	}
}

func TestChat_fullscreenUserMessageFillsViewport(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, nil)
	model.layout.fullscreen = true
	model.applyLayout(140, 32)
	if got, want := model.chat.component.Viewport.Width(), model.layout.width-6; got != want {
		t.Fatalf("chat viewport width = %d, want pane interior width %d", got, want)
	}
	model.chat.component.ActiveRun().Messages = []ai.Message{{Role: ai.RoleUser, Content: "full width"}}
	model.chat.component.RefreshView()

	content := model.chat.component.Viewport.GetContent()
	if strings.Contains(content, "\n") {
		t.Fatalf("user message content = %q, want one rendered row", content)
	}
	line := content
	if got, want := ansi.StringWidth(line), model.chat.component.Viewport.Width(); got != want {
		t.Fatalf("user message width = %d, want viewport width %d", got, want)
	}
	if !strings.Contains(line, "\u00a0") {
		t.Fatal("user message uses clearable ASCII space padding")
	}
	viewLine, _, _ := strings.Cut(model.chat.component.Viewport.View(), "\n")
	if !strings.Contains(viewLine, "full width") {
		t.Fatalf("user message viewport line = %q, want prompt text beside its accent", viewLine)
	}
}

func TestChat_shareResultsTool(t *testing.T) {
	model := readyModel(t)
	model.State, model.Focus = stateReady, focusChat
	model.queryLog.results.SetColumns(tableColumns([]string{"id", "name"}, []table.Row{{"1", "alice"}}))
	model.queryLog.results.SetRows([]table.Row{{"1", "alice"}})

	// Sharing is off by default: no tool, no results block in context.
	if hasTool(model.chat.component.DatabaseTools(model.chatLayout()), "get_visible_results") {
		t.Fatal("get_visible_results present while sharing is off")
	}
	if context := model.chat.component.ContextText(model.chatLayout()); strings.Contains(context, "Visible results:") {
		t.Fatalf("chat context = %q, want no visible results block", context)
	}

	// Sharing on via the AI slash command: the tool appears and returns the rows.
	model.chat.component.Input.SetValue("/share-results")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	assertOnlyNotificationTick(t, command)
	if !model.chat.component.ShareResults {
		t.Fatal("shareResults should be true after command")
	}
	if !hasTool(model.chat.component.DatabaseTools(model.chatLayout()), "get_visible_results") {
		t.Fatal("get_visible_results missing while sharing is on")
	}
	result := model.chat.component.ExecuteTool(context.Background(), ai.ToolCall{ID: "call_1", Name: "get_visible_results"}, model.chatLayout())
	if result.Error != "" || !strings.Contains(result.Content, "id | name") || !strings.Contains(result.Content, "1 | alice") {
		t.Fatalf("tool result = %#v", result)
	}

	// Sharing off again: the tool disappears.
	model.chat.component.Input.SetValue("/unshare-results")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.chat.component.ShareResults {
		t.Fatal("shareResults should be false after stop command")
	}
	if hasTool(model.chat.component.DatabaseTools(model.chatLayout()), "get_visible_results") {
		t.Fatal("get_visible_results present while sharing is off")
	}
}

func hasTool(tools []ai.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// recordingChatClient captures the request context so tests can assert what
// tool-less providers receive.
type recordingChatClient struct {
	context string
}

func (client *recordingChatClient) AgentForPrompt(string) string { return "assistant" }

func (client *recordingChatClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}

func (client *recordingChatClient) Chat(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{}, nil
}

func (client *recordingChatClient) Complete(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{}, nil
}

func (client *recordingChatClient) SupportsTools(string) bool { return false }

func (client *recordingChatClient) ChatStream(_ context.Context, request ai.Request) (<-chan ai.StreamEvent, error) {
	client.context = request.Context
	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Response: &ai.Response{Agent: "Assistant", Content: "ok"}}
	close(ch)
	return ch, nil
}

// TestChat_shareResultsToollessProvider guards the fallback for providers
// without tool support: the results must ride along in the request context
// since they cannot call get_visible_results.
func TestChat_shareResultsToollessProvider(t *testing.T) {
	client := &recordingChatClient{}
	model := readyModel(t)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(client, nil)
	model.applyLayout(140, 32)
	model.queryLog.results.SetColumns(tableColumns([]string{"id", "name"}, []table.Row{{"1", "alice"}}))
	model.queryLog.results.SetRows([]table.Row{{"1", "alice"}})

	// Enable sharing via the AI slash command.
	model.chat.component.Input.SetValue("/share-results")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	assertOnlyNotificationTick(t, command)

	model.chat.component.Input.SetValue("what do the results say?")
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}
	model = driveStreamToCompletion(t, model, command)

	if !strings.Contains(client.context, "Visible results:\n1 | alice") {
		t.Fatalf("request context = %q, want visible results", client.context)
	}
}

func TestChat_escapeCancelsActiveRequest(t *testing.T) {
	started := make(chan struct{})
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(waitingChatClient{started: started}, nil)
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("cancel this request")

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}

	response := make(chan tea.Msg, 1)
	go runChatCommand(command, response)
	<-started

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	if model.chat.component.ChatMode != chat.ModeNormal {
		t.Fatalf("chat mode = %d, want normal after first escape", model.chat.component.ChatMode)
	}
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("first escape canceled the chat request")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model, _ = resolveChatMessage(model, <-response)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("chat request remains loading after cancellation")
	}
	if model.Status != "AI request canceled" {
		t.Fatalf("status = %q, want cancellation status", model.Status)
	}
}

func TestChat_escapeCancelsActiveRequest_fullScreen(t *testing.T) {
	started := make(chan struct{})
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.layout.fullscreen = true
	model.SetAI(waitingChatClient{started: started}, nil)
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("cancel this request")

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}

	response := make(chan tea.Msg, 1)
	go runChatCommand(command, response)
	<-started

	// First Escape: exit insert mode.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.chat.component.ChatMode != chat.ModeNormal {
		t.Fatalf("chat mode = %d, want normal after first escape", model.chat.component.ChatMode)
	}
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("first escape canceled the chat request")
	}

	// Second Escape: cancel the agent call.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model, _ = resolveChatMessage(model, <-response)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("chat request remains loading after cancellation")
	}
	if model.Status != "AI request canceled" {
		t.Fatalf("status = %q, want cancellation status", model.Status)
	}
}

func TestChat_paletteOnlyShowsAICommandsWhenConfigured(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	for _, item := range newCommandPalette(model).items {
		if item.id == "ai.toggle" || item.id == "focus.chat" {
			t.Fatalf("unconfigured palette includes %q", item.id)
		}
	}

	model.SetAI(fakeChatClient{}, nil)
	model.Focus = focusChat
	foundToggle, foundShareResults, foundHistory := false, false, false
	for _, item := range newCommandPalette(model).items {
		switch item.id {
		case "ai.toggle":
			foundToggle = true
		case "chat.share_results":
			foundShareResults = true
		case "chat.history":
			foundHistory = true
		}
	}
	if !foundToggle {
		t.Fatal("configured palette does not include AI toggle")
	}
	if foundShareResults {
		t.Fatal("chat palette still includes result-sharing toggle")
	}
	if foundHistory {
		t.Fatal("chat palette still includes chat history command")
	}
}

// exhaustClient reaches the hard tool-call fuse, then checks finalization disables tools.
type exhaustClient struct {
	round         int
	finalToolsNil bool
}

func (c *exhaustClient) AgentForPrompt(string) string { return "assistant" }
func (c *exhaustClient) SupportsTools(string) bool    { return true }
func (c *exhaustClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}
func (c *exhaustClient) Chat(_ context.Context, _ ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Chat response"}, nil
}
func (c *exhaustClient) ChatStream(_ context.Context, _ ai.Request) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent)
	close(ch)
	return ch, nil
}
func (c *exhaustClient) Complete(_ context.Context, req ai.Request) (ai.Response, error) {
	c.round++
	if c.round <= chat.MaxToolCalls {
		return ai.Response{Agent: "Assistant", ToolCalls: []ai.ToolCall{{
			ID: fmt.Sprintf("call_exhaust_%d", c.round), Name: "get_connection_info",
			Input: map[string]any{"page": c.round},
		}}}, nil
	}
	if len(req.Tools) != 0 {
		return ai.Response{}, errors.New("expected nil/empty Tools on finalization")
	}
	c.finalToolsNil = true
	return ai.Response{Agent: "Assistant", Content: "final answer after a long tool run"}, nil
}

type deadlineClient struct {
	calls         int
	finalToolsNil bool
}

func (c *deadlineClient) AgentForPrompt(string) string { return "assistant" }
func (c *deadlineClient) SupportsTools(string) bool    { return true }
func (c *deadlineClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}
func (c *deadlineClient) Chat(_ context.Context, _ ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Chat response"}, nil
}
func (c *deadlineClient) ChatStream(_ context.Context, _ ai.Request) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent)
	close(ch)
	return ch, nil
}
func (c *deadlineClient) Complete(_ context.Context, req ai.Request) (ai.Response, error) {
	c.calls++
	if c.calls == 1 {
		return ai.Response{}, context.DeadlineExceeded
	}
	if len(req.Tools) != 0 {
		return ai.Response{}, errors.New("expected nil/empty Tools after tool deadline")
	}
	c.finalToolsNil = true
	return ai.Response{Agent: "Assistant", Content: "final answer after tool deadline"}, nil
}

// toolChatClient simulates an OpenAI provider that uses tools.
type toolChatClient struct {
	round int
}

func (c *toolChatClient) AgentForPrompt(string) string { return "assistant" }
func (c *toolChatClient) SupportsTools(string) bool    { return true }
func (c *toolChatClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}
func (c *toolChatClient) Chat(_ context.Context, _ ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Chat response"}, nil
}
func (c *toolChatClient) ChatStream(_ context.Context, _ ai.Request) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent)
	close(ch)
	return ch, nil
}
func (c *toolChatClient) Complete(_ context.Context, req ai.Request) (ai.Response, error) {
	c.round++
	if c.round == 1 {
		return ai.Response{Agent: "Assistant", ToolCalls: []ai.ToolCall{{
			ID: "call_test", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}}}, nil
	}
	if c.round == 2 {
		// Verify the request has assistant tool-call message + matching tool result.
		var gotAssistantCall, gotToolResult bool
		for _, m := range req.Messages {
			if m.Role == ai.RoleAssistant && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "call_test" {
				gotAssistantCall = true
			}
			if m.Role == ai.RoleTool && m.ToolID == "call_test" && m.Content != "" {
				gotToolResult = true
			}
		}
		if !gotAssistantCall {
			return ai.Response{}, errors.New("round 2 missing assistant tool-call message")
		}
		if !gotToolResult {
			return ai.Response{}, errors.New("round 2 missing tool result message")
		}
		return ai.Response{Agent: "Assistant", Content: "There is 1 row."}, nil
	}
	return ai.Response{Agent: "Assistant", Content: "Fallback"}, nil
}

// driveToolRoundToCompletion feeds tool-round messages until loading is cleared.
func driveToolRoundToCompletion(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil && model.chat.component.ActiveRun().Loading {
		model, cmd = resolveChatCommand(model, cmd)
	}
	return model
}

func TestChat_runsToolRoundThenDeliversFinalAnswer(t *testing.T) {
	// Set up model with a real in-memory SQLite database.
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	tc := &toolChatClient{}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	// Send a prompt that triggers databaseTools.
	model.chat.component.Input.SetValue("count rows")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)

	var command tea.Cmd
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Drive the message-driven tool round to completion.
	model = driveToolRoundToCompletion(t, model, command)

	// Verify the final answer is displayed.
	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading after completion")
	}
	stripped := ansi.Strip(model.chat.component.Viewport.GetContent())
	if !strings.Contains(stripped, "There is 1 row.") {
		t.Fatalf("final viewport content = %q, want %q", stripped, "There is 1 row.")
	}

	// Verify the assistant message was appended to chat history.
	if len(model.chat.component.ActiveRun().Messages) < 2 {
		t.Fatalf("chat has %d messages, want at least 2", len(model.chat.component.ActiveRun().Messages))
	}
	last := model.chat.component.ActiveRun().Messages[len(model.chat.component.ActiveRun().Messages)-1]
	if last.Role != ai.RoleAssistant || last.Content != "There is 1 row." {
		t.Fatalf("last message = %#v, want assistant with final content", last)
	}
}

func TestChat_rendersTableWithinViewportWidth(t *testing.T) {
	tableMD := "Here are the record counts:\n\n" +
		"| Rank | Table | Row Count |\n" +
		"|------|-------|-----------|\n" +
		"| 🥇 | orderdetails | 2,996 |\n" +
		"| 🥈 | orders | 326 |\n" +
		"| 3 | payments | 273 |\n"

	for _, terminalWidth := range []int{140, 100, 80, 60} {
		t.Run(fmt.Sprintf("width_%d", terminalWidth), func(t *testing.T) {
			model := New(":memory:", context.Background(), nil, false)
			model.State = stateReady
			ctx := context.Background()
			service, err := openTestSQLite(ctx, ":memory:")
			if err != nil {
				t.Fatalf("opening test service: %v", err)
			}
			t.Cleanup(func() { _ = service.Close() })
			model.SetAI(fakeChatClient{}, nil)
			model.Database = service
			model.chat.component.Executor = chatExecutor{service: service}
			model.databaseInfo = service.Info()
			model.applyLayout(terminalWidth, 32)

			model.chat.component.ActiveRun().Messages = []ai.Message{
				{Role: ai.RoleUser, Content: "which tables has most records?"},
				{Role: ai.RoleAssistant, Content: tableMD},
			}
			model.chat.component.RefreshView()

			content := model.chat.component.Viewport.GetContent()
			lines := strings.Split(content, "\n")
			width := model.chat.component.Viewport.Width()

			for i, line := range lines {
				if strings.TrimSpace(ansi.Strip(line)) == "" {
					continue
				}
				visible := ansi.StringWidth(line)
				if visible > width {
					t.Errorf("line %d: visible width %d exceeds viewport width %d\n  raw=%q\n  stripped=%q",
						i, visible, width, line, ansi.Strip(line))
				}
			}

			// Assert table structure: multiple lines, box-drawing separators, no raw md.
			stripped := ansi.Strip(content)

			checkLines := strings.Split(stripped, "\n")
			nonEmpty := 0
			for _, l := range checkLines {
				if strings.TrimSpace(l) != "" {
					nonEmpty++
				}
			}
			if nonEmpty < 5 {
				t.Errorf("expected >= 5 non-empty lines (header+sep+3 rows), got %d", nonEmpty)
			}

			// Must contain box-drawing column separators.
			if !strings.Contains(content, "│") {
				t.Error("rendered content missing box-drawing column separators (│)")
			}

			// Must NOT contain raw GFM table separator syntax.
			if strings.Contains(stripped, "|---") || strings.Contains(stripped, "----|") {
				t.Error("rendered content contains raw markdown table separators instead of box-drawing")
			}

			// Must contain expected data values.
			if !strings.Contains(stripped, "orderdetails") && !strings.Contains(stripped, "orderde") {
				t.Error("missing expected table data content")
			}
		})
	}
}

func TestChat_exhaustedToolRoundsForcesAnswer(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	ec := &exhaustClient{}
	model.SetAI(ec, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	model.chat.component.Input.SetValue("investigate")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Drive the message-driven tool round to completion.
	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model still loading after all rounds")
	}
	if !ec.finalToolsNil {
		t.Fatal("final round was not reached or Tools was not nil")
	}
	stripped := ansi.Strip(model.chat.component.Viewport.GetContent())
	if !strings.Contains(stripped, "final answer") {
		t.Fatalf("viewport = %q, want final-answer content", stripped)
	}
}

func TestChat_toolDeadlineFinalizesOnFreshContext(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	client := &deadlineClient{}
	model.SetAI(client, nil)
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("investigate")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = driveToolRoundToCompletion(t, model, cmd)

	if !client.finalToolsNil {
		t.Fatal("tool deadline did not start a tools-disabled finalization")
	}
	if got := ansi.Strip(model.chat.component.Viewport.GetContent()); !strings.Contains(got, "final answer after tool") || !strings.Contains(got, "deadline") {
		t.Fatalf("viewport = %q, want final answer", got)
	}
	if model.Status != "Assistant response complete" {
		t.Fatalf("status = %q, want completed finalization", model.Status)
	}
}

func TestToolRoundState_repeatedToolResultTriggersFinalization(t *testing.T) {
	state := chat.ToolRoundState{}
	call := ai.ToolCall{Name: "get_connection_info", Input: map[string]any{"scope": "current"}}
	for range chat.RepeatedToolResultLimit - 1 {
		if state.RecordToolResult(call, "SQLite") {
			t.Fatal("repetition detected too early")
		}
	}
	if !state.RecordToolResult(call, "SQLite") {
		t.Fatal("expected repeated tool result detection")
	}
}

func TestToolRoundState_skipsUnexecutedCallsBeforeFinalization(t *testing.T) {
	state := chat.ToolRoundState{
		ToolCalls: []ai.ToolCall{
			{ID: "done", Name: "sql", Input: map[string]any{"query": "SELECT 1"}},
			{ID: "skip", Name: "sql", Input: map[string]any{"query": "SELECT 2"}},
		},
		NextCall: 1,
	}
	state.SkipRemainingToolCalls("tool budget ended")

	if state.NextCall != len(state.ToolCalls) || len(state.Messages) != 1 {
		t.Fatalf("state = %#v, want remaining tool call recorded", state)
	}
	if got := state.Messages[0]; got.ToolID != "skip" || got.Content != "Skipped: tool budget ended" {
		t.Fatalf("tool result = %#v", got)
	}
}

func TestAssistantBlockingToolDoesNotFreezeUI(t *testing.T) {
	ctx := context.Background()
	entered := make(chan struct{})
	db := &blockingDB{entered: entered}

	model := New("", ctx, nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.Database = db
	model.chat.component.Executor = chatExecutor{service: db}
	model.databaseInfo = sharedsql.DatabaseInfo{Product: "SQLite"}
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	gen := int64(1)
	model.chat.component.ActiveRun().Gen = gen
	model.chat.component.ActiveRun().Loading = true

	toolCtx, toolCancel := context.WithCancel(ctx)
	model.chat.component.ActiveRun().RoundState = &chat.ToolRoundState{
		Gen:         gen,
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "test"}},
		Client:      model.chat.component.Client,
		ChatContext: toolCtx,
		ToolCancel:  toolCancel,
		ToolCalls: []ai.ToolCall{{
			ID: "call_block", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}},
		ToolDeadline: time.Now().Add(30 * time.Second),
	}

	// Send chat.ToolContinueMsg — must return immediately with an async cmd.
	modelI, cmd := model.Update(chat.ToolContinueMsg{Gen: gen})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for async tool execution")
	}
	model = modelI.(Model)
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("model should still be loading")
	}
	if model.chat.component.ActiveRun().RoundState == nil {
		t.Fatal("roundState should still be present")
	}

	// Run the blocking cmd in a goroutine.
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	// Wait for the mock to enter the blocking call.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tool execution did not start within 5s")
	}

	// Send Escape — must cancel immediately without blocking.
	modelI, escCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if escCmd != nil {
		t.Fatal("Escape should not produce a cmd")
	}
	if model.chat.component.ActiveRun().RoundState != nil {
		t.Fatal("Escape should clear roundState")
	}

	// The blocking command must unblock via context cancellation.
	select {
	case msg := <-done:
		if result, ok := msg.(chat.ToolResultMsg); !ok {
			t.Fatalf("expected chat.ToolResultMsg, got %T", msg)
		} else if result.Err == "" {
			t.Fatal("expected error from canceled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocking tool did not unblock within 5s after cancel")
	}
}

func TestAssistant_fullScreenEscapeExitsInsertModeThenCancels(t *testing.T) {
	ctx := context.Background()
	rootCtx, rootCancel := context.WithCancel(ctx)
	toolCtx, toolCancel := context.WithCancel(rootCtx)

	model := New("", ctx, nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.layout.fullscreen = true
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	gen := int64(1)
	model.chat.component.ActiveRun().Gen = gen
	model.chat.component.ActiveRun().Loading = true
	model.chat.component.ActiveRun().Cancel = rootCancel
	model.chat.component.ChatMode = chat.ModeInsert
	model.chat.component.ActiveRun().RoundState = &chat.ToolRoundState{
		Gen:         gen,
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "test"}},
		Client:      model.chat.component.Client,
		ChatContext: toolCtx,
		ToolCancel:  toolCancel,
		ToolCalls: []ai.ToolCall{{
			ID: "call_esc", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}},
		ToolDeadline: time.Now().Add(30 * time.Second),
	}

	// First Escape: must exit insert mode, not cancel.
	modelI, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	assertOnlyNotificationTick(t, cmd)
	if model.chat.component.ChatMode != chat.ModeNormal {
		t.Fatal("first Escape should exit insert mode")
	}
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("first Escape should NOT interrupt loading")
	}
	if model.chat.component.ActiveRun().RoundState == nil {
		t.Fatal("first Escape should NOT clear roundState")
	}

	// Second Escape: must cancel the agent call.
	modelI, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if model.chat.component.ActiveRun().RoundState != nil {
		t.Fatal("second Escape should clear roundState")
	}
	if !model.chat.component.ActiveRun().Canceled {
		t.Fatal("second Escape should mark canceled")
	}
}

func TestAssistant_fullscreenTransitionPreservesEscapeOrder(t *testing.T) {
	ctx := context.Background()

	model := New("", ctx, nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	// Toggle fullscreen via keybinding (works in normal mode).
	modelI, _ := model.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	model = modelI.(Model)
	if !model.layout.fullscreen {
		t.Fatal("expected fullscreen after toggle")
	}

	rootCtx, rootCancel := context.WithCancel(ctx)
	toolCtx, toolCancel := context.WithCancel(rootCtx)
	gen := int64(1)
	model.chat.component.ActiveRun().Gen = gen
	model.chat.component.ActiveRun().Loading = true
	model.chat.component.ActiveRun().Cancel = rootCancel
	model.chat.component.ChatMode = chat.ModeInsert
	model.chat.component.ActiveRun().RoundState = &chat.ToolRoundState{
		Gen:         gen,
		Messages:    []ai.Message{{Role: ai.RoleUser, Content: "test"}},
		Client:      model.chat.component.Client,
		ChatContext: toolCtx,
		ToolCancel:  toolCancel,
		ToolCalls: []ai.ToolCall{{
			ID: "call_t", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}},
		ToolDeadline: time.Now().Add(30 * time.Second),
	}

	// First Escape: exit insert mode, not cancel.
	modelI, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if model.chat.component.ChatMode != chat.ModeNormal {
		t.Fatal("first Escape should exit insert mode after fullscreen transition")
	}
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("first Escape should NOT interrupt loading after fullscreen transition")
	}
	if model.chat.component.ActiveRun().RoundState == nil {
		t.Fatal("first Escape should NOT clear roundState after fullscreen transition")
	}

	// Second Escape: cancel the agent call.
	modelI, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if model.chat.component.ActiveRun().RoundState != nil {
		t.Fatal("second Escape should clear roundState after fullscreen transition")
	}
	if !model.chat.component.ActiveRun().Canceled {
		t.Fatal("second Escape should mark canceled after fullscreen transition")
	}
}

type blockingDB struct {
	sharedsql.Service
	entered chan struct{}
}

func (d *blockingDB) Close() error { return nil }
func (d *blockingDB) Execute(_ context.Context, _ string) (sharedsql.Result, error) {
	return sharedsql.Result{}, nil
}
func (d *blockingDB) ExecuteReadOnly(ctx context.Context, _ string) (sharedsql.Result, error) {
	close(d.entered)
	<-ctx.Done()
	return sharedsql.Result{}, ctx.Err()
}
func (d *blockingDB) ListSchema(_ context.Context) ([]sharedsql.SchemaObject, error) { return nil, nil }
func (d *blockingDB) TableInfo(_ context.Context, _ string) ([]sharedsql.ColumnInfo, error) {
	return nil, nil
}
func (d *blockingDB) ListIndexes(_ context.Context, _ string) ([]sharedsql.IndexInfo, error) {
	return nil, nil
}

func TestChatContext_includesLastFailedQuery(t *testing.T) {
	model := New("", context.Background(), nil, false)
	model.State = stateReady

	// No failed queries → no error context.
	ctx := model.chat.component.ContextText(model.chatLayout())
	if strings.Contains(ctx, "Last failed query") {
		t.Fatal("context includes failed query when there are none")
	}

	// Add a failed query.
	model.queryLog.component.Entries = []queryLogEntry{
		{Statement: "SELECT * FROM nonexistent", Message: "no such table: nonexistent", Status: "failed"},
	}
	model.chat.component.LastFailedQuery = "SELECT * FROM nonexistent"
	model.chat.component.LastFailedError = "no such table: nonexistent"
	ctx = model.chat.component.ContextText(model.chatLayout())
	if !strings.Contains(ctx, "no such table: nonexistent") {
		t.Fatal("context should include first failed query error")
	}
	if !strings.Contains(ctx, "SELECT * FROM nonexistent") {
		t.Fatal("context should include failed query statement")
	}

	// Newer successful query does not hide the failure.
	model.queryLog.component.Entries = []queryLogEntry{
		{Statement: "SELECT 1", Status: "success"},
		{Statement: "SELECT * FROM nonexistent", Message: "no such table: nonexistent", Status: "failed"},
	}
	ctx = model.chat.component.ContextText(model.chatLayout())
	if !strings.Contains(ctx, "no such table: nonexistent") {
		t.Fatal("context should still include failed query even after a successful one")
	}

	// All successful → no error context.
	model.queryLog.component.Entries = []queryLogEntry{
		{Statement: "SELECT 1", Status: "success"},
	}
	model.refreshChatFailedContext()
	ctx = model.chat.component.ContextText(model.chatLayout())
	if strings.Contains(ctx, "Last failed query") {
		t.Fatal("context includes failed query when all succeeded")
	}
}

// TestChat_promptHistoryArrowRecall guards Up/Down recall of accepted user
// prompts: newest-to-oldest on Up with a blank input, no wrap at the oldest
// entry, newer on Down, a cleared input when Down leaves the newest entry,
// slash commands never entering recall, visible completion keeping arrows,
// and a changed draft exiting recall so Down cannot overwrite it.
func TestChat_promptHistoryArrowRecallAndEditExit(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.applyLayout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	submit := func(prompt string) {
		t.Helper()
		model.chat.component.Input.SetValue(prompt)
		updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)
		if command == nil {
			t.Fatalf("sending %q did not return a command", prompt)
		}
		model = driveStreamToCompletion(t, model, command)
	}
	submit("first question")
	submit("second question")

	// A slash command is accepted but must never enter recall history.
	model.chat.component.Input.SetValue("/new")
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	for _, message := range executeCommandAll(command) {
		if _, tick := message.(notification.DismissMsg); !tick {
			t.Fatalf("/new sent an unexpected message %T", message)
		}
	}

	press := func(code rune, text string) {
		t.Helper()
		updated, _ := model.Update(tea.KeyPressMsg{Code: code, Text: text})
		model = updated.(Model)
	}

	// When — Up recalls newest, then older, then stops at the oldest.
	press(tea.KeyUp, "")
	if got, want := model.chat.component.Input.Value(), "second question"; got != want {
		t.Fatalf("Up from blank = %q, want %q", got, want)
	}
	press(tea.KeyUp, "")
	if got, want := model.chat.component.Input.Value(), "first question"; got != want {
		t.Fatalf("second Up = %q, want %q", got, want)
	}
	press(tea.KeyUp, "")
	if got, want := model.chat.component.Input.Value(), "first question"; got != want {
		t.Fatalf("third Up = %q, want %q (oldest boundary, no wrap)", got, want)
	}

	// When — Down recalls newer, then clears.
	press(tea.KeyDown, "")
	if got, want := model.chat.component.Input.Value(), "second question"; got != want {
		t.Fatalf("first Down = %q, want %q", got, want)
	}
	press(tea.KeyDown, "")
	if got, want := model.chat.component.Input.Value(), ""; got != want {
		t.Fatalf("second Down = %q, want cleared input", got)
	}

	// When — a value-changing edit after recall exits recall mode.
	press(tea.KeyUp, "")
	if got, want := model.chat.component.Input.Value(), "second question"; got != want {
		t.Fatalf("recalled prompt = %q, want %q", got, want)
	}
	press('?', "?")
	if got, want := model.chat.component.Input.Value(), "second question?"; got != want {
		t.Fatalf("edited draft = %q, want %q", got, want)
	}
	press(tea.KeyDown, "")
	if got, want := model.chat.component.Input.Value(), "second question?"; got != want {
		t.Fatalf("Down after edit = %q, want %q (recall exited)", got, want)
	}

	// When — a visible slash completion keeps Up/Down for its selection.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // exit insert
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // re-enter insert
	model = updated.(Model)
	model.chat.component.Input.SetValue("")
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if !model.chat.component.Completion.Visible() {
		t.Fatal("completion should be visible for \"/\"")
	}
	press(tea.KeyDown, "")
	if got := model.chat.component.Completion.Selected; got != 1 {
		t.Fatalf("completion selected after Down = %d, want 1", got)
	}
	if got, want := model.chat.component.Input.Value(), "/"; got != want {
		t.Fatalf("input after completion Down = %q, want %q", got, want)
	}
	press(tea.KeyUp, "")
	if got := model.chat.component.Completion.Selected; got != 0 {
		t.Fatalf("completion selected after Up = %d, want 0", got)
	}
	if got, want := model.chat.component.Input.Value(), "/"; got != want {
		t.Fatalf("input after completion Up = %q, want %q", got, want)
	}
}

// TestChat_historyPersistenceIsConnectionScoped guards the end-to-end scope:
// a chat turn on one connection persists only under that connection's scope.
func TestChat_historyPersistenceIsConnectionScoped(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, history)
	model.connectionID = "conn-a"
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("How do I add an index?")

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("sending chat did not return a command")
	}
	model = driveStreamToCompletion(t, model, command)

	if conversations, err := history.Conversations(context.Background(), "conn-a"); err != nil || len(conversations) != 1 {
		t.Fatalf("conn-a conversations = %#v, err %v, want one", conversations, err)
	}
	if conversations, err := history.Conversations(context.Background(), "conn-b"); err != nil || len(conversations) != 0 {
		t.Fatalf("conn-b conversations = %#v, err %v, want none", conversations, err)
	}
	if messages, err := history.Messages(context.Background(), "conn-b", model.chat.component.ActiveID); err != nil || len(messages) != 0 {
		t.Fatalf("conn-b messages for conn-a conversation = %#v, err %v, want none", messages, err)
	}
}

// TestChat_persistenceRequiresConnectionScope guards the no-scope path: chat
// stays usable in memory, but nothing is persisted and the user is told
// history is unavailable.
func TestChat_persistenceRequiresConnectionScope(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, history) // history present, but no connection scope
	model.applyLayout(140, 32)
	model.chat.component.Input.SetValue("How do I add an index?")

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("chat without a connection scope must still send the request")
	}
	if model.Status != "AI conversation history is unavailable" {
		t.Fatalf("status = %q, want history-unavailable notice", model.Status)
	}
	model = driveStreamToCompletion(t, model, command)

	if len(model.chat.component.ActiveRun().Messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant in memory", model.chat.component.ActiveRun().Messages)
	}
	if model.chat.component.ActiveID != "" {
		t.Fatalf("active conversation = %q, want fresh unsent view without a scope", model.chat.component.ActiveID)
	}
	if conversations, err := history.Conversations(context.Background(), "any-scope"); err != nil || len(conversations) != 0 {
		t.Fatalf("persisted conversations = %#v, err %v, want none", conversations, err)
	}
}

// TestChat_connectionSwitchDropsStaleHistoryResults guards the async scope
// check: list, title, and delete results started on a previous connection must
// not touch the current connection's UI.
func TestChat_connectionSwitchDropsStaleHistoryResults(t *testing.T) {
	history, err := ai.OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, history)
	model.connectionID = "conn-a"
	model.applyLayout(140, 32)

	// A history load started on conn-a must not open the picker after conn-b
	// became active.
	cmd := model.chat.component.LoadHistory(model.chatLayout())
	model.connectionID = "conn-b"
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if model.chat.component.HistoryPicker != nil {
		t.Fatal("stale history load opened the picker on the new connection")
	}

	// A title failure from the old scope must not surface a status.
	updated, _ = model.Update(chat.TitleMsg{ConnectionID: "conn-a", ConversationID: "c1", Err: errors.New("boom")})
	model = updated.(Model)
	if model.Status != "" {
		t.Fatalf("stale title error surfaced: %q", model.Status)
	}

	// A delete result from the old scope must not clear, cancel runs, or
	// report anything.
	model.chat.component.Runs["c1"] = &chat.Run{ConversationID: "c1"}
	updated, _ = model.Update(chat.HistoryDeletedMsg{ConnectionID: "conn-a", Clear: true})
	model = updated.(Model)
	if model.Status != "" {
		t.Fatalf("stale delete result surfaced: %q", model.Status)
	}
	if model.chat.component.Runs["c1"] == nil {
		t.Fatal("stale clear wiped a live background run")
	}
}
