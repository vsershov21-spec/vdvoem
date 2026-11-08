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
	var picks []ScoutPick
	if sc, ok := scanPost(j.Post.Text, now); ok { // the usual afisha format: read exactly, no model needed
		picks = []ScoutPick{showPick(sc, j.Post, loc)}
	} else if a.AI.Enabled() {
		for _, e := range checkEvents(a.readPost(ctx, j.Post.Text, now), j.Post, now) {
			picks = append(picks, eventPick(e, j.Post))
		}
	}
	if len(picks) == 0 { // nothing certain: the post goes to the bank as one idea, the usual parser takes it
		res, err := a.AddIdea(ctx, c, firstLine(j.Post.Text)+postURL(j.Post))
		if err == nil && res.Existing == nil {
			a.send(ctx, j.Chat, "Не нашёл в посте событий с точной датой — сохранил его в копилку как идею.", nil)
		}
		return nil
	}
	var made []store.Idea
	for _, p := range picks {
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

func eventPick(e postEvent, post ChannelPost) ScoutPick {
	p := ScoutPick{Title: e.Title, URL: e.URL, Address: e.Venue, Category: e.Category, Why: "из канала «" + clip(post.From, 40) + "»"}
	if p.URL == "" {
		p.URL = post.Link
	}
	p.Starts = e.Date
	if e.Time != "" {
		p.Starts += "T" + e.Time
	}
	if e.Price > 0 {
		p.PriceRub = e.Price * 2 // posts give the price of a ticket; the app counts for two
		p.Why += fmt.Sprintf(": билет %s", domain.Money(e.Price*100))
	}
	return p
}

// showPick: one show, its nearest date; the other dates and the discount go into «why»
func showPick(sc scanned, post ChannelPost, loc *time.Location) ScoutPick {
	d := sc.dates[0]
	p := ScoutPick{Title: sc.Title, Address: sc.Venue, Category: sc.Category, URL: post.Link, Why: "из канала «" + clip(post.From, 40) + "»",
		About: sc.info.About, Genres: sc.info.Genres, Cast: sc.info.Cast, Age: sc.info.Age}
	if len(post.Links) > 0 {
		p.URL = post.Links[0]
	}
	p.Starts = d.Format("2006-01-02")
	if d.Hour() != 0 || d.Minute() != 0 {
		p.Starts = d.Format("2006-01-02T15:04")
	}
	if sc.Price > 0 {
		p.PriceRub = sc.Price * 2
		p.Why += ": билет " + domain.Money(sc.Price*100)
		if sc.orig > sc.Price {
			p.Why += " вместо " + domain.Money(sc.orig*100)
		}
	}
	if len(sc.dates) > 1 {
		var more []string
		for _, x := range sc.dates[1:] {
			if len(more) == 3 {
				break
			}
			more = append(more, x.In(loc).Format("02.01"))
		}
		p.Why += " · ещё " + strings.Join(more, ", ")
	}
	return p
}

// ChannelImport: many posts at once (a chat export) → drafts in «Афиша», one quiet summary.
func (a *App) ChannelImport(ctx context.Context, spaceID int64, from string, texts []string) (int, error) {
	c, err := a.coupleOfSpace(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	loc := c.Loc()
	now := a.now().In(loc)
	n := 0
	for _, t := range texts {
		sc, ok := scanPost(t, now)
		if !ok {
			continue
		}
		i, ok := a.pickIdea(ctx, showPick(sc, ChannelPost{From: from, Text: t}, loc), spaceID, c.Me.ID, "channel", loc, a.now())
		if !ok {
			continue
		}
		ni, err := a.S.CreateIdea(ctx, i, a.now())
		if err != nil {
			return n, err
		}
		a.scheduleAnnouncement(ctx, ni)
		n++
	}
	if n > 0 {
		a.Live.Changed(spaceID, 0, "")
		msg := fmt.Sprintf("📨 Из канала «%s» в «Афишу» — %s со скидочными билетами. Отмечайте, что нравится: я учту вкусы обоих.", esc(clip(from, 40)), ncaseRu(n, "событие", "события", "событий"))
		for _, m := range []*store.Member{&c.Me, c.Partner} {
			if m != nil {
				a.send(ctx, m.ChatID, msg, tg.Keyboard(tg.Row(tg.App("Открыть афишу", a.appURL("ideas/scout")))))
			}
		}
	}
	return n, nil
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

// ---------- the deterministic reader: a typical afisha post («🔴 ЧАЙКА … ✅ 19 ноября 19.00 … 🔥4500 вместо 5500») ----------

var (
	monthStems = []struct {
		stem string
		m    time.Month
	}{{"январ", 1}, {"феврал", 2}, {"март", 3}, {"апрел", 4}, {"мая", 5}, {"май", 5}, {"июн", 6}, {"июл", 7}, {"август", 8}, {"сентябр", 9}, {"октябр", 10}, {"ноябр", 11}, {"декабр", 12}}
	dateRe    = regexp.MustCompile(`((?:\d{1,2}\s*(?:,|и|-|–)\s*)*\d{1,2})\s+(январ|феврал|март|апрел|мая|май|июн|июл|август|сентябр|октябр|ноябр|декабр)[а-я]*(?:\s*(?:в\s*)?([01]?\d|2[0-3])[.:\-]([0-5]\d))?`)
	anyTimeRe = regexp.MustCompile(`(?:^|[^\d])(1\d|2[0-2])[.:\-]([0-5]\d)(?:[^\d]|$)`)
	priceVRe  = regexp.MustCompile(`(\d[\d ]{2,6})\s*(?:₽|[рp]\.?|руб[а-я]*\.?)?\s*\(?\s*вместо\s*(\d[\d ]{2,6})?`)
	priceRRe  = regexp.MustCompile(`(?:от\s*)?(\d[\d ]{1,5}\d)\s*(?:₽|руб)`)
	priceFRe  = regexp.MustCompile(`[🔥💥]+\s*(?:по\s*)?(\d[\d ]{2,5}\d)`)
	quoteRe   = regexp.MustCompile(`«([^»]{3,60})»`)
	ageRe     = regexp.MustCompile(`[\s,]*(?:0|6|12|14|16|18)\+?$`)
	notTitle  = regexp.MustCompile(`^хит театра|^вднх$|^рамт$|^et cetera$|^космос$|^ленком$|^огоньке$|звездн|эксклюзив|раннее бронир|^премьера|акция|редкост|мест мало|спешите|^детям|состав|билет|партер|скидк|цена|^театр|^мхт|сцен[аеы]|^зал|^дк `)
)

type scanned struct {
	postEvent
	dates []time.Time // every date of the show, nearest first
	orig  int64       // the usual price, when the post gives «вместо …»
	info  store.Info
}

func digits(s string) int64 {
	n, _ := strconv.ParseInt(strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s), 10, 64)
	return n
}

func scanPost(text string, now time.Time) (scanned, bool) {
	low := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(text), "*", ""), " ", " ")
	var out scanned
	seen := map[string]bool{}
	for _, m := range dateRe.FindAllStringSubmatch(low, -1) {
		var mon time.Month
		for _, s := range monthStems {
			if s.stem == m[2] {
				mon = s.m
			}
		}
		h, mi := -1, 0
		if m[3] != "" {
			h, _ = strconv.Atoi(m[3])
			mi, _ = strconv.Atoi(m[4])
		}
		for _, ds := range regexp.MustCompile(`\d{1,2}`).FindAllString(m[1], -1) {
			d, _ := strconv.Atoi(ds)
			y := now.Year()
			if int(mon) < int(now.Month())-1 {
				y++
			}
			t := time.Date(y, mon, d, 0, 0, 0, 0, now.Location())
			if t.Day() != d || t.Before(now.Truncate(24*time.Hour).Add(-24*time.Hour)) || t.After(now.AddDate(0, 6, 0)) {
				continue
			}
			if h >= 0 {
				t = t.Add(time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute)
			}
			if k := t.Format(time.RFC3339); !seen[k] {
				seen[k] = true
				out.dates = append(out.dates, t)
			}
		}
	}
	if len(out.dates) == 0 {
		return out, false
	}
	if tm := anyTimeRe.FindStringSubmatch(low); tm != nil { // «19.00» on its own line applies to dates without a time
		h, _ := strconv.Atoi(tm[1])
		mi, _ := strconv.Atoi(tm[2])
		for k, d := range out.dates {
			if d.Hour() == 0 && d.Minute() == 0 {
				out.dates[k] = d.Add(time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute)
			}
		}
	}
	sortTimes(out.dates)
	future := out.dates[:0]
	for _, d := range out.dates {
		if d.After(now) || (d.Hour() == 0 && d.Minute() == 0 && !d.Before(now.Truncate(24*time.Hour))) {
			future = append(future, d)
		}
	}
	if out.dates = future; len(out.dates) == 0 {
		return out, false
	}
	for _, m := range priceVRe.FindAllStringSubmatch(low, -1) {
		if p := digits(m[1]); p >= 300 && p <= 100000 && (out.Price == 0 || p < out.Price) {
			out.Price = p
			if m[2] != "" {
				out.orig = digits(m[2])
			}
		}
	}
	if out.Price == 0 {
		for _, m := range append(priceFRe.FindAllStringSubmatch(low, -1), priceRRe.FindAllStringSubmatch(low, -1)...) {
			if p := digits(m[1]); p >= 300 && p <= 100000 && (out.Price == 0 || p < out.Price) {
				out.Price = p
			}
		}
	}
	lines := strings.Split(text, "\n")
	clean := func(l string) string {
		l = strings.NewReplacer("*", "", "_", "").Replace(l)
		l = strings.TrimFunc(l, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '«' && r != '"' && r != '!' && r != '?'
		})
		return strings.TrimLeft(strings.TrimRightFunc(l, func(r rune) bool {
			return r != '!' && r != '?' && !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '»' && r != '"'
		}), "!?")
	}
	ok := func(c string) bool {
		return len([]rune(c)) >= 3 && !notTitle.MatchString(strings.ToLower(c)) && !dateRe.MatchString(strings.ToLower(c))
	}
	for _, l := range lines { // the show is marked 🔴 in most afisha channels
		if strings.Contains(l, "🔴") {
			if q := quoteRe.FindStringSubmatch(l); q != nil && ok(clean(q[1])) {
				out.Title = clean(q[1])
				break
			}
			if c := clean(l); ok(c) {
				out.Title = c
				break
			}
		}
	}
	if out.Title == "" {
		for _, l := range lines {
			c := clean(l)
			letters := 0
			for _, r := range c {
				if unicode.IsLetter(r) {
					letters++
				}
			}
			if letters >= 4 && strings.ToUpper(c) == c && ok(c) {
				out.Title = c
				break
			}
		}
	}
	if out.Title == "" {
		if q := quoteRe.FindStringSubmatch(text); q != nil && ok(clean(q[1])) {
			out.Title = clean(q[1])
		}
	}
	out.Title = strings.Trim(ageRe.ReplaceAllString(strings.Trim(out.Title, "«»\" "), ""), "«»\" ,")
	if out.Title == "" {
		return out, false
	}
	for _, l := range lines {
		ll := strings.ToLower(l)
		if len([]rune(l)) < 80 && regexp.MustCompile(`театр|мхт|дк |филармон|концертн|дом музыки|кремл|оперетт|мюзик-холл|et cetera`).MatchString(ll) {
			if c := clean(l); c != "" && c != out.Title {
				out.Venue = c
				break
			}
		}
	}
	out.info = InfoFromText(out.Title, text)
	out.Category = "culture"
	if regexp.MustCompile(`гастро|ужин|ресторан|дегустац`).MatchString(low) && !strings.Contains(low, "театр") {
		out.Category = "food"
	}
	return out, true
}

func sortTimes(t []time.Time) {
	for i := 1; i < len(t); i++ {
		for j := i; j > 0 && t[j].Before(t[j-1]); j-- {
			t[j], t[j-1] = t[j-1], t[j]
		}
	}
}
