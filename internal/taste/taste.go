// Package taste learns what one person likes: a small logistic regression over simple features of
// an idea (category, price band, day, time of day, words of the title, where it came from), trained on
// that person's own marks — «очень», «хочу», «пропущу», «повторим?». Each person has their own model;
// a couple's score is built from the two. Kilobytes, trains in microseconds, explains itself.
package taste

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Item is what the model sees of an idea.
type Item struct {
	Title    string
	Category string
	PriceKop int64
	Starts   *time.Time
	Source   string
	Address  string
	Tags     []string // moods: chill, active, romantic… — the model learns which ones each person picks
	Genres   []string // комедия, драма, мюзикл… — and which genres
	Venue    string   // a theatre / a place: people have favourites
}

// Example is one mark: Label 1 = wants, 0 = skips; Weight how sure (very = 2).
type Example struct {
	Item   Item
	Label  float64
	Weight float64
}

type Model struct {
	W    map[string]float64
	Bias float64
	N    int // examples seen
}

var stop = map[string]bool{"в": true, "на": true, "и": true, "с": true, "по": true, "для": true, "из": true, "the": true, "of": true, "at": true, "для двоих": true}

// Features of an item; prefixes keep groups apart (cat:, price:, dow:, tod:, w:, src:).
func Features(it Item, loc *time.Location) []string {
	f := []string{"cat:" + nz(it.Category, "other")}
	switch p := it.PriceKop / 100; {
	case p == 0:
		f = append(f, "price:free_or_unknown")
	case p <= 1500:
		f = append(f, "price:low")
	case p <= 5000:
		f = append(f, "price:mid")
	default:
		f = append(f, "price:high")
	}
	if it.Starts != nil {
		t := it.Starts.In(loc)
		if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			f = append(f, "dow:weekend")
		} else {
			f = append(f, "dow:weekday")
		}
		switch h := t.Hour(); {
		case h == 0 && t.Minute() == 0: // a date without a time
		case h < 13:
			f = append(f, "tod:morning")
		case h < 18:
			f = append(f, "tod:day")
		default:
			f = append(f, "tod:evening")
		}
	}
	if it.Source != "" {
		f = append(f, "src:"+it.Source)
	}
	for _, t := range it.Tags {
		f = append(f, "tag:"+t)
	}
	for _, g := range it.Genres {
		f = append(f, "g:"+g)
	}
	if v := venueKey(it.Venue); v != "" {
		f = append(f, "v:"+v)
	}
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(it.Title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		w = stem(w)
		if len([]rune(w)) < 4 || stop[w] || seen[w] {
			continue
		}
		seen[w] = true
		f = append(f, "w:"+w)
	}
	return f
}

// stem cuts common Russian endings so «театр», «театра», «театре» meet.
func stem(w string) string {
	r := []rune(w)
	for _, end := range []string{"ами", "ями", "ого", "его", "ому", "ему", "ыми", "ими", "ая", "яя", "ой", "ей", "ий", "ый", "ые", "ие", "ах", "ях", "ом", "ем", "ам", "ям", "ов", "ев", "а", "я", "ы", "и", "е", "у", "ю", "о"} {
		e := []rune(end)
		if len(r)-len(e) >= 4 && string(r[len(r)-len(e):]) == end {
			return string(r[:len(r)-len(e)])
		}
	}
	return w
}

