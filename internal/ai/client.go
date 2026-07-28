package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Request struct {
	AgentID  string
	Messages []Message
	Context  string
	Tools    []ToolDefinition
}

type Response struct {
	Agent     string
	Content   string
	ToolCalls []ToolCall
}

type StreamEventKind string

const (
	EventDelta    StreamEventKind = "delta"
	EventDone     StreamEventKind = "done"
	EventToolCall StreamEventKind = "tool_call"
)

// StreamEvent is an event on the channel returned by ChatStream.
type StreamEvent struct {
	Kind     StreamEventKind
	Delta    string    // incremental content token (empty for terminal events)
	Response *Response // non-nil on successful completion (carries Agent + full Content)
	Err      error     // non-nil on error
	ToolCall ToolCall  // populated when Kind is EventToolCall
}

type Client struct {
	config Config
	http   *http.Client
}

func NewClient(config Config) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if _, ok := config.Agents["assistant"]; !ok {
		return nil, fmt.Errorf("AI configuration requires an assistant agent")
	}
	return &Client{config: config, http: &http.Client{Timeout: time.Minute}}, nil
}

func (c *Client) AgentForPrompt(prompt string) string {
	lower := strings.ToLower(prompt)
	if agent, ok := c.config.Agents["oracle"]; ok && (strings.Contains(lower, "/premium") || strings.Contains(lower, "@oracle") || strings.Contains(lower, strings.ToLower(agent.Name)) || complexPrompt(lower)) {
		return "oracle"
	}
	if agent, ok := c.config.Agents["spark"]; ok && (strings.Contains(lower, "/lite") || strings.Contains(lower, "@spark") || strings.Contains(lower, strings.ToLower(agent.Name))) {
		return "spark"
	}
	return "assistant"
}

func complexPrompt(prompt string) bool {
	return len([]rune(prompt)) > 350 || strings.Contains(prompt, "migration") || strings.Contains(prompt, "multi-step") || strings.Contains(prompt, "reason") || strings.Contains(prompt, "analyze")
}

// SupportsTools reports whether the given agent's provider supports function calling.
func (c *Client) SupportsTools(agentID string) bool {
	agent, ok := c.config.Agents[agentID]
	if !ok {
		return false
	}
	provider, ok := c.config.Providers[agent.Provider]
	if !ok {
		return false
	}
	apiValue, err := ResolveValue(string(provider.API))
	if err != nil {
		return false
	}
	return API(apiValue) == APIOpenAI || API(apiValue) == APIOpenAICompatible
}

func (c *Client) Chat(ctx context.Context, request Request) (Response, error) {
	agent, ok := c.config.Agents[request.AgentID]
	if !ok {
		return Response{}, fmt.Errorf("unknown AI agent %q", request.AgentID)
	}
	provider := c.config.Providers[agent.Provider]
	baseURL, err := ResolveValue(provider.BaseURL)
	if err != nil {
		return Response{}, fmt.Errorf("provider %q base URL: %w", agent.Provider, err)
	}
	apiKey, err := ResolveValue(provider.APIKey)
	if err != nil {
		return Response{}, fmt.Errorf("provider %q API key: %w", agent.Provider, err)
	}
	model, err := ResolveValue(agent.Model)
	if err != nil {
		return Response{}, fmt.Errorf("agent %q model: %w", request.AgentID, err)
	}
	system, err := ResolveValue(agent.SystemPrompt)
	if err != nil {
		return Response{}, fmt.Errorf("agent %q system prompt: %w", request.AgentID, err)
	}
	apiValue, err := ResolveValue(string(provider.API))
	if err != nil {
		return Response{}, fmt.Errorf("provider %q API: %w", agent.Provider, err)
	}
	name, err := ResolveValue(agent.Name)
	if err != nil {
		return Response{}, fmt.Errorf("agent %q name: %w", request.AgentID, err)
	}
	if contextText := strings.TrimSpace(request.Context); contextText != "" {
		system = strings.TrimSpace(system + "\n\nDatabase context:\n" + contextText)
	}

	switch API(apiValue) {
	case APIOpenAI, APIOpenAICompatible:
		content, err := c.openAI(ctx, baseURL, apiKey, model, system, request.Messages)
		return Response{Agent: name, Content: content}, err
	case APIAnthropic:
		content, err := c.anthropic(ctx, baseURL, apiKey, model, system, request.Messages)
		return Response{Agent: name, Content: content}, err
	case APIGemini:
		content, err := c.gemini(ctx, baseURL, apiKey, model, system, request.Messages)
		return Response{Agent: name, Content: content}, err
	default:
		return Response{}, fmt.Errorf("provider %q has unsupported API %q", agent.Provider, apiValue)
	}
}

