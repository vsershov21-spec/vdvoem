package app

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"vdvoem/internal/domain"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// FindByName: the idea a person means by a few words («Гамлет», «джаз в пятницу»), from the bank and the afisha.
func (a *App) FindByName(ctx context.Context, c Couple, q string) (store.Idea, bool) {
	words := nameWords(q)
	if len(words) == 0 {
		return store.Idea{}, false
	}
	ideas, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	type sc struct {
		i store.Idea
		s float64
	}
	var all []sc
	for _, i := range ideas {
		have := nameWords(i.Title + " " + i.Address)
		hit := 0
		for _, w := range words {
			for _, h := range have {
				if strings.HasPrefix(h, w) || strings.HasPrefix(w, h) {
					hit++
					break
				}
			}
		}
		if hit > 0 {
			all = append(all, sc{i, float64(hit) / float64(len(words))})
		}
	}
	sort.SliceStable(all, func(x, y int) bool { return all[x].s > all[y].s })
	if len(all) == 0 || all[0].s < 0.5 {
		return store.Idea{}, false
	}
	return all[0].i, true
}

// SameName: the ideas the words point to best — one is the answer, two or more tied is a question to ask.
// Scored over the whole phrase («расскажи про Мастера и Маргариту»), so «мастер» alone does not pull in
// «Мастер-класс по керамике».
func (a *App) SameName(ctx context.Context, c Couple, text string) []store.Idea {
	words := nameWords(text)
	if len(words) == 0 {
		return nil
	}
	ideas, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	best, top := 0, []store.Idea(nil)
	for _, i := range ideas {
		have := nameWords(i.Title)
		hit := 0
		for _, w := range words {
			for _, h := range have {
				if strings.HasPrefix(h, w) || strings.HasPrefix(w, h) {
					hit++
					break
				}
			}
		}
		switch {
		case hit == 0 || hit < best:
		case hit > best:
			best, top = hit, []store.Idea{i}
		default:
			top = append(top, i)
		}
	}
	if best*2 < len(words) && best < 2 { // too little in common to point anywhere
		return nil
	}
	if len(top) > 4 {
		top = top[:4]
	}
	return top
}

func nameWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		r := []rune(w)
		if len(r) < 3 || stopName[w] {
			continue
		}
		if len(r) > 5 {
			w = string(r[:len(r)-2]) // «гамлета» → «гамле», «джазе» → «джаз»
		} else if len(r) >= 4 {
			w = string(r[:len(r)-1]) // «тёщу» / «тёща» → «тёщ»
		}
		out = append(out, w)
	}
	return out
}

var stopName = map[string]bool{"билеты": true, "билет": true, "стоит": true, "сколько": true, "цена": true, "для": true, "двоих": true, "это": true, "наш": true, "нас": true,
	// the words around a name in a question: «расскажи про …», «подойдёт ли нам …», «кто играет в …»
	"про": true, "расскажи": true, "расскажите": true, "подойдёт": true, "подойдет": true, "нам": true, "вам": true, "кто": true, "играет": true,
	"что": true, "такое": true, "чём": true, "чем": true, "хочу": true, "хотим": true, "давай": true, "идти": true, "сходить": true, "пойти": true,
	"понравится": true, "мне": true, "мой": true, "наше": true, "наши": true, "есть": true, "ещё": true, "еще": true}

// PriceAnswer: what we know about the price of a thing — from the bank, a fresh look at its page,
// or, when the page does not say, the link to look yourself; a question goes to the scout either way.
type PriceAnswer struct {
	Idea      *store.Idea
	PriceKop  int64  // for two
	Fresh     bool   // just read from the page
	Link      string // where to look
	Asked     bool   // queued for the scout
	SearchURL string // when we don't know the thing at all
}

