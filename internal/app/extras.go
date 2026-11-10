package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// «А потом?» — a meeting can continue: museum, then dinner nearby. The main idea is chosen as usual;
// next stops are proposed by either partner and the other one confirms, like everything else.

const MaxExtras = 2

var (
	ErrExtraState = errors.New("extra_state") // round is not matched/planned yet, or already done
	ErrExtraLimit = errors.New("extra_limit")
	ErrExtraOwn   = errors.New("extra_own") // you can't confirm your own proposal
)

// Stop is one next stop with its computed time.
type Stop struct {
	Extra store.Extra
	Idea  store.Idea
	domain.Leg
}

func durationOf(i store.Idea) int {
	if i.DurationMin > 0 {
		return i.DurationMin
	}
	if i.Category == "food" {
		return 90
	}
	return domain.DefaultLeg
}

// Route lists the next stops (proposed and accepted) with their times, after the main idea.
func (a *App) Route(ctx context.Context, c Couple, r domain.Round) []Stop {
	if r.StartsAt.IsZero() {
		return nil
	}
	ex, _ := a.S.Extras(ctx, r.ID)
	if len(ex) == 0 {
		return nil
	}
	var winEnd time.Time
	for _, w := range domain.CommonWindows(r.SlotsA, r.SlotsB, c.Loc()) {
		if !w.From.After(r.StartsAt) && w.To.After(r.StartsAt) {
			winEnd = w.To
		}
	}
	var out []Stop
	var durs []int
	for _, e := range ex {
		i, err := a.S.Idea(ctx, c.Space.ID, e.IdeaID)
		if err != nil {
			continue
		}
		out = append(out, Stop{Extra: e, Idea: i})
		durs = append(durs, durationOf(i))
	}
	for k, l := range domain.Route(r.EndsAt, durs, winEnd, c.Loc()) {
		out[k].Leg = l
	}
	return out
}

// AcceptedRoute: only the stops both agreed on (for the calendar and reminders).
func (a *App) AcceptedRoute(ctx context.Context, c Couple, r domain.Round) []Stop {
	var out []Stop
	for _, s := range a.Route(ctx, c, r) {
		if s.Extra.State == "accepted" {
			out = append(out, s)
		}
	}
	return out
}

// SuggestExtras: up to three ideas to continue with — food first after anything that isn't food,
// what both want, what fits the budget; never the main idea or a stop already on the route.
func (a *App) SuggestExtras(ctx context.Context, c Couple, r domain.Round) []store.Idea {
	main, err := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	if err != nil {
		return nil
	}
	skip := map[int64]bool{main.ID: true}
	ex, _ := a.S.Extras(ctx, r.ID)
	for _, e := range ex {
		skip[e.IdeaID] = true
	}
	// what fits after it: after a show, a play, a walk — somewhere to eat or a short stroll, not a second show;
	// after a dinner — a walk, a dessert, a bar. Events at their own fixed time fit only if they start right after.
	ideas, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	planned := a.S.PlannedIdeas(ctx, c.Space.ID)
	end := r.StartsAt.Add(time.Duration(durationOf(main)) * time.Minute)
	type sc struct {
		i store.Idea
		s int
	}
	var cand []sc
	w := map[domain.Wish]int{domain.WishVery: 3, domain.WishWant: 2, domain.WishNone: 0, domain.WishSkip: -4}
	for _, i := range ideas {
		if skip[i.ID] || planned[i.ID] || i.Kind != domain.KindLeisure {
			continue
		}
		if r.BudgetKop > 0 && i.PriceMin > r.BudgetKop {
			continue
		}
		if i.EndsAt != nil && i.EndsAt.Before(r.StartsAt) {
			continue
		}
		if i.StartsAt != nil && (i.EndsAt == nil || i.EndsAt.Sub(*i.StartsAt) < 30*time.Hour) {
			// a dated event: only if it starts within the hour after ours ends
			if d := i.StartsAt.Sub(end); d < 0 || d > 90*time.Minute {
				continue
			}
		}
		after := main.Category != "food" && i.Category == "food" ||
			main.Category == "food" && (i.Category == "walk" || i.Category == "other" && durationOf(i) <= 90)
		if !after && !(i.Category == "walk" && durationOf(i) <= 90) {
			continue // a second big thing in one evening is not «а потом»
		}
		s := w[i.WishA] + w[i.WishB]
		if after {
			s += 4
		}
		if i.Status == domain.IdeaDraft {
			s-- // ours first, the afisha's next
		}
		cand = append(cand, sc{i, s})
	}
	sort.SliceStable(cand, func(x, y int) bool { return cand[x].s > cand[y].s })
	out := []store.Idea{}
	for _, x := range cand {
		if len(out) == 3 {
			break
		}
		out = append(out, x.i)
	}
	return out
}

func extraOpen(r domain.Round) bool {
	return r.State == domain.StateConfirm || r.State == domain.StatePlanned
}

