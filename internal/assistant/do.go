package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"vdvoem/internal/ai"

	"vdvoem/internal/app"
	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

// do executes an understood request. Every fact in the answer comes from the data, never from a model.
func (r *run) do(ctx context.Context, in Intent, text string) Reply {
	switch in.I {
	case "remember":
		if in.Text == "" {
			in.Text = text
		}
		if _, err := r.as.App.Remember(ctx, r.c, in.Text); err != nil {
			return Reply{Text: "Не понял, что запомнить."}
		}
		return Reply{Text: "Запомнил: «" + short(in.Text, 80) + "». Буду учитывать — и скаут тоже. Посмотреть и удалить — «Мы → Помощник помнит»."}
	case "goal":
		if in.Text == "" {
			in.Text = text
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
		return r.about(ctx, in)
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
		Query: in.Q, Horizon: in.Hz, WholeBank: !in.Scout, Moods: in.Moods, Limit: 4}
}

var moodWords = map[string]string{"chill": "отдохнуть", "active": "подвигаться", "romantic": "романтика", "culture": "культурно", "fun": "повеселиться",
	"food": "вкусно поесть", "outdoor": "на воздухе", "home": "дома", "free": "бесплатно", "new": "что-то новое", "night": "ночью", "kids": "с детьми"}

var catNames = map[string]string{"food": "еда", "culture": "культура", "walk": "прогулки", "sport": "спорт", "other": "другое"}

var partNames = map[string]string{"morning": "утром", "day": "днём", "evening": "вечером"}

// conditions in words — so the person sees how they were understood
func (r *run) cond(in Intent) string {
	var p []string
	loc := r.c.Loc()
	if in.Date != "" {
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
	if len(events) > 0 {
		if !r.bookButton(ctx, events) {
			if id, _ := events[0]["id"].(int64); id != 0 {
				r.propose(ctx, "plan", map[string]any{"idea_id": id}, "Идём: "+short(fmt.Sprint(events[0]["title"]), 26))
			}
		}
		if len(events) < 3 {
			r.catalogButtons(ctx, in, 2)
		}
		found, _ := res["found"].(int)
		return Reply{Text: r.talk(events, found, in, cond)}
	}
	// nothing in the data: ideas of the kind, as buttons to add — and a question to the scout
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
		_ = r.as.App.AskScout(ctx, r.c, "Найти: "+strings.TrimSpace(in.Q+" "+cond), 0)
		txt += "\nСкаут поищет это при следующем обходе и ответит здесь, в боте."
	}
	return Reply{Text: txt}
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
	for _, c := range app.CatalogFor(moods, have, in.Max, n) {
		if len(in.Cats) > 0 && !contains(in.Cats, c.Category) || contains(in.Not, c.Category) {
			continue
		}
		r.propose(ctx, "add_idea", map[string]any{"text": c.Title}, "+ "+short(c.Title, 30))
		k++
	}
	return k
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
		return Reply{Text: textFor(res, "🎲 Вот что выпало из вашей копилки.")}
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

func (r *run) about(ctx context.Context, in Intent) Reply {
	i, ok := r.as.App.FindByName(ctx, r.c, in.Q)
	if !ok {
		_ = r.as.App.AskScout(ctx, r.c, "Расскажи про: "+in.Q, 0)
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
		_ = r.as.App.AskScout(ctx, r.c, "Расскажи про: "+i.Title+" (жанр, кто играет, о чём)", i.ID)
		b.WriteString("\n\nПодробностей пока нет — спросил скаута, он дополнит карточку.")
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
	why := func(pros, cons []string) string {
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
	switch {
	case len(in.Moods) > 0:
		fmt.Fprintf(&b, "Под настроение%s нашёл %s.", condSuffix(cond), plural(found, "вариант", "варианта", "вариантов"))
	case in.Scout:
		fmt.Fprintf(&b, "Скаут нашёл%s %s.", condSuffix(cond), plural(found, "вариант", "варианта", "вариантов"))
	case cond != "":
		fmt.Fprintf(&b, "%s — %s.", upperFirst(cond), plural(found, "вариант", "варианта", "вариантов"))
	default:
		fmt.Fprintf(&b, "Нашёл %s.", plural(found, "вариант", "варианта", "вариантов"))
	}
	top := events[0]
	b.WriteString(" Лучше всего вам двоим — «" + fmt.Sprint(top["title"]) + "»")
	var meta []string
	if w, ok := top["when"].(string); ok && w != "" {
		meta = append(meta, w)
	}
	if p, ok := top["price_rub"].(int64); ok && p > 0 {
		meta = append(meta, "≈ "+domain.Money(p*100)+" на двоих")
	}
	if len(meta) > 0 {
		b.WriteString(": " + strings.Join(meta, ", "))
	}
	b.WriteString(".")
	if w, ok := top["why"].(string); ok && w != "" {
		b.WriteString(" " + upperFirst(strings.TrimSuffix(w, ".")) + ".")
	}
	switch top["partner_wish"] {
	case "want", "very":
		b.WriteString(" " + pn + " уже отметил(а) «хочу».")
	case "skip":
		b.WriteString(" Правда, " + pn + " это пропускал(а).")
	}
	if len(events) > 1 {
		var more []string
		for _, e := range events[1:] {
			t := "«" + fmt.Sprint(e["title"]) + "»"
			if w, ok := e["when"].(string); ok && w != "" {
				t += " (" + w + ")"
			}
			more = append(more, t)
		}
		b.WriteString("\nЕщё: " + strings.Join(more, ", ") + ".")
	}
	b.WriteString("\nМожно уточнить: «подешевле», «а в воскресенье?», «без театра».")
	return b.String()
}

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
