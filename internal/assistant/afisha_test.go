package assistant

import (
	"testing"
	"time"
)

func TestRulesUnderstand(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, loc) // Wednesday
	q := afishaFromText("что в субботу вечером до 3000, только без театра", now)
	if q.Date != "2026-11-14" || q.Part != "evening" || q.MaxRub != 3000 || len(q.Exclude) == 0 || q.Exclude[0] != "culture" {
		t.Fatalf("%+v", q)
	}
	for _, c := range q.Include {
		if c == "culture" {
			t.Fatalf("excluded theatre must not be included: %+v", q)
		}
	}
	if q = afishaFromText("анонсы концертов на потом", now); q.Horizon != "later" || q.WholeBank {
		t.Fatalf("%+v", q)
	}
	if q = afishaFromText("что-нибудь недорогое на выходных", now); !q.Weekend || q.MaxRub != 2000 {
		t.Fatalf("%+v", q)
	}
	if m := memoryFromText("Запомни, что Настя не любит стендап"); m != "Настя не любит стендап" {
		t.Fatalf("memory %q", m)
	}
	if m := memoryFromText("я не люблю ужастики"); m == "" {
		t.Fatal("first-person dislike is a memory")
	}
	if m := memoryFromText("запомни https://afisha.ru/x"); m != "" {
		t.Fatal("a link is an idea, not a memory")
	}
	if g := goalFromText("Следи за концертом Земфиры, когда приедет"); g == "" {
		t.Fatal("goal")
	}
	if g := goalFromText("хотим сходить в театр в январе"); g != "театр в январе" {
		t.Fatalf("goal %q", g)
	}
}
