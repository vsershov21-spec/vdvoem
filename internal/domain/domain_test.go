package domain

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

var msk = func() *time.Location { l, _ := time.LoadLocation("Europe/Moscow"); return l }()

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, msk) }

func TestCommonWindowsMergesAdjacent(t *testing.T) {
	sat := day(2026, 11, 14)
	a := []Slot{{sat, 1}, {sat, 2}, {day(2026, 11, 15), 0}}
	b := []Slot{{sat, 2}, {sat, 1}}
	w := CommonWindows(a, b, msk)
	if len(w) != 1 {
		t.Fatalf("want 1 window, got %d", len(w))
	}
	if w[0].From.Hour() != 13 || w[0].To.Hour() != 23 || w[0].Minutes() != 600 {
		t.Fatalf("bad window %v–%v", w[0].From, w[0].To)
	}
	if len(CommonWindows(a, nil, msk)) != 0 {
		t.Fatal("no overlap must give no windows")
	}
}

func idea(id int64, cat string, a, b Wish) Idea {
	return Idea{ID: id, Title: "idea", Kind: KindLeisure, Status: IdeaActive, Category: cat, WishA: a, WishB: b,
		DurationMin: 120, CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, msk)}
}

func TestPickTriple(t *testing.T) {
	now := time.Date(2026, 11, 12, 19, 0, 0, 0, msk)
	win := []Window{{time.Date(2026, 11, 14, 18, 0, 0, 0, msk), time.Date(2026, 11, 14, 23, 0, 0, 0, msk)}}
	long := idea(9, "food", WishVery, WishVery)
	long.DurationMin = 400 // does not fit a 5 h window
	expensive := idea(10, "food", WishVery, WishVery)
	expensive.PriceMin = 900000
	met := idea(11, "walk", WishVery, WishVery)
	mt := now.Add(-5 * 24 * time.Hour)
	met.LastMetAt = &mt
	chore := idea(12, "home", WishVery, WishVery)
	chore.Kind = KindChore
	ideas := []Idea{
		idea(1, "food", WishVery, WishWant),    // 5
		idea(2, "food", WishVery, WishVery),    // 6 — best
		idea(3, "culture", WishWant, WishNone), // 2 — other category
		idea(4, "food", WishSkip, WishVery),    // filtered: skip
		idea(5, "walk", WishWant, WishWant),    // 4
		long, expensive, met, chore,
	}
	got := PickTriple(PickInput{Ideas: ideas, Windows: win, BudgetKop: 600000, Now: now})
	ids := []int64{}
	for _, s := range got {
		ids = append(ids, s.Idea.ID)
	}
	want := []int64{2, 5, 1} // best; best of another category (walk 4 beats culture 2); best of the rest
	if len(ids) != 3 || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("triple %v, want %v", ids, want)
	}
	if got[0].Score != 6 {
		t.Fatalf("score %d", got[0].Score)
	}
	if Why(got[0]) != "Вы оба очень хотите, и время подходит." {
		t.Fatalf("why: %s", Why(got[0]))
	}
	// excluded ideas never come back in «Другая тройка»
	again := PickTriple(PickInput{Ideas: ideas, Windows: win, BudgetKop: 600000, Now: now, Exclude: map[int64]bool{2: true, 5: true, 1: true}})
	if len(again) != 1 || again[0].Idea.ID != 3 {
		t.Fatalf("reshuffle got %v", again)
	}
}

func TestScoreSignals(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, msk)
	i := idea(1, "culture", WishNone, WishNone)
	end := now.Add(10 * 24 * time.Hour)
	i.EndsAt = &end
	i.CreatedAt = now.Add(-40 * 24 * time.Hour)
	i.AgainNo = 1
	s, sig := Score(i, now)
	if s != 2+1-3 || !strings.Contains(strings.Join(sig, ","), "ends_soon") {
		t.Fatalf("score %d %v", s, sig)
	}
}

