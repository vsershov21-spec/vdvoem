package taste

import (
	"testing"
	"time"
)

func TestLearnsPerson(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	sat := time.Date(2026, 11, 14, 19, 0, 0, 0, loc)
	var ex []Example
	for _, x := range []struct {
		t, c string
		p    int64
		y    float64
	}{
		{"Спектакль в театре Вахтангова", "culture", 600000, 1}, {"Премьера в театре на Таганке", "culture", 500000, 1},
		{"Театр Наций: Гамлет", "culture", 700000, 1}, {"Стендап вечер", "other", 200000, 0}, {"Открытый микрофон стендап", "other", 100000, 0},
		{"Футбол ЦСКА", "sport", 300000, 0}, {"Хоккей КХЛ", "sport", 400000, 0}, {"Ужин в грузинском ресторане", "food", 400000, 1},
		{"Грузинская кухня: хинкали", "food", 300000, 1}, {"Бургерная", "food", 150000, 0}, {"Театр Ермоловой", "culture", 400000, 1},
	} {
		ex = append(ex, Example{Item: Item{Title: x.t, Category: x.c, PriceKop: x.p, Starts: &sat}, Label: x.y, Weight: 1})
	}
	m := Train(ex, loc)
	theatre := m.Score(Item{Title: "Новый спектакль в театре Маяковского", Category: "culture", PriceKop: 500000, Starts: &sat}, loc)
	standup := m.Score(Item{Title: "Большой стендап концерт", Category: "other", PriceKop: 200000, Starts: &sat}, loc)
	if theatre < 0.7 || standup > 0.35 {
		t.Fatalf("theatre %.2f standup %.2f", theatre, standup)
	}
	likes, dislikes := m.Explain(3)
	if len(likes) == 0 || len(dislikes) == 0 {
		t.Fatalf("explain: %v / %v", likes, dislikes)
	}
	if empty := Train(nil, loc); empty.Score(Item{Title: "x"}, loc) != 0.5 {
		t.Fatal("no marks → no opinion")
	}
}
