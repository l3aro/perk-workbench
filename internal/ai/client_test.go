package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_GenerateTitlePrefersSparkFallsBackToAssistant(t *testing.T) {
	var models []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct{ Model string }
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		models = append(models, payload.Model)
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"\"Cheap title\"\n"}}]}`)
	}))
	t.Cleanup(server.Close)
	config := Config{
		Providers: map[string]Provider{"cloud": {
			Name: "Cloud", API: APIOpenAICompatible, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []string{"small", "flash"},
		}},
		Agents: map[string]Agent{
			"assistant": {Name: "Assistant", Provider: "cloud", Model: "small", SystemPrompt: "Help."},
			"spark":     {Name: "Spark", Provider: "cloud", Model: "flash", SystemPrompt: "Help."},
		},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	title, err := client.GenerateTitle(context.Background(), "how do I speed this up?")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Cheap title" {
		t.Fatalf("title = %q, want trimmed first line without quotes", title)
	}
	if len(models) != 1 || models[0] != "flash" {
		t.Fatalf("models = %v, want spark's flash model", models)
	}

	config.Agents = map[string]Agent{"assistant": {Name: "Assistant", Provider: "cloud", Model: "small", SystemPrompt: "Help."}}
	client, err = NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GenerateTitle(context.Background(), "how do I speed this up?"); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[1] != "small" {
		t.Fatalf("models = %v, want assistant's small model as fallback", models)
	}
}

func TestCleanChatTitle_firstLineQuotesAndCap(t *testing.T) {
	if got := cleanChatTitle("\n   \n  first\nsecond"); got != "first" {
		t.Fatalf("first line = %q", got)
	}
	if got := cleanChatTitle("  \u201cquoted\u201d  "); got != "quoted" {
		t.Fatalf("quotes = %q", got)
	}
	long := strings.Repeat("字", 120)
	if got := cleanChatTitle(long); got != strings.Repeat("字", 97)+"..." {
		t.Fatalf("capped = %q", got)
	}
	if got := cleanChatTitle(" \n \n "); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

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

func TestClient_chatStreamOpenAIEmitsDeltasThenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"Use \"}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"an \"}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"index.\"}}]}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
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

	eventCh, err := client.ChatStream(context.Background(), Request{AgentID: "assistant", Messages: []Message{{Role: RoleUser, Content: "How can this query be faster?"}}})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	var finalResponse *Response
	for ev := range eventCh {
		if ev.Delta != "" {
			deltas = append(deltas, ev.Delta)
		}
		if ev.Response != nil {
			finalResponse = ev.Response
		}
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if len(deltas) != 3 || deltas[0] != "Use " || deltas[1] != "an " || deltas[2] != "index." {
		t.Fatalf("deltas = %#v", deltas)
	}
	if finalResponse == nil || finalResponse.Agent != "Assistant" || finalResponse.Content != "Use an index." {
		t.Fatalf("final response = %#v", finalResponse)
	}
}

func TestClient_chatStreamOpenAIMissingDoneEmitsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"Partial\"}}]}\n\n")
		// No [DONE] — connection just closes
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

	eventCh, err := client.ChatStream(context.Background(), Request{AgentID: "assistant", Messages: []Message{{Role: RoleUser, Content: "query"}}})
	if err != nil {
		t.Fatal(err)
	}

	var finalErr error
	for ev := range eventCh {
		if ev.Err != nil {
			finalErr = ev.Err
		}
	}
	if finalErr == nil || !strings.Contains(finalErr.Error(), "truncated") {
		t.Fatalf("expected truncated error, got %v", finalErr)
	}
}

func TestClient_chatStreamAnthropicEmitsDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "event: content_block_delta\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Use \"}}\n\n")
		_, _ = io.WriteString(writer, "event: content_block_delta\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"an index.\"}}\n\n")
		_, _ = io.WriteString(writer, "event: message_stop\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	t.Cleanup(server.Close)
	config := Config{
		Providers: map[string]Provider{"claude": {
			Name: "Claude", API: APIAnthropic, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []string{"haiku"},
		}},
		Agents: map[string]Agent{"assistant": {
			Name: "Assistant", Provider: "claude", Model: "haiku", SystemPrompt: "Help.",
		}},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	eventCh, err := client.ChatStream(context.Background(), Request{AgentID: "assistant", Messages: []Message{{Role: RoleUser, Content: "Help me"}}})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	var finalResponse *Response
	for ev := range eventCh {
		if ev.Delta != "" {
			deltas = append(deltas, ev.Delta)
		}
		if ev.Response != nil {
			finalResponse = ev.Response
		}
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if len(deltas) != 2 || deltas[0] != "Use " || deltas[1] != "an index." {
		t.Fatalf("deltas = %#v", deltas)
	}
	if finalResponse == nil || finalResponse.Content != "Use an index." {
		t.Fatalf("final response = %#v", finalResponse)
	}
}

func TestClient_chatStreamGeminiEmitsDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.Path, ":streamGenerateContent") {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Use \"}]}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"an index.\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	t.Cleanup(server.Close)
	config := Config{
		Providers: map[string]Provider{"gem": {
			Name: "Gem", API: APIGemini, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []string{"flash"},
		}},
		Agents: map[string]Agent{"assistant": {
			Name: "Assistant", Provider: "gem", Model: "flash", SystemPrompt: "Help.",
		}},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	eventCh, err := client.ChatStream(context.Background(), Request{AgentID: "assistant", Messages: []Message{{Role: RoleUser, Content: "Help me"}}})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	var finalResponse *Response
	for ev := range eventCh {
		if ev.Delta != "" {
			deltas = append(deltas, ev.Delta)
		}
		if ev.Response != nil {
			finalResponse = ev.Response
		}
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if len(deltas) != 2 || deltas[0] != "Use " || deltas[1] != "an index." {
		t.Fatalf("deltas = %#v", deltas)
	}
	if finalResponse == nil || finalResponse.Content != "Use an index." {
		t.Fatalf("final response = %#v", finalResponse)
	}
}

func TestClient_chatStreamGeminiMissingFinishEmitsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Partial\"}]}}]}\n\n")
		// No finishReason — stream closes
	}))
	t.Cleanup(server.Close)
	config := Config{
		Providers: map[string]Provider{"gem": {
			Name: "Gem", API: APIGemini, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []string{"flash"},
		}},
		Agents: map[string]Agent{"assistant": {
			Name: "Assistant", Provider: "gem", Model: "flash", SystemPrompt: "Help.",
		}},
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	eventCh, err := client.ChatStream(context.Background(), Request{AgentID: "assistant", Messages: []Message{{Role: RoleUser, Content: "query"}}})
	if err != nil {
		t.Fatal(err)
	}

	var finalErr error
	for ev := range eventCh {
		if ev.Err != nil {
			finalErr = ev.Err
		}
	}
	if finalErr == nil || !strings.Contains(finalErr.Error(), "truncated") {
		t.Fatalf("expected truncated error, got %v", finalErr)
	}
}
