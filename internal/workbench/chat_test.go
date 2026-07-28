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
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/sqlite"
)

type fakeChatClient struct{}

func (fakeChatClient) AgentForPrompt(string) string { return "assistant" }

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

func TestChat_streamingRendersPartialContent(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
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
	msg := command()
	updated, cmd := model.Update(msg)
	model = updated.(Model)
	stripped := ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "Add ") {
		t.Fatalf("after first delta = %q, want \"Add \"", stripped)
	}
	// Label should say "streaming"
	if !strings.Contains(stripped, "streaming") {
		t.Fatalf("viewport = %q, want streaming label", stripped)
	}
	if model.chat.loading != true {
		t.Fatal("model should still be loading after first delta")
	}
	if model.chat.streamBuffer != "Add " {
		t.Fatalf("streamBuffer = %q", model.chat.streamBuffer)
	}

	// Second delta
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)
	stripped = ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "Add an ") {
		t.Fatalf("after second delta = %q, want \"Add an \"", stripped)
	}

	// Third delta
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// Completion
	msg = cmd()
	updated, _ = model.Update(msg)
	model = updated.(Model)

	if model.chat.loading {
		t.Fatal("model should not be loading after completion")
	}
	if len(model.chat.messages) != 2 {
		t.Fatalf("messages = %#v, want 2", model.chat.messages)
	}
	if model.chat.messages[1].Content != "Add an index." {
		t.Fatalf("response = %q", model.chat.messages[1].Content)
	}
}

func TestChat_enterSendsPromptAndRendersResponse(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
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

	if len(model.chat.messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant messages", model.chat.messages)
	}
	if got := model.chat.messages[1].Content; got != "Add an index." {
		t.Fatalf("assistant response = %q", got)
	}
	if strings.Contains(model.chat.viewport.GetContent(), "Assistant") {
		t.Fatal("chat response renders an assistant label")
	}
}

