package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestChoreSections(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, loc)
	day := func(d int) *time.Time { x := time.Date(2026, 11, d, 0, 0, 0, 0, loc); return &x }
	at := func(d, h int) *time.Time { x := time.Date(2026, 11, d, h, 0, 0, 0, loc); return &x }
	for _, c := range []struct {
		name string
		ch   Chore
		want string
	}{
		{"a date lasts until the end of its day", Chore{State: ChoreOpen, Due: day(14)}, "today"},
		{"yesterday's date is overdue", Chore{State: ChoreOpen, Due: day(13)}, "overdue"},
		{"a time that has passed today is overdue", Chore{State: ChoreOpen, Due: at(14, 12), DueTime: true}, "overdue"},
		{"a time later today", Chore{State: ChoreOpen, Due: at(14, 19), DueTime: true}, "today"},
		{"next week", Chore{State: ChoreOpen, Due: day(20)}, "later"},
		{"no deadline", Chore{State: ChoreOpen}, "later"},
		{"asked — waits for an answer", Chore{State: ChoreAsk, Due: day(13)}, "ask"},
		{"declined — back with the author", Chore{State: ChoreDeclined}, "ask"},
		{"done", Chore{State: ChoreDone, Due: day(13)}, "done"},
		{"deleted — nowhere", Chore{State: ChoreDeleted}, ""},
	} {
		if got := c.ch.Section(now, loc); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestChoreActionsByRole(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, loc)
	old := time.Date(2026, 11, 12, 0, 0, 0, 0, loc)
	const me, partner = 1, 2
	for _, c := range []struct {
		name string
		ch   Chore
		want []string
	}{
		{"mine", Chore{State: ChoreOpen, Assignee: me}, []string{"done", "pass"}},
		{"the partner's: remind or take over, never tick for them", Chore{State: ChoreOpen, Assignee: partner}, []string{"nudge", "take"}},
		{"anyone's", Chore{State: ChoreOpen}, []string{"take", "done"}},
		{"mine and overdue", Chore{State: ChoreOpen, Assignee: me, Due: &old}, []string{"done", "pass", "move"}},
		{"asked of me", Chore{State: ChoreAsk, AuthorID: partner, Assignee: me}, []string{"take", "decline"}},
		{"I asked", Chore{State: ChoreAsk, AuthorID: me, Assignee: partner}, []string{"withdraw"}},
		{"declined, back with me", Chore{State: ChoreDeclined, AuthorID: me}, []string{"self", "any"}},
		{"done", Chore{State: ChoreDone}, []string{"reopen"}},
	} {
		if got := c.ch.Actions(me, now, loc); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if (Chore{State: ChoreOpen, Assignee: partner}).CanTick(me) {
		t.Error("the partner's chore is not ticked for them")
	}
	if !(Chore{State: ChoreOpen}).CanTick(me) || !(Chore{State: ChoreOpen, Assignee: me}).CanTick(me) {
		t.Error("mine and anyone's can be ticked")
	}
}

func TestChoreRepeatAndHistory(t *testing.T) {
	loc := time.UTC
	d := func(m time.Month, day int) *time.Time { x := time.Date(2026, m, day, 0, 0, 0, 0, loc); return &x }
	if n := (Chore{Due: d(11, 20), Repeat: RepeatMonth}).NextDue(loc); n == nil || !n.Equal(*d(12, 20)) {
		t.Errorf("monthly: %v", n)
	}
	if n := (Chore{Due: d(1, 31), Repeat: RepeatMonth}).NextDue(loc); n == nil || !n.Equal(*d(2, 28)) {
		t.Errorf("the 31st in February is its last day: %v", n)
	}
	if n := (Chore{Due: d(11, 14), Repeat: RepeatWeek}).NextDue(loc); n == nil || !n.Equal(*d(11, 21)) {
		t.Errorf("weekly: %v", n)
	}
	if (Chore{Due: d(11, 14)}).NextDue(loc) != nil || (Chore{Repeat: RepeatWeek}).NextDue(loc) != nil {
		t.Error("no repeat or no date — no next occurrence")
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, loc)
	done := func(daysAgo int) Chore { x := now.AddDate(0, 0, -daysAgo); return Chore{State: ChoreDone, DoneAt: &x} }
	if !done(6).InTab(now, 7) || done(7).InTab(now, 7) {
		t.Error("done chores stay in the tab for the kept days, then only in the history")
	}
	if !done(20).InTab(now, 30) {
		t.Error("the couple can keep them for 30 days")
	}
}

func TestChoreSummary(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, loc)
	d := func(day int) *time.Time { x := time.Date(2026, 11, day, 0, 0, 0, 0, loc); return &x }
	cs := []Chore{
		{Title: "Билеты на поезд", State: ChoreOpen, Assignee: 1, Due: d(14)},
		{Title: "Химчистка", State: ChoreOpen, Assignee: 1, Due: d(12)},
		{Title: "Стоматолог", State: ChoreOpen, Assignee: 2, Due: d(14)}, // the partner's — not in my line
		{Title: "Посылка", State: ChoreOpen, Due: d(20)},                 // later
		{Title: "Вода", State: ChoreOpen, Due: d(14)},                    // anyone's, today
		{Title: "Торт", State: ChoreAsk, Assignee: 1, Due: d(14)},        // not answered yet
	}
	s := SummarizeChores(cs, 1, now, loc)
	if s.Today != 2 || s.Overdue != 1 || !reflect.DeepEqual(s.Titles, []string{"Химчистка", "Билеты на поезд", "Вода"}) {
		t.Fatalf("summary: %+v", s)
	}
	if e := SummarizeChores(nil, 1, now, loc); e.Titles == nil || e.Today != 0 {
		t.Fatalf("empty summary keeps an empty list: %+v", e)
	}
}

// «Вместе»: either of the two ticks it, nobody takes it over or passes it on, it is on both summaries
func TestChoreTogether(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc)
	due := now.Add(2 * time.Hour)
	c := Chore{ID: 1, AuthorID: 1, Assignee: ChoreTogether, Title: "Генеральная уборка", Due: &due, DueTime: true, State: ChoreOpen}
	for _, me := range []int64{1, 2} {
		if !c.CanTick(me) {
			t.Fatalf("%d cannot tick", me)
		}
		if a := c.Actions(me, now, loc); len(a) != 1 || a[0] != "done" {
			t.Fatalf("actions of %d: %v", me, a)
		}
		if s := SummarizeChores([]Chore{c}, me, now, loc); s.Today != 1 {
			t.Fatalf("summary of %d: %+v", me, s)
		}
	}
	late := now.Add(3 * time.Hour)
	if a := c.Actions(2, late, loc); len(a) != 2 || a[1] != "move" {
		t.Fatalf("overdue: %v", a)
	}
}
