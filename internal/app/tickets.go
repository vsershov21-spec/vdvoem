package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// After «мы идём»: tickets or a table. The event's own link when there is one (the scout and channels bring
// ticket pages), otherwise a search that lands on sellers. «Билеты есть» tells the partner, so nobody buys twice.

// BookLink: where to buy / book, and the word for it ("" — nothing to buy: a walk, a free place)
func BookLink(i store.Idea) (label, link string) {
	free := i.PriceMin == 0 && (i.Category == "walk" || i.Category == "home")
	switch {
	case free:
		return "", ""
	case i.Category == "food":
		label = "Забронировать стол"
		if i.URL != "" {
			return label, i.URL
		}
		return label, "https://yandex.ru/maps/?text=" + url.QueryEscape(i.Title+" "+i.Address)
	default:
		label = "Купить билеты"
		if i.URL != "" {
			return label, i.URL
		}
		return label, "https://yandex.ru/search/?text=" + url.QueryEscape(i.Title+" билеты")
	}
}

func (a *App) bookButton(i store.Idea) *tg.Button {
	l, u := BookLink(i)
	if u == "" {
		return nil
	}
	b := tg.Link("🎟 "+l, u)
	return &b
}

func bookWord(i store.Idea) string { l, _ := BookLink(i); return l }

func otherName(c Couple, m store.Member) string {
	if m.ID == c.Me.ID {
		return c.PartnerName()
	}
	return c.Me.Name
}

// NameDat: «Насте», «Вове»
func NameDat(name string) string {
	r := []rune(name)
	if len(r) < 2 {
		return name
	}
	switch r[len(r)-1] {
	case 'а', 'я':
		return string(r[:len(r)-1]) + "е"
	case 'й', 'ь':
		return string(r[:len(r)-1]) + "ю"
	}
	if r[len(r)-1] >= 'б' && r[len(r)-1] <= 'щ' {
		return name + "у"
	}
	return name
}

// Tickets: who marked «билеты есть» for a meeting (0 — nobody yet)
func (a *App) Tickets(ctx context.Context, spaceID, roundID int64) int64 {
	id, _ := strconv.ParseInt(a.S.Setting(ctx, fmt.Sprintf("tickets:%d:%d", spaceID, roundID)), 10, 64)
	return id
}

// GotTickets: I have the tickets (or the table) — the partner hears it once
func (a *App) GotTickets(ctx context.Context, c Couple, roundID int64) error {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return err
	}
	if r.State != domain.StatePlanned && r.State != domain.StateConfirm {
		return domain.ErrWrongState
	}
	if a.Tickets(ctx, c.Space.ID, r.ID) != 0 {
		return nil
	}
	if err := a.S.SetSetting(ctx, fmt.Sprintf("tickets:%d:%d", c.Space.ID, r.ID), strconv.FormatInt(c.Me.ID, 10)); err != nil {
		return err
	}
	i, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	what := "билеты"
	if i.Category == "food" {
		what = "стол"
	}
	if c.Partner != nil {
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("🎟 %s %s %s на <b>%s</b>. Покупать не нужно.", esc(c.Me.Name), Past(c.Me.Name, "взял"), what, esc(i.Title)), nil)
	}
	// and me: the mark is fixed, and the tickets themselves can be kept with the meeting
	if a.TicketFile(ctx, c.Space.ID, r.ID).Kind == "" {
		_ = a.S.SetSetting(ctx, fmt.Sprintf("tickets_wait:%d", c.Me.ID), fmt.Sprintf("%d:%d", r.ID, a.now().Unix()))
		a.send(ctx, c.Me.ChatID, fmt.Sprintf("🎟 Отметил: %s на <b>%s</b> есть — %s в курсе.\n\nПришли сюда сами %s — файл, скрин или ссылку: перешлю %s и прикреплю к встрече, ссылка будет и в событии календаря.",
			what, esc(i.Title), esc(c.PartnerName()), what, NameDat(c.PartnerName())), nil)
	}
	a.Live.From(ctx, c.Space.ID)
	return nil
}

