package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"vdvoem/internal/ai"

	"vdvoem/internal/app"
	"vdvoem/internal/domain"
	"vdvoem/internal/leisure"
	"vdvoem/internal/store"
)

// do executes an understood request. Every fact in the answer comes from the data, never from a model.
func (r *run) do(ctx context.Context, in Intent, text string) Reply {
	switch in.I {
	case "remember":
		if in.Text == "" {
			in.Text = text
		}
		if r.dry {
			return Reply{Text: "(проба) Запомнил бы: «" + short(in.Text, 80) + "»."}
		}
		if _, err := r.as.App.Remember(ctx, r.c, in.Text); err != nil {
			return Reply{Text: "Не понял, что запомнить."}
		}
		if tags := r.as.App.NudgeTaste(ctx, r.c, in.Text); len(tags) > 0 { // «обожаю джаз» moves «Мой вкус» too
			r.propose(ctx, "open", map[string]any{"path": "taste"}, "Мой вкус")
			return Reply{Text: "Запомнил: «" + short(in.Text, 80) + "» — и отметил в «Моём вкусе»: " + strings.Join(tags, ", ") + ". Подборки сразу это учтут."}
		}
		return Reply{Text: "Запомнил: «" + short(in.Text, 80) + "». Буду учитывать — и скаут тоже. Посмотреть и удалить — «Мы → Помощник помнит»."}
	case "taste":
		return r.taste(ctx)
	case "goal":
		if in.Text == "" {
			in.Text = text
		}
		if r.dry {
			return Reply{Text: "(проба) Скаут следил бы: «" + short(in.Text, 80) + "»."}
		}
		if _, err := r.as.App.AddGoal(ctx, r.c, in.Text); err != nil {
			return Reply{Text: "Не понял, за чем следить."}
		}
		return Reply{Text: "Скаут будет следить: «" + short(in.Text, 80) + "». Напишу, когда найдётся — даже если это через пару месяцев."}
	case "add_idea":
		if strings.TrimSpace(in.Text) == "" {
			return Reply{Text: "Что добавить? Напиши, например: «добавь каток в Парке Горького» или пришли ссылку."}
		}
		r.propose(ctx, "add_idea", map[string]any{"text": in.Text}, "Добавить «"+short(in.Text, 28)+"»")
		return Reply{Text: "Добавить в копилку?"}
	case "budget":
		r.exec(ctx, "propose_budget", mustJSON(map[string]any{"rub": in.Rub}))
		if in.Rub == 0 {
			return Reply{Text: "Убрать лимит бюджета?"}
		}
		return Reply{Text: "Поставить бюджет " + domain.Money(in.Rub*100) + " на встречу?"}
	case "suggest_mode":
		r.exec(ctx, "propose_suggest_mode", mustJSON(map[string]any{"mode": in.Mode}))
		return Reply{Text: "Поменять, как я присылаю идеи?"}
	case "plan":
		o := r.overview(ctx).(map[string]any)
		if oc, ok := o["open_choice"].(map[string]any); ok {
			return Reply{Text: "Выбор уже идёт — ждём: " + strings.Join(oc["waiting_for"].([]string), ", ") + "."}
		}
		r.exec(ctx, "propose_round", nil)
		return Reply{Text: "Начнём? Сначала отметите время, потом я дам три варианта."}
	case "overview":
		return r.overviewText(ctx)
	case "past":
		res := r.past(ctx, 5).(map[string]any)
		l, _ := res["meetings"].([]map[string]any)
		if len(l) == 0 {
			return Reply{Text: "Пока ни одной завершённой встречи. Самое время первую запланировать."}
		}
		var lines []string
		for _, m := range l {
			lines = append(lines, "• "+fmt.Sprint(m["title"])+" — "+fmt.Sprint(m["when"]))
		}
		return Reply{Text: "Недавно вы были:\n" + strings.Join(lines, "\n")}
	case "chores":
		return r.choresText(ctx)
	case "price":
		return r.price(ctx, in)
	case "book":
		return r.book(ctx, in)
	case "about":
		return r.about(ctx, in, text)
	case "suggest":
		return r.suggestText(ctx, in)
	case "find":
		return r.find(ctx, in, text)
	}
	if !isGreeting(text) {
		if t, ok := r.chatter(ctx, text); ok {
			return Reply{Text: t}
		}
	}
	h := r.as.hello(r.c)
	if in.conf < 0.5 && in.I == "chat" && !isGreeting(text) {
		h.Text = "Не до конца понял. " + h.Text + "\n• Хотим чилить в воскресенье\n• Сколько стоит …?"
	}
	return h
}

func isGreeting(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return has(t, "привет", "здравств", "hi", "hello", "добрый", "что ты умеешь", "помощь", "кто ты")
}

