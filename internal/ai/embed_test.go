package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/v1/embeddings" || in.Model != "bge-m3" || len(in.Input) != 2 {
			http.Error(w, "bad", 400)
			return
		}
		// out of order on purpose: the index decides
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0,2]},{"index":0,"embedding":[3,4]}]}`))
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL + "/v1", Model: "qwen"}
	vs, err := c.Embed(context.Background(), "bge-m3", []string{"а", "б"})
	if err != nil || len(vs) != 2 {
		t.Fatal(vs, err)
	}
	if vs[0][0] != 0.6 || vs[1][1] != 1 {
		t.Fatalf("not unit / wrong order: %v", vs)
	}
	if c := Cos(vs[0], vs[1]); c < 0.79 || c > 0.81 {
		t.Fatalf("cos %v", c)
	}
	if _, err := c.Embed(context.Background(), "", []string{"x"}); err == nil {
		t.Fatal("no model must be off")
	}
}
