package workbench

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
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

// TestChat_spinnerWhileLoading guards the assistant progress spinner: it must
// render right-aligned in the chat mode line only while loading, advance on
// each tick, re-arm while loading, and stop when loading ends.
func TestChat_spinnerWhileLoading(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	if strings.Contains(model.chatModeBadge(), "⠋") {
		t.Fatal("spinner shown while idle")
	}

	model.chat.activeRun().loading = true
	model.chat.activeRun().spinnerFrame = 1
	badge := model.chatModeBadge()
	if !strings.Contains(badge, "⠙") {
		t.Fatalf("badge = %q, want spinner frame while loading", badge)
	}
	if !strings.Contains(ansi.Strip(badge), "NORMAL") {
		t.Fatalf("badge = %q, want NORMAL badge on the left", badge)
	}

	updated, cmd := model.Update(chatSpinnerTickMsg{})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("tick while loading should re-arm the spinner")
	}
	if model.chat.activeRun().spinnerFrame != 2 {
		t.Fatalf("spinnerFrame = %d, want 2", model.chat.activeRun().spinnerFrame)
	}

	model.chat.activeRun().loading = false
	updated, cmd = model.Update(chatSpinnerTickMsg{})
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("tick after loading should not re-arm")
	}
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
	model.layout(140, 32)

	model.chat.yoloWrites = true
	model.chat.activeRun().loading = true
	model.chat.activeRun().spinnerFrame = 2
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
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'})
	model = updated.(Model)
	model.chat.input.SetValue("talk like a pirate")

	// Send
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// First delta — assert partial content appears
	model, cmd := resolveChatCommand(model, command)
	stripped := ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "Add ") {
		t.Fatalf("after first delta = %q, want \"Add \"", stripped)
	}
	// Label should say "streaming"
	if !strings.Contains(stripped, "streaming") {
		t.Fatalf("viewport = %q, want streaming label", stripped)
	}
	if model.chat.activeRun().loading != true {
		t.Fatal("model should still be loading after first delta")
	}
	if model.chat.activeRun().streamBuffer != "Add " {
		t.Fatalf("streamBuffer = %q", model.chat.activeRun().streamBuffer)
	}

	// Second delta
	model, cmd = resolveChatCommand(model, cmd)
	stripped = ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "Add an ") {
		t.Fatalf("after second delta = %q, want \"Add an \"", stripped)
	}

	// Third delta
	model, cmd = resolveChatCommand(model, cmd)

	// Completion
	model, _ = resolveChatCommand(model, cmd)

	if model.chat.activeRun().loading {
		t.Fatal("model should not be loading after completion")
	}
	if len(model.chat.activeRun().messages) != 2 {
		t.Fatalf("messages = %#v, want 2", model.chat.activeRun().messages)
	}
	if model.chat.activeRun().messages[1].Content != "Add an index." {
		t.Fatalf("response = %q", model.chat.activeRun().messages[1].Content)
	}
}

