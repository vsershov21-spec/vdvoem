package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

var ErrNoIdeas = errors.New("no_ideas")

// Suggestion is one idea the app proposes on its own (or on «Удиви нас»).
type Suggestion struct {
	Idea store.Idea
	Why  string
	Mode domain.SuggestMode
}

func (a *App) rng() *rand.Rand { return rand.New(rand.NewSource(a.now().UnixNano())) }

// Suggest picks one idea for the coming week. exclude: ideas already offered in this chain («Другую»).
func (a *App) Suggest(ctx context.Context, c Couple, mode domain.SuggestMode, exclude []int64) (Suggestion, error) {
	if !mode.Valid() || mode == domain.SuggestOff {
		mode = domain.SuggestBest
	}
	ideas, err := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive)
	if err != nil {
		return Suggestion{}, err
	}
	ex := map[int64]bool{}
	for _, id := range exclude {
		ex[id] = true
	}
	in := domain.PickInput{BudgetKop: c.Space.BudgetKop, Exclude: ex, Now: a.now()}
	for _, i := range ideas {
		in.Ideas = append(in.Ideas, i.Idea)
	}
	s, ok := domain.PickOne(in, mode, a.rng())
	if !ok {
		return Suggestion{}, ErrNoIdeas
	}
	full, _ := a.S.Idea(ctx, c.Space.ID, s.Idea.ID)
	why := domain.Why(s)
	if a.AI.Enabled() && mode.Resolve(a.now()) != domain.SuggestRandom {
		if t, err := a.AI.Why(ctx, s.Idea.Title, append([]string{why}, s.Signals...)); err == nil {
			why = t
		}
	}
	return Suggestion{Idea: full, Why: why, Mode: mode.Resolve(a.now())}, nil
}

func excludeKey(ids []int64) string {
	if len(ids) > 4 {
		ids = ids[len(ids)-4:]
	}
	var p []string
	for _, id := range ids {
		p = append(p, strconv.FormatInt(id, 10))
	}
	return strings.Join(p, ".")
}

// ParseExclude reads the «sn:<mode>:<ids>» tail.
func ParseExclude(s string) []int64 {
	var out []int64
	for _, p := range strings.Split(s, ".") {
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// SendSuggestion: «Как насчёт…?» with Давай / Другую / 🎲.
func (a *App) SendSuggestion(ctx context.Context, chatID int64, s Suggestion, chain []int64, lead string) {
	chain = append(chain, s.Idea.ID)
	ex := excludeKey(chain)
	if lead == "" {
		lead = "Как насчёт этого на выходных?"
		if s.Mode == domain.SuggestRandom {
			lead = "🎲 Случайная идея из вашей копилки:"
		}
	}
	text := fmt.Sprintf("%s\n\n<b>%s</b>\n%s\n\n<i>%s</i>", lead, esc(s.Idea.Title), esc(IdeaMeta(s.Idea)), esc(s.Why))
	a.send(ctx, chatID, text, tg.Keyboard(
		tg.Row(tg.CB("Давай!", fmt.Sprintf("sg:%d", s.Idea.ID)), tg.CB("Другую", "sn:best:"+ex), tg.CB("🎲 Рандом", "sn:random:"+ex)),
		tg.Row(tg.App("Подробнее", a.appURL(fmt.Sprintf("idea/%d", s.Idea.ID)))),
	))
}

// GoWith opens a round where the idea is already chosen: only the time is left to agree on.
func (a *App) GoWith(ctx context.Context, c Couple, ideaID int64) (domain.Round, error) {
	if c.Partner == nil {
		return domain.Round{}, ErrNotPaired
	}
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return domain.Round{}, err
	}
	if r, err := a.S.OpenRound(ctx, c.Space.ID); err == nil {
		return r, ErrRoundOpen
	}
	nr := domain.NewRound(c.Space.ID, a.now(), c.Space.BudgetKop)
	nr.Chosen = ideaID
	r, err := a.S.CreateRound(ctx, nr, a.now())
	if err != nil {
		return r, err
	}
	for _, m := range a.both(c) {
		lead := fmt.Sprintf("Решено: <b>%s</b>.", esc(i.Title))
		if m.ID != c.Me.ID {
			lead = fmt.Sprintf("%s хочет: <b>%s</b>.", esc(c.Me.Name), esc(i.Title))
		}
		a.send(ctx, m.ChatID, lead+" Осталось найти время — отметь, когда тебе удобно.",
			tg.Keyboard(tg.Row(tg.App("Отметить время", a.appURL(fmt.Sprintf("round/%d", r.ID)))), tg.Row(tg.CB("Не сейчас", fmt.Sprintf("x:%d", r.ID)))))
	}
	return r, nil
}

var ErrRoundOpen = errors.New("round_open")

// SwapIdea puts a specific idea into the round that is already open («давай лучше это»):
// marked time stays, the vote is skipped, and if both already marked time it goes straight to confirmation.
func (a *App) SwapIdea(ctx context.Context, c Couple, ideaID int64) (domain.Round, error) {
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return domain.Round{}, err
	}
	r, err := a.S.OpenRound(ctx, c.Space.ID)
	if err != nil {
		return a.GoWith(ctx, c, ideaID)
	}
	ver := r.Version
	r.State, r.Chosen, r.Triple = domain.StateTime, ideaID, nil
	r.VoteA, r.VoteB, r.ConfirmA, r.ConfirmB = domain.VoteNone, domain.VoteNone, false, false
	r.StartsAt, r.EndsAt = time.Time{}, time.Time{}
	r.Version++
	if r.SentA && r.SentB {
		if err := a.startVoting(ctx, c, &r, nil); err != nil {
			return r, err
		}
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	for _, m := range a.both(c) {
		lead := fmt.Sprintf("Меняем выбор: <b>%s</b>.", esc(i.Title))
		if m.ID != c.Me.ID {
			lead = fmt.Sprintf("%s предлагает вместо этого: <b>%s</b>.", esc(c.Me.Name), esc(i.Title))
		}
		if r.State == domain.StateTime {
			a.send(ctx, m.ChatID, lead+" Отмеченное время сохранилось.", tg.Keyboard(tg.Row(tg.App("Открыть", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
		}
	}
	if r.State == domain.StateConfirm {
		a.announceConfirm(ctx, c, r)
	}
	return r, nil
}

// nudge: the weekly proactive suggestion to both, unless something is already being planned.
func (a *App) nudge(ctx context.Context, sp store.Space) error {
	mode := domain.SuggestMode(sp.SuggestMode)
	if mode == domain.SuggestOff {
		return nil
	}
	if _, err := a.S.OpenRound(ctx, sp.ID); err == nil {
		return nil
	}
	if r, err := a.S.NextPlanned(ctx, sp.ID); err == nil && r.StartsAt.After(a.now()) && r.StartsAt.Sub(a.now()) < 6*24*time.Hour {
		return nil
	}
	ms, err := a.S.Members(ctx, sp.ID)
	if err != nil || len(ms) < 2 {
		return err
	}
	c, err := a.coupleOf(ctx, ms[0])
	if err != nil {
		return err
	}
	s, err := a.Suggest(ctx, c, mode, nil)
	if errors.Is(err, ErrNoIdeas) {
		return nil // nothing to offer: silence is better than «добавьте идеи» every week
	}
	if err != nil {
		return err
	}
	for _, m := range ms {
		a.SendSuggestion(ctx, m.ChatID, s, nil, "")
	}
	return nil
}
