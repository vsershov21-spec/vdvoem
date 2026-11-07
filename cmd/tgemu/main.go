// tgemu is a tiny Telegram Bot API emulator for e2e tests.
//
// The bot talks to it as if it were api.telegram.org (TG_API_BASE=http://127.0.0.1:8098).
// Tests talk to it through /_emu:
//
//	POST /_emu/say      {"from":1,"name":"Вова","text":"/start"}        → delivers a message update to the bot
//	POST /_emu/press    {"from":1,"name":"Вова","data":"w:3:want"}      → delivers a button press
//	POST /_emu/press    {"from":1,"label":"Подтвердить"}                → presses the button with this text in the latest message that has it
//	GET  /_emu/chat/1                                                     → everything the bot sent to chat 1
//	POST /_emu/reset
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type sent struct {
	ID       int64           `json:"message_id"`
	Method   string          `json:"method"`
	Chat     int64           `json:"chat_id"`
	Text     string          `json:"text,omitempty"`
	Markup   json.RawMessage `json:"reply_markup,omitempty"`
	Silent   bool            `json:"silent,omitempty"`
	FileName string          `json:"file_name,omitempty"`
	File     string          `json:"file,omitempty"`
	At       int64           `json:"at"`
}

type emu struct {
	mu      sync.Mutex
	next    int64
	upd     int64
	msgs    []sent
	answers []string
	webhook string
	secret  string
}

func (e *emu) bot(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/bot"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	method := parts[1]
	body := map[string]any{}
	var fileName, file string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.ParseMultipartForm(4 << 20)
		for k, v := range r.MultipartForm.Value {
			body[k] = v[0]
		}
		if f, h, err := r.FormFile("document"); err == nil {
			b, _ := io.ReadAll(f)
			fileName, file = h.Filename, string(b)
		}
	} else {
		d := json.NewDecoder(r.Body)
		d.UseNumber() // big chat ids must not become 8e+08
		d.Decode(&body)
	}
	ok := func(res any) { json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res}) }
	e.mu.Lock()
	defer e.mu.Unlock()
	switch method {
	case "getMe":
		ok(map[string]any{"id": 777, "is_bot": true, "first_name": "Вдвоём", "username": "vdvoem_test_bot"})
	case "setWebhook":
		e.webhook, _ = body["url"].(string)
		e.secret, _ = body["secret_token"].(string)
		ok(true)
	case "sendMessage", "sendDocument", "sendPhoto", "editMessageText":
		e.next++
		var chat int64
		fmt.Sscan(fmt.Sprint(body["chat_id"]), &chat)
		text, _ := body["text"].(string)
		if text == "" {
			text, _ = body["caption"].(string)
		}
		mk, _ := json.Marshal(body["reply_markup"])
		if string(mk) == "null" {
			mk = nil
		}
		silent, _ := body["disable_notification"].(bool)
		e.msgs = append(e.msgs, sent{ID: e.next, Method: method, Chat: chat, Text: text, Markup: mk, Silent: silent, FileName: fileName, File: file, At: time.Now().UnixMilli()})
		ok(map[string]any{"message_id": e.next, "chat": map[string]any{"id": chat, "type": "private"}, "text": text})
	case "answerCallbackQuery":
		t, _ := body["text"].(string)
		e.answers = append(e.answers, t)
		ok(true)
	default: // setMyCommands, setChatMenuButton, descriptions…
		ok(true)
	}
}

func (e *emu) deliver(u map[string]any) error {
	e.mu.Lock()
	e.upd++
	u["update_id"] = e.upd
	hook, secret := e.webhook, e.secret
	e.mu.Unlock()
	if hook == "" {
		return fmt.Errorf("webhook not set")
	}
	b, _ := json.Marshal(u)
	req, _ := http.NewRequest("POST", hook, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("webhook status %d", res.StatusCode)
	}
	return nil
}

