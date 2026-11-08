// Package stt turns a voice message into text with a local whisper.cpp server (deploy/stt.sh) — the audio
// never leaves the machine. Without STT_URL voice is off and the bot says so.
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	URL  string // http://127.0.0.1:8178
	HTTP *http.Client
}

func (c *Client) Enabled() bool { return c != nil && c.URL != "" }

var ErrEmpty = errors.New("stt: nothing heard")

// MaxBytes: about a minute of Telegram voice (opus ≈ 16 kbit/s) or a browser recording
const MaxBytes = 2 << 20

// Text: speech → text (Russian). name only hints the container (voice.ogg, rec.webm, rec.mp4); the server converts.
func (c *Client) Text(ctx context.Context, audio []byte, name string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("stt: off")
	}
	if len(audio) == 0 || len(audio) > MaxBytes {
		return "", fmt.Errorf("stt: %d bytes", len(audio))
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, _ := w.CreateFormFile("file", name)
	_, _ = f.Write(audio)
	for k, v := range map[string]string{"response_format": "json", "language": "ru", "temperature": "0"} {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.URL, "/")+"/inference", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != 200 {
		return "", fmt.Errorf("stt: %d %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("stt: decode: %w", err)
	}
	t := Clean(out.Text)
	if t == "" {
		return "", ErrEmpty
	}
	return t, nil
}

// Clean drops what whisper adds on silence or noise («[музыка]», «Продолжение следует…», subtitles credits)
func Clean(t string) string {
	t = strings.TrimSpace(t)
	for {
		i, j := strings.IndexAny(t, "[("), -1
		if i < 0 {
			break
		}
		if k := strings.IndexAny(t[i:], "])"); k > 0 {
			j = i + k
		}
		if j < 0 {
			break
		}
		t = strings.TrimSpace(t[:i] + " " + t[j+1:])
	}
	l := strings.ToLower(t)
	for _, junk := range []string{"продолжение следует", "субтитры", "спасибо за просмотр", "редактор субтитров", "подписывайтесь на канал"} {
		if strings.Contains(l, junk) && len([]rune(t)) < 60 {
			return ""
		}
	}
	return strings.Join(strings.Fields(t), " ")
}