// TicketFile: the tickets kept with a meeting — a Telegram file (photo/document) or a link.
type TicketFile struct {
	Kind   string `json:"kind"` // photo | document | link
	FileID string `json:"file_id,omitempty"`
	URL    string `json:"url,omitempty"`
	Name   string `json:"name,omitempty"`
	Token  string `json:"token,omitempty"` // the public link to the file: /t/<token>
	By     int64  `json:"by"`
}

func (a *App) TicketFile(ctx context.Context, spaceID, roundID int64) TicketFile {
	var f TicketFile
	_ = json.Unmarshal([]byte(a.S.Setting(ctx, fmt.Sprintf("ticketsfile:%d:%d", spaceID, roundID))), &f)
	return f
}

// TicketsLink: where the tickets open — the link itself, or our page that serves the file
func (a *App) TicketsLink(f TicketFile) string {
	if f.Kind == "link" {
		return f.URL
	}
	if f.Token != "" {
		return strings.TrimRight(a.PublicURL, "/") + "/t/" + f.Token
	}
	return ""
}

// AwaitingTickets: I marked «билеты есть» in the last two hours and have not sent them yet
func (a *App) AwaitingTickets(ctx context.Context, c Couple) int64 {
	var id, at int64
	fmt.Sscanf(a.S.Setting(ctx, fmt.Sprintf("tickets_wait:%d", c.Me.ID)), "%d:%d", &id, &at)
	if id == 0 || a.now().Sub(time.Unix(at, 0)) > 2*time.Hour {
		return 0
	}
	return id
}

type fileSender interface {
	SendFile(ctx context.Context, chatID int64, kind, fileID, caption string) error
	UploadDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) (tg.Message, error)
}

// UploadTickets: a file from the Mini App — it goes through the bot (so Telegram keeps it), then as AttachTickets
func (a *App) UploadTickets(ctx context.Context, c Couple, roundID int64, name string, data []byte) error {
	fs, ok := a.TG.(fileSender)
	if !ok {
		return errors.New("files are not supported")
	}
	m, err := fs.UploadDocument(ctx, c.Me.ChatID, name, data, "🎟 Билеты сохранены к встрече")
	if err != nil {
		return err
	}
	if m.Document == nil {
		return errors.New("no file id")
	}
	return a.attachTickets(ctx, c, roundID, TicketFile{Kind: "document", FileID: m.Document.FileID, Name: name}, true)
}

// AttachTickets: the tickets themselves — kept with the meeting, sent to the partner, linked from the calendar
func (a *App) AttachTickets(ctx context.Context, c Couple, roundID int64, f TicketFile) error {
	return a.attachTickets(ctx, c, roundID, f, false)
}

