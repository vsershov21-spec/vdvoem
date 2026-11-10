package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// Скаут: a Claude agent runs once a week (a scheduled task on the owner's account), reads the couple's
// taste brief, researches city listings and posts a draft of 15–20 events. The best five go to the bank
// and to the bot; the rest wait in «Афиша недели» for the assistant to dig through. Text only —
// images stay on the listing sites and load in the app.

const (
	ScoutTop     = 5
	ScoutPoolMax = 25
	scoutTTL     = 14 * 24 * time.Hour
)

// ScoutBrief is what the agent needs to know about a couple — no names, no chat ids.
type ScoutBrief struct {
	Space     int64            `json:"space"`
	City      string           `json:"city"`
	Budget    int64            `json:"budget_rub"` // for two, 0 = not set
	FreeDays  []string         `json:"free_days"`  // the next two weekends without a planned meeting
	BothWant  []string         `json:"both_want"`
	OneWants  []string         `json:"one_wants"`
	Skipped   []string         `json:"skipped"`
	Repeat    []string         `json:"want_again"`
	Been      []string         `json:"been_recently"`
	Existing  []string         `json:"already_in_bank"`
	Scout     map[string]any   `json:"scout_feedback"` // what they did with earlier picks
	Favorites map[string]int   `json:"category_score"` // food/culture/walk/sport/other
	People    []TasteProfile   `json:"people"`         // each person separately: learned likes/dislikes + what they said
	Goals     []ScoutGoal      `json:"goals"`          // things to watch for, also months ahead
	Watch     []map[string]any `json:"recheck"`        // links to re-check price/date/sold out
	Questions []map[string]any `json:"questions"`      // asked in the app, waiting for the scout: answer via POST /agent/answers
	Enrich    []map[string]any `json:"enrich"`         // events without a description/genres/cast: fill them via POST /agent/prices
}

type ScoutGoal struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
}

func (a *App) ScoutBriefs(ctx context.Context) ([]ScoutBrief, error) {
	spaces, err := a.S.Spaces(ctx)
	if err != nil {
		return nil, err
	}
	out := []ScoutBrief{}
	for _, sp := range spaces {
		out = append(out, a.scoutBrief(ctx, sp))
	}
	return out, nil
}

