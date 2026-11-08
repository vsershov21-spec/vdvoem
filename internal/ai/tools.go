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

// Tool is a function the model may call. Parameters is a JSON Schema object.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	ID   string `json:"id"`
	Name string
	Args json.RawMessage
}

// Message in an OpenAI-compatible conversation with tools.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"-"`
	ToolCallID string     `json:"-"`
}

func (m Message) wire() map[string]any {
	w := map[string]any{"role": m.Role, "content": m.Content}
	if len(m.ToolCalls) > 0 {
		var tc []map[string]any
		for _, c := range m.ToolCalls {
			tc = append(tc, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Args)}})
		}
		w["tool_calls"] = tc
	}
	if m.ToolCallID != "" {
		w["tool_call_id"] = m.ToolCallID
	}
	return w
}

type Turn struct {
	Content string
	Calls   []ToolCall
}

// Step sends the conversation and returns either text or tool calls.
func (c *Client) Step(ctx context.Context, task string, msgs []Message, tools []Tool) (Turn, error) {
	if !c.Enabled() {
		return Turn{}, errors.New("ai disabled")
	}
	t0 := time.Now()
	var wm []map[string]any
	for _, m := range msgs {
		wm = append(wm, m.wire())
	}
	var wt []map[string]any
	for _, t := range tools {
		wt = append(wt, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
	}
	req := map[string]any{"model": c.Model, "temperature": 0.3, "max_tokens": 600, "messages": wm}
	if len(wt) > 0 {
		req["tools"] = wt
	}
	body, _ := json.Marshal(req)
	hr, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.URL, "/")+"/chat/completions", bytes.NewReader(body))
	hr.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		hr.Header.Set("Authorization", "Bearer "+c.Key)
	}
	hc := c.client(25 * time.Second)
	last := ""
	if len(msgs) > 0 {
		last = msgs[len(msgs)-1].Content
	}
	res, err := hc.Do(hr)
	if err != nil {
		c.log(task, last, err.Error(), false, t0)
		return Turn{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var r struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if res.StatusCode != 200 || json.Unmarshal(raw, &r) != nil || len(r.Choices) == 0 {
		c.log(task, last, fmt.Sprintf("%d %s", res.StatusCode, trunc(string(raw), 300)), false, t0)
		return Turn{}, fmt.Errorf("ai: status %d", res.StatusCode)
	}
	m := r.Choices[0].Message
	t := Turn{Content: strings.TrimSpace(m.Content)}
	for _, tc := range m.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if !json.Valid(args) {
			args = json.RawMessage("{}")
		}
		t.Calls = append(t.Calls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: args})
	}
	out := t.Content
	for _, tc := range t.Calls {
		out += " →" + tc.Name + string(tc.Args)
	}
	c.log(task, last, out, true, t0)
	return t, nil
}
