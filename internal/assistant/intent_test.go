package assistant

import (
	"encoding/json"
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
	for _, tc := range intentHoldout {
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
	t.Logf("holdout: rules %d/%d, sure %d, sure-but-wrong %d", ok, len(intentHoldout), sure, sureBad)
}
