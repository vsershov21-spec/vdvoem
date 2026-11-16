package app_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

	docChat int64 // the last file sent: to whom, its name and what is in it
	docName string
	doc     []byte
}

func (f *fakeTG) Send(_ context.Context, chat int64, text string, o tg.SendOpts) (tg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, sent{chat, text, o.Markup})
	return tg.Message{MessageID: int64(len(f.msgs))}, nil
}
func (f *fakeTG) SendDocument(_ context.Context, chat int64, name string, data []byte, _ string) error {
	f.mu.Lock()
	f.docs++
	f.docChat, f.docName, f.doc = chat, name, append([]byte(nil), data...)
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
	// The new interface is the default. The tests of the bot's previous flow (slots, three variants, «копилка»)
	// are about people whose app reported the old screens (?v2=0), so the pair says so explicitly; a test of
	// the new flow turns it on with SetUI(…, 2), and TestNewInterfaceIsDefault covers someone who said nothing.
	oldUI(t, a, v.Me.ID, n.Me.ID)
	return v, n
}

// oldUI: these members' app has reported the old screens
func oldUI(t *testing.T, a *app.App, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if err := a.SetUI(context.Background(), id, 1); err != nil || a.UsesV2(context.Background(), id) {
			t.Fatalf("the old screens report themselves: %v", err)
		}
	}
}