func (a *App) scoutBrief(ctx context.Context, sp store.Space) ScoutBrief {
	loc := sp.Loc()
	b := ScoutBrief{Space: sp.ID, City: sp.City, Budget: sp.BudgetKop / 100, Favorites: map[string]int{}, Scout: map[string]any{}}
	if b.City == "" {
		b.City = "Москва"
	}
	ideas, _ := a.S.Ideas(ctx, sp.ID, domain.IdeaActive, domain.IdeaDone, domain.IdeaDraft)
	w := map[domain.Wish]int{domain.WishVery: 2, domain.WishWant: 1, domain.WishSkip: -2}
	var sLiked, sSkipped, sIgnored []string
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure {
			continue
		}
		t := i.Title
		if i.Status == domain.IdeaActive {
			b.Existing = append(b.Existing, t)
		}
		want := func(x domain.Wish) bool { return x == domain.WishWant || x == domain.WishVery }
		switch {
		case want(i.WishA) && want(i.WishB):
			b.BothWant = append(b.BothWant, t)
		case want(i.WishA) || want(i.WishB):
			b.OneWants = append(b.OneWants, t)
		}
		if i.WishA == domain.WishSkip || i.WishB == domain.WishSkip {
			b.Skipped = append(b.Skipped, t)
		}
		if i.AgainYes > 0 {
			b.Repeat = append(b.Repeat, t)
		}
		b.Favorites[i.Category] += w[i.WishA] + w[i.WishB] + 2*i.AgainYes - 2*i.AgainNo
		if i.Source == "scout" {
			switch {
			case want(i.WishA) || want(i.WishB):
				sLiked = append(sLiked, t)
			case i.WishA == domain.WishSkip || i.WishB == domain.WishSkip:
				sSkipped = append(sSkipped, t)
			case i.Status == domain.IdeaActive:
				sIgnored = append(sIgnored, t)
			}
		}
	}
	b.Scout["liked"], b.Scout["skipped"], b.Scout["not_marked"] = nz(sLiked), nz(sSkipped), nz(sIgnored)
	rounds, _ := a.S.RecentRounds(ctx, sp.ID, 12)
	busy := map[string]bool{}
	for _, r := range rounds {
		if r.State == domain.StatePlanned && !r.StartsAt.IsZero() {
			busy[r.StartsAt.In(loc).Format("2006-01-02")] = true
		}
		if r.State == domain.StateDone && r.Chosen != 0 {
			if i, err := a.S.Idea(ctx, sp.ID, r.Chosen); err == nil {
				b.Been = append(b.Been, i.Title)
			}
		}
	}
	d := a.now().In(loc)
	for k := 0; k < 15 && len(b.FreeDays) < 4; k++ {
		x := d.AddDate(0, 0, k)
		if (x.Weekday() == time.Saturday || x.Weekday() == time.Sunday) && !busy[x.Format("2006-01-02")] {
			b.FreeDays = append(b.FreeDays, x.Format("2006-01-02"))
		}
	}
	b.BothWant, b.OneWants, b.Skipped, b.Repeat, b.Been, b.Existing = nz(b.BothWant), nz(b.OneWants), nz(b.Skipped), nz(b.Repeat), nz(b.Been), nz(b.Existing)
	b.People, b.Goals = []TasteProfile{}, []ScoutGoal{}
	if c, err := a.coupleOfSpace(ctx, sp.ID); err == nil {
		me, partner := a.Profiles(ctx, c)
		b.People = append(b.People, me)
		if c.Partner != nil {
			b.People = append(b.People, partner)
		}
	}
	gs, _ := a.S.Goals(ctx, sp.ID)
	for _, g := range gs {
		b.Goals = append(b.Goals, ScoutGoal{ID: g.ID, Text: g.Text})
	}
	b.Watch = a.PriceChecks(ctx, sp.ID)
	b.Enrich = a.InfoChecks(ctx, sp.ID)
	b.Questions = questionsOf(ctx, a.S, sp.ID)
	return b
}

func nz(s []string) []string {
	if s == nil {
		return []string{}
	}
	if len(s) > 60 {
		return s[:60]
	}
	return s
}

// ScoutPick is one event the agent found.
type ScoutPick struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Image    string `json:"image"`
	Address  string `json:"address"`
	Category string `json:"category"` // food | culture | walk | sport | other
	Starts   string `json:"starts"`   // RFC3339 or 2006-01-02T15:04 local; empty = any time
	Ends     string `json:"ends"`     // last day it is on, if known
	PriceRub int64  `json:"price_rub"`
	Duration int    `json:"duration_min"`
	Why      string `json:"why"`
	Score    int    `json:"score"`   // agent's own 1–10; the top ones go to the bot
	Horizon  string `json:"horizon"` // week (default) | later — an announcement 1–3 months ahead
	OnSale   string `json:"on_sale"` // when tickets go on sale, if not yet
	GoalID   int64  `json:"goal_id"` // it answers one of the couple's goals
	// what it is — only what the source says
	About  string   `json:"about"`  // 1–3 sentences
	Genres []string `json:"genres"` // комедия, драма, мюзикл, концерт…
	Cast   []string `json:"cast"`   // who plays / performs
	Age    string   `json:"age"`    // 12+
}

type ScoutResult struct {
	Added, Skipped, Promoted int
}

