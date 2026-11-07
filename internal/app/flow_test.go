package app_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"vdvoem/internal/ai"
	"vdvoem/internal/app"
	"vdvoem/internal/assistant"
	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

type sent struct {
	chat int64
	text string
	mk   *tg.Markup
}

type fakeTG struct {
	mu   sync.Mutex
	msgs []sent
	docs int
}

func (f *fakeTG) Send(_ context.Context, chat int64, text string, o tg.SendOpts) (tg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, sent{chat, text, o.Markup})
	return tg.Message{MessageID: int64(len(f.msgs))}, nil
}
func (f *fakeTG) SendDocument(context.Context, int64, string, []byte, string) error {
	f.mu.Lock()
	f.docs++
	f.mu.Unlock()
	return nil
}
func (f *fakeTG) SendPhoto(context.Context, int64, string, string) error { return nil }

func (f *fakeTG) last(chat int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.msgs) - 1; i >= 0; i-- {
		if f.msgs[i].chat == chat {
			return f.msgs[i].text
		}
	}
	return ""
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T) (*app.App, *fakeTG, *clock) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	msk, _ := time.LoadLocation("Europe/Moscow")
	ck := &clock{time.Date(2026, 11, 11, 12, 0, 0, 0, msk)} // Wednesday
	f := &fakeTG{}
	a := &app.App{S: st, TG: f, AI: &ai.Client{}, Now: ck.now, PublicURL: "https://x.test", BotName: "vdvoem_bot", NoKick: true,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return a, f, ck
}

func pair(t *testing.T, a *app.App) (app.Couple, app.Couple) {
	ctx := context.Background()
	_, link, err := a.Start(ctx, app.Profile{TGID: 1, ChatID: 1, Name: "Вова"}, "")
	if err != nil || !strings.Contains(link, "inv_") {
		t.Fatalf("start: %v %q", err, link)
	}
	code := link[strings.Index(link, "inv_"):]
	if _, _, err := a.Start(ctx, app.Profile{TGID: 2, ChatID: 2, Name: "Настя"}, code); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Start(ctx, app.Profile{TGID: 3, ChatID: 3, Name: "Третий"}, code); err == nil {
		t.Fatal("invite must work once")
	}
	v, _ := a.Couple(ctx, 1)
	n, _ := a.Couple(ctx, 2)
	if v.Partner == nil || n.Partner == nil || v.Space.ID != n.Space.ID {
		t.Fatal("not paired")
	}
	return v, n
}