func (r *run) overviewText(ctx context.Context) Reply {
	o := r.overview(ctx).(map[string]any)
	txt := "Ничего не запланировано."
	if nm, ok := o["next_meeting"].(map[string]any); ok {
		txt = fmt.Sprintf("Ближайшая встреча: %s, %s.", nm["title"], nm["when"])
	}
	if oc, ok := o["open_choice"].(map[string]any); ok {
		txt += " Идёт выбор — ждём: " + strings.Join(oc["waiting_for"].([]string), ", ") + "."
	} else if _, ok := o["next_meeting"]; !ok {
		r.exec(ctx, "propose_round", nil)
	}
	return Reply{Text: txt}
}

func (r *run) choresText(ctx context.Context) Reply {
	res := r.chores(ctx).(map[string]any)
	l, _ := res["chores"].([]map[string]any)
	if len(l) == 0 {
		return Reply{Text: "Дел в списке нет."}
	}
	var lines []string
	for _, c := range l {
		line := "• " + fmt.Sprint(c["title"]) + " — " + fmt.Sprint(c["who"])
		if d, ok := c["deadline"]; ok {
			line += ", до " + fmt.Sprint(d)
		}
		lines = append(lines, line)
	}
	return Reply{Text: strings.Join(lines, "\n")}
}

// ---------- find: afisha → bank → catalog ----------

func (in Intent) afisha() afishaQ {
	return afishaQ{Date: in.Date, Weekend: in.Wknd, Part: in.Part, MaxRub: in.Max, Include: in.Cats, Exclude: in.Not, NotWords: in.NotW,
		Query: in.Q, Horizon: in.Hz, WholeBank: !in.Scout, Moods: in.Moods, Limit: 4, All: in.All, Whose: in.Whose, From: in.From, To: in.To, G: in.G}
}

var moodWords = map[string]string{"chill": "отдохнуть", "active": "подвигаться", "romantic": "романтика", "culture": "культурно", "fun": "повеселиться",
	"food": "вкусно поесть", "outdoor": "на воздухе", "home": "дома", "free": "бесплатно", "new": "что-то новое", "night": "ночью", "kids": "с детьми"}

var catNames = map[string]string{"food": "еда", "culture": "культура", "walk": "прогулки", "sport": "спорт", "other": "другое"}

var partNames = map[string]string{"morning": "утром", "day": "днём", "evening": "вечером"}

// conditions in words — so the person sees how they were understood
func (r *run) cond(in Intent) string {
	var p []string
	loc := r.c.Loc()
	if len(in.G) > 0 {
		p = append(p, strings.Join(in.G, ", "))
	}
	if in.From != "" {
		f, _ := time.ParseInLocation("2006-01-02", in.From, loc)
		t, _ := time.ParseInLocation("2006-01-02", in.To, loc)
		if f.Month() == t.Month() {
			p = append(p, fmt.Sprintf("%d–%d %s", f.Day(), t.Day(), monthForms[t.Month()-1][1]))
		} else {
			p = append(p, fmt.Sprintf("%d %s – %d %s", f.Day(), monthForms[f.Month()-1][1], t.Day(), monthForms[t.Month()-1][1]))
		}
	} else if in.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", in.Date, loc); err == nil {
			p = append(p, dayWord(d, r.as.now().In(loc)))
		}
	} else if in.Wknd {
		p = append(p, "на выходных")
	}
	if w := partNames[in.Part]; w != "" {
		p = append(p, w)
	}
	if in.Max > 0 {
		p = append(p, "до "+domain.Money(in.Max*100))
	}
	for _, c := range in.Cats {
		p = append(p, catNames[c])
	}
	for _, c := range in.Not {
		p = append(p, "без «"+catNames[c]+"»")
	}
	for _, w := range in.NotW {
		p = append(p, "без «"+w+"»")
	}
	for _, m := range in.Moods {
		if w := moodWords[m]; w != "" {
			p = append(p, w)
		}
	}
	if in.Hz == "later" {
		p = append(p, "на месяцы вперёд")
	}
	return strings.Join(p, ", ")
}

func dayWord(d, now time.Time) string {
	d0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch int(d.Sub(d0).Hours() / 24) {
	case 0:
		return "сегодня"
	case 1:
		return "завтра"
	case 2:
		return "послезавтра"
	}
	return "в " + strings.ToLower(ruWeekdayAcc(d)) + " " + d.Format("02.01")
}

func ruWeekdayAcc(t time.Time) string {
	return [...]string{"воскресенье", "понедельник", "вторник", "среду", "четверг", "пятницу", "субботу"}[t.Weekday()]
}