func TestSilenceIsNotConsent(t *testing.T) {
	now := time.Date(2026, 11, 12, 19, 0, 0, 0, msk)
	r := NewRound(1, now, 0)
	if both, _ := r.SubmitSlots("a", []Slot{{day(2026, 11, 14), 2}}, now); both {
		t.Fatal("one answer is not both")
	}
	r.SubmitSlots("b", []Slot{{day(2026, 11, 14), 2}}, now)
	if err := r.StartVoting([]int64{7, 8, 9}); err != nil || r.State != StateVote {
		t.Fatal(err, r.State)
	}
	if m, _ := r.Vote("a", 7, now); m {
		t.Fatal("single vote matched")
	}
	if _, err := r.Vote("b", 42, now); err != ErrNotInTriple {
		t.Fatal("vote outside triple accepted")
	}
	if m, _ := r.Vote("b", 7, now); !m || r.State != StateConfirm || r.Chosen != 7 {
		t.Fatal("matched vote did not move to confirm")
	}
	if p, _ := r.Confirm("a", now); p || r.State != StateConfirm {
		t.Fatal("one confirmation planned the meeting")
	}
	// Nobody else answers: after 48 h the round closes, no meeting.
	if !r.Tick(now.Add(48*time.Hour)) || r.State != StateExpired {
		t.Fatal("silence did not expire the round")
	}
	if _, err := r.Confirm("b", now.Add(49*time.Hour)); err != ErrClosed {
		t.Fatal("late confirmation accepted")
	}
}

func TestTwoConfirmationsPlan(t *testing.T) {
	now := time.Date(2026, 11, 12, 19, 0, 0, 0, msk)
	r := NewRound(1, now, 0)
	r.SubmitSlots("a", nil, now)
	r.SubmitSlots("b", nil, now)
	r.StartVoting([]int64{1})
	r.Vote("a", 1, now)
	r.Vote("b", 1, now)
	r.Confirm("b", now)
	if p, _ := r.Confirm("a", now); !p || r.State != StatePlanned {
		t.Fatal("two confirmations must plan")
	}
	if err := r.OtherTime(now); err != nil || r.State != StateTime || r.Chosen != 1 {
		t.Fatal("other time must keep the idea and reopen time")
	}
}

func TestReminders(t *testing.T) {
	now := time.Date(2026, 11, 12, 19, 0, 0, 0, msk)
	r := NewRound(1, now, 0)
	r.SubmitSlots("a", nil, now)
	if r.ReminderDue(now.Add(11 * time.Hour)) {
		t.Fatal("too early")
	}
	if !r.ReminderDue(now.Add(12 * time.Hour)) {
		t.Fatal("12h reminder missing")
	}
	r.Reminders = 1
	if r.ReminderDue(now.Add(20*time.Hour)) || !r.ReminderDue(now.Add(36*time.Hour)) {
		t.Fatal("36h reminder wrong")
	}
	r.Reminders = 2
	if r.ReminderDue(now.Add(47 * time.Hour)) {
		t.Fatal("more than two reminders")
	}
	if w := r.Waiting(); len(w) != 1 || w[0] != "b" {
		t.Fatalf("waiting %v", w)
	}
}

func TestQuietHours(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 14, h, 30, 0, 0, msk) }
	if got := QuietShift(at(2), msk); got.Hour() != 9 || got.Day() != 14 {
		t.Fatalf("02:30 → %v", got)
	}
	if got := QuietShift(at(23), msk); got.Hour() != 9 || got.Day() != 15 {
		t.Fatalf("23:30 → %v", got)
	}
	if got := QuietShift(at(15), msk); !got.Equal(at(15)) {
		t.Fatal("day time moved")
	}
}

func TestICS(t *testing.T) {
	st := time.Date(2026, 11, 15, 19, 30, 0, 0, msk)
	e := ICS{UID: "m42@vdvoem", Sequence: 1, Title: "Ужин на Покровке, 2/1", Location: "Покровка, 2/1", Start: st, End: st.Add(2 * time.Hour), Stamp: st}
	s := e.Render()
	for _, want := range []string{"UID:m42@vdvoem", "SEQUENCE:1", "DTSTART:20261115T163000Z", "SUMMARY:Ужин на Покровке\\, 2/1", "TRIGGER:-PT2H", "\r\n"} {
		if !strings.Contains(s, want) {
			t.Fatalf("ics misses %q:\n%s", want, s)
		}
	}
	for _, line := range strings.Split(s, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line too long: %q", line)
		}
	}
}

