package workbench

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
)

type fakeChatClient struct{}

func (fakeChatClient) AgentForPrompt(string) string { return "assistant" }

func (fakeChatClient) Chat(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Add an index."}, nil
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
	updated, _ = model.Update(command())
	model = updated.(Model)

	if len(model.chat.messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant messages", model.chat.messages)
	}
	if got := model.chat.messages[1].Content; got != "Add an index." {
		t.Fatalf("assistant response = %q", got)
	}
}

func TestChat_realProviderResponsePersistsConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"Use an index."}}]}`)
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
	updated, _ = model.Update(command())
	model = updated.(Model)

	if len(model.chat.messages) != 2 || model.chat.messages[1].Content != "Use an index." {
		t.Fatalf("messages = %#v", model.chat.messages)
	}
	messages, err := history.Messages(context.Background(), model.chat.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("persisted messages = %#v", messages)
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

func TestChat_paletteOnlyShowsAICommandsWhenConfigured(t *testing.T) {
	model := New(":memory:", context.Background(), nil)
	model.State = stateReady
	for _, item := range newCommandPalette(model).items {
		if item.id == "ai.toggle" || item.id == "focus.chat" {
			t.Fatalf("unconfigured palette includes %q", item.id)
		}
	}

	model.SetAI(fakeChatClient{}, nil)
	foundToggle := false
	for _, item := range newCommandPalette(model).items {
		if item.id == "ai.toggle" {
			foundToggle = true
		}
	}
	if !foundToggle {
		t.Fatal("configured palette does not include AI toggle")
	}
}
