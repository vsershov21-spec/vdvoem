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

func TestScanPost(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	for _, c := range []struct {
		text, title string
		dates       int
		hour        int
		price, orig int64
	}{
		{"ЗВЕЗДНЫЙ СОСТАВ🌠💫\n\n🔴💥 ЧАЙКА\n🏛МХТ имени А.П. Чехова\nОсновная сцена\n\n✅ 19 ноября 19.00\n✅ 23 декабря 19.00\n\nПартер 👍\n🔥4500 вместо 5500-5000", "ЧАЙКА", 2, 19, 4500, 5500},
		{"🎭 ТЕАТР КОМЕДИИ 🎭\n\n🏛ДК ВДНХ\n\n9, 16, 23, 30 ноября в 19-30\n\n💥 Цена со скидкой\n2500 вместо 4000-3000\n2000 вместо 2500\n\n🔥 «ТЁЩА С СЮРПРИЗОМ!»", "ТЁЩА С СЮРПРИЗОМ!", 4, 19, 2000, 2500},
		{"РЕДКОСТЬ\n\n🏦ТЕАТР им. Вл. МАЯКОВСКОГО\nМалый зал\n\n🔴МЕРА ЗА МЕРУ\n\n🗓 11 ноября 19.00\n\n🔥3200 (вместо 4800-4000)", "МЕРА ЗА МЕРУ", 1, 19, 3200, 4800},
		{"«АВИАТОР»\n💥3800р\nВместо 7500-18000\n24 ноября в 19:00, Дом музыки", "АВИАТОР", 1, 19, 3800, 7500},
	} {
		sc, ok := scanPost(c.text, now)
		if !ok || sc.Title != c.title || len(sc.dates) != c.dates || sc.dates[0].Hour() != c.hour || sc.Price != c.price || sc.orig != c.orig {
			t.Errorf("%q → ok=%v %q dates=%v price=%d orig=%d", c.title, ok, sc.Title, sc.dates, sc.Price, sc.orig)
		}
	}
	if _, ok := scanPost("Всем привет! Скоро новые спектакли", now); ok {
		t.Error("no date — no event")
	}
}
