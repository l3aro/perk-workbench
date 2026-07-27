package ai

import (
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
}

type Response struct {
	Agent   string
	Content string
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
