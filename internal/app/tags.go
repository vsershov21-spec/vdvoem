package app

import (
	"strings"

	"vdvoem/internal/store"
)

// Moods turn «хотим почилить», «есть энергия», «романтично» into tags; every idea gets tags from its
// category, title and the scout's reason. The tags feed the per-person taste model (they learn which
// moods each person picks) and let the assistant answer by mood. Fully deterministic — instant.

var Moods = []string{"chill", "active", "romantic", "culture", "fun", "food", "outdoor", "home", "free", "new", "night", "kids"}

var MoodName = map[string]string{"chill": "спокойно", "active": "активно", "romantic": "романтично", "culture": "культурно", "fun": "весело",
	"food": "вкусно", "outdoor": "на воздухе", "home": "дома", "free": "бесплатно", "new": "что-то новое", "night": "ночью", "kids": "с детьми"}

// what people say → moods
var moodWords = map[string][]string{
	"chill":    {"чил", "отдохн", "расслаб", "лениво", "без напряг", "уютн", "спокойн", "тих", "выдохн", "перезагруз", "релакс", "устал", "медленн", "неспешн"},
	"active":   {"актив", "подвигат", "энерги", "спорт", "размят", "движ", "драйв", "адреналин", "покатат"},
	"romantic": {"романт", "свидан", "вдвоём красиво", "вдвоем красиво", "атмосферн", "при свечах", "закат"},
	"culture":  {"культур", " умн", "искусств", "интеллект", "просвет", "вдохнов", "красив"},
	"fun":      {"весел", "поржать", "посмеят", "тусит", "оторват", "угар", "развлеч", "поиграт"},
	"food":     {"поесть", "вкусн", "голод", "покушат", "поужинат", "позавтракат", "бранч", "перекус"},
	"outdoor":  {"на воздух", "на улиц", "погулят", "прогул", "природ", "свеж", "парк"},
	"home":     {"дома", "никуда не", "не выходя", "не выход", "из дома", "домашн", "под пледом"},
	"free":     {"бесплатн", "без денег", "даром", "ничего не тратить", "на мели", "сэконом"},
	"new":      {"новое", "необычн", "впервые", "никогда не", "удиви", "экзотик", "неожидан"},
	"night":    {"ночью", "ночн", "до утра", "поздно"},
	"kids":     {"с детьми", "с ребёнк", "с ребенк", "детск"},
}

// MoodsFrom finds moods in a request.
func MoodsFrom(text string) []string {
	t := strings.ToLower(text)
	var out []string
	for _, m := range Moods {
		for _, w := range moodWords[m] {
			if strings.Contains(t, w) {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// what an idea is like → tags
var tagWords = map[string][]string{
	"chill":    {"спа", "баня", "сауна", "чайн", "кофе", "парк", "сад", "кино", "плед", "массаж", "йога", "библиот", "набережн", "аквариум", "планетари", "оранжере"},
	"active":   {"каток", "лыж", "скалодр", "батут", "картинг", "сап", "велос", "бассейн", "квест", "пейнтбол", "лазертаг", "танц", "боулинг", "футбол", "хоккей", "пробеж", "хайк", "поход", "роллер", "сноуборд"},
	"romantic": {"ужин", "крыш", "закат", "вино", "колесо обозрения", "пикник", "свеч", "джаз", "опер", "балет", "теплоход", "фейерверк"},
	"culture":  {"театр", "спектак", "музей", "выставк", "галере", "опер", "балет", "филармон", "концерт", "лекци", "экскурс", "премьер"},
	"fun":      {"стендап", "квиз", "караоке", "вечеринк", "фестив", "шоу", "цирк", "аттракцион", "настолк", "игр", "бар"},
	"food":     {"ресторан", "кафе", "бар", "ужин", "завтрак", "бранч", "кухн", "гастро", "хинкал", "пицц", "суши", "дегустац", "фудкорт", "рынок"},
	"outdoor":  {"парк", "прогул", "набережн", "лес", "пикник", "каток", "сад", "вднх", "усадьб", "заповедн", "озер", "пляж", "крыш"},
	"home":     {"дома", "кино дома", "настолк", "готовить вместе", "доставк"},
	"free":     {"бесплатн", "вход свободн", "free"},
	"new":      {"мастер-класс", "мастер класс", "впервые", "иммерсив", "vr", "необычн", "экспериментальн"},
	"night":    {"ночн", "до утра", "ночь музеев"},
	"kids":     {"детск", "для детей", "семейн"},
}

var genreMoods = map[string][]string{"комедия": {"fun"}, "стендап": {"fun"}, "шоу": {"fun"}, "мелодрама": {"romantic"}, "оперетта": {"romantic", "culture"},
	"балет": {"romantic", "culture"}, "опера": {"culture"}, "джаз": {"romantic"}, "иммерсивный": {"new"}, "детям": {"kids"}, "гастро": {"food"},
	"классическая музыка": {"chill", "culture"}, "выставка": {"chill", "culture"}, "лекция": {"culture"}}

var catTags = map[string][]string{"food": {"food"}, "culture": {"culture"}, "walk": {"outdoor", "chill"}, "sport": {"active"}}

// TagsOf: an idea's moods, from its category, words and the scout's reason.
func TagsOf(i store.Idea) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range catTags[i.Category] {
		add(t)
	}
	text := strings.ToLower(i.Title + " " + i.Note + " " + i.Why)
	for _, m := range Moods {
		for _, w := range tagWords[m] {
			if strings.Contains(text, w) {
				add(m)
				break
			}
		}
	}
	for _, g := range i.Info.Genres {
		for _, t := range genreMoods[g] {
			add(t)
		}
	}
	if i.PriceMin == 0 && strings.Contains(text, "бесплат") {
		add("free")
	}
	return out
}

// MoodFit: 0..1 — how well an idea answers the asked moods.
func MoodFit(i store.Idea, moods []string) float64 {
	if len(moods) == 0 {
		return 0
	}
	tags := map[string]bool{}
	for _, t := range TagsOf(i) {
		tags[t] = true
	}
	hit := 0
	for _, m := range moods {
		if tags[m] {
			hit++
		}
	}
	return float64(hit) / float64(len(moods))
}
