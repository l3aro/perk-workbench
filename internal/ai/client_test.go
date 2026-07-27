package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_chatOpenAICompatibleReturnsAssistantResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"Use an index."}}]}`)
	}))
	t.Cleanup(server.Close)
	config := Config{
		Providers: map[string]Provider{"cloud": {
			Name:    "Cloud",
			API:     APIOpenAICompatible,
			BaseURL: server.URL + "/v1",
			APIKey:  "test-key",
			Models:  []string{"small"},
		}},
		Agents: map[string]Agent{"assistant": {
			Name: "Assistant", Provider: "cloud", Model: "small", SystemPrompt: "Help with SQL.",
		}},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Chat(context.Background(), Request{AgentID: "assistant", Messages: []Message{{Role: RoleUser, Content: "How can this query be faster?"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Agent != "Assistant" || response.Content != "Use an index." {
		t.Fatalf("response = %#v", response)
	}
}
