package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"vdvoem/internal/ai"
	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// A post forwarded from a Telegram channel (often a private one, where cheap tickets are announced):
// the local model pulls out the events, the server checks every field against the post's own text —
// a title, a price or a day that is not in the post is dropped — and the events land in «Афиша» as drafts.

// Afisha: found outside the couple — by the scout or in a forwarded channel post (drafts until marked)
func Afisha(i store.Idea) bool { return i.Source == "scout" || i.Source == "channel" }

type ChannelPost struct {
	From  string   `json:"from"` // the channel's title
	Text  string   `json:"text"`
	Link  string   `json:"link,omitempty"`  // t.me/<channel>/<id> when the channel is public
	Links []string `json:"links,omitempty"` // links hidden under words in the post
}

type postJob struct {
	Space  int64       `json:"space"`
	Member int64       `json:"member"`
	Chat   int64       `json:"chat"`
	Post   ChannelPost `json:"post"`
}

// Repost queues a forwarded post for reading (the reply comes in the bot in a few seconds).
func (a *App) Repost(ctx context.Context, c Couple, chat int64, p ChannelPost) error {
	p.Text = clip(p.Text, 3500)
	key := fmt.Sprintf("post:%d:%d", c.Space.ID, len(p.Text)) + p.Link + clip(p.Text, 40)
	return a.S.Enqueue(ctx, "post", a.now(), postJob{Space: c.Space.ID, Member: c.Me.ID, Chat: chat, Post: p}, key)
}

var postSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"events": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":    map[string]any{"type": "string"},
				"date":     map[string]any{"type": "string"},
				"time":     map[string]any{"type": "string"},
				"price":    map[string]any{"type": "integer"},
				"venue":    map[string]any{"type": "string"},
				"category": map[string]any{"type": "string", "enum": []string{"food", "culture", "walk", "sport", "other"}},
				"url":      map[string]any{"type": "string"},
			},
			"required": []string{"title"},
		}},
	},
	"required": []string{"events"},
}

type postEvent struct {
	Title    string `json:"title"`
	Date     string `json:"date"`
	Time     string `json:"time"`
	Price    int64  `json:"price"`
	Venue    string `json:"venue"`
	Category string `json:"category"`
	URL      string `json:"url"`
}