func (r *run) find(ctx context.Context, in Intent, text string) Reply {
	cond := r.cond(in)
	q := in.afisha()
	q.Sem = r.as.Semantic(ctx, r.c.Space.ID, firstNonEmpty(in.Q, text))
	q.WholeBank = true // the bank and the afisha together; the scout's own finds rank by the same taste
	if in.Scout && !in.refined {
		q.WholeBank = false
	}
	res := r.afisha(ctx, q).(map[string]any)
	events, _ := res["events"].([]map[string]any)
	if len(events) == 0 && !q.WholeBank {
		q.WholeBank = true
		res = r.afisha(ctx, q).(map[string]any)
		events, _ = res["events"].([]map[string]any)
	}
	near := ""
	if len(events) == 0 && len(q.G) > 0 {
		// nothing of exactly this kind: the closest of the same sort (category, mood, days) — said plainly
		near = strings.Join(q.G, ", ")
		q.G = nil
		res = r.afisha(ctx, q).(map[string]any)
		events, _ = res["events"].([]map[string]any)
	}
	found, _ := res["found"].(int)
	r.found = found
	if len(events) > 0 {
		if near != "" {
			// not what was asked: no «Идём» on something else — offer to watch for the very thing
			r.proposeAlt(ctx, map[string]any{"intent": Intent{I: "goal", Text: near}, "text": "следи за " + near}, "🔭 Следить: "+short(near, 22))
		} else if !r.bookButton(ctx, events) {
			if id, _ := events[0]["id"].(int64); id != 0 {
				r.propose(ctx, "plan", map[string]any{"idea_id": id}, "Идём: "+short(fmt.Sprint(events[0]["title"]), 26))
			}
		}
		if len(events) < 3 {
			r.catalogButtons(ctx, in, 2)
		}

		// a collection only when there is more than the cards show
		if ids, _ := res["ids"].([]int64); len(ids) > len(events) {
			title := upperFirst(cond)
			if in.Scout && title == "" {
				title = "Новое от скаута"
			}
			if title == "" {
				title = "Все варианты"
			}
			if r.dry {
				r.pick = &PickRef{Title: title, N: len(ids)}
			} else if id, err := r.as.App.S.AddPick(ctx, store.Pick{SpaceID: r.c.Space.ID, MemberID: r.c.Me.ID, Title: title, IDs: ids}, r.as.now()); err == nil {
				r.pick = &PickRef{ID: id, Title: title, N: len(ids)}
			}
		}
		if in.All {
			r.propose(ctx, "open", map[string]any{"path": "ideas/scout"}, "Вся афиша")
			return Reply{Text: r.list(events, found, in, cond)}
		}
		t := r.talk(events, found, in, cond)
		if near != "" {
			t = "Именно «" + near + "» сейчас нет. Близкое по духу — ниже; могу следить, когда появится.\n" + t
		}
		// new things come only from the scout: asked for them in words — it searches tomorrow morning; little that is
		// new among the answers — one tap asks it to
		what := strings.TrimSpace(firstNonEmpty(cond, in.Q, text))
		// a definite kind («кино в субботу») with little or nothing of it: the scout looks without being asked
		thin := near != "" || len(in.G) > 0 && found < 3
		if askNewRe.MatchString(strings.ToLower(text)) && !in.Scout || thin { // «что нового нашёл скаут» asks about finds, not for a search
			if r.askScout(ctx, "Найти новое: "+what, 0) == nil {
				t += "\n🔭 Попросил скаута поискать новое — " + short(what, 40) + ". Принесёт утром, пришлю сюда и в бот."
			}
		} else if r.fresh(ctx, events) < 2 {
			r.propose(ctx, "scout_ask", map[string]any{"text": "Найти новое: " + what}, "🔭 Поискать новое")
		}
		return Reply{Text: t}
	}
	// nothing in the data: where to look right now (a definite kind — today's schedule outside), ideas of the
	// kind as buttons to add — never of another kind: «кино» gets no «планетарий» — and a question to the scout
	r.lookNow(ctx, in)
	n := r.catalogButtons(ctx, in, 3)
	txt := "В афише и копилке под это ничего нет"
	if cond != "" {
		txt += " (" + cond + ")"
	}
	txt += "."
	if n > 0 {
		txt += " Вот идеи под такой запрос — добавить в копилку одним нажатием:"
	} else {
		txt += " Ослабь условия — или скажи «следи за …», и скаут поищет специально."
	}
	if in.Q != "" || len(in.Cats) > 0 || in.Hz == "later" {
		_ = r.askScout(ctx, "Найти: "+strings.TrimSpace(in.Q+" "+cond), 0)
		txt += "\nСкаут поищет это при следующем обходе и ответит здесь, в боте."
	}
	return Reply{Text: txt}
}

// spendRe: a question about money already spent («сколько мы потратили», «наши траты за месяц»)
var spendRe = regexp.MustCompile(`потратил|потрачен|(?:наши|мои|все|общие) траты|траты за|наши расход|расходы за|сколько (?:у нас )?(?:ушло|вышло) (?:денег|на )|во сколько нам обошл`)