// ProposeExtra adds a next stop: an idea from the bank, or a new one from text («поесть рядом»).
func (a *App) ProposeExtra(ctx context.Context, c Couple, roundID, ideaID int64, text string) (store.Extra, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return store.Extra{}, err
	}
	if !extraOpen(r) {
		return store.Extra{}, ErrExtraState
	}
	if c.Partner == nil {
		return store.Extra{}, ErrNotPaired
	}
	if ex, _ := a.S.Extras(ctx, r.ID); len(ex) >= MaxExtras {
		return store.Extra{}, ErrExtraLimit
	}
	if ideaID == 0 {
		text = strings.TrimSpace(text)
		if text == "" {
			return store.Extra{}, errors.New("empty")
		}
		res, err := a.AddIdea(ctx, c, text)
		if err != nil {
			return store.Extra{}, err
		}
		ideaID = res.Idea.ID
		if res.Existing != nil {
			ideaID = res.Existing.ID
		}
		if res.Existing == nil && a.AI.Enabled() { // «поужинать рядом с …»: concrete places to choose from, without asking
			id := ideaID
			go func() {
				bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
				defer cancel()
				if _, err := a.SuggestPlaces(bg, c, id); err == nil {
					a.Live.From(bg, c.Space.ID)
				}
			}()
		}
	}
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return store.Extra{}, err
	}
	if i.ID == r.Chosen {
		return store.Extra{}, ErrExtraState
	}
	e, err := a.S.AddExtra(ctx, store.Extra{RoundID: r.ID, IdeaID: i.ID, ProposedBy: c.Me.ID}, a.now())
	if err != nil {
		return e, err
	}
	a.Live.From(ctx, c.Space.ID)
	when := ""
	for _, s := range a.Route(ctx, c, r) {
		if s.Extra.ID == e.ID {
			when = fmt.Sprintf(" в %02d:%02d", s.Start.In(c.Loc()).Hour(), s.Start.In(c.Loc()).Minute())
			if s.Tight {
				when += " (впритык — можете не успеть)"
			}
		}
	}
	main, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s предлагает после «%s»: <b>%s</b>%s. Добавим?", esc(c.Me.Name), esc(main.Title), esc(i.Title), esc(when)),
		tg.Keyboard(tg.Row(tg.CB("Да, добавим", fmt.Sprintf("ex:%d:y", e.ID)), tg.CB("Не в этот раз", fmt.Sprintf("ex:%d:n", e.ID))),
			tg.Row(tg.App("Открыть встречу", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
	return e, nil
}

// AnswerExtra: the partner who did not propose it says yes or no.
func (a *App) AnswerExtra(ctx context.Context, c Couple, extraID int64, yes bool) (domain.Round, error) {
	e, err := a.S.Extra(ctx, extraID)
	if err != nil {
		return domain.Round{}, err
	}
	r, err := a.S.Round(ctx, c.Space.ID, e.RoundID)
	if err != nil {
		return r, err
	}
	if e.ProposedBy == c.Me.ID {
		return r, ErrExtraOwn
	}
	state := "declined"
	if yes {
		state = "accepted"
	}
	ok, err := a.S.SetExtraState(ctx, e.ID, state)
	if err != nil || !ok {
		return r, err // already answered: nothing to do
	}
	a.Live.From(ctx, c.Space.ID)
	i, _ := a.S.Idea(ctx, c.Space.ID, e.IdeaID)
	if c.Partner != nil {
		if yes {
			a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s за: <b>%s</b> — в маршруте.", esc(c.Me.Name), esc(i.Title)), nil)
		} else {
			a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s не хочет добавлять «%s» в этот раз.", esc(c.Me.Name), esc(i.Title)), nil)
		}
	}
	if yes && r.State == domain.StatePlanned {
		a.resendICS(ctx, c, r)
	}
	return r, nil
}

// RemoveExtra takes a stop off the route (either partner); the calendar follows.
func (a *App) RemoveExtra(ctx context.Context, c Couple, extraID int64) (domain.Round, error) {
	e, err := a.S.Extra(ctx, extraID)
	if err != nil {
		return domain.Round{}, err
	}
	r, err := a.S.Round(ctx, c.Space.ID, e.RoundID)
	if err != nil {
		return r, err
	}
	if err := a.S.DeleteExtra(ctx, e.ID); err != nil {
		return r, err
	}
	a.Live.From(ctx, c.Space.ID)
	if e.State == "accepted" && r.State == domain.StatePlanned {
		a.resendICS(ctx, c, r)
	}
	return r, nil
}

func (a *App) resendICS(ctx context.Context, c Couple, r domain.Round) {
	text := "Маршрут обновился — обнови событие в календаре."
	if l := a.routeLine(ctx, c, r); l != "" {
		text = "Маршрут обновился. " + l + "\nОбнови событие в календаре:"
	}
	for _, m := range a.both(c) {
		if cal := a.calKeys(ctx, c, m, r); len(cal) > 0 {
			a.sendQuiet(ctx, m.ChatID, text, tg.Keyboard(cal))
		}
	}
}

// routeLine for bot messages: «Потом: 17:00 Ужин · 19:00 Кино».
func (a *App) routeLine(ctx context.Context, c Couple, r domain.Round) string {
	var parts []string
	for _, s := range a.AcceptedRoute(ctx, c, r) {
		parts = append(parts, fmt.Sprintf("%02d:%02d %s", s.Start.In(c.Loc()).Hour(), s.Start.In(c.Loc()).Minute(), esc(s.Idea.Title)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "<b>Потом:</b> " + strings.Join(parts, " · ")
}
