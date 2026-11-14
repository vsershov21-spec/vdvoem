package assistant

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/app"
	"vdvoem/internal/domain"
	"vdvoem/internal/leisure"
	"vdvoem/internal/store"
)

// The afisha tool digs through what the scout collected (and, optionally, the whole bank): the model
// only turns a request into filters; the server filters and ranks by both people's learned taste.

type afishaQ struct {
	Date      string            `json:"date"`    // YYYY-MM-DD
	Weekend   bool              `json:"weekend"` // the nearest Saturday or Sunday
	Part      string            `json:"part"`    // morning | day | evening
	MaxRub    int64             `json:"max_price_rub"`
	Include   []string          `json:"categories"`
	Exclude   []string          `json:"exclude_categories"`
	NotWords  []string          `json:"exclude_words"`
	Query     string            `json:"query"`
	Horizon   string            `json:"horizon"` // week | later | any
	WholeBank bool              `json:"include_bank"`
	Limit     int               `json:"limit"`
	Moods     []string          `json:"moods"`
	Sem       map[int64]float64 `json:"-"` // similarity by meaning to the request (nil — off)
	All       bool              `json:"-"` // the whole list (cards: the first few)
	Whose     string            `json:"-"` // ours | new | ""
	From, To  string            `json:"-"` // a period of days (YYYY-MM-DD)
	G         []string          `json:"-"` // kinds of leisure (джаз, баня…): an event fits if it is any of them
}

func (r *run) afisha(ctx context.Context, q afishaQ) any {
	loc := r.c.Loc()
	ideas, _ := r.as.App.S.Ideas(ctx, r.c.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	t := r.as.App.Tastes(ctx, r.c)
	now := r.as.now().In(loc)
	if q.Limit <= 0 || q.Limit > 6 {
		q.Limit = 4
	}
	if q.All {
		q.Limit = listMax
	}
	in := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	type hit struct {
		i store.Idea
		m app.Match
		k int // how surely it is the kind asked for: 2 — by its name or genre, 1 — only its description mentions it
	}
	var hits, late []hit
	planned := r.as.App.S.PlannedIdeas(ctx, r.c.Space.ID) // already agreed: it is in «Мы идём», not a new option
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure || (!q.WholeBank && !app.Afisha(i)) || planned[i.ID] {
			continue
		}
		if q.Whose == "ours" && !Ours(i) || q.Whose == "new" && Ours(i) {
			continue
		}
		kind := 0
		if len(q.G) > 0 {
			if kind = kindOf(i, q.G); kind == 0 {
				continue
			}
		}
		hz := i.Horizon
		if hz == "" {
			hz = "week"
		}
		if q.Horizon == "week" || q.Horizon == "later" {
			if app.Afisha(i) && hz != q.Horizon {
				continue
			}
		}
		// a kind asked for says more than a category: a stand-up filed under «культура» is still a stand-up
		if len(q.Include) > 0 && len(q.G) == 0 && !in(q.Include, i.Category) || in(q.Exclude, i.Category) {
			continue
		}
		if q.MaxRub > 0 && i.PriceMin > q.MaxRub*100 {
			continue
		}
		low := strings.ToLower(i.Title + " " + i.Address + " " + i.Why)
		skip := false
		for _, w := range q.NotWords {
			if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(low, stemQ(w)) {
				skip = true
			}
		}
		if skip || q.Query != "" && !strings.Contains(low, stemQ(strings.ToLower(q.Query))) && q.Sem[i.ID] < semMin {
			continue
		}
		if !fitsDay(i, q, now, loc) {
			continue
		}
		if len(q.Moods) > 0 && app.MoodFit(i, q.Moods) == 0 && len(hits) >= 0 {
			// a mood was asked: ideas that don't fit it wait at the end (kept only if nothing fits)
			late = append(late, hit{i, t.Of(i), kind})
			continue
		}
		hits = append(hits, hit{i, t.Of(i), kind})
	}
	// a kind asked for: when some are surely it (by name or genre), those mentioned only in passing go — an
	// exhibition whose text says «кинохроника» is not «кино»
	if len(q.G) > 0 {
		sure := hits[:0:0]
		for _, h := range hits {
			if h.k == 2 {
				sure = append(sure, h)
			}
		}
		if len(sure) > 0 {
			hits = sure
		}
	}
	score := func(h hit) float64 {
		sc := h.m.Both + 0.35*app.MoodFit(h.i, q.Moods)
		// a day was asked: what happens exactly then goes before ideas that can be any day
		if q.Date != "" || q.Weekend {
			if h.i.StartsAt != nil {
				d := h.i.StartsAt.In(loc)
				if d.Format("2006-01-02") == q.Date || q.Weekend && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday) {
					sc += 0.3
				}
			} else {
				sc -= 0.05
			}
		}
		if v := q.Sem[h.i.ID]; v > 0.5 { // closer in meaning to what was asked — a little higher (scores sit in 0.45–0.65)
			sc += v - 0.5
		}
		return sc
	}
	sort.SliceStable(hits, func(a, b int) bool { return score(hits[a]) > score(hits[b]) })
	if len(hits) == 0 && len(q.Moods) > 0 && len(late) > 0 && len(q.Moods) > 1 {
		hits = late // two moods asked and nothing fits both: show the best of the rest
	}
	if q.WholeBank && q.Whose == "" && !q.All {
		// the bank's ideas carry marks and so rank higher; take turns, so what the scout and the channels found
		// (and you have not seen) is always in the first answer too
		var ours, news []hit
		for _, h := range hits {
			if Ours(h.i) {
				ours = append(ours, h)
			} else {
				news = append(news, h)
			}
		}
		if len(ours) > 0 && len(news) > 0 {
			mixed := make([]hit, 0, len(hits))
			a, b := ours, news
			if score(news[0]) > score(ours[0]) {
				a, b = news, ours
			}
			for len(a) > 0 || len(b) > 0 {
				if len(a) > 0 {
					mixed, a = append(mixed, a[0]), a[1:]
				}
				if len(b) > 0 {
					mixed, b = append(mixed, b[0]), b[1:]
				}
			}
			hits = mixed
		}
	}
	out := []map[string]any{}
	for k, h := range hits {
		if k == q.Limit {
			break
		}
		b := r.brief(h.i)
		b["match_percent"] = int(h.m.Both * 100)
		if h.i.Why != "" {
			b["why"] = h.i.Why
		}
		if h.i.StartsAt != nil {
			b["when"] = domain.When(*h.i.StartsAt, loc)
		}
		out = append(out, b)
		note := app.MatchNote(h.m, r.c.PartnerName())
		if h.i.Why != "" {
			note = strings.TrimSpace(h.i.Why + " " + note)
		}
		r.addCard(ideaCard(h.i, note))
	}
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		if len(ids) < 300 {
			ids = append(ids, h.i.ID)
		}
	}
	res := map[string]any{"events": out, "found": len(hits), "total": len(hits), "ids": ids}
	if len(hits) == 0 {
		res["note"] = "ничего не подошло под эти условия — можно ослабить фильтры или добавить цель скауту (add_goal)"
	}
	return res
}

