package leisure

import (
	"reflect"
	"testing"
)

func TestDetect(t *testing.T) {
	for q, want := range map[string][]string{
		"хотим на джаз в пятницу":          {"джаз"},
		"органный концерт при свечах":      {"орган", "концерт при свечах"},
		"сходим на хоккей?":                {"хоккей"},
		"хочу в баню":                      {"баня"},
		"есть ли выставки импрессионистов": {"живопись"},
		"давай в планетарий":               {"планетарий"},
		"срок сдачи проекта":               nil,
		"купить бананы":                    nil,
		"мастер-класс по керамике":         {"керамика"},
		"иммерсивный спектакль":            {"иммерсивный"},
		"экскурсия по крышам":              {"крыши"},
		"что в афише без стендапа?":        nil,
		"квиз, но не хотим караоке":        {"квиз"},
		"где поужинать после планетария":   nil,
		"после театра хотим в бар":         {"бар"},
		"хочу поиграть в падел":            {"ракетки"},
		"грузинская кухня в субботу":       {"грузинская кухня"},
		"индийский ресторан":               {"ресторан"},
	} {
		if got := Detect(q); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("%q → %v, want %v", q, got, want)
		}
	}
	if !Fits("джаз", "Джазовый вечер в Козловском клубе") || Fits("рок", "Срок годности") {
		t.Error("fits")
	}
	if len(Tags()) < 80 {
		t.Error("the map is too small")
	}
}

func TestSpheresCoverTheMap(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Spheres {
		for _, tag := range s.Tags {
			if !Known(tag) {
				t.Errorf("%s: %q is not a kind", s.ID, tag)
			}
			if seen[tag] {
				t.Errorf("%q in two spheres", tag)
			}
			seen[tag] = true
		}
	}
	for _, k := range Kinds {
		if !seen[k.Tag] && k.Tag != "детям" {
			t.Errorf("kind %q has no sphere", k.Tag)
		}
	}
}

// artists and movements read as painting, the everyday words around them do not
func TestArtists(t *testing.T) {
	has := func(s string) bool {
		for _, k := range Detect(s) {
			if k == "живопись" {
				return true
			}
		}
		return false
	}
	for _, s := range []string{"хочу на выставку Айвазовского", "что-нибудь с Моне", "Ван Гог в Москве", "где сейчас Малевич", "Шишкин в Третьяковке", "посмотреть на импрессионистов", "что-то про авангард", "Фрида Кало"} {
		if !has(s) {
			t.Errorf("not painting: %q", s)
		}
	}
	for _, s := range []string{"нумизматика, монеты", "нам дали билеты", "кафе на Рублёвском шоссе", "сходить в бар", "купить шишки для ёлки"} {
		if has(s) {
			t.Errorf("painting by mistake: %q", s)
		}
	}
}
