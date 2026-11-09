package app

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"vdvoem/internal/domain"
	"vdvoem/internal/leisure"
	"vdvoem/internal/store"
)

// Genres and other facts about an event, read from its own text (a post, a page, the scout's notes).
// Deterministic: a word list, not a guess — the taste model then learns which genres each person picks.

var genreWords = []struct {
	g     string
	words []string
}{
	{"комедия", []string{"комеди", "смешн", "юмор", "уморит", "хохот", "водевил", "фарс"}},
	{"антикомедия", []string{"антикомеди"}},
	{"драма", []string{"драма", "драмат"}},
	{"трагедия", []string{"трагеди"}},
	{"мелодрама", []string{"мелодрам", "история любви", "о любви"}},
	{"мюзикл", []string{"мюзикл"}},
	{"оперетта", []string{"оперетт", "кальман", "легар"}},
	{"опера", []string{"опера ", "оперы", "оперу", "опере "}},
	{"балет", []string{"балет", "щелкунчик", "лебединое"}},
	{"классика", []string{"чехов", "шекспир", "островск", "гоголь", "гоголя", "пушкин", "достоевск", "толст", "булгаков", "мольер", "грибоедов", "тургенев", "классик"}},
	{"современная пьеса", []string{"современн пьес", "новая драма", "современной драм"}},
	{"моноспектакль", []string{"моноспектакл"}},
	{"иммерсивный", []string{"иммерсив"}},
	{"детям", []string{"для детей", "детск", "сказк", "семейн", " 0+", " 6+"}},
	{"концерт", []string{"концерт", "оркестр", "симфон", "филармон"}},
	{"джаз", []string{"джаз"}},
	{"рок", []string{"рок-", " рок ", "рок-группа"}},
	{"классическая музыка", []string{"классическ музык", "симфон", "спиваков", "камерн"}},
	{"стендап", []string{"стендап", "stand-up", "standup"}},
	{"шоу", []string{" шоу", "шоу "}},
	{"выставка", []string{"выставк", "экспозиц"}},
	{"лекция", []string{"лекци"}},
	{"экскурсия", []string{"экскурси"}},
	{"фестиваль", []string{"фестивал"}},
	{"гастро", []string{"гастро", "дегустац", "ужин-спектакл"}},
	{"кино", []string{"кинопоказ", "фильм", "кино "}},
	{"мистика", []string{"мистик", "мистич", "призрак"}},
	{"детектив", []string{"детектив", "убийств", "расследован"}},
	{"исторический", []string{"историческ", "эпох", "века "}},
}

// GenresOf: genres named in a text (title + description), most specific first.
func GenresOf(text string) []string {
	t := " " + strings.ToLower(text) + " "
	var out []string
	for _, g := range genreWords {
		for _, w := range g.words {
			if strings.Contains(t, w) {
				out = append(out, g.g)
				break
			}
		}
	}
	if contains(out, "антикомедия") {
		out = remove(out, "комедия")
	}
	if len(out) > 4 {
		out = out[:4]
	}
	// the shared map of kinds (джаз, керамика, баня…): the same tags the assistant searches by
	for _, k := range leisure.Detect(text) {
		if !contains(out, k) && len(out) < 6 {
			out = append(out, k)
		}
	}
	return out
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func remove(l []string, v string) []string {
	var out []string
	for _, x := range l {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

var (
	ageInRe   = regexp.MustCompile(`(?:^|[^\d])(0|6|12|14|16|18)\s?\+`)
	castRe    = regexp.MustCompile(`(?i)(?:в ролях|в главных ролях|исполнители|солист[а-я]*|состав)\s*:?\s*([^\n]{5,240})`)
	starLine  = regexp.MustCompile(`^\s*(?:⭐️|⭐|🌟)\s*([А-ЯЁA-Z][^\n]{3,60})$`)
	nameShape = regexp.MustCompile(`^[А-ЯЁA-Z][а-яёa-zА-ЯЁ.\- ]+[А-ЯЁA-Z][а-яёa-zА-ЯЁ\-]+$`)
	noise     = regexp.MustCompile(`(?i)паспорт|заказ|фио|билет|партер|амфитеатр|бельэтаж|скидк|вместо|₽|руб|пишите|бронир`)
)

// InfoFromText: the facts a post or a page states about the event.
func InfoFromText(title, text string) store.Info {
	var in store.Info
	in.Genres = GenresOf(title + "\n" + text)
	if m := ageInRe.FindStringSubmatch(title + " " + text); m != nil {
		in.Age = m[1] + "+"
	}
	if m := castRe.FindStringSubmatch(text); m != nil {
		for _, n := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ';' }) {
			n = strings.TrimSpace(strings.Trim(n, " .*_"))
			n = strings.TrimSuffix(strings.TrimSuffix(n, " и др"), " и другие")
			if n != "" && len([]rune(n)) <= 40 && len(in.Cast) < 6 {
				in.Cast = append(in.Cast, n)
			}
		}
	}
	if len(in.Cast) == 0 {
		for _, l := range strings.Split(text, "\n") {
			if m := starLine.FindStringSubmatch(l); m != nil {
				n := strings.TrimFunc(strings.NewReplacer("*", "", "_", "", "❗️", "", "❗", "").Replace(m[1]), func(r rune) bool { return !unicode.IsLetter(r) })
				if nameShape.MatchString(n) && len(in.Cast) < 6 {
					in.Cast = append(in.Cast, titleCase(n))
				}
			}
		}
	}
	// the description: the first paragraph of prose
	for _, p := range regexp.MustCompile(`\n\s*\n`).Split(text, -1) {
		p = strings.TrimSpace(strings.NewReplacer("*", "", "_", "").Replace(p))
		letters := 0
		for _, r := range p {
			if unicode.IsLetter(r) && unicode.IsLower(r) {
				letters++
			}
		}
		if len([]rune(p)) >= 90 && letters*10 > len([]rune(p))*5 && !noise.MatchString(p) {
			in.About = sentences(p, 320)
			break
		}
	}
	if !in.Empty() {
		in.Src = "post"
	}
	return in
}

func titleCase(n string) string {
	ws := strings.Fields(strings.ToLower(n))
	for k, w := range ws {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		ws[k] = string(r)
	}
	return strings.Join(ws, " ")
}

// sentences: whole sentences up to n characters
func sentences(p string, n int) string {
	p = strings.Join(strings.Fields(p), " ")
	r := []rune(p)
	if len(r) <= n {
		return p
	}
	cut := string(r[:n])
	if k := strings.LastIndexAny(cut, ".!?…"); k > n/3 {
		return cut[:k+1]
	}
	return strings.TrimSpace(cut) + "…"
}

// FillGenres: ideas already in the bank get genres from what they say (title, note, the scout's «why», the
// description) — so taste, search and «подойдёт ли нам» work on the old ones too. Never overwrites.
func (a *App) FillGenres(ctx context.Context) int {
	spaces, err := a.S.Spaces(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, sp := range spaces {
		ideas, _ := a.S.Ideas(ctx, sp.ID, domain.IdeaActive, domain.IdeaDraft)
		for _, i := range ideas {
			if i.Kind != domain.KindLeisure || len(i.Info.Genres) > 0 {
				continue
			}
			g := GenresOf(strings.Join([]string{i.Title, i.Note, i.Why, i.Info.About}, " "))
			if len(g) == 0 {
				continue
			}
			i.Info.Genres = g
			if i.Info.Src == "" {
				i.Info.Src = "text"
			}
			if a.S.UpdateIdea(ctx, i, a.now()) == nil {
				n++
			}
		}
	}
	return n
}
