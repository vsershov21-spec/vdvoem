package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// «Важно»: «хочу на этот спектакль, билеты могут уйти — ответь». The partner gets an alert with three
// answers and is reminded until they give one: 1 h, 2 h, 3 h… (never at night), at most 8 times, and never
// past the event itself. Yes plans the meeting right away (straight to the calendar when the event has its
// own time); no tells the author at once.

var (
	ErrNotYours   = errors.New("not_yours")
	ErrNotPending = errors.New("not_pending")
)

const urgentMax = 8

func urgentGap(n int) time.Duration { return time.Duration(n+1) * time.Hour }

// MarkUrgent: I ask the partner to answer about this idea quickly
func (a *App) MarkUrgent(ctx context.Context, c Couple, ideaID int64, note string) (store.Urgent, error) {
	if c.Partner == nil {
		return store.Urgent{}, ErrNotPaired
	}
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return store.Urgent{}, err
	}
	if r := []rune(strings.TrimSpace(note)); len(r) > 200 {
		note = string(r[:200])
	}
	now := a.now()
	u := store.Urgent{IdeaID: i.ID, SpaceID: c.Space.ID, By: c.Me.ID, Note: strings.TrimSpace(note), CreatedAt: now, NextAt: now.Add(urgentGap(0))}
	if err := a.S.SetUrgent(ctx, u); err != nil {
		return u, err
	}
	if i.Status == domain.IdeaDraft { // asked about — it is in the bank now
		i.Status = domain.IdeaActive
		_ = a.S.UpdateIdea(ctx, i, now)
	}
	// my own mark: asking means I want it
	if mine := wishOf(i, c.Me.Color); mine == domain.WishNone || mine == domain.WishSkip {
		_ = a.S.SetWish(ctx, c.Space.ID, i.ID, c.Me.Color, domain.WishVery, now)
	}
	a.send(ctx, c.Partner.ChatID, a.urgentText(i, u, c.Me.Name, 0), a.urgentKeys(i))
	a.Live.From(ctx, c.Space.ID)
	return u, nil
}

func wishOf(i store.Idea, color string) domain.Wish {
	if color == "b" {
		return i.WishB
	}
	return i.WishA
}

func (a *App) urgentText(i store.Idea, u store.Urgent, by string, nudge int) string {
	var b strings.Builder
	if nudge == 0 {
		fmt.Fprintf(&b, "🔥 <b>%s просит ответить</b>: <b>%s</b>", esc(by), esc(i.Title))
	} else {
		fmt.Fprintf(&b, "🔥 Напоминаю: %s ждёт твоего ответа про <b>%s</b>", esc(by), esc(i.Title))
	}
	if i.StartsAt != nil {
		b.WriteString(" — " + esc(domain.When(*i.StartsAt, a.locOf(u.SpaceID))))
	}
	if i.PriceMin > 0 {
		b.WriteString(", ≈ " + esc(domain.Money(i.PriceMin)) + " на двоих")
	}
	b.WriteString(".")
	if u.Note != "" {
		b.WriteString("\n«" + esc(u.Note) + "»")
	}
	if nudge == 0 {
		b.WriteString("\nДата может быстро уйти — ответь, пожалуйста. Буду напоминать, пока не ответишь.")
	}
	return b.String()
}

func (a *App) urgentKeys(i store.Idea) *tg.Markup {
	yes := "Идём — выбрать время"
	if a.fixed(i, a.now()) {
		yes = "Идём! В календарь"
	}
	return tg.Keyboard(
		tg.Row(tg.CB(yes, fmt.Sprintf("ur:%d:yes", i.ID))),
		tg.Row(tg.CB("Не смогу", fmt.Sprintf("ur:%d:no", i.ID)), tg.CB("Отвечу через час", fmt.Sprintf("ur:%d:later", i.ID))),
		tg.Row(tg.App("Открыть", a.appURL(fmt.Sprintf("idea/%d", i.ID)))))
}

