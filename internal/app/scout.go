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
	Space     int64          `json:"space"`
	City      string         `json:"city"`
	Budget    int64          `json:"budget_rub"` // for two, 0 = not set
	FreeDays  []string       `json:"free_days"`  // the next two weekends without a planned meeting
	BothWant  []string       `json:"both_want"`
	OneWants  []string       `json:"one_wants"`
	Skipped   []string       `json:"skipped"`
	Repeat    []string       `json:"want_again"`
	Been      []string       `json:"been_recently"`
	Existing  []string       `json:"already_in_bank"`
	Scout     map[string]any `json:"scout_feedback"` // what they did with earlier picks
	Favorites map[string]int `json:"category_score"` // food/culture/walk/sport/other
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
	Score    int    `json:"score"` // agent's own 1–10; the top ones go to the bot
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
	sort.SliceStable(picks, func(x, y int) bool { return picks[x].Score > picks[y].Score })
	if len(picks) > ScoutPoolMax {
		picks = picks[:ScoutPoolMax]
	}
	var top []store.Idea
	now := a.now()
	for _, p := range picks {
		p.Title = clip(strings.TrimSpace(p.Title), 120)
		if p.Title == "" {
			res.Skipped++
			continue
		}
		fp := domain.Fingerprint(p.URL, p.Title)
		if _, err := a.S.IdeaByFingerprint(ctx, spaceID, fp); err == nil {
			res.Skipped++
			continue
		}
		i := store.Idea{Source: "scout", Why: clip(p.Why, 240), Parsed: true}
		i.SpaceID, i.AuthorID = spaceID, ms[0].ID
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
		if i.EndsAt != nil && i.EndsAt.Before(now) {
			res.Skipped++
			continue
		}
		if len(top) < ScoutTop && (sp.BudgetKop == 0 || i.PriceMin <= sp.BudgetKop) {
			i.Status = domain.IdeaActive
		}
		ni, err := a.S.CreateIdea(ctx, i, now)
		if err != nil {
			return res, err
		}
		res.Added++
		if ni.Status == domain.IdeaActive {
			top = append(top, ni)
			res.Promoted++
		}
	}
	_ = a.S.LogScout(ctx, spaceID, res.Added, 0, "", now)
	a.Live.Changed(spaceID, 0, "")
	if len(top) > 0 {
		a.scoutDigest(ctx, ms, top, res.Added-res.Promoted, loc)
	}
	return res, nil
}

func (a *App) scoutDigest(ctx context.Context, ms []store.Member, top []store.Idea, more int, loc *time.Location) {
	var b strings.Builder
	b.WriteString("🔭 <b>Скаут нашёл для вас на неделю</b>\n")
	var rows [][]tg.Button
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
		rows = append(rows, tg.Row(tg.CB(fmt.Sprintf("%d · Хочу", k+1), fmt.Sprintf("w:%d:want", i.ID)), tg.CB(fmt.Sprintf("%d · Пропущу", k+1), fmt.Sprintf("w:%d:skip", i.ID))))
	}
	if more > 0 {
		fmt.Fprintf(&b, "\n\nЕщё %d — в «Афише недели». Помощник поможет выбрать: «что в субботу до 3000?»", more)
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
		if i.Source != "scout" || i.WishA != domain.WishNone || i.WishB != domain.WishNone {
			continue
		}
		over := i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Add(6*time.Hour).Before(now)
		if over || now.Sub(i.CreatedAt) > scoutTTL {
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
