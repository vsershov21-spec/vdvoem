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
}