// fixed: the event has its own date and time (a booking can go straight to the calendar) — as BookFixed sees it
func (a *App) fixed(i store.Idea, now time.Time) bool {
	if i.StartsAt == nil || i.StartsAt.Before(now) || i.EndsAt != nil && i.EndsAt.Sub(*i.StartsAt) > 30*time.Hour {
		return false
	}
	s := i.StartsAt.In(a.locOf(i.SpaceID))
	return !(s.Hour() == 0 && s.Minute() == 0)
}

func (a *App) locOf(spaceID int64) *time.Location {
	if sp, err := a.S.Space(context.Background(), spaceID); err == nil {
		return sp.Loc()
	}
	return time.UTC
}

// AnswerUrgent: the partner answers — yes | no | later. Only the one who was asked answers.
func (a *App) AnswerUrgent(ctx context.Context, c Couple, ideaID int64, answer string) (string, error) {
	u, err := a.S.Urgent(ctx, c.Space.ID, ideaID)
	if err != nil || u.DoneAt != nil {
		return "", ErrNotPending
	}
	if u.By == c.Me.ID {
		return "", ErrNotYours
	}
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return "", err
	}
	now := a.now()
	author := c.Partner
	switch answer {
	case "later":
		_ = a.S.NudgeUrgent(ctx, ideaID, now.Add(time.Hour), false)
		return "Напомню через час.", nil
	case "no":
		if ok, _ := a.S.CloseUrgent(ctx, c.Space.ID, ideaID, "no", now); !ok {
			return "", ErrNotPending
		}
		if author != nil {
			a.send(ctx, author.ChatID, fmt.Sprintf("%s не сможет на <b>%s</b>.", esc(c.Me.Name), esc(i.Title)), nil)
		}
		a.Live.From(ctx, c.Space.ID)
		return "Передал, что не сможешь.", nil
	case "yes":
		if _, err := a.SetWish(ctx, c, ideaID, domain.WishVery); err != nil {
			return "", err
		}
		var rerr error
		if a.fixed(i, now) {
			_, rerr = a.BookFixed(ctx, c, ideaID)
		} else {
			rerr = ErrNoTime
		}
		if errors.Is(rerr, ErrNoTime) {
			_, rerr = a.GoWith(ctx, c, ideaID)
		}
		if errors.Is(rerr, ErrRoundOpen) { // a choice is already going on: this idea takes its place
			_, rerr = a.SwapIdea(ctx, c, ideaID)
		}
		if rerr != nil {
			return "", rerr
		}
		_, _ = a.S.CloseUrgent(ctx, c.Space.ID, ideaID, "yes", now)
		a.Live.From(ctx, c.Space.ID)
		if a.fixed(i, now) {
			return "Отлично! Как только " + c.PartnerName() + " подтвердит — встреча в календаре у обоих.", nil
		}
		return "Отлично! Теперь отметьте время — я найду, когда вы оба можете.", nil
	}
	return "", ErrForbidden
}

// CancelUrgent: the author takes the request back
func (a *App) CancelUrgent(ctx context.Context, c Couple, ideaID int64) error {
	u, err := a.S.Urgent(ctx, c.Space.ID, ideaID)
	if err != nil || u.DoneAt != nil {
		return ErrNotPending
	}
	if u.By != c.Me.ID {
		return ErrNotYours
	}
	_, err = a.S.CloseUrgent(ctx, c.Space.ID, ideaID, "cancelled", a.now())
	a.Live.From(ctx, c.Space.ID)
	return err
}

