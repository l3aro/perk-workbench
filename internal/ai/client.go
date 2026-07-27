package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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

// Turn is the result of one non-streaming provider request round.
// It carries either text content, tool calls, or both.
func (c *Client) completeOpenAI(ctx context.Context, baseURL, apiKey, model, system string, messages []Message, tools []ToolDefinition) (Response, error) {
	// OpenAI-specific types — order matters for local type scoping.
	type openAIToolCall struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	type openAIMsg struct {
		Role       string           `json:"role"`
		Content    string           `json:"content,omitempty"`
		ToolCallID string           `json:"tool_call_id,omitempty"`
		ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	}

	// Build request messages.
	omsgs := make([]openAIMsg, 0, len(messages)+1)
	if system != "" {
		omsgs = append(omsgs, openAIMsg{Role: "system", Content: system})
	}
	for _, msg := range messages {
		switch msg.Role {
		case RoleTool:
			omsgs = append(omsgs, openAIMsg{
				Role:       "tool",
				ToolCallID: msg.ToolID,
				Content:    msg.Content,
			})
		case RoleAssistant:
			om := openAIMsg{Role: "assistant", Content: msg.Content}
			if len(msg.ToolCalls) > 0 {
				om.ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					args, err := json.Marshal(tc.Input)
					if err != nil {
						args = []byte("{}")
					}
					om.ToolCalls[i] = openAIToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      tc.Name,
							Arguments: string(args),
						},
					}
				}
			}
			omsgs = append(omsgs, om)
		default:
			omsgs = append(omsgs, openAIMsg{Role: string(msg.Role), Content: msg.Content})
		}
	}

	// OpenAI tool schema.
	type toolSchema struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	}

	payload := struct {
		Model    string       `json:"model"`
		Messages []openAIMsg  `json:"messages"`
		Tools    []toolSchema `json:"tools,omitempty"`
	}{Model: model, Messages: omsgs}

	if len(tools) > 0 {
		payload.Tools = make([]toolSchema, len(tools))
		for i, t := range tools {
			payload.Tools[i] = toolSchema{
				Type: "function",
				Function: struct {
					Name        string         `json:"name"`
					Description string         `json:"description"`
					Parameters  map[string]any `json:"parameters"`
				}{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			}
		}
	}

	// Response type — uses the same openAIToolCall for parsing tool_calls.
	type choiceMsg struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
	}
	var response struct {
		Choices []struct {
			Message choiceMsg `json:"message"`
		} `json:"choices"`
		Data *struct {
			Choices []struct {
				Message choiceMsg `json:"message"`
			} `json:"choices"`
		} `json:"data"`
	}

	if err := c.post(ctx, joinURL(baseURL, "chat/completions"), apiKey, "Authorization", "Bearer ", payload, &response, nil); err != nil {
		return Response{}, err
	}

	if len(response.Choices) == 0 && response.Data != nil {
		response.Choices = response.Data.Choices
	}
	if len(response.Choices) == 0 {
		return Response{}, fmt.Errorf("provider returned no choices")
	}

	msg := response.Choices[0].Message
	res := Response{Content: strings.TrimSpace(msg.Content)}

	if len(msg.ToolCalls) > 0 {
		res.ToolCalls = make([]ToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{"raw": tc.Function.Arguments}
			}
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: args,
			})
		}
	}

	return res, nil
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

// postReader is like post but returns the response body for streaming (caller must close).
func (c *Client) postReader(ctx context.Context, endpoint, apiKey, keyHeader, keyPrefix string, payload any, headers http.Header) (io.ReadCloser, error) {
	contents, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding provider request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("creating provider request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(keyHeader, keyPrefix+apiKey)
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	result, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("calling AI provider: %w", err)
	}
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(result.Body, 8<<10))
		result.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading AI provider error: %w", readErr)
		}
		return nil, fmt.Errorf("AI provider returned %s: %s", result.Status, strings.TrimSpace(string(body)))
	}
	return result.Body, nil
}

// sseEvent is a single SSE line parsed from a streaming response.
type sseEvent struct {
	event string
	data  string
	err   error
}

