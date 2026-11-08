package assistant

import (
	"strings"
	"testing"
	"time"
)

// the bot: a request goes to the assistant, a bare name to the bank
func TestLooksLikeRequest(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for q, want := range map[string]bool{
		"Театр на Таганке": false, "Театр на Таганке вечером": false, "Каток в Парке Горького": false, "Каток": false,
		"Чайка во МХТ": false, "грузинский ресторан": false,
		"хотим почилить в субботу": true, "давай в кино завтра вечером": true, "сколько стоит Чайка": true,
		"что-нибудь до 3000 на выходных": true, "запиши нас на джаз в пятницу": true,
	} {
		if got := LooksLikeRequest(q, now); got != want {
			in := rulesIntent(q, now)
			t.Errorf("%q → %v, want %v (%s %.2f)", q, got, want, in.I, in.conf)
		}
	}
}

func TestPhrasesUnderstood(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, p := range Phrases {
		q := p
		if strings.HasSuffix(q, " ") {
			q += "Чайку"
		}
		in := rulesIntent(q, now)
		if in.I == "chat" || in.conf < 0.6 {
			t.Errorf("%q → %s %.2f", q, in.I, in.conf)
		}
	}
}

func TestTopicAndGoal(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if in := rulesIntent("что-нибудь про технологии", now); in.I != "find" || in.Q == "" {
		t.Errorf("topic: %+v", in)
	}
	if in := rulesIntent("хотим на природу", now); in.I == "goal" {
		t.Errorf("«на природу» is a search now, not a goal")
	}
	if in := rulesIntent("хотим на Земфиру", now); in.I != "goal" {
		t.Errorf("a name is a goal: %+v", in)
	}
	if q := bookQ("Щелкунчика — подойдёт нам?"); q != "Щелкунчика" {
		t.Errorf("about q %q", q)
	}
	if past("Настя", "отметил") != "отметила" || past("Вова", "отметил") != "отметил" {
		t.Error("gender")
	}
}

func TestPeriod(t *testing.T) {
	now := time.Date(2026, 11, 8, 23, 0, 0, 0, time.UTC)
	for q, want := range map[string]string{
		"какие есть мероприятия в конце наебря":   "2026-11-20..2026-11-30",
		"что-нибудь в начале декабря":             "2026-12-01..2026-12-10",
		"куда сходить на следующей неделе":        "2026-11-09..2026-11-15",
		"что есть в январе":                       "2027-01-01..2027-01-31",
		"до конца месяца":                         "2026-11-08..2026-11-30",
		"хотим в театр":                           "..",
	} {
		f, to := periodFrom(q, now)
		if got := f + ".." + to; got != want {
			t.Errorf("%q → %s, want %s", q, got, want)
		}
	}
	in := withPeriod(Intent{I: "suggest"}, "Подскажи, какие есть еще мероприятия в конце наебря, куда мы можем сходить вдвоем", now)
	if in.I != "find" || in.From != "2026-11-20" {
		t.Errorf("model's «suggest» with a period: %+v", in)
	}
	r := mergeFollowUp(Intent{I: "find", Wknd: true, Max: 3000}, rulesIntent("Покажи все 155", now), "Покажи все 155")
	w := widen(r, "Покажи все 155")
	if !w.All || !w.Wknd || w.Max != 3000 {
		t.Errorf("«покажи все» keeps the conditions: %+v", w)
	}
}
