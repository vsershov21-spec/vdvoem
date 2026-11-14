package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

// The meeting card of the new interface. Nothing is decided for the couple: a time, a move, a place «а потом»
// are proposals — one offers, the other answers, and only a «да» changes the meeting.

var ErrMeetState = errors.New("meet_state")

// ProposeTime: «Вова зовёт: Планетарий, вс 15 ноя, 13:00». For a planned meeting this is a move: the old time
// stands until the partner says yes.
func (a *App) ProposeTime(ctx context.Context, c Couple, roundID int64, start time.Time) (domain.Proposal, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return domain.Proposal{}, err
	}
	if (!r.State.Open() && r.State != domain.StatePlanned) || r.Chosen == 0 {
		return domain.Proposal{}, ErrMeetState
	}
	if !start.After(a.now()) {
		return domain.Proposal{}, ErrBadTime
	}
	i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	if err != nil {
		return domain.Proposal{}, err
	}
	pl, _ := json.Marshal(map[string]int64{"start": start.Unix()})
	l := start.In(c.Loc())
	return a.Propose(ctx, c, domain.ProposalTime, r.ID, i.Title, ruDate(l)+l.Format(", 15:04"), string(pl))
}

// timeAnswered: «да» puts the meeting into the plans at the proposed time
func (a *App) timeAnswered(ctx context.Context, c Couple, p domain.Proposal, yes bool) error {
	if !yes {
		return nil
	}
	var pl struct {
		Start int64 `json:"start"`
	}
	if json.Unmarshal([]byte(p.Payload), &pl) != nil || pl.Start == 0 {
		return nil // a free-form offer without a meeting behind it
	}
	r, err := a.S.Round(ctx, c.Space.ID, p.RefID)
	if err != nil || r.Chosen == 0 || (!r.State.Open() && r.State != domain.StatePlanned) {
		return nil
	}
	i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	if err != nil {
		return err
	}
	ver, moved := r.Version, r.State == domain.StatePlanned
	r.SetTime(time.Unix(pl.Start, 0), durationOf(i))
	r.State, r.ConfirmA, r.ConfirmB = domain.StatePlanned, true, true
	if err = a.save(ctx, r, ver); err != nil {
		return err
	}
	if i.Status == domain.IdeaDraft {
		i.Status = domain.IdeaActive
		_ = a.S.UpdateIdea(ctx, i, a.now())
	}
	if moved {
		a.resendICS(ctx, c, r)
	} else {
		a.planned(ctx, c, r, 0)
	}
	return nil
}

// ProposeStop: a place after the meeting. The place itself is an idea of the couple, a find of the scout,
// or something typed in (then it becomes a stop of this route only).
func (a *App) ProposeStop(ctx context.Context, c Couple, roundID, ideaID int64, text string) (domain.Proposal, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return domain.Proposal{}, err
	}
	if !extraOpen(r) {
		return domain.Proposal{}, ErrMeetState
	}
	if ideaID == 0 {
		if clip(text, 80) == "" {
			return domain.Proposal{}, ErrBadInput
		}
		res, err := a.AddIdea(ctx, c, text)
		if err != nil {
			return domain.Proposal{}, err
		}
		ideaID = res.Idea.ID
		if res.Existing != nil {
			ideaID = res.Existing.ID
		} else {
			_ = a.S.SetIdeaSource(ctx, c.Space.ID, ideaID, "route")
		}
	}
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return domain.Proposal{}, err
	}
	if i.ID == r.Chosen {
		return domain.Proposal{}, ErrBadInput
	}
	main, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	pl, _ := json.Marshal(map[string]int64{"idea": i.ID})
	return a.Propose(ctx, c, domain.ProposalStop, r.ID, i.Title, "после «"+main.Title+"»", string(pl))
}

// stopAnswered: «да» adds the place to the route
func (a *App) stopAnswered(ctx context.Context, c Couple, p domain.Proposal, yes bool) error {
	if !yes {
		return nil
	}
	var pl struct {
		Idea int64 `json:"idea"`
	}
	if json.Unmarshal([]byte(p.Payload), &pl) != nil || pl.Idea == 0 {
		return nil
	}
	r, err := a.S.Round(ctx, c.Space.ID, p.RefID)
	if err != nil || !extraOpen(r) {
		return nil
	}
	e, err := a.S.AddExtra(ctx, store.Extra{RoundID: r.ID, IdeaID: pl.Idea, ProposedBy: p.AuthorID}, a.now())
	if err != nil {
		return err
	}
	if _, err = a.S.SetExtraState(ctx, e.ID, "accepted"); err != nil {
		return err
	}
	if r.State == domain.StatePlanned {
		a.resendICS(ctx, c, r)
	}
	return nil
}