// urgentTick: reminders that are due; expired requests close quietly
func (a *App) urgentTick(ctx context.Context, now time.Time) {
	due, err := a.S.DueUrgent(ctx, now)
	if err != nil {
		return
	}
	for _, u := range due {
		i, err := a.S.Idea(ctx, u.SpaceID, u.IdeaID)
		if err != nil || i.Status == domain.IdeaDone || i.Status == domain.IdeaArchived || i.StartsAt != nil && i.StartsAt.Before(now) || u.Nudges >= urgentMax {
			_, _ = a.S.CloseUrgent(ctx, u.SpaceID, u.IdeaID, "expired", now)
			continue
		}
		sp, err := a.S.Space(ctx, u.SpaceID)
		if err != nil {
			continue
		}
		if q := domain.QuietShift(now, sp.Loc()); !q.Equal(now) { // night: the morning
			_ = a.S.NudgeUrgent(ctx, u.IdeaID, q, false)
			continue
		}
		// already decided some other way: the idea is in a choice or planned
		if r, err := a.S.OpenRound(ctx, u.SpaceID); err == nil && r.Chosen == u.IdeaID {
			_, _ = a.S.CloseUrgent(ctx, u.SpaceID, u.IdeaID, "yes", now)
			continue
		}
		ms, _ := a.S.Members(ctx, u.SpaceID)
		var by, to *store.Member
		for k := range ms {
			if ms[k].ID == u.By {
				by = &ms[k]
			} else {
				to = &ms[k]
			}
		}
		if by == nil || to == nil {
			_, _ = a.S.CloseUrgent(ctx, u.SpaceID, u.IdeaID, "expired", now)
			continue
		}
		if wishOf(i, to.Color) == domain.WishSkip { // marked «пропущу» meanwhile — that is a no
			_, _ = a.S.CloseUrgent(ctx, u.SpaceID, u.IdeaID, "no", now)
			a.send(ctx, by.ChatID, fmt.Sprintf("%s %s «пропущу» на <b>%s</b>.", esc(to.Name), Past(to.Name, "отметил"), esc(i.Title)), nil)
			continue
		}
		a.send(ctx, to.ChatID, a.urgentText(i, u, by.Name, u.Nudges+1), a.urgentKeys(i))
		_ = a.S.NudgeUrgent(ctx, u.IdeaID, now.Add(urgentGap(u.Nudges+1)), true)
	}
}

// UrgentView: what the app shows about a request on an idea (nil — none open)
func (a *App) UrgentView(ctx context.Context, c Couple, ideaID int64) map[string]any {
	u, err := a.S.Urgent(ctx, c.Space.ID, ideaID)
	if err != nil || u.DoneAt != nil {
		return nil
	}
	return map[string]any{"by_me": u.By == c.Me.ID, "note": u.Note, "nudges": u.Nudges, "since": u.CreatedAt.Unix()}
}

// UrgentForMe: requests waiting for my answer (Home shows them on top)
func (a *App) UrgentForMe(ctx context.Context, c Couple) []map[string]any {
	us, _ := a.S.OpenUrgent(ctx, c.Space.ID)
	out := []map[string]any{}
	for _, u := range us {
		if u.By == c.Me.ID {
			continue
		}
		i, err := a.S.Idea(ctx, c.Space.ID, u.IdeaID)
		if err != nil {
			continue
		}
		m := map[string]any{"idea_id": i.ID, "title": i.Title, "note": u.Note, "fixed": a.fixed(i, a.now())}
		if i.StartsAt != nil {
			m["when"] = domain.When(*i.StartsAt, c.Loc())
		}
		out = append(out, m)
	}
	return out
}

var (
	urgentLead = regexp.MustCompile(`(?i)^\s*(?:срочно|очень важно|важно|скорее)[\s,:!—-]+`)
	urgentTail = regexp.MustCompile(`(?i)[,.;!—-]*\s*(?:а то |пока |ведь )?(?:билеты|места|столики)[^,.;!]*(?:кончаются|заканчиваются|разбирают|раскупают|уходят|мало)[^,.;!]*[.!]*\s*$`)
	wantLead   = regexp.MustCompile(`(?i)^\s*(?:хочу|хотим|давай|надо|нужно)\s+(?:сходить\s+|пойти\s+|попасть\s+)?(?:на|в|во)?\s*`)
)

// UrgentText: «Срочно хочу на Мастера и Маргариту, билеты быстро кончаются» — an urgent wish about something,
// not a new idea named so. Returns what it is about.
func UrgentText(text string) (string, bool) {
	t := strings.TrimSpace(text)
	lead, tail := urgentLead.MatchString(t), urgentTail.MatchString(t)
	if !lead && !tail {
		return "", false
	}
	t = urgentLead.ReplaceAllString(t, "")
	t = urgentTail.ReplaceAllString(t, "")
	t = strings.TrimSpace(wantLead.ReplaceAllString(t, ""))
	if len([]rune(t)) < 3 {
		return "", false
	}
	return upperFirstRune(t), true
}

func upperFirstRune(s string) string {
	r := []rune(s)
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}
