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

// Proposals: «предложил один, отвечает второй». The same proposal lies in three places — the home screen
// («Ждёт тебя»), the card it is about, and the bot — and is answered in any of them. See domain/proposal.go.

func init() { domain.PastVerb = Past }

// ProposalEffect runs when a proposal is answered: the meeting gets its time, the chore its owner.
// A kind without an effect is still a complete exchange — the answer is stored and both are told.
type ProposalEffect func(ctx context.Context, c Couple, p domain.Proposal, yes bool) error

// OnProposal registers what an answer to a kind of proposal does (called once, at start-up)
func (a *App) OnProposal(kind string, f ProposalEffect) {
	if a.effects == nil {
		a.effects = map[string]ProposalEffect{}
	}
	a.effects[kind] = f
}

// Propose: I offer the partner something to answer. A previous waiting offer about the same thing is replaced.
func (a *App) Propose(ctx context.Context, c Couple, kind string, ref int64, title, sub, payload string) (domain.Proposal, error) {
	if c.Partner == nil {
		return domain.Proposal{}, ErrNotPaired
	}
	if !domain.ValidProposalKind(kind) || strings.TrimSpace(title) == "" {
		return domain.Proposal{}, ErrBadInput
	}
	p, err := a.S.CreateProposal(ctx, domain.Proposal{SpaceID: c.Space.ID, Kind: kind, RefID: ref, AuthorID: c.Me.ID,
		Title: clip(title, 120), Sub: clip(sub, 120), Payload: payload}, a.now())
	if err != nil {
		return p, err
	}
	a.tell(ctx, *c.Partner, c.Loc(), "notify.proposals", a.proposalText(p, c.Me.Name, false), a.proposalKeys(p))
	a.Live.From(ctx, c.Space.ID)
	return p, nil
}

// AnswerProposal: the partner says yes or no. The author is told at once, and a «нет» stays on the author's
// home screen until they have seen it.
func (a *App) AnswerProposal(ctx context.Context, c Couple, id int64, yes bool) (domain.Proposal, error) {
	p, err := a.S.Proposal(ctx, c.Space.ID, id)
	if err != nil {
		return p, err
	}
	if err = p.CanAnswer(c.Me.ID); err != nil {
		return p, err
	}
	st := domain.ProposalNo
	notice := p.Kind == domain.ProposalCancel || p.Kind == domain.ProposalNote
	if yes || notice { // a notice has only «понятно»
		st = domain.ProposalYes
	}
	if err = a.S.CloseProposal(ctx, c.Space.ID, id, st, a.now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return p, domain.ErrProposalClosed
		}
		return p, err
	}
	p.State = st
	f := a.effects[p.Kind]
	if f == nil { // the built-in effects: a chore goes to the one who took it, a meeting gets its time, a route its stop
		switch p.Kind {
		case domain.ProposalChore:
			f = a.choreAnswered
		case domain.ProposalTime:
			f = a.timeAnswered
		case domain.ProposalStop:
			f = a.stopAnswered
		}
	}
	if f != nil {
		if err = f(ctx, c, p, yes); err != nil {
			return p, err
		}
	}
	if c.Partner != nil && !notice {
		word := "«да»"
		if !yes {
			word = "«нет»"
		}
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s %s %s: <b>%s</b>%s", esc(c.Me.Name), Past(c.Me.Name, "ответил"), word, esc(p.Title), subLine(p.Sub)),
			tg.Keyboard(tg.Row(tg.App("Открыть", a.appURL(proposalPath(p))))))
	}
	a.Live.From(ctx, c.Space.ID)
	return p, nil
}

// WithdrawProposal: the author takes the offer back
func (a *App) WithdrawProposal(ctx context.Context, c Couple, id int64) error {
	p, err := a.S.Proposal(ctx, c.Space.ID, id)
	if err != nil {
		return err
	}
	if p.AuthorID != c.Me.ID {
		return ErrNotYours
	}
	if err = a.S.CloseProposal(ctx, c.Space.ID, id, domain.ProposalWithdrawn, a.now()); errors.Is(err, store.ErrNotFound) {
		return domain.ErrProposalClosed
	}
	return err
}

// SeenProposal: the author has read the «нет» (or acted on it) — the row leaves the home screen
func (a *App) SeenProposal(ctx context.Context, c Couple, id int64) error {
	p, err := a.S.Proposal(ctx, c.Space.ID, id)
	if err != nil {
		return err
	}
	if p.AuthorID != c.Me.ID {
		return ErrNotYours
	}
	return a.S.MarkProposalSeen(ctx, c.Space.ID, id)
}

