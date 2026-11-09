package assistant

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The rules alone must already understand most requests correctly (the model only helps with the rest),
// and never misread a clear one with high confidence.
func TestRulesIntents(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	ok, sure, sureBad := 0, 0, 0
	for _, tc := range intentCases {
		var want Intent
		_ = json.Unmarshal([]byte(tc.Want), &want)
		got := rulesIntent(tc.Q, now)
		m := matches(got, want, now)
		if m {
			ok++
		}
		if got.conf >= 0.8 {
			sure++
			if !m {
				sureBad++
				b, _ := json.Marshal(got)
				t.Logf("sure but wrong: %q → %s (want %s)", tc.Q, b, tc.Want)
			}
		} else if !m {
			b, _ := json.Marshal(got)
			t.Logf("unsure: %q → %s %.2f (want %s)", tc.Q, b, got.conf, tc.Want)
		}
	}
	t.Logf("rules: %d/%d right, %d sure, %d sure-but-wrong", ok, len(intentCases), sure, sureBad)
	if sureBad*20 > sure { // ≤5% of confident readings may be wrong
		t.Errorf("too many confident mistakes: %d of %d", sureBad, sure)
	}
	if ok*10 < len(intentCases)*7 {
		t.Errorf("rules understand only %d of %d", ok, len(intentCases))
	}
}

func TestFollowUp(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	prev := rulesIntent("куда сходить в субботу вечером до 4000", now)
	got := mergeFollowUp(prev, rulesIntent("а подешевле?", now), "а подешевле?")
	if got.I != "find" || got.Max >= 4000 || got.Max == 0 || got.Date != prev.Date || got.Part != "evening" {
		t.Fatalf("follow-up: %+v", got)
	}
	got = mergeFollowUp(prev, rulesIntent("а без театра", now), "а без театра")
	if len(got.Not) == 0 || got.Date != prev.Date {
		t.Fatalf("without theatre: %+v", got)
	}
}

func TestRulesHoldout(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	ok, sure, sureBad := 0, 0, 0
	for _, tc := range append(append([]intentCase{}, intentHoldout...), intentHoldout2...) {
		var want Intent
		_ = json.Unmarshal([]byte(tc.Want), &want)
		got := rulesIntent(tc.Q, now)
		m := matches(got, want, now)
		if m {
			ok++
		}
		if got.conf >= 0.8 {
			sure++
		}
		if got.conf >= 0.8 && !m {
			sureBad++
		}
		if !m {
			b, _ := json.Marshal(got)
			t.Logf("%q → %s %.2f (want %s)", tc.Q, b, got.conf, tc.Want)
		}
	}
	t.Logf("holdout: rules %d/%d, sure %d, sure-but-wrong %d", ok, len(intentHoldout)+len(intentHoldout2), sure, sureBad)
}

// The model's real mistakes from the server eval (qwen2.5 3b and 1.5b): reconcile must fix most of them.
func TestReconcileModelErrors(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	want := map[string]Intent{}
	for _, c := range append(append([]intentCase{}, intentCases...), intentHoldout...) {
		var w Intent
		_ = json.Unmarshal([]byte(c.Want), &w)
		want[c.Q] = w
	}
	b, err := os.ReadFile("testdata/model_errors.txt")
	if err != nil {
		t.Fatal(err)
	}
	fixed, total := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		q, js, ok := strings.Cut(line, " → ")
		w, known := want[q]
		if !ok || !known {
			continue
		}
		var m Intent
		_ = json.Unmarshal([]byte(js), &m)
		total++
		got := reconcile(sanitize(m), rulesIntent(q, now), q)
		if matches(got, w, now) {
			fixed++
		} else {
			g, _ := json.Marshal(got)
			t.Logf("still wrong: %q → %s (want %s)", q, g, w.I)
		}
	}
	t.Logf("reconcile fixed %d of %d model mistakes", fixed, total)
	if fixed*10 < total*7 {
		t.Errorf("only %d of %d fixed", fixed, total)
	}
}

func TestDayWords(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600)) // Sunday
	if d := dayFrom("где позавтракать в субботу", now); d != "2026-11-14" {
		t.Fatalf("«позавтракать» is not «завтра»: %s", d)
	}
	if d := dayFrom("а завтра?", now); d != "2026-11-09" {
		t.Fatal(d)
	}
}

// With a fine-tuned model: a right answer from it must survive reconcile on every case we have
// (gold + both held-out sets), and its real mistakes from the server eval should mostly be fixed.
func TestReconcileTuned(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	broken := 0
	want := map[string]Intent{}
	for _, c := range append(append(append([]intentCase{}, intentCases...), intentHoldout...), intentHoldout2...) {
		var w Intent
		_ = json.Unmarshal([]byte(c.Want), &w)
		want[c.Q] = w
		m := w
		for _, code := range []string{"TOD", "TOM", "SAT", "SUN", "FRI"} { // the model answers with real dates
			if m.Date == code {
				m.Date = caseDate(code, now)
			}
		}
		rule := rulesIntent(c.Q, now)
		if rule.conf >= 0.8 && trivial(rule, c.Q) {
			continue // answered by the rules, the model is not asked
		}
		if got := reconcileTuned(sanitize(m), rule, c.Q); !matches(got, w, now) {
			broken++
			g, _ := json.Marshal(got)
			t.Logf("a right answer broken: %q → %s (want %s)", c.Q, g, c.Want)
		}
	}
	if broken > 2 {
		t.Errorf("reconcile breaks %d right answers of the tuned model", broken)
	}
	b, _ := os.ReadFile("testdata/tuned_errors.txt")
	fixed, total := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		q, js, _ := strings.Cut(line, " → ")
		w, ok := want[q]
		if !ok {
			continue
		}
		var m Intent
		_ = json.Unmarshal([]byte(js), &m)
		total++
		if got := reconcileTuned(sanitize(m), rulesIntent(q, now), q); matches(got, w, now) {
			fixed++
		} else {
			g, _ := json.Marshal(got)
			t.Logf("still wrong: %q → %s (want %s)", q, g, w.I)
		}
	}
	t.Logf("tuned: %d right answers broken; %d of %d mistakes fixed", broken, fixed, total)
}

