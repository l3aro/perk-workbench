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
)

// chatMessage is the wire-format message sent to providers.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
