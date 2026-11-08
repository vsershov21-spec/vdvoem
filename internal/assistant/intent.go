package assistant

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/ai"
	"vdvoem/internal/app"
)

// Intent — what a request means, as data. Understanding is the only thing a model does here: the server
// executes the intent and writes the answer from real data, so a small local model cannot invent prices,
// dates or places. Rules understand the clear requests instantly; the model takes the rest within a budget.
type Intent struct {
	I       string   `json:"i"`               // what to do (see intentNames)
	Date    string   `json:"date,omitempty"`  // YYYY-MM-DD
	Wknd    bool     `json:"wknd,omitempty"`  // the nearest weekend
	Part    string   `json:"part,omitempty"`  // morning | day | evening
	Max     int64    `json:"max,omitempty"`   // max price for two, ₽
	Cats    []string `json:"cats,omitempty"`  // food culture walk sport other
	Not     []string `json:"not,omitempty"`   // categories to exclude
	NotW    []string `json:"notw,omitempty"`  // words to exclude
	Moods   []string `json:"moods,omitempty"` // chill active romantic culture fun food outdoor home free new night
	Q       string   `json:"q,omitempty"`     // a name / thing asked about
	Hz      string   `json:"hz,omitempty"`    // week | later
	Text    string   `json:"text,omitempty"`  // for remember / goal / add_idea
	Mode    string   `json:"mode,omitempty"`  // suggest: best | random; suggest_mode: best | random | mix | off
	Rub     int64    `json:"rub,omitempty"`   // budget
	Scout   bool     `json:"scout,omitempty"` // only what the scout found
	conf    float64  // rules' confidence
	refined bool     // a follow-up («а подешевле?») merged into the previous request
}

var intentNames = []string{"find", "suggest", "price", "book", "remember", "goal", "overview", "past", "chores", "add_idea", "budget", "suggest_mode", "plan", "chat"}

var intentSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"i":     map[string]any{"type": "string", "enum": intentNames},
		"date":  map[string]any{"type": "string"},
		"wknd":  map[string]any{"type": "boolean"},
		"part":  map[string]any{"type": "string", "enum": []string{"morning", "day", "evening"}},
		"max":   map[string]any{"type": "integer"},
		"cats":  map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"food", "culture", "walk", "sport", "other"}}},
		"not":   map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"food", "culture", "walk", "sport", "other"}}},
		"notw":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"moods": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": app.Moods}},
		"q":     map[string]any{"type": "string"},
		"hz":    map[string]any{"type": "string", "enum": []string{"week", "later"}},
		"text":  map[string]any{"type": "string"},
		"mode":  map[string]any{"type": "string", "enum": []string{"best", "random", "mix", "off"}},
		"rub":   map[string]any{"type": "integer"},
		"scout": map[string]any{"type": "boolean"},
	},
	"required": []string{"i"},
}

// ---------- rules: instant, for clear requests ----------

var (
	priceAskRe  = regexp.MustCompile(`(?i)(?:сколько стоит|сколько стоят|сколько будет стоить|почём|почем|какая цена|цена на|цены на|стоимость|есть ли (?:ещё |еще )?билеты на|остались ли билеты на|дорого ли|дорого в|во сколько обойд[её]тся|сколько обойд[её]тся|билеты на)\s*(.*)`)
	bookRe      = regexp.MustCompile(`(?i)(?:забронируй|бронируй|запиши нас на|запиши в календарь|добавь в календарь|поставь в календарь|идём на|идем на|пойдём на|пойдем на|давай пойдём на|давай пойдем на|берём билеты на|берем билеты на|бер[её]м|позови (?:её|ее|его) на|пойти с (?:ней|ним) на)\s*(.*)`)
	budgetSetRe = regexp.MustCompile(`(?i)бюджет\D{0,12}(\d[\d\s]{2,6})`)
	followRe    = regexp.MustCompile(`(?i)^(?:(?:а|и|ну|тогда|лучше|только|ещё|еще)(?:\s|,|$)|подешевле|дешевле|подороже|без |не |кроме)`)
	timeQ       = regexp.MustCompile(`(?i)^(?:эт(?:ой|у) недел|выходн|сегодня|завтра|неделе)`)
)

