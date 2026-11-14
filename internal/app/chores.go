package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// Chores: the «Дела» tab. The rules of where a chore sits and who may do what are in domain/chore.go;
// here they meet the database, the partner's answer (a chore request is a proposal) and the bot.

var ErrChoreState = errors.New("chore_state")

type ChoreIn struct {
	Title, Note string
	Who         string // me | partner | any
	Due         *time.Time
	DueTime     bool
	Repeat      string
}

// ChoreView is a chore as one member sees it
type ChoreView struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Note     string   `json:"note"`
	Who      string   `json:"who"` // me | partner | any
	WhoName  string   `json:"who_name"`
	Due      *int64   `json:"due,omitempty"`
	DueTime  bool     `json:"due_time"`
	Repeat   string   `json:"repeat"`
	State    string   `json:"state"`
	Section  string   `json:"section"`
	Actions  []string `json:"actions"`
	AskedBy  string   `json:"asked_by,omitempty"` // me | partner — while it waits or was declined
	DoneBy   string   `json:"done_by,omitempty"`  // the name
	DoneAt   *int64   `json:"done_at,omitempty"`
	Proposal int64    `json:"proposal,omitempty"`
}

func (a *App) whoID(c Couple, who string) (int64, error) {
	switch who {
	case "me", "":
		return c.Me.ID, nil
	case "any":
		return 0, nil
	case "partner":
		if c.Partner == nil {
			return 0, ErrNotPaired
		}
		return c.Partner.ID, nil
	}
	return 0, ErrBadInput
}

func whoOf(c Couple, id int64) (string, string) {
	switch {
	case id == 0:
		return "any", "кто угодно"
	case id == c.Me.ID:
		return "me", c.Me.Name
	}
	return "partner", c.PartnerName()
}

func (a *App) choreView(c Couple, ch domain.Chore, now time.Time) ChoreView {
	v := ChoreView{ID: ch.ID, Title: ch.Title, Note: ch.Note, DueTime: ch.DueTime, Repeat: ch.Repeat, State: string(ch.State),
		Section: ch.Section(now, c.Loc()), Actions: ch.Actions(c.Me.ID, now, c.Loc()), Proposal: ch.ProposalID}
	v.Who, v.WhoName = whoOf(c, ch.Assignee)
	if v.Actions == nil {
		v.Actions = []string{}
	}
	if ch.Due != nil {
		u := ch.Due.Unix()
		v.Due = &u
	}
	if ch.State == domain.ChoreAsk || ch.State == domain.ChoreDeclined {
		v.AskedBy, _ = whoOf(c, ch.AuthorID)
	}
	if ch.State == domain.ChoreDone {
		_, v.DoneBy = whoOf(c, ch.DoneBy)
		if ch.DoneAt != nil {
			u := ch.DoneAt.Unix()
			v.DoneAt = &u
		}
	}
	return v
}

func (a *App) keepDays(ctx context.Context, c Couple) int {
	if a.pairSetting(ctx, c.Space.ID, "chores.keep") == "30" {
		return 30
	}
	return 7
}

// Chores: the tab — every chore that still has a place in it, in the order of its sections
func (a *App) Chores(ctx context.Context, c Couple) ([]ChoreView, domain.ChoreSummary, error) {
	cs, err := a.S.Chores(ctx, c.Space.ID)
	if err != nil {
		return nil, domain.ChoreSummary{}, err
	}
	now, keep := a.now(), a.keepDays(ctx, c)
	domain.SortChores(cs)
	out := []ChoreView{}
	for _, sec := range []string{"ask", "overdue", "today", "later", "done"} {
		for _, ch := range cs {
			if ch.InTab(now, keep) && ch.Section(now, c.Loc()) == sec {
				out = append(out, a.choreView(c, ch, now))
			}
		}
	}
	return out, domain.SummarizeChores(cs, c.Me.ID, now, c.Loc()), nil
}

// ChoreSummary: the one line of the home screen
func (a *App) ChoreSummary(ctx context.Context, c Couple) domain.ChoreSummary {
	cs, err := a.S.Chores(ctx, c.Space.ID)
	if err != nil {
		return domain.ChoreSummary{Titles: []string{}}
	}
	return domain.SummarizeChores(cs, c.Me.ID, a.now(), c.Loc())
}

// ChoreHistory: who did what and when — every done chore, the newest first
func (a *App) ChoreHistory(ctx context.Context, c Couple, q, who string) ([]ChoreView, error) {
	var by int64
	switch who {
	case "me":
		by = c.Me.ID
	case "partner":
		if c.Partner != nil {
			by = c.Partner.ID
		}
	}
	cs, err := a.S.DoneChores(ctx, c.Space.ID, q, by, 200)
	if err != nil {
		return nil, err
	}
	out := []ChoreView{}
	for _, ch := range cs {
		out = append(out, a.choreView(c, ch, a.now()))
	}
	return out, nil
}

