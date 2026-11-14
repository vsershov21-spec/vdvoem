package assistant

import (
	"slices"
	"strings"
	"testing"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

// a dialogue keeps its point: «хочу в бар в субботу» → «а с караоке?» → «нет, лучше кофейня» → «до 3000»
func TestFollowUpsKeepTheRequest(t *testing.T) {
	now := time.Date(2026, 11, 10, 20, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	step := func(prev Intent, text string) Intent { return mergeFollowUp(prev, rulesIntent(text, now), text) }
	bar := withKinds(rulesIntent("хочу в бар в субботу", now), "хочу в бар в субботу")
	if bar.I != "find" || !slices.Contains(bar.G, "бар") || bar.Date == "" {
		t.Fatalf("start: %+v", bar)
	}
	k := step(bar, "а с караоке?")
	if !slices.Contains(k.G, "бар") || !slices.Contains(k.G, "караоке") || k.Date != bar.Date {
		t.Errorf("add a kind: %+v", k)
	}
	c := step(k, "нет, лучше кофейня")
	if !slices.Equal(c.G, []string{"кофейня"}) || c.Date != bar.Date {
		t.Errorf("swap the kind: %+v", c)
	}
	m := step(c, "до 3000 на двоих")
	if m.Max != 3000 || !slices.Equal(m.G, []string{"кофейня"}) || m.Date != bar.Date {
		t.Errorf("a budget: %+v", m)
	}
	n := step(m, "а без кофеен")
	if len(n.NotW) == 0 || n.Date != bar.Date {
		t.Errorf("a no: %+v", n)
	}
}

func TestFoodWords(t *testing.T) {
	now := time.Date(2026, 11, 10, 20, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	for _, s := range []string{"Расскажи что интересного по еде", "что-нибудь вкусное на выходных", "где покушать в субботу"} {
		in := withKinds(rulesIntent(s, now), s)
		if in.I != "find" || !slices.Contains(in.Cats, "food") {
			t.Errorf("%q → %+v", s, in)
		}
	}
	if in := rulesIntent("прогулка в парке Победы", now); slices.Contains(in.Cats, "food") {
		t.Errorf("Победы is not food: %+v", in)
	}
}

// «опять мимо» is a complaint about the previous answer, not a request; «нет, не дороже 2000» is a follow-up.
func TestMissWords(t *testing.T) {
	for _, s := range []string{"опять мимо", "Опять мимо", "мимо", "не то", "нет, не то", "ну не то!", "это не то", "не то что я просил", "ты не понял", "совсем мимо)"} {
		if !missRe.MatchString(strings.ToLower(s)) {
			t.Errorf("%q must be a miss", s)
		}
	}
	for _, s := range []string{"нет, не дороже 2000", "не театр", "мимо кассы концерт", "а что-то не то чтобы дорогое", "кино в субботу"} {
		if missRe.MatchString(strings.ToLower(s)) {
			t.Errorf("%q is not a miss", s)
		}
	}
}

// «сколько мы потратили» is a sum of past meetings, not a search; «куда без трат» is a search for something free
func TestSpendWords(t *testing.T) {
	for _, s := range []string{"сколько мы потратили в этом месяце", "наши траты за ноябрь", "Сколько ушло денег на свидания?", "во сколько нам обошлись выходные", "покажи расходы за месяц"} {
		if !spendRe.MatchString(strings.ToLower(s)) {
			t.Errorf("%q is about money spent", s)
		}
	}
	for _, s := range []string{"куда сходить без трат", "что-нибудь без больших трат", "сколько стоит Щелкунчик", "хочу потратить не больше 3000"} {
		if spendRe.MatchString(strings.ToLower(s)) {
			t.Errorf("%q is not", s)
		}
	}
}

// «тир» is a kind of its own; «с боевого оружия» is not a fight night
func TestShootingRange(t *testing.T) {
	now := time.Now()
	for s, want := range map[string]string{"сходить в тир пострелять с боевого оружия": "тир", "хотим на бои в субботу": "бои", "турнир боёв": "бои"} {
		if g := withKinds(rulesIntent(s, now), s).G; len(g) != 1 || g[0] != want {
			t.Errorf("%q: %v, want %s", s, g, want)
		}
	}
}

// one pick from the bank and «во сколько начинается …» are read by the rules, not left to the model
func TestBankPickAndStartTime(t *testing.T) {
	now := time.Now()
	for s, want := range map[string]string{"выбери что-то из нашей копилки": "suggest", "что нам точно зайдёт?": "suggest", "когда начинается Щелкунчик": "about", "во сколько начало концерта Земфиры": "about", "во сколько начинается завтра": "find"} {
		if in := rulesIntent(s, now); in.I != want && !(want == "find" && in.I != "about") {
			t.Errorf("%q: %s, want %s", s, in.I, want)
		}
	}
	if in := rulesIntent("когда начинается Щелкунчик", now); !strings.Contains(strings.ToLower(in.Q), "щелкунчик") {
		t.Errorf("q: %q", in.Q)
	}
}

// the model's «без …» stands only where the words say so
func TestNegationsBacked(t *testing.T) {
	m := negationsBacked(Intent{I: "find", Cats: []string{"food"}, Not: []string{"other", "food"}, NotW: []string{"грузинское"}}, "а грузинское есть?")
	if len(m.NotW) != 0 || len(m.Not) != 0 {
		t.Errorf("nothing was excluded: %+v", m)
	}
	m = negationsBacked(Intent{I: "find", Cats: []string{"food"}, Not: []string{"food"}, NotW: []string{"ужин"}}, "поищи что-нибудь новое по еде")
	if len(m.NotW) != 0 || len(m.Not) != 0 {
		t.Errorf("«новое по еде» excludes nothing: %+v", m)
	}
	for text, w := range map[string]string{"только не мюзикл": "мюзикл", "хочу в театр без драмы": "драма", "куда угодно кроме кино": "кино", "никаких квестов": "квест", "не люблю стендап, не надо его": "стендап"} {
		if m := negationsBacked(Intent{I: "find", NotW: []string{w}}, text); len(m.NotW) != 1 {
			t.Errorf("%q must keep «без %s»", text, w)
		}
	}
	if m := negationsBacked(Intent{I: "find", Not: []string{"culture"}}, "только не театр"); len(m.Not) != 1 {
		t.Errorf("a negated category stays: %+v", m)
	}
}

// cuisines are kinds of their own (not one «кухня мира»); «с рыбами» could be dinner or the oceanarium — both;
// «на Новый год» is the holidays, 30 December — 8 January
func TestCuisinesFishNewYear(t *testing.T) {
	now := time.Date(2026, 11, 11, 22, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	read := func(s string) Intent { return withPeriod(withKinds(widen(rulesIntent(s, now), s), s), s, now) }
	for s, want := range map[string][]string{
		"Лучшие итальянские рестораны": {"итальянская кухня"}, "Грузинские рестики": {"грузинская кухня"}, "Азиатская кухня": {"азиатская кухня"},
		"хочу суши": {"азиатская кухня"}, "рыбный ресторан": {"морепродукты"}, "посмотреть на рыбок": {"океанариум"},
		"Что-то с рыбами": {"океанариум", "морепродукты"}, "готовить вместе пасту на мастер-классе": {"кулинарный мастер-класс"},
	} {
		if g := read(s).G; !slices.Equal(g, want) {
			t.Errorf("%q: %v, want %v", s, g, want)
		}
	}
	if in := read("А что на Новый год"); in.From != "2026-12-30" || in.To != "2027-01-08" {
		t.Errorf("Новый год: %s..%s", in.From, in.To)
	}
	// the model's broad kind gives way to the words' narrow one
	if g := withKinds(Intent{I: "find", G: []string{"кухня мира"}}, "грузинское что-нибудь").G; !slices.Equal(g, []string{"грузинская кухня"}) {
		t.Errorf("narrow: %v", g)
	}
}

// the model's moods and «наши / новое» stand only where the words say so
func TestWordsBacked(t *testing.T) {
	m := wordsBacked(Intent{I: "find", Moods: []string{"culture"}, G: []string{"итальянская кухня"}}, "Лучшие итальянские рестораны")
	if len(m.Moods) != 0 {
		t.Errorf("no mood was said: %v", m.Moods)
	}
	m = wordsBacked(Intent{I: "find", Moods: []string{"romantic"}, Whose: "ours"}, "что-нибудь романтичное до 5000")
	if !slices.Equal(m.Moods, []string{"romantic"}) || m.Whose != "" {
		t.Errorf("romantic stays, «ours» goes: %+v", m)
	}
	if m := wordsBacked(Intent{I: "find", Whose: "ours"}, "что-нибудь из нашей копилки"); m.Whose != "ours" {
		t.Errorf("ours was said: %+v", m)
	}
	if m := wordsBacked(Intent{I: "find", Whose: "new"}, "Хочу другие варианты которых нет у нас в банке"); m.Whose != "new" {
		t.Errorf("new was said: %+v", m)
	}
}

// a cuisine fits only a place to eat; «кино» is a film, not «кинопарк»
func TestKindFitsCategory(t *testing.T) {
	expo := store.Idea{Idea: domain.Idea{Title: "«Ютори — слушая воздух» в галерее", Category: "culture"}, Why: "японская графика"}
	cafe := store.Idea{Idea: domain.Idea{Title: "«Белый журавль» — корейский ресторан", Category: "food"}}
	if fitsKinds(expo, []string{"азиатская кухня"}) || !fitsKinds(cafe, []string{"азиатская кухня"}) {
		t.Error("азиатская кухня: only the restaurant")
	}
	park := store.Idea{Idea: domain.Idea{Title: "Автокиновыставка в кинопарке «Москино»", Category: "culture"}}
	film := store.Idea{Idea: domain.Idea{Title: "Кино в «Октябре»: премьера", Category: "culture"}}
	if fitsKinds(park, []string{"кино"}) || !fitsKinds(film, []string{"кино"}) {
		t.Error("кино: only the film")
	}
}