func (a *App) runPost(ctx context.Context, j postJob) error {
	c, err := a.coupleOfSpace(ctx, j.Space)
	if err != nil {
		return nil
	}
	for _, m := range []*store.Member{&c.Me, c.Partner} { // the reply goes to the one who forwarded it
		if m != nil && m.ID == j.Member {
			c.Me = *m
		}
	}
	loc := c.Loc()
	now := a.now().In(loc)
	var events []postEvent
	if a.AI.Enabled() {
		events = a.readPost(ctx, j.Post.Text, now)
	}
	events = checkEvents(events, j.Post, now)
	if len(events) == 0 { // nothing certain: the post goes to the bank as one idea, the usual parser takes it
		res, err := a.AddIdea(ctx, c, firstLine(j.Post.Text)+postURL(j.Post))
		if err == nil && res.Existing == nil {
			a.send(ctx, j.Chat, "Не нашёл в посте событий с точной датой — сохранил его в копилку как идею.", nil)
		}
		return nil
	}
	var made []store.Idea
	for _, e := range events {
		p := ScoutPick{Title: e.Title, URL: e.URL, Address: e.Venue, Category: e.Category, Why: "из канала «" + clip(j.Post.From, 40) + "»"}
		if p.URL == "" {
			p.URL = j.Post.Link
		}
		if e.Date != "" {
			p.Starts = e.Date
			if e.Time != "" {
				p.Starts += "T" + e.Time
			}
		}
		if e.Price > 0 {
			p.PriceRub = e.Price * 2 // posts give the price of a ticket; the app counts for two
			p.Why += fmt.Sprintf(": билет %s", domain.Money(e.Price*100))
		}
		i, ok := a.pickIdea(ctx, p, j.Space, j.Member, "channel", loc, a.now())
		if !ok {
			continue
		}
		ni, err := a.S.CreateIdea(ctx, i, a.now())
		if err != nil {
			return err
		}
		a.scheduleAnnouncement(ctx, ni)
		made = append(made, ni)
	}
	a.Live.Changed(j.Space, 0, "")
	if len(made) == 0 {
		a.send(ctx, j.Chat, "Эти события уже есть в афише (или уже прошли).", nil)
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📨 Из поста «%s» — %s в афишу:\n", esc(clip(j.Post.From, 40)), ncaseRu(len(made), "событие", "события", "событий"))
	var rows [][]tg.Button
	for k, i := range made {
		fmt.Fprintf(&b, "\n%d. <b>%s</b>", k+1, esc(i.Title))
		var meta []string
		if i.StartsAt != nil {
			meta = append(meta, ruDayTime(*i.StartsAt, loc))
		}
		if i.PriceMin > 0 {
			meta = append(meta, "≈ "+domain.Money(i.PriceMin)+" на двоих")
		}
		if len(meta) > 0 {
			b.WriteString(" — " + esc(strings.Join(meta, " · ")))
		}
		if k < 5 {
			rows = append(rows, tg.Row(tg.CB(fmt.Sprintf("%d · Хочу", k+1), fmt.Sprintf("w:%d:want", i.ID)), tg.CB(fmt.Sprintf("%d · Пропущу", k+1), fmt.Sprintf("w:%d:skip", i.ID))))
		}
	}
	b.WriteString("\n\nОтметь, что нравится — попадёт в копилку, а партнёр увидит в афише.")
	rows = append(rows, tg.Row(tg.App("Афиша", a.appURL("ideas/scout"))))
	a.send(ctx, j.Chat, b.String(), tg.Keyboard(rows...))
	return nil
}

func ncaseRu(n int, one, few, many string) string {
	m := n % 100
	w := many
	switch {
	case m >= 11 && m <= 14:
	case m%10 == 1:
		w = one
	case m%10 >= 2 && m%10 <= 4:
		w = few
	}
	return strconv.Itoa(n) + " " + w
}

func firstLine(t string) string {
	for _, l := range strings.Split(t, "\n") {
		if l = strings.TrimSpace(l); len([]rune(l)) > 3 {
			return clip(l, 120)
		}
	}
	return clip(t, 120)
}

func postURL(p ChannelPost) string {
	if len(p.Links) > 0 {
		return " " + p.Links[0]
	}
	if p.Link != "" {
		return " " + p.Link
	}
	return ""
}

// readPost: the model lists the events; checkEvents decides what is true.
func (a *App) readPost(ctx context.Context, text string, now time.Time) []postEvent {
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second) // a long post on a CPU model takes a minute or two
	defer cancel()
	sys := `Ты достаёшь мероприятия из поста телеграм-канала. Сегодня ` + now.Format("2006-01-02") + `. Ответ — ТОЛЬКО JSON по схеме.
Для каждого события: title — название как в посте; date — YYYY-MM-DD (если год не указан — ближайшая будущая дата); time — HH:MM, если указано; price — цена одного билета в рублях, если указана (минимальная); venue — площадка/адрес; category: food, culture (театр, концерт, выставка, кино), walk, sport, other; url — ссылка из поста на это событие.
Только то, что написано в посте. Нет событий — пустой список.`
	raw, err := a.AI.Structured(ctx, "post", []ai.Message{{Role: "system", Content: sys}, {Role: "user", Content: text}}, postSchema, 700)
	if err != nil {
		a.Log.Warn("read post", "err", err)
		return nil
	}
	var out struct {
		Events []postEvent `json:"events"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Events) > 12 {
		out.Events = out.Events[:12]
	}
	return out.Events
}

var (
	numRe  = regexp.MustCompile(`\d[\d\s ]*`)
	timeRe = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)
)

var ruMonths = []string{"янв", "фев", "мар", "апр", "мая|май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

// checkEvents: every field the model gave must be backed by the post's own text.
func checkEvents(in []postEvent, p ChannelPost, now time.Time) []postEvent {
	low := strings.ToLower(p.Text)
	nums := map[int64]bool{}
	for _, m := range numRe.FindAllString(p.Text, -1) {
		n, _ := strconv.ParseInt(strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return r
			}
			return -1
		}, m), 10, 64)
		nums[n] = true
	}
	links := map[string]bool{}
	for _, l := range p.Links {
		links[l] = true
	}
	var out []postEvent
	for _, e := range in {
		e.Title = strings.TrimSpace(e.Title)
		if !textBacked(e.Title, low) {
			continue
		}
		if e.Price > 0 && (!nums[e.Price] || e.Price > 200_000) {
			e.Price = 0
		}
		if e.Date != "" {
			d, err := time.ParseInLocation("2006-01-02", e.Date, now.Location())
			if err != nil || !dayInText(d, low, now) || d.Before(now.AddDate(0, 0, -1)) || d.After(now.AddDate(1, 0, 0)) {
				e.Date = ""
			}
		}
		if e.Date == "" {
			continue // without a day it is not an event we can put on the calendar
		}
		if !timeRe.MatchString(e.Time) || !strings.Contains(low, e.Time) && !strings.Contains(low, strings.Replace(e.Time, ":", ".", 1)) {
			e.Time = ""
		}
		if e.URL != "" && !links[e.URL] && !strings.Contains(p.Text, e.URL) {
			e.URL = ""
		}
		if e.Venue != "" && !textBacked(e.Venue, low) {
			e.Venue = ""
		}
		out = append(out, e)
	}
	return out
}

// textBacked: most words of s are in the post (the model may change a case ending, not invent a name)
func textBacked(s, low string) bool {
	ws := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(ws) == 0 {
		return false
	}
	hit := 0
	for _, w := range ws {
		r := []rune(w)
		if len(r) > 5 {
			r = r[:len(r)-2]
		}
		if strings.Contains(low, string(r)) {
			hit++
		}
	}
	return hit*3 >= len(ws)*2
}

// dayInText: the post names this day — «15 ноября», «15.11», «15/11»
func dayInText(d time.Time, low string, now time.Time) bool {
	day := strconv.Itoa(d.Day())
	for _, f := range []string{day + "." + fmt.Sprintf("%02d", int(d.Month())), fmt.Sprintf("%02d.%02d", d.Day(), int(d.Month())), day + "/" + strconv.Itoa(int(d.Month()))} {
		if strings.Contains(low, f) {
			return true
		}
	}
	re := regexp.MustCompile(`(?:^|\D)0?` + day + `\s*(?:-|–|и|,)?\s*(?:\d{1,2}\s*)?(?:` + ruMonths[d.Month()-1] + `)`)
	if re.MatchString(low) {
		return true
	}
	// «в субботу» / «завтра» without a number: only for the next 7 days
	if d.Sub(now) < 8*24*time.Hour {
		wd := [...]string{"воскрес", "понедел", "вторн", "сред", "четверг", "пятниц", "суббот"}[d.Weekday()]
		if strings.Contains(low, wd) || strings.Contains(low, "сегодня") || strings.Contains(low, "завтра") {
			return true
		}
	}
	return false
}