// AddChore: for me or for anyone it is simply there; for the partner it is a request they answer
func (a *App) AddChore(ctx context.Context, c Couple, in ChoreIn) (domain.Chore, error) {
	title := clip(in.Title, 120)
	if title == "" || !domain.ValidRepeat(in.Repeat) {
		return domain.Chore{}, ErrBadInput
	}
	who, err := a.whoID(c, in.Who)
	if err != nil {
		return domain.Chore{}, err
	}
	ch := domain.Chore{SpaceID: c.Space.ID, AuthorID: c.Me.ID, Assignee: who, Title: title, Note: clip(in.Note, 500),
		Due: in.Due, DueTime: in.DueTime, Repeat: in.Repeat, State: domain.ChoreOpen}
	if in.Who == "partner" {
		ch.State = domain.ChoreAsk
	}
	if ch, err = a.S.CreateChore(ctx, ch, a.now()); err != nil {
		return ch, err
	}
	if ch.State == domain.ChoreAsk {
		if err = a.askChore(ctx, c, &ch); err != nil {
			return ch, err
		}
	}
	a.Live.From(ctx, c.Space.ID)
	return ch, nil
}

func (a *App) askChore(ctx context.Context, c Couple, ch *domain.Chore) error {
	p, err := a.Propose(ctx, c, domain.ProposalChore, ch.ID, ch.Title, a.dueWords(c, *ch), "")
	if err != nil {
		return err
	}
	ch.ProposalID = p.ID
	return a.S.SaveChore(ctx, *ch)
}

// choreAnswered is what «Беру» / «Не могу» does: the chore goes to the one who took it, or back to the asker
func (a *App) choreAnswered(ctx context.Context, c Couple, p domain.Proposal, yes bool) error {
	ch, err := a.S.Chore(ctx, c.Space.ID, p.RefID)
	if err != nil || ch.State != domain.ChoreAsk {
		return nil // a request about nothing in particular, or the chore has moved on
	}
	ch.ProposalID = 0
	if yes {
		ch.State, ch.Assignee = domain.ChoreOpen, c.Me.ID
	} else {
		ch.State, ch.Assignee = domain.ChoreDeclined, 0
		ch.ProposalID = p.ID // the author's «нет» row is closed together with their choice
	}
	return a.S.SaveChore(ctx, ch)
}