func rulesIntent(text string, now time.Time) Intent {
	t := strings.ToLower(strings.TrimSpace(text))
	if mem := memoryFromText(text); mem != "" {
		return Intent{I: "remember", Text: mem, conf: 0.95}
	}
	if g := goalFromText(text); g != "" {
		return Intent{I: "goal", Text: g, conf: 0.9}
	}
	if m := bookRe.FindStringSubmatch(text); m != nil && strings.TrimSpace(m[1]) != "" && !timeQ.MatchString(cleanQ(m[1])) && !has(t, "куда", "?") {
		return Intent{I: "book", Q: bookQ(m[1]), conf: 0.88}
	}
	if has(t, "позови", "позвать") {
		if k := strings.Index(t, " на "); k >= 0 {
			return Intent{I: "book", Q: bookQ(text[k+len(" на "):]), conf: 0.85}
		}
	}
	if m := addRe.FindStringSubmatch(strings.TrimSpace(text)); m != nil {
		return Intent{I: "add_idea", Text: strings.TrimSpace(m[1]), conf: 0.95}
	}
	if m := priceAskRe.FindStringSubmatch(text); m != nil {
		return Intent{I: "price", Q: cleanQ(m[1]), conf: 0.9}
	}
	if m := budgetSetRe.FindStringSubmatch(t); m != nil && has(t, "постав", "сделай", "давай", "установ", "измени", "пусть") {
		n, _ := strconv.ParseInt(strings.ReplaceAll(m[1], " ", ""), 10, 64)
		return Intent{I: "budget", Rub: n, conf: 0.9}
	}
	if has(t, "присылай", "предлагай") && has(t, "случайн", "рандом", "по оценк", "лучш", "не присылай", "перестань", "выключи") {
		mode := "mix"
		switch {
		case has(t, "не присылай", "перестань", "выключи"):
			mode = "off"
		case has(t, "случайн", "рандом"):
			mode = "random"
		case has(t, "по оценк", "лучш"):
			mode = "best"
		}
		return Intent{I: "suggest_mode", Mode: mode, conf: 0.85}
	}
	if has(t, "удиви нас", "рандом", "случайн", "наугад") || t == "удиви" || t == "удиви!" {
		return Intent{I: "suggest", Mode: "random", conf: 0.9}
	}
	if has(t, "что запланир", "что у нас запланир", "какие планы", "ближайшая встреча", "когда встреча", "что у нас на ", "наши планы", "что дальше по плану", "следующая встреча", "следующее свидание", "мы куда-то идём", "мы куда-то идем", "мы куда-нибудь идём", "мы куда-нибудь идем") {
		return Intent{I: "overview", conf: 0.9}
	}
	if has(t, "где мы были", "куда мы ходили", "где были", "куда ходили", "прошлые встречи", "история встреч", "история наших", "наши встречи были") {
		return Intent{I: "past", conf: 0.9}
	}
	if has(t, "дела по дому", "дела дома", "домашние дела", "по хозяйству", "уборк", "какие у нас дела", "что по делам", "мусор", "посуд", "кто выносит", "кто моет", "кто убирает", "стирк") {
		return Intent{I: "chores", conf: 0.9}
	}
	if has(t, "давай выберем", "выбрать вместе", "давай запланируем", "запланируй встречу", "начни выбор", "выберем куда") {
		return Intent{I: "plan", conf: 0.85}
	}
	if t == "привет" || t == "hi" || has(t, "спасибо", "благодар", "супер", "класс!", "отлично") && len([]rune(t)) < 25 || has(t, "что ты умеешь", "помощь", "как пользоваться", "кто ты") {
		return Intent{I: "chat", conf: 0.9}
	}
	// what to do / where to go — by day, price, category, mood
	q := afishaFromText(t, now)
	moods := app.MoodsFrom(t)
	in := Intent{I: "find", Date: q.Date, Wknd: q.Weekend, Part: q.Part, Max: q.MaxRub, Cats: q.Include, Not: q.Exclude, NotW: q.NotWords,
		Moods: moods, Q: q.Query, Hz: q.Horizon, Scout: !q.WholeBank}
	signals := 0
	for _, b := range []bool{q.Date != "", q.Weekend, q.Part != "", q.MaxRub > 0, len(q.Include) > 0, len(q.Exclude) > 0, len(moods) > 0, q.Horizon == "later", !q.WholeBank, q.Query != ""} {
		if b {
			signals++
		}
	}
	asks := has(t, "куда", "что ", "что-", "чем ", "посовет", "предлож", "подбер", "найди", "покажи", "есть ли", "хочу", "хотим", "давай", "можно", "есть что", "?")
	words := len(strings.Fields(t))
	switch {
	case (signals >= 2 || signals == 1 && asks) && words <= 7, signals >= 3:
		in.conf = 0.85
	case signals >= 1 && asks: // a longer request may hold what the rules don't see — let the model read it
		in.conf = 0.7
	case signals == 1:
		in.conf = 0.6
	case asks:
		in.I, in.Mode, in.conf = "suggest", "best", 0.5
	default:
		in.I, in.conf = "chat", 0.2
	}
	return in
}

