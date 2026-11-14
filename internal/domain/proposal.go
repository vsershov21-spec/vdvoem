package domain

import (
	"errors"
	"strings"
	"time"
)

// A proposal is the one way two people agree on anything here: the time of a meeting, a move, a stop
// «а потом», a chore. One proposes, the other answers «да» or «нет» in the row itself — on the home screen,
// in the card, or in the bot; answered in one place, it goes out in the other two.

type ProposalState string

const (
	ProposalWait      ProposalState = "wait"
	ProposalYes       ProposalState = "yes"
	ProposalNo        ProposalState = "no"
	ProposalWithdrawn ProposalState = "withdrawn"
)

const (
	ProposalTime   = "time"   // «Вова зовёт: Планетарий, вс 13:00»
	ProposalStop   = "stop"   // a place after the meeting
	ProposalChore  = "chore"  // «Вова просит: забрать торт»
	ProposalCancel = "cancel" // a cancelled meeting: nothing to answer, only to see
	ProposalNote   = "note"   // a plain notice for the partner: «Вова изменил бюджет на встречу»
)

const (
	ProposalRemindAfter = 24 * time.Hour // the partner is reminded once
	ProposalStaleAfter  = 72 * time.Hour // then the author sees «без ответа»
)

var (
	ErrOwnProposal    = errors.New("own_proposal")
	ErrProposalClosed = errors.New("proposal_closed")
)

type Proposal struct {
	ID, SpaceID, RefID, AuthorID int64
	Kind, Title, Sub, Payload    string
	State                        ProposalState
	CreatedAt                    time.Time
	AnsweredAt                   *time.Time
	Reminded, AuthorSaw          bool
}

func ValidProposalKind(k string) bool {
	return k == ProposalTime || k == ProposalStop || k == ProposalChore || k == ProposalCancel || k == ProposalNote
}

// CanAnswer: only the other one, and only while it waits
func (p Proposal) CanAnswer(member int64) error {
	if p.AuthorID == member {
		return ErrOwnProposal
	}
	if p.State != ProposalWait {
		return ErrProposalClosed
	}
	return nil
}

func (p Proposal) RemindDue(now time.Time) bool {
	return p.State == ProposalWait && !p.Reminded && !now.Before(p.CreatedAt.Add(ProposalRemindAfter))
}

func (p Proposal) Stale(now time.Time) bool {
	return p.State == ProposalWait && !now.Before(p.CreatedAt.Add(ProposalStaleAfter))
}

// InboxRow is one line of «Ждёт тебя». Role says what the member can do with it:
//
//	answer   — the partner proposed, I answer (yes / no, and «other» where a counter-offer makes sense)
//	declined — I proposed, the partner said no: I choose what next
//	stale    — I proposed three days ago and there is still no answer: remind or withdraw
//	notice   — nothing to decide, only to see (a cancelled meeting)
//	waiting  — I proposed, the partner has not answered yet (shown in the card, not on the home screen)
type InboxRow struct {
	ProposalID  int64    `json:"id"`
	Kind        string   `json:"kind"`
	RefID       int64    `json:"ref_id"`
	Role        string   `json:"role"`
	Title       string   `json:"title"`
	Sub         string   `json:"sub"`
	Hot         bool     `json:"hot"`
	Actions     []string `json:"actions"`
	CreatedUnix int64    `json:"created"`
}

// Row: how this proposal looks to a member, or false when it is none of their business right now.
// author and partner are the two names as the reader should see them («Вова», «Настя»).
func (p Proposal) Row(me int64, author, partner string, now time.Time) (InboxRow, bool) {
	r := InboxRow{ProposalID: p.ID, Kind: p.Kind, RefID: p.RefID, Sub: p.Sub, CreatedUnix: p.CreatedAt.Unix()}
	mine := p.AuthorID == me
	switch {
	case p.Kind == ProposalCancel || p.Kind == ProposalNote:
		if mine || p.State != ProposalWait {
			return r, false
		}
		r.Role, r.Hot, r.Actions = "notice", true, []string{"ok"}
		r.Title = p.Title
		if p.Kind == ProposalCancel {
			r.Title = author + " " + pastVerb(author, "отменил") + " встречу: " + p.Title
		}
	case p.State == ProposalWait && !mine:
		r.Role, r.Hot = "answer", true
		switch p.Kind {
		case ProposalTime:
			r.Title, r.Actions = author+" зовёт: "+p.Title, []string{"yes", "other", "no"}
		case ProposalStop:
			r.Title, r.Actions = author+" предлагает после: "+p.Title, []string{"yes", "no"}
		default:
			r.Title, r.Actions = author+" просит: "+lowerFirst(p.Title), []string{"yes", "no"}
		}
	case p.State == ProposalWait && mine:
		if p.Stale(now) {
			r.Role, r.Hot, r.Actions = "stale", true, []string{"remind", "withdraw"}
			r.Title = "Без ответа: " + p.Title
		} else {
			r.Role = "waiting"
			r.Title = "Ждём ответа: " + p.Title
		}
	case p.State == ProposalNo && mine && !p.AuthorSaw:
		r.Role, r.Hot = "declined", true
		switch p.Kind {
		case ProposalTime:
			r.Title, r.Actions = partner+" не может "+p.Sub, []string{"other"}
			r.Sub = p.Title
		case ProposalStop:
			r.Title, r.Actions = partner+": нет, не «"+p.Title+"»", []string{"other"}
		default:
			r.Title, r.Actions = partner+" не может: "+lowerFirst(p.Title), []string{"self", "any"}
		}
	default:
		return r, false
	}
	return r, true
}

// Inbox: the rows of a member, what needs an answer first, then the newest
func Inbox(ps []Proposal, me int64, nameOf func(int64) string, partner string, now time.Time) []InboxRow {
	var hot, rest []InboxRow
	for _, p := range ps {
		if r, ok := p.Row(me, nameOf(p.AuthorID), partner, now); ok {
			if r.Hot {
				hot = append(hot, r)
			} else {
				rest = append(rest, r)
			}
		}
	}
	return append(hot, rest...)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToLower(string(r[:1])) + string(r[1:])
}

// pastVerb: «отменил» / «отменила» by the name's ending — the same rule the bot's texts use
func pastVerb(name, verb string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasSuffix(n, "а") || strings.HasSuffix(n, "я") {
		return verb + "а"
	}
	return verb
}