// ChoreAct: one tap on a chore. The action must be one the member is offered (domain.Chore.Actions).
func (a *App) ChoreAct(ctx context.Context, c Couple, id int64, act string) (domain.Chore, error) {
	ch, err := a.S.Chore(ctx, c.Space.ID, id)
	if err != nil {
		return ch, err
	}
	now := a.now()
	if act == "delete" || act == "restore" {
		if act == "delete" && ch.State != domain.ChoreDeleted {
			if ch.State == domain.ChoreAsk && ch.ProposalID > 0 {
				_ = a.S.CloseProposal(ctx, c.Space.ID, ch.ProposalID, domain.ProposalWithdrawn, now)
			}
			ch.Note = string(ch.State) + "\x00" + ch.Note // the state to come back to
			ch.State = domain.ChoreDeleted
		} else if act == "restore" && ch.State == domain.ChoreDeleted {
			st, note, _ := strings.Cut(ch.Note, "\x00")
			ch.State, ch.Note = domain.ChoreState(st), note
			if ch.State == domain.ChoreAsk || ch.State == "" { // the request was withdrawn with the delete
				ch.State, ch.Assignee, ch.ProposalID = domain.ChoreOpen, c.Me.ID, 0
			}
		} else {
			return ch, ErrChoreState
		}
		err = a.S.SaveChore(ctx, ch)
		a.Live.From(ctx, c.Space.ID)
		return ch, err
	}
	ok := false
	for _, x := range ch.Actions(c.Me.ID, now, c.Loc()) {
		ok = ok || x == act
	}
	if !ok {
		return ch, ErrChoreState
	}
	switch act {
	case "done":
		ch.State, ch.DoneBy, ch.DoneAt = domain.ChoreDone, c.Me.ID, &now
		if next := ch.NextDue(c.Loc()); next != nil { // a repeating chore: the next one appears at once
			n := domain.Chore{SpaceID: ch.SpaceID, AuthorID: ch.AuthorID, Assignee: ch.Assignee, Title: ch.Title, Note: ch.Note,
				Due: next, DueTime: ch.DueTime, Repeat: ch.Repeat, State: domain.ChoreOpen}
			if _, err = a.S.CreateChore(ctx, n, now); err != nil {
				return ch, err
			}
			ch.Repeat = "" // the done one is a single past occurrence
		}
	case "reopen":
		ch.State, ch.DoneBy, ch.DoneAt = domain.ChoreOpen, 0, nil
	case "take":
		if ch.State == domain.ChoreAsk {
			_, err = a.AnswerProposal(ctx, c, ch.ProposalID, true)
			return a.reread(ctx, c, id, err)
		}
		ch.Assignee = c.Me.ID
	case "decline":
		_, err = a.AnswerProposal(ctx, c, ch.ProposalID, false)
		return a.reread(ctx, c, id, err)
	case "pass": // mine → a request to the partner; if they say no it comes back to me
		if c.Partner == nil {
			return ch, ErrNotPaired
		}
		ch.State, ch.AuthorID, ch.Assignee = domain.ChoreAsk, c.Me.ID, c.Partner.ID
		if err = a.askChore(ctx, c, &ch); err != nil {
			return ch, err
		}
		a.Live.From(ctx, c.Space.ID)
		return ch, nil
	case "withdraw":
		if ch.ProposalID > 0 {
			_ = a.S.CloseProposal(ctx, c.Space.ID, ch.ProposalID, domain.ProposalWithdrawn, now)
		}
		ch.State, ch.Assignee, ch.ProposalID = domain.ChoreOpen, c.Me.ID, 0
	case "self", "any":
		if ch.ProposalID > 0 {
			_ = a.S.MarkProposalSeen(ctx, c.Space.ID, ch.ProposalID)
		}
		ch.State, ch.ProposalID, ch.Assignee = domain.ChoreOpen, 0, 0
		if act == "self" {
			ch.Assignee = c.Me.ID
		}
	case "nudge":
		if c.Partner != nil {
			a.tell(ctx, *c.Partner, c.Loc(), "notify.chores", fmt.Sprintf("%s напоминает: <b>%s</b>%s", esc(c.Me.Name), esc(ch.Title), subLine(a.dueWords(c, ch))), a.choreKeys(ch))
		}
		return ch, nil
	case "move": // «Перенести срок» without a date: to tomorrow; a date comes through EditChore
		t := now.In(c.Loc()).AddDate(0, 0, 1)
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, c.Loc())
		ch.Due, ch.DueTime = &d, false
	}
	err = a.S.SaveChore(ctx, ch)
	a.Live.From(ctx, c.Space.ID)
	return ch, err
}

func (a *App) reread(ctx context.Context, c Couple, id int64, err error) (domain.Chore, error) {
	if err != nil {
		return domain.Chore{}, err
	}
	return a.S.Chore(ctx, c.Space.ID, id)
}

// EditChore: the fields of the card. Handing it to the partner is «pass», not an edit.
func (a *App) EditChore(ctx context.Context, c Couple, id int64, in ChoreIn, setDue bool) (domain.Chore, error) {
	ch, err := a.S.Chore(ctx, c.Space.ID, id)
	if err != nil {
		return ch, err
	}
	if ch.State == domain.ChoreDone || ch.State == domain.ChoreDeleted {
		return ch, ErrChoreState
	}
	if t := clip(in.Title, 120); t != "" {
		ch.Title = t
	}
	ch.Note = clip(in.Note, 500)
	if !domain.ValidRepeat(in.Repeat) {
		return ch, ErrBadInput
	}
	ch.Repeat = in.Repeat
	if setDue {
		ch.Due, ch.DueTime = in.Due, in.DueTime && in.Due != nil
	}
	if ch.State == domain.ChoreOpen && (in.Who == "me" || in.Who == "any") {
		ch.Assignee, _ = a.whoID(c, in.Who)
	}
	err = a.S.SaveChore(ctx, ch)
	a.Live.From(ctx, c.Space.ID)
	return ch, err
}

func (a *App) dueWords(c Couple, ch domain.Chore) string {
	if ch.Due == nil {
		return ""
	}
	d := ch.Due.In(c.Loc())
	s := "до " + ruDate(d)
	if ch.DueTime {
		s += d.Format(", 15:04")
	}
	return s
}