func stemQ(w string) string {
	r := []rune(w)
	if len(r) > 5 {
		return string(r[:len(r)-2])
	}
	return w
}

func fitsDay(i store.Idea, q afishaQ, now time.Time, loc *time.Location) bool {
	if q.From != "" && !inPeriod(i, q.From, q.To, loc) {
		return false
	}
	if q.Date == "" && !q.Weekend && q.Part == "" {
		return true
	}
	// a place without a date (a restaurant, a long exhibition) fits any day it is on
	if i.StartsAt == nil || (i.EndsAt != nil && i.EndsAt.Sub(*i.StartsAt) > 30*time.Hour) {
		if q.Date != "" {
			d, err := time.ParseInLocation("2006-01-02", q.Date, loc)
			if err == nil && i.EndsAt != nil && i.EndsAt.Before(d) {
				return false
			}
		}
		return true
	}
	s := i.StartsAt.In(loc)
	day := s.Format("2006-01-02")
	if q.Date != "" && day != q.Date {
		return false
	}
	if q.Weekend && q.Date == "" {
		if !(s.Weekday() == time.Saturday || s.Weekday() == time.Sunday) || s.Sub(now) > 9*24*time.Hour {
			return false
		}
	}
	if h, ok := partHours[q.Part]; ok && s.Hour() != 0 && (s.Hour() < h[0] || s.Hour() >= h[1]) {
		return false
	}
	return true
}

// fitsKinds: the event is one of the kinds asked for — by its genres or by its own words
// kindOf: 2 — the idea is this kind by its name or genres; 1 — only its description or the scout's reason
// mention it; 0 — not this kind (a cuisine is only ever a place to eat)
func kindOf(i store.Idea, g []string) int {
	best := 0
	for _, k := range g {
		if leisure.CatOf(k) == "food" && i.Category != "food" {
			continue
		}
		if slices.Contains(i.Info.Genres, k) || leisure.Fits(k, i.Title) {
			return 2
		}
		if leisure.Fits(k, i.Why+" "+i.Info.About+" "+strings.Join(i.Info.Genres, " ")) {
			best = 1
		}
	}
	return best
}