func (a *App) Price(ctx context.Context, c Couple, q string) PriceAnswer {
	i, ok := a.FindByName(ctx, c, q)
	if !ok {
		_, _ = a.S.AddQuestion(ctx, store.Question{SpaceID: c.Space.ID, MemberID: c.Me.ID, Text: "Сколько стоит: " + q}, a.now())
		return PriceAnswer{Asked: true, SearchURL: "https://yandex.ru/search/?text=" + url.QueryEscape(q+" билеты цена")}
	}
	ans := PriceAnswer{Idea: &i, PriceKop: i.PriceMin, Link: i.URL}
	if i.URL != "" && a.Fetch != nil {
		fctx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
		r, err := parse.Fetch(fctx, a.Fetch, i.URL)
		cancel()
		if err == nil && r.PriceRub > 0 {
			ans.PriceKop, ans.Fresh = r.PriceRub*2*100, true
			if i.PriceMin != ans.PriceKop {
				i.PriceMin, i.PriceMax = ans.PriceKop, ans.PriceKop
				_ = a.S.UpdateIdea(ctx, i, a.now())
			}
		}
	}
	if ans.PriceKop == 0 {
		_, _ = a.S.AddQuestion(ctx, store.Question{SpaceID: c.Space.ID, MemberID: c.Me.ID, IdeaID: i.ID, Text: "Сколько стоит: " + i.Title}, a.now())
		ans.Asked = true
		if ans.Link == "" {
			ans.Link = "https://yandex.ru/search/?text=" + url.QueryEscape(i.Title+" билеты цена")
		}
	}
	return ans
}

// AskScout: any question the app cannot answer from its data («есть ли билеты на…») — the scout answers in the bot.
func (a *App) AskScout(ctx context.Context, c Couple, text string, ideaID int64) error {
	_, err := a.S.AddQuestion(ctx, store.Question{SpaceID: c.Space.ID, MemberID: c.Me.ID, IdeaID: ideaID, Text: clip(text, 300)}, a.now())
	return err
}

// ScoutAnswer: the agent's reply to a question → the person who asked, in the bot; a price lands on the idea.
type ScoutAnswer struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	PriceRub int64  `json:"price_rub"` // for two, if the question was about a price
	URL      string `json:"url"`
}

func (a *App) ScoutAnswers(ctx context.Context, spaceID int64, ans []ScoutAnswer) (int, error) {
	n := 0
	for _, x := range ans {
		q, ok, err := a.S.AnswerQuestion(ctx, spaceID, x.ID, clip(x.Text, 600), a.now())
		if err != nil || !ok {
			continue
		}
		n++
		if q.IdeaID != 0 && x.PriceRub > 0 {
			if i, err := a.S.Idea(ctx, spaceID, q.IdeaID); err == nil {
				i.PriceMin, i.PriceMax = x.PriceRub*100, x.PriceRub*100
				_ = a.S.UpdateIdea(ctx, i, a.now())
			}
		}
		m, err := a.S.MemberByID(ctx, q.MemberID)
		if err != nil {
			continue
		}
		text := fmt.Sprintf("🔭 Скаут ответил на «%s»:\n%s", esc(q.Text), esc(clip(x.Text, 600)))
		var kb *tg.Markup
		if u := safeURL(x.URL); u != "" {
			kb = tg.Keyboard(tg.Row(tg.Link("Открыть", u)))
		}
		a.send(ctx, m.ChatID, text, kb)
	}
	a.Live.Changed(spaceID, 0, "")
	return n, nil
}

func questionsOf(ctx context.Context, st *store.Store, spaceID int64) []map[string]any {
	qs, _ := st.OpenQuestions(ctx, spaceID)
	out := []map[string]any{}
	for _, q := range qs {
		m := map[string]any{"id": q.ID, "text": q.Text}
		if q.IdeaID != 0 {
			if i, err := st.Idea(ctx, spaceID, q.IdeaID); err == nil {
				m["title"], m["url"] = i.Title, i.URL
			}
		}
		out = append(out, m)
	}
	return out
}