// bookQ: «Гамлета, поставь в календарь» → «Гамлета»
func bookQ(s string) string {
	for _, sep := range []string{",", " и ", " в пятниц", " в суббот", " в воскрес", " сегодня", " завтра", " позови", " поставь"} {
		if k := strings.Index(strings.ToLower(s), sep); k > 0 {
			s = s[:k]
		}
	}
	return cleanQ(s)
}

func cleanQ(s string) string {
	s = strings.TrimSpace(strings.Trim(s, "?!. "))
	for _, p := range []string{"этот ", "эту ", "это ", "этого ", "на ", "в ", "во "} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

// merge a follow-up («а подешевле?», «а без театра», «только в воскресенье») into the previous search
func mergeFollowUp(prev, cur Intent, text string) Intent {
	if prev.I != "find" || (cur.I != "find" && cur.I != "suggest" && cur.I != "chat") || !followRe.MatchString(strings.TrimSpace(strings.ToLower(text))) {
		return cur
	}
	out := prev
	out.conf, out.refined = 0.9, true
	if cur.Date != "" || cur.Wknd {
		out.Date, out.Wknd = cur.Date, cur.Wknd
	}
	if cur.Part != "" {
		out.Part = cur.Part
	}
	if cur.Max > 0 {
		out.Max = cur.Max
	} else if has(strings.ToLower(text), "подешевле", "дешевле") {
		if out.Max == 0 {
			out.Max = 3000
		} else {
			out.Max = out.Max * 2 / 3
		}
	}
	if len(cur.Cats) > 0 {
		out.Cats = cur.Cats
	}
	out.Not, out.NotW = append(out.Not, cur.Not...), append(out.NotW, cur.NotW...)
	if len(cur.Moods) > 0 {
		out.Moods = cur.Moods
	}
	if cur.Hz != "" {
		out.Hz = cur.Hz
	}
	return out
}

// ---------- the model: only for what the rules are not sure about ----------

// LLMBudget: how long a request may wait for the model; after that the rules' reading is used.
var LLMBudget = 6500 * time.Millisecond

func intentSystem(now time.Time) string {
	return `Ты разбираешь запросы пары к приложению «Вдвоём» и отвечаешь ТОЛЬКО JSON по схеме. Сегодня ` + now.Format("2006-01-02") + ` (` + ruWeekday(now) + `).
i: find — что поделать/куда сходить/что есть (афиша скаута, копилка), с фильтрами; suggest — выбрать одну идею из копилки (mode random — «удиви»); price — сколько стоит что-то (q — что именно); book — записать конкретное событие в календарь/пойти на него (q); remember — человек сообщает свой вкус (text — фраза); goal — следить за чем-то в будущем (text); overview — наши планы; past — где были; chores — дела по дому; add_idea — добавить идею (text); budget — поставить бюджет (rub); suggest_mode — как бот присылает идеи (mode); plan — начать выбор вместе; chat — приветствие и прочее.
Фильтры find: date (YYYY-MM-DD), wknd (ближайшие выходные), part (morning/day/evening), max (₽ на двоих), cats/not (food culture walk sport other), notw (слова-исключения), moods (chill — отдохнуть, расслабиться; active; romantic; culture; fun; food; outdoor; home; free; new; night), hz (week — ближайшее, later — анонсы на месяцы вперёд), scout (только находки скаута/афиша).
Пиши только нужные поля. Ничего не придумывай.`
}

func ruWeekday(t time.Time) string {
	return []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}[t.Weekday()]
}