func nz(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// Train fits a fresh model (full batch, L2): with a few hundred marks it takes microseconds,
// so the model is always exactly the person's current marks — no drift, nothing to store.
func Train(ex []Example, loc *time.Location) Model {
	m := Model{W: map[string]float64{}, N: len(ex)}
	if len(ex) == 0 {
		return m
	}
	feats := make([][]string, len(ex))
	for k, e := range ex {
		feats[k] = Features(e.Item, loc)
	}
	const lr, l2, epochs = 0.3, 0.02, 60
	for ep := 0; ep < epochs; ep++ {
		for k, e := range ex {
			z := m.Bias
			for _, f := range feats[k] {
				z += m.W[f]
			}
			g := (e.Label - sigmoid(z)) * e.Weight
			m.Bias += lr * g * 0.5
			for _, f := range feats[k] {
				m.W[f] += lr * (g - l2*m.W[f])
			}
		}
	}
	return m
}

// Score: 0..1 — how much this person would like it. With few marks it stays near the middle.
func (m Model) Score(it Item, loc *time.Location) float64 {
	z := m.Bias
	for _, f := range Features(it, loc) {
		z += m.W[f]
	}
	// shrink toward 0.5 until there are ~10 marks: a model of 2 examples should not be confident
	conf := math.Min(1, float64(m.N)/10)
	return 0.5 + (sigmoid(z)-0.5)*conf
}

// Reasons: what in this item pulls the person's score up and down (strongest first).
func (m Model) Reasons(it Item, loc *time.Location, n int) (pros, cons []string) {
	type kv struct {
		k string
		v float64
	}
	var all []kv
	for _, f := range Features(it, loc) {
		if v := m.W[f]; math.Abs(v) > 0.12 {
			all = append(all, kv{f, v})
		}
	}
	sort.Slice(all, func(i, j int) bool { return math.Abs(all[i].v) > math.Abs(all[j].v) })
	for _, x := range all {
		h := Human(x.k)
		if h == "" {
			continue
		}
		if x.v > 0 && len(pros) < n {
			pros = append(pros, h)
		} else if x.v < 0 && len(cons) < n {
			cons = append(cons, h)
		}
	}
	return
}

// Explain: the strongest likes and dislikes, readable («театр», «недорого», «по выходным»).
func (m Model) Explain(n int) (likes, dislikes []string) {
	type kv struct {
		k string
		v float64
	}
	var all []kv
	for k, v := range m.W {
		if math.Abs(v) > 0.15 {
			all = append(all, kv{k, v})
		}
	}
	sort.Slice(all, func(i, j int) bool { return math.Abs(all[i].v) > math.Abs(all[j].v) })
	for _, x := range all {
		h := Human(x.k)
		if h == "" {
			continue
		}
		if x.v > 0 && len(likes) < n {
			likes = append(likes, h)
		} else if x.v < 0 && len(dislikes) < n {
			dislikes = append(dislikes, h)
		}
	}
	return
}

var human = map[string]string{
	"cat:food": "еда", "cat:culture": "культура", "cat:walk": "прогулки", "cat:sport": "спорт", "cat:other": "разное",
	"price:low": "недорого", "price:mid": "средний чек", "price:high": "дорогое", "price:free_or_unknown": "",
	"dow:weekend": "по выходным", "dow:weekday": "по будням", "tod:morning": "утром", "tod:day": "днём", "tod:evening": "вечером",
	"src:scout": "находки скаута",
	"tag:chill": "спокойный отдых", "tag:active": "активное", "tag:romantic": "романтика", "tag:culture": "культура", "tag:fun": "весёлое",
	"tag:food": "вкусная еда", "tag:outdoor": "на воздухе", "tag:home": "дома", "tag:free": "бесплатное", "tag:new": "новое", "tag:night": "ночное",
}

// venueKey: «МХТ имени А.П. Чехова», «МХТ им. Чехова» → «мхт»; «Театр Вахтангова» → «вахтангов»
func venueKey(v string) string {
	ws := strings.FieldsFunc(strings.ToLower(v), func(r rune) bool { return !unicode.IsLetter(r) })
	skip := map[string]bool{"театр": true, "театра": true, "имени": true, "им": true, "основная": true, "сцена": true, "малый": true, "большой": true, "зал": true, "п": true, "р": true}
	for _, w := range ws {
		if len([]rune(w)) >= 3 && !skip[w] {
			return stem(w)
		}
	}
	return ""
}

func Human(f string) string {
	if h, ok := human[f]; ok {
		return h
	}
	if strings.HasPrefix(f, "g:") {
		return f[2:]
	}
	if strings.HasPrefix(f, "v:") {
		return "площадка «" + f[2:] + "…»"
	}
	if strings.HasPrefix(f, "w:") {
		return "«" + f[2:] + "…»"
	}
	return ""
}
