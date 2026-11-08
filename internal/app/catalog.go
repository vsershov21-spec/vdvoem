package app

import "strings"

// Catalog — the built-in bank of kinds of dates, for when the bank and the afisha have nothing for a mood.
// Generic on purpose (no venues): the assistant offers them as «добавить в копилку», and the place is found later.
type CatalogItem struct {
	Title    string
	Category string
	Tags     []string
	PriceRub int64 // rough, for two
}

var Catalog = []CatalogItem{
	{"СПА или баня на двоих", "other", []string{"chill", "romantic"}, 8000},
	{"Кино дома с пледом и попкорном", "other", []string{"chill", "home", "free"}, 0},
	{"Неспешный бранч в новом месте", "food", []string{"chill", "food", "new"}, 3000},
	{"Чайная церемония", "other", []string{"chill", "new"}, 2500},
	{"Прогулка по набережной на закате", "walk", []string{"chill", "romantic", "outdoor", "free"}, 0},
	{"Пикник в парке", "walk", []string{"chill", "romantic", "outdoor"}, 1000},
	{"Ботанический сад или оранжерея", "walk", []string{"chill", "outdoor"}, 1000},
	{"Планетарий", "culture", []string{"chill", "culture", "new"}, 1600},
	{"Массаж для двоих", "other", []string{"chill", "romantic"}, 9000},
	{"Готовим вместе новое блюдо", "food", []string{"home", "food", "new", "romantic"}, 1500},
	{"Настолки дома", "other", []string{"home", "fun", "chill"}, 0},
	{"Каток", "sport", []string{"active", "outdoor", "fun"}, 1500},
	{"Скалодром", "sport", []string{"active", "new"}, 2500},
	{"Батутный центр", "sport", []string{"active", "fun"}, 2000},
	{"Картинг", "sport", []string{"active", "fun"}, 4000},
	{"Боулинг", "sport", []string{"active", "fun"}, 2500},
	{"Велопрогулка", "sport", []string{"active", "outdoor"}, 1000},
	{"Бассейн", "sport", []string{"active", "chill"}, 1500},
	{"Квест-комната", "other", []string{"fun", "new", "active"}, 5000},
	{"Танцевальный мастер-класс", "other", []string{"active", "romantic", "new"}, 3000},
	{"Ужин на крыше с видом", "food", []string{"romantic", "food"}, 7000},
	{"Колесо обозрения вечером", "walk", []string{"romantic", "outdoor"}, 1200},
	{"Теплоход по реке", "walk", []string{"romantic", "outdoor", "chill"}, 3000},
	{"Джазовый вечер", "culture", []string{"romantic", "culture", "night"}, 3000},
	{"Дегустация вина или сыра", "food", []string{"romantic", "food", "new"}, 5000},
	{"Спектакль", "culture", []string{"culture", "romantic"}, 5000},
	{"Выставка в музее", "culture", []string{"culture", "chill"}, 1200},
	{"Иммерсивный спектакль", "culture", []string{"culture", "new"}, 6000},
	{"Лекция или экскурсия", "culture", []string{"culture", "new"}, 1500},
	{"Органный или классический концерт", "culture", []string{"culture", "romantic", "chill"}, 3000},
	{"Стендап", "other", []string{"fun"}, 2500},
	{"Квиз в баре", "other", []string{"fun", "food"}, 1500},
	{"Караоке", "other", []string{"fun", "night"}, 3000},
	{"Гастрономический рынок", "food", []string{"food", "fun", "new"}, 2500},
	{"Кухня, которую ещё не пробовали", "food", []string{"food", "new"}, 3500},
	{"Ночная прогулка по центру", "walk", []string{"night", "outdoor", "romantic", "free"}, 0},
	{"Бесплатный музейный день", "culture", []string{"culture", "free"}, 0},
	{"Гончарный мастер-класс", "other", []string{"new", "romantic", "chill"}, 4000},
	{"Поездка в усадьбу за город", "walk", []string{"outdoor", "culture", "new"}, 2000},
	{"Фотопрогулка", "walk", []string{"outdoor", "new", "free"}, 0},
}

// CatalogFor: up to n kinds of dates for these moods that are not in the bank yet.
func CatalogFor(moods []string, have []string, maxRub int64, n int) []CatalogItem {
	in := func(t string) bool {
		t = strings.ToLower(t)
		for _, h := range have {
			if strings.Contains(strings.ToLower(h), t) || strings.Contains(t, strings.ToLower(h)) {
				return true
			}
		}
		return false
	}
	type sc struct {
		c CatalogItem
		s int
	}
	var all []sc
	for _, c := range Catalog {
		if in(c.Title) || (maxRub > 0 && c.PriceRub > maxRub) {
			continue
		}
		s := 0
		for _, m := range moods {
			for _, t := range c.Tags {
				if t == m {
					s += 2
				}
			}
		}
		if s > 0 {
			all = append(all, sc{c, s})
		}
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].s > all[j-1].s; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	var out []CatalogItem
	for k := 0; k < len(all) && len(out) < n; k++ {
		out = append(out, all[k].c)
	}
	return out
}
