package domain

import (
	"sort"
	"strings"
	"time"
)

// Window is the common free time of both partners: contiguous runs of matched slots.
type Window struct {
	From, To time.Time
}

func (w Window) Minutes() int { return int(w.To.Sub(w.From) / time.Minute) }

// CommonWindows merges the slots both partners marked into contiguous windows, sorted by start.
func CommonWindows(a, b []Slot, loc *time.Location) []Window {
	inB := map[string]bool{}
	for _, s := range b {
		inB[s.Key()] = true
	}
	var common []Slot
	for _, s := range a {
		if inB[s.Key()] {
			common = append(common, s)
		}
	}
	sort.Slice(common, func(i, j int) bool { return common[i].Start(loc).Before(common[j].Start(loc)) })
	var out []Window
	for _, s := range common {
		st, en := s.Start(loc), s.End(loc)
		if n := len(out); n > 0 && !st.After(out[n-1].To) {
			if en.After(out[n-1].To) {
				out[n-1].To = en
			}
			continue
		}
		out = append(out, Window{st, en})
	}
	return out
}

// PickInput is everything the picker needs; Now and Loc make it deterministic in tests.
type PickInput struct {
	Ideas       []Idea
	Windows     []Window
	BudgetKop   int64          // 0 = no limit
	RecentShown map[int64]bool // ideas from the last two shown triples
	Exclude     map[int64]bool // ideas already shown in this round («Другая тройка»)
	Now         time.Time
}

type Scored struct {
	Idea    Idea
	Score   int
	Signals []string // machine-readable reasons, turned into the bot's sentence
	Window  Window   // the window it fits
}

// Eligible applies the hard filter. It returns the first window an idea fits and whether it fits at all.
func Eligible(i Idea, in PickInput) (Window, bool) {
	if i.Kind != KindLeisure || i.Status != IdeaActive {
		return Window{}, false
	}
	if i.WishA == WishSkip || i.WishB == WishSkip {
		return Window{}, false
	}
	if in.RecentShown[i.ID] || in.Exclude[i.ID] {
		return Window{}, false
	}
	if i.LastMetAt != nil && in.Now.Sub(*i.LastMetAt) < 14*24*time.Hour {
		return Window{}, false
	}
	if in.BudgetKop > 0 && i.PriceMin > in.BudgetKop {
		return Window{}, false
	}
	for _, w := range in.Windows {
		if i.DurationMin > 0 && i.DurationMin > w.Minutes() {
			continue
		}
		if i.EndsAt != nil && i.EndsAt.Before(w.From) {
			continue
		}
		if i.StartsAt != nil && i.StartsAt.After(w.To) {
			continue
		}
		return w, true
	}
	return Window{}, false
}

func wishPoints(w Wish) int {
	switch w {
	case WishVery:
		return 3
	case WishWant:
		return 2
	}
	return 0
}

// Score is integer so tests can be exact.
func Score(i Idea, now time.Time) (int, []string) {
	s, sig := 0, []string{}
	s += wishPoints(i.WishA) + wishPoints(i.WishB)
	switch {
	case i.WishA == WishVery && i.WishB == WishVery:
		sig = append(sig, "both_very")
	case i.WishA != WishNone && i.WishB != WishNone:
		sig = append(sig, "both_want")
	}
	if i.EndsAt != nil && i.EndsAt.Sub(now) <= 14*24*time.Hour && !i.EndsAt.Before(now) {
		s += 2
		sig = append(sig, "ends_soon")
	}
	if !i.EverShown && now.Sub(i.CreatedAt) > 30*24*time.Hour {
		s += 1
		sig = append(sig, "waiting_long")
	}
	s += i.AgainYes - 3*i.AgainNo
	if i.AgainYes > 0 {
		sig = append(sig, "liked_before")
	}
	return s, sig
}

// PickTriple returns up to three ideas: best, best of another category, best of the rest.
// Ties go to the older idea. The first one is the bot's favourite.
func PickTriple(in PickInput) []Scored {
	var cands []Scored
	for _, i := range in.Ideas {
		w, ok := Eligible(i, in)
		if !ok {
			continue
		}
		sc, sig := Score(i, in.Now)
		if i.PriceMin == 0 || in.BudgetKop == 0 || i.PriceMax <= in.BudgetKop {
			sig = append(sig, "fits_budget")
		}
		cands = append(cands, Scored{i, sc, sig, w})
	}
	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].Score != cands[b].Score {
			return cands[a].Score > cands[b].Score
		}
		if !cands[a].Idea.CreatedAt.Equal(cands[b].Idea.CreatedAt) {
			return cands[a].Idea.CreatedAt.Before(cands[b].Idea.CreatedAt)
		}
		return cands[a].Idea.ID < cands[b].Idea.ID
	})
	if len(cands) == 0 {
		return nil
	}
	out := []Scored{cands[0]}
	used := map[int64]bool{cands[0].Idea.ID: true}
	for _, c := range cands[1:] {
		if c.Idea.Category != cands[0].Idea.Category {
			out = append(out, c)
			used[c.Idea.ID] = true
			break
		}
	}
	for _, c := range cands {
		if len(out) == 3 {
			break
		}
		if !used[c.Idea.ID] {
			out = append(out, c)
			used[c.Idea.ID] = true
		}
	}
	return out
}

// Why turns signals into the bot's one-line reason (the AI may rephrase it; this is the fallback).
func Why(s Scored) string {
	has := func(k string) bool {
		for _, x := range s.Signals {
			if x == k {
				return true
			}
		}
		return false
	}
	if has("random") {
		return "Выпало случайно из вашей копилки — " + lowerWhy(whyBase(has))
	}
	return whyBase(has)
}

func lowerWhy(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}

func whyBase(has func(string) bool) string {
	switch {
	case has("both_very"):
		return "Вы оба очень хотите, и время подходит."
	case has("ends_soon"):
		return "Скоро закончится — потом не успеть."
	case has("liked_before"):
		return "Вам понравилось в прошлый раз."
	case has("both_want") && has("fits_budget"):
		return "Вы оба отметили «хочу», и укладывается в бюджет."
	case has("both_want"):
		return "Вы оба отметили «хочу»."
	case has("waiting_long"):
		return "Давно лежит в копилке — пора."
	}
	return "Подходит по времени."
}
