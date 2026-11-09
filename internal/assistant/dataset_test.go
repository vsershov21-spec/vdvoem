package assistant

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"vdvoem/internal/leisure"
)

// deploy/train/system.txt is the system prompt the dataset is built with: it must be the app's own.
// UPDATE_SYSTEM=1 go test -run TestDatasetSystem ./internal/assistant  — rewrites it after a prompt change.
func TestDatasetSystem(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	want := strings.ReplaceAll(strings.ReplaceAll(intentSystem(now), now.Format("2006-01-02"), "{DATE}"), ruWeekday(now), "{WD}")
	path := "../../deploy/train/system.txt"
	if os.Getenv("UPDATE_SYSTEM") == "1" {
		if err := os.WriteFile(path, []byte(want+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
		t.Fatalf("deploy/train/system.txt is not the app's intent prompt — run with UPDATE_SYSTEM=1 and regenerate the dataset")
	}
}

// DATASET=dir: every label is a valid intent, and how the rules alone do on the test split (the baseline).
func TestDatasetLabels(t *testing.T) {
	dir := os.Getenv("DATASET")
	if dir == "" {
		t.Skip()
	}
	for _, split := range []string{"train", "valid", "test"} {
		b, err := os.ReadFile(dir + "/" + split + ".jsonl")
		if err != nil {
			t.Fatal(err)
		}
		ok, n, kinds, kindsOK := 0, 0, 0, 0
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var e struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			if err := json.Unmarshal([]byte(line), &e); err != nil || len(e.Messages)%2 != 1 || len(e.Messages) < 3 {
				t.Fatalf("%s: bad line", split)
			}
			last := e.Messages[len(e.Messages)-1]
			var in Intent
			if err := json.Unmarshal([]byte(last.Content), &in); err != nil || last.Role != "assistant" {
				t.Fatalf("%s: bad label %s", split, last.Content)
			}
			s := sanitize(in)
			if a, _ := json.Marshal(s); string(a) != string(mustJSON(in)) {
				t.Fatalf("%s: label has values the app drops: %s → %s", split, last.Content, a)
			}
			if len(e.Messages) > 3 {
				n++
				continue // a dialogue: the rules alone do not see the first turn
			}
			day := e.Messages[0].Content[strings.Index(e.Messages[0].Content, "Сегодня ")+len("Сегодня ") : strings.Index(e.Messages[0].Content, "Сегодня ")+len("Сегодня ")+10]
			now, _ := time.ParseInLocation("2006-01-02", day, time.FixedZone("MSK", 3*3600))
			now = now.Add(12 * time.Hour)
			n++
			if matches(rulesIntent(e.Messages[1].Content, now), in, now) {
				ok++
			}
			if len(in.G) > 0 { // the kinds named in words must be found by the app's own map, whatever the model says
				kinds++
				got := withKinds(rulesIntent(e.Messages[1].Content, now), e.Messages[1].Content)
				if sameSet(got.G, in.G) {
					kindsOK++
				} else if misspelt(e.Messages[1].Content, in.G) {
					kinds-- // a typo inside the very word is the model's job, not the map's
				} else if kinds-kindsOK <= 8 {
					t.Logf("kinds: %q → %v, label %v", e.Messages[1].Content, got.G, in.G)
				}
			}
		}
		t.Logf("%s: %d examples, rules alone right on %d (%.0f%%); kinds found by the map %d/%d", split, n, ok, float64(ok)*100/float64(n), kindsOK, kinds)
		if kinds > 0 && kindsOK*100 < kinds*95 {
			t.Errorf("%s: the map finds only %d of %d labelled kinds", split, kindsOK, kinds)
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

// misspelt: none of a labelled kind's own phrasings is in the text as written (the generator put a typo into it)
func misspelt(text string, g []string) bool {
	t := strings.ReplaceAll(strings.ToLower(text), "ё", "е")
	for _, tag := range g {
		found := false
		for _, k := range leisure.Kinds {
			if k.Tag != tag {
				continue
			}
			for _, s := range k.Say {
				if strings.Contains(t, strings.ReplaceAll(strings.ToLower(s), "ё", "е")) {
					found = true
				}
			}
		}
		if !found {
			return true
		}
	}
	return false
}