// Someone the app has said nothing about is a person of the new interface: the bot writes in its words, and
// their invitation opens the «Приглашение» screen. Only an explicit report of the old screens gives the old
// flow back, and the new shell takes it again.
func TestNewInterfaceIsDefault(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	v, link, err := a.Start(ctx, app.Profile{TGID: 1, ChatID: 1, Name: "Вова"}, "")
	if err != nil {
		t.Fatal(err)
	}
	code := app.InviteCode(link)
	if !a.UsesV2(ctx, v.Me.ID) || !a.InviteV2(ctx, code) {
		t.Fatal("nothing reported: the new interface")
	}
	if got := a.WordsFor(ctx, 1, "Это уже есть в копилке."); got != "Это уже есть в идеях." {
		t.Fatalf("the words of the new interface by default: %q", got)
	}
	oldUI(t, a, v.Me.ID)
	if a.InviteV2(ctx, code) {
		t.Fatal("the old screens reported: the link joins at once, as before")
	}
	if got := a.WordsFor(ctx, 1, "Это уже есть в копилке."); got != "Это уже есть в копилке." {
		t.Fatalf("the old screens keep their word: %q", got)
	}
	if err := a.SetUI(ctx, v.Me.ID, 2); err != nil || !a.UsesV2(ctx, v.Me.ID) || !a.InviteV2(ctx, code) {
		t.Fatalf("the new shell reports itself: %v", err)
	}
	if a.InviteV2(ctx, "NOSUCHCODE") {
		t.Fatal("nothing is known about an unknown link")
	}
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
	// the same idea at a common time is the decision: planned at once, no second «подтверди»
	if err != nil || r.State != domain.StatePlanned {
		t.Fatalf("vote: %v %s", err, r.State)
	}
	if r.StartsAt.In(v.Loc()).Weekday() != time.Saturday || r.StartsAt.In(v.Loc()).Hour() < 18 {
		t.Fatalf("time %v", r.StartsAt)
	}
	r, _ = a.Confirm(ctx, v, r.ID) // a late «Подтвердить» from an old message changes nothing
	if r.State != domain.StatePlanned || f.count(1, "Мы идём") != 1 || f.count(2, "Мы идём") != 1 {
		t.Fatalf("planned? %s", r.State)
	}
	// «в календарь» is a link that opens the event, not a file to share
	if mk := f.lastMarkup(1); !strings.Contains(mk, "/cal/") || !strings.Contains(mk, "calendar.google.com") {
		t.Fatalf("calendar buttons: %s", mk)
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
	if r.State != domain.StatePlanned || r.Chosen != res.Idea.ID {
		t.Fatalf("with preset idea vote is skipped and the common time plans it: %s", r.State)
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
	ck.t = ck.t.Add(2 * time.Hour) // 14:00: the morning (9–13) is over
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
		t.Fatalf("only the far evening is valid (today's morning is over at 14:00): %+v", r.SlotsA)
	}
	r, _ = a.SubmitSlots(ctx, n, r.ID, []string{far, today + ":2"})
	if r.State != domain.StateVote || len(r.Triple) == 0 {
		t.Fatalf("a common evening three weeks ahead must start the vote: %s", r.State)
	}
}

func TestNameIns(t *testing.T) {
	for in, want := range map[string]string{"Настя": "Настей", "Вова": "Вовой", "Саша": "Сашей", "Иван": "Иваном", "Андрей": "Андреем", "Игорь": "Игорем", "Kate": "Kate", "Маша-Мила": "Маша-Мила"} {
		if got := app.NameIns(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

// «А потом?»: museum, then dinner — proposed by one, confirmed by the other, in the calendar as a second event.
func TestExtras(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	mus, _ := a.AddIdea(ctx, v, "Музей космонавтики")
	din, _ := a.AddIdea(ctx, v, "Ужин в грузинском ресторане")
	a.SetWish(ctx, v, din.Idea.ID, domain.WishVery)
	a.SetWish(ctx, n, din.Idea.ID, domain.WishWant)
	r, err := a.GoWith(ctx, v, mus.Idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProposeExtra(ctx, v, r.ID, din.Idea.ID, ""); !errors.Is(err, app.ErrExtraState) {
		t.Fatalf("no next stop before the main one is matched: %v", err)
	}
	day := app.SlotDays(ck.t, v.Loc())[1].Format("2006-01-02")
	a.SubmitSlots(ctx, v, r.ID, []string{day + ":1", day + ":2"})
	r, _ = a.SubmitSlots(ctx, n, r.ID, []string{day + ":1", day + ":2"})
	if r.State != domain.StatePlanned {
		t.Fatalf("state %s", r.State)
	}
	sug := a.SuggestExtras(ctx, v, r)
	if len(sug) == 0 || sug[0].ID != din.Idea.ID {
		t.Fatalf("dinner both want comes first after a museum: %+v", sug)
	}
	e, err := a.ProposeExtra(ctx, v, r.ID, din.Idea.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(2), "предлагает после «Музей космонавтики»") {
		t.Fatalf("partner asked: %q", f.last(2))
	}
	if _, err := a.AnswerExtra(ctx, v, e.ID, true); !errors.Is(err, app.ErrExtraOwn) {
		t.Fatal("the proposer cannot confirm their own stop")
	}
	route := a.Route(ctx, v, r)
	if len(route) != 1 || !route[0].Start.After(r.EndsAt) {
		t.Fatalf("route: %+v", route)
	}
	a.AnswerExtra(ctx, n, e.ID, true)
	a.Confirm(ctx, v, r.ID)
	r, _ = a.Confirm(ctx, n, r.ID)
	ics := string(a.ICS(ctx, v, r, 0, false))
	if strings.Count(ics, "BEGIN:VEVENT") != 2 || !strings.Contains(ics, "SUMMARY:Ужин в грузинском ресторане") {
		t.Fatalf("calendar has both stops:\n%s", ics)
	}
	if !strings.Contains(f.last(1), "Потом:") {
		t.Fatalf("the route update shows the stops: %q", f.last(1))
	}
	// text proposal creates the idea; the limit is two stops
	if _, err := a.ProposeExtra(ctx, n, r.ID, 0, "Прогулка по Арбату"); err != nil {
		t.Fatal(err)
	}
	// a stop made inside the route is not an idea of the bank: no card in the deck, nothing for the assistant
	all, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	for _, i := range all {
		if i.Title == "Прогулка по Арбату" {
			t.Fatalf("a route stop leaked into the bank: %+v", i.Title)
		}
	}
	if _, err := a.ProposeExtra(ctx, n, r.ID, 0, "Кино"); !errors.Is(err, app.ErrExtraLimit) {
		t.Fatalf("limit: %v", err)
	}
}

// Скаут: a weekly draft — the best five to the bank and the bot, the rest wait; announcements months ahead
// speak when tickets go on sale; each person's taste re-ranks; nobody-wanted drafts vanish in two weeks.
func TestScout(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	// Вова loves theatre; Настя — Georgian food; nobody wants stand-up
	for _, x := range []struct {
		title, cat string
		w          domain.Wish
		who        app.Couple
	}{{"Спектакль в театре Вахтангова", "culture", domain.WishVery, v}, {"Театр на Таганке", "culture", domain.WishWant, v},
		{"Стендап вечер", "other", domain.WishSkip, v}, {"Стендап открытый микрофон", "other", domain.WishSkip, n},
		{"Хинкальная на Арбате", "food", domain.WishVery, n}, {"Грузинский ресторан", "food", domain.WishWant, n}} {
		r, _ := a.AddIdea(ctx, v, x.title)
		i := r.Idea
		i.Category = x.cat
		a.S.UpdateIdea(ctx, i, ck.t)
		a.SetWish(ctx, x.who, i.ID, x.w)
	}
	sat := ck.t.AddDate(0, 0, 3).Format("2006-01-02") + "T19:00"
	later := ck.t.AddDate(0, 2, 0).Format("2006-01-02") + "T19:00"
	onSale := ck.t.AddDate(0, 0, 5).Format("2006-01-02") + "T10:00"
	g, _ := a.AddGoal(ctx, n, "театр в январе")
	var picks []app.ScoutPick
	for k, p := range []struct{ t, c string }{{"Большой стендап-концерт", "other"}, {"Премьера в театре Маяковского", "culture"},
		{"Фестиваль грузинской кухни", "food"}, {"Прогулка по ВДНХ", "walk"}, {"Квиз в баре", "other"}, {"Выставка в Пушкинском", "culture"},
		{"Каток на ВДНХ", "sport"}, {"Джазовый концерт", "culture"}} {
		picks = append(picks, app.ScoutPick{Title: p.t, Category: p.c, Starts: sat, PriceRub: 2000 + int64(k)*100, Why: "под ваши вкусы", Score: 6, URL: fmt.Sprintf("https://afisha.example/%d", k)})
	}
	picks = append(picks, app.ScoutPick{Title: "Гамлет в театре Наций", Category: "culture", Starts: later, OnSale: onSale, Horizon: "later", Score: 8, GoalID: g.ID, URL: "https://afisha.example/hamlet"},
		app.ScoutPick{Title: "Уже прошло", Starts: ck.t.AddDate(0, 0, -2).Format("2006-01-02")})
	res, err := a.ScoutPicks(ctx, v.Space.ID, picks)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 9 || res.Promoted != app.ScoutTop || res.Skipped != 1 {
		t.Fatalf("result %+v", res)
	}
	digest := f.last(2)
	if !strings.Contains(digest, "Скаут нашёл") || !strings.Contains(digest, "Анонсы на потом") || !strings.Contains(digest, "🎯") {
		t.Fatalf("digest: %q", digest)
	}
	if strings.Index(digest, "стендап") >= 0 && strings.Index(digest, "стендап") < strings.Index(digest, "Анонсы") {
		t.Fatalf("taste must keep stand-up out of the top five: %q", digest)
	}
	// duplicates are skipped next time
	if r2, _ := a.ScoutPicks(ctx, v.Space.ID, picks[:2]); r2.Added != 0 {
		t.Fatalf("dupes: %+v", r2)
	}
	// per-person taste: Вова → theatre, Настя → food
	tt := a.Tastes(ctx, v)
	all, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	var theatre, food store.Idea
	for _, i := range all {
		switch i.Title {
		case "Премьера в театре Маяковского":
			theatre = i
		case "Фестиваль грузинской кухни":
			food = i
		}
	}
	if m := tt.Of(theatre); m.Me <= m.Partner {
		t.Fatalf("theatre: Вова %.2f vs Настя %.2f", m.Me, m.Partner)
	}
	if m := tt.Of(food); m.Partner <= m.Me {
		t.Fatalf("food: Настя %.2f vs Вова %.2f", m.Partner, m.Me)
	}
	// tickets on sale → both hear it
	ck.t = ck.t.AddDate(0, 0, 5).Add(time.Hour)
	a.Tick(ctx)
	heard := false
	for _, m := range f.msgs {
		if m.chat == 1 && strings.Contains(m.text, "Билеты на <b>Гамлет") {
			heard = true
		}
	}
	if !heard {
		t.Fatalf("on sale: %q", f.last(1))
	}
	// two weeks later the drafts nobody touched are gone; the announcement waits for its date
	ck.t = ck.t.AddDate(0, 0, 10)
	gone := a.ScoutCleanup(ctx, v.Space.ID)
	left, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaDraft)
	if gone == 0 || len(left) != 1 || left[0].Horizon != "later" {
		t.Fatalf("cleanup %d, left %+v", gone, left)
	}
	// memory is personal
	a.Remember(ctx, v, "не люблю стендап")
	pv, _ := a.Profiles(ctx, v)
	pn, _ := a.Profiles(ctx, n)
	if len(pv.Said) != 1 || len(pn.Said) != 0 || len(pv.Likes) == 0 {
		t.Fatalf("profiles: %+v / %+v", pv, pn)
	}
}

// The assistant v2: rules understand, the server answers from data — price, mood, booking into the calendar, follow-ups.
func TestAssistantSeamless(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	sat := ck.t.AddDate(0, 0, 3).Format("2006-01-02")
	_, err := a.ScoutPicks(ctx, v.Space.ID, []app.ScoutPick{
		{Title: "Джазовый концерт в Козловском клубе", Category: "culture", Starts: sat + "T19:00", PriceRub: 3000, Score: 9, URL: "https://afisha.example/jazz"},
		{Title: "СПА-вечер в банях", Category: "other", Starts: sat, PriceRub: 7000, Score: 7, URL: "https://afisha.example/spa"},
		{Title: "Выставка импрессионистов", Category: "culture", Starts: sat, PriceRub: 1200, Score: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	// price from the data, with a link
	rep, _ := as.Ask(ctx, v, "сколько стоит джазовый концерт?")
	if rep.Intent == nil || rep.Intent.I != "price" || !strings.Contains(rep.Text, "₽") || !strings.Contains(rep.Text, "afisha.example/jazz") {
		t.Fatalf("price: %+v", rep)
	}
	// unknown thing: a search link and a question for the scout
	rep, _ = as.Ask(ctx, v, "сколько стоят билеты на Лебединое озеро")
	if !strings.Contains(rep.Text, "yandex.ru/search") {
		t.Fatalf("unknown price: %q", rep.Text)
	}
	if q, _ := a.S.OpenQuestions(ctx, v.Space.ID); len(q) != 1 {
		t.Fatalf("scout question: %+v", q)
	}
	// mood: the spa first
	rep, _ = as.Ask(ctx, v, "хотим почилить в субботу")
	spa := false // calm places come up: the spa (and a quiet exhibition is fine too); the loud jazz club is not first
	for _, cd := range rep.Cards {
		spa = spa || strings.Contains(cd.Title, "СПА")
	}
	if !spa || strings.Contains(rep.Cards[0].Title, "Джаз") {
		t.Fatalf("chill: %+v", rep.Cards)
	}
	// follow-up keeps the day, lowers the price
	rep, _ = as.Ask(ctx, v, "а подешевле?")
	if rep.Intent == nil || rep.Intent.Date == "" || rep.Intent.Max == 0 {
		t.Fatalf("follow-up: %+v", rep.Intent)
	}
	// book a fixed-time event: no time picking, the partner confirms, both get it in the calendar
	rep, _ = as.Ask(ctx, v, "запиши нас на джазовый концерт")
	if !strings.Contains(rep.Text, "Позвал") {
		t.Fatalf("book: %q", rep.Text)
	}
	rd, err := a.S.OpenRound(ctx, v.Space.ID)
	if err != nil || rd.State != domain.StateConfirm || !(rd.ConfirmA || rd.ConfirmB) || rd.StartsAt.In(v.Loc()).Hour() != 19 {
		t.Fatalf("round: %+v %v", rd, err)
	}
	if !strings.Contains(f.last(2), "зовёт") {
		t.Fatalf("partner: %q", f.last(2))
	}
	if _, err := a.Confirm(ctx, n, rd.ID); err != nil {
		t.Fatal(err)
	}
	if rd, _ = a.S.Round(ctx, v.Space.ID, rd.ID); rd.State != domain.StatePlanned {
		t.Fatalf("after confirm: %s", rd.State)
	}
	// the scout answers a question → the person hears it in the bot
	qs, _ := a.S.OpenQuestions(ctx, v.Space.ID)
	if k, err := a.ScoutAnswers(ctx, v.Space.ID, []app.ScoutAnswer{{ID: qs[0].ID, Text: "От 2500 ₽ за билет", PriceRub: 5000, URL: "https://tickets.example/swan"}}); err != nil || k != 1 {
		t.Fatalf("answers %d %v", k, err)
	}
	if !strings.Contains(f.last(1), "Скаут ответил") {
		t.Fatalf("answer: %q", f.last(1))
	}
}

// A post forwarded from a channel: the model's events are checked against the text, the true ones land in «Афиша».
func TestChannelPost(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"events\":[{\"title\":\"Вишнёвый сад в Современнике\",\"date\":\"2026-11-15\",\"time\":\"19:00\",\"price\":900,\"category\":\"culture\"},{\"title\":\"Гамлет в МХТ\",\"date\":\"2026-11-20\",\"price\":500}]}"}}]}`))
	}))
	defer srv.Close()
	a.AI = &ai.Client{URL: srv.URL, Model: "fake"}
	post := app.ChannelPost{From: "Билеты дёшево", Text: "🎭 «Вишнёвый сад» в Современнике — 15 ноября, 19:00. Билеты от 900 ₽, пишите в личку"}
	if err := a.Repost(ctx, v, 1, post); err != nil {
		t.Fatal(err)
	}
	a.Tick(ctx)
	got := f.last(1)
	if !strings.Contains(got, "1 событие") || !strings.Contains(got, "Вишнёвый сад") || strings.Contains(got, "Гамлет") || !strings.Contains(got, "800") {
		t.Fatalf("reply: %q", got)
	}
	all, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaDraft)
	if len(all) != 1 || all[0].Source != "channel" || all[0].StartsAt == nil || all[0].StartsAt.In(v.Loc()).Hour() != 19 {
		t.Fatalf("ideas: %+v", all)
	}
	// the same post again: nothing new
	_ = a.Repost(ctx, v, 1, app.ChannelPost{From: "Билеты дёшево", Text: post.Text + "\nПишите в личку!"})
	ck.t = ck.t.Add(time.Minute)
	a.Tick(ctx)
	if !strings.Contains(f.last(1), "уже есть") {
		t.Fatalf("dupe: %q", f.last(1))
	}
}

// «Расскажи про …»: facts from the data, a verdict for each of the two with reasons.
func TestAboutAnswer(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	// Вова loves comedies, Настя skips them
	for _, title := range []string{"Ревизор", "Слуга двух господ", "Женитьба", "Безымянная звезда", "Двенадцатая ночь", "Тартюф", "Мнимый больной", "Дядюшкин сон", "Укрощение строптивой"} {
		r, _ := a.AddIdea(ctx, v, title)
		i := r.Idea
		i.Category, i.Info.Genres = "culture", []string{"комедия"}
		a.S.UpdateIdea(ctx, i, ck.t)
		a.SetWish(ctx, v, i.ID, domain.WishVery)
		a.SetWish(ctx, n, i.ID, domain.WishSkip)
	}
	sat := ck.t.AddDate(0, 0, 3).Format("2006-01-02")
	if _, err := a.ScoutPicks(ctx, v.Space.ID, []app.ScoutPick{{Title: "Тёща с сюрпризом", Category: "culture", Starts: sat + "T19:30", PriceRub: 4000, Score: 7,
		About: "Молодая пара хочет обзавестись ребёнком, а тёща готовит сюрприз.", Genres: []string{"комедия"}, Cast: []string{"Иван Иванов"}, Age: "16+"}}); err != nil {
		t.Fatal(err)
	}
	rep, _ := as.Ask(ctx, v, "расскажи про Тёщу с сюрпризом")
	for _, want := range []string{"Комедия", "16+", "В ролях: Иван Иванов", "Молодая пара", "Подойдёт ли вам", "Тебе — очень подходит", "Настя — вряд ли"} {
		if !strings.Contains(rep.Text, want) {
			t.Fatalf("no %q in %q", want, rep.Text)
		}
	}
	if len(rep.Actions) == 0 || !strings.HasPrefix(rep.Actions[0].Label, "Идём") {
		t.Fatalf("actions: %+v", rep.Actions)
	}
}

// Time is agreed in the open: the partner gets the proposal with one-tap answers; no overlap is not a dead end.
func TestTimeTalk(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	for _, x := range []string{"Ужин в ресторане", "Кино"} {
		a.AddIdea(ctx, v, x)
	}
	rd, err := a.Go(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	days := app.SlotDays(ck.t, v.Loc())
	k1 := days[1].Format("2006-01-02") + ":2"
	if _, err := a.SubmitSlots(ctx, v, rd.ID, []string{k1}); err != nil {
		t.Fatal(err)
	}
	if got := f.last(2); !strings.Contains(got, "предлагает время") {
		t.Fatalf("proposal: %q", got)
	}
	if _, err := a.SubmitSlots(ctx, n, rd.ID, []string{days[2].Format("2006-01-02") + ":0"}); err != nil {
		t.Fatal(err)
	}
	if got := f.last(2); !strings.Contains(got, "Пока не совпало") {
		t.Fatalf("no overlap: %q", got)
	}
	r2, err := a.AcceptSlot(ctx, n, rd.ID, k1)
	if err != nil || r2.State == domain.StateTime {
		t.Fatalf("accept: %s %v", r2.State, err)
	}
	// the feed: personal, the meeting in it once planned
	tok, _ := a.FeedToken(ctx, v, false)
	if b, err := a.Feed(ctx, tok); err != nil || !strings.Contains(string(b), "BEGIN:VCALENDAR") {
		t.Fatalf("feed: %v", err)
	}
	if a.Subscribed(ctx, n.Me.ID) || !a.Subscribed(ctx, v.Me.ID) {
		t.Fatal("subscribed is per person and set by reading the feed")
	}
	if _, err := a.Feed(ctx, strings.Repeat("0", 36)); err != app.ErrNoFeed {
		t.Fatal("unknown token must not open a feed")
	}
}

func (f *fakeTG) lastMarkup(chat int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.msgs) - 1; i >= 0; i-- {
		if f.msgs[i].chat == chat && f.msgs[i].mk != nil {
			var b strings.Builder
			for _, row := range f.msgs[i].mk.InlineKeyboard {
				for _, x := range row {
					b.WriteString(x.Text + " " + x.URL + " " + x.CallbackData + "\n")
				}
			}
			return b.String()
		}
	}
	return ""
}

func (f *fakeTG) count(chat int64, sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.msgs {
		if m.chat == chat && strings.Contains(m.text, sub) {
			n++
		}
	}
	return n
}

// «важно»: Настя asks, Вова is reminded until he answers; yes plans the meeting, no tells her
func TestUrgent(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	res, _ := a.AddIdea(ctx, n, "Спектакль Чайка во МХТ")
	id := res.Idea.ID
	if _, err := a.MarkUrgent(ctx, n, id, "Билеты разбирают!"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(1), "Настя просит ответить") || !strings.Contains(f.last(1), "Билеты разбирают") {
		t.Fatalf("alert: %q", f.last(1))
	}
	if i, _ := a.S.Idea(ctx, n.Space.ID, id); i.WishB != domain.WishVery && i.WishA != domain.WishVery {
		t.Fatal("asking marks it «очень» for the author")
	}
	if _, err := a.AnswerUrgent(ctx, n, id, "yes"); !errors.Is(err, app.ErrNotYours) {
		t.Fatalf("the author cannot answer her own request: %v", err)
	}
	ck.t = ck.t.Add(30 * time.Minute)
	a.Tick(ctx)
	if c := f.count(1, "Напоминаю: Настя ждёт"); c != 0 {
		t.Fatal("too early")
	}
	ck.t = ck.t.Add(31 * time.Minute)
	a.Tick(ctx)
	if c := f.count(1, "Напоминаю: Настя ждёт"); c != 1 {
		t.Fatalf("reminder after an hour: %d", c)
	}
	if msg, err := a.AnswerUrgent(ctx, v, id, "later"); err != nil || !strings.Contains(msg, "через час") {
		t.Fatal(msg, err)
	}
	ck.t = ck.t.Add(50 * time.Minute)
	a.Tick(ctx)
	if c := f.count(1, "Напоминаю: Настя ждёт"); c != 1 {
		t.Fatal("«later» means an hour of quiet")
	}
	// night: nothing until the morning
	msk := v.Loc()
	ck.t = time.Date(2026, 11, 11, 23, 40, 0, 0, msk)
	a.Tick(ctx)
	if c := f.count(1, "Напоминаю: Настя ждёт"); c != 1 {
		t.Fatal("no reminders at night")
	}
	ck.t = time.Date(2026, 11, 12, 9, 5, 0, 0, msk)
	a.Tick(ctx)
	if c := f.count(1, "Напоминаю: Настя ждёт"); c != 2 {
		t.Fatalf("morning reminder: %d", c)
	}
	if got := a.UrgentForMe(ctx, v); len(got) != 1 {
		t.Fatal("home shows it to Вова")
	}
	if got := a.UrgentForMe(ctx, n); len(got) != 0 {
		t.Fatal("not to Настя")
	}
	if _, err := a.AnswerUrgent(ctx, v, id, "yes"); err != nil {
		t.Fatal(err)
	}
	r, err := a.S.OpenRound(ctx, v.Space.ID)
	if err != nil || r.Chosen != id {
		t.Fatalf("yes plans the meeting: %v %d", err, r.Chosen)
	}
	ck.t = ck.t.Add(5 * time.Hour)
	a.Tick(ctx)
	if c := f.count(1, "Напоминаю: Настя ждёт"); c != 2 {
		t.Fatal("answered — no more reminders")
	}
	if _, err := a.AnswerUrgent(ctx, v, id, "no"); !errors.Is(err, app.ErrNotPending) {
		t.Fatal("answered once")
	}
	// no: the author hears at once
	res2, _ := a.AddIdea(ctx, n, "Джаз в Эрмитаже")
	a.MarkUrgent(ctx, n, res2.Idea.ID, "")
	if _, err := a.AnswerUrgent(ctx, v, res2.Idea.ID, "no"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(2), "не сможет") {
		t.Fatalf("%q", f.last(2))
	}
}

// «подборка»: the assistant keeps everything it found as a list; the list goes to the partner in one tap
func TestPickAndBusy(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	var picks []app.ScoutPick
	for k := 0; k < 9; k++ {
		picks = append(picks, app.ScoutPick{Title: fmt.Sprintf("Концерт номер %d", k+1), Category: "culture", Starts: ck.t.AddDate(0, 0, 2+k%4).Format("2006-01-02") + "T19:00", PriceRub: int64(1000 + 300*k), Score: 7})
	}
	if _, err := a.ScoutPicks(ctx, v.Space.ID, picks); err != nil {
		t.Fatal(err)
	}
	rep, _ := as.Ask(ctx, v, "что есть на неделе?")
	if rep.Pick == nil || rep.Pick.N < 5 || len(rep.Cards) >= rep.Pick.N {
		t.Fatalf("a search with more than fits the chat keeps a list: %+v", rep.Pick)
	}
	p, err := a.S.Pick(ctx, v.Space.ID, rep.Pick.ID)
	if err != nil || len(p.IDs) != rep.Pick.N {
		t.Fatal(err)
	}
	if err := a.SendPick(ctx, v, p.ID); err != nil || !strings.Contains(f.last(2), "Вова прислал подборку") {
		t.Fatalf("send: %v %q", err, f.last(2))
	}
	if _, err := a.S.Pick(ctx, n.Space.ID+99, p.ID); err == nil {
		t.Fatal("another space cannot read the list")
	}
	// busy time from Вова's own calendar: work on weekdays 9–18
	day := ck.t.In(v.Loc())
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nDTSTART;TZID=Europe/Moscow:" + day.Format("20060102") + "T090000\r\nDTEND;TZID=Europe/Moscow:" + day.Format("20060102") +
		"T180000\r\nRRULE:FREQ=DAILY;COUNT=30\r\nSUMMARY:Работа\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, ics) }))
	defer srv.Close()
	a.Fetch = srv.Client()
	if _, err := a.ConnectBusy(ctx, v, "http://not-https.example/x.ics"); err == nil {
		t.Fatal("only https links")
	}
	if n, err := a.ConnectBusy(ctx, v, srv.URL+"/basic.ics"); err != nil || n == 0 {
		t.Fatalf("connect: %d %v", n, err)
	}
	busy := a.BusySlots(ctx, v.Me.ID, v.Loc(), 7)
	tomorrow := day.AddDate(0, 0, 1).Format("2006-01-02")
	if !busy[tomorrow+":0"] || !busy[tomorrow+":1"] || busy[tomorrow+":2"] {
		t.Fatalf("work hours busy, evenings free: %v", busy)
	}
	if a.BusySlots(ctx, n.Me.ID, n.Loc(), 7) != nil {
		t.Fatal("Настя has not connected hers")
	}
}

// «Билеты есть» → the one who marked hears it too and may send the tickets; a link goes to the partner and into
// the calendar event.
func TestTicketsFile(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	pl, _ := a.AddIdea(ctx, v, "Планетарий")
	r, err := a.GoWith(ctx, v, pl.Idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	day := app.SlotDays(ck.t, v.Loc())[1].Format("2006-01-02")
	a.SubmitSlots(ctx, v, r.ID, []string{day + ":2"})
	r, _ = a.SubmitSlots(ctx, n, r.ID, []string{day + ":2"})
	if r.State != domain.StatePlanned {
		t.Fatalf("state %s", r.State)
	}
	if err := a.GotTickets(ctx, v, r.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(1), "Пришли сюда") || !strings.Contains(f.last(2), "Покупать не нужно") {
		t.Fatalf("both hear about the tickets: %q / %q", f.last(1), f.last(2))
	}
	if a.AwaitingTickets(ctx, v) != r.ID {
		t.Fatal("waiting for the tickets after the mark")
	}
	if err := a.AttachTickets(ctx, v, r.ID, app.TicketFile{Kind: "link", URL: "https://tickets.example/42"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(2), "https://tickets.example/42") || !strings.Contains(f.last(1), "Билеты сохранены") {
		t.Fatalf("the link to the partner, a receipt to me: %q / %q", f.last(2), f.last(1))
	}
	if a.AwaitingTickets(ctx, v) != 0 {
		t.Fatal("no more waiting once sent")
	}
	if ics := string(a.ICS(ctx, v, r, 0, false)); !strings.Contains(ics, "tickets.example/42") {
		t.Fatalf("the calendar event links the tickets: %s", ics)
	}
}

// a re-sent card fills what the first one lacked (a picture, a description), never overwrites; a page title is no card
func TestScoutRefills(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	sat := ck.t.AddDate(0, 0, 3).Format("2006-01-02") + "T19:00"
	first := []app.ScoutPick{{Title: "Выставка «Хлеб» в Центре «Зотов»", Category: "culture", Starts: sat, URL: "https://zotov.example/bread", Score: 6},
		{Title: "Афиша мероприятий Московского Планетария, цены на официальном сайте", Category: "culture", Starts: sat, URL: "https://planetarium.example/", Score: 6}}
	if res, _ := a.ScoutPicks(ctx, v.Space.ID, first); res.Added != 1 || res.Skipped != 1 {
		t.Fatalf("first: %+v", res)
	}
	again := []app.ScoutPick{{Title: "Выставка «Хлеб» в Центре «Зотов»", Category: "culture", Starts: sat, URL: "https://zotov.example/bread",
		Image: "https://zotov.example/bread.jpg", About: "Выставка о хлебе в культуре.", Genres: []string{"выставка"}}}
	if res, _ := a.ScoutPicks(ctx, v.Space.ID, again); res.Added != 0 {
		t.Fatalf("again added: %+v", res)
	}
	ideas, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	n := 0
	for _, i := range ideas {
		if strings.Contains(i.Title, "Хлеб") {
			n++
			if i.ImageURL != "https://zotov.example/bread.jpg" || i.Info.About == "" {
				t.Fatalf("not refilled: %+v", i)
			}
		}
		if strings.Contains(i.Title, "официальном сайте") {
			t.Fatal("a page title became a card")
		}
	}
	if n != 1 {
		t.Fatalf("%d copies", n)
	}
	// what is there stays
	if res, _ := a.ScoutPicks(ctx, v.Space.ID, []app.ScoutPick{{Title: "Выставка «Хлеб» в Центре «Зотов»", URL: "https://zotov.example/bread", Image: "https://other.example/x.jpg"}}); res.Added != 0 {
		t.Fatal(res)
	}
	ideas, _ = a.S.Ideas(ctx, v.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	for _, i := range ideas {
		if strings.Contains(i.Title, "Хлеб") && i.ImageURL != "https://zotov.example/bread.jpg" {
			t.Fatal("overwritten")
		}
	}
}

// «Кинчик в субботу» with almost no cinema in the data asks the scout by itself; «опять мимо» is not a search —
// the previous request becomes a miss (to label and train on) and goes to the scout.
func TestAssistantMiss(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	sat := ck.t.AddDate(0, 0, 3).Format("2006-01-02") + "T19:00"
	a.ScoutPicks(ctx, v.Space.ID, []app.ScoutPick{{Title: "Стендап в баре", Category: "other", Starts: sat, URL: "https://x.example/1", Score: 6},
		{Title: "Спектакль «Чайка»", Category: "culture", Starts: sat, URL: "https://x.example/2", Score: 6}})
	rep, _ := as.Ask(ctx, v, "кинчик в субботу")
	if !strings.Contains(strings.ToLower(rep.Text), "скаут") {
		t.Fatalf("no cinema → the scout looks by itself: %q", rep.Text)
	}
	miss, _ := as.Ask(ctx, v, "опять мимо")
	if !strings.Contains(miss.Text, "промах") || len(miss.Cards) != 0 {
		t.Fatalf("a miss is not a search: %+v", miss)
	}
	ms, _ := a.S.Misses(ctx, ck.t.Add(-time.Hour))
	if len(ms) != 1 || ms[0].Input != "кинчик в субботу" {
		t.Fatalf("the previous request is the miss: %+v", ms)
	}
	qs, _ := a.S.OpenQuestions(ctx, v.Space.ID)
	n := 0
	for _, q := range qs {
		if strings.Contains(q.Text, "кинчик в субботу") || strings.Contains(q.Text, "кино") {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("the scout must get it: %+v", qs)
	}
}

// «сколько мы потратили» answers with the meetings' sum, not a search
func TestAssistantSpend(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	rep, _ := as.Ask(ctx, v, "сколько мы потратили в этом месяце?")
	if !strings.Contains(rep.Text, "встреч") || len(rep.Cards) != 0 {
		t.Fatalf("a sum, not a search: %+v", rep)
	}
}

// a dry run (/agent/try) answers on the couple's real data and leaves no trace: no chat, no journal, no scout
// question, no memory
func TestAssistantTryLeavesNoTrace(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	a.ScoutPicks(ctx, v.Space.ID, []app.ScoutPick{{Title: "Спектакль «Чайка»", Category: "culture", Starts: ck.t.AddDate(0, 0, 3).Format("2006-01-02") + "T19:00", URL: "https://x.example/3", Score: 6}})
	steps := as.Try(ctx, v, []string{"кинчик в субботу", "не люблю стендап", "следи за Земфирой", "хочу в театр"}, false)
	if len(steps) != 4 || steps[3].Intent.I != "find" || len(steps[3].Cards) == 0 {
		t.Fatalf("steps: %+v", steps)
	}
	day := ck.t.Add(-48 * time.Hour)
	if n, _ := a.S.ChatCountSince(ctx, v.Me.ID, day); n != 0 {
		t.Fatalf("chat written: %d", n)
	}
	if qs, _ := a.S.OpenQuestions(ctx, v.Space.ID); len(qs) != 0 {
		t.Fatalf("scout asked: %+v", qs)
	}
	if ms, _ := a.S.Misses(ctx, day); len(ms) != 0 {
		t.Fatalf("journal written: %+v", ms)
	}
	if mem, _ := a.S.Memories(ctx, v.Space.ID); len(mem) != 0 {
		t.Fatalf("memory written: %+v", mem)
	}
}

// «что сегондя в кино» with no cinema in the data: the typo still reads «today», the answer gives a button to
// today's sessions outside, and never offers ideas of another kind (no «планетарий» for cinema)
func TestAssistantKinoTodayNothing(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	v, _ := pair(t, a)
	as := &assistant.Assistant{App: a, AI: a.AI}
	rep, _ := as.Ask(ctx, v, "что сегондя в кино?")
	link := false
	for _, b := range rep.Actions {
		if b.Kind == "link" && strings.Contains(b.URL, "yandex") {
			link = true
		}
		if strings.Contains(b.Label, "Планетарий") || strings.Contains(b.Label, "Выставка") || strings.Contains(b.Label, "Джаз") {
			t.Fatalf("an idea of another kind offered for cinema: %+v", rep.Actions)
		}
	}
	if !link {
		t.Fatalf("no button to today's sessions: %+v", rep)
	}
	// a cuisine that is not there: said so, no restaurant of another cuisine in its place
	a.ScoutPicks(ctx, v.Space.ID, []app.ScoutPick{{Title: "Ресторан «Китайская грамота»", Category: "food", URL: "https://x.example/kg", Score: 7}})
	it, _ := as.Ask(ctx, v, "лучшие итальянские рестораны")
	if len(it.Cards) != 0 || !strings.Contains(it.Text, "нет") {
		t.Fatalf("no Italian → say so, no other cards: %+v", it)
	}
}

// someone who collected ideas alone and then joins by an invitation keeps them: they move into the pair's
// space with the joiner as the author and their own mark on their side
func TestJoinKeepsSoloIdeas(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	_, link, _ := a.Start(ctx, app.Profile{TGID: 1, ChatID: 1, Name: "Вова"}, "")
	solo, _, err := a.Start(ctx, app.Profile{TGID: 2, ChatID: 2, Name: "Настя"}, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.AddIdea(ctx, solo, "Бранч на Патриках")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetWish(ctx, solo, res.Idea.ID, domain.WishVery); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Start(ctx, app.Profile{TGID: 2, ChatID: 2, Name: "Настя"}, link[strings.Index(link, "inv_"):]); err != nil {
		t.Fatal(err)
	}
	n, _ := a.Couple(ctx, 2)
	if n.Partner == nil || n.Me.Color != "b" {
		t.Fatalf("not paired: %+v", n.Me)
	}
	ideas, _ := a.S.Ideas(ctx, n.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	if len(ideas) != 1 || ideas[0].Title != "Бранч на Патриках" || ideas[0].WishB != domain.WishVery || ideas[0].WishA != domain.WishNone || ideas[0].AuthorID != n.Me.ID {
		t.Fatalf("the solo idea must come along as hers: %+v", ideas)
	}
}

// One mechanism for everything that needs the partner: proposed by one, answered by the other — in the app
// or in the bot — and a «нет» comes back to the author instead of vanishing.
func TestProposals(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	effects := 0
	a.OnProposal(domain.ProposalChore, func(_ context.Context, _ app.Couple, _ domain.Proposal, yes bool) error {
		if yes {
			effects++
		}
		return nil
	})
	p, err := a.Propose(ctx, v, domain.ProposalChore, 0, "Забрать торт", "до сб 21 ноя", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(2), "Вова просит") || !strings.Contains(f.last(2), "Забрать торт") {
		t.Fatalf("the partner is told in the bot: %q", f.last(2))
	}
	if rows := a.Inbox(ctx, n); len(rows) != 1 || rows[0].Role != "answer" || rows[0].Title != "Вова просит: забрать торт" {
		t.Fatalf("partner inbox: %+v", rows)
	}
	if rows := a.Inbox(ctx, v); len(rows) != 1 || rows[0].Role != "waiting" || rows[0].Hot {
		t.Fatalf("author inbox: %+v", rows)
	}
	if _, err := a.AnswerProposal(ctx, v, p.ID, true); !errors.Is(err, domain.ErrOwnProposal) {
		t.Fatalf("the author cannot answer: %v", err)
	}
	// a day of silence: one reminder, not two
	ck.t = ck.t.Add(25 * time.Hour)
	a.Tick(ctx)
	a.Tick(ctx)
	if c := f.count(2, "Напоминаю. Вова просит"); c != 1 {
		t.Fatalf("reminders: %d", c)
	}
	// «не могу»: back to the author, on the home screen until seen
	if _, err := a.AnswerProposal(ctx, n, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(1), "«нет»") {
		t.Fatalf("the author is told: %q", f.last(1))
	}
	rows := a.Inbox(ctx, v)
	if len(rows) != 1 || rows[0].Role != "declined" || rows[0].Title != "Настя не может: забрать торт" {
		t.Fatalf("declined row: %+v", rows)
	}
	if len(a.Inbox(ctx, n)) != 0 {
		t.Fatal("answered — gone from the partner's inbox")
	}
	if _, err := a.AnswerProposal(ctx, n, p.ID, true); !errors.Is(err, domain.ErrProposalClosed) {
		t.Fatalf("a second answer is refused: %v", err)
	}
	if err := a.SeenProposal(ctx, v, p.ID); err != nil || len(a.Inbox(ctx, v)) != 0 {
		t.Fatalf("seen — the row leaves: %v", err)
	}
	// a new offer about the same thing replaces the waiting one; «да» runs the effect
	p1, _ := a.Propose(ctx, v, domain.ProposalTime, 7, "Планетарий", "вс 15 ноя, 13:00", "")
	p2, _ := a.Propose(ctx, n, domain.ProposalTime, 7, "Планетарий", "сб 21 ноя, 19:00", "")
	if rows := a.Inbox(ctx, v); len(rows) != 1 || rows[0].ProposalID != p2.ID || rows[0].Title != "Настя зовёт: Планетарий" {
		t.Fatalf("the counter-offer replaces the first: %+v (first was %d)", rows, p1.ID)
	}
	p3, _ := a.Propose(ctx, v, domain.ProposalChore, 0, "Купить цветы", "", "")
	if _, err := a.AnswerProposal(ctx, n, p3.ID, true); err != nil || effects != 1 {
		t.Fatalf("yes runs the effect: %v, %d", err, effects)
	}
	if err := a.WithdrawProposal(ctx, v, p2.ID); !errors.Is(err, app.ErrNotYours) {
		t.Fatalf("only the author withdraws: %v", err)
	}
	if err := a.WithdrawProposal(ctx, n, p2.ID); err != nil || len(a.Inbox(ctx, v)) != 0 {
		t.Fatalf("withdrawn — gone for both: %v", err)
	}
}

// Chores: every state has a place, a request is answered by the partner, a «не могу» comes back,
// a repeating chore brings its next occurrence, and done ones end up in the history.
func TestChores(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	find := func(c app.Couple, title string) *app.ChoreView {
		list, _, err := a.Chores(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		for k := range list {
			if list[k].Title == title {
				return &list[k]
			}
		}
		return nil
	}
	today := ck.t
	// mine, for today: in the summary of the home screen, ticked by me only
	mine, err := a.AddChore(ctx, v, app.ChoreIn{Title: "Купить билеты на поезд", Who: "me", Due: &today})
	if err != nil {
		t.Fatal(err)
	}
	if s := a.ChoreSummary(ctx, v); s.Today != 1 || s.Titles[0] != "Купить билеты на поезд" {
		t.Fatalf("summary: %+v", s)
	}
	if s := a.ChoreSummary(ctx, n); s.Today != 0 {
		t.Fatalf("the partner's chore is not in my line: %+v", s)
	}
	if _, err := a.ChoreAct(ctx, n, mine.ID, "done"); !errors.Is(err, app.ErrChoreState) {
		t.Fatalf("a chore is not ticked for the partner: %v", err)
	}
	if x := find(n, "Купить билеты на поезд"); x == nil || x.Who != "partner" || x.Actions[0] != "nudge" {
		t.Fatalf("the partner sees «Напомнить / Забрать себе»: %+v", x)
	}
	// a request: the partner answers; «не могу» returns it to the asker
	ask, err := a.AddChore(ctx, v, app.ChoreIn{Title: "Забрать торт", Who: "partner"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(2), "Вова просит") {
		t.Fatalf("the bot tells the partner: %q", f.last(2))
	}
	if x := find(n, "Забрать торт"); x == nil || x.Section != "ask" || x.Actions[0] != "take" {
		t.Fatalf("asked chore for the partner: %+v", x)
	}
	if _, err := a.ChoreAct(ctx, n, ask.ID, "decline"); err != nil {
		t.Fatal(err)
	}
	x := find(v, "Забрать торт")
	if x == nil || x.State != "declined" || x.Actions[0] != "self" || x.Actions[1] != "any" {
		t.Fatalf("declined — back with the asker: %+v", x)
	}
	if rows := a.Inbox(ctx, v); len(rows) != 1 || rows[0].Role != "declined" {
		t.Fatalf("and on the asker's home screen: %+v", rows)
	}
	if _, err := a.ChoreAct(ctx, v, ask.ID, "any"); err != nil {
		t.Fatal(err)
	}
	if x := find(n, "Забрать торт"); x == nil || x.Who != "any" || x.State != "open" || len(a.Inbox(ctx, v)) != 0 {
		t.Fatalf("left to anyone, the «нет» row is gone: %+v", x)
	}
	if _, err := a.ChoreAct(ctx, n, ask.ID, "take"); err != nil {
		t.Fatal(err)
	}
	if x := find(n, "Забрать торт"); x.Who != "me" {
		t.Fatalf("taken: %+v", x)
	}
	// «Беру» on a request puts the chore into the taker's list
	ask2, _ := a.AddChore(ctx, n, app.ChoreIn{Title: "Вынести ёлку", Who: "partner"})
	if _, err := a.ChoreAct(ctx, v, ask2.ID, "take"); err != nil {
		t.Fatal(err)
	}
	if x := find(v, "Вынести ёлку"); x == nil || x.Who != "me" || x.State != "open" {
		t.Fatalf("«Беру»: %+v", x)
	}
	// a repeating chore: ticked — the next occurrence is there, the done one keeps its day
	rep, _ := a.AddChore(ctx, v, app.ChoreIn{Title: "Оплатить интернет", Who: "me", Due: &today, Repeat: "month"})
	if _, err := a.ChoreAct(ctx, v, rep.ID, "done"); err != nil {
		t.Fatal(err)
	}
	list, _, _ := a.Chores(ctx, v)
	var open, done int
	for _, c := range list {
		if c.Title == "Оплатить интернет" {
			if c.State == "open" && c.Repeat == "month" && c.Due != nil && *c.Due > today.Unix() {
				open++
			}
			if c.State == "done" && c.DoneBy == "Вова" {
				done++
			}
		}
	}
	if open != 1 || done != 1 {
		t.Fatalf("repeat: %d open, %d done in %+v", open, done, list)
	}
	// done chores leave the tab after the kept days and stay in the history, searchable
	if _, err := a.ChoreAct(ctx, v, mine.ID, "done"); err != nil {
		t.Fatal(err)
	}
	ck.t = ck.t.AddDate(0, 0, 8)
	if find(v, "Купить билеты на поезд") != nil {
		t.Fatal("after a week a done chore is only in the history")
	}
	h, _ := a.ChoreHistory(ctx, v, "билеты", "")
	if len(h) != 1 || h[0].DoneBy != "Вова" {
		t.Fatalf("history search: %+v", h)
	}
	if h, _ := a.ChoreHistory(ctx, v, "", "partner"); len(h) != 0 {
		t.Fatalf("history by who: %+v", h)
	}
	// delete and bring back
	if _, err := a.ChoreAct(ctx, v, ask2.ID, "delete"); err != nil || find(v, "Вынести ёлку") != nil {
		t.Fatalf("deleted chores are nowhere: %v", err)
	}
	if _, err := a.ChoreAct(ctx, v, ask2.ID, "restore"); err != nil || find(v, "Вынести ёлку") == nil {
		t.Fatalf("«Вернуть»: %v", err)
	}
}

func TestSettingsAndQuietBot(t *testing.T) {
	a, f, _ := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	view := a.SettingsView(ctx, v)
	if view["me"].(map[string]string)["notify.proposals"] != "1" || view["pair"].(map[string]string)["meet.budget"] != "5000" {
		t.Fatalf("defaults: %+v", view)
	}
	if err := a.SetSetting(ctx, v, "meet.budget", "100500"); !errors.Is(err, app.ErrBadInput) {
		t.Fatalf("unknown values are refused: %v", err)
	}
	// a shared setting: either changes it, the other is told on the home screen
	if err := a.SetSetting(ctx, v, "meet.budget", "3000"); err != nil {
		t.Fatal(err)
	}
	if a.SettingsView(ctx, n)["pair"].(map[string]string)["meet.budget"] != "3000" {
		t.Fatal("the couple's setting is one for both")
	}
	rows := a.Inbox(ctx, n)
	if len(rows) != 1 || rows[0].Role != "notice" || !strings.Contains(rows[0].Title, "Вова изменил настройку «Бюджет на встречу»") || rows[0].Sub != "до 3 000 ₽" {
		t.Fatalf("the partner is told: %+v", rows)
	}
	if _, err := a.AnswerProposal(ctx, n, rows[0].ProposalID, true); err != nil || len(a.Inbox(ctx, n)) != 0 {
		t.Fatalf("«Понятно» closes the notice: %v", err)
	}
	// mine: with proposals switched off the bot stays silent, the home screen still shows the row
	if err := a.SetSetting(ctx, n, "notify.proposals", "0"); err != nil {
		t.Fatal(err)
	}
	before := f.count(2, "")
	if _, err := a.Propose(ctx, v, domain.ProposalTime, 1, "Планетарий", "вс 15 ноя, 13:00", ""); err != nil {
		t.Fatal(err)
	}
	if f.count(2, "") != before {
		t.Fatal("the bot must not write what the member switched off")
	}
	if rows := a.Inbox(ctx, n); len(rows) != 1 || rows[0].Role != "answer" {
		t.Fatalf("the home screen shows everything regardless: %+v", rows)
	}
}

// The meeting of the new interface: a time is offered and accepted (never set by one), a move is a new
// offer, a stop comes into the route only after «да», and a cancel is seen by the other one.
func TestMeetProposals(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	res, _ := a.AddIdea(ctx, n, "Планетарий на Баррикадной")
	id := res.Idea.ID
	_, _ = a.SetWish(ctx, n, id, domain.WishWant)
	_, _ = a.SetWish(ctx, v, id, domain.WishWant)
	r, err := a.GoWith(ctx, v, id)
	if err != nil {
		t.Fatal(err)
	}
	at := ck.t.Add(48 * time.Hour).Truncate(time.Hour)
	if _, err := a.ProposeTime(ctx, v, r.ID, ck.t.Add(-time.Hour)); !errors.Is(err, app.ErrBadTime) {
		t.Fatalf("a time in the past is refused: %v", err)
	}
	p, err := a.ProposeTime(ctx, v, r.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if rd, _ := a.S.Round(ctx, v.Space.ID, r.ID); rd.State == domain.StatePlanned {
		t.Fatal("an offer alone plans nothing")
	}
	if rows := a.Inbox(ctx, n); len(rows) != 1 || rows[0].Title != "Вова зовёт: Планетарий на Баррикадной" {
		t.Fatalf("partner inbox: %+v", rows)
	}
	if mp := a.MeetProposals(ctx, v, r.ID)["time"]; mp == nil || !mp.Mine || mp.State != "wait" || mp.Start != at.Unix() {
		t.Fatalf("the author sees «ждём»: %+v", mp)
	}
	if _, err := a.AnswerProposal(ctx, n, p.ID, true); err != nil {
		t.Fatal(err)
	}
	rd, _ := a.S.Round(ctx, v.Space.ID, r.ID)
	if rd.State != domain.StatePlanned || !rd.StartsAt.Equal(at) {
		t.Fatalf("«да» plans the meeting at the offered time: %s %v", rd.State, rd.StartsAt)
	}
	// a move: the old time stands until the other says yes
	at2 := at.Add(24 * time.Hour)
	p2, _ := a.ProposeTime(ctx, n, r.ID, at2)
	if rd, _ := a.S.Round(ctx, v.Space.ID, r.ID); !rd.StartsAt.Equal(at) {
		t.Fatal("a move waits for the answer")
	}
	if _, err := a.AnswerProposal(ctx, v, p2.ID, true); err != nil {
		t.Fatal(err)
	}
	if rd, _ := a.S.Round(ctx, v.Space.ID, r.ID); !rd.StartsAt.Equal(at2) || rd.State != domain.StatePlanned {
		t.Fatalf("moved: %v", rd.StartsAt)
	}
	// a stop: «нет» comes back to the one who offered; «да» puts it into the route
	s1, err := a.ProposeStop(ctx, v, r.ID, 0, "Кафе у Патриарших")
	if err != nil {
		t.Fatal(err)
	}
	if ex, _ := a.S.Extras(ctx, r.ID); len(ex) != 0 {
		t.Fatal("an offered stop is not in the route yet")
	}
	_, _ = a.AnswerProposal(ctx, n, s1.ID, false)
	if mp := a.MeetProposals(ctx, v, r.ID)["stop"]; mp == nil || mp.State != "no" {
		t.Fatalf("the «нет» is shown to the author: %+v", mp)
	}
	if a.MeetProposals(ctx, n, r.ID)["stop"] != nil {
		t.Fatal("the one who said no has nothing to see")
	}
	s2, _ := a.ProposeStop(ctx, v, r.ID, 0, "Ресторан Хачапури и вино")
	if _, err := a.AnswerProposal(ctx, n, s2.ID, true); err != nil {
		t.Fatal(err)
	}
	if ex, _ := a.S.Extras(ctx, r.ID); len(ex) != 1 || ex[0].State != "accepted" {
		t.Fatalf("accepted stop in the route: %+v", ex)
	}
	// cancel: a row for the other one
	if err := a.CancelMeet(ctx, v, r.ID); err != nil {
		t.Fatal(err)
	}
	rows := a.Inbox(ctx, n)
	if len(rows) != 1 || rows[0].Role != "notice" || rows[0].Title != "Вова отменил встречу: Планетарий на Баррикадной" {
		t.Fatalf("cancel notice: %+v", rows)
	}
	// free windows and busy days
	day := ck.t.In(v.Loc()).AddDate(0, 0, 1).Format("2006-01-02")
	_ = a.SetFreeSlot(ctx, v, day, 1, true)
	if f := a.Free(ctx, n); len(f["common"].([]string)) != 0 || len(f["partner"].([]string)) != 1 {
		t.Fatalf("one mark is not a common window: %+v", f)
	}
	_ = a.SetFreeSlot(ctx, n, day, 1, true)
	_ = a.SetBusyDay(ctx, n, day, true)
	f := a.Free(ctx, v)
	if c := f["common"].([]string); len(c) != 1 || c[0] != day+":1" {
		t.Fatalf("common window: %+v", f)
	}
	if b := f["partner_busy"].([]string); len(b) != 1 || b[0] != day {
		t.Fatalf("the partner's busy day: %+v", f)
	}
}

// since: what a chat got after the first `from` messages — the texts and every button as «label|data-or-url»
func (f *fakeTG) since(from int, chat int64) (texts, keys string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs[from:] {
		if m.chat != chat {
			continue
		}
		texts += m.text + "\n---\n"
		if m.mk == nil {
			continue
		}
		for _, row := range m.mk.InlineKeyboard {
			for _, b := range row {
				keys += b.Text + "|" + b.CallbackData + b.URL
				if b.WebApp != nil {
					keys += b.WebApp.URL
				}
				keys += "\n"
			}
		}
	}
	return
}

func (f *fakeTG) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

// The bot for someone whose app is the new interface: the partner is called without slots, the time is a
// proposal with «Да / Не могу», the meeting is in the plans after «Да» — and none of the old choice
// (mark your time, three variants, confirm) is sent. The old interface keeps its messages.
func TestBotFollowsNewInterface(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	if a.UsesV2(ctx, v.Me.ID) || a.UsesV2(ctx, n.Me.ID) {
		t.Fatal("this pair reported the old screens (see pair)")
	}
	if err := a.SetUI(ctx, v.Me.ID, 3); !errors.Is(err, app.ErrBadInput) {
		t.Fatalf("an unknown interface is refused: %v", err)
	}
	for _, id := range []int64{v.Me.ID, n.Me.ID} {
		if err := a.SetUI(ctx, id, 2); err != nil || !a.UsesV2(ctx, id) {
			t.Fatalf("the new shell reports itself: %v", err)
		}
	}
	_ = a.SetUI(ctx, v.Me.ID, 1)
	if a.UsesV2(ctx, v.Me.ID) {
		t.Fatal("the old screens take the old messages back")
	}
	_ = a.SetUI(ctx, v.Me.ID, 2)

	res, _ := a.AddIdea(ctx, n, "Планетарий на Баррикадной")
	id := res.Idea.ID
	a.Tick(ctx) // the idea card
	if tx, keys := f.since(0, 2); !strings.Contains(tx, "Сохранил в идеи") || !strings.Contains(keys, "Не моё|") || strings.Contains(keys, "Очень хочу") {
		t.Fatalf("the idea card has the two marks of the new interface: %q %q", tx, keys)
	}
	_, _ = a.SetWish(ctx, n, id, domain.WishWant)
	_, _ = a.SetWish(ctx, v, id, domain.WishWant)

	start := f.n()
	r, err := a.GoWith(ctx, v, id)
	if err != nil {
		t.Fatal(err)
	}
	tx, keys := f.since(start, 2)
	if !strings.Contains(tx, "Вова зовёт: <b>Планетарий на Баррикадной</b>. Время ещё не выбрано.") {
		t.Fatalf("the partner is called in one line: %q", tx)
	}
	if !strings.Contains(keys, fmt.Sprintf("/app/meet/%d", r.ID)) || strings.Contains(keys, "|ts:") || strings.Contains(keys, "|x:") || strings.Contains(keys, "round/") {
		t.Fatalf("one button to the meeting card, no slots: %q", keys)
	}
	if tx, _ := f.since(start, 1); tx != "" {
		t.Fatalf("the one who called is in the card already: %q", tx)
	}

	at := ck.t.Add(48 * time.Hour).Truncate(time.Hour)
	p, err := a.ProposeTime(ctx, v, r.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, keys := f.since(start, 2); !strings.Contains(keys, fmt.Sprintf("Да|pr:%d:yes", p.ID)) || !strings.Contains(keys, fmt.Sprintf("Не могу|pr:%d:no", p.ID)) {
		t.Fatalf("the time is a proposal with «Да / Не могу»: %q", keys)
	}
	if _, err := a.AnswerProposal(ctx, n, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if rd, _ := a.S.Round(ctx, v.Space.ID, r.ID); rd.State != domain.StatePlanned || !rd.StartsAt.Equal(at) {
		t.Fatalf("«Да» puts the meeting into the plans: %s", rd.State)
	}
	for _, chat := range []int64{1, 2} {
		tx, keys := f.since(start, chat)
		for _, old := range []string{"Когда тебе удобно", "отметь, когда", "Куда идём", "Совпало!", "Подтверди", "предлагает время", "Пока не совпало", "Мы идём", "ответила «да»", "копилк"} {
			if strings.Contains(tx, old) {
				t.Fatalf("chat %d got a message of the old choice (%q): %q", chat, old, tx)
			}
		}
		for _, old := range []string{"|ts:", "|v:", "|c:", "|ot:", "|xq:", "|rs:"} {
			if strings.Contains(keys, old) {
				t.Fatalf("chat %d got a button of the old choice (%q): %q", chat, old, keys)
			}
		}
		if strings.Count(tx, "Встреча в планах.") != 1 || !strings.Contains(keys, fmt.Sprintf("/app/meet/%d", r.ID)) {
			t.Fatalf("chat %d hears «в планах» once, with the card: %q %q", chat, tx, keys)
		}
	}

	// a move is a proposal too; after «Да» both hear the new time and the reminder follows it
	at2 := at.Add(24 * time.Hour)
	p2, _ := a.ProposeTime(ctx, n, r.ID, at2)
	start = f.n()
	if _, err := a.AnswerProposal(ctx, v, p2.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, chat := range []int64{1, 2} {
		if tx, _ := f.since(start, chat); strings.Count(tx, "Встреча перенесена.") != 1 || strings.Contains(tx, "ответил «да»") {
			t.Fatalf("chat %d hears the move once: %q", chat, tx)
		}
	}
	start = f.n()
	ck.t = at2.Add(-2*time.Hour + time.Minute)
	a.Tick(ctx)
	if tx, keys := f.since(start, 2); !strings.Contains(tx, "Через 2 часа") || !strings.Contains(keys, fmt.Sprintf("/app/meet/%d", r.ID)) || strings.Contains(keys, "|ot:") {
		t.Fatalf("the reminder opens the card and moves nothing by itself: %q %q", tx, keys)
	}
	rd, _ := a.S.Round(ctx, v.Space.ID, r.ID)
	start = f.n()
	ck.t = rd.EndsAt.Add(3*time.Hour + time.Minute)
	a.Tick(ctx)
	if tx, keys := f.since(start, 2); !strings.Contains(tx, "Как прошло: <b>Планетарий на Баррикадной</b>?") || !strings.Contains(keys, fmt.Sprintf("Понравилось|af:%d:like", r.ID)) || strings.Contains(keys, "|d:") {
		t.Fatalf("«как прошло» asks each one with the three answers of the new interface: %q %q", tx, keys)
	}
	start = f.n()
	if err := a.Rate(ctx, n, r.ID, "like"); err != nil {
		t.Fatal(err)
	}
	if err := a.Rate(ctx, v, r.ID, "soso"); err != nil {
		t.Fatal(err)
	}
	if rd, _ := a.S.Round(ctx, v.Space.ID, r.ID); rd.State != domain.StateDone || rd.AgainA == "" || rd.AgainB == "" {
		t.Fatalf("each rated for themselves: %s %q %q", rd.State, rd.AgainA, rd.AgainB)
	}
	for _, chat := range []int64{1, 2} {
		if tx, _ := f.since(start, chat); strings.Contains(tx, "Повторим") || strings.Contains(tx, "Вы оба ответили") {
			t.Fatalf("chat %d: the app has asked already, the bot does not ask again: %q", chat, tx)
		}
	}

	// a cancellation: one line to the partner, in the words of the new interface
	res2, _ := a.AddIdea(ctx, v, "Каток в Парке Горького")
	r2, err := a.GoWith(ctx, v, res2.Idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	p3, _ := a.ProposeTime(ctx, v, r2.ID, ck.t.Add(72*time.Hour))
	start = f.n()
	if _, err := a.Cancel(ctx, v, r2.ID); err != nil {
		t.Fatal(err)
	}
	if tx, _ := f.since(start, 2); !strings.Contains(tx, "Вова отменил встречу: <b>Каток в Парке Горького</b>.") || strings.Contains(tx, "выбор") {
		t.Fatalf("cancel: %q", tx)
	}
	if _, err := a.AnswerProposal(ctx, n, p3.ID, true); err == nil {
		t.Fatalf("the offered time of a cancelled meeting is withdrawn: %v", err)
	}

	// the partner went back to the old screens: they are written to the old way, the one with the new shell is not
	_ = a.SetUI(ctx, n.Me.ID, 1)
	start = f.n()
	if _, err := a.GoWith(ctx, v, res2.Idea.ID); err != nil {
		t.Fatal(err)
	}
	if tx, keys := f.since(start, 2); !strings.Contains(tx, "Вова хочет") || !strings.Contains(keys, "round/") {
		t.Fatalf("the old interface keeps its message: %q %q", tx, keys)
	}
}

// A dated event in the new interface: «Идём» offers its time as a proposal at once — one message, no «подтверди»
func TestCallDatedEvent(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	_ = a.SetUI(ctx, v.Me.ID, 2)
	_ = a.SetUI(ctx, n.Me.ID, 2)
	res, _ := a.AddIdea(ctx, v, "Концерт в Зарядье")
	i, _ := a.S.Idea(ctx, v.Space.ID, res.Idea.ID)
	at := ck.t.Add(72 * time.Hour).Truncate(time.Hour).Add(7 * time.Hour) // 19:00
	i.StartsAt = &at
	if err := a.S.UpdateIdea(ctx, i, ck.t); err != nil {
		t.Fatal(err)
	}
	start := f.n()
	r, err := a.Call(ctx, v, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, keys := f.since(start, 2)
	if strings.Count(tx, "---") != 1 || !strings.Contains(tx, "Вова зовёт: <b>Концерт в Зарядье</b>") || strings.Contains(tx, "Время ещё не выбрано") || strings.Contains(tx, "Подтверди") {
		t.Fatalf("one proposal with the time of the event: %q", tx)
	}
	if !strings.Contains(keys, "Да|pr:") || strings.Contains(keys, "|c:") {
		t.Fatalf("answered as a proposal: %q", keys)
	}
	if mp := a.MeetProposals(ctx, n, r.ID)["time"]; mp == nil || mp.Start != at.Unix() {
		t.Fatalf("the time of the event is offered: %+v", mp)
	}
	if _, err := a.Call(ctx, n, i.ID); !errors.Is(err, app.ErrRoundOpen) {
		t.Fatalf("one open meeting at a time: %v", err)
	}
}

// «Идём» from a chat (the assistant's button, the bot) in the new interface: the same meeting without a time,
// and words for the one who called instead of «отметьте время»
func TestCallWords(t *testing.T) {
	a, f, _ := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	_ = a.SetUI(ctx, v.Me.ID, 2)
	_ = a.SetUI(ctx, n.Me.ID, 2)
	one, _ := a.AddIdea(ctx, v, "Музей космонавтики")
	two, _ := a.AddIdea(ctx, v, "Кофе на Патриках")
	start := f.n()
	w, err := a.CallWords(ctx, v, one.Idea.ID)
	if err != nil || !strings.Contains(w, "Предложи время в карточке встречи, Настя ответит") {
		t.Fatalf("%q %v", w, err)
	}
	if tx, keys := f.since(start, 2); !strings.Contains(tx, "Вова зовёт: <b>Музей космонавтики</b>. Время ещё не выбрано.") || strings.Contains(keys, "|ts:") {
		t.Fatalf("the partner is called: %q %q", tx, keys)
	}
	if w, _ := a.CallWords(ctx, n, one.Idea.ID); !strings.Contains(w, "уже есть") {
		t.Fatalf("the same meeting is not made twice: %q", w)
	}
	if w, _ := a.CallWords(ctx, n, two.Idea.ID); !strings.Contains(w, "встреча без времени") {
		t.Fatalf("one open meeting at a time, said in words: %q", w)
	}
	// the weekly «время планировать» has nothing to open in the new interface
	sp, _ := a.S.Space(ctx, v.Space.ID)
	r, _ := a.S.OpenRound(ctx, v.Space.ID)
	_, _ = a.Cancel(ctx, v, r.ID)
	start = f.n()
	if err := a.ScheduledGo(ctx, sp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.S.OpenRound(ctx, v.Space.ID); err == nil || f.n() != start {
		t.Fatal("no round by slots is opened for the new interface")
	}
}

// The invitation of the new interface: the one who opens the link sees who calls, and the pair is made by
// «Принять приглашение» — not by opening the link. Everything they had alone comes along; the author gets a
// line in the bot and on the home screen. A link that cannot be accepted says why.
func TestInviteAccept(t *testing.T) {
	a, f, _ := setup(t)
	ctx := context.Background()
	v, link, err := a.Start(ctx, app.Profile{TGID: 1, ChatID: 1, Name: "Вова"}, "")
	if err != nil {
		t.Fatal(err)
	}
	code := app.InviteCode(link)
	oldUI(t, a, v.Me.ID)
	if a.InviteV2(ctx, code) {
		t.Fatal("the author has the old interface: the link joins at once, as before")
	}
	_ = a.SetUI(ctx, v.Me.ID, 2)
	if !a.InviteV2(ctx, code) || a.InviteV2(ctx, "NOSUCHCODE") {
		t.Fatal("the link of someone with the new interface opens the invitation screen")
	}
	n, _, _ := a.Start(ctx, app.Profile{TGID: 2, ChatID: 2, Name: "Настя"}, "")
	res, err := a.AddIdea(ctx, n, "Бранч на Патриках")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetWish(ctx, n, res.Idea.ID, domain.WishVery); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddChore(ctx, n, app.ChoreIn{Title: "Купить корм", Who: "me"}); err != nil {
		t.Fatal(err)
	}

	// the screen: who calls, how many of her ideas will move; looking joins nothing
	info := a.InviteInfo(ctx, n, code)
	if info.State != app.InviteOK || info.From != "Вова" || info.Ideas != 1 {
		t.Fatalf("the invitation: %+v", info)
	}
	if c, _ := a.Couple(ctx, 2); c.Partner != nil {
		t.Fatal("the pair is made by «Принять», not by looking")
	}
	// the author opens their own link
	if st := a.InviteInfo(ctx, v, code).State; st != app.InviteOwn {
		t.Fatalf("own link: %s", st)
	}
	if _, _, err := a.AcceptInvite(ctx, v, code); !errors.Is(err, app.ErrInviteOwn) {
		t.Fatalf("own link is not accepted: %v", err)
	}

	start := f.n()
	nc, moved, err := a.AcceptInvite(ctx, n, code)
	if err != nil || moved != 1 || nc.Partner == nil || nc.Partner.ID != v.Me.ID || nc.Space.ID != v.Space.ID {
		t.Fatalf("accept: %v moved=%d %+v", err, moved, nc.Me)
	}
	// nothing of the solo space is lost: the idea with her mark on her side, and the chore
	ideas, _ := a.S.Ideas(ctx, nc.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	if len(ideas) != 1 || ideas[0].Title != "Бранч на Патриках" || ideas[0].WishB != domain.WishVery || ideas[0].WishA != domain.WishNone || ideas[0].AuthorID != nc.Me.ID {
		t.Fatalf("the solo idea must come along as hers: %+v", ideas)
	}
	chores, _ := a.S.Chores(ctx, nc.Space.ID)
	if len(chores) != 1 || chores[0].Title != "Купить корм" || chores[0].Assignee != nc.Me.ID {
		t.Fatalf("the solo chore must come along: %+v", chores)
	}
	// the author: one line in the bot and the same on the home screen; the invited one is in the app already
	if tx, _ := f.since(start, 1); strings.Count(tx, "---") != 1 || !strings.Contains(tx, "Настя приняла приглашение") {
		t.Fatalf("the author hears it once: %q", tx)
	}
	if tx, _ := f.since(start, 2); tx != "" {
		t.Fatalf("the invited one is not written to: %q", tx)
	}
	vc, _ := a.Couple(ctx, 1)
	rows := a.Inbox(ctx, vc)
	if len(rows) != 1 || rows[0].Role != "notice" || rows[0].Title != "Настя приняла приглашение" || rows[0].RefID >= 0 {
		t.Fatalf("the line on the author's home screen: %+v", rows)
	}
	if len(a.Inbox(ctx, nc)) != 0 {
		t.Fatal("the line is the author's, not hers")
	}

	// used: somebody else opens the same link
	x, _, _ := a.Start(ctx, app.Profile{TGID: 3, ChatID: 3, Name: "Третий"}, "")
	if info := a.InviteInfo(ctx, x, code); info.State != app.InviteUsed || info.From != "Вова" {
		t.Fatalf("a used link says who is in a pair already: %+v", info)
	}
	if _, _, err := a.AcceptInvite(ctx, x, code); !errors.Is(err, app.ErrInviteUsed) {
		t.Fatalf("a used link is not accepted: %v", err)
	}
	if c, _ := a.Couple(ctx, 3); c.Partner != nil {
		t.Fatal("the third one stays alone")
	}
	// the one who is in a pair opens another invitation
	_, link2, _ := a.Start(ctx, app.Profile{TGID: 4, ChatID: 4, Name: "Чужой"}, "")
	if info := a.InviteInfo(ctx, nc, app.InviteCode(link2)); info.State != app.InvitePaired || info.Partner != "Вова" {
		t.Fatalf("already in a pair: %+v", info)
	}
	if _, _, err := a.AcceptInvite(ctx, nc, app.InviteCode(link2)); !errors.Is(err, app.ErrInvitePaired) {
		t.Fatalf("in a pair, another invitation is not accepted: %v", err)
	}
	if c, _ := a.Couple(ctx, 2); c.Partner == nil || c.Partner.ID != v.Me.ID {
		t.Fatal("the pair stays as it was")
	}
	if st := a.InviteInfo(ctx, x, "NOSUCHCODE").State; st != app.InviteGone {
		t.Fatalf("an unknown link: %s", st)
	}
}

// A link older than three days: «Попросить новую» — its author hears «Настя просит новую ссылку» once, in the
// bot and on the home screen, and the new link works
func TestInviteExpired(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, link, _ := a.Start(ctx, app.Profile{TGID: 1, ChatID: 1, Name: "Вова"}, "")
	_ = a.SetUI(ctx, v.Me.ID, 2)
	code := app.InviteCode(link)
	n, _, _ := a.Start(ctx, app.Profile{TGID: 2, ChatID: 2, Name: "Настя"}, "")
	if err := a.AskInvite(ctx, n, code); !errors.Is(err, app.ErrInviteUsed) {
		t.Fatalf("a link that still works is accepted, not asked for again: %v", err)
	}
	ck.t = ck.t.Add(73 * time.Hour)
	if info := a.InviteInfo(ctx, n, code); info.State != app.InviteExpired || info.From != "Вова" || info.Asked {
		t.Fatalf("expired: %+v", info)
	}
	if _, _, err := a.AcceptInvite(ctx, n, code); !errors.Is(err, app.ErrInviteExpired) {
		t.Fatalf("an expired link is not accepted: %v", err)
	}
	if c, _ := a.Couple(ctx, 2); c.Partner != nil {
		t.Fatal("still alone")
	}
	start := f.n()
	if err := a.AskInvite(ctx, n, code); err != nil {
		t.Fatal(err)
	}
	if tx, keys := f.since(start, 1); strings.Count(tx, "---") != 1 || !strings.Contains(tx, "Настя просит новую ссылку") || !strings.Contains(keys, "Новая ссылка|inv") {
		t.Fatalf("the author is asked in the bot: %q %q", tx, keys)
	}
	rows := a.Inbox(ctx, v)
	if len(rows) != 1 || rows[0].Role != "notice" || rows[0].Title != "Настя просит новую ссылку" {
		t.Fatalf("the author is asked on the home screen: %+v", rows)
	}
	start = f.n()
	if err := a.AskInvite(ctx, n, code); err != nil || f.n() != start {
		t.Fatalf("asked once: %v", err)
	}
	if !a.InviteInfo(ctx, n, code).Asked {
		t.Fatal("the screen knows a new link has been asked for")
	}
	link2, err := a.NewInvite(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if nc, _, err := a.AcceptInvite(ctx, n, app.InviteCode(link2)); err != nil || nc.Partner == nil {
		t.Fatalf("the new link works: %v", err)
	}
}

// «Общее время» → «Идём»: the meeting is made with the time at once, and the partner gets one message — the
// proposal — not «зовёт» and then the time
func TestCallAtOneMessage(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	_ = a.SetUI(ctx, v.Me.ID, 2)
	_ = a.SetUI(ctx, n.Me.ID, 2)
	res, _ := a.AddIdea(ctx, v, "Планетарий на Баррикадной")
	at := ck.t.Add(48 * time.Hour)
	start := f.n()
	r, err := a.CallAt(ctx, v, res.Idea.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	tx, keys := f.since(start, 2)
	if strings.Count(tx, "---") != 1 || !strings.Contains(tx, "Вова зовёт: <b>Планетарий на Баррикадной</b>") || strings.Contains(tx, "Время ещё не выбрано") {
		t.Fatalf("one message, the proposal of the time: %q", tx)
	}
	if !strings.Contains(keys, "Да|pr:") {
		t.Fatalf("answered as a proposal: %q", keys)
	}
	if tx, _ := f.since(start, 1); tx != "" {
		t.Fatalf("the one who called is in the card already: %q", tx)
	}
	if mp := a.MeetProposals(ctx, n, r.ID)["time"]; mp == nil || mp.Start != at.Unix() {
		t.Fatalf("the time is offered: %+v", mp)
	}
	// «Идём» in another window for the same idea: the same meeting gets the new offer
	at2 := at.Add(24 * time.Hour)
	r2, err := a.CallAt(ctx, v, res.Idea.ID, at2)
	if err != nil || r2.ID != r.ID {
		t.Fatalf("the same meeting: %v %d %d", err, r.ID, r2.ID)
	}
	mp := a.MeetProposals(ctx, n, r.ID)["time"]
	if mp == nil || mp.Start != at2.Unix() {
		t.Fatalf("the offer is replaced: %+v", mp)
	}
	if _, err := a.AnswerProposal(ctx, n, mp.ID, true); err != nil {
		t.Fatal(err)
	}
	if rd, _ := a.S.Round(ctx, v.Space.ID, r.ID); rd.State != domain.StatePlanned || rd.StartsAt.Unix() != at2.Unix() {
		t.Fatalf("«да» puts it into the plans: %s", rd.State)
	}
}

// «Как прошло»: «Не ходили» of one is their own answer — it does not cancel the meeting for the partner,
// and both answers stay with the meeting
func TestRateDoesNotDecideForTwo(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	_ = a.SetUI(ctx, v.Me.ID, 2)
	_ = a.SetUI(ctx, n.Me.ID, 2)
	res, _ := a.AddIdea(ctx, v, "Каток в Парке Горького")
	r, err := a.CallAt(ctx, v, res.Idea.ID, ck.t.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AnswerProposal(ctx, n, a.MeetProposals(ctx, n, r.ID)["time"].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := a.Rate(ctx, v, r.ID, "like"); !errors.Is(err, domain.ErrWrongState) {
		t.Fatalf("not rated before it happened: %v", err)
	}
	rd, _ := a.S.Round(ctx, v.Space.ID, r.ID)
	ck.t = rd.EndsAt.Add(4 * time.Hour)
	if err := a.Rate(ctx, v, r.ID, "skip"); err != nil {
		t.Fatal(err)
	}
	if rd, _ = a.S.Round(ctx, v.Space.ID, r.ID); rd.State != domain.StatePlanned || rd.AgainA != domain.AgainSkip || rd.AgainB != "" {
		t.Fatalf("his «не ходили» is his: the meeting is not cancelled and she is not answered for: %s %q %q", rd.State, rd.AgainA, rd.AgainB)
	}
	if err := a.Rate(ctx, n, r.ID, "like"); err != nil {
		t.Fatal(err)
	}
	if rd, _ = a.S.Round(ctx, v.Space.ID, r.ID); rd.State != domain.StateDone || rd.AgainA != domain.AgainSkip || rd.AgainB != "yes" {
		t.Fatalf("she went and liked it; both answers are kept: %s %q %q", rd.State, rd.AgainA, rd.AgainB)
	}
	if i, _ := a.S.Idea(ctx, v.Space.ID, res.Idea.ID); i.AgainYes != 1 || i.AgainNo != 0 || i.LastMetAt == nil {
		t.Fatalf("her mark counts for the idea, his «не ходили» does not: %d %d", i.AgainYes, i.AgainNo)
	}
	if err := a.Rate(ctx, v, r.ID, "like"); !errors.Is(err, domain.ErrWrongState) {
		t.Fatalf("an answer is given once: %v", err)
	}
	if err := a.Rate(ctx, n, r.ID, "like"); err != nil {
		t.Fatalf("the same answer again is not an error: %v", err)
	}
	if i, _ := a.S.Idea(ctx, v.Space.ID, res.Idea.ID); i.AgainYes != 1 {
		t.Fatalf("and it is not counted twice: %d", i.AgainYes)
	}
	if err := a.Rate(ctx, n, r.ID, "maybe"); !errors.Is(err, app.ErrBadInput) {
		t.Fatalf("an unknown answer: %v", err)
	}
}

// «Когда напоминать о встрече» is each one's own: a day ahead, two hours ahead, or both times
func TestMeetingReminderSetting(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	_ = a.SetUI(ctx, v.Me.ID, 2)
	_ = a.SetUI(ctx, n.Me.ID, 2)
	if err := a.SetSetting(ctx, v, "notify.meeting_when", "day"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSetting(ctx, n, "notify.meeting_when", "both"); err != nil {
		t.Fatal(err)
	}
	res, _ := a.AddIdea(ctx, v, "Планетарий на Баррикадной")
	at := ck.t.Add(72 * time.Hour) // Saturday, 12:00
	r, err := a.CallAt(ctx, v, res.Idea.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	start := f.n()
	if _, err := a.AnswerProposal(ctx, n, a.MeetProposals(ctx, n, r.ID)["time"].ID, true); err != nil {
		t.Fatal(err)
	}
	if tx, _ := f.since(start, 1); !strings.Contains(tx, "Напомню за день.") {
		t.Fatalf("the message says when he will be reminded: %q", tx)
	}
	if tx, _ := f.since(start, 2); !strings.Contains(tx, "Напомню за день и за 2 часа.") {
		t.Fatalf("the message says when she will be reminded: %q", tx)
	}
	start = f.n()
	ck.t = at.Add(-24*time.Hour + time.Minute)
	a.Tick(ctx)
	for _, chat := range []int64{1, 2} {
		if tx, _ := f.since(start, chat); strings.Count(tx, "Завтра: <b>Планетарий на Баррикадной</b>") != 1 || strings.Contains(tx, "Через 2 часа") {
			t.Fatalf("chat %d is reminded a day ahead: %q", chat, tx)
		}
	}
	start = f.n()
	ck.t = at.Add(-2*time.Hour + time.Minute)
	a.Tick(ctx)
	if tx, _ := f.since(start, 1); strings.Contains(tx, "Через 2 часа") {
		t.Fatalf("«за день» only: no second reminder for him: %q", tx)
	}
	if tx, _ := f.since(start, 2); strings.Count(tx, "Через 2 часа: <b>Планетарий на Баррикадной</b>") != 1 {
		t.Fatalf("«оба раза»: she is reminded two hours ahead too: %q", tx)
	}
}

// the word «копилка» belongs to the old screens: a message on its way to someone with the new interface
// says «идеи»
func TestNewInterfaceWords(t *testing.T) {
	a, f, _ := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	_ = a.SetUI(ctx, v.Me.ID, 2)
	if got := a.WordsFor(ctx, 1, "Это уже есть в копилке."); got != "Это уже есть в идеях." {
		t.Fatalf("the new interface: %q", got)
	}
	if got := a.WordsFor(ctx, 2, "Это уже есть в копилке."); got != "Это уже есть в копилке." {
		t.Fatalf("the old interface keeps its word: %q", got)
	}
	// a cancelled choice of the old flow, heard by both: each in the words of their interface
	res, _ := a.AddIdea(ctx, n, "Каток в Парке Горького")
	_ = a.SetUI(ctx, v.Me.ID, 1)
	r, err := a.GoWith(ctx, n, res.Idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = a.SetUI(ctx, v.Me.ID, 2)
	start := f.n()
	if _, err := a.Cancel(ctx, n, r.ID); err != nil {
		t.Fatal(err)
	}
	if tx, _ := f.since(start, 1); strings.Contains(tx, "опилк") {
		t.Fatalf("no «копилка» for the new interface: %q", tx)
	}
	if tx, _ := f.since(start, 2); !strings.Contains(tx, "идеи остаются в копилке") {
		t.Fatalf("the old interface hears what it heard before: %q", tx)
	}
}

// ---------- leaving the pair, the file with one's data, deleting oneself ----------

// ideaSnap: every idea of a space with what must not change for the one who stays
func ideaSnap(t *testing.T, a *app.App, space int64) string {
	t.Helper()
	rows, err := a.S.DB.Query(`SELECT id, title, author_id, wish_a, wish_b, note, source, status FROM ideas WHERE space_id=? ORDER BY id`, space)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, author int64
		var title, wa, wb, note, src, st string
		if err := rows.Scan(&id, &title, &author, &wa, &wb, &note, &src, &st); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%d|%s|%d|%s|%s|%s|%s|%s\n", id, title, author, wa, wb, note, src, st)
	}
	return b.String()
}

func count(t *testing.T, a *app.App, q string, args ...any) int {
	t.Helper()
	var n int
	if err := a.S.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// refsTo: every row of every table that still points at a member — by any column that holds a member's id
func refsTo(t *testing.T, a *app.App, id int64) []string {
	t.Helper()
	list := func(q string, args ...any) []string {
		rows, err := a.S.DB.Query(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	var bad []string
	for _, tb := range list(`SELECT name FROM sqlite_master WHERE type='table'`) {
		for _, col := range list(`SELECT name FROM pragma_table_info(?)`, tb) {
			switch col {
			case "member_id", "author_id", "by_member", "proposed_by", "assignee_id", "done_by":
				if n := count(t, a, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s=?`, tb, col), id); n > 0 {
					bad = append(bad, fmt.Sprintf("%s.%s: %d", tb, col, n))
				}
			}
		}
	}
	if n := count(t, a, `SELECT COUNT(*) FROM members WHERE id=?`, id); n > 0 {
		bad = append(bad, "members")
	}
	for _, k := range []string{"taste", "busyurl", "cal", "calseen", "tickets_wait"} {
		if n := count(t, a, `SELECT COUNT(*) FROM settings WHERE key=?`, fmt.Sprintf("%s:%d", k, id)); n > 0 {
			bad = append(bad, "settings "+k)
		}
	}
	if n := count(t, a, `SELECT COUNT(*) FROM settings WHERE (key LIKE 'caltoken:%' OR key LIKE 'tickets:%') AND value=?`, fmt.Sprint(id)); n > 0 {
		bad = append(bad, "settings by value")
	}
	return bad
}