// driveStreamToCompletion feeds streaming events until the stream completes.
func driveStreamToCompletion(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		updated, nextCmd := model.Update(msg)
		model = updated.(Model)
		cmd = nextCmd
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
	model := New(":memory:", context.Background(), nil)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(client, history)
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

	if len(model.chat.messages) != 2 || model.chat.messages[1].Content != "Use an index." {
		t.Fatalf("messages = %#v", model.chat.messages)
	}
	// Check that the persisted conversation includes both messages.
	persisted, err := history.Messages(context.Background(), model.chat.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 2 {
		t.Fatalf("persisted messages = %#v", persisted)
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
	model := New(":memory:", context.Background(), nil)
	model.State = stateReady
	model.layout(140, 32)
	if model.chat.visible {
		t.Fatal("AI pane is visible without an Assistant")
	}

	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)
	if !model.chat.visible || model.editorWidth != 72 {
		t.Fatalf("AI pane = visible:%t editorWidth:%d", model.chat.visible, model.editorWidth)
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if model.chat.visible || model.editorWidth != 108 {
		t.Fatalf("AI pane after toggle = visible:%t editorWidth:%d", model.chat.visible, model.editorWidth)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if !model.chat.visible || model.editorWidth != 72 {
		t.Fatalf("AI pane after second toggle = visible:%t editorWidth:%d", model.chat.visible, model.editorWidth)
	}
}

func TestChat_inputPaddingPreservesPaneHeight(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
	model.State = stateReady
	model.SetAI(fakeChatClient{}, nil)
	model.layout(140, 32)

	if got, want := lipgloss.Height(model.chatContentView()), model.height-4; got != want {
		t.Fatalf("chat content height = %d, want %d", got, want)
	}
}

func TestChat_fullscreenUserMessageFillsViewport(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
	model.State, model.Focus = stateReady, focusChat
	model.SetAI(fakeChatClient{}, nil)
	model.fullscreen = true
	model.layout(140, 32)
	if got, want := model.chat.viewport.Width(), model.width-6; got != want {
		t.Fatalf("chat viewport width = %d, want pane interior width %d", got, want)
	}
	model.chat.messages = []ai.Message{{Role: ai.RoleUser, Content: "full width"}}
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

func TestChat_contextExcludesResultRowsByDefault(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
	model.results.SetRows([]table.Row{{"secret"}})

	if context := model.chatContext(); strings.Contains(context, "Visible results:") {
		t.Fatalf("chat context = %q, want no visible results", context)
	}
}

func TestChat_contextIncludesResultRowsWhenEnabled(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
	model.State, model.Focus = stateReady, focusChat
	model.results.SetRows([]table.Row{{"secret"}})
	updated, _ := model.handlePaletteCommand("chat.share_results")
	model = updated.(Model)

	if context := model.chatContext(); !strings.Contains(context, "Visible results:\nsecret") {
		t.Fatalf("chat context = %q, want visible results", context)
	}
}

func TestChat_escapeCancelsActiveRequest(t *testing.T) {
	started := make(chan struct{})
	model := New(":memory:", context.Background(), nil)
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
	go func() { response <- command() }()
	<-started

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	if model.chat.chatMode != formModeNormal {
		t.Fatalf("chat mode = %d, want normal after first escape", model.chat.chatMode)
	}
	if !model.chat.loading {
		t.Fatal("first escape canceled the chat request")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(<-response)
	model = updated.(Model)

	if model.chat.loading {
		t.Fatal("chat request remains loading after cancellation")
	}
	if model.Status != "AI request canceled" {
		t.Fatalf("status = %q, want cancellation status", model.Status)
	}
}

func TestChat_paletteOnlyShowsAICommandsWhenConfigured(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
	model.State = stateReady
	for _, item := range newCommandPalette(model).items {
		if item.id == "ai.toggle" || item.id == "focus.chat" {
			t.Fatalf("unconfigured palette includes %q", item.id)
		}
	}

	model.SetAI(fakeChatClient{}, nil)
	model.Focus = focusChat
	foundToggle, foundShareResults := false, false
	for _, item := range newCommandPalette(model).items {
		switch item.id {
		case "ai.toggle":
			foundToggle = true
		case "chat.share_results":
			foundShareResults = true
		}
	}
	if !foundToggle {
		t.Fatal("configured palette does not include AI toggle")
	}
	if !foundShareResults {
		t.Fatal("chat palette does not include result-sharing toggle")
	}
}

// toolChatClient simulates an OpenAI provider that uses tools.
type toolChatClient struct {
	round int
}

func (c *toolChatClient) AgentForPrompt(string) string { return "assistant" }
func (c *toolChatClient) SupportsTools(string) bool    { return true }
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
			ID: "call_test", Name: "sql",
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

func TestChat_runsToolRoundThenDeliversFinalAnswer(t *testing.T) {
	// Set up model with a real in-memory SQLite database.
	ctx := context.Background()
	service, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil)
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

	// Round 1: Complete returns a tool call → synthetic delta event.
	msg := command()
	updated, cmd := model.Update(msg)
	model = updated.(Model)

	// Round 2: tool results appended, second Complete returns final answer → synthetic done.
	msg = cmd()
	updated, _ = model.Update(msg)
	model = updated.(Model)

	// Verify the final answer is displayed.
	if model.chat.loading {
		t.Fatal("model should not be loading after completion")
	}
	stripped := ansi.Strip(model.chat.viewport.GetContent())
	if !strings.Contains(stripped, "There is 1 row.") {
		t.Fatalf("final viewport content = %q, want %q", stripped, "There is 1 row.")
	}

	// Verify the assistant message was appended to chat history.
	if len(model.chat.messages) < 2 {
		t.Fatalf("chat has %d messages, want at least 2", len(model.chat.messages))
	}
	last := model.chat.messages[len(model.chat.messages)-1]
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
			model := New(":memory:", context.Background(), nil)
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

			model.chat.messages = []ai.Message{
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