// TestChat_slashNewStartsNewConversation guards the /new slash command: it
// must clear messages and the conversation ID without sending a request.
func TestChat_slashNewStartsNewConversation(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	model.chat.activeID = "existing"
	model.chat.activeRun().messages = []ai.Message{{Role: ai.RoleUser, Content: "old turn"}}

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	model.chat.input.SetValue("/new")

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if command != nil {
		t.Fatal("/new must not send a request")
	}
	if model.chat.activeID != "" {
		t.Fatalf("conversation ID = %q, want cleared", model.chat.activeID)
	}
	if len(model.chat.activeRun().messages) != 0 {
		t.Fatalf("messages = %#v, want cleared", model.chat.activeRun().messages)
	}
	if got := model.chat.input.Value(); got != "" {
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
	model.layout(140, 32)

	// Conversation A: send a prompt whose stream blocks until released.
	model.chat.input.SetValue("slow question")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmdA := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmdA == nil {
		t.Fatal("sending chat A did not return a command")
	}
	idA := model.chat.activeID
	if idA == "" {
		t.Fatal("conversation A was not created")
	}
	streamA := make(chan tea.Msg, 1)
	go runChatCommand(cmdA, streamA)
	<-client.started // A's stream is blocked and waiting

	// /new while A runs: fresh view, A keeps running in the background.
	model.chat.input.SetValue("/new")
	updated, cmdNew := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmdNew != nil {
		t.Fatal("/new must not send a request")
	}
	if model.chat.activeID != "" {
		t.Fatalf("active conversation = %q, want fresh view", model.chat.activeID)
	}
	if !model.chat.runs[idA].loading {
		t.Fatal("switching to /new interrupted conversation A")
	}

	// Conversation B: send and complete while A is still running.
	model.chat.input.SetValue("fast question")
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmdB := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	idB := model.chat.activeID
	if idB == "" || idB == idA {
		t.Fatalf("conversation B id = %q, want distinct from A %q", idB, idA)
	}
	model = driveStreamToCompletion(t, model, cmdB)

	if !model.chat.runs[idA].loading {
		t.Fatal("completing B interrupted conversation A")
	}
	if len(model.chat.runs[idB].messages) != 2 || model.chat.runs[idB].messages[1].Content != "fast answer" {
		t.Fatalf("B messages = %#v", model.chat.runs[idB].messages)
	}

	// Switch back to A: open the /history picker, then drive the selection
	// handoff its submit produces (huh's own key handling is library
	// plumbing; the load command it emits is the product contract).
	model.chat.input.SetValue("/history")
	updated, cmdHistory := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmdHistory == nil {
		t.Fatal("/history sent no command")
	}
	model, cmdHistory = resolveChatCommand(model, cmdHistory)
	if model.chatHistoryPicker == nil {
		t.Fatal("/history did not open the conversation picker")
	}
	model.chat.historyChoice = idA // what a selection in the form binds
	model.chatHistoryPicker = nil  // what the submit handler does first
	cmdLoad := model.loadChatMessages(idA)
	updated, _ = model.Update(cmdLoad())
	model = updated.(Model)
	if model.chat.activeID != idA {
		t.Fatalf("active conversation = %q, want A", model.chat.activeID)
	}
	if !model.chat.activeRun().loading {
		t.Fatal("A should still be loading when switching back")
	}
	if model.chat.activeRun().messages[0].Content != "slow question" {
		t.Fatalf("A view = %#v, want the slow question", model.chat.activeRun().messages)
	}

	// Release A's stream and drive it to completion.
	close(client.release)
	model, cmd := resolveChatMessage(model, <-streamA)
	for cmd != nil {
		model, cmd = resolveChatCommand(model, cmd)
	}
	if model.chat.activeRun().loading {
		t.Fatal("A still loading after stream completion")
	}
	if len(model.chat.activeRun().messages) != 2 || model.chat.activeRun().messages[1].Content != "slow answer" {
		t.Fatalf("A messages = %#v", model.chat.activeRun().messages)
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
	model.layout(140, 32)

	loadA := model.loadChatMessages(a.ID) // seq 1
	loadB := model.loadChatMessages(b.ID) // seq 2

	// B's load lands first and activates B.
	updated, _ := model.Update(loadB())
	model = updated.(Model)
	if model.chat.activeID != b.ID {
		t.Fatalf("active conversation = %q, want B", model.chat.activeID)
	}

	// A's stale load lands late and must be dropped.
	updated, _ = model.Update(loadA())
	model = updated.(Model)
	if model.chat.activeID != b.ID {
		t.Fatalf("stale load switched active conversation to %q, want B", model.chat.activeID)
	}
}

// TestChat_historyOptionLabelPrefixesRunningConversations guards the /history
// picker labels: a conversation whose agent run is active gets a spinner
// glyph prefix, idle ones do not.
func TestChat_historyOptionLabelPrefixesRunningConversations(t *testing.T) {
	if got := chatHistoryOptionLabel(nil, "old chat"); got != "old chat" {
		t.Fatalf("idle label = %q", got)
	}
	if got := chatHistoryOptionLabel(&chatRun{}, "old chat"); got != "old chat" {
		t.Fatalf("idle run label = %q", got)
	}
	run := &chatRun{loading: true, spinnerFrame: 1}
	if got := chatHistoryOptionLabel(run, "old chat"); got != "⠙ old chat" {
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
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	// Type "/n".
	for _, ch := range []rune{'/', 'n'} {
		updated, _ = model.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		model = updated.(Model)
	}

	if !model.chat.completion.visible() {
		t.Fatal("completion should be visible for \"/n\"")
	}
	if item := model.chat.completion.accept(); item.Label != "/new" || item.Kind != KindCommand {
		t.Fatalf("suggestion = %#v, want /new command", item)
	}
	if got := ansi.Strip(model.chatContentView()); !strings.Contains(got, "command") {
		t.Fatal("overlay does not show the command type")
	}
	linesWithOverlay := len(strings.Split(ansi.Strip(model.chatContentView()), "\n"))

	// Tab accepts: input becomes "/new", completion hides.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if got := model.chat.input.Value(); got != "/new" {
		t.Fatalf("input = %q, want /new", got)
	}
	if model.chat.completion.visible() {
		t.Fatal("completion should hide after accept")
	}
	// The dropdown overlays the viewport: content height must not change.
	if lines := len(strings.Split(ansi.Strip(model.chatContentView()), "\n")); lines != linesWithOverlay {
		t.Fatalf("content lines = %d with overlay, %d after, want equal", linesWithOverlay, lines)
	}

	// Enter runs the /new command: no request, messages cleared.
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("Enter after /new must not send a request")
	}
	if len(model.chat.activeRun().messages) != 0 {
		t.Fatalf("messages = %#v, want cleared", model.chat.activeRun().messages)
	}
}

// TestChat_completionArrowNavigation guards dropdown navigation: Up/Down and
// Ctrl+K/Ctrl+J move the selection (wrapping), typing refilters, and Tab
// accepts the selected item.
func TestChat_completionArrowNavigation(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if got := len(model.chat.completion.matches); got != 4 {
		t.Fatalf("matches = %d, want 4", got)
	}

	move := func(code rune, mod tea.KeyMod) {
		updated, _ = model.Update(tea.KeyPressMsg{Code: code, Mod: mod})
		model = updated.(Model)
	}
	move(tea.KeyDown, 0)
	if got := model.chat.completion.selected; got != 1 {
		t.Fatalf("selected after Down = %d, want 1", got)
	}
	// A no-text event (the terminal's key-release echo) must not reset it.
	updated, _ = model.Update(tea.KeyReleaseMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if got := model.chat.completion.selected; got != 1 {
		t.Fatalf("selected after key release = %d, want 1", got)
	}
	move(tea.KeyDown, 0)
	if got := model.chat.completion.selected; got != 2 {
		t.Fatalf("selected after 2x Down = %d, want 2", got)
	}
	move(tea.KeyDown, 0)
	if got := model.chat.completion.selected; got != 3 {
		t.Fatalf("selected after 3x Down = %d, want 3", got)
	}
	// Wraps around the list.
	move(tea.KeyDown, 0)
	if got := model.chat.completion.selected; got != 0 {
		t.Fatalf("selected after wrap = %d, want 0", got)
	}
	// Up arrow walks back.
	move(tea.KeyUp, 0)
	if got := model.chat.completion.selected; got != 3 {
		t.Fatalf("selected after Up = %d, want 3", got)
	}
	// Ctrl+J / Ctrl+K behave like Down/Up.
	move('j', tea.ModCtrl)
	if got := model.chat.completion.selected; got != 0 {
		t.Fatalf("selected after Ctrl+J = %d, want 0", got)
	}
	move('k', tea.ModCtrl)
	if got := model.chat.completion.selected; got != 3 {
		t.Fatalf("selected after Ctrl+K = %d, want 3", got)
	}

	// Tab accepts the selected item, not the first one.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	want := "/share-results"
	if got := model.chat.input.Value(); got != want {
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
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	model.chat.input.SetValue("/history")
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("/history sent no command")
	}
	msg := command()
	if _, ok := msg.(chatHistoryLoadedMsg); !ok {
		t.Fatalf("command returned %T, want chatHistoryLoadedMsg", msg)
	}
	updated, _ = model.Update(msg)
	model = updated.(Model)
	if model.chatHistoryPicker == nil {
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
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	// Off state suggests only "/yolo-on".
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	matches := model.chat.completion.matches
	if len(matches) != 1 || matches[0].Label != "/yolo-on" || matches[0].Kind != KindCommand {
		t.Fatalf("matches = %#v, want only /yolo-on command", matches)
	}

	// Tab accepts; Enter runs it without sending a request.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if got := model.chat.input.Value(); got != "/yolo-on" {
		t.Fatalf("input = %q, want /yolo-on", got)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("YOLO command must not send a request")
	}
	if !model.chat.yoloWrites {
		t.Fatal("yoloWrites = false, want true")
	}

	// On state suggests only "/yolo-off".
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	matches = model.chat.completion.matches
	if len(matches) != 1 || matches[0].Label != "/yolo-off" {
		t.Fatalf("matches = %#v, want only /yolo-off", matches)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("YOLO command must not send a request")
	}
	if model.chat.yoloWrites {
		t.Fatal("yoloWrites = true, want false")
	}

	// Prose mentioning YOLO must be sent to the AI, not executed as a command.
	model.chat.input.SetValue("turn on YOLO")
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("prose prompt must be sent to the AI")
	}
	if model.chat.yoloWrites {
		t.Fatal("prose prompt toggled yoloWrites")
	}
}

func TestChat_enterSendsPromptAndRendersResponse(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'})
	model = updated.(Model)
	if model.Focus != focusChat {
		t.Fatalf("focus = %v, want chat", model.Focus)
	}
	model.chat.input.SetValue("How should I speed this up?")

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

	if len(model.chat.activeRun().messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant messages", model.chat.activeRun().messages)
	}
	if got := model.chat.activeRun().messages[1].Content; got != "Add an index." {
		t.Fatalf("assistant response = %q", got)
	}
	if strings.Contains(model.chat.viewport.GetContent(), "Assistant") {
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
		var last tea.Cmd
		for _, child := range msg {
			var next tea.Cmd
			model, next = resolveChatCommand(model, child)
			if next != nil {
				last = next
			}
		}
		return model, last
	case chatSpinnerTickMsg:
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
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, child := range batch {
			if result := child(); result != nil {
				if _, tick := result.(chatSpinnerTickMsg); !tick {
					ch <- result
					return
				}
			}
		}
		return
	}
	ch <- message
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
	model.layout(140, 32)
	model.chat.input.SetValue("How should I speed this up?")

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

	if len(model.chat.activeRun().messages) != 2 || model.chat.activeRun().messages[1].Content != "Use an index." {
		t.Fatalf("messages = %#v", model.chat.activeRun().messages)
	}
	// Check that the persisted conversation includes both messages.
	persisted, err := history.Messages(context.Background(), "test-connection", model.chat.activeID)
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
	model.layout(140, 32)
	model.chat.input.SetValue("How do I add an index?")

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

	statement := chatSQL(messages)
	if statement != "CREATE TABLE projects (id integer);" {
		t.Fatalf("statement = %q", statement)
	}
}

func TestChat_toggleVisibilityChangesPaneLayout(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.layout(140, 32)
	if model.chat.visible {
		t.Fatal("AI pane is visible without an Assistant")
	}

	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)
	if !model.chat.visible || model.editorWidth != 58 {
		t.Fatalf("AI pane = visible:%t editorWidth:%d", model.chat.visible, model.editorWidth)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if model.chat.visible || model.editorWidth != 94 {
		t.Fatalf("AI pane after toggle = visible:%t editorWidth:%d", model.chat.visible, model.editorWidth)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if !model.chat.visible || model.editorWidth != 58 {
		t.Fatalf("AI pane after second toggle = visible:%t editorWidth:%d", model.chat.visible, model.editorWidth)
	}
}

func TestChat_inputPaddingPreservesPaneHeight(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	if got, want := lipgloss.Height(model.chatContentView()), model.height-4; got != want {
		t.Fatalf("chat content height = %d, want %d", got, want)
	}
}

func TestChat_fullscreenUserMessageFillsViewport(t *testing.T) {
	model := New(":memory:", context.Background(), nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, nil)
	model.fullscreen = true
	model.layout(140, 32)
	if got, want := model.chat.viewport.Width(), model.width-6; got != want {
		t.Fatalf("chat viewport width = %d, want pane interior width %d", got, want)
	}
	model.chat.activeRun().messages = []ai.Message{{Role: ai.RoleUser, Content: "full width"}}
	model.refreshChatView()

	content := model.chat.viewport.GetContent()
	if strings.Contains(content, "\n") {
		t.Fatalf("user message content = %q, want one rendered row", content)
	}
	line := content
	if got, want := ansi.StringWidth(line), model.chat.viewport.Width(); got != want {
		t.Fatalf("user message width = %d, want viewport width %d", got, want)
	}
	if !strings.Contains(line, "\u00a0") {
		t.Fatal("user message uses clearable ASCII space padding")
	}
	viewLine, _, _ := strings.Cut(model.chat.viewport.View(), "\n")
	if !strings.Contains(viewLine, "full width") {
		t.Fatalf("user message viewport line = %q, want prompt text beside its accent", viewLine)
	}
}

func TestChat_shareResultsTool(t *testing.T) {
	model := readyModel(t)
	model.State, model.Focus = stateReady, focusChat
	model.results.SetColumns(tableColumns([]string{"id", "name"}, []table.Row{{"1", "alice"}}))
	model.results.SetRows([]table.Row{{"1", "alice"}})

	// Sharing is off by default: no tool, no results block in context.
	if hasTool(model.databaseTools(), "get_visible_results") {
		t.Fatal("get_visible_results present while sharing is off")
	}
	if context := model.chatContext(); strings.Contains(context, "Visible results:") {
		t.Fatalf("chat context = %q, want no visible results block", context)
	}

	// Sharing on via the AI slash command: the tool appears and returns the rows.
	model.chat.input.SetValue("/share-results")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("share command must not send a request")
	}
	if !model.chat.shareResults {
		t.Fatal("shareResults should be true after command")
	}
	if !hasTool(model.databaseTools(), "get_visible_results") {
		t.Fatal("get_visible_results missing while sharing is on")
	}
	result := model.executeTool(context.Background(), ai.ToolCall{ID: "call_1", Name: "get_visible_results"})
	if result.Error != "" || !strings.Contains(result.Content, "id | name") || !strings.Contains(result.Content, "1 | alice") {
		t.Fatalf("tool result = %#v", result)
	}

	// Sharing off again: the tool disappears.
	model.chat.input.SetValue("/unshare-results")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.chat.shareResults {
		t.Fatal("shareResults should be false after stop command")
	}
	if hasTool(model.databaseTools(), "get_visible_results") {
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
	model.layout(140, 32)
	model.results.SetColumns(tableColumns([]string{"id", "name"}, []table.Row{{"1", "alice"}}))
	model.results.SetRows([]table.Row{{"1", "alice"}})

	// Enable sharing via the AI slash command.
	model.chat.input.SetValue("/share-results")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("share command must not send a request")
	}

	model.chat.input.SetValue("what do the results say?")
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
	model.layout(140, 32)
	model.chat.input.SetValue("cancel this request")

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

	if model.chat.chatMode != formModeNormal {
		t.Fatalf("chat mode = %d, want normal after first escape", model.chat.chatMode)
	}
	if !model.chat.activeRun().loading {
		t.Fatal("first escape canceled the chat request")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model, _ = resolveChatMessage(model, <-response)

	if model.chat.activeRun().loading {
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
	model.fullscreen = true
	model.SetAI(waitingChatClient{started: started}, nil)
	model.layout(140, 32)
	model.chat.input.SetValue("cancel this request")

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
	if model.chat.chatMode != formModeNormal {
		t.Fatalf("chat mode = %d, want normal after first escape", model.chat.chatMode)
	}
	if !model.chat.activeRun().loading {
		t.Fatal("first escape canceled the chat request")
	}

	// Second Escape: cancel the agent call.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	model, _ = resolveChatMessage(model, <-response)

	if model.chat.activeRun().loading {
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
	if c.round <= assistantMaxToolCalls {
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
	for cmd != nil && model.chat.activeRun().loading {
		model, cmd = resolveChatCommand(model, cmd)
	}
	return model
}

func TestChat_runsToolRoundThenDeliversFinalAnswer(t *testing.T) {
	// Set up model with a real in-memory SQLite database.
	ctx := context.Background()
	service, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

	tc := &toolChatClient{}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.layout(140, 32)

	// Send a prompt that triggers databaseTools.
	model.chat.input.SetValue("count rows")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)

	var command tea.Cmd
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Drive the message-driven tool round to completion.
	model = driveToolRoundToCompletion(t, model, command)

	// Verify the final answer is displayed.
	if model.chat.activeRun().loading {
		t.Fatal("model should not be loading after completion")
	}
	stripped := ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "There is 1 row.") {
		t.Fatalf("final viewport content = %q, want %q", stripped, "There is 1 row.")
	}

	// Verify the assistant message was appended to chat history.
	if len(model.chat.activeRun().messages) < 2 {
		t.Fatalf("chat has %d messages, want at least 2", len(model.chat.activeRun().messages))
	}
	last := model.chat.activeRun().messages[len(model.chat.activeRun().messages)-1]
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
			service, err := sqlite.Open(ctx, ":memory:")
			if err != nil {
				t.Fatalf("opening test service: %v", err)
			}
			t.Cleanup(func() { _ = service.Close() })
			model.SetAI(fakeChatClient{}, nil)
			model.Database = service
			model.databaseInfo = service.Info()
			model.layout(terminalWidth, 32)

			model.chat.activeRun().messages = []ai.Message{
				{Role: ai.RoleUser, Content: "which tables has most records?"},
				{Role: ai.RoleAssistant, Content: tableMD},
			}
			model.refreshChatView()

			content := model.chat.viewport.GetContent()
			lines := strings.Split(content, "\n")
			width := model.chat.viewport.Width()

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
	service, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

	ec := &exhaustClient{}
	model.SetAI(ec, nil)
	model.Focus = focusChat
	model.layout(140, 32)

	model.chat.input.SetValue("investigate")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Drive the message-driven tool round to completion.
	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.activeRun().loading {
		t.Fatal("model still loading after all rounds")
	}
	if !ec.finalToolsNil {
		t.Fatal("final round was not reached or Tools was not nil")
	}
	stripped := ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "final answer") {
		t.Fatalf("viewport = %q, want final-answer content", stripped)
	}
}

func TestChat_toolDeadlineFinalizesOnFreshContext(t *testing.T) {
	ctx := context.Background()
	service, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.Database = service
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

	client := &deadlineClient{}
	model.SetAI(client, nil)
	model.layout(140, 32)
	model.chat.input.SetValue("investigate")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = driveToolRoundToCompletion(t, model, cmd)

	if !client.finalToolsNil {
		t.Fatal("tool deadline did not start a tools-disabled finalization")
	}
	if got := ansi.Strip(model.chat.viewport.GetContent()); !strings.Contains(got, "final answer after tool") || !strings.Contains(got, "deadline") {
		t.Fatalf("viewport = %q, want final answer", got)
	}
	if model.Status != "Assistant response complete" {
		t.Fatalf("status = %q, want completed finalization", model.Status)
	}
}

func TestToolRoundState_repeatedToolResultTriggersFinalization(t *testing.T) {
	state := toolRoundState{}
	call := ai.ToolCall{Name: "get_connection_info", Input: map[string]any{"scope": "current"}}
	for range assistantRepeatedToolResultLimit - 1 {
		if state.recordToolResult(call, "SQLite") {
			t.Fatal("repetition detected too early")
		}
	}
	if !state.recordToolResult(call, "SQLite") {
		t.Fatal("expected repeated tool result detection")
	}
}

func TestToolRoundState_skipsUnexecutedCallsBeforeFinalization(t *testing.T) {
	state := toolRoundState{
		toolCalls: []ai.ToolCall{
			{ID: "done", Name: "sql", Input: map[string]any{"query": "SELECT 1"}},
			{ID: "skip", Name: "sql", Input: map[string]any{"query": "SELECT 2"}},
		},
		nextCall: 1,
	}
	state.skipRemainingToolCalls("tool budget ended")

	if state.nextCall != len(state.toolCalls) || len(state.messages) != 1 {
		t.Fatalf("state = %#v, want remaining tool call recorded", state)
	}
	if got := state.messages[0]; got.ToolID != "skip" || got.Content != "Skipped: tool budget ended" {
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
	model.databaseInfo = sharedsql.DatabaseInfo{Product: "SQLite"}
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	gen := int64(1)
	model.chat.activeRun().gen = gen
	model.chat.activeRun().loading = true

	toolCtx, toolCancel := context.WithCancel(ctx)
	model.chat.activeRun().roundState = &toolRoundState{
		gen:         gen,
		messages:    []ai.Message{{Role: ai.RoleUser, Content: "test"}},
		client:      model.chat.client,
		chatContext: toolCtx,
		toolCancel:  toolCancel,
		toolCalls: []ai.ToolCall{{
			ID: "call_block", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}},
		toolDeadline: time.Now().Add(30 * time.Second),
	}

	// Send assistantToolContinueMsg — must return immediately with an async cmd.
	modelI, cmd := model.Update(assistantToolContinueMsg{gen: gen})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for async tool execution")
	}
	model = modelI.(Model)
	if !model.chat.activeRun().loading {
		t.Fatal("model should still be loading")
	}
	if model.chat.activeRun().roundState == nil {
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
	if model.chat.activeRun().roundState != nil {
		t.Fatal("Escape should clear roundState")
	}

	// The blocking command must unblock via context cancellation.
	select {
	case msg := <-done:
		if result, ok := msg.(assistantToolResultMsg); !ok {
			t.Fatalf("expected assistantToolResultMsg, got %T", msg)
		} else if result.err == "" {
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
	model.fullscreen = true
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	gen := int64(1)
	model.chat.activeRun().gen = gen
	model.chat.activeRun().loading = true
	model.chat.activeRun().cancel = rootCancel
	model.chat.chatMode = formModeInsert
	model.chat.activeRun().roundState = &toolRoundState{
		gen:         gen,
		messages:    []ai.Message{{Role: ai.RoleUser, Content: "test"}},
		client:      model.chat.client,
		chatContext: toolCtx,
		toolCancel:  toolCancel,
		toolCalls: []ai.ToolCall{{
			ID: "call_esc", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}},
		toolDeadline: time.Now().Add(30 * time.Second),
	}

	// First Escape: must exit insert mode, not cancel.
	modelI, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if cmd != nil {
		t.Fatal("Escape from insert mode should not produce a cmd")
	}
	if model.chat.chatMode != formModeNormal {
		t.Fatal("first Escape should exit insert mode")
	}
	if !model.chat.activeRun().loading {
		t.Fatal("first Escape should NOT interrupt loading")
	}
	if model.chat.activeRun().roundState == nil {
		t.Fatal("first Escape should NOT clear roundState")
	}

	// Second Escape: must cancel the agent call.
	modelI, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if model.chat.activeRun().roundState != nil {
		t.Fatal("second Escape should clear roundState")
	}
	if !model.chat.activeRun().canceled {
		t.Fatal("second Escape should mark canceled")
	}
}

func TestAssistant_fullscreenTransitionPreservesEscapeOrder(t *testing.T) {
	ctx := context.Background()

	model := New("", ctx, nil, false)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	// Toggle fullscreen via keybinding (works in normal mode).
	modelI, _ := model.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	model = modelI.(Model)
	if !model.fullscreen {
		t.Fatal("expected fullscreen after toggle")
	}

	rootCtx, rootCancel := context.WithCancel(ctx)
	toolCtx, toolCancel := context.WithCancel(rootCtx)
	gen := int64(1)
	model.chat.activeRun().gen = gen
	model.chat.activeRun().loading = true
	model.chat.activeRun().cancel = rootCancel
	model.chat.chatMode = formModeInsert
	model.chat.activeRun().roundState = &toolRoundState{
		gen:         gen,
		messages:    []ai.Message{{Role: ai.RoleUser, Content: "test"}},
		client:      model.chat.client,
		chatContext: toolCtx,
		toolCancel:  toolCancel,
		toolCalls: []ai.ToolCall{{
			ID: "call_t", Name: "sql_read",
			Input: map[string]any{"query": "SELECT 1"},
		}},
		toolDeadline: time.Now().Add(30 * time.Second),
	}

	// First Escape: exit insert mode, not cancel.
	modelI, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if model.chat.chatMode != formModeNormal {
		t.Fatal("first Escape should exit insert mode after fullscreen transition")
	}
	if !model.chat.activeRun().loading {
		t.Fatal("first Escape should NOT interrupt loading after fullscreen transition")
	}
	if model.chat.activeRun().roundState == nil {
		t.Fatal("first Escape should NOT clear roundState after fullscreen transition")
	}

	// Second Escape: cancel the agent call.
	modelI, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = modelI.(Model)
	if model.chat.activeRun().roundState != nil {
		t.Fatal("second Escape should clear roundState after fullscreen transition")
	}
	if !model.chat.activeRun().canceled {
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
	ctx := model.chatContext()
	if strings.Contains(ctx, "Last failed query") {
		t.Fatal("context includes failed query when there are none")
	}

	// Add a failed query.
	model.queryLogEntries = []queryLogEntry{
		{statement: "SELECT * FROM nonexistent", message: "no such table: nonexistent", status: "failed"},
	}
	ctx = model.chatContext()
	if !strings.Contains(ctx, "no such table: nonexistent") {
		t.Fatal("context should include first failed query error")
	}
	if !strings.Contains(ctx, "SELECT * FROM nonexistent") {
		t.Fatal("context should include failed query statement")
	}

	// Newer successful query does not hide the failure.
	model.queryLogEntries = []queryLogEntry{
		{statement: "SELECT 1", status: "success"},
		{statement: "SELECT * FROM nonexistent", message: "no such table: nonexistent", status: "failed"},
	}
	ctx = model.chatContext()
	if !strings.Contains(ctx, "no such table: nonexistent") {
		t.Fatal("context should still include failed query even after a successful one")
	}

	// All successful → no error context.
	model.queryLogEntries = []queryLogEntry{
		{statement: "SELECT 1", status: "success"},
	}
	ctx = model.chatContext()
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
	model.layout(140, 32)

	updated, _ := model.Update(tea.KeyPressMsg{Code: '4'}) // focus chat
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // enter insert
	model = updated.(Model)

	submit := func(prompt string) {
		t.Helper()
		model.chat.input.SetValue(prompt)
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
	model.chat.input.SetValue("/new")
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("/new must not send a request")
	}

	press := func(code rune, text string) {
		t.Helper()
		updated, _ := model.Update(tea.KeyPressMsg{Code: code, Text: text})
		model = updated.(Model)
	}

	// When — Up recalls newest, then older, then stops at the oldest.
	press(tea.KeyUp, "")
	if got, want := model.chat.input.Value(), "second question"; got != want {
		t.Fatalf("Up from blank = %q, want %q", got, want)
	}
	press(tea.KeyUp, "")
	if got, want := model.chat.input.Value(), "first question"; got != want {
		t.Fatalf("second Up = %q, want %q", got, want)
	}
	press(tea.KeyUp, "")
	if got, want := model.chat.input.Value(), "first question"; got != want {
		t.Fatalf("third Up = %q, want %q (oldest boundary, no wrap)", got, want)
	}

	// When — Down recalls newer, then clears.
	press(tea.KeyDown, "")
	if got, want := model.chat.input.Value(), "second question"; got != want {
		t.Fatalf("first Down = %q, want %q", got, want)
	}
	press(tea.KeyDown, "")
	if got, want := model.chat.input.Value(), ""; got != want {
		t.Fatalf("second Down = %q, want cleared input", got)
	}

	// When — a value-changing edit after recall exits recall mode.
	press(tea.KeyUp, "")
	if got, want := model.chat.input.Value(), "second question"; got != want {
		t.Fatalf("recalled prompt = %q, want %q", got, want)
	}
	press('?', "?")
	if got, want := model.chat.input.Value(), "second question?"; got != want {
		t.Fatalf("edited draft = %q, want %q", got, want)
	}
	press(tea.KeyDown, "")
	if got, want := model.chat.input.Value(), "second question?"; got != want {
		t.Fatalf("Down after edit = %q, want %q (recall exited)", got, want)
	}

	// When — a visible slash completion keeps Up/Down for its selection.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // exit insert
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i'}) // re-enter insert
	model = updated.(Model)
	model.chat.input.SetValue("")
	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	if !model.chat.completion.visible() {
		t.Fatal("completion should be visible for \"/\"")
	}
	press(tea.KeyDown, "")
	if got := model.chat.completion.selected; got != 1 {
		t.Fatalf("completion selected after Down = %d, want 1", got)
	}
	if got, want := model.chat.input.Value(), "/"; got != want {
		t.Fatalf("input after completion Down = %q, want %q", got, want)
	}
	press(tea.KeyUp, "")
	if got := model.chat.completion.selected; got != 0 {
		t.Fatalf("completion selected after Up = %d, want 0", got)
	}
	if got, want := model.chat.input.Value(), "/"; got != want {
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
	model.layout(140, 32)
	model.chat.input.SetValue("How do I add an index?")

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
	if messages, err := history.Messages(context.Background(), "conn-b", model.chat.activeID); err != nil || len(messages) != 0 {
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
	model.layout(140, 32)
	model.chat.input.SetValue("How do I add an index?")

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

	if len(model.chat.activeRun().messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant in memory", model.chat.activeRun().messages)
	}
	if model.chat.activeID != "" {
		t.Fatalf("active conversation = %q, want fresh unsent view without a scope", model.chat.activeID)
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
	model.layout(140, 32)

	// A history load started on conn-a must not open the picker after conn-b
	// became active.
	cmd := model.loadChatHistory()
	model.connectionID = "conn-b"
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if model.chatHistoryPicker != nil {
		t.Fatal("stale history load opened the picker on the new connection")
	}

	// A title failure from the old scope must not surface a status.
	updated, _ = model.Update(chatTitleMsg{connectionID: "conn-a", conversationID: "c1", err: errors.New("boom")})
	model = updated.(Model)
	if model.Status != "" {
		t.Fatalf("stale title error surfaced: %q", model.Status)
	}

	// A delete result from the old scope must not clear, cancel runs, or
	// report anything.
	model.chat.runs["c1"] = &chatRun{conversationID: "c1"}
	updated, _ = model.Update(chatHistoryDeletedMsg{connectionID: "conn-a", clear: true})
	model = updated.(Model)
	if model.Status != "" {
		t.Fatalf("stale delete result surfaced: %q", model.Status)
	}
	if model.chat.runs["c1"] == nil {
		t.Fatal("stale clear wiped a live background run")
	}
}