// spend: what the meetings cost, by the prices of the events (what was paid is not known — said so)
func (r *run) spend(ctx context.Context) Reply {
	st, err := r.as.App.Stats(ctx, r.c)
	if err != nil || st.Meetings == 0 {
		return Reply{Text: "Пока ни одной завершённой встречи — тратить было не на что. Как сходите куда-нибудь, посчитаю."}
	}
	t := fmt.Sprintf("В этом месяце: %s, %s вместе", plural(st.ThisMonth, "встреча", "встречи", "встреч"), hoursText(st.HoursMonth))
	if st.SpentMonth > 0 {
		t += ", около " + domain.Money(st.SpentMonth)
	}
	t += fmt.Sprintf(".\nЗа всё время: %s, %s", plural(st.Meetings, "встреча", "встречи", "встреч"), hoursText(st.Hours))
	if st.SpentKop > 0 {
		t += ", около " + domain.Money(st.SpentKop)
	}
	t += ".\nСчитаю по ценам событий на двоих — сколько заплатили на самом деле, я не знаю."
	return Reply{Text: t}
}

func hoursText(h float64) string {
	n := int(h + 0.5)
	return plural(n, "час", "часа", "часов")
}

// missRe: the previous answer missed — said in a few words, without a new request
var missRe = regexp.MustCompile(`^(?:(?:нет|ну|опять|снова|всё|все|совсем|вообще|блин)[,!.]?\s+)*(?:мимо|не то|не туда|не попал|не угадал|не понял|не понимаешь|ты не понял|это не то|не то,? что (?:я )?(?:просил|спрашивал|хотел))[\s!.?)(]*$`)

// miss: «опять мимо» — the previous request becomes a miss (the admin's «Промахи», labeled and trained on), the
// scout gets it as a question, and the answer says so and asks for one more word.
func (r *run) miss(ctx context.Context, pt Turn) Reply {
	if e, err := r.as.App.S.LastExample(ctx, r.c.Me.ID); err == nil && !e.Accepted && !r.dry {
		_ = r.as.App.S.SetOutcome(ctx, e.ID, "rephrased", "")
	}
	what := strings.TrimSpace(firstNonEmpty(pt.Text, describe(pt.In)))
	t := "Понял, мимо — записал как промах, на таких доучиваю себя."
	if pt.In.I == "find" && what != "" && r.askScout(ctx, "Найти новое: "+what, 0) == nil {
		t += "\n🔭 Попросил скаута поискать «" + short(what, 50) + "» — принесёт утром, пришлю сюда и в бот."
	}
	t += "\nСкажи одним-двумя словами, что именно хотелось: например «кино», «стендап», «ужин в грузинском»."
	return Reply{Text: t}
}

var askNewRe = regexp.MustCompile(`поищи|найди (что-нибудь |что-то )?нов|что нового|новеньк|чего-нибудь нового|что-то новое|что-нибудь новое|ещё вариант|еще вариант|другие вариант`)

// fresh: how many of the answers are new to us — the scout's or a channel's finds nobody has marked yet
func (r *run) fresh(ctx context.Context, events []map[string]any) int {
	n := 0
	for _, e := range events {
		if id, _ := e["id"].(int64); id != 0 {
			if i, err := r.as.App.S.Idea(ctx, r.c.Space.ID, id); err == nil && !Ours(i) && i.WishA == domain.WishNone && i.WishB == domain.WishNone {
				n++
			}
		}
	}
	return n
}

// the first event with its own date and time gets «Идём!» — a meeting in the calendar right away
func (r *run) bookButton(ctx context.Context, events []map[string]any) bool {
	for _, e := range events {
		id, _ := e["id"].(int64)
		if id == 0 {
			continue
		}
		i, err := r.as.App.S.Idea(ctx, r.c.Space.ID, id)
		if err != nil || !fixedTime(i, r.c.Loc(), r.as.now()) {
			continue
		}
		r.propose(ctx, "book", map[string]any{"idea_id": i.ID}, "Идём: "+short(i.Title, 24))
		return true
	}
	return false
}

func fixedTime(i store.Idea, loc *time.Location, now time.Time) bool {
	if i.StartsAt == nil || i.StartsAt.Before(now) || (i.EndsAt != nil && i.EndsAt.Sub(*i.StartsAt) > 30*time.Hour) {
		return false
	}
	s := i.StartsAt.In(loc)
	return !(s.Hour() == 0 && s.Minute() == 0)
}