// ScoutPicks stores a weekly draft: duplicates are skipped, the best go to the bank and to the bot.
func (a *App) ScoutPicks(ctx context.Context, spaceID int64, picks []ScoutPick) (ScoutResult, error) {
	var res ScoutResult
	sp, err := a.S.Space(ctx, spaceID)
	if err != nil {
		return res, err
	}
	ms, _ := a.S.Members(ctx, spaceID)
	if len(ms) == 0 {
		return res, ErrNotPaired
	}
	loc := sp.Loc()
	a.ScoutCleanup(ctx, spaceID)
	// the agent's guess and the couple's own learned taste rank together
	var t *Tastes
	if c, err := a.coupleOfSpace(ctx, spaceID); err == nil {
		tt := a.Tastes(ctx, c)
		t = &tt
	}
	rank := func(p ScoutPick) float64 {
		r := float64(p.Score) / 10
		if t != nil {
			i := store.Idea{Source: "scout"}
			i.Title, i.Category, i.PriceMin, i.StartsAt = p.Title, normCategory(p.Category), p.PriceRub*100, parseWhen(p.Starts, loc)
			r = 0.5*r + 0.5*t.Of(i).Both
		}
		if p.GoalID != 0 {
			r += 0.3
		}
		return r
	}
	sort.SliceStable(picks, func(x, y int) bool { return rank(picks[x]) > rank(picks[y]) })
	if len(picks) > ScoutPoolMax {
		picks = picks[:ScoutPoolMax]
	}
	var top, later, goals []store.Idea
	now := a.now()
	for _, p := range picks {
		p.Title = clip(strings.TrimSpace(p.Title), 120)
		if p.Title == "" {
			res.Skipped++
			continue
		}
		i, ok := a.pickIdea(ctx, p, spaceID, ms[0].ID, "scout", loc, now)
		if !ok {
			res.Skipped++
			continue
		}
		if i.Horizon == "week" && len(top) < ScoutTop && (sp.BudgetKop == 0 || i.PriceMin <= sp.BudgetKop) {
			i.Status = domain.IdeaActive
		}
		ni, err := a.S.CreateIdea(ctx, i, now)
		if err != nil {
			return res, err
		}
		a.wantPicture(ctx, ni, time.Duration(res.Added)*15*time.Second)
		res.Added++
		switch {
		case ni.Status == domain.IdeaActive:
			top = append(top, ni)
			res.Promoted++
		case ni.Horizon == "later" && len(later) < 3:
			later = append(later, ni)
		}
		if p.GoalID != 0 {
			goals = append(goals, ni)
		}
		a.scheduleAnnouncement(ctx, ni)
	}
	_ = a.S.LogScout(ctx, spaceID, res.Added, 0, "", now)
	a.Live.Changed(spaceID, 0, "")
	if len(top) > 0 || len(later) > 0 {
		a.scoutDigest(ctx, ms, top, later, goals, res.Added-res.Promoted, loc)
	}
	return res, nil
}

