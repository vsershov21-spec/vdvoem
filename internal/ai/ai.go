// Package ai: one adapter for any OpenAI-compatible chat API (OpenAI, YandexGPT's compatible endpoint,
// local models). The model only parses text and phrases one sentence; it never decides anything.
// Every answer is checked against a schema and falls back to deterministic code.
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
	"unicode/utf8"
)

type Logger func(task, provider, in, out string, ok bool, ms int64)

type Client struct {
	URL, Key, Model string
	HTTP            *http.Client
	Log             Logger
}

func (c *Client) Enabled() bool { return c != nil && c.URL != "" && c.Model != "" }

func (c *Client) provider() string {
	if c == nil || c.URL == "" {
		return "none"
	}
	return c.Model
}

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) complete(ctx context.Context, task, system, user string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("ai disabled")
	}
	t0 := time.Now()
	body, _ := json.Marshal(map[string]any{"model": c.Model, "temperature": 0.2, "max_tokens": 300,
		"messages": []msg{{"system", system}, {"user", user}}})
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.URL, "/")+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 12 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		c.log(task, user, err.Error(), false, t0)
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var r struct {
		Choices []struct {
			Message msg `json:"message"`
		} `json:"choices"`
	}
	if res.StatusCode != 200 || json.Unmarshal(raw, &r) != nil || len(r.Choices) == 0 {
		c.log(task, user, fmt.Sprintf("%d %s", res.StatusCode, trunc(string(raw), 300)), false, t0)
		return "", fmt.Errorf("ai: status %d", res.StatusCode)
	}
	out := r.Choices[0].Message.Content
	c.log(task, user, out, true, t0)
	return out, nil
}

func (c *Client) log(task, in, out string, ok bool, t0 time.Time) {
	if c.Log != nil {
		c.Log(task, c.provider(), trunc(in, 1000), trunc(out, 1000), ok, time.Since(t0).Milliseconds())
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// IdeaGuess is what the model may say about a free-text idea.
type IdeaGuess struct {
	Title       string `json:"title"`
	Kind        string `json:"kind"`     // leisure | chore
	Category    string `json:"category"` // food | culture | walk | sport | home | other
	PriceRub    int64  `json:"price_rub"`
	DurationMin int    `json:"duration_min"`
}

var cats = map[string]bool{"food": true, "culture": true, "walk": true, "sport": true, "home": true, "other": true}

func (g IdeaGuess) valid() bool {
	return g.Title != "" && utf8.RuneCountInString(g.Title) <= 80 && (g.Kind == "leisure" || g.Kind == "chore") && cats[g.Category] &&
		g.PriceRub >= 0 && g.PriceRub < 1_000_000 && g.DurationMin >= 0 && g.DurationMin <= 24*60
}

const parseSystem = `Ты разбираешь короткую заметку пары о том, куда сходить или что сделать. Ответь ТОЛЬКО JSON без пояснений:
{"title": "короткое название с заглавной буквы, без кавычек", "kind": "leisure" или "chore", "category": "food|culture|walk|sport|home|other",
"price_rub": примерная цена на двоих в рублях или 0, "duration_min": примерная длительность в минутах или 0}.
"chore" — бытовое дело (убрать, купить, починить, записаться). Не придумывай цену, если её нет в тексте и она неочевидна: ставь 0.`

func (c *Client) ParseIdea(ctx context.Context, text string) (IdeaGuess, error) {
	out, err := c.complete(ctx, "parse_idea", parseSystem, text)
	if err != nil {
		return IdeaGuess{}, err
	}
	var g IdeaGuess
	if err := json.Unmarshal([]byte(jsonPart(out)), &g); err != nil || !g.valid() {
		return IdeaGuess{}, errors.New("ai: invalid parse")
	}
	return g, nil
}

const whySystem = `Ты пишешь одну короткую фразу (до 110 символов) для пары: почему эта идея подходит на ближайшую встречу.
Пиши на «вы», тепло и просто, без эмодзи, без кавычек и без имён. Опирайся только на переданные факты.`

func (c *Client) Why(ctx context.Context, title string, facts []string) (string, error) {
	out, err := c.complete(ctx, "why", whySystem, "Идея: "+title+"\nФакты: "+strings.Join(facts, "; "))
	if err != nil {
		return "", err
	}
	out = strings.Trim(strings.TrimSpace(out), `"«»`)
	if out == "" || utf8.RuneCountInString(out) > 140 || strings.Contains(out, "\n") {
		return "", errors.New("ai: invalid why")
	}
	return out, nil
}

func jsonPart(s string) string {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return s
	}
	return s[i : j+1]
}

// PlaceGuess is one concrete place the model proposes for a generic idea («каток» → «Каток на ВДНХ»).
// The model never gives addresses: they would be invented; the app adds a map search link instead.
type PlaceGuess struct {
	Name string `json:"name"`
	Why  string `json:"why"`
}

const placesSystem = `Пара хочет: общая идея досуга. Предложи 3 конкретных, реально существующих и известных места в указанном городе,
где это можно сделать. Ответь ТОЛЬКО JSON: {"places":[{"name":"название места, как его ищут на картах","why":"чем хорошо, до 60 символов"}]}.
Без адресов, телефонов и цен. Если не уверен, что место существует, не включай его.`

func (c *Client) SuggestPlaces(ctx context.Context, idea, city string) ([]PlaceGuess, error) {
	if city == "" {
		city = "Москва"
	}
	out, err := c.complete(ctx, "suggest_places", placesSystem, "Идея: "+idea+"\nГород: "+city)
	if err != nil {
		return nil, err
	}
	var r struct {
		Places []PlaceGuess `json:"places"`
	}
	if err := json.Unmarshal([]byte(jsonPart(out)), &r); err != nil {
		return nil, errors.New("ai: invalid places")
	}
	var ok []PlaceGuess
	for _, p := range r.Places {
		p.Name, p.Why = strings.TrimSpace(p.Name), strings.TrimSpace(p.Why)
		if p.Name == "" || utf8.RuneCountInString(p.Name) > 80 || strings.ContainsAny(p.Name, "<>") {
			continue
		}
		if utf8.RuneCountInString(p.Why) > 90 {
			p.Why = string([]rune(p.Why)[:90])
		}
		ok = append(ok, p)
		if len(ok) == 3 {
			break
		}
	}
	if len(ok) == 0 {
		return nil, errors.New("ai: no places")
	}
	return ok, nil
}
