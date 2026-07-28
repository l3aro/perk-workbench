package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

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