// sseReader reads SSE events from body and sends them on the returned channel.
func sseReader(ctx context.Context, body io.ReadCloser) <-chan sseEvent {
	ch := make(chan sseEvent, 10)
	go func() {
		defer close(ch)
		defer body.Close()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		var currentEvent string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				select {
				case ch <- sseEvent{event: currentEvent, data: data}:
				case <-ctx.Done():
					return
				}
				currentEvent = ""
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case ch <- sseEvent{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return ch
}

func (c *Client) openAIStream(ctx context.Context, baseURL, apiKey, model, system string, messages []Message) (<-chan StreamEvent, error) {
	requestMessages := make([]chatMessage, 0, len(messages)+1)
	if system != "" {
		requestMessages = append(requestMessages, chatMessage{Role: "system", Content: system})
	}
	for _, message := range messages {
		requestMessages = append(requestMessages, chatMessage{Role: string(message.Role), Content: message.Content})
	}
	payload := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}{Model: model, Messages: requestMessages, Stream: true}
	body, err := c.postReader(ctx, joinURL(baseURL, "chat/completions"), apiKey, "Authorization", "Bearer ", payload, nil)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamEvent, 10)
	go func() {
		defer close(ch)
		sawDone := false
		for line := range sseReader(ctx, body) {
			if line.err != nil {
				select {
				case ch <- StreamEvent{Err: line.err}:
				case <-ctx.Done():
				}
				return
			}
			if line.data == "[DONE]" {
				sawDone = true
				return
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(line.data), &chunk); err != nil {
				select {
				case ch <- StreamEvent{Err: fmt.Errorf("parse SSE chunk: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				if content != "" {
					select {
					case ch <- StreamEvent{Delta: content}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
		if !sawDone {
			select {
			case ch <- StreamEvent{Err: fmt.Errorf("truncated response: stream closed without [DONE]")}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (c *Client) anthropicStream(ctx context.Context, baseURL, apiKey, model, system string, messages []Message) (<-chan StreamEvent, error) {
	requestMessages := make([]chatMessage, 0, len(messages))
	for _, message := range messages {
		requestMessages = append(requestMessages, chatMessage{Role: string(message.Role), Content: message.Content})
	}
	payload := struct {
		Model     string        `json:"model"`
		MaxTokens int           `json:"max_tokens"`
		System    string        `json:"system"`
		Messages  []chatMessage `json:"messages"`
		Stream    bool          `json:"stream"`
	}{Model: model, MaxTokens: 1024, System: system, Messages: requestMessages, Stream: true}
	headers := http.Header{"anthropic-version": []string{"2023-06-01"}}
	body, err := c.postReader(ctx, joinURL(baseURL, "v1/messages"), apiKey, "x-api-key", "", payload, headers)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamEvent, 10)
	go func() {
		defer close(ch)
		sawStop := false
		for line := range sseReader(ctx, body) {
			if line.err != nil {
				select {
				case ch <- StreamEvent{Err: line.err}:
				case <-ctx.Done():
				}
				return
			}
			if line.event == "message_stop" {
				sawStop = true
				continue
			}
			if line.event != "content_block_delta" {
				continue
			}
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(line.data), &delta); err != nil {
				select {
				case ch <- StreamEvent{Err: fmt.Errorf("parse SSE chunk: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			if delta.Delta.Text != "" {
				select {
				case ch <- StreamEvent{Delta: delta.Delta.Text}:
				case <-ctx.Done():
					return
				}
			}
		}
		if !sawStop {
			select {
			case ch <- StreamEvent{Err: fmt.Errorf("truncated response: stream closed without message_stop")}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (c *Client) geminiStream(ctx context.Context, baseURL, apiKey, model, system string, messages []Message) (<-chan StreamEvent, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	contents := make([]content, 0, len(messages))
	for _, message := range messages {
		role := "user"
		if message.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, content{Role: role, Parts: []part{{Text: message.Content}}})
	}
	payload := struct {
		SystemInstruction content   `json:"systemInstruction"`
		Contents          []content `json:"contents"`
	}{SystemInstruction: content{Parts: []part{{Text: system}}}, Contents: contents}
	streamURL := joinURL(baseURL, "v1beta/models/"+url.PathEscape(model)+":streamGenerateContent")
	parsed, _ := url.Parse(streamURL)
	q := parsed.Query()
	q.Set("alt", "sse")
	parsed.RawQuery = q.Encode()
	body, err := c.postReader(ctx, parsed.String(), apiKey, "x-goog-api-key", "", payload, nil)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamEvent, 10)
	go func() {
		defer close(ch)
		sawFinish := false
		for line := range sseReader(ctx, body) {
			if line.err != nil {
				select {
				case ch <- StreamEvent{Err: line.err}:
				case <-ctx.Done():
				}
				return
			}
			var chunk struct {
				Candidates []struct {
					Content      content `json:"content"`
					FinishReason string  `json:"finishReason"`
				} `json:"candidates"`
			}
			if err := json.Unmarshal([]byte(line.data), &chunk); err != nil {
				select {
				case ch <- StreamEvent{Err: fmt.Errorf("parse SSE chunk: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			if len(chunk.Candidates) > 0 {
				for _, p := range chunk.Candidates[0].Content.Parts {
					if p.Text != "" {
						select {
						case ch <- StreamEvent{Delta: p.Text}:
						case <-ctx.Done():
							return
						}
					}
				}
				if chunk.Candidates[0].FinishReason != "" {
					sawFinish = true
				}
			}
		}
		if !sawFinish {
			select {
			case ch <- StreamEvent{Err: fmt.Errorf("truncated response: stream closed without finishReason")}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

// chatMessage is the wire-format message sent to providers.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) openAI(ctx context.Context, baseURL, apiKey, model, system string, messages []Message) (string, error) {
	requestMessages := make([]chatMessage, 0, len(messages)+1)
	if system != "" {
		requestMessages = append(requestMessages, chatMessage{Role: "system", Content: system})
	}
	for _, message := range messages {
		requestMessages = append(requestMessages, chatMessage{Role: string(message.Role), Content: message.Content})
	}
	payload := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
	}{Model: model, Messages: requestMessages}
	var response struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
		Data *struct {
			Choices []struct {
				Message chatMessage `json:"message"`
			} `json:"choices"`
		} `json:"data"`
	}
	if err := c.post(ctx, joinURL(baseURL, "chat/completions"), apiKey, "Authorization", "Bearer ", payload, &response, nil); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 && response.Data != nil {
		response.Choices = response.Data.Choices
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("provider returned no response text")
	}
	return response.Choices[0].Message.Content, nil
}

func (c *Client) anthropic(ctx context.Context, baseURL, apiKey, model, system string, messages []Message) (string, error) {
	requestMessages := make([]chatMessage, 0, len(messages))
	for _, message := range messages {
		requestMessages = append(requestMessages, chatMessage{Role: string(message.Role), Content: message.Content})
	}
	payload := struct {
		Model     string        `json:"model"`
		MaxTokens int           `json:"max_tokens"`
		System    string        `json:"system"`
		Messages  []chatMessage `json:"messages"`
	}{Model: model, MaxTokens: 1024, System: system, Messages: requestMessages}
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	headers := http.Header{"anthropic-version": []string{"2023-06-01"}}
	if err := c.post(ctx, joinURL(baseURL, "v1/messages"), apiKey, "x-api-key", "", payload, &response, headers); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, part := range response.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("provider returned no response text")
	}
	return text.String(), nil
}

func (c *Client) gemini(ctx context.Context, baseURL, apiKey, model, system string, messages []Message) (string, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	contents := make([]content, 0, len(messages))
	for _, message := range messages {
		role := "user"
		if message.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, content{Role: role, Parts: []part{{Text: message.Content}}})
	}
	payload := struct {
		SystemInstruction content   `json:"systemInstruction"`
		Contents          []content `json:"contents"`
	}{SystemInstruction: content{Parts: []part{{Text: system}}}, Contents: contents}
	var response struct {
		Candidates []struct {
			Content content `json:"content"`
		} `json:"candidates"`
	}
	if err := c.post(ctx, joinURL(baseURL, "v1beta/models/"+url.PathEscape(model)+":generateContent"), apiKey, "x-goog-api-key", "", payload, &response, nil); err != nil {
		return "", err
	}
	if len(response.Candidates) == 0 {
		return "", fmt.Errorf("provider returned no response text")
	}
	var text strings.Builder
	for _, part := range response.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("provider returned no response text")
	}
	return text.String(), nil
}

func (c *Client) post(ctx context.Context, endpoint, apiKey, keyHeader, keyPrefix string, payload, response any, headers http.Header) error {
	contents, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding provider request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(contents))
	if err != nil {
		return fmt.Errorf("creating provider request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(keyHeader, keyPrefix+apiKey)
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	result, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("calling AI provider: %w", err)
	}
	defer result.Body.Close()
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(result.Body, 8<<10))
		if readErr != nil {
			return fmt.Errorf("reading AI provider error: %w", readErr)
		}
		return fmt.Errorf("AI provider returned %s: %s", result.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(result.Body).Decode(response); err != nil {
		return fmt.Errorf("decoding AI provider response: %w", err)
	}
	return nil
}

func joinURL(baseURL, suffix string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	parsed.Path = path.Join(parsed.Path, suffix)
	return parsed.String()
}