// few-shot: fixed, so the prefix stays cached by the local model between requests
var shots = []struct{ q, a string }{
	{"хотим просто почилить в субботу", `{"i":"find","date":"SAT","moods":["chill"]}`},
	{"что есть интересного на выходных до 4000?", `{"i":"find","wknd":true,"max":4000}`},
	{"куда сходить поесть вечером", `{"i":"find","cats":["food"],"part":"evening"}`},
	{"а без театра?", `{"i":"find","not":["culture"],"notw":["театр"]}`},
	{"что-нибудь активное, есть энергия", `{"i":"find","moods":["active"]}`},
	{"романтичное на годовщину", `{"i":"find","moods":["romantic"]}`},
	{"какие концерты будут через пару месяцев", `{"i":"find","hz":"later","q":"концерт"}`},
	{"что нашёл скаут?", `{"i":"find","scout":true,"hz":"week"}`},
	{"сколько стоят билеты на Гамлета", `{"i":"price","q":"Гамлет"}`},
	{"запиши нас на джаз в пятницу", `{"i":"book","q":"джаз"}`},
	{"я не ем морепродукты", `{"i":"remember","text":"не ем морепродукты"}`},
	{"дай знать когда будет фестиваль фейерверков", `{"i":"goal","text":"фестиваль фейерверков"}`},
	{"удиви нас", `{"i":"suggest","mode":"random"}`},
	{"что у нас на этой неделе по планам", `{"i":"overview"}`},
	{"добавь бранч в Пушкине", `{"i":"add_idea","text":"бранч в Пушкине"}`},
	{"нам лень выходить из дома", `{"i":"find","moods":["home","chill"]}`},
}

func shotMsgs(now time.Time) []ai.Message {
	sat := now
	for sat.Weekday() != time.Saturday {
		sat = sat.AddDate(0, 0, 1)
	}
	msgs := []ai.Message{{Role: "system", Content: intentSystem(now)}}
	for _, s := range shots {
		msgs = append(msgs, ai.Message{Role: "user", Content: s.q}, ai.Message{Role: "assistant", Content: strings.ReplaceAll(s.a, "SAT", sat.Format("2006-01-02"))})
	}
	return msgs
}

func modelIntent(ctx context.Context, c *ai.Client, text string, now time.Time, like []intentCase) (Intent, error) {
	ctx, cancel := context.WithTimeout(ctx, LLMBudget)
	defer cancel()
	msgs := shotMsgs(now)
	for _, l := range like {
		a := l.Want
		for _, code := range []string{"TOD", "TOM", "SAT", "SUN", "FRI"} {
			a = strings.ReplaceAll(a, `"`+code+`"`, `"`+caseDate(code, now)+`"`)
		}
		msgs = append(msgs, ai.Message{Role: "user", Content: l.Q}, ai.Message{Role: "assistant", Content: a})
	}
	raw, err := c.Structured(ctx, "intent", append(msgs, ai.Message{Role: "user", Content: text}), intentSchema, 120)
	if err != nil {
		return Intent{}, err
	}
	var in Intent
	if err := json.Unmarshal(raw, &in); err != nil || in.I == "" {
		return Intent{}, err
	}
	return sanitize(in), nil
}

// sanitize: the model's output is data from outside — keep only known values.
func sanitize(in Intent) Intent {
	ok := map[string]bool{}
	for _, n := range intentNames {
		ok[n] = true
	}
	if !ok[in.I] {
		in.I = "chat"
	}
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		in.Date = ""
	}
	if in.Part != "morning" && in.Part != "day" && in.Part != "evening" {
		in.Part = ""
	}
	if in.Max < 0 || in.Max > 1_000_000 {
		in.Max = 0
	}
	cat := func(l []string) []string {
		var out []string
		for _, x := range l {
			if x == "food" || x == "culture" || x == "walk" || x == "sport" || x == "other" {
				out = append(out, x)
			}
		}
		return out
	}
	in.Cats, in.Not = cat(in.Cats), cat(in.Not)
	var moods []string
	for _, m := range in.Moods {
		for _, k := range app.Moods {
			if m == k {
				moods = append(moods, m)
			}
		}
	}
	in.Moods = moods
	if in.Hz != "week" && in.Hz != "later" {
		in.Hz = ""
	}
	in.Q, in.Text = clipS(in.Q, 80), clipS(in.Text, 200)
	return in
}

func clipS(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

// Understand: rules first; the model only when they are unsure and it answers within the budget.
func (as *Assistant) Understand(ctx context.Context, text string, prev Intent, now time.Time) (Intent, string) {
	rule := mergeFollowUp(prev, rulesIntent(text, now), text)
	if rule.conf >= 0.8 || !as.AI.Enabled() {
		return rule, "rules"
	}
	m, err := modelIntent(ctx, as.AI, text, now, recall(as.bank(ctx), text, 3))
	if err != nil {
		return rule, "rules-fallback"
	}
	m = mergeFollowUp(prev, m, text)
	return m, "model"
}

// Warm keeps the local model loaded with its prompt prefix cached (called every few minutes).
func (as *Assistant) Warm(ctx context.Context) {
	if !as.AI.IsLocal() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, _ = as.AI.Structured(ctx, "warm", append(shotMsgs(as.now()), ai.Message{Role: "user", Content: "привет"}), intentSchema, 8)
}
