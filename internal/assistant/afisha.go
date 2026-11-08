package assistant

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/app"
	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

// The afisha tool digs through what the scout collected (and, optionally, the whole bank): the model
// only turns a request into filters; the server filters and ranks by both people's learned taste.

type afishaQ struct {
	Date      string   `json:"date"`    // YYYY-MM-DD
	Weekend   bool     `json:"weekend"` // the nearest Saturday or Sunday
	Part      string   `json:"part"`    // morning | day | evening
	MaxRub    int64    `json:"max_price_rub"`
	Include   []string `json:"categories"`
	Exclude   []string `json:"exclude_categories"`
	NotWords  []string `json:"exclude_words"`
	Query     string   `json:"query"`
	Horizon   string   `json:"horizon"` // week | later | any
	WholeBank bool     `json:"include_bank"`
	Limit     int      `json:"limit"`
	Moods     []string `json:"moods"`
}

func (r *run) afisha(ctx context.Context, q afishaQ) any {
	loc := r.c.Loc()
	ideas, _ := r.as.App.S.Ideas(ctx, r.c.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	t := r.as.App.Tastes(ctx, r.c)
	now := r.as.now().In(loc)
	if q.Limit <= 0 || q.Limit > 6 {
		q.Limit = 4
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
	}
	var hits, late []hit
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure || (!q.WholeBank && !app.Afisha(i)) {
			continue
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
		if len(q.Include) > 0 && !in(q.Include, i.Category) || in(q.Exclude, i.Category) {
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
		if skip || q.Query != "" && !strings.Contains(low, stemQ(strings.ToLower(q.Query))) {
			continue
		}
		if !fitsDay(i, q, now, loc) {
			continue
		}
		if len(q.Moods) > 0 && app.MoodFit(i, q.Moods) == 0 && len(hits) >= 0 {
			// a mood was asked: ideas that don't fit it wait at the end (kept only if nothing fits)
			late = append(late, hit{i, t.Of(i)})
			continue
		}
		hits = append(hits, hit{i, t.Of(i)})
	}
	score := func(h hit) float64 { return h.m.Both + 0.35*app.MoodFit(h.i, q.Moods) }
	sort.SliceStable(hits, func(a, b int) bool { return score(hits[a]) > score(hits[b]) })
	if len(hits) == 0 && len(q.Moods) > 0 && len(late) > 0 && len(q.Moods) > 1 {
		hits = late // two moods asked and nothing fits both: show the best of the rest
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
	res := map[string]any{"events": out, "found": len(hits), "total": len(hits)}
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

// ---------- rules: the same tool from plain Russian ----------

var (
	priceRe = regexp.MustCompile(`(?:до|не дороже|дешевле|максимум)\s*(\d[\d\s]{2,6})`)
	kRe     = regexp.MustCompile(`(?:до|не дороже|дешевле|максимум|за)\s*(\d{1,3}(?:[.,]\d)?)\s*(?:тыс|тысяч|к\b|k\b)`)
	notRe   = regexp.MustCompile(`(?:без|кроме|не хочу|только не)\s+([а-яё]+)`)
)

var catWords = map[string]string{"поесть": "food", "ресторан": "food", "кафе": "food", "ужин": "food", "еда": "food", "бар": "food",
	"театр": "culture", "спектак": "culture", "музей": "culture", "кино": "culture", "выставк": "culture", "концерт": "culture", "опер": "culture", "балет": "culture",
	"гулять": "walk", "прогул": "walk", "парк": "walk", "спорт": "sport", "каток": "sport", "лыж": "sport", "стендап": "other", "квиз": "other"}

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
	case has(t, "анонс", "через месяц", "через пару месяц", "месяца вперёд", "месяцы вперёд", "месяца вперед", "месяцы вперед", "потом", "на будущ", "январ", "феврал", "март", "апрел", "декабр", "весн", "летом", "на лето", "новый год", "новогодн"):
		q.Horizon = "later"
	case has(t, "на неделе", "эту неделю", "этой неделе", "выходн", "сегодня", "завтра"):
		q.Horizon = "week"
	}
	q.WholeBank = !has(t, "афиш", "скаут", "нашёл", "нашел", "анонс")
	return q
}

// memoryFromText: «запомни, что Настя не любит стендап», «я не люблю ужастики» → the sentence to keep.
var memRe = regexp.MustCompile(`(?i)^(?:запомни|учти|имей в виду)[,:\s-]+(?:что\s+)?(.+)$`)

func memoryFromText(text string) string {
	if m := memRe.FindStringSubmatch(strings.TrimSpace(text)); m != nil && !strings.Contains(m[1], "http") {
		return m[1]
	}
	t := strings.ToLower(text)
	if has(t, "я не люблю", "я люблю", "мы не любим", "мы любим", "мне не нравится", "мне нравится", "мне нравятся", "нам нравится", "нам нравятся",
		"не предлагай", "у меня аллерг", "я не ем", "мы не едим", "я не пью", "обожаю", "обожаем", "терпеть не могу", "ненавижу", "не переношу", "я вегетариан", "я веган") &&
		!strings.Contains(t, "?") {
		return strings.TrimSpace(text)
	}
	return ""
}

var goalRe = regexp.MustCompile(`(?i)(?:следи|найди|поищи|ищи|дай знать|сообщи|напомни|напиши|скажи)[^,.]*?(?:когда|если|за)?\s*(.+)|хотим\s+(?:сходить\s+)?(?:в|на)\s+(.+)`)

func goalFromText(text string) string {
	t := strings.ToLower(text)
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
