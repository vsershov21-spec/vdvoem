package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"vdvoem/internal/app"
)

func TestPasswordAndLockout(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	if err := a.SetPassword(ctx, v, "vova", "short"); err != app.ErrWeakPass {
		t.Fatalf("weak: %v", err)
	}
	if err := a.SetPassword(ctx, v, "в о в а", "longenough1"); err != app.ErrBadLoginID {
		t.Fatalf("login name: %v", err)
	}
	if err := a.SetPassword(ctx, v, "vova", "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPassword(ctx, n, "VOVA", "another pass"); err != app.ErrLoginTaken {
		t.Fatalf("taken (case-insensitive): %v", err)
	}
	if id, err := a.PasswordLogin(ctx, "Vova", "correct horse"); err != nil || id != v.Me.ID {
		t.Fatalf("login: %v %d", err, id)
	}
	for k := 0; k < 5; k++ {
		if _, err := a.PasswordLogin(ctx, "vova", "wrong"); err != app.ErrBadLogin {
			t.Fatalf("attempt %d: %v", k, err)
		}
	}
	if _, err := a.PasswordLogin(ctx, "vova", "correct horse"); err != app.ErrLocked {
		t.Fatalf("must be locked: %v", err)
	}
	ck.t = ck.t.Add(16 * time.Minute)
	if _, err := a.PasswordLogin(ctx, "vova", "correct horse"); err != nil {
		t.Fatalf("unlocked: %v", err)
	}
	if _, err := a.PasswordLogin(ctx, "nobody", "x"); err != app.ErrBadLogin {
		t.Fatal("unknown login")
	}
}

func TestLoginThroughBot(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	code, link, err := a.LoginCode(ctx)
	if err != nil || !strings.Contains(link, "start=login_"+code) {
		t.Fatal(err, link)
	}
	if id, alive, _ := a.PollLogin(ctx, code); id != 0 || !alive {
		t.Fatal("unconfirmed code must wait")
	}
	a.AskLogin(ctx, 1, code)
	if !strings.Contains(f.last(1), "Войти во «Вдвоём» в браузере?") {
		t.Fatal(f.last(1))
	}
	if err := a.ConfirmLogin(ctx, v, code); err != nil {
		t.Fatal(err)
	}
	if id, _, _ := a.PollLogin(ctx, code); id != v.Me.ID {
		t.Fatal("confirmed code must log in")
	}
	if id, _, _ := a.PollLogin(ctx, code); id != 0 {
		t.Fatal("code works once")
	}
	c2, _, _ := a.LoginCode(ctx)
	ck.t = ck.t.Add(6 * time.Minute)
	if err := a.ConfirmLogin(ctx, v, c2); err == nil {
		t.Fatal("expired code must fail")
	}
	tok, err := a.NewSession(ctx, v.Me.ID, "telegram", "test")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := a.S.SessionMember(ctx, tok, a.Now()); err != nil || id != v.Me.ID {
		t.Fatal("session")
	}
	if _, err := a.S.SessionMember(ctx, tok, a.Now().Add(31*24*time.Hour)); err == nil {
		t.Fatal("session must expire")
	}
}

func TestStatsCalendarAndDays(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	res, _ := a.AddIdea(ctx, v, "Ужин в ресторане")
	a.SetWish(ctx, v, res.Idea.ID, "very")
	a.SetWish(ctx, n, res.Idea.ID, "want")
	days, ok := app.DaysFrom("2026-11-20", ck.t, v.Loc())
	if !ok || days[2] != "2026-11-22" {
		t.Fatal(days)
	}
	if _, ok := app.DaysFrom("2026-01-01", ck.t, v.Loc()); ok {
		t.Fatal("past day must be refused")
	}
	r, err := a.GoOn(ctx, v, days)
	if err != nil {
		t.Fatal(err)
	}
	k := "2026-11-21:2"
	a.SubmitSlots(ctx, v, r.ID, []string{k, "2026-11-14:2"})
	r, _ = a.SubmitSlots(ctx, n, r.ID, []string{k})
	if r.State != "vote" {
		t.Fatalf("custom days: %s", r.State)
	}
	a.Vote(ctx, v, r.ID, r.Triple[0])
	a.Vote(ctx, n, r.ID, r.Triple[0])
	a.Confirm(ctx, v, r.ID)
	a.Confirm(ctx, n, r.ID)
	ev, _ := a.Calendar(ctx, v, "2026-11")
	if len(ev) != 1 || ev[0].Date != "2026-11-21" || ev[0].Kind != "planned" {
		t.Fatalf("calendar %+v", ev)
	}
	st, _ := a.Stats(ctx, v)
	if st.Match != 100 || st.BothWant != 1 || st.Months[5].Planned != 1 {
		t.Fatalf("stats %+v", st)
	}
}