// RemindProposal: the author nudges by hand («Напомнить» on a proposal without an answer)
func (a *App) RemindProposal(ctx context.Context, c Couple, id int64) error {
	p, err := a.S.Proposal(ctx, c.Space.ID, id)
	if err != nil {
		return err
	}
	if p.AuthorID != c.Me.ID {
		return ErrNotYours
	}
	if p.State != domain.ProposalWait || c.Partner == nil {
		return domain.ErrProposalClosed
	}
	a.send(ctx, c.Partner.ChatID, a.proposalText(p, c.Me.Name, true), a.proposalKeys(p))
	return a.S.MarkProposalReminded(ctx, p.ID)
}

// Inbox: «Ждёт тебя» of a member, plus what they are waiting for themselves
func (a *App) Inbox(ctx context.Context, c Couple) []domain.InboxRow {
	ps, err := a.S.LiveProposals(ctx, c.Space.ID)
	if err != nil || len(ps) == 0 {
		return []domain.InboxRow{}
	}
	name := func(id int64) string {
		if id == c.Me.ID {
			return c.Me.Name
		}
		return c.PartnerName()
	}
	rows := domain.Inbox(ps, c.Me.ID, name, c.PartnerName(), a.now())
	if rows == nil {
		rows = []domain.InboxRow{}
	}
	return rows
}

// proposalTick: one reminder after a day of silence, never at night
func (a *App) proposalTick(ctx context.Context, now time.Time) {
	ps, err := a.S.WaitingProposals(ctx)
	if err != nil {
		return
	}
	for _, p := range ps {
		if !p.RemindDue(now) {
			continue
		}
		sp, err := a.S.Space(ctx, p.SpaceID)
		if err != nil {
			continue
		}
		if q := domain.QuietShift(now, sp.Loc()); !q.Equal(now) {
			continue // the morning tick sends it
		}
		ms, _ := a.S.Members(ctx, p.SpaceID)
		var by, to *store.Member
		for k := range ms {
			if ms[k].ID == p.AuthorID {
				by = &ms[k]
			} else {
				to = &ms[k]
			}
		}
		if by != nil && to != nil {
			a.send(ctx, to.ChatID, a.proposalText(p, by.Name, true), a.proposalKeys(p))
		}
		_ = a.S.MarkProposalReminded(ctx, p.ID)
	}
}

func (a *App) proposalText(p domain.Proposal, author string, again bool) string {
	head := ""
	if again {
		head = "Напоминаю. "
	}
	switch p.Kind {
	case domain.ProposalTime:
		return fmt.Sprintf("%s%s зовёт: <b>%s</b>%s", head, esc(author), esc(p.Title), subLine(p.Sub))
	case domain.ProposalStop:
		return fmt.Sprintf("%s%s предлагает после встречи: <b>%s</b>%s", head, esc(author), esc(p.Title), subLine(p.Sub))
	case domain.ProposalNote:
		return esc(p.Title) + subLine(p.Sub)
	case domain.ProposalCancel:
		return fmt.Sprintf("%s %s встречу: <b>%s</b>%s", esc(author), Past(author, "отменил"), esc(p.Title), subLine(p.Sub))
	default:
		return fmt.Sprintf("%s%s просит: <b>%s</b>%s", head, esc(author), esc(p.Title), subLine(p.Sub))
	}
}

func (a *App) proposalKeys(p domain.Proposal) *tg.Markup {
	open := tg.Row(tg.App("Открыть", a.appURL(proposalPath(p))))
	switch p.Kind {
	case domain.ProposalCancel, domain.ProposalNote:
		return tg.Keyboard(tg.Row(tg.CB("Понятно", fmt.Sprintf("pr:%d:yes", p.ID))), open)
	case domain.ProposalChore:
		return tg.Keyboard(tg.Row(tg.CB("Беру", fmt.Sprintf("pr:%d:yes", p.ID)), tg.CB("Не могу", fmt.Sprintf("pr:%d:no", p.ID))), open)
	case domain.ProposalTime:
		return tg.Keyboard(tg.Row(tg.CB("Да", fmt.Sprintf("pr:%d:yes", p.ID)), tg.CB("Не могу", fmt.Sprintf("pr:%d:no", p.ID))),
			tg.Row(tg.App("Другое время", a.appURL(proposalPath(p)))))
	}
	return tg.Keyboard(tg.Row(tg.CB("Да", fmt.Sprintf("pr:%d:yes", p.ID)), tg.CB("Нет", fmt.Sprintf("pr:%d:no", p.ID))), open)
}

// proposalPath: where in the app the proposal lives
func proposalPath(p domain.Proposal) string {
	if p.Kind == domain.ProposalChore {
		return "chores"
	}
	if p.Kind == domain.ProposalNote {
		return "settings"
	}
	if p.RefID > 0 {
		return fmt.Sprintf("round/%d", p.RefID)
	}
	return ""
}

func subLine(s string) string {
	if s == "" {
		return ""
	}
	return "\n" + esc(s)
}