func fitsKinds(i store.Idea, g []string) bool {
	text := i.Title + " " + i.Why + " " + i.Info.About + " " + strings.Join(i.Info.Genres, " ")
	for _, k := range g {
		// a cuisine is a place to eat: a Japanese exhibition is not «азиатская кухня»
		if leisure.CatOf(k) == "food" && i.Category != "food" {
			continue
		}
		for _, x := range i.Info.Genres {
			if x == k {
				return true
			}
		}
		if leisure.Fits(k, text) {
			return true
		}
	}
	return false
}

// inPeriod: an event on one of the days, or a place / an exhibition open then
func inPeriod(i store.Idea, from, to string, loc *time.Location) bool {
	if i.StartsAt == nil {
		return i.EndsAt == nil || i.EndsAt.In(loc).Format("2006-01-02") >= from
	}
	s := i.StartsAt.In(loc).Format("2006-01-02")
	long := i.EndsAt != nil && i.EndsAt.Sub(*i.StartsAt) > 30*time.Hour
	if long {
		return s <= to && i.EndsAt.In(loc).Format("2006-01-02") >= from
	}
	return s >= from && s <= to
}

// ---------- rules: the same tool from plain Russian ----------

var (
	priceRe = regexp.MustCompile(`(?:до|не дороже|дешевле|максимум|в пределах)\s*(\d[\d\s]{2,6})`)
	kRe     = regexp.MustCompile(`(?:до|не дороже|дешевле|максимум|за)\s*(\d{1,3}(?:[.,]\d)?)\s*(?:тыс|тысяч|к(?:$|[^\p{L}])|k\b)`)
	notRe   = regexp.MustCompile(`(?:без|кроме|не хочу|только не)\s+([а-яё]+)`)
	// «не дороже трёх тысяч», «в пределах пяти тысяч»
	wordKRe = regexp.MustCompile(`(?:до|не дороже|дешевле|максимум|в пределах|за)\s+(одной|одна|двух|две|тр[её]х|три|четыр[её]х|четыре|пяти|пять|шести|семи|восьми|десяти|тысячи)\s*(?:тыс|тысяч)?`)
)

var wordK = map[string]int64{"одной": 1, "одна": 1, "тысячи": 1, "двух": 2, "две": 2, "трёх": 3, "трех": 3, "три": 3, "четырёх": 4, "четырех": 4, "четыре": 4,
	"пяти": 5, "пять": 5, "шести": 6, "семи": 7, "восьми": 8, "десяти": 10}

var catWords = map[string]string{"поесть": "food", " еде": "food", " еды": "food", " едой": "food", "покушать": "food", "перекус": "food", "гастро": "food", "вкусн": "food", "ресторан": "food", "кафе": "food", "ужин": "food", "еда": "food", "бар": "food",
	"театр": "culture", "спектак": "culture", "музей": "culture", "кино": "culture", "выставк": "culture", "концерт": "culture", "опер": "culture", "балет": "culture",
	"гуля": "walk", "прогул": "walk", "парк": "walk", "спорт": "sport", "каток": "sport", "лыж": "sport", "стендап": "other", "квиз": "other"}

