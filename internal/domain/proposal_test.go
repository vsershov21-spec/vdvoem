package domain

import (
	"errors"
	"testing"
	"time"
)

func TestProposalAnswerRules(t *testing.T) {
	p := Proposal{ID: 1, AuthorID: 10, State: ProposalWait}
	if err := p.CanAnswer(10); !errors.Is(err, ErrOwnProposal) {
		t.Fatalf("the author must not answer their own proposal: %v", err)
	}
	if err := p.CanAnswer(20); err != nil {
		t.Fatalf("the partner answers: %v", err)
	}
	p.State = ProposalYes
	if err := p.CanAnswer(20); !errors.Is(err, ErrProposalClosed) {
		t.Fatalf("answered once — closed: %v", err)
	}
}

func TestProposalRows(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	names := func(id int64) string { return map[int64]string{10: "Вова", 20: "Настя"}[id] }
	tm := Proposal{ID: 1, Kind: ProposalTime, AuthorID: 10, Title: "Планетарий", Sub: "вс 15 ноя, 13:00", State: ProposalWait, CreatedAt: t0}

	// the partner sees the call with the answers in the row
	r, ok := tm.Row(20, "Вова", "Вова", t0)
	if !ok || r.Role != "answer" || r.Title != "Вова зовёт: Планетарий" || len(r.Actions) != 3 || !r.Hot {
		t.Fatalf("partner row: %+v %v", r, ok)
	}
	// the author sees «ждём», quietly
	r, ok = tm.Row(10, "Вова", "Настя", t0)
	if !ok || r.Role != "waiting" || r.Hot {
		t.Fatalf("author row: %+v %v", r, ok)
	}
	// three days of silence: the author gets «без ответа»
	r, _ = tm.Row(10, "Вова", "Настя", t0.Add(ProposalStaleAfter))
	if r.Role != "stale" || !r.Hot || r.Actions[0] != "remind" || r.Actions[1] != "withdraw" {
		t.Fatalf("stale row: %+v", r)
	}
	// «нет» comes back to the author and never disappears silently
	no := tm
	no.State = ProposalNo
	r, ok = no.Row(10, "Вова", "Настя", t0)
	if !ok || r.Role != "declined" || r.Title != "Настя не может вс 15 ноя, 13:00" || r.Actions[0] != "other" {
		t.Fatalf("declined row: %+v %v", r, ok)
	}
	if _, ok = no.Row(20, "Вова", "Вова", t0); ok {
		t.Fatal("the one who said no has nothing more to do")
	}
	no.AuthorSaw = true
	if _, ok = no.Row(10, "Вова", "Настя", t0); ok {
		t.Fatal("seen by the author — the row leaves")
	}
	// a chore that was declined returns with two ways out
	ch := Proposal{ID: 2, Kind: ProposalChore, AuthorID: 10, Title: "Забрать торт", State: ProposalNo}
	r, _ = ch.Row(10, "Вова", "Настя", t0)
	if r.Title != "Настя не может: забрать торт" || len(r.Actions) != 2 || r.Actions[0] != "self" {
		t.Fatalf("declined chore: %+v", r)
	}
	// a cancelled meeting: the other one sees it, with the right verb form
	c := Proposal{ID: 3, Kind: ProposalCancel, AuthorID: 20, Title: "Планетарий", State: ProposalWait}
	r, ok = c.Row(10, "Настя", "Настя", t0)
	if !ok || r.Role != "notice" || r.Title != "Настя отменила встречу: Планетарий" {
		t.Fatalf("cancel notice: %+v %v", r, ok)
	}
	if _, ok = c.Row(20, "Настя", "Вова", t0); ok {
		t.Fatal("the one who cancelled is not told about it")
	}

	// the inbox: what needs me first
	rows := Inbox([]Proposal{tm, ch, c}, 10, names, "Настя", t0)
	if len(rows) != 3 || rows[0].Role != "declined" || rows[1].Role != "notice" || rows[2].Role != "waiting" {
		t.Fatalf("inbox order: %+v", rows)
	}
}

func TestProposalReminder(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	p := Proposal{State: ProposalWait, CreatedAt: t0}
	if p.RemindDue(t0.Add(23 * time.Hour)) {
		t.Fatal("too early")
	}
	if !p.RemindDue(t0.Add(24 * time.Hour)) {
		t.Fatal("a day has passed")
	}
	p.Reminded = true
	if p.RemindDue(t0.Add(48 * time.Hour)) {
		t.Fatal("one reminder only")
	}
	p.State = ProposalYes
	if p.Stale(t0.Add(100 * time.Hour)) {
		t.Fatal("answered proposals do not go stale")
	}
}