// «что есть на неделе» is a look at the afisha, not one pick from the bank; small talk is talk.
func TestWeekAndTalk(t *testing.T) {
	now := time.Date(2026, 11, 8, 21, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	for q, want := range map[string]string{"а что есть на неделе": "find", "что интересного на неделе?": "find", "куда сходить на этой неделе": "find",
		"что нарыл агент": "find", "как дела?": "chat", "что посоветуешь": "suggest"} {
		if got := rulesIntent(q, now); got.I != want {
			t.Errorf("%q → %s, want %s", q, got.I, want)
		}
	}
	if !rulesIntent("что нарыл агент", now).Scout {
		t.Error("the agent's finds are the afisha")
	}
}

func TestRefersTo(t *testing.T) {
	titles := []string{"Джаз в Козловском", "Чайка", "Каток в Парке Горького"}
	for q, want := range map[string]string{"расскажи про второе": "about:Чайка", "сколько стоит первое?": "price:Джаз в Козловском",
		"давай на последнее": "book:Каток в Парке Горького", "давай туда": "book:Джаз в Козловском", "а второе подороже?": "price:Чайка"} {
		in, ok := refersTo(q, titles)
		if !ok || in.I+":"+in.Q != want {
			t.Errorf("%q → %v %s:%s, want %s", q, ok, in.I, in.Q, want)
		}
	}
	for _, q := range []string{"что это за спектакль Чайка", "а что есть на неделе", "подешевле"} {
		if _, ok := refersTo(q, titles); ok {
			t.Errorf("%q is not a reference", q)
		}
	}
	if _, ok := refersTo("расскажи про второе", nil); ok {
		t.Error("nothing was shown")
	}
}

// every one-tap follow-up must be understood without the model (instant, never misread)
func TestNextUnderstood(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	titles := []string{"Чайка", "Джаз в саду"}
	seen := map[string]bool{}
	for _, in := range []Intent{{I: "find"}, {I: "find", Wknd: true, Max: 1000}, {I: "suggest"}, {I: "about"}, {I: "price"}, {I: "overview"}, {I: "chat"}} {
		for _, q := range nextFor(in, []Card{{Type: "idea", Title: titles[0]}, {Type: "idea", Title: titles[1]}}) {
			if seen[q] {
				continue
			}
			seen[q] = true
			if r, ok := refersTo(q, titles); ok {
				if r.Q == "" {
					t.Errorf("%q: refers to nothing", q)
				}
				continue
			}
			r := mergeFollowUp(Intent{I: "find"}, rulesIntent(q, now), q)
			if r.conf < 0.8 || r.I == "chat" {
				t.Errorf("%q: rules give %s %.2f", q, r.I, r.conf)
			}
		}
	}
}

func TestDescribe(t *testing.T) {
	for in, want := range map[string]string{
		`{"i":"find","date":"2026-11-10","part":"evening","max":3000}`: "Подобрать, куда сходить во вторник вечером до 3000 ₽",
		`{"i":"price","q":"Чайка"}`:                                    "Узнать цену «Чайка»",
		`{"i":"price"}`:                                                "",
	} {
		var x Intent
		_ = json.Unmarshal([]byte(in), &x)
		if got := describe(x); got != want {
			t.Errorf("%s → %q, want %q", in, got, want)
		}
	}
}

func TestAltFor(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	q := "хотим в субботу вечером что-то спокойное"
	rule := rulesIntent(q, now)
	if a := altFor(Intent{I: "chat"}, rule, q); a.I == "" {
		t.Errorf("chat for a request: no alternative offered (rule %s %.2f)", rule.I, rule.conf)
	}
	if a := altFor(Intent{I: "chat"}, rulesIntent("привет", now), "привет"); a.I != "" {
		t.Errorf("greeting got an alternative %s", a.I)
	}
	if a := altFor(rule, rule, q); a.I != "" {
		t.Errorf("same reading offered again")
	}
}

// Every kind of leisure in the gold set is found without the model: the map is the floor the model stands on.
func TestKindCases(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	for _, tc := range kindCases {
		var want Intent
		if err := json.Unmarshal([]byte(tc.Want), &want); err != nil {
			t.Fatalf("%q: %v", tc.Q, err)
		}
		if s := sanitize(want); !sameSet(s.G, want.G) {
			t.Errorf("%q: label kinds %v are not on the map", tc.Q, want.G)
		}
		lab := want
		lab.G, lab.Cats = nil, nil
		if m := withKinds(lab, tc.Q); !sameSet(m.Cats, want.Cats) {
			t.Errorf("%q: label cats %v, the map says %v", tc.Q, want.Cats, m.Cats)
		}
		got := withKinds(rulesIntent(tc.Q, now), tc.Q)
		if !sameSet(got.G, want.G) || got.I != "find" {
			b, _ := json.Marshal(got)
			t.Errorf("%q → %s, want %s", tc.Q, b, tc.Want)
		}
	}
}
