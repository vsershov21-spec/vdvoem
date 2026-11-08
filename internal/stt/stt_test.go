package stt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		" Куда сходить в субботу? ":          "Куда сходить в субботу?",
		"[музыка] Хотим почилить":            "Хотим почилить",
		"Продолжение следует...":             "",
		"Субтитры сделал DimaTorzok":         "",
		"Сколько стоит (неразборчиво) Чайка": "Сколько стоит Чайка",
	} {
		if got := Clean(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, h, err := r.FormFile("file")
		if err != nil || r.URL.Path != "/inference" || r.FormValue("language") != "ru" {
			http.Error(w, "bad", 400)
			return
		}
		b, _ := io.ReadAll(f)
		if h.Filename != "voice.ogg" || string(b) != "OGG" {
			http.Error(w, "file", 400)
			return
		}
		io.WriteString(w, `{"text":" Что есть на неделе? "}`)
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL}
	got, err := c.Text(context.Background(), []byte("OGG"), "voice.ogg")
	if err != nil || got != "Что есть на неделе?" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := (&Client{}).Text(context.Background(), []byte("x"), "a.ogg"); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal("off client must refuse")
	}
}