func afishaFromText(t string, now time.Time) afishaQ {
	var q afishaQ
	if d := dayFrom(t, now); d != "" {
		q.Date = d
	} else if has(t, "выходн") {
		q.Weekend = true
	}
	switch {
	case has(t, "вечер"):
		q.Part = "evening"
	case has(t, "утр"):
		q.Part = "morning"
	case has(t, "днём", "днем"):
		q.Part = "day"
	case has(t, "поужинат", "ужин"):
		q.Part = "evening"
	}
	if m := kRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
		q.MaxRub = int64(n * 1000)
	} else if m := priceRe.FindStringSubmatch(t); m != nil {
		q.MaxRub, _ = strconv.ParseInt(strings.ReplaceAll(m[1], " ", ""), 10, 64)
	} else if m := wordKRe.FindStringSubmatch(t); m != nil && strings.Contains(t, "тысяч") {
		q.MaxRub = wordK[m[1]] * 1000
	} else if has(t, "недорог", "дешев", "бюджетн") {
		q.MaxRub = 2000
	}
	excluded := map[string]bool{}
	for _, m := range notRe.FindAllStringSubmatch(t, -1) {
		w := m[1]
		matched := false
		for k, c := range catWords {
			if strings.HasPrefix(w, k) || strings.HasPrefix(k, stemQ(w)) || len([]rune(k)) >= 4 && strings.HasPrefix(w, string([]rune(k)[:4])) {
				q.Exclude = append(q.Exclude, c)
				q.NotWords = append(q.NotWords, k)
				excluded[k] = true
				matched = true
			}
		}
		if !matched {
			q.NotWords = append(q.NotWords, w)
		}
	}
	for k, c := range catWords {
		if strings.Contains(t, k) && !excluded[k] {
			if c == "other" {
				q.Query = k
			} else {
				q.Include = append(q.Include, c)
			}
		}
	}
	switch {
	case has(t, "анонс", "через месяц", "через пару месяц", "месяца вперёд", "месяцы вперёд", "месяца вперед", "месяцы вперед", "потом", "на будущ", "январ", "феврал", "март", "апрел", "декабр", "весн", "летом", "на лето", "зимой", "этой зимой", "новый год", "новогодн"):
		q.Horizon = "later"
	case has(t, "на неделе", "эту неделю", "этой неделе", "выходн", "сегодня", "завтра"):
		q.Horizon = "week"
	}
	q.WholeBank = !has(t, "афиш", "скаут", "нашёл", "нашел", "анонс", "агент", "нарыл", "подборк")
	return q
}

// memoryFromText: «запомни, что Настя не любит стендап», «я не люблю ужастики» → the sentence to keep.
var memRe = regexp.MustCompile(`(?i)^(?:запомни|учти|имей в виду)[,:\s-]+(?:что\s+)?(.+)$`)

func memoryFromText(text string) string {
	if m := memRe.FindStringSubmatch(strings.TrimSpace(text)); m != nil && !strings.Contains(m[1], "http") {
		return m[1]
	}
	t := strings.ToLower(text)
	if strings.HasPrefix(strings.TrimSpace(t), "не люблю ") || strings.HasPrefix(strings.TrimSpace(t), "люблю ") || has(t, "я не люблю", "я люблю", "мы не любим", "мы любим", "мне не нравится", "мне нравится", "мне нравятся", "нам нравится", "нам нравятся",
		"не предлагай", "у меня аллерг", "я не ем", "мы не едим", "я не пью", "обожаю", "обожаем", "терпеть не могу", "ненавижу", "не переношу", "я вегетариан", "я веган") &&
		!strings.Contains(t, "?") {
		return strings.TrimSpace(text)
	}
	return ""
}

var goalRe = regexp.MustCompile(`(?i)(?:следи|найди|поищи|ищи|дай знать|сообщи|напомни|напиши|скажи)[^,.]*?(?:когда|если|за)?\s*(.+)|хотим\s+(?:сходить\s+)?(?:в|на)\s+(.+)`)

func goalFromText(text string) string {
	t := strings.ToLower(text)
	// «хотим на Земфиру» is a goal (a name), «хотим на природу» is a wish for now — a search
	if has(t, "хотим в ", "хотим на ", "хотим сходить на", "хотим сходить в ") && !has(t, "следи", "дай знать", "сообщи", "напомни", "когда") {
		k := strings.Index(t, "хотим")
		later := has(t, "январ", "феврал", "март", "апрел", "в мае", "июн", "июл", "август", "сентябр", "октябр", "декабр", "весн", "летом", "на лето", "осен", "зим", "через ", "новый год")
		if rest := text[k:]; !later && !regexp.MustCompile(`\s[«"A-ZА-ЯЁ]`).MatchString(rest) {
			return ""
		}
	}
	if !has(t, "следи", "дай знать", "сообщи когда", "сообщи, когда", "напомни когда", "хотим сходить на", "хотим сходить в ", "хотим в ", "хотим на ", "когда приедет", "когда появ", "когда начнут", "когда откро") &&
		!(has(t, "найди", "поищи", "ищи") && has(t, "на весну", "на лето", "на осень", "на зиму", "весной", "летом", "через месяц", "через пару", "в январе", "в феврале", "в марте", "в апреле", "в мае", "в декабре", "на новый год")) {
		return ""
	}
	if m := goalRe.FindStringSubmatch(text); m != nil {
		for _, g := range m[1:] {
			if g = strings.TrimSpace(strings.Trim(g, ".!?")); g != "" {
				return g
			}
		}
	}
	return strings.TrimSpace(text)
}
