package domain

import (
	"errors"
	"testing"
	"time"
)

// «Как прошло»: each answers for themselves, and one answer never decides for the two
func TestRateEachForThemselves(t *testing.T) {
	planned := func() Round { return Round{State: StatePlanned, StartsAt: time.Unix(1000, 0)} }

	// «Не ходили» of one leaves the meeting as it is; the partner who went makes it «прошла», both marks stay
	r := planned()
	if ch, err := r.Rate("a", "skip"); err != nil || !ch || r.State != StatePlanned || r.AgainA != AgainSkip {
		t.Fatalf("one «не ходили» must not close or cancel the meeting: %v %s %q", err, r.State, r.AgainA)
	}
	if ch, err := r.Rate("b", "like"); err != nil || !ch || r.State != StateDone || r.AgainB != "yes" || r.AgainA != AgainSkip {
		t.Fatalf("the partner went and liked it: %v %s %q %q", err, r.State, r.AgainA, r.AgainB)
	}

	// the other order: «прошла» stays «прошла» when the second says «не ходили»
	r = planned()
	_, _ = r.Rate("b", "soso")
	if _, err := r.Rate("a", "skip"); err != nil || r.State != StateDone || r.AgainA != AgainSkip || r.AgainB != "no" {
		t.Fatalf("a later «не ходили» cancels nothing: %v %s %q %q", err, r.State, r.AgainA, r.AgainB)
	}

	// both «не ходили»: still not cancelled by anybody — the meeting stays in the history with both answers
	r = planned()
	_, _ = r.Rate("a", "skip")
	_, _ = r.Rate("b", "skip")
	if r.State != StatePlanned || r.AgainA != AgainSkip || r.AgainB != AgainSkip {
		t.Fatalf("both skipped: %s %q %q", r.State, r.AgainA, r.AgainB)
	}

	// the first answer of a person stands: the same again changes nothing, another one is refused
	r = planned()
	_, _ = r.Rate("a", "like")
	v := r.Version
	if ch, err := r.Rate("a", "like"); err != nil || ch || r.Version != v {
		t.Fatalf("the same answer twice is one answer: %v %v", ch, err)
	}
	if _, err := r.Rate("a", "skip"); !errors.Is(err, ErrWrongState) || r.AgainA != "yes" {
		t.Fatalf("an answer is not rewritten: %v %q", err, r.AgainA)
	}
	if _, err := r.Rate("a", "maybe"); !errors.Is(err, ErrBadRate) {
		t.Fatalf("an unknown answer: %v", err)
	}
	c := Round{State: StateCancelled}
	if _, err := c.Rate("a", "like"); !errors.Is(err, ErrWrongState) {
		t.Fatalf("a cancelled meeting is not rated: %v", err)
	}
}

// «Когда напоминать о встрече»: two hours ahead, a day ahead, or both — for each one by their own setting
func TestRemindAt(t *testing.T) {
	for _, c := range []struct {
		when, lead string
		want       bool
	}{
		{"2h", "", true}, {"2h", "day", false},
		{"day", "", false}, {"day", "day", true},
		{"both", "", true}, {"both", "day", true},
		{"", "", true}, {"", "day", false}, // no setting: as before, two hours ahead
	} {
		if got := RemindAt(c.when, c.lead); got != c.want {
			t.Errorf("RemindAt(%q, %q) = %v, want %v", c.when, c.lead, got, c.want)
		}
	}
}

// the new interface says «идеи»; the word «копилка» of the old screens does not reach it
func TestNewWords(t *testing.T) {
	for in, want := range map[string]string{
		"Сохранил в копилку:\n\n<b>Театр</b>":                           "Сохранил в идеи:\n\n<b>Театр</b>",
		"Это уже есть в копилке: <b>Театр</b>.":                         "Это уже есть в идеях: <b>Театр</b>.",
		"🎲 Случайная идея из вашей копилки:":                            "🎲 Случайная идея из ваших идей:",
		"Новое в копилке:\n• Каток":                                     "Новое в идеях:\n• Каток",
		"В копилке пока нечего предложить.":                             "В идеях пока нечего предложить.",
		"Добавить в копилку":                                            "Добавить в идеи",
		"Не нашёл «джаз» ни в афише, ни в копилке.":                     "Не нашёл «джаз» ни в афише, ни в идеях.",
		"Повторим — идея вернулась в копилку с пометкой «понравилось».": "Повторим — идея вернулась в идеи с пометкой «понравилось».",
		"Встреча в планах.":                                             "Встреча в планах.",
	} {
		if got := NewWords(in); got != want {
			t.Errorf("NewWords(%q) = %q, want %q", in, got, want)
		}
	}
}