var ruWeekdays = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
var choreMonths = [...]string{"", "янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

// ruDate: «сб 21 ноя»
func ruDate(t time.Time) string {
	return fmt.Sprintf("%s %d %s", ruWeekdays[t.Weekday()], t.Day(), choreMonths[t.Month()])
}

func (a *App) choreKeys(ch domain.Chore) *tg.Markup {
	return tg.Keyboard(tg.Row(tg.CB("✓ Сделано", fmt.Sprintf("cx:%d", ch.ID)), tg.App("Открыть", a.appURL("chores"))))
}

// choreDigest: once a day, at the couple's hour, each one gets their chores for today and the overdue ones
func (a *App) choreDigest(ctx context.Context, now time.Time) {
	all, err := a.S.OpenChoresAll(ctx)
	if err != nil || len(all) == 0 {
		return
	}
	bySpace := map[int64][]domain.Chore{}
	for _, ch := range all {
		bySpace[ch.SpaceID] = append(bySpace[ch.SpaceID], ch)
	}
	for sid, cs := range bySpace {
		sp, err := a.S.Space(ctx, sid)
		if err != nil {
			continue
		}
		loc := sp.Loc()
		hour := a.pairSetting(ctx, sid, "chores.remind")
		if hour == "off" || fmt.Sprint(now.In(loc).Hour()) != hour {
			continue
		}
		day := now.In(loc).Format("2006-01-02")
		ms, _ := a.S.Members(ctx, sid)
		for _, m := range ms {
			if a.S.MemberSettings(ctx, m.ID)["sys.chores_digest"] == day {
				continue
			}
			_ = a.S.SetMemberSetting(ctx, m.ID, "sys.chores_digest", day)
			s := domain.SummarizeChores(cs, m.ID, now, loc)
			if s.Today+s.Overdue == 0 {
				continue
			}
			var b strings.Builder
			if s.Overdue > 0 {
				b.WriteString("<b>Просрочено:</b>\n")
				for _, t := range s.Titles[:s.Overdue] {
					b.WriteString("• " + esc(t) + "\n")
				}
			}
			if s.Today > 0 {
				b.WriteString("<b>Дела на сегодня:</b>\n")
				for _, t := range s.Titles[s.Overdue:] {
					b.WriteString("• " + esc(t) + "\n")
				}
			}
			a.tell(ctx, m, loc, "notify.chores", strings.TrimSpace(b.String()), tg.Keyboard(tg.Row(tg.App("Открыть дела", a.appURL("chores")))))
		}
	}
}

// ---------- settings ----------

func (a *App) pairSetting(ctx context.Context, spaceID int64, key string) string {
	return domain.ResolveSettings(domain.ScopePair, a.S.SpaceSettings(ctx, spaceID))[key]
}

func (a *App) mySetting(ctx context.Context, memberID int64, key string) string {
	return domain.ResolveSettings(domain.ScopeMe, a.S.MemberSettings(ctx, memberID))[key]
}

// SettingsView: both groups with their current values, and the list of what can be set
func (a *App) SettingsView(ctx context.Context, c Couple) map[string]any {
	return map[string]any{
		"me":   domain.ResolveSettings(domain.ScopeMe, a.S.MemberSettings(ctx, c.Me.ID)),
		"pair": domain.ResolveSettings(domain.ScopePair, a.S.SpaceSettings(ctx, c.Space.ID)),
		"defs": domain.Settings,
	}
}

// SetSetting: mine changes quietly; the couple's is changed by either, and the other one is told
func (a *App) SetSetting(ctx context.Context, c Couple, key, value string) error {
	d, ok := domain.SettingOf(key)
	if !ok || !domain.ValidSetting(key, value) {
		return ErrBadInput
	}
	if d.Scope == domain.ScopeMe {
		return a.S.SetMemberSetting(ctx, c.Me.ID, key, value)
	}
	if a.pairSetting(ctx, c.Space.ID, key) == value {
		return nil
	}
	if err := a.S.SetSpaceSetting(ctx, c.Space.ID, key, value); err != nil {
		return err
	}
	if c.Partner != nil {
		ref := int64(0)
		for k, x := range domain.Settings {
			if x.Key == key {
				ref = int64(k + 1)
			}
		}
		_, _ = a.Propose(ctx, c, domain.ProposalNote, ref, fmt.Sprintf("%s %s настройку «%s»", c.Me.Name, Past(c.Me.Name, "изменил"), d.Label), domain.SettingWords(key, value), "")
	}
	a.Live.From(ctx, c.Space.ID)
	return nil
}

// tell: a message to a member in the bot, if they want this kind there; in their quiet hours it comes silently
func (a *App) tell(ctx context.Context, m store.Member, loc *time.Location, key, text string, mk *tg.Markup) {
	if a.mySetting(ctx, m.ID, key) == "0" {
		return
	}
	now := a.now()
	if a.mySetting(ctx, m.ID, "notify.quiet") == "1" && !domain.QuietShift(now, loc).Equal(now) {
		a.sendQuiet(ctx, m.ChatID, text, mk)
		return
	}
	a.send(ctx, m.ChatID, text, mk)
}
