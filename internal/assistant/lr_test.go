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