func (a *App) scoutDigest(ctx context.Context, ms []store.Member, top, later, goals []store.Idea, more int, loc *time.Location) {
	var b strings.Builder
	if len(top) > 0 {
		b.WriteString("🔭 <b>Скаут нашёл для вас на неделю</b>\n")
	} else {
		b.WriteString("🔭 <b>Скаут нашёл для вас анонсы</b>")
	}
	var rows [][]tg.Button
	isGoal := map[int64]bool{}
	for _, g := range goals {
		isGoal[g.ID] = true
	}
	for k, i := range top {
		fmt.Fprintf(&b, "\n%d. <b>%s</b>", k+1, esc(i.Title))
		var meta []string
		if i.StartsAt != nil {
			meta = append(meta, ruDayTime(*i.StartsAt, loc))
		}
		if i.PriceMin > 0 {
			meta = append(meta, "≈ "+domain.Money(i.PriceMin))
		}
		if len(meta) > 0 {
			b.WriteString(" — " + esc(strings.Join(meta, " · ")))
		}
		if i.Why != "" {
			b.WriteString("\n<i>" + esc(i.Why) + "</i>")
		}
		if isGoal[i.ID] {
			b.WriteString("\n🎯 по вашей цели")
		}
		rows = append(rows, tg.Row(tg.CB(fmt.Sprintf("%d · Хочу", k+1), fmt.Sprintf("w:%d:want", i.ID)), tg.CB(fmt.Sprintf("%d · Пропущу", k+1), fmt.Sprintf("w:%d:skip", i.ID))))
	}
	if len(later) > 0 {
		if len(top) > 0 {
			b.WriteString("\n\n📅 <b>Анонсы на потом</b>")
		}
		for _, i := range later {
			fmt.Fprintf(&b, "\n• %s", esc(i.Title))
			if i.StartsAt != nil {
				b.WriteString(" — " + esc(ruDayTime(*i.StartsAt, loc)))
			}
			if isGoal[i.ID] {
				b.WriteString(" 🎯")
			}
		}
	}
	if more > 0 {
		fmt.Fprintf(&b, "\n\nЕщё %d — в «Афише». Спроси помощника: «что в субботу до 3000?»", more)
	}
	rows = append(rows, tg.Row(tg.App("Афиша недели", a.appURL("ideas/scout"))))
	for _, m := range ms {
		a.send(ctx, m.ChatID, b.String(), tg.Keyboard(rows...))
	}
}

// ScoutCleanup drops what nobody wanted: drafts and unmarked picks that are over or two weeks old.
// Deleted for good (no archive) — the bank stays small; a mark («хочу» / «пропущу») is what is kept.
func (a *App) ScoutCleanup(ctx context.Context, spaceID int64) int {
	ideas, _ := a.S.Ideas(ctx, spaceID, domain.IdeaDraft, domain.IdeaActive)
	now, n := a.now(), 0
	for _, i := range ideas {
		if !Afisha(i) || i.WishA != domain.WishNone || i.WishB != domain.WishNone {
			continue
		}
		over := i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Add(6*time.Hour).Before(now)
		stale := i.Horizon != "later" && now.Sub(i.CreatedAt) > scoutTTL // announcements wait for their date
		if over || stale {
			if a.S.DeleteIdea(ctx, spaceID, i.ID) == nil {
				n++
			}
		}
	}
	return n
}

// ScoutPrice is a re-check of an event with a ticket link.
type ScoutPrice struct {
	IdeaID   int64  `json:"idea_id"`
	PriceRub int64  `json:"price_rub"`
	Ends     string `json:"ends"`
	Status   string `json:"status"` // ok | sold_out | ended
	// what it is (from the event's page or a review) — only what the source says
	About  string   `json:"about"`
	Genres []string `json:"genres"`
	Cast   []string `json:"cast"`
	Age    string   `json:"age"`
	URL    string   `json:"url"` // the event's page, if the idea had none
}

