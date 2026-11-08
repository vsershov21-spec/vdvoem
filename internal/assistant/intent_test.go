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
