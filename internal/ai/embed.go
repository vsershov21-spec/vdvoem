package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embed: texts → unit vectors with an embedding model on the same endpoint (Ollama /v1/embeddings).
// Used to find things by meaning («что-нибудь душевное» → a chamber concert) where words don't match.
func (c *Client) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if c == nil || c.URL == "" || model == "" || len(texts) == 0 {
		return nil, fmt.Errorf("embed: off")
	}
	b, _ := json.Marshal(map[string]any{"model": model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.URL, "/")+"/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	t0 := time.Now()
	res, err := c.client(30 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("embed: %d %s", res.StatusCode, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embed: bad answer (%d of %d)", len(out.Data), len(texts))
	}
	vs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index >= 0 && d.Index < len(vs) {
			vs[d.Index] = Unit(d.Embedding)
		}
	}
	c.log("embed", fmt.Sprint(len(texts)), "", true, t0)
	return vs, nil
}

func Unit(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	n := float32(1 / math.Sqrt(s))
	out := make([]float32, len(v))
	for k, x := range v {
		out[k] = x * n
	}
	return out
}

// Cos of two unit vectors
func Cos(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float64
	for k := range a {
		s += float64(a[k]) * float64(b[k])
	}
	return s
}
