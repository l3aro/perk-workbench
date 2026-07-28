package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

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
