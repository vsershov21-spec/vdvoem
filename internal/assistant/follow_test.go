package assistant

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// a dialogue keeps its point: «хочу в бар в субботу» → «а с караоке?» → «нет, лучше кофейня» → «до 3000»
func TestFollowUpsKeepTheRequest(t *testing.T) {
	now := time.Date(2026, 11, 10, 20, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	step := func(prev Intent, text string) Intent { return mergeFollowUp(prev, rulesIntent(text, now), text) }
	bar := withKinds(rulesIntent("хочу в бар в субботу", now), "хочу в бар в субботу")
	if bar.I != "find" || !slices.Contains(bar.G, "бар") || bar.Date == "" {
		t.Fatalf("start: %+v", bar)
	}
	k := step(bar, "а с караоке?")
	if !slices.Contains(k.G, "бар") || !slices.Contains(k.G, "караоке") || k.Date != bar.Date {
		t.Errorf("add a kind: %+v", k)
	}
	c := step(k, "нет, лучше кофейня")
	if !slices.Equal(c.G, []string{"кофейня"}) || c.Date != bar.Date {
		t.Errorf("swap the kind: %+v", c)
	}
	m := step(c, "до 3000 на двоих")
	if m.Max != 3000 || !slices.Equal(m.G, []string{"кофейня"}) || m.Date != bar.Date {
		t.Errorf("a budget: %+v", m)
	}
	n := step(m, "а без кофеен")
	if len(n.NotW) == 0 || n.Date != bar.Date {
		t.Errorf("a no: %+v", n)
	}
}

func TestFoodWords(t *testing.T) {
	now := time.Date(2026, 11, 10, 20, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	for _, s := range []string{"Расскажи что интересного по еде", "что-нибудь вкусное на выходных", "где покушать в субботу"} {
		in := withKinds(rulesIntent(s, now), s)
		if in.I != "find" || !slices.Contains(in.Cats, "food") {
			t.Errorf("%q → %+v", s, in)
		}
	}
	if in := rulesIntent("прогулка в парке Победы", now); slices.Contains(in.Cats, "food") {
		t.Errorf("Победы is not food: %+v", in)
	}
}

// «опять мимо» is a complaint about the previous answer, not a request; «нет, не дороже 2000» is a follow-up.
func TestMissWords(t *testing.T) {
	for _, s := range []string{"опять мимо", "Опять мимо", "мимо", "не то", "нет, не то", "ну не то!", "это не то", "не то что я просил", "ты не понял", "совсем мимо)"} {
		if !missRe.MatchString(strings.ToLower(s)) {
			t.Errorf("%q must be a miss", s)
		}
	}
	for _, s := range []string{"нет, не дороже 2000", "не театр", "мимо кассы концерт", "а что-то не то чтобы дорогое", "кино в субботу"} {
		if missRe.MatchString(strings.ToLower(s)) {
			t.Errorf("%q is not a miss", s)
		}
	}
}

// «тир» is a kind of its own; «с боевого оружия» is not a fight night
func TestShootingRange(t *testing.T) {
	now := time.Now()
	for s, want := range map[string]string{"сходить в тир пострелять с боевого оружия": "тир", "хотим на бои в субботу": "бои", "турнир боёв": "бои"} {
		if g := withKinds(rulesIntent(s, now), s).G; len(g) != 1 || g[0] != want {
			t.Errorf("%q: %v, want %s", s, g, want)
		}
	}
}