func (a *App) ScoutPrices(ctx context.Context, spaceID int64, ups []ScoutPrice) (int, error) {
	sp, err := a.S.Space(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range ups {
		i, err := a.S.Idea(ctx, spaceID, u.IdeaID)
		if err != nil {
			continue
		}
		switch u.Status {
		case "ended", "sold_out":
			i.Status = domain.IdeaArchived
			if u.Status == "sold_out" {
				i.Note = strings.TrimSpace("Билеты закончились. " + i.Note)
			}
		default:
			if u.PriceRub > 0 {
				i.PriceMin, i.PriceMax = u.PriceRub*100, u.PriceRub*100
			}
			if e := parseWhen(u.Ends, sp.Loc()); e != nil {
				i.EndsAt = e
			}
			if u.About != "" || len(u.Genres) > 0 || len(u.Cast) > 0 || u.Age != "" {
				in := i.Info
				if u.About != "" {
					in.About = sentences(u.About, 400)
				}
				if len(u.Genres) > 0 {
					in.Genres = limitStr(u.Genres, 5)
				}
				if len(u.Cast) > 0 {
					in.Cast = limitStr(u.Cast, 8)
				}
				if u.Age != "" {
					in.Age = clip(u.Age, 4)
				}
				if in.Src == "" {
					in.Src = "web"
				}
				i.Info = in
			}
			if i.URL == "" && safeURL(u.URL) != "" {
				i.URL = safeURL(u.URL)
			}
		}
		if a.S.UpdateIdea(ctx, i, a.now()) == nil {
			n++
		}
	}
	_ = a.S.LogScout(ctx, spaceID, 0, n, "prices", a.now())
	a.Live.Changed(spaceID, 0, "")
	return n, nil
}

// PriceChecks lists ideas with links the agent should re-check (active, not over).
func (a *App) PriceChecks(ctx context.Context, spaceID int64) []map[string]any {
	ideas, _ := a.S.Ideas(ctx, spaceID, domain.IdeaActive)
	out := []map[string]any{}
	for _, i := range ideas {
		if i.URL == "" || i.Kind != domain.KindLeisure {
			continue
		}
		out = append(out, map[string]any{"idea_id": i.ID, "title": i.Title, "url": i.URL, "price_rub": i.PriceMin / 100})
		if len(out) == 20 {
			break
		}
	}
	return out
}

// InfoChecks: events in the afisha and the bank the assistant cannot tell about yet
func (a *App) InfoChecks(ctx context.Context, spaceID int64) []map[string]any {
	ideas, _ := a.S.Ideas(ctx, spaceID, domain.IdeaActive, domain.IdeaDraft)
	out := []map[string]any{}
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure || i.Info.About != "" && len(i.Info.Genres) > 0 {
			continue
		}
		if i.StartsAt != nil && i.StartsAt.Before(a.now()) {
			continue
		}
		m := map[string]any{"idea_id": i.ID, "title": i.Title, "venue": i.Address}
		if i.URL != "" {
			m["url"] = i.URL
		}
		out = append(out, m)
		if len(out) == 25 {
			break
		}
	}
	return out
}

func limitStr(l []string, n int) []string {
	var out []string
	for _, x := range l {
		if x = clip(x, 60); x != "" && len(out) < n {
			out = append(out, x)
		}
	}
	return out
}

func normCategory(c string) string {
	switch c {
	case "food", "culture", "walk", "sport":
		return c
	}
	return "other"
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

func safeURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		return clip(u, 600)
	}
	return ""
}

func parseWhen(s string, loc *time.Location) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, f := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return &t
		}
	}
	return nil
}