func (r *run) catalogButtons(ctx context.Context, in Intent, n int) int {
	if len(in.Moods) == 0 && len(in.Cats) == 0 {
		return 0
	}
	ideas, _ := r.as.App.S.Ideas(ctx, r.c.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	have := make([]string, 0, len(ideas))
	for _, i := range ideas {
		have = append(have, i.Title)
	}
	moods := in.Moods
	for _, c := range in.Cats {
		if c == "food" || c == "culture" {
			moods = append(moods, c)
		}
	}
	k := 0
	for _, c := range app.CatalogFor(moods, have, in.Max, n+8) {
		if k >= n {
			break
		}
		if len(in.Cats) > 0 && !contains(in.Cats, c.Category) || contains(in.Not, c.Category) || len(in.G) > 0 && !fitsAny(in.G, c.Title) {
			continue
		}
		r.propose(ctx, "add_idea", map[string]any{"text": c.Title}, "+ "+short(c.Title, 30))
		k++
	}
	return k
}

func fitsAny(kinds []string, title string) bool {
	for _, g := range kinds {
		if leisure.Fits(g, title) {
			return true
		}
	}
	return false
}

// lookNow: a definite kind with nothing in our data — a button to today's schedule outside (Яндекс Афиша for
// shows, Яндекс Карты for places), so «что сегодня в кино» is answered now, not tomorrow morning
func (r *run) lookNow(ctx context.Context, in Intent) {
	if len(in.G) == 0 {
		return
	}
	g := in.G[0]
	city := strings.TrimSpace(r.c.Space.City)
	if city == "" {
		city = "Москва"
	}
	when := ""
	if in.From != "" && in.From == in.To {
		if d, err := time.Parse("2006-01-02", in.From); err == nil {
			when = " " + d.Format("02.01")
		}
	}
	q, label := g, "Где идёт: "+g
	if leisure.CatOf(g) == "food" {
		u := "https://yandex.ru/maps/?text=" + url.QueryEscape(g+" "+city)
		r.propose(ctx, "link", map[string]any{"url": u}, "🗺 "+short(g, 22)+" на карте")
		return
	}
	if g == "кино" {
		q, label = "кино расписание сеансов", "🎬 Сеансы"+when
	}
	u := "https://yandex.ru/search/?text=" + url.QueryEscape(q+" "+city+when)
	r.propose(ctx, "link", map[string]any{"url": u}, short(label, 30))
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// ---------- suggest: one pick from the bank ----------

func (r *run) suggestText(ctx context.Context, in Intent) Reply {
	switch {
	case in.Whose == "ours":
		r.from = ""
	case in.Scout || in.Whose == "new":
		r.from = "afisha"
	default: // «удиви» picks from everything — the bank and what the scout found
		r.from = "all"
	}
	cat := ""
	if len(in.Cats) == 1 {
		cat = in.Cats[0]
	}
	if len(in.Moods) > 0 && in.Mode != "random" { // a mood is a search, not a lottery
		in.I = "find"
		return r.find(ctx, in, "")
	}
	res := r.suggest(ctx, in.Mode, in.Date, in.Part, in.Max, cat, 3)
	if in.Mode == "random" {
		return Reply{Text: textFor(res, "🎲 Вот что выпало "+map[string]string{"": "из вашей копилки", "afisha": "из того, что нашли скаут и каналы", "all": "из всего, что есть"}[r.from]+".")}
	}
	return Reply{Text: textFor(res, "Подобрал по вашим оценкам — первый вариант мой фаворит.")}
}

// ---------- price: data → the page → the scout + a link ----------

func (r *run) price(ctx context.Context, in Intent) Reply {
	q := strings.TrimSpace(in.Q)
	if q == "" {
		return Reply{Text: "Цену чего узнать? Напиши название — например, «сколько стоит Щелкунчик»."}
	}
	a := r.as.App.Price(ctx, r.c, q)
	if a.Idea == nil {
		return Reply{Text: "Не нашёл «" + short(q, 40) + "» ни в афише, ни в копилке. Спросил скаута — ответит в боте.\nПосмотреть самому: " + a.SearchURL}
	}
	r.addCard(ideaCard(*a.Idea, ""))
	if fixedTime(*a.Idea, r.c.Loc(), r.as.now()) {
		r.propose(ctx, "book", map[string]any{"idea_id": a.Idea.ID}, "Идём: "+short(a.Idea.Title, 24))
	}
	if a.PriceKop > 0 {
		src := "по данным скаута"
		if a.Fresh {
			src = "только что проверил на странице"
		}
		txt := fmt.Sprintf("«%s» — около %s на двоих (%s).", a.Idea.Title, domain.Money(a.PriceKop), src)
		if a.Link != "" {
			txt += "\nТочная цена и билеты: " + a.Link
		}
		return Reply{Text: txt}
	}
	return Reply{Text: "Цену «" + a.Idea.Title + "» пока не знаю — спросил скаута, он ответит в боте.\nПосмотреть сейчас: " + a.Link}
}

// ---------- book: an event with a time → the calendar ----------

func (r *run) book(ctx context.Context, in Intent) Reply {
	i, ok := r.as.App.FindByName(ctx, r.c, in.Q)
	if !ok {
		return Reply{Text: "Не нашёл «" + short(in.Q, 40) + "». Пришли ссылку — добавлю и сразу позову " + app.NameAcc(r.c.PartnerName()) + "."}
	}
	r.addCard(ideaCard(i, ""))
	_, err := r.as.App.BookFixed(ctx, r.c, i.ID)
	if errors.Is(err, app.ErrNoTime) {
		_, err = r.as.App.GoWith(ctx, r.c, i.ID)
		if err == nil {
			return Reply{Text: "У «" + i.Title + "» нет точного времени — отправил вам обоим выбрать, когда. Потом событие придёт в календарь."}
		}
	}
	switch {
	case errors.Is(err, app.ErrNotPaired):
		return Reply{Text: "Сначала пригласи партнёра — планировать вдвоём интереснее."}
	case errors.Is(err, app.ErrRoundOpen):
		return Reply{Text: "У вас уже идёт выбор — завершите его, и я сразу запишу «" + i.Title + "»."}
	case err != nil:
		return Reply{Text: "Не получилось записать — попробуй из карточки события."}
	}
	return Reply{Text: "Позвал " + app.NameAcc(r.c.PartnerName()) + " на «" + i.Title + "», " + domain.When(*i.StartsAt, r.c.Loc()) + ". Как только подтвердит — событие придёт вам обоим в календарь."}
}

// ---------- about: what it is and whether it suits us ----------

func (r *run) about(ctx context.Context, in Intent, text string) Reply {
	alts := r.as.App.SameName(ctx, r.c, firstNonEmpty(text, in.Q))
	if len(alts) > 1 {
		// two ideas answer to the name: ask which one, never pick silently
		for _, a := range alts {
			r.proposeAlt(ctx, map[string]any{"intent": Intent{I: "about", Q: a.Title}, "text": "расскажи про " + a.Title}, short(a.Title, 34))
		}
		return Reply{Text: "У вас " + plural(len(alts), "идея", "идеи", "идей") + " с таким названием. Про какую?"}
	}
	i, ok := r.as.App.FindByName(ctx, r.c, in.Q)
	if len(alts) == 1 { // the whole phrase points at one idea — better than the short query alone
		i, ok = alts[0], true
	}
	if !ok {
		_ = r.askScout(ctx, "Расскажи про: "+in.Q, 0)
		return Reply{Text: "Не нашёл «" + short(in.Q, 40) + "» в афише и копилке. Спросил скаута — расскажет в боте. Если есть ссылка или пост — пришли, разберу."}
	}
	ab := r.as.App.About(ctx, r.c, i)
	loc := r.c.Loc()
	var b strings.Builder
	b.WriteString("«" + i.Title + "»")
	var meta []string
	if i.Address != "" {
		meta = append(meta, i.Address)
	}
	if i.StartsAt != nil {
		meta = append(meta, domain.When(*i.StartsAt, loc))
	}
	if i.PriceMin > 0 {
		meta = append(meta, "≈ "+domain.Money(i.PriceMin)+" на двоих")
	}
	if len(meta) > 0 {
		b.WriteString(" — " + strings.Join(meta, " · "))
	}
	b.WriteString(".")
	if g := i.Info.Genres; len(g) > 0 || i.Info.Age != "" {
		line := strings.Join(g, ", ")
		if i.Info.Age != "" {
			line = strings.TrimPrefix(line+" · "+i.Info.Age, " · ")
		}
		b.WriteString("\n" + upperFirst(line))
	}
	if len(i.Info.Cast) > 0 {
		b.WriteString("\nВ ролях: " + strings.Join(i.Info.Cast, ", "))
	}
	if i.Info.About != "" {
		b.WriteString("\n\n" + i.Info.About)
	} else if i.Why != "" {
		b.WriteString("\n" + i.Why)
	}
	if i.Info.Empty() {
		_ = r.askScout(ctx, "Расскажи про: "+i.Title+" (жанр, кто играет, о чём)", i.ID)
		b.WriteString("\n\nПодробностей пока нет — попросил скаута дополнить карточку.")
	}
	// does it suit us
	b.WriteString("\n\n" + r.suits(ab))
	r.addCard(ideaCard(i, ""))
	if fixedTime(i, loc, r.as.now()) {
		r.propose(ctx, "book", map[string]any{"idea_id": i.ID}, "Идём: "+short(i.Title, 24))
	} else {
		r.propose(ctx, "plan", map[string]any{"idea_id": i.ID}, "Идём: "+short(i.Title, 24))
	}
	if me, _ := r.myWish(i); me == domain.WishNone {
		r.propose(ctx, "wish", map[string]any{"idea_id": i.ID, "wish": "want"}, "Хочу")
	}
	return Reply{Text: b.String()}
}

func (r *run) suits(ab app.About) string {
	level := func(x float64) string {
		switch {
		case x >= 0.75:
			return "очень подходит"
		case x >= 0.58:
			return "скорее да"
		case x <= 0.3:
			return "вряд ли"
		case x <= 0.42:
			return "сомнительно"
		}
		return "нейтрально"
	}
	clean := func(l []string) []string { // no cut tokens («мастер…»), no repeats
		var out []string
		seen := map[string]bool{}
		for _, x := range l {
			x = strings.TrimSpace(x)
			if x == "" || strings.HasSuffix(x, "…") || seen[x] {
				continue
			}
			seen[x] = true
			out = append(out, x)
		}
		return out
	}
	why := func(pros, cons []string) string {
		pros, cons = clean(pros), clean(cons)
		var p []string
		if len(pros) > 0 {
			p = append(p, "нравится "+strings.Join(pros, ", "))
		}
		if len(cons) > 0 {
			p = append(p, "не любит "+strings.Join(cons, ", "))
		}
		if len(p) == 0 {
			return ""
		}
		return " (" + strings.Join(p, "; ") + ")"
	}
	person := func(name string, marks int, score float64, pros, cons []string) string {
		if marks < 5 {
			return name + " — пока мало отметок, чтобы судить"
		}
		return name + " — " + level(score) + why(pros, cons)
	}
	lines := []string{"Подойдёт ли вам: " + itoaPct(ab.Match.Both) + " вместе."}
	lines = append(lines, "• "+person("Тебе", ab.MeMarks, ab.Match.Me, ab.MePros, ab.MeCons))
	if r.c.Partner != nil {
		lines = append(lines, "• "+person(r.c.Partner.Name, ab.PartnerMarks, ab.Match.Partner, ab.PaPros, ab.PaCons))
	}
	for _, s := range ab.Said {
		lines = append(lines, "• помню: "+s)
	}
	return strings.Join(lines, "\n")
}

func itoaPct(x float64) string { return fmt.Sprintf("%d%%", int(x*100+0.5)) }

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// talk: an answer the way a good assistant says it — the best option for both first, with when, price and why,
// then the rest in one line. Every fact comes from the found events; nothing is made up.
func (r *run) talk(events []map[string]any, found int, in Intent, cond string) string {
	pn := r.c.PartnerName()
	var b strings.Builder
	n := plural(found, "вариант", "варианта", "вариантов")
	switch {
	case len(in.Moods) > 0:
		fmt.Fprintf(&b, "Под настроение%s — %s.", condSuffix(cond), n)
	case in.Scout:
		fmt.Fprintf(&b, "Скаут нашёл%s %s.", condSuffix(cond), n)
	case cond != "":
		fmt.Fprintf(&b, "%s — %s.", upperFirst(cond), n)
	default:
		fmt.Fprintf(&b, "Нашёл %s.", n)
	}
	// one line about the best one — the cards below carry the rest
	top := events[0]
	b.WriteString(" Лучший для вас двоих — «" + fmt.Sprint(top["title"]) + "»")
	var meta []string
	if w, ok := top["when"].(string); ok && w != "" {
		meta = append(meta, w)
	}
	if p, ok := top["price_rub"].(int64); ok && p > 0 {
		meta = append(meta, "≈ "+domain.Money(p*100))
	}
	if len(meta) > 0 {
		b.WriteString(", " + strings.Join(meta, ", "))
	}
	switch top["partner_wish"] {
	case "want", "very":
		b.WriteString(": " + pn + " " + past(pn, "отметил") + " «хочу»")
	case "skip":
		b.WriteString(", правда " + pn + " это " + past(pn, "пропускал"))
	}
	b.WriteString(".")
	return b.String()
}

// list: «вообще всё» — every event by date (the soonest first), places without a date at the end
func (r *run) list(events []map[string]any, found int, in Intent, cond string) string {
	loc := r.c.Loc()
	at := func(e map[string]any) int64 {
		if t, ok := e["starts_at"].(int64); ok && t > 0 {
			return t
		}
		return 1 << 62
	}
	sort.SliceStable(events, func(a, b int) bool { return at(events[a]) < at(events[b]) })
	var ours, news []map[string]any
	for _, e := range events {
		if o, _ := e["ours"].(bool); o {
			ours = append(ours, e)
		} else {
			news = append(news, e)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Всё, что есть%s — %s.", condSuffix(cond), plural(found, "вариант", "варианта", "вариантов"))
	shown := 0
	section := func(title string, list []map[string]any) {
		if len(list) == 0 || shown >= listMax {
			return
		}
		b.WriteString("\n\n" + title)
		month := ""
		for _, e := range list {
			if shown == listMax {
				break
			}
			if t := at(e); t < 1<<62 {
				if k := time.Unix(t, 0).In(loc).Format("2006-01"); k != month && len(list) > 8 {
					month = k
					b.WriteString("\n— " + ruMonth(time.Unix(t, 0).In(loc)))
				}
			} else if month != "none" && len(list) > 8 {
				month = "none"
				b.WriteString("\n— без даты")
			}
			line := "\n• "
			if w, ok := e["when"].(string); ok && w != "" {
				line += w + " — "
			}
			line += fmt.Sprint(e["title"])
			if p, ok := e["price_rub"].(int64); ok && p > 0 {
				line += " · ≈ " + domain.Money(p*100)
			}
			switch e["partner_wish"] {
			case "want", "very":
				line += " · " + r.c.PartnerName() + " хочет"
			}
			if e["my_wish"] == "want" || e["my_wish"] == "very" {
				line += " · ты хочешь"
			}
			b.WriteString(line)
			shown++
		}
	}
	section(fmt.Sprintf("🗂 Ваше — из копилки (%d):", len(ours)), ours)
	section(fmt.Sprintf("🆕 Новое — вы ещё не видели, нашли скаут и каналы (%d):", len(news)), news)
	if found > shown {
		fmt.Fprintf(&b, "\n\n…и ещё %d — все в «Афише».", found-shown)
	}
	b.WriteString("\n\nСпроси про любое: «расскажи про …», «сколько стоит …», «запиши нас на …» — или «только новое», «только наше».")
	return b.String()
}

const listMax = 40

func ruMonth(t time.Time) string {
	return [...]string{"", "январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}[t.Month()] + " " + strconv.Itoa(t.Year())
}

func past(name, verb string) string { return app.Past(name, verb) }

func condSuffix(c string) string {
	if c == "" {
		return ""
	}
	return " (" + c + ")"
}

func plural(n int, one, few, many string) string {
	m := n % 100
	w := many
	switch {
	case m >= 11 && m <= 14:
	case m%10 == 1:
		w = one
	case m%10 >= 2 && m%10 <= 4:
		w = few
	}
	return fmt.Sprintf("%d %s", n, w)
}

var chatSchema = map[string]any{"type": "object", "properties": map[string]any{"reply": map[string]any{"type": "string"}}, "required": []string{"reply"}}

// chatter: small talk in the model's own words — short, warm, and never a fact: places, prices and dates come only
// from the app's data, so a reply that names one (digits, «…») is dropped for the plain help text.
func (r *run) chatter(ctx context.Context, text string) (string, bool) {
	c := r.as.AI
	if !c.Enabled() {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, LLMBudget)
	defer cancel()
	sys := "Ты — помощник пары в приложении «Вдвоём»: помогаешь выбрать, куда сходить вдвоём, и договориться о времени. " +
		"Ответь по-русски одним-двумя короткими предложениями, тепло и по делу. Не называй мест, событий, цен и дат — их подбирает приложение. " +
		"Если человеку нужна идея — подскажи, что можно написать, например «хотим почилить в субботу» или «что интересного на неделе»."
	raw, err := c.Structured(ctx, "chat", []ai.Message{{Role: "system", Content: sys}, {Role: "user", Content: text}}, chatSchema, 70)
	if err != nil {
		return "", false
	}
	var out struct {
		Reply string `json:"reply"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return "", false
	}
	t := strings.TrimSpace(out.Reply)
	if len([]rune(t)) < 8 || len([]rune(t)) > 320 || strings.ContainsAny(t, "0123456789«»\"") || !cyrillic(t) {
		return "", false
	}
	return t, true
}

func cyrillic(s string) bool {
	n, cy := 0, 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
			if unicode.Is(unicode.Cyrillic, r) {
				cy++
			}
		}
	}
	return n > 0 && cy*10 >= n*8
}

// taste: «обо мне» — the portrait from their own words and marks, and the way to correct it
func (r *run) taste(ctx context.Context) Reply {
	r.propose(ctx, "open", map[string]any{"path": "taste"}, "✨ Настроить мой вкус")
	p := r.as.App.Portrait(ctx, r.c, r.c.Me.ID)
	if p == "" {
		return Reply{Text: "Пока я знаю о тебе мало — только то, что ты отмечаешь. Давай настроим за минуту: отметишь, что нравится — от сфер до жанров, — и подборки станут точнее с первого раза.", Next: []string{"Что нам подходит на выходных?"}}
	}
	return Reply{Text: "Вот что я о тебе знаю:\n" + p + "\n\nПоправь или дополни — подборки подстроятся сразу.", Next: []string{"Что мне понравится на неделе?", "Удиви нас"}}
}

// askScout: a question to the scout (not in a dry run)
func (r *run) askScout(ctx context.Context, text string, ideaID int64) error {
	if r.dry {
		return nil
	}
	return r.as.App.AskScout(ctx, r.c, text, ideaID)
}