type in struct {
	From  int64  `json:"from"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	Data  string `json:"data"`
	Label string `json:"label"`
	Photo bool   `json:"photo"`
	Reply int64  `json:"reply"` // message id this message answers
}

func user(i in) map[string]any {
	n := i.Name
	if n == "" {
		n = fmt.Sprint("User", i.From)
	}
	return map[string]any{"id": i.From, "first_name": n, "is_bot": false}
}

func (e *emu) say(w http.ResponseWriter, r *http.Request) {
	var i in
	json.NewDecoder(r.Body).Decode(&i)
	msg := map[string]any{"message_id": time.Now().UnixNano() % 1e9, "from": user(i), "chat": map[string]any{"id": i.From, "type": "private"}, "date": time.Now().Unix(), "text": i.Text}
	if i.Reply != 0 {
		msg["reply_to_message"] = map[string]any{"message_id": i.Reply, "chat": map[string]any{"id": i.From, "type": "private"}}
	}
	if i.Photo {
		delete(msg, "text")
		msg["photo"] = []map[string]any{{"file_id": "photo-small", "width": 90, "height": 90}, {"file_id": fmt.Sprint("photo-", time.Now().UnixNano()), "width": 1280, "height": 960}}
	}
	if err := e.deliver(map[string]any{"message": msg}); err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Write([]byte(`{"ok":true}`))
}

func (e *emu) press(w http.ResponseWriter, r *http.Request) {
	var i in
	json.NewDecoder(r.Body).Decode(&i)
	data, msgID := i.Data, int64(0)
	text := ""
	if data == "" && i.Label != "" {
		e.mu.Lock()
	find:
		for k := len(e.msgs) - 1; k >= 0; k-- {
			m := e.msgs[k]
			if m.Chat != i.From || m.Markup == nil {
				continue
			}
			var mk struct {
				Rows [][]struct {
					Text string `json:"text"`
					Data string `json:"callback_data"`
				} `json:"inline_keyboard"`
			}
			json.Unmarshal(m.Markup, &mk)
			for _, row := range mk.Rows {
				for _, b := range row {
					if b.Data != "" && strings.Contains(b.Text, i.Label) {
						data, msgID, text = b.Data, m.ID, m.Text
						break find
					}
				}
			}
		}
		e.mu.Unlock()
		if data == "" {
			http.Error(w, "no button "+i.Label, 404)
			return
		}
	}
	cb := map[string]any{"id": fmt.Sprint(time.Now().UnixNano()), "from": user(i), "data": data,
		"message": map[string]any{"message_id": msgID, "chat": map[string]any{"id": i.From, "type": "private"}, "text": text}}
	if err := e.deliver(map[string]any{"callback_query": cb}); err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Write([]byte(`{"ok":true,"data":` + fmt.Sprintf("%q", data) + `}`))
}

func (e *emu) chat(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscan(r.PathValue("id"), &id)
	e.mu.Lock()
	out := []sent{}
	for _, m := range e.msgs {
		if m.Chat == id {
			out = append(out, m)
		}
	}
	e.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func main() {
	addr := flag.String("listen", "127.0.0.1:8098", "")
	flag.Parse()
	e := &emu{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /_emu/say", e.say)
	mux.HandleFunc("POST /_emu/press", e.press)
	mux.HandleFunc("GET /_emu/chat/{id}", e.chat)
	mux.HandleFunc("GET /_emu/answers", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		json.NewEncoder(w).Encode(e.answers)
	})
	mux.HandleFunc("POST /_emu/reset", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.msgs, e.answers = nil, nil
		e.mu.Unlock()
	})
	// fake place pages for the link parser (og:* + schema.org)
	places := map[string][4]string{
		"khachapuri": {"Хачапури и вино", "Restaurant", "Покровка, 17", "1800 ₽"},
		"garage":     {"Музей Гараж", "Museum", "ул. Крымский Вал, 9, стр. 32", "600 ₽"},
		"katok":      {"Каток в Парке Горького", "SportsActivityLocation", "ул. Крымский Вал, 9", "700 ₽"},
	}
	mux.HandleFunc("GET /_emu/place/{name}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := places[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><title>%[1]s — Яндекс Карты</title>
<meta property="og:title" content="%[1]s — Яндекс Карты"><meta property="og:description" content="%[1]s. Средний чек %[4]s">
<script type="application/ld+json">{"@type":"%[2]s","name":"%[1]s","address":{"streetAddress":"%[3]s"},"priceRange":"%[4]s"}</script>
</head><body>ok</body></html>`, p[0], p[1], p[2], p[3])
	})
	// a tiny OpenAI-compatible endpoint: it only knows how to suggest places; everything else fails,
	// which also exercises the rules fallback of the assistant
	mux.HandleFunc("POST /_ai/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Content, "реально существующих") {
			http.Error(w, `{"error":"emulator: not supported"}`, 500)
			return
		}
		content := `{"places":[{"name":"Каток на ВДНХ","why":"самый большой в городе"},{"name":"Каток в Сокольниках","why":"спокойнее и уютнее"},{"name":"Каток «Лужники»","why":"музыка и подсветка"}]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	})
	mux.HandleFunc("/", e.bot)
	log.Printf("tgemu on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
