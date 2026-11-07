// Package tg is a small Telegram Bot API client. Base URL is configurable so tests run against an emulator.
package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

type Client struct {
	Base  string // https://api.telegram.org
	Token string
	HTTP  *http.Client
}

func New(base, token string) *Client {
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Client{Base: base, Token: token, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// ---------- incoming ----------

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type PhotoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Message struct {
	MessageID int64       `json:"message_id"`
	From      *User       `json:"from,omitempty"`
	Chat      Chat        `json:"chat"`
	Text      string      `json:"text,omitempty"`
	Caption   string      `json:"caption,omitempty"`
	Photo     []PhotoSize `json:"photo,omitempty"`
	ReplyTo   *Message    `json:"reply_to_message,omitempty"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

// ---------- outgoing ----------

type WebApp struct {
	URL string `json:"url"`
}

type Button struct {
	Text         string  `json:"text"`
	CallbackData string  `json:"callback_data,omitempty"`
	URL          string  `json:"url,omitempty"`
	WebApp       *WebApp `json:"web_app,omitempty"`
}

type Markup struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}

func Row(b ...Button) []Button          { return b }
func CB(text, data string) Button       { return Button{Text: text, CallbackData: data} }
func App(text, url string) Button       { return Button{Text: text, WebApp: &WebApp{URL: url}} }
func Link(text, url string) Button      { return Button{Text: text, URL: url} }
func Keyboard(rows ...[]Button) *Markup { return &Markup{InlineKeyboard: rows} }

type apiResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

func (c *Client) call(ctx context.Context, method string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/bot%s/%s", c.Base, c.Token, method), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, method, out)
}

func (c *Client) do(req *http.Request, method string, out any) error {
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("tg %s: %w", method, err)
	}
	defer res.Body.Close()
	var r apiResp
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&r); err != nil {
		return fmt.Errorf("tg %s: decode: %w", method, err)
	}
	if !r.OK {
		return fmt.Errorf("tg %s: %d %s", method, r.ErrorCode, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

type SendOpts struct {
	Markup  *Markup
	Silent  bool
	Preview bool
}

func (c *Client) Send(ctx context.Context, chatID int64, html string, o SendOpts) (Message, error) {
	body := map[string]any{"chat_id": chatID, "text": html, "parse_mode": "HTML",
		"disable_notification": o.Silent, "link_preview_options": map[string]bool{"is_disabled": !o.Preview}}
	if o.Markup != nil {
		body["reply_markup"] = o.Markup
	}
	var m Message
	err := c.call(ctx, "sendMessage", body, &m)
	return m, err
}

func (c *Client) Edit(ctx context.Context, chatID, msgID int64, html string, mk *Markup) error {
	body := map[string]any{"chat_id": chatID, "message_id": msgID, "text": html, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true}}
	if mk != nil {
		body["reply_markup"] = mk
	}
	return c.call(ctx, "editMessageText", body, nil)
}

func (c *Client) Answer(ctx context.Context, callbackID, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callbackID, "text": text}, nil)
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, fileID, caption string) error {
	return c.call(ctx, "sendPhoto", map[string]any{"chat_id": chatID, "photo": fileID, "caption": caption, "parse_mode": "HTML"}, nil)
}

// SendDocument uploads a file (the .ics event).
func (c *Client) SendDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("chat_id", fmt.Sprint(chatID))
	w.WriteField("caption", caption)
	w.WriteField("parse_mode", "HTML")
	fw, _ := w.CreateFormFile("document", filename)
	fw.Write(data)
	w.Close()
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/bot%s/sendDocument", c.Base, c.Token), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.do(req, "sendDocument", nil)
}

func (c *Client) SetWebhook(ctx context.Context, url, secret string) error {
	return c.call(ctx, "setWebhook", map[string]any{"url": url, "secret_token": secret,
		"allowed_updates": []string{"message", "callback_query"}, "drop_pending_updates": false}, nil)
}

func (c *Client) SetMenuButton(ctx context.Context, text, url string) error {
	return c.call(ctx, "setChatMenuButton", map[string]any{"menu_button": map[string]any{"type": "web_app", "text": text, "web_app": WebApp{URL: url}}}, nil)
}

func (c *Client) SetCommands(ctx context.Context, cmds map[string]string, order []string) error {
	var list []map[string]string
	for _, k := range order {
		list = append(list, map[string]string{"command": k, "description": cmds[k]})
	}
	return c.call(ctx, "setMyCommands", map[string]any{"commands": list}, nil)
}

func (c *Client) SetDescription(ctx context.Context, short, long string) error {
	if err := c.call(ctx, "setMyShortDescription", map[string]any{"short_description": short}, nil); err != nil {
		return err
	}
	return c.call(ctx, "setMyDescription", map[string]any{"description": long}, nil)
}

func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	err := c.call(ctx, "getMe", map[string]any{}, &u)
	return u, err
}
