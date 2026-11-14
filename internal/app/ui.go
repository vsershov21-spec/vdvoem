package app

import (
	"context"
	"fmt"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// Which interface a member has open decides how the bot talks to them. The app says it at every launch:
// the old screens — 1, «Вдвоём 2.0» — 2. Someone who has not opened the app since is written to the old way.
const uiKey = "sys.ui"

// SetUI remembers the interface the member has just opened
func (a *App) SetUI(ctx context.Context, memberID int64, v int) error {
	val := map[int]string{1: "v1", 2: "v2"}[v]
	if val == "" {
		return ErrBadInput
	}
	if a.S.MemberSettings(ctx, memberID)[uiKey] == val {
		return nil
	}
	return a.S.SetMemberSetting(ctx, memberID, uiKey, val)
}

func (a *App) usesV2(ctx context.Context, memberID int64) bool {
	return a.S.MemberSettings(ctx, memberID)[uiKey] == "v2"
}

// UsesV2: the member's app is the new interface, so the bot follows its flow — one offers, the other answers
func (a *App) UsesV2(ctx context.Context, memberID int64) bool { return a.usesV2(ctx, memberID) }

// anyV2: at least one of the space uses the new interface (what is sent to the couple as a whole)
func (a *App) anyV2(ctx context.Context, ms []store.Member) bool {
	for _, m := range ms {
		if a.usesV2(ctx, m.ID) {
			return true
		}
	}
	return false
}

// meetPath: the meeting as the member's interface names it. The new shell opens the old path too.
func (a *App) meetPath(ctx context.Context, memberID, roundID int64) string {
	if a.usesV2(ctx, memberID) {
		return fmt.Sprintf("meet/%d", roundID)
	}
	return fmt.Sprintf("round/%d", roundID)
}

func (a *App) meetKeys(roundID int64) *tg.Markup {
	return tg.Keyboard(tg.Row(tg.App("Открыть встречу", a.appURL(fmt.Sprintf("meet/%d", roundID)))))
}

// tellQuiet: a silent message of a kind the member may have turned off
func (a *App) tellQuiet(ctx context.Context, m store.Member, key, text string, mk *tg.Markup) {
	if a.mySetting(ctx, m.ID, key) != "0" {
		a.sendQuiet(ctx, m.ChatID, text, mk)
	}
}

// dropMeetProposals: a meeting is cancelled, closed or got another idea — what was waiting about it has
// nothing to wait for
func (a *App) dropMeetProposals(ctx context.Context, spaceID, roundID int64) {
	ps, err := a.S.LiveProposals(ctx, spaceID)
	if err != nil {
		return
	}
	for _, p := range ps {
		if p.RefID == roundID && p.State == domain.ProposalWait && (p.Kind == domain.ProposalTime || p.Kind == domain.ProposalStop) {
			_ = a.S.CloseProposal(ctx, spaceID, p.ID, domain.ProposalWithdrawn, a.now())
		}
	}
}

// Call: «Идём» and «Позвать» of the new interface. The meeting is made without a time, and the partner is
// told who calls. An event with its own date and time is offered at that time at once — as a proposal the
// partner answers «Да» or «Не могу», never as a vote on slots.
func (a *App) Call(ctx context.Context, c Couple, ideaID int64) (domain.Round, error) {
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err != nil {
		return domain.Round{}, err
	}
	fixed := a.fixed(i, a.now())
	r, err := a.goWith(ctx, c, i, !fixed)
	if err != nil {
		return r, err
	}
	if fixed {
		if _, err := a.ProposeTime(ctx, c, r.ID, *i.StartsAt); err != nil {
			a.announceCall(ctx, c, r, i, "")
		}
	}
	return r, nil
}

// announceCall: what the two hear when a meeting without a time appears
func (a *App) announceCall(ctx context.Context, c Couple, r domain.Round, i store.Idea, instead string) {
	for _, m := range a.both(c) {
		if a.usesV2(ctx, m.ID) {
			// the one who called is in the card already; the partner gets one line and the card, no slots
			if m.ID != c.Me.ID {
				a.tell(ctx, m, c.Loc(), "notify.proposals", fmt.Sprintf("%s зовёт: <b>%s</b>%s. Время ещё не выбрано.", esc(c.Me.Name), esc(i.Title), instead), a.meetKeys(r.ID))
			}
			continue
		}
		lead := fmt.Sprintf("Решено: <b>%s</b>.", esc(i.Title))
		if m.ID != c.Me.ID {
			lead = fmt.Sprintf("%s хочет: <b>%s</b>.", esc(c.Me.Name), esc(i.Title))
		}
		a.send(ctx, m.ChatID, lead+" Осталось найти время — отметь, когда тебе удобно.",
			tg.Keyboard(tg.Row(tg.App("Отметить время", a.appURL(fmt.Sprintf("round/%d", r.ID)))), tg.Row(tg.CB("Не сейчас", fmt.Sprintf("x:%d", r.ID)))))
	}
}

// Rate: «как прошло» of the new interface, each for themselves — like | soso | skip
func (a *App) Rate(ctx context.Context, c Couple, roundID int64, k string) error {
	switch k {
	case "like", "soso":
		if _, err := a.Done(ctx, c, roundID, "yes"); err != nil {
			return err
		}
		_, err := a.Again(ctx, c, roundID, map[bool]string{true: "yes", false: "no"}[k == "like"])
		return err
	case "skip":
		_, err := a.Done(ctx, c, roundID, "no")
		return err
	}
	return ErrBadInput
}