func TestFingerprintAndSimilarity(t *testing.T) {
	a := Fingerprint("https://www.yandex.ru/maps/org/khachapuri/123/?utm_source=x&ll=37,55", "")
	b := Fingerprint("https://yandex.ru/maps/org/khachapuri/123", "")
	if a != b {
		t.Fatalf("%s != %s", a, b)
	}
	if Similarity("Хачапури и вино", "хачапури и вино!") < 0.99 {
		t.Fatal("same title differs")
	}
	if Similarity("Хачапури на Покровке", "Хачапури и вино") < SimilarThreshold {
		t.Fatalf("similar titles not caught: %.2f", Similarity("Хачапури на Покровке", "Хачапури и вино"))
	}
	if Similarity("Выставка в Пушкинском", "Хачапури и вино") > 0.2 {
		t.Fatal("different titles look similar")
	}
}

func TestMoneyAndWhen(t *testing.T) {
	if Money(400000) != "4\u202f000\u00a0₽" {
		t.Fatalf("money %q", Money(400000))
	}
	if When(time.Date(2026, 11, 14, 19, 30, 0, 0, msk), msk) != "Сб, 14 ноя · 19:30" {
		t.Fatal(When(time.Date(2026, 11, 14, 19, 30, 0, 0, msk), msk))
	}
}

func TestPickOne(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -40)
	ideas := []Idea{
		{ID: 1, Kind: KindLeisure, Status: IdeaActive, Category: "food", CreatedAt: old, WishA: WishVery, WishB: WishVery},
		{ID: 2, Kind: KindLeisure, Status: IdeaActive, Category: "walk", CreatedAt: now},
		{ID: 3, Kind: KindLeisure, Status: IdeaActive, Category: "walk", CreatedAt: now, WishA: WishSkip},
	}
	in := PickInput{Ideas: ideas, Now: now}
	best, ok := PickOne(in, SuggestBest, nil)
	if !ok || best.Idea.ID != 1 {
		t.Fatalf("best = %+v", best.Idea.ID)
	}
	rng := rand.New(rand.NewSource(7))
	count := map[int64]int{}
	for k := 0; k < 2000; k++ {
		s, _ := PickOne(in, SuggestRandom, rng)
		count[s.Idea.ID]++
	}
	if count[3] != 0 {
		t.Fatal("skipped idea must never come up")
	}
	if count[2] == 0 || count[1] <= count[2] {
		t.Fatalf("weighting off: %v", count)
	}
	s, _ := PickOne(in, SuggestRandom, rand.New(rand.NewSource(1)))
	if !strings.HasPrefix(Why(s), "Выпало случайно") {
		t.Fatal(Why(s))
	}
	if SuggestMix.Resolve(now) == SuggestMix {
		t.Fatal("mix must resolve")
	}
}

func TestRoute(t *testing.T) {
	msk, _ := time.LoadLocation("Europe/Moscow")
	end := time.Date(2026, 11, 14, 16, 30, 0, 0, msk) // museum 14:00–16:30
	win := time.Date(2026, 11, 14, 23, 0, 0, 0, msk)
	legs := Route(end, []int{0, 120}, win, msk)
	if got := legs[0].Start.Format("15:04"); got != "17:00" || legs[0].End.Format("15:04") != "18:30" || legs[0].Tight {
		t.Fatalf("dinner: %v", legs[0])
	}
	if legs[1].Start.Format("15:04") != "19:00" || legs[1].Tight {
		t.Fatalf("second: %v", legs[1])
	}
	late := Route(time.Date(2026, 11, 14, 21, 10, 0, 0, msk), []int{120}, win, msk)
	if late[0].Start.Format("15:04") != "21:45" || !late[0].Tight {
		t.Fatalf("late stop must be tight and start on a quarter: %v", late[0])
	}
}
