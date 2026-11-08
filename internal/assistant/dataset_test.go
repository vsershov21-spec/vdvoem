package assistant

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
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
		ok, n := 0, 0
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var e struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			if err := json.Unmarshal([]byte(line), &e); err != nil || len(e.Messages) != 3 {
				t.Fatalf("%s: bad line", split)
			}
			var in Intent
			if err := json.Unmarshal([]byte(e.Messages[2].Content), &in); err != nil {
				t.Fatalf("%s: bad label %s", split, e.Messages[2].Content)
			}
			s := sanitize(in)
			if a, _ := json.Marshal(s); string(a) != string(mustJSON(in)) {
				t.Fatalf("%s: label has values the app drops: %s → %s", split, e.Messages[2].Content, a)
			}
			day := e.Messages[0].Content[strings.Index(e.Messages[0].Content, "Сегодня ")+len("Сегодня ") : strings.Index(e.Messages[0].Content, "Сегодня ")+len("Сегодня ")+10]
			now, _ := time.ParseInLocation("2006-01-02", day, time.FixedZone("MSK", 3*3600))
			now = now.Add(12 * time.Hour)
			n++
			if matches(rulesIntent(e.Messages[1].Content, now), in, now) {
				ok++
			}
		}
		t.Logf("%s: %d examples, rules alone right on %d (%.0f%%)", split, n, ok, float64(ok)*100/float64(n))
	}
}
