package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// completeOpenAI sends a non-streaming request that MAY return tool calls.
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
