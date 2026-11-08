package app

import (
	"context"
	"strings"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/taste"
)

// Each person has their own taste model, trained on their own marks; nothing is shared between people
// except what they mark together («повторим?» after a meeting counts for both).

func itemOf(i store.Idea) taste.Item {
	return taste.Item{Title: i.Title, Category: i.Category, PriceKop: i.PriceMin, Starts: i.StartsAt, Source: i.Source, Address: i.Address}
}

func examplesFor(ideas []store.Idea, color string) []taste.Example {
	var ex []taste.Example
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure {
			continue
		}
		w := i.WishA
		if color == "b" {
			w = i.WishB
		}
		switch w {
		case domain.WishVery:
			ex = append(ex, taste.Example{Item: itemOf(i), Label: 1, Weight: 2})
		case domain.WishWant:
			ex = append(ex, taste.Example{Item: itemOf(i), Label: 1, Weight: 1})
		case domain.WishSkip:
			ex = append(ex, taste.Example{Item: itemOf(i), Label: 0, Weight: 1.5})
		}
		for k := 0; k < i.AgainYes; k++ {
			ex = append(ex, taste.Example{Item: itemOf(i), Label: 1, Weight: 1.5})
		}
		for k := 0; k < i.AgainNo; k++ {
			ex = append(ex, taste.Example{Item: itemOf(i), Label: 0, Weight: 1.5})
		}
	}
	return ex
}

// Tastes trains both people's models (microseconds; always the current marks).
type Tastes struct {
	Me, Partner taste.Model
	c           Couple
}

func (a *App) Tastes(ctx context.Context, c Couple) Tastes {
	ideas, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDone, domain.IdeaArchived, domain.IdeaDraft)
	t := Tastes{c: c, Me: taste.Train(examplesFor(ideas, c.Me.Color), c.Loc())}
	if c.Partner != nil {
		t.Partner = taste.Train(examplesFor(ideas, c.Partner.Color), c.Loc())
	}
	return t
}

// Match: how much each of the two would like it (0..1) and together (the weaker one counts more —
// a plan both are fine with beats one person's favourite).
type Match struct {
	Me, Partner, Both float64
}

func (t Tastes) Of(i store.Idea) Match {
	loc := t.c.Loc()
	m := Match{Me: t.Me.Score(itemOf(i), loc), Partner: 0.5}
	if t.c.Partner != nil {
		m.Partner = t.Partner.Score(itemOf(i), loc)
	}
	lo, hi := m.Me, m.Partner
	if lo > hi {
		lo, hi = hi, lo
	}
	m.Both = 0.65*lo + 0.35*hi
	return m
}

// TasteProfile: one person for the scout and the «Мы» screen: learned likes/dislikes + what they said.
type TasteProfile struct {
	Likes    []string `json:"likes"`
	Dislikes []string `json:"dislikes"`
	Said     []string `json:"said"`
	Marks    int      `json:"marks"`
}

func (a *App) Profiles(ctx context.Context, c Couple) (me, partner TasteProfile) {
	t := a.Tastes(ctx, c)
	mem, _ := a.S.Memories(ctx, c.Space.ID)
	fill := func(m taste.Model, memberID int64) TasteProfile {
		p := TasteProfile{Marks: m.N, Said: []string{}}
		p.Likes, p.Dislikes = m.Explain(5)
		if p.Likes == nil {
			p.Likes = []string{}
		}
		if p.Dislikes == nil {
			p.Dislikes = []string{}
		}
		for _, x := range mem {
			if x.MemberID == memberID {
				p.Said = append(p.Said, x.Text)
			}
		}
		return p
	}
	me = fill(t.Me, c.Me.ID)
	if c.Partner != nil {
		partner = fill(t.Partner, c.Partner.ID)
	}
	return
}

// MatchNote for cards: «Насте очень, тебе нормально».
func MatchNote(m Match, partner string) string {
	lvl := func(x float64) string {
		switch {
		case x >= 0.75:
			return "очень"
		case x >= 0.58:
			return "скорее да"
		case x <= 0.3:
			return "вряд ли"
		case x <= 0.42:
			return "сомнительно"
		}
		return ""
	}
	var parts []string
	if l := lvl(m.Partner); l != "" && partner != "" {
		parts = append(parts, partner+": "+l)
	}
	if l := lvl(m.Me); l != "" {
		parts = append(parts, "тебе: "+l)
	}
	return strings.Join(parts, " · ")
}
