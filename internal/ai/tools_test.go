package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_completeOpenAIReturnsToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", request.URL.Path)
		}

		// Verify tools in request.
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if len(body.Tools) == 0 {
			t.Fatal("expected tools in request")
		}
		if body.Tools[0].Function.Name != "sql" {
			t.Fatalf("first tool name = %q, want %q", body.Tools[0].Function.Name, "sql")
		}

		// Respond with a tool call.
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_abc123","type":"function","function":{"name":"sql","arguments":"{\"query\":\"SELECT 1\"}"}}]}}]}`)
	}))
	t.Cleanup(server.Close)

	config := Config{
		Providers: map[string]Provider{"cloud": {
			Name: "Cloud", API: APIOpenAICompatible, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []string{"small"},
		}},
		Agents: map[string]Agent{"assistant": {
			Name: "Assistant", Provider: "cloud", Model: "small", SystemPrompt: "Help with SQL.",
		}},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Complete(context.Background(), Request{
		AgentID:  "assistant",
		Messages: []Message{{Role: RoleUser, Content: "What's in the database?"}},
		Tools: []ToolDefinition{
			{Name: "sql", Description: "Run a query", InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]any{"type": "string"}},
				"required":   []string{"query"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(response.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want 1 call", response.ToolCalls)
	}
	tc := response.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Fatalf("ToolCall.ID = %q, want %q", tc.ID, "call_abc123")
	}
	if tc.Name != "sql" {
		t.Fatalf("ToolCall.Name = %q, want %q", tc.Name, "sql")
	}
	query, ok := tc.Input["query"].(string)
	if !ok || query != "SELECT 1" {
		t.Fatalf("ToolCall.Input[query] = %v, want %q", tc.Input["query"], "SELECT 1")
	}
}

func TestClient_completeOpenAIWithToolResults(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, body)

		// First request: return a tool call.
		if len(requests) == 1 {
			_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"sql","arguments":"{\"query\":\"SELECT 1\"}"}}]}}]}`)
			return
		}
		// Second request: return text answer (tool results have been sent).
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"There is 1 row."}}]}`)
	}))
	t.Cleanup(server.Close)

	config := Config{
		Providers: map[string]Provider{"cloud": {
			Name: "Cloud", API: APIOpenAICompatible, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []string{"small"},
		}},
		Agents: map[string]Agent{"assistant": {
			Name: "Assistant", Provider: "cloud", Model: "small", SystemPrompt: "Help.",
		}},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	// Round 1: get a tool call back.
	res1, err := client.Complete(context.Background(), Request{
		AgentID:  "assistant",
		Messages: []Message{{Role: RoleUser, Content: "count rows"}},
		Tools: []ToolDefinition{{
			Name: "sql", Description: "run query", InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]any{"type": "string"}},
				"required":   []string{"query"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.ToolCalls) != 1 {
		t.Fatalf("round 1: got %d tool calls, want 1", len(res1.ToolCalls))
	}

	// Round 2: send assistant tool-call message + tool result, expect text answer.
	res2, err := client.Complete(context.Background(), Request{
		AgentID: "assistant",
		Messages: []Message{
			{Role: RoleUser, Content: "count rows"},
			{Role: RoleAssistant, Content: "", ToolCalls: res1.ToolCalls},
			{Role: RoleTool, ToolID: "call_1", ToolName: "sql", Content: "1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Content != "There is 1 row." {
		t.Fatalf("round 2 content = %q, want %q", res2.Content, "There is 1 row.")
	}

	// Verify round 2 request contains tool_call_id for the tool message.
	if len(requests) < 2 {
		t.Fatal("expected at least 2 requests")
	}
	var round2 struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id,omitempty"`
			Content    string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(requests[1], &round2); err != nil {
		t.Fatalf("decoding round 2: %v", err)
	}

	var foundToolResult bool
	for _, m := range round2.Messages {
		if m.Role == "tool" {
			foundToolResult = true
			if m.ToolCallID != "call_1" {
				t.Fatalf("tool message tool_call_id = %q, want %q", m.ToolCallID, "call_1")
			}
			if m.Content != "1" {
				t.Fatalf("tool message content = %q, want %q", m.Content, "1")
			}
		}
	}
	if !foundToolResult {
		t.Fatal("round 2 request missing a tool message")
	}
}
