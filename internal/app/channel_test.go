package app

import (
	"testing"
	"time"
)

// The model's reading of a post is checked against the post: invented titles, prices and days are dropped.
func TestCheckEvents(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	post := ChannelPost{From: "Дешёвые билеты", Text: `🎭 «Вишнёвый сад» в Современнике — 15 ноября, 19:00. Билеты от 900 ₽
🎷 Джаз в Козловском клубе, в субботу в 20:30 — 1 200 р.
Скоро: Щелкунчик в Большом`, Links: []string{"https://tickets.example/sad"}}
	got := checkEvents([]postEvent{
		{Title: "Вишнёвый сад", Date: "2026-11-15", Time: "19:00", Price: 900, Venue: "Современник", URL: "https://tickets.example/sad"},
		{Title: "Джаз в Козловском клубе", Date: "2026-11-14", Time: "20:30", Price: 1200},
		{Title: "Гамлет в МХТ", Date: "2026-11-20", Price: 500}, // not in the post
		{Title: "Щелкунчик", Date: "2026-12-25"},                // no day in the post
		{Title: "Вишнёвый сад", Date: "2026-11-16", Price: 750}, // wrong day, invented price
	}, post, now)
	if len(got) != 2 {
		t.Fatalf("kept %d: %+v", len(got), got)
	}
	if got[0].Price != 900 || got[0].Time != "19:00" || got[0].URL == "" || got[1].Price != 1200 || got[1].Time != "20:30" {
		t.Fatalf("fields: %+v", got)
	}
	if ncaseRu(1, "событие", "события", "событий") != "1 событие" || ncaseRu(12, "событие", "события", "событий") != "12 событий" {
		t.Fatal("ncase")
	}
}