// lifeOfPair: two people who have used everything — ideas of both with marks, a field typed by the partner,
// what the scout found, a meeting that happened and was rated by both, one in the plans with a stop, one
// still without a time, chores in every state. Returns the ids the tests look at.
type pairLife struct {
	v, n                            app.Couple
	planet, rink, brunch            int64 // Вова's, Вова's with Настя's note, Настя's
	past, future, open              domain.Round
	tickets, flowers, cake, garbage int64 // chores: Вова's, Настя's, Вова asked Настя, done by Вова
}

func lifeOfPair(t *testing.T, a *app.App, ck *clock) pairLife {
	t.Helper()
	ctx := context.Background()
	var l pairLife
	l.v, l.n = pair(t, a)
	_ = a.SetUI(ctx, l.v.Me.ID, 2)
	_ = a.SetUI(ctx, l.n.Me.ID, 2)
	add := func(c app.Couple, title string) int64 {
		res, err := a.AddIdea(ctx, c, title)
		if err != nil {
			t.Fatal(err)
		}
		return res.Idea.ID
	}
	l.planet, l.rink, l.brunch = add(l.v, "Планетарий на Баррикадной"), add(l.v, "Каток в Парке Горького"), add(l.n, "Бранч на Патриках")
	wish := func(c app.Couple, id int64, w domain.Wish) {
		if _, err := a.SetWish(ctx, c, id, w); err != nil {
			t.Fatal(err)
		}
	}
	wish(l.v, l.planet, domain.WishWant)
	wish(l.n, l.planet, domain.WishVery)
	wish(l.n, l.rink, domain.WishWant)
	wish(l.v, l.brunch, domain.WishSkip)
	wish(l.n, l.brunch, domain.WishVery)
	note := func(id int64, by app.Couple, text string) {
		i, _ := a.S.Idea(ctx, l.v.Space.ID, id)
		i.Note = text
		if err := a.S.UpdateIdea(ctx, i, ck.t); err != nil {
			t.Fatal(err)
		}
		_ = a.S.MarkIdeaEdit(ctx, id, "note", by.Me.ID, ck.t)
	}
	note(l.planet, l.v, "взять плед")
	note(l.rink, l.n, "Настина заметка про коньки")
	if _, err := a.S.CreateIdea(ctx, store.Idea{Idea: domain.Idea{SpaceID: l.v.Space.ID, AuthorID: l.v.Me.ID, Title: "Концерт из афиши"}, Source: "scout"}, ck.t); err != nil {
		t.Fatal(err)
	}
	meet := func(c, other app.Couple, id int64, at time.Time) domain.Round {
		r, err := a.GoWith(ctx, c, id)
		if err != nil {
			t.Fatal(err)
		}
		p, err := a.ProposeTime(ctx, c, r.ID, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.AnswerProposal(ctx, other, p.ID, true); err != nil {
			t.Fatal(err)
		}
		r, _ = a.S.Round(ctx, c.Space.ID, r.ID)
		if r.State != domain.StatePlanned {
			t.Fatalf("not planned: %s", r.State)
		}
		return r
	}
	l.past = meet(l.v, l.n, l.planet, ck.t.Add(48*time.Hour).Truncate(time.Hour))
	ck.t = l.past.StartsAt.Add(6 * time.Hour)
	if err := a.Rate(ctx, l.n, l.past.ID, "like"); err != nil {
		t.Fatal(err)
	}
	if err := a.Rate(ctx, l.v, l.past.ID, "soso"); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.Photo(ctx, l.n, "photo-file"); err != nil || !ok {
		t.Fatalf("photo: %v %v", ok, err)
	}
	l.future = meet(l.v, l.n, l.rink, ck.t.Add(48*time.Hour).Truncate(time.Hour))
	stop, err := a.ProposeStop(ctx, l.n, l.future.ID, 0, "Кофейня у катка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AnswerProposal(ctx, l.v, stop.ID, true); err != nil {
		t.Fatal(err)
	}
	if l.open, err = a.GoWith(ctx, l.n, l.brunch); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProposeTime(ctx, l.n, l.open.ID, ck.t.Add(96*time.Hour).Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	chore := func(c app.Couple, title, who string) int64 {
		ch, err := a.AddChore(ctx, c, app.ChoreIn{Title: title, Who: who})
		if err != nil {
			t.Fatal(err)
		}
		return ch.ID
	}
	l.tickets, l.flowers, l.cake, l.garbage = chore(l.v, "Купить билеты на поезд", "me"), chore(l.n, "Полить цветы", "me"), chore(l.v, "Забрать торт", "partner"), chore(l.v, "Вынести мусор", "me")
	if _, err := a.ChoreAct(ctx, l.v, l.garbage, "done"); err != nil {
		t.Fatal(err)
	}
	if id := chore(l.n, "Помыть окна", "me"); id != 0 {
		if _, err := a.ChoreAct(ctx, l.n, id, "done"); err != nil {
			t.Fatal(err)
		}
	}
	// what is only one person's: memory, taste, a busy day, a free window, a login, the chat with the assistant
	for _, c := range []app.Couple{l.v, l.n} {
		if _, err := a.Remember(ctx, c, "не люблю стендап, "+c.Me.Name); err != nil {
			t.Fatal(err)
		}
		_ = a.S.SetSetting(ctx, fmt.Sprintf("taste:%d", c.Me.ID), `{"spheres":{"music":1},"tags":{}}`)
		day := ck.t.AddDate(0, 0, 3).Format("2006-01-02")
		if err := a.SetBusyDay(ctx, c, day, true); err != nil {
			t.Fatal(err)
		}
		if err := a.SetFreeSlot(ctx, c, day, 2, true); err != nil {
			t.Fatal(err)
		}
		if _, err := a.S.AddChat(ctx, c.Space.ID, c.Me.ID, "user", map[string]string{"text": "куда сходить"}, ck.t); err != nil {
			t.Fatal(err)
		}
		if _, err := a.S.AddExample(ctx, c.Me.ID, "куда сходить", "[]", "вот", ck.t); err != nil {
			t.Fatal(err)
		}
		if _, err := a.S.AddPick(ctx, store.Pick{SpaceID: c.Space.ID, MemberID: c.Me.ID, Title: "Подборка", IDs: []int64{l.planet}}, ck.t); err != nil {
			t.Fatal(err)
		}
		if _, err := a.FeedToken(ctx, c, false); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

// «Выйти из пары»: the one who stays keeps everything, the one who leaves gets copies of their own ideas,
// meetings that have not happened are cancelled, and the two can be a pair again without anything doubling.
func TestLeavePair(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	l := lifeOfPair(t, a, ck)
	v, n, space := l.v, l.n, l.v.Space.ID
	token, err := a.NewSession(ctx, v.Me.ID, "telegram", "test")
	if err != nil {
		t.Fatal(err)
	}
	before := ideaSnap(t, a, space)
	choresBefore := count(t, a, `SELECT COUNT(*) FROM chores WHERE space_id=?`, space)

	start := f.n()
	nc, copied, err := a.LeavePair(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if nc.Partner != nil || nc.Me.ID != v.Me.ID || nc.Space.ID == space || copied != 2 {
		t.Fatalf("the leaver is the same member, alone, in a new space: %+v copied=%d", nc.Me, copied)
	}
	if _, _, err := a.LeavePair(ctx, nc); !errors.Is(err, app.ErrNotPaired) {
		t.Fatalf("nothing to leave twice: %v", err)
	}

	// the one who stays: every idea with every mark, as it was — also those the leaver added
	if after := ideaSnap(t, a, space); after != before {
		t.Fatalf("the ideas of the one who stays changed:\n%s\nwas:\n%s", after, before)
	}
	ns, _ := a.Couple(ctx, 2)
	if ns.Partner != nil || ns.Space.ID != space {
		t.Fatalf("the one who stays is alone in the same space: %+v", ns.Me)
	}
	// the history: the meeting that happened, with both ratings
	if r, _ := a.S.Round(ctx, space, l.past.ID); r.State != domain.StateDone || r.AgainA != "no" || r.AgainB != "yes" {
		t.Fatalf("the history stays: %s %q %q", r.State, r.AgainA, r.AgainB)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM photos WHERE round_id=?`, l.past.ID); n != 1 {
		t.Fatalf("the photo stays: %d", n)
	}
	// meetings that have not happened are cancelled, what waited is withdrawn
	for _, id := range []int64{l.future.ID, l.open.ID} {
		if r, _ := a.S.Round(ctx, space, id); r.State != domain.StateCancelled {
			t.Fatalf("meeting %d must be cancelled: %s", id, r.State)
		}
	}
	if n := count(t, a, `SELECT COUNT(*) FROM proposals WHERE space_id=? AND state='wait' AND kind<>'note'`, space); n != 0 {
		t.Fatalf("waiting proposals must be withdrawn: %d", n)
	}
	// she learns it in the two places
	rows := a.Inbox(ctx, ns)
	if len(rows) != 1 || rows[0].Role != "notice" || rows[0].Title != "Вова вышел из пары" || !strings.Contains(rows[0].Sub, "Идеи и история остались у тебя") ||
		!strings.Contains(rows[0].Sub, "Каток в Парке Горького") {
		t.Fatalf("the row of the one who stays: %+v", rows)
	}
	if tx, _ := f.since(start, 2); !strings.Contains(tx, "<b>Вова вышел из пары.</b> Идеи и история остались у тебя") || !strings.Contains(tx, "Каток в Парке Горького") {
		t.Fatalf("the bot tells the one who stays: %q", tx)
	}
	if _, err := a.AnswerProposal(ctx, ns, rows[0].ProposalID, true); err != nil || len(a.Inbox(ctx, ns)) != 0 {
		t.Fatalf("«Понятно» closes the row: %v", err)
	}
	// chores: nothing is gone; the leaver's goes to «кто угодно», the request becomes a plain chore of hers
	if got := count(t, a, `SELECT COUNT(*) FROM chores WHERE space_id=?`, space); got != choresBefore {
		t.Fatalf("chores of the one who stays: %d, were %d", got, choresBefore)
	}
	if ch, _ := a.S.Chore(ctx, space, l.tickets); ch.State != domain.ChoreOpen || ch.Assignee != 0 {
		t.Fatalf("the leaver's chore is for anyone now: %+v", ch)
	}
	if ch, _ := a.S.Chore(ctx, space, l.cake); ch.State != domain.ChoreOpen || ch.Assignee != n.Me.ID || ch.ProposalID != 0 {
		t.Fatalf("a request nobody can answer becomes an open chore: %+v", ch)
	}
	if ch, _ := a.S.Chore(ctx, space, l.garbage); ch.State != domain.ChoreDone || ch.DoneBy != v.Me.ID {
		t.Fatalf("the history of chores stays: %+v", ch)
	}
	if _, sum, _ := a.Chores(ctx, ns); sum.Today+sum.Overdue != 0 {
		t.Fatalf("summary: %+v", sum)
	}

	// the one who left: copies of the ideas he added himself — not hers, not the scout's, not the stop
	mine, _ := a.S.Ideas(ctx, nc.Space.ID, domain.IdeaActive, domain.IdeaDraft, domain.IdeaDone, domain.IdeaArchived)
	got := map[string]store.Idea{}
	for _, i := range mine {
		got[i.Title] = i
	}
	p, r := got["Планетарий на Баррикадной"], got["Каток в Парке Горького"]
	if len(mine) != 2 || p.ID == 0 || r.ID == 0 || p.ID == l.planet || p.AuthorID != v.Me.ID || p.WishA != domain.WishWant || p.WishB != domain.WishNone || p.Note != "взять плед" {
		t.Fatalf("the copies: %+v", mine)
	}
	if r.Note != "" || r.WishA != domain.WishNone || r.WishB != domain.WishNone {
		t.Fatalf("what the partner typed and marked is not copied: %+v", r)
	}
	if cs, _ := a.S.Chores(ctx, nc.Space.ID); len(cs) != 1 || cs[0].Title != "Купить билеты на поезд" || cs[0].Assignee != v.Me.ID {
		t.Fatalf("his chore comes along as a copy: %+v", cs)
	}
	if rs, _ := a.S.RecentRounds(ctx, nc.Space.ID, 10); len(rs) != 0 {
		t.Fatalf("meetings stay with the one who stayed: %d", len(rs))
	}
	// he is the same person: the login works, memory, taste, busy days and the chat are his
	if id, err := a.S.SessionMember(ctx, token, ck.t); err != nil || id != v.Me.ID {
		t.Fatalf("the login of the one who left: %v %d", err, id)
	}
	if c, err := a.Couple(ctx, 1); err != nil || c.Me.ID != v.Me.ID || c.Space.ID != nc.Space.ID {
		t.Fatalf("telegram login: %v", err)
	}
	if ms, _ := a.S.Memories(ctx, nc.Space.ID); len(ms) != 1 || ms[0].MemberID != v.Me.ID {
		t.Fatalf("his memory: %+v", ms)
	}
	if a.Taste(ctx, v.Me.ID).Empty() || len(a.S.BusyDays(ctx, v.Me.ID, "2000-01-01")) != 1 || !a.UsesV2(ctx, v.Me.ID) {
		t.Fatal("taste, busy days and settings stay his")
	}
	if ch, _ := a.S.Chat(ctx, v.Me.ID, 10); len(ch) != 1 {
		t.Fatalf("his chat: %d", len(ch))
	}
	if ms, _ := a.S.Memories(ctx, space); len(ms) != 1 || ms[0].MemberID != n.Me.ID {
		t.Fatalf("her memory is hers only: %+v", ms)
	}

	// a pair again, with each other: she invites, he accepts. Nothing doubles, his marks and chore are back.
	link, err := a.NewInvite(ctx, ns)
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := a.AcceptInvite(ctx, nc, app.InviteCode(link))
	if err != nil || back.Partner == nil || back.Space.ID != space || back.Me.ID != v.Me.ID || back.Me.Color != "a" {
		t.Fatalf("pair again: %v %+v", err, back.Me)
	}
	if after := ideaSnap(t, a, space); after != before {
		t.Fatalf("pairing again must not double or change ideas:\n%s\nwas:\n%s", after, before)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM chores WHERE space_id=? AND title='Купить билеты на поезд'`, space); n != 1 {
		t.Fatalf("the chore must not double: %d", n)
	}
	if ch, _ := a.S.Chore(ctx, space, l.tickets); ch.Assignee != v.Me.ID {
		t.Fatalf("the chore is his again: %+v", ch)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM spaces WHERE id=?`, nc.Space.ID); n != 0 {
		t.Fatal("the space he was alone in is gone")
	}
	if r, _ := a.S.Round(ctx, space, l.past.ID); r.AgainA != "no" || r.AgainB != "yes" {
		t.Fatalf("the history after pairing again: %q %q", r.AgainA, r.AgainB)
	}
	if !a.S.PairedSince(ctx, space).Equal(time.Unix(ck.t.Unix(), 0)) {
		t.Fatalf("the pair counts from today: %v", a.S.PairedSince(ctx, space))
	}

	// the other way round: she leaves, then he invites and she accepts. The space with the history is kept.
	ns, _ = a.Couple(ctx, 2)
	if _, _, err := a.LeavePair(ctx, ns); err != nil {
		t.Fatal(err)
	}
	vs, _ := a.Couple(ctx, 1)
	ns, _ = a.Couple(ctx, 2)
	if vs.Space.ID != space || ns.Space.ID == space || vs.Me.Color != "a" {
		t.Fatalf("he stays, she is in a new space: %d %d", vs.Space.ID, ns.Space.ID)
	}
	link, _ = a.NewInvite(ctx, ns)
	vb, _, err := a.AcceptInvite(ctx, vs, app.InviteCode(link))
	if err != nil || vb.Partner == nil || vb.Space.ID != space {
		t.Fatalf("he accepts her invitation and keeps the history: %v space %d", err, vb.Space.ID)
	}
	if nb, _ := a.Couple(ctx, 2); nb.Space.ID != space || nb.Me.Color != "b" || nb.Me.ID != n.Me.ID {
		t.Fatalf("she is back on her side: %+v", nb.Me)
	}
	if after := ideaSnap(t, a, space); after != before {
		t.Fatalf("after the second round trip:\n%s\nwas:\n%s", after, before)
	}
	if rs, _ := a.S.RecentRounds(ctx, space, 10); len(rs) != 3 {
		t.Fatalf("the meetings are all there: %d", len(rs))
	}
}

// Somebody else takes the place of the one who left: they do not inherit his marks and ratings
func TestLeaveThenNewPartner(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	l := lifeOfPair(t, a, ck)
	space := l.v.Space.ID
	stale, _ := a.NewInvite(ctx, l.v) // a link made while they were a pair
	if _, _, err := a.LeavePair(ctx, l.v); err != nil {
		t.Fatal(err)
	}
	ns, _ := a.Couple(ctx, 2)
	if _, _, err := a.Start(ctx, app.Profile{TGID: 6, ChatID: 6, Name: "Чужой"}, "inv_"+app.InviteCode(stale)); !errors.Is(err, app.ErrInviteUsed) {
		t.Fatalf("a link from before the leaving must not bring anybody in: %v", err)
	}
	if c, _ := a.Couple(ctx, 2); c.Partner != nil {
		t.Fatal("she stays alone")
	}
	// alone, the marks of the one who left are kept but belong to nobody: nothing «совпало»
	link, _ := a.NewInvite(ctx, ns)
	x, _, err := a.Start(ctx, app.Profile{TGID: 5, ChatID: 5, Name: "Олег"}, "inv_"+app.InviteCode(link))
	if err != nil || x.Partner == nil || x.Space.ID != space || x.Me.Color != "a" {
		t.Fatalf("a new partner takes the free side: %v %+v", err, x.Me)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM ideas WHERE space_id=? AND wish_a<>''`, space); n != 0 {
		t.Fatalf("the new partner must not inherit marks: %d", n)
	}
	if i, _ := a.S.Idea(ctx, space, l.planet); i.WishB != domain.WishVery || i.AuthorID != l.v.Me.ID {
		t.Fatalf("her marks and the ideas stay: %+v", i)
	}
	if r, _ := a.S.Round(ctx, space, l.past.ID); r.AgainA != "" || r.AgainB != "yes" || r.State != domain.StateDone {
		t.Fatalf("the new partner must not inherit ratings: %q %q", r.AgainA, r.AgainB)
	}
	// the one who left still has his copies and his own mark
	vs, _ := a.Couple(ctx, 1)
	if is, _ := a.S.Ideas(ctx, vs.Space.ID, domain.IdeaActive, domain.IdeaDraft, domain.IdeaDone); len(is) != 2 {
		t.Fatalf("his copies: %d", len(is))
	}
	// chores done by the one who left are not shown under the new partner's name
	xs, _ := a.Couple(ctx, 5)
	hist, _ := a.ChoreHistory(ctx, xs, "мусор", "")
	if len(hist) != 1 || hist[0].DoneBy != "прежний партнёр" {
		t.Fatalf("history of chores: %+v", hist)
	}
}

// «Скачать мои идеи и историю»: my own things, and nothing the partner marked, rated or typed
func TestExportData(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	l := lifeOfPair(t, a, ck)
	if err := a.SendExport(ctx, l.v); err != nil {
		t.Fatal(err)
	}
	if f.docChat != 1 || !strings.HasSuffix(f.docName, ".json") {
		t.Fatalf("the file goes to the one who asked: chat %d %q", f.docChat, f.docName)
	}
	doc := string(f.doc)
	for _, want := range []string{"Планетарий на Баррикадной", "Каток в Парке Горького", "взять плед", "не очень", "Вынести мусор", "не люблю стендап, Вова", `"моя отметка": "хочу"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("the file must have %q:\n%s", want, doc)
		}
	}
	for _, not := range []string{"Настина заметка", "понравилось", "очень хочу", "Бранч на Патриках", "Помыть окна", "Полить цветы", "не люблю стендап, Настя", "Концерт из афиши"} {
		if strings.Contains(doc, not) {
			t.Fatalf("the file must not have the partner's %q:\n%s", not, doc)
		}
	}
	// hers: her rating and her idea, not his
	if err := a.SendExport(ctx, l.n); err != nil {
		t.Fatal(err)
	}
	doc = string(f.doc)
	if f.docChat != 2 || !strings.Contains(doc, "понравилось") || !strings.Contains(doc, "Бранч на Патриках") || strings.Contains(doc, "не очень") || strings.Contains(doc, "взять плед") {
		t.Fatalf("her file:\n%s", doc)
	}
	// the meeting they went to is shared: its name and date are in both files
	if !strings.Contains(doc, `"что": "Планетарий на Баррикадной"`) {
		t.Fatalf("the shared meeting:\n%s", doc)
	}
}

// «Удалить мои данные»: the partner loses nothing but the name, marks and ratings of the one who is gone;
// no row points at the deleted member; the next /start is a new person.
func TestForgetMe(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	l := lifeOfPair(t, a, ck)
	v, n, space := l.v, l.n, l.v.Space.ID
	token, _ := a.NewSession(ctx, n.Me.ID, "telegram", "test")
	ideasBefore := count(t, a, `SELECT COUNT(*) FROM ideas WHERE space_id=?`, space)
	choresBefore := count(t, a, `SELECT COUNT(*) FROM chores WHERE space_id=?`, space)
	if len(refsTo(t, a, n.Me.ID)) < 10 {
		t.Fatalf("the test must start with her rows in many tables: %v", refsTo(t, a, n.Me.ID))
	}

	if err := a.ForgetMe(ctx, n, "да"); !errors.Is(err, app.ErrNotConfirmed) {
		t.Fatalf("the word must be typed: %v", err)
	}
	if c, _ := a.Couple(ctx, 2); c.Partner == nil {
		t.Fatal("nothing happens without the word")
	}
	start := f.n()
	if err := a.ForgetMe(ctx, n, " Удалить "); err != nil {
		t.Fatal(err)
	}

	// nothing of her is left anywhere
	if bad := refsTo(t, a, n.Me.ID); len(bad) != 0 {
		t.Fatalf("rows still point at the deleted member: %v", bad)
	}
	if _, err := a.S.SessionMember(ctx, token, ck.t); err == nil {
		t.Fatal("her login must be gone")
	}
	if _, err := a.Couple(ctx, 2); !errors.Is(err, app.ErrNoSpace) {
		t.Fatalf("she is nobody now: %v", err)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM proposals WHERE title LIKE '%Настя%' OR sub LIKE '%Настя%'`); n != 0 {
		t.Fatalf("her name is left in %d proposals", n)
	}
	if tx, _ := f.since(start, 2); !strings.Contains(tx, "Твои данные удалены") {
		t.Fatalf("she is told: %q", tx)
	}

	// the partner: every idea and chore is there, hers are his now; her marks and ratings are gone, his stay
	vs, _ := a.Couple(ctx, 1)
	if vs.Partner != nil || vs.Space.ID != space {
		t.Fatalf("he is alone in the same space: %+v", vs.Me)
	}
	if got := count(t, a, `SELECT COUNT(*) FROM ideas WHERE space_id=?`, space); got != ideasBefore {
		t.Fatalf("his ideas: %d, were %d", got, ideasBefore)
	}
	if got := count(t, a, `SELECT COUNT(*) FROM chores WHERE space_id=?`, space); got != choresBefore {
		t.Fatalf("his chores: %d, were %d", got, choresBefore)
	}
	if i, _ := a.S.Idea(ctx, space, l.brunch); i.AuthorID != v.Me.ID || i.WishA != domain.WishSkip || i.WishB != domain.WishNone {
		t.Fatalf("her idea stays with him, without her mark: %+v", i)
	}
	if i, _ := a.S.Idea(ctx, space, l.planet); i.WishA != domain.WishWant || i.WishB != domain.WishNone || i.Note != "взять плед" {
		t.Fatalf("his idea: %+v", i)
	}
	if r, _ := a.S.Round(ctx, space, l.past.ID); r.State != domain.StateDone || r.AgainA != "no" || r.AgainB != "" {
		t.Fatalf("the history stays without her rating: %s %q %q", r.State, r.AgainA, r.AgainB)
	}
	if r, _ := a.S.Round(ctx, space, l.future.ID); r.State != domain.StateCancelled {
		t.Fatalf("the planned meeting is cancelled: %s", r.State)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM photos WHERE round_id=?`, l.past.ID); n != 1 {
		t.Fatalf("the photo of the meeting stays: %d", n)
	}
	if ex, _ := a.S.Extras(ctx, l.future.ID); len(ex) != 1 || ex[0].ProposedBy != v.Me.ID {
		t.Fatalf("the stop she proposed stays in the meeting: %+v", ex)
	}
	if ch, _ := a.S.Chore(ctx, space, l.flowers); ch.State != domain.ChoreOpen || ch.Assignee != 0 || ch.AuthorID != v.Me.ID {
		t.Fatalf("her chore stays for anyone: %+v", ch)
	}
	hist, _ := a.ChoreHistory(ctx, vs, "окна", "")
	if len(hist) != 1 || hist[0].DoneBy != "прежний партнёр" {
		t.Fatalf("what she did stays in the history without her name: %+v", hist)
	}
	rows := a.Inbox(ctx, vs)
	if len(rows) != 1 || rows[0].Title != "Партнёр вышел из пары и удалил свои данные" || !strings.Contains(rows[0].Sub, "Идеи и история остались у тебя") {
		t.Fatalf("his row: %+v", rows)
	}
	if tx, _ := f.since(start, 1); !strings.Contains(tx, "Партнёр вышел из пары и удалил свои данные") || strings.Contains(tx, "Настя") {
		t.Fatalf("the bot tells him without her name: %q", tx)
	}
	if ms, _ := a.S.Memories(ctx, space); len(ms) != 1 || ms[0].MemberID != v.Me.ID {
		t.Fatalf("his memory: %+v", ms)
	}
	if a.Taste(ctx, v.Me.ID).Empty() || len(a.S.BusyDays(ctx, v.Me.ID, "2000-01-01")) != 1 {
		t.Fatal("his taste and busy days are his")
	}

	// /start again: a new person with nothing — even though the row id may be given out again
	c, link, err := a.Start(ctx, app.Profile{TGID: 2, ChatID: 2, Name: "Настя"}, "")
	if err != nil || c.Partner != nil || c.Space.ID == space || !strings.Contains(link, "inv_") {
		t.Fatalf("start as new: %v %+v", err, c.Me)
	}
	if is, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDraft, domain.IdeaDone, domain.IdeaArchived); len(is) != 0 {
		t.Fatalf("a new person has no ideas: %d", len(is))
	}
	if cs, _ := a.S.Chores(ctx, c.Space.ID); len(cs) != 0 {
		t.Fatalf("a new person has no chores: %+v", cs)
	}
	if !a.Taste(ctx, c.Me.ID).Empty() || a.S.MemberSettings(ctx, c.Me.ID)["sys.ui"] != "" || len(a.Inbox(ctx, c)) != 0 || len(a.S.BusyDays(ctx, c.Me.ID, "2000-01-01")) != 0 || a.Subscribed(ctx, c.Me.ID) {
		t.Fatal("a new person inherits nothing")
	}
	if ch, _ := a.S.Chat(ctx, c.Me.ID, 10); len(ch) != 0 {
		t.Fatalf("no chat: %d", len(ch))
	}
	// and the two can be a pair again: he keeps the history, she starts clean
	back, _, err := a.AcceptInvite(ctx, vs, app.InviteCode(link))
	if err != nil || back.Partner == nil || back.Space.ID != space {
		t.Fatalf("a pair again: %v space %d", err, back.Space.ID)
	}
	if n := count(t, a, `SELECT COUNT(*) FROM ideas WHERE space_id=? AND wish_b<>''`, space); n != 0 {
		t.Fatalf("she starts without marks: %d", n)
	}

	// someone alone deletes themselves: their space goes with them
	x, _, _ := a.Start(ctx, app.Profile{TGID: 7, ChatID: 7, Name: "Олег"}, "")
	if _, err := a.AddIdea(ctx, x, "Баня на выходных"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddChore(ctx, x, app.ChoreIn{Title: "Купить веник", Who: "me"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ForgetMe(ctx, x, "удалить"); err != nil {
		t.Fatal(err)
	}
	if bad := refsTo(t, a, x.Me.ID); len(bad) != 0 {
		t.Fatalf("rows of the one who was alone: %v", bad)
	}
	for _, tb := range []string{"spaces WHERE id", "ideas WHERE space_id", "chores WHERE space_id", "invites WHERE space_id", "proposals WHERE space_id", "space_settings WHERE space_id"} {
		if n := count(t, a, `SELECT COUNT(*) FROM `+tb+`=?`, x.Space.ID); n != 0 {
			t.Fatalf("%s: %d rows left", tb, n)
		}
	}
	if got := count(t, a, `SELECT COUNT(*) FROM ideas WHERE space_id=?`, space); got != ideasBefore {
		t.Fatalf("somebody else's deletion touched the pair: %d", got)
	}
}

// The one who stayed alone after a partner left deletes their data: the space they kept goes, the one who
// left keeps his copies and his login
func TestForgetAfterPartnerLeft(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	l := lifeOfPair(t, a, ck)
	space := l.v.Space.ID
	nc, _, err := a.LeavePair(ctx, l.v)
	if err != nil {
		t.Fatal(err)
	}
	ns, _ := a.Couple(ctx, 2)
	if err := a.ForgetMe(ctx, ns, "удалить"); err != nil {
		t.Fatal(err)
	}
	if bad := refsTo(t, a, l.n.Me.ID); len(bad) != 0 {
		t.Fatalf("rows still point at her: %v", bad)
	}
	for _, tb := range []string{"spaces WHERE id", "ideas WHERE space_id", "rounds WHERE space_id", "chores WHERE space_id", "proposals WHERE space_id"} {
		if n := count(t, a, `SELECT COUNT(*) FROM `+tb+`=?`, space); n != 0 {
			t.Fatalf("%s: %d rows left", tb, n)
		}
	}
	vs, err := a.Couple(ctx, 1)
	if err != nil || vs.Space.ID != nc.Space.ID {
		t.Fatalf("he is where he was: %v", err)
	}
	if is, _ := a.S.Ideas(ctx, vs.Space.ID, domain.IdeaActive, domain.IdeaDraft, domain.IdeaDone); len(is) != 2 {
		t.Fatalf("his copies are his: %d", len(is))
	}
	// he deletes himself later: the marks he left in her space are gone with it, nothing fails
	if err := a.ForgetMe(ctx, vs, "удалить"); err != nil {
		t.Fatal(err)
	}
	if bad := refsTo(t, a, l.v.Me.ID); len(bad) != 0 {
		t.Fatalf("rows still point at him: %v", bad)
	}
}

// «Ждёт тебя» asks about the meeting itself: who buys the tickets while it is ahead, how it went when it has passed
func TestMeetRowsInWaiting(t *testing.T) {
	a, _, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	res, _ := a.AddIdea(ctx, n, "Планетарий на Баррикадной")
	id := res.Idea.ID
	_, _ = a.SetWish(ctx, n, id, domain.WishWant)
	_, _ = a.SetWish(ctx, v, id, domain.WishWant)
	r, err := a.GoWith(ctx, v, id)
	if err != nil {
		t.Fatal(err)
	}
	if rows := a.MeetRows(ctx, v); len(rows) != 0 {
		t.Fatalf("no time yet, nothing to ask: %+v", rows)
	}
	at := ck.t.Add(48 * time.Hour).Truncate(time.Hour)
	p, _ := a.ProposeTime(ctx, v, r.ID, at)
	if _, err := a.AnswerProposal(ctx, n, p.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, c := range []app.Couple{v, n} {
		rows := a.Waiting(ctx, c)
		if len(rows) != 1 || rows[0].Kind != "tickets" || rows[0].Title != "Билеты: Планетарий на Баррикадной" || rows[0].Hot || rows[0].RefID != r.ID {
			t.Fatalf("both are asked who buys: %+v", rows)
		}
	}
	if len(a.Inbox(ctx, v)) != 0 {
		t.Fatal("the rows of meetings are not proposals")
	}
	// «Куплю я»: it is on him now, she has nothing to do
	if err := a.S.SetSpaceSetting(ctx, v.Space.ID, fmt.Sprintf("sys.buy:%d", r.ID), fmt.Sprint(v.Me.ID)); err != nil {
		t.Fatal(err)
	}
	if rows := a.Waiting(ctx, v); len(rows) != 1 || rows[0].Title != "Купить билеты: Планетарий на Баррикадной" || !rows[0].Hot || rows[0].Actions[0] != "bought" {
		t.Fatalf("the buyer: %+v", rows)
	}
	if rows := a.Waiting(ctx, n); len(rows) != 0 {
		t.Fatalf("the partner of the buyer: %+v", rows)
	}
	if err := a.GotTickets(ctx, v, r.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.Waiting(ctx, v))+len(a.Waiting(ctx, n)) != 0 {
		t.Fatal("the tickets are bought: nothing waits")
	}
	// the meeting has passed: each is asked, each answers for themselves
	ck.t = at.Add(3 * time.Hour)
	for _, c := range []app.Couple{v, n} {
		rows := a.Waiting(ctx, c)
		if len(rows) != 1 || rows[0].Kind != "after" || rows[0].Title != "Как прошло: Планетарий на Баррикадной?" || !rows[0].Hot {
			t.Fatalf("after the meeting: %+v", rows)
		}
	}
	if err := a.Rate(ctx, v, r.ID, "like"); err != nil {
		t.Fatal(err)
	}
	if len(a.Waiting(ctx, v)) != 0 {
		t.Fatal("he has answered")
	}
	if rows := a.Waiting(ctx, n); len(rows) != 1 || rows[0].Kind != "after" {
		t.Fatalf("she is still asked after his answer: %+v", rows)
	}
	if err := a.Rate(ctx, n, r.ID, "skip"); err != nil || len(a.Waiting(ctx, n)) != 0 {
		t.Fatalf("she has answered: %v", err)
	}
	// two weeks later nobody is asked about an unanswered meeting
	ck.t = at.Add(15 * 24 * time.Hour)
	if len(a.MeetRows(ctx, v)) != 0 {
		t.Fatal("too old to ask")
	}
}

// «Вместе»: a chore of the two of them — the partner learns about it, either ticks it, both see it as theirs;
// leaving the pair gives the leaver a copy and leaves the original for anyone
func TestChoreTogether(t *testing.T) {
	a, f, ck := setup(t)
	ctx := context.Background()
	v, n := pair(t, a)
	today := ck.t
	ch, err := a.AddChore(ctx, v, app.ChoreIn{Title: "Генеральная уборка", Who: "both", Due: &today})
	if err != nil {
		t.Fatal(err)
	}
	if ch.State != domain.ChoreOpen || ch.Assignee != domain.ChoreTogether {
		t.Fatalf("open and on both, not a request: %+v", ch)
	}
	if !strings.Contains(f.last(2), "на вас обоих") || !strings.Contains(f.last(2), "Генеральная уборка") {
		t.Fatalf("the partner is told: %q", f.last(2))
	}
	for _, c := range []app.Couple{v, n} {
		list, s, _ := a.Chores(ctx, c)
		if len(list) != 1 || list[0].Who != "both" || list[0].WhoName != "вместе" || len(list[0].Actions) != 1 || list[0].Actions[0] != "done" || s.Today != 1 {
			t.Fatalf("both see it as theirs: %+v %+v", list, s)
		}
	}
	if _, err := a.ChoreAct(ctx, n, ch.ID, "done"); err != nil {
		t.Fatalf("either ticks it: %v", err)
	}
	if h, _ := a.ChoreHistory(ctx, v, "", "partner"); len(h) != 1 || h[0].Title != "Генеральная уборка" {
		t.Fatalf("history: who ticked it: %+v", h)
	}
	// edited into «вместе» and back; nobody can be asked for it without a partner
	two, _ := a.AddChore(ctx, v, app.ChoreIn{Title: "Разобрать балкон", Who: "me"})
	if e, err := a.EditChore(ctx, v, two.ID, app.ChoreIn{Title: "Разобрать балкон", Who: "both"}, false); err != nil || e.Assignee != domain.ChoreTogether {
		t.Fatalf("edit to both: %+v %v", e, err)
	}
	// leaving: the leaver takes a copy, the one who stays keeps it for anyone
	v2, _, err := a.LeavePair(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	n2, _ := a.CoupleByMember(ctx, n.Me.ID)
	if list, _, _ := a.Chores(ctx, n2); len(list) < 1 || list[0].Title != "Разобрать балкон" || list[0].Who != "any" {
		t.Fatalf("the one who stays: %+v", list)
	}
	if list, _, _ := a.Chores(ctx, v2); len(list) != 1 || list[0].Title != "Разобрать балкон" || list[0].Who != "me" {
		t.Fatalf("the leaver's copy: %+v", list)
	}
	if _, err := a.AddChore(ctx, v2, app.ChoreIn{Title: "Вдвоём без пары", Who: "both"}); !errors.Is(err, app.ErrNotPaired) {
		t.Fatalf("no partner, no «вместе»: %v", err)
	}
}