func TestFullFlow(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)

	var ids []int64
	for _, txt := range []string{"Ужин в грузинском ресторане", "Прогулка по набережной", "Театр на Таганке", "Каток в парке"} {
		res, err := a.AddIdea(ctx, v, txt)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.Idea.ID)
	}
	if res, _ := a.AddIdea(ctx, n, "ужин в грузинском ресторане"); res.Existing == nil {
		t.Fatal("exact duplicate must be detected")
	}
	a.Tick(ctx) // runs parse jobs → idea cards
	if !strings.Contains(f.last(1), "Сохранил в копилку") {
		t.Fatalf("no idea card: %q", f.last(1))
	}
	for _, id := range ids[:2] {
		a.SetWish(ctx, v, id, domain.WishVery)
		a.SetWish(ctx, n, id, domain.WishWant)
	}
	a.SetWish(ctx, n, ids[3], domain.WishSkip)

	r, err := a.Go(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	days := app.SlotDays(ck.t, v.Loc())
	fri := days[0].Format("2006-01-02")
	sat := days[1].Format("2006-01-02")
	if days[0].Weekday() != time.Friday {
		t.Fatalf("slot days start %v", days[0].Weekday())
	}
	if _, err := a.SubmitSlots(ctx, v, r.ID, []string{fri + ":2", sat + ":1", sat + ":2"}); err != nil {
		t.Fatal(err)
	}
	r, err = a.SubmitSlots(ctx, n, r.ID, []string{sat + ":2", "2030-01-01:2"}) // a foreign day is ignored
	if err != nil {
		t.Fatal(err)
	}
	if r.State != domain.StateVote || len(r.Triple) == 0 {
		t.Fatalf("state %s triple %v", r.State, r.Triple)
	}
	for _, id := range r.Triple {
		if id == ids[3] {
			t.Fatal("skipped idea in triple")
		}
	}
	fav := r.Triple[0]
	if _, err := a.Vote(ctx, v, r.ID, fav); err != nil {
		t.Fatal(err)
	}
	r, err = a.Vote(ctx, n, r.ID, fav)
	if err != nil || r.State != domain.StateConfirm {
		t.Fatalf("vote: %v %s", err, r.State)
	}
	if r.StartsAt.In(v.Loc()).Weekday() != time.Saturday || r.StartsAt.In(v.Loc()).Hour() < 18 {
		t.Fatalf("time %v", r.StartsAt)
	}
	r, _ = a.Confirm(ctx, v, r.ID)
	if r.State != domain.StateConfirm {
		t.Fatal("one confirmation must not plan")
	}
	r, _ = a.Confirm(ctx, n, r.ID)
	if r.State != domain.StatePlanned || f.docs != 2 {
		t.Fatalf("planned? %s docs %d", r.State, f.docs)
	}

	// 2h before → reminder; +3h after end → «Сходили?»
	ck.t = r.StartsAt.Add(-2*time.Hour + time.Minute)
	a.Tick(ctx)
	if !strings.Contains(f.last(2), "Через 2 часа") {
		t.Fatalf("no reminder: %q", f.last(2))
	}
	ck.t = r.EndsAt.Add(3*time.Hour + time.Minute)
	a.Tick(ctx)
	if !strings.Contains(f.last(1), "Сходили?") && !strings.Contains(f.last(1), "Как прошло") {
		// quiet hours may shift it to the morning
		ck.t = ck.t.Add(12 * time.Hour)
		a.Tick(ctx)
	}
	if !strings.Contains(f.last(1), "Как прошло") {
		t.Fatalf("no after-question: %q", f.last(1))
	}
	if _, err := a.Done(ctx, v, r.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Again(ctx, v, r.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	a.Again(ctx, n, r.ID, "yes")
	i, _ := a.S.Idea(ctx, v.Space.ID, fav)
	if i.AgainYes == 0 || i.LastMetAt == nil {
		t.Fatalf("after meeting not stored: %+v", i.Idea)
	}
}

func TestSilenceExpires(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	r, _ := a.Go(ctx, v)
	ck.t = ck.t.Add(13 * time.Hour)
	a.Tick(ctx)
	ck.t = ck.t.Add(36 * time.Hour)
	a.Tick(ctx)
	got, _ := a.S.Round(ctx, v.Space.ID, r.ID)
	if got.State != domain.StateExpired {
		t.Fatalf("state %s", got.State)
	}
	if !strings.Contains(f.last(2), "Выбор закрылся") {
		t.Fatalf("%q", f.last(2))
	}
}

func TestSuggestAndGoWith(t *testing.T) {
	a, f, _ := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	if _, err := a.Suggest(ctx, v, domain.SuggestBest, nil); err != app.ErrNoIdeas {
		t.Fatalf("empty копилка: %v", err)
	}
	res, _ := a.AddIdea(ctx, v, "Музей космонавтики")
	a.AddIdea(ctx, v, "Кофе на Патриках")
	a.SetWish(ctx, n, res.Idea.ID, domain.WishVery)
	s, err := a.Suggest(ctx, v, domain.SuggestBest, nil)
	if err != nil || s.Idea.ID != res.Idea.ID {
		t.Fatalf("suggest: %v %d", err, s.Idea.ID)
	}
	if s2, err := a.Suggest(ctx, v, domain.SuggestBest, []int64{s.Idea.ID}); err != nil || s2.Idea.ID == s.Idea.ID {
		t.Fatal("«Другую» must exclude")
	}
	r, err := a.GoWith(ctx, n, res.Idea.ID)
	if err != nil || r.Chosen != res.Idea.ID {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(1), "Настя хочет") {
		t.Fatalf("%q", f.last(1))
	}
	days := app.SlotDays(a.Now(), v.Loc())
	k := days[1].Format("2006-01-02") + ":1"
	a.SubmitSlots(ctx, v, r.ID, []string{k})
	r, _ = a.SubmitSlots(ctx, n, r.ID, []string{k})
	if r.State != domain.StateConfirm || r.Chosen != res.Idea.ID {
		t.Fatalf("with preset idea vote is skipped: %s", r.State)
	}
}

func TestAssistantRules(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	a.AddIdea(ctx, v, "Ужин в ресторане")
	rep, err := as.Ask(ctx, v, "Куда сходить в субботу вечером?")
	if err != nil || len(rep.Cards) == 0 || len(rep.Actions) == 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	got, _ := as.Act(ctx, v, rep.Actions[0].ID)
	if !strings.Contains(got.Text, "Отправил") {
		t.Fatalf("act: %q", got.Text)
	}
	again, _ := as.Act(ctx, v, rep.Actions[0].ID)
	if !strings.Contains(again.Text, "уже сработала") {
		t.Fatal("action must work once")
	}
	add, _ := as.Ask(ctx, v, "добавь: каток в Парке Горького")
	if len(add.Actions) != 1 {
		t.Fatalf("%+v", add)
	}
	ideas, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaActive)
	if len(ideas) != 1 {
		t.Fatal("assistant must not add without a press")
	}
}

// Free time is not limited to the three suggested days: any day up to the horizon works, past and far days are dropped.
func TestSlotsAnyDay(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	a.AddIdea(ctx, v, "Ужин в ресторане")
	r, err := a.Go(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	far := ck.t.AddDate(0, 0, 20).Format("2006-01-02") + ":2"
	today := ck.t.Format("2006-01-02")
	r, _ = a.SubmitSlots(ctx, v, r.ID, []string{far, today + ":0", ck.t.AddDate(0, 0, -1).Format("2006-01-02") + ":2", ck.t.AddDate(0, 0, 90).Format("2006-01-02") + ":1"})
	if len(r.SlotsA) != 1 || r.SlotsA[0].Key() != far {
		t.Fatalf("only the far evening is valid (today's morning has passed at 12:00): %+v", r.SlotsA)
	}
	r, _ = a.SubmitSlots(ctx, n, r.ID, []string{far, today + ":2"})
	if r.State != domain.StateVote || len(r.Triple) == 0 {
		t.Fatalf("a common evening three weeks ahead must start the vote: %s", r.State)
	}
}