func (a *App) attachTickets(ctx context.Context, c Couple, roundID int64, f TicketFile, fromApp bool) error {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return err
	}
	if r.State != domain.StatePlanned && r.State != domain.StateConfirm {
		return domain.ErrWrongState
	}
	if a.Tickets(ctx, c.Space.ID, r.ID) == 0 {
		_ = a.S.SetSetting(ctx, fmt.Sprintf("tickets:%d:%d", c.Space.ID, r.ID), strconv.FormatInt(c.Me.ID, 10))
	}
	f.By = c.Me.ID
	if f.Kind != "link" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		f.Token = hex.EncodeToString(b)
		_ = a.S.SetSetting(ctx, "ticketstoken:"+f.Token, fmt.Sprintf("%d:%d", c.Space.ID, r.ID))
	}
	js, _ := json.Marshal(f)
	if err := a.S.SetSetting(ctx, fmt.Sprintf("ticketsfile:%d:%d", c.Space.ID, r.ID), string(js)); err != nil {
		return err
	}
	_ = a.S.SetSetting(ctx, fmt.Sprintf("tickets_wait:%d", c.Me.ID), "")
	i, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	cap := fmt.Sprintf("🎟 Билеты на <b>%s</b> — %s %s. Покупать не нужно.", esc(i.Title), esc(c.Me.Name), Past(c.Me.Name, "прислал"))
	open := tg.Keyboard(tg.Row(tg.App("Открыть встречу", a.appURL(fmt.Sprintf("round/%d", r.ID)))))
	if c.Partner != nil {
		if fs, ok := a.TG.(fileSender); ok && f.FileID != "" {
			_ = fs.SendFile(ctx, c.Partner.ChatID, f.Kind, f.FileID, cap)
		} else if f.URL != "" {
			a.send(ctx, c.Partner.ChatID, cap+"\n"+esc(f.URL), tg.Keyboard(tg.Row(tg.Link("Открыть билеты", f.URL)), tg.Row(tg.App("Открыть встречу", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
		}
	}
	mine := fmt.Sprintf("✓ Билеты сохранены к «%s»%s. Ссылка на них — в событии календаря.", esc(i.Title), map[bool]string{true: " и отправлены " + esc(NameDat(c.PartnerName())), false: ""}[c.Partner != nil])
	if !fromApp || c.Partner != nil {
		a.send(ctx, c.Me.ChatID, mine, open)
	}
	a.Live.From(ctx, c.Space.ID)
	return nil
}

// TicketsByToken: the file behind a /t/<token> link (for the calendar and the app)
func (a *App) TicketsByToken(ctx context.Context, token string) (TicketFile, bool) {
	var sp, rd int64
	if _, err := fmt.Sscanf(a.S.Setting(ctx, "ticketstoken:"+token), "%d:%d", &sp, &rd); err != nil || sp == 0 {
		return TicketFile{}, false
	}
	f := a.TicketFile(ctx, sp, rd)
	return f, f.Token == token
}

// SendPick: «посмотри, что нашлось» — the partner gets the list in the bot, marks what they like
func (a *App) SendPick(ctx context.Context, c Couple, pickID int64) error {
	if c.Partner == nil {
		return ErrNotPaired
	}
	p, err := a.S.Pick(ctx, c.Space.ID, pickID)
	if err != nil {
		return err
	}
	var top []string
	for k, id := range p.IDs {
		if k == 3 {
			break
		}
		if i, err := a.S.Idea(ctx, c.Space.ID, id); err == nil {
			top = append(top, "• "+esc(i.Title))
		}
	}
	a.send(ctx, c.Partner.ChatID, fmt.Sprintf("📋 %s %s подборку: <b>%s</b> — %d %s.\n%s\n\nОтметь, что нравится, — совпадения появятся у вас обоих.",
		esc(c.Me.Name), Past(c.Me.Name, "прислал"), esc(p.Title), len(p.IDs), plural(len(p.IDs), "вариант", "варианта", "вариантов"), strings.Join(top, "\n")),
		tg.Keyboard(tg.Row(tg.App("Открыть подборку", a.appURL(fmt.Sprintf("pick/%d", p.ID))))))
	return nil
}

func plural(n int, one, few, many string) string {
	m := n % 100
	switch {
	case m >= 11 && m <= 14:
		return many
	case m%10 == 1:
		return one
	case m%10 >= 2 && m%10 <= 4:
		return few
	}
	return many
}

// TicketsBytes: the file itself, fetched from Telegram when someone opens the /t/ link
func (a *App) TicketsBytes(ctx context.Context, token string) ([]byte, TicketFile, error) {
	f, ok := a.TicketsByToken(ctx, token)
	if !ok || f.FileID == "" {
		return nil, f, store.ErrNotFound
	}
	g, ok := a.TG.(interface {
		File(ctx context.Context, fileID string, max int64) ([]byte, error)
	})
	if !ok {
		return nil, f, store.ErrNotFound
	}
	b, err := g.File(ctx, f.FileID, 20<<20)
	return b, f, err
}
