package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Structured asks for one JSON object that matches schema. A local Ollama gets it natively (`format`, grammar-
// constrained: the model cannot produce anything else); other providers get OpenAI's response_format.
// Keep the system prompt and the examples identical between calls: Ollama reuses their cached prefix,
// so only the question itself is processed — that is what keeps a CPU model within a few seconds.
func (c *Client) Structured(ctx context.Context, task string, msgs []Message, schema map[string]any, maxTokens int) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	t0 := time.Now()
	last := ""
	if len(msgs) > 0 {
		last = msgs[len(msgs)-1].Content
	}
	var body map[string]any
	var url string
	if c.IsLocal() {
		url = strings.TrimSuffix(strings.TrimSuffix(c.URL, "/"), "/v1") + "/api/chat"
		body = map[string]any{"model": c.Model, "messages": msgs, "stream": false, "format": schema, "keep_alive": "60m",
			"options": map[string]any{"temperature": 0, "num_predict": maxTokens, "num_ctx": 4096}}
	} else {
		url = strings.TrimRight(c.URL, "/") + "/chat/completions"
		body = map[string]any{"model": c.Model, "messages": msgs, "temperature": 0, "max_tokens": maxTokens,
			"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "intent", "schema": schema}}}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	res, err := c.client(25 * time.Second).Do(req)
	if err != nil {
		c.log(task, last, err.Error(), false, t0)
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var content string
	if c.IsLocal() {
		var r struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		_ = json.Unmarshal(raw, &r)
		content = r.Message.Content
	} else {
		var r struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(raw, &r) == nil && len(r.Choices) > 0 {
			content = r.Choices[0].Message.Content
		}
	}
	content = strings.TrimSpace(content)
	if res.StatusCode != 200 || !json.Valid([]byte(content)) {
		c.log(task, last, fmt.Sprintf("%d %s", res.StatusCode, trunc(string(raw), 300)), false, t0)
		return nil, fmt.Errorf("ai: status %d", res.StatusCode)
	}
	c.log(task, last, content, true, t0)
	return json.RawMessage(content), nil
}

var ErrDisabled = errors.New("ai disabled")

// IsLocal: an Ollama on this machine.
func (c *Client) IsLocal() bool {
	return c != nil && (strings.Contains(c.URL, "127.0.0.1:11434") || strings.Contains(c.URL, "localhost:11434"))
}