// Complete sends a non-streaming chat request with optional tool definitions and
// returns the parsed turn (text + tool calls). Provider-specific wire translation
// is handled per adapter.
func (c *Client) Complete(ctx context.Context, request Request) (Response, error) {
	agent, ok := c.config.Agents[request.AgentID]
	if !ok {
		return Response{}, fmt.Errorf("unknown AI agent %q", request.AgentID)
	}
	provider := c.config.Providers[agent.Provider]
	baseURL, err := ResolveValue(provider.BaseURL)
	if err != nil {
		return Response{}, fmt.Errorf("provider %q base URL: %w", agent.Provider, err)
	}
	apiKey, err := ResolveValue(provider.APIKey)
	if err != nil {
		return Response{}, fmt.Errorf("provider %q API key: %w", agent.Provider, err)
	}
	model, err := ResolveValue(agent.Model)
	if err != nil {
		return Response{}, fmt.Errorf("agent %q model: %w", request.AgentID, err)
	}
	system, err := ResolveValue(agent.SystemPrompt)
	if err != nil {
		return Response{}, fmt.Errorf("agent %q system prompt: %w", request.AgentID, err)
	}
	apiValue, err := ResolveValue(string(provider.API))
	if err != nil {
		return Response{}, fmt.Errorf("provider %q API: %w", agent.Provider, err)
	}
	name, err := ResolveValue(agent.Name)
	if err != nil {
		return Response{}, fmt.Errorf("agent %q name: %w", request.AgentID, err)
	}

	if contextText := strings.TrimSpace(request.Context); contextText != "" {
		system = strings.TrimSpace(system + "\n\nDatabase context:\n" + contextText)
	}

	switch API(apiValue) {
	case APIOpenAI, APIOpenAICompatible:
		res, err := c.completeOpenAI(ctx, baseURL, apiKey, model, system, request.Messages, request.Tools)
		res.Agent = name
		return res, err
	case APIAnthropic:
		if len(request.Tools) > 0 {
			return Response{}, fmt.Errorf("Anthropic tool support not yet implemented")
		}
		content, err := c.anthropic(ctx, baseURL, apiKey, model, system, request.Messages)
		return Response{Agent: name, Content: content}, err
	case APIGemini:
		if len(request.Tools) > 0 {
			return Response{}, fmt.Errorf("Gemini tool support not yet implemented")
		}
		content, err := c.gemini(ctx, baseURL, apiKey, model, system, request.Messages)
		return Response{Agent: name, Content: content}, err
	default:
		return Response{}, fmt.Errorf("provider %q has unsupported API %q", agent.Provider, apiValue)
	}
}

// ChatStream sends a streaming chat request, returning a channel of StreamEvents.
// The channel is safe to read until closed.
func (c *Client) ChatStream(ctx context.Context, request Request) (<-chan StreamEvent, error) {
	agent, ok := c.config.Agents[request.AgentID]
	if !ok {
		return nil, fmt.Errorf("unknown AI agent %q", request.AgentID)
	}
	provider := c.config.Providers[agent.Provider]
	baseURL, err := ResolveValue(provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("provider %q base URL: %w", agent.Provider, err)
	}
	apiKey, err := ResolveValue(provider.APIKey)
	if err != nil {
		return nil, fmt.Errorf("provider %q API key: %w", agent.Provider, err)
	}
	model, err := ResolveValue(agent.Model)
	if err != nil {
		return nil, fmt.Errorf("agent %q model: %w", request.AgentID, err)
	}
	system, err := ResolveValue(agent.SystemPrompt)
	if err != nil {
		return nil, fmt.Errorf("agent %q system prompt: %w", request.AgentID, err)
	}
	apiValue, err := ResolveValue(string(provider.API))
	if err != nil {
		return nil, fmt.Errorf("provider %q API: %w", agent.Provider, err)
	}
	name, err := ResolveValue(agent.Name)
	if err != nil {
		return nil, fmt.Errorf("agent %q name: %w", request.AgentID, err)
	}
	if contextText := strings.TrimSpace(request.Context); contextText != "" {
		system = strings.TrimSpace(system + "\n\nDatabase context:\n" + contextText)
	}

	var eventCh <-chan StreamEvent
	switch API(apiValue) {
	case APIOpenAI, APIOpenAICompatible:
		eventCh, err = c.openAIStream(ctx, baseURL, apiKey, model, system, request.Messages)
	case APIAnthropic:
		eventCh, err = c.anthropicStream(ctx, baseURL, apiKey, model, system, request.Messages)
	case APIGemini:
		eventCh, err = c.geminiStream(ctx, baseURL, apiKey, model, system, request.Messages)
	default:
		return nil, fmt.Errorf("provider %q has unsupported API %q", agent.Provider, apiValue)
	}
	if err != nil {
		return nil, err
	}
	// Wrap provider events with agent name and accumulate content for final Response.
	ch := make(chan StreamEvent, 5)
	go func() {
		defer close(ch)
		var buf strings.Builder
		for ev := range eventCh {
			buf.WriteString(ev.Delta)
			if ev.Err != nil {
				select {
				case ch <- StreamEvent{Err: ev.Err}:
				case <-ctx.Done():
				}
				return
			}
			if ev.Delta != "" {
				select {
				case ch <- StreamEvent{Delta: ev.Delta}:
				case <-ctx.Done():
					return
				}
			}
		}
		// Successful completion
		select {
		case ch <- StreamEvent{Response: &Response{Agent: name, Content: buf.String()}}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}