var ruWD = []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
var ruMon = []string{"", "янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

func ruDayTime(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	s := fmt.Sprintf("%s %d %s", ruWD[t.Weekday()], t.Day(), ruMon[t.Month()])
	if t.Hour() != 0 || t.Minute() != 0 {
		s += fmt.Sprintf(", %02d:%02d", t.Hour(), t.Minute())
	}
	return s
}

// scheduleAnnouncement: for events months ahead the bot speaks at the right moments —
// when tickets go on sale, and a month / a week before (only if someone wanted it by then).
func (a *App) scheduleAnnouncement(ctx context.Context, i store.Idea) {
	if i.Horizon != "later" || i.StartsAt == nil {
		return
	}
	now := a.now()
	if i.OnSale != nil && i.OnSale.After(now) {
		_ = a.S.Enqueue(ctx, "scout_note", *i.OnSale, map[string]any{"idea": i.ID, "kind": "on_sale"}, fmt.Sprintf("onsale:%d", i.ID))
	}
	for _, d := range []time.Duration{30 * 24 * time.Hour, 7 * 24 * time.Hour} {
		if at := i.StartsAt.Add(-d); at.After(now) {
			_ = a.S.Enqueue(ctx, "scout_note", at, map[string]any{"idea": i.ID, "kind": "soon", "days": int(d.Hours() / 24)}, fmt.Sprintf("soon:%d:%d", i.ID, int(d.Hours())))
		}
	}
}

func (a *App) runScoutNote(ctx context.Context, ideaID int64, kind string, days int) error {
	i, err := a.S.IdeaAny(ctx, ideaID)
	if err != nil || i.Status == domain.IdeaArchived {
		return nil
	}
	c, err := a.coupleOfSpace(ctx, i.SpaceID)
	if err != nil {
		return nil
	}
	want := func(w domain.Wish) bool { return w == domain.WishWant || w == domain.WishVery }
	if kind == "soon" && !want(i.WishA) && !want(i.WishB) {
		return nil // nobody wanted it: no nagging
	}
	text := fmt.Sprintf("🎟 Билеты на <b>%s</b> поступили в продажу.", esc(i.Title))
	if kind == "soon" {
		text = fmt.Sprintf("📅 Через %d дней — <b>%s</b>. Берём билеты?", days, esc(i.Title))
	}
	kb := tg.Keyboard(tg.Row(tg.App("Открыть", a.appURL(fmt.Sprintf("idea/%d", i.ID)))))
	if i.URL != "" {
		kb = tg.Keyboard(tg.Row(tg.Link("Билеты", i.URL), tg.App("Открыть", a.appURL(fmt.Sprintf("idea/%d", i.ID)))))
	}
	for _, m := range a.both(c) {
		a.send(ctx, m.ChatID, text, kb)
	}
	return nil
}

// coupleOfSpace: the couple as seen by its first member (for background work).
func (a *App) coupleOfSpace(ctx context.Context, spaceID int64) (Couple, error) {
	ms, err := a.S.Members(ctx, spaceID)
	if err != nil || len(ms) == 0 {
		return Couple{}, ErrNotPaired
	}
	return a.coupleOf(ctx, ms[0])
}

// pickIdea: one found event → a draft idea (false: a duplicate or already over)
func (a *App) pickIdea(ctx context.Context, p ScoutPick, spaceID, authorID int64, source string, loc *time.Location, now time.Time) (store.Idea, bool) {
	fp := domain.Fingerprint(p.URL, p.Title)
	if _, err := a.S.IdeaByFingerprint(ctx, spaceID, fp); err == nil {
		return store.Idea{}, false
	}
	i := store.Idea{Source: source, Why: clip(p.Why, 240), Parsed: true}
	i.SpaceID, i.AuthorID = spaceID, authorID
	i.Title, i.URL, i.ImageURL, i.Address = p.Title, safeURL(p.URL), safeURL(p.Image), clip(p.Address, 200)
	i.Kind, i.Category, i.Status = domain.KindLeisure, normCategory(p.Category), domain.IdeaDraft
	if p.PriceRub > 0 {
		i.PriceMin, i.PriceMax = p.PriceRub*100, p.PriceRub*100
	}
	i.DurationMin = p.Duration
	if i.DurationMin <= 0 {
		i.DurationMin = defaultDuration(i.Category)
	}
	i.StartsAt, i.EndsAt = parseWhen(p.Starts, loc), parseWhen(p.Ends, loc)
	i.OnSale = parseWhen(p.OnSale, loc)
	if i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Before(now) {
		return store.Idea{}, false
	}
	i.Horizon = "week"
	if p.Horizon == "later" || i.StartsAt != nil && i.StartsAt.Sub(now) > 21*24*time.Hour {
		i.Horizon = "later"
	}
	i.Info = store.Info{About: sentences(p.About, 400), Genres: p.Genres, Cast: p.Cast, Age: clip(p.Age, 4)}
	if len(i.Info.Cast) > 8 {
		i.Info.Cast = i.Info.Cast[:8]
	}
	if len(i.Info.Genres) == 0 {
		i.Info.Genres = GenresOf(p.Title + "\n" + p.About + "\n" + p.Address)
	}
	if !i.Info.Empty() {
		i.Info.Src = map[string]string{"scout": "web", "channel": "post"}[source]
	}
	return i, true
}