// CancelMeet: the meeting is cancelled and the partner gets a row on the home screen (and the bot's message)
func (a *App) CancelMeet(ctx context.Context, c Couple, roundID int64) error {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return err
	}
	title, when := "встреча", ""
	if i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen); err == nil {
		title = i.Title
	}
	if r.State == domain.StatePlanned {
		l := r.StartsAt.In(c.Loc())
		when = ruDate(l) + l.Format(", 15:04")
	}
	if _, err = a.Cancel(ctx, c, roundID); err != nil {
		return err
	}
	now := a.now()
	// what was still waiting about this meeting has nothing to wait for
	if ps, err := a.S.LiveProposals(ctx, c.Space.ID); err == nil {
		for _, p := range ps {
			if p.RefID == r.ID && p.State == domain.ProposalWait && (p.Kind == domain.ProposalTime || p.Kind == domain.ProposalStop) {
				_ = a.S.CloseProposal(ctx, c.Space.ID, p.ID, domain.ProposalWithdrawn, now)
			}
		}
	}
	if c.Partner != nil { // the bot has already told them (Cancel does): only the row is added
		_, _ = a.S.CreateProposal(ctx, domain.Proposal{SpaceID: c.Space.ID, Kind: domain.ProposalCancel, RefID: r.ID, AuthorID: c.Me.ID, Title: title, Sub: when}, now)
	}
	a.Live.From(ctx, c.Space.ID)
	return nil
}

// MeetProposals: what is being decided about a meeting right now, as the member sees it
type MeetProposal struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Sub   string `json:"sub"`
	Mine  bool   `json:"mine"`
	State string `json:"state"` // wait | no
	Start int64  `json:"start,omitempty"`
	Idea  int64  `json:"idea,omitempty"`
}

func (a *App) MeetProposals(ctx context.Context, c Couple, roundID int64) map[string]*MeetProposal {
	out := map[string]*MeetProposal{}
	ps, err := a.S.LiveProposals(ctx, c.Space.ID)
	if err != nil {
		return out
	}
	for k := len(ps) - 1; k >= 0; k-- { // oldest first, so the newest of a kind wins
		p := ps[k]
		if p.RefID != roundID || (p.Kind != domain.ProposalTime && p.Kind != domain.ProposalStop) {
			continue
		}
		if p.State == domain.ProposalNo && p.AuthorID != c.Me.ID {
			continue // my own «нет» is not something I need to see again
		}
		var pl struct{ Start, Idea int64 }
		_ = json.Unmarshal([]byte(p.Payload), &pl)
		out[p.Kind] = &MeetProposal{ID: p.ID, Title: p.Title, Sub: p.Sub, Mine: p.AuthorID == c.Me.ID, State: string(p.State), Start: pl.Start, Idea: pl.Idea}
	}
	return out
}

// ---------- Планы: busy days by hand, free windows of the two ----------

func (a *App) today(c Couple) string { return a.now().In(c.Loc()).Format("2006-01-02") }

func validDay(s string) bool { _, err := time.Parse("2006-01-02", s); return err == nil }

func (a *App) SetBusyDay(ctx context.Context, c Couple, day string, on bool) error {
	if !validDay(day) {
		return ErrBadInput
	}
	err := a.S.SetBusyDay(ctx, c.Me.ID, day, on)
	a.Live.From(ctx, c.Space.ID)
	return err
}

func (a *App) SetFreeSlot(ctx context.Context, c Couple, day string, part int, on bool) error {
	if !validDay(day) || part < 0 || part > 2 {
		return ErrBadInput
	}
	err := a.S.SetFreeSlot(ctx, c.Me.ID, day, part, on)
	a.Live.From(ctx, c.Space.ID)
	return err
}

// Free: the grid of «Общее время» and of the time sheet — the coming days, what each marked as free, who is
// busy on which day, and the windows both are free in
func (a *App) Free(ctx context.Context, c Couple) map[string]any {
	from := a.today(c)
	t0, _ := time.ParseInLocation("2006-01-02", from, c.Loc())
	days := []map[string]string{}
	for k := 0; k < 7; k++ {
		d := t0.AddDate(0, 0, k)
		days = append(days, map[string]string{"day": d.Format("2006-01-02"), "label": ruDate(d)})
	}
	last := t0.AddDate(0, 0, 6).Format("2006-01-02")
	in := func(xs []string, cut int) []string {
		out := []string{}
		for _, x := range xs {
			if x[:cut] <= last {
				out = append(out, x)
			}
		}
		return out
	}
	mine, my := in(a.S.FreeSlots(ctx, c.Me.ID, from), 10), a.S.BusyDays(ctx, c.Me.ID, from)
	theirs, their := []string{}, []string{}
	if c.Partner != nil {
		theirs, their = in(a.S.FreeSlots(ctx, c.Partner.ID, from), 10), a.S.BusyDays(ctx, c.Partner.ID, from)
	}
	set := map[string]bool{}
	for _, x := range theirs {
		set[x] = true
	}
	common := []string{}
	for _, x := range mine {
		if set[x] {
			common = append(common, x)
		}
	}
	return map[string]any{"days": days, "mine": mine, "partner": theirs, "common": common, "busy": my, "partner_busy": their, "today": from}
}

// SlotStart: the default hour of a part of a day — 11:00, 14:00, 19:00
func SlotStart(day string, part int, loc *time.Location) (time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil || part < 0 || part > 2 {
		return time.Time{}, fmt.Errorf("bad slot")
	}
	return d.Add(time.Duration([]int{11, 14, 19}[part]) * time.Hour), nil
}
