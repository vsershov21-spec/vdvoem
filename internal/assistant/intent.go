package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/ai"
	"vdvoem/internal/app"
	"vdvoem/internal/leisure"
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
	All     bool     `json:"all,omitempty"`   // «вообще всё, что есть»: the whole list, not the best few
	Whose   string   `json:"whose,omitempty"` // ours — only the couple's own ideas; new — only what they have not seen
	G       []string `json:"g,omitempty"`     // kinds of leisure asked for: «джаз», «иммерсивный», «керамика» (internal/leisure)
	From    string   `json:"from,omitempty"`  // a period: «в конце ноября» → from…to (YYYY-MM-DD)
	To      string   `json:"to,omitempty"`
	conf    float64  // rules' confidence
	refined bool     // a follow-up («а подешевле?») merged into the previous request
}

var intentNames = []string{"find", "suggest", "price", "book", "about", "remember", "goal", "overview", "past", "chores", "add_idea", "budget", "suggest_mode", "plan", "chat"}

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
		"g":     map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": leisure.Tags()}},
		"all":   map[string]any{"type": "boolean"},
		"whose": map[string]any{"type": "string", "enum": []string{"ours", "new"}},
		"from":  map[string]any{"type": "string"},
		"to":    map[string]any{"type": "string"},
	},
	"required": []string{"i"},
}

// ---------- rules: instant, for clear requests ----------

var (
	priceAskRe  = regexp.MustCompile(`(?i)(?:сколько стоит|сколько стоят|сколько будет стоить|почём|почем|какая цена|цена на|цены на|стоимость|есть ли (?:ещё |еще )?билеты на|остались ли билеты на|дорого ли|дорого в|во сколько обойд[её]тся|сколько обойд[её]тся|билеты на)\s*(.*)`)
	aboutRe     = regexp.MustCompile(`(?i)(?:расскажи (?:про|о|об|поподробнее (?:про|о))|что за спектакль|что за концерт|что за событие|что это за|что такое|о ч[её]м (?:спектакль|постановка|концерт)?|кто играет в|кто играет|кто в составе|какой жанр у|подойд[её]т ли нам|нам подойд[её]т|стоит ли идти на|стоит ли сходить на|понравится ли нам|понравится ли (?:насте|ей|ему))\s*(.*)`)
	bookRe      = regexp.MustCompile(`(?i)(?:забронируй|бронируй|запиши нас на|запиши в календарь|добавь в календарь|поставь в календарь|идём на|идем на|пойдём на|пойдем на|давай пойдём на|давай пойдем на|берём билеты на|берем билеты на|бер[её]м|позови (?:её|ее|его) на|пойти с (?:ней|ним) на)\s*(.*)`)
	budgetSetRe = regexp.MustCompile(`(?i)бюджет\D{0,12}(\d[\d\s]{2,6})`)
	followRe    = regexp.MustCompile(`(?i)^(?:(?:а|и|ну|тогда|лучше|только|ещё|еще)(?:\s|,|$)|подешевле|дешевле|подороже|без |не |кроме|покажи вс|показать вс|все варианты)`)
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
	if m := aboutRe.FindStringSubmatch(text); m != nil && len([]rune(cleanQ(m[1]))) >= 3 && !timeQ.MatchString(cleanQ(m[1])) {
		return Intent{I: "about", Q: bookQ(m[1]), conf: 0.86}
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
	if has(t, "удиви нас", "удиви ещё", "удиви еще", "рандом", "случайн", "наугад") || t == "удиви" || t == "удиви!" {
		return Intent{I: "suggest", Mode: "random", conf: 0.9}
	}
	if has(t, "что запланир", "что у нас запланир", "какие планы", "ближайшая встреча", "когда встреча", "что у нас на ", "наши планы", "что дальше по плану", "следующая встреча", "следующее свидание", "мы куда-то идём", "мы куда-то идем", "мы куда-нибудь идём", "мы куда-нибудь идем") {
		return Intent{I: "overview", conf: 0.9}
	}
	if has(t, "где мы были", "куда мы ходили", "где были", "куда ходили", "прошлые встречи", "история встреч", "история наших", "наши встречи были") {
		return Intent{I: "past", conf: 0.9}
	}
	if has(t, "дела по дому", "дела дома", "домашние дела", "по хозяйству", "уборк", "какие у нас дела", "что по делам", "мусор", "посуд", "кто выносит", "кто моет", "кто убирает", "стирк", "готовит", "по хозяйству", "дежурит") {
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
	for _, b := range []bool{q.Date != "", q.Weekend, q.Part != "", q.MaxRub > 0, len(q.Include) > 0, len(q.Exclude) > 0, len(moods) > 0, q.Horizon == "later" || q.Horizon == "week" && q.Date == "" && !q.Weekend, !q.WholeBank, q.Query != ""} {
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
	case asks && has(t, "куда", "посовет", "предлож", "подбер", "идею", "идея", "чем заняться", "что делать"):
		in.I, in.Mode, in.conf = "suggest", "best", 0.5
	case asks && aboutTopicRe.MatchString(t): // «что-нибудь про технологии», «есть что-то с котиками?»
		in.Q = stemQ(strings.TrimSpace(aboutTopicRe.FindStringSubmatch(t)[1]))
		in.conf = 0.6
	case asks:
		in.I, in.conf = "chat", 0.3 // «как дела?» — talk, not a pick
	default:
		in.I, in.conf = "chat", 0.2
	}
	return in
}

var aboutTopicRe = regexp.MustCompile(`(?:что-нибудь|что-то|что|чего-нибудь|есть|сходить)\s+(?:интересное\s+)?(?:про|о|об|по|связанное с|с|со)\s+([а-яёa-z][а-яёa-z -]{2,30}?)[?.!]*$`)

// bookQ: «Гамлета, поставь в календарь» → «Гамлета»
func bookQ(s string) string {
	for _, sep := range []string{" — ", " - ", "?", " подойд", " нам ", " понрав", ",", " и ", " в пятниц", " в суббот", " в воскрес", " сегодня", " завтра", " позови", " поставь"} {
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
	if cur.From != "" {
		out.From, out.To = cur.From, cur.To
	}
	if showAllRe.MatchString(strings.ToLower(text)) {
		out.All = true
	}
	return out
}

var showAllRe = regexp.MustCompile(`все варианты|покажи вс|показать вс|весь список|полностью|списком`)

// ---------- the model: only for what the rules are not sure about ----------

// LLMBudget: how long a request may wait for the model; after that the rules' reading is used.
var LLMBudget = 6500 * time.Millisecond

func intentSystem(now time.Time) string {
	return `Ты разбираешь запросы пары к приложению «Вдвоём» и отвечаешь ТОЛЬКО JSON по схеме. Сегодня ` + now.Format("2006-01-02") + ` (` + ruWeekday(now) + `).
i: find — что поделать/куда сходить/что есть (афиша скаута, копилка), с фильтрами; suggest — выбрать одну идею из копилки (mode random — «удиви»); price — сколько стоит что-то (q — что именно); book — записать конкретное событие в календарь/пойти на него (q); about — рассказать о конкретном событии: что это, жанр, кто играет, подойдёт ли нам (q); remember — человек сообщает свой вкус (text — фраза); goal — следить за чем-то в будущем (text); overview — наши планы; past — где были; chores — дела по дому; add_idea — добавить идею (text); budget — поставить бюджет (rub); suggest_mode — как бот присылает идеи (mode); plan — начать выбор вместе; chat — приветствие и прочее.
Фильтры find: date (YYYY-MM-DD), wknd (ближайшие выходные), part (morning/day/evening), max (₽ на двоих), cats/not (food culture walk sport other), notw (слова-исключения), moods (chill — отдохнуть, расслабиться; active; romantic; culture; fun; food; outdoor; home; free; new; night), hz (week — ближайшее, later — анонсы на месяцы вперёд), scout (только находки скаута/афиша), g (вид досуга из словаря: джаз, иммерсивный, керамика, квест, баня…), from/to (период: «в конце ноября», «на следующей неделе» — YYYY-MM-DD), all (вообще всё списком), whose (ours — только наши идеи, new — только то, чего мы не видели).
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
	{"что за спектакль Чайка, нам подойдёт?", `{"i":"about","q":"Чайка"}`},
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

func modelIntent(ctx context.Context, c *ai.Client, text string, now time.Time, like []intentCase, pt ...Turn) (Intent, error) {
	ctx, cancel := context.WithTimeout(ctx, LLMBudget)
	defer cancel()
	msgs := shotMsgs(now)
	if Tuned(c.M()) { // trained on exactly this: the system prompt and the request
		msgs = []ai.Message{{Role: "system", Content: intentSystem(now)}}
		like = nil
	}
	for _, l := range like {
		a := l.Want
		for _, code := range []string{"TOD", "TOM", "SAT", "SUN", "FRI"} {
			a = strings.ReplaceAll(a, `"`+code+`"`, `"`+caseDate(code, now)+`"`)
		}
		msgs = append(msgs, ai.Message{Role: "user", Content: l.Q}, ai.Message{Role: "assistant", Content: a})
	}
	if len(pt) > 0 && pt[0].Text != "" && pt[0].In.I != "" { // the previous exchange: a follow-up is read in its light
		b, _ := json.Marshal(pt[0].In)
		msgs = append(msgs, ai.Message{Role: "user", Content: pt[0].Text}, ai.Message{Role: "assistant", Content: string(b)})
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
	var g []string
	for _, x := range in.G {
		if leisure.Known(x) && len(g) < 4 {
			g = append(g, x)
		}
	}
	in.G = g
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
func (as *Assistant) Understand(ctx context.Context, text string, pt Turn, now time.Time) (Intent, string) {
	in, src, _ := as.understand(ctx, text, pt, now)
	return in, src
}

// understand also returns the other plausible reading (alt.I == "" — none): when the model and fairly sure rules
// disagree, the answer follows one and offers the other as a button, so a wrong guess costs one tap, not a retype.
func (as *Assistant) understand(ctx context.Context, text string, pt Turn, now time.Time) (Intent, string, Intent) {
	in, src, alt := as.understand1(ctx, text, pt, now)
	in = widen(in, text)
	in = withKinds(in, text)
	return withPeriod(in, text, now), src, alt
}

var (
	allRe    = regexp.MustCompile(`вообще вс|вс[её] что (есть|нашл|нарыл|наш[её]л)|вс[её] подряд|полн(ый|ую|ая) (список|подборк|афиш)|подборк|всю афишу|все события|все находки|всё,? что есть|покажи вс[её]|список всего|что вообще (есть|идёт|идет)`)
	scoutRe  = regexp.MustCompile(`скаут|агент|нарыл|нашёл|нашел|находк|в афише|афиш`)
	newRe    = regexp.MustCompile(`не видели|не знаем|не слышали|только нов|новые находки|новенькое|свежее|чего нет в копилке|чего у нас нет`)
	oursRe   = regexp.MustCompile(`только наш|наши идеи|наше|из копилки|в копилке|в нашем банке|в банке|что мы (сами )?добавл|мы хотели`)
	randomRe = regexp.MustCompile(`удиви|рандом|случайн|наугад|наобум`)
	weekRe   = regexp.MustCompile(`недел|выходн|сегодня|завтра|суббот|воскрес|пятниц|ближайш`)
)

// withKinds: a kind of leisure named in words («на джаз», «в баню», «мастер-класс по керамике») is searched for,
// whatever read the request; the model may add kinds it inferred («с саксофоном» → джаз)
func withKinds(in Intent, text string) Intent {
	g := leisure.Detect(text)
	if len(g) == 0 {
		return in
	}
	if in.I == "chat" || in.I == "chores" || in.I == "suggest" && in.Mode != "random" { // «готовить вместе пасту» is a date, not a chore
		in = Intent{I: "find", Date: in.Date, Wknd: in.Wknd, Part: in.Part, Max: in.Max, Moods: in.Moods, Not: in.Not, NotW: in.NotW, From: in.From, To: in.To}
	}
	if in.I != "find" && !(in.I == "suggest" && in.Mode == "random") && in.I != "goal" {
		return in
	}
	if in.I == "goal" {
		return in
	}
	seen := map[string]bool{}
	for _, x := range in.G {
		seen[x] = true
	}
	for _, x := range g {
		if !seen[x] && len(in.G) < 4 {
			in.G = append(in.G, x)
		}
	}
	if len(in.Cats) == 0 {
		cs := map[string]bool{}
		for _, x := range in.G {
			if c := leisure.Cat(x); c != "" && !cs[c] {
				cs[c] = true
				in.Cats = append(in.Cats, c)
			}
		}
	}
	// the kind says it better than a substring would
	if in.Q != "" {
		for _, x := range in.G {
			if leisure.Fits(x, in.Q) {
				in.Q = ""
				break
			}
		}
	}
	return in
}

// withPeriod: days named in words always count, whatever read the request; «куда сходить» without a pick
// mode is a search over everything, not one idea from the bank
func withPeriod(in Intent, text string, now time.Time) Intent {
	if in.I == "suggest" && in.Mode != "random" && in.Whose != "ours" {
		in = Intent{I: "find", Date: in.Date, Wknd: in.Wknd, Part: in.Part, Max: in.Max, Cats: in.Cats, Not: in.Not, NotW: in.NotW, Moods: in.Moods, Scout: in.Scout, All: in.All, Whose: in.Whose, From: in.From, To: in.To}
	}
	if in.I != "find" && in.I != "suggest" && in.I != "chat" {
		return in
	}
	f, t := periodFrom(text, now)
	if f == "" {
		return in
	}
	if in.I == "chat" {
		in = Intent{I: "find"}
	}
	in.From, in.To = f, t
	if in.Date != "" && (in.Date < f || in.Date > t) {
		in.Date = ""
	}
	in.Wknd, in.Hz = false, ""
	return in
}

// widen: «что нарыл агент» means everything the scout and the channels found, not only the next days;
// «подборку всего» means the whole list. Words decide this, whatever read the request.
func widen(in Intent, text string) Intent {
	t := strings.ToLower(text)
	all, scout := allRe.MatchString(t), scoutRe.MatchString(t)
	whose := ""
	switch {
	case newRe.MatchString(t):
		whose = "new"
	case oursRe.MatchString(t):
		whose = "ours"
	}
	if whose != "" && (in.I == "find" || in.I == "suggest" || in.I == "chat") {
		if in.I != "find" {
			in = Intent{I: "find", Date: in.Date, Wknd: in.Wknd, Part: in.Part, Max: in.Max, Cats: in.Cats, Not: in.Not, NotW: in.NotW, Moods: in.Moods}
		}
		in.Whose = whose
		if whose == "new" {
			in.Moods = remove(in.Moods, "new") // «новое для нас» is about the source, not a new experience
		}
		if in.Hz == "week" && !weekRe.MatchString(t) {
			in.Hz = ""
		}
	}
	if !all && !scout || in.I != "find" && in.I != "suggest" && in.I != "chat" {
		return in
	}
	if in.I == "suggest" && in.Mode == "random" || randomRe.MatchString(t) { // «удиви из того, что нарыл агент»
		return Intent{I: "suggest", Mode: "random", Scout: scout && !all, All: all, Date: in.Date, Part: in.Part, Max: in.Max, Cats: in.Cats}
	}
	if in.I != "find" {
		in = Intent{I: "find", Date: in.Date, Wknd: in.Wknd, Part: in.Part, Max: in.Max, Cats: in.Cats, Not: in.Not, NotW: in.NotW, Moods: in.Moods}
	}
	in.Scout = in.Scout || scout && !all && in.Whose != "ours"
	in.All = in.All || all
	if all && !showAllRe.MatchString(t) { // «подборку всего» starts afresh: only the conditions said now stay
		fresh := rulesIntent(text, time.Now())
		in.Date, in.Wknd, in.Part, in.Moods, in.Cats, in.Max = fresh.Date, fresh.Wknd, fresh.Part, fresh.Moods, fresh.Cats, fresh.Max
		in.Q, in.Not, in.NotW = "", nil, nil
	}
	if in.Hz == "week" && !weekRe.MatchString(t) { // no time named: announcements months ahead count too
		in.Hz = ""
	}
	return in
}

func (as *Assistant) understand1(ctx context.Context, text string, pt Turn, now time.Time) (Intent, string, Intent) {
	prev := pt.In
	if in, ok := refersTo(text, pt.Titles); ok { // «расскажи про второе», «сколько стоит первое», «давай туда»
		return in, "rules", Intent{}
	}
	rule := mergeFollowUp(prev, rulesIntent(text, now), text)
	tuned := as.AI.Enabled() && Tuned(as.AI.M())
	// a general small model is weaker than confident rules; a fine-tuned one is stronger (eval: 84% vs 56% on
	// unseen requests), so then the rules only answer greetings and pure «удиви» and fill the details
	if !as.AI.Enabled() || rule.conf >= 0.8 && (!tuned || trivial(rule, text)) {
		return rule, "rules", Intent{}
	}
	m, err := modelIntent(ctx, as.IntentModel(), text, now, recall(as.bank(ctx), text, 3), pt)
	if err != nil {
		return rule, "rules-fallback", altFor(rule, rule, text)
	}
	if tuned {
		m = reconcileTuned(m, rule, text)
	} else {
		m = reconcile(m, rule, text)
	}
	m = mergeFollowUp(prev, m, text)
	return m, "model", altFor(m, rule, text)
}

// altFor: the reading worth offering next to the chosen one
func altFor(chosen, rule Intent, text string) Intent {
	if chosen.I == "chat" && isGreeting(text) {
		return Intent{}
	}
	if rule.I != chosen.I && rule.I != "chat" && rule.conf >= 0.5 && describe(rule) != "" {
		return rule
	}
	// a request answered with small talk, or small talk the rules could not place: offer a search with what
	// the request did say (day, part, budget, mood)
	if chosen.I == "chat" && rule.conf < 0.5 && len(strings.Fields(text)) >= 3 {
		f := rule
		f.I, f.conf = "find", 0
		if f.Date != "" || f.Wknd || f.Part != "" || f.Max > 0 || len(f.Moods) > 0 || len(f.Cats) > 0 {
			return f
		}
	}
	return Intent{}
}

// describe: a button label for a reading («Найти, куда сходить в субботу вечером»); "" — not offerable
func describe(in Intent) string {
	q := func(s string) string {
		if s == "" {
			return ""
		}
		return " «" + s + "»"
	}
	switch in.I {
	case "find", "suggest":
		var sb strings.Builder
		if in.I == "suggest" && in.Mode == "random" {
			return "Удивить случайной идеей"
		}
		sb.WriteString("Подобрать, куда сходить")
		switch {
		case in.Date != "":
			if t, err := time.Parse("2006-01-02", in.Date); err == nil {
				if t.Weekday() == time.Tuesday {
					sb.WriteString(" во вторник")
				} else {
					sb.WriteString(" в " + ruWeekdayAcc(t))
				}
			}
		case in.Wknd:
			sb.WriteString(" на выходных")
		case in.Hz == "week":
			sb.WriteString(" на неделе")
		}
		switch in.Part {
		case "morning":
			sb.WriteString(" утром")
		case "day":
			sb.WriteString(" днём")
		case "evening":
			sb.WriteString(" вечером")
		}
		if in.Max > 0 {
			sb.WriteString(fmt.Sprintf(" до %d ₽", in.Max))
		}
		return sb.String()
	case "price":
		if in.Q == "" {
			return ""
		}
		return "Узнать цену" + q(in.Q)
	case "book":
		if in.Q == "" {
			return ""
		}
		return "Записать нас на" + q(in.Q)
	case "about":
		if in.Q == "" {
			return ""
		}
		return "Рассказать про" + q(in.Q)
	case "goal":
		return "Следить за" + q(firstNonEmpty(in.Q, in.Text))
	case "remember":
		return "Запомнить это"
	case "add_idea":
		return "Добавить в копилку"
	case "overview":
		return "Показать, что запланировано"
	case "past":
		return "Показать, где мы были"
	case "chores":
		return "Показать дела по дому"
	case "plan":
		return "Позвать вместе выбрать время"
	case "budget":
		if in.Rub <= 0 {
			return ""
		}
		return fmt.Sprintf("Поставить бюджет %d ₽", in.Rub)
	}
	return ""
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

var (
	ordinals = []struct {
		re *regexp.Regexp
		k  int
	}{{regexp.MustCompile(`перв`), 0}, {regexp.MustCompile(`втор`), 1}, {regexp.MustCompile(`трет`), 2}, {regexp.MustCompile(`четв[её]рт`), 3}, {regexp.MustCompile(`последн`), -1}}
	namedRe   = regexp.MustCompile(`\s[«"A-ZА-ЯЁ]`)
	pronounRe = regexp.MustCompile(`(?:^|\s)(?:это|этот|эту|его|её|ее|туда|на него|на неё|про него|про неё|про это)(?:\s|$|[?!.,])`)
)

// refersTo: a short request about something just shown («расскажи про второе», «почём первое», «давай туда»)
func refersTo(text string, titles []string) (Intent, bool) {
	if len(titles) == 0 {
		return Intent{}, false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	if len(strings.Fields(t)) > 7 {
		return Intent{}, false
	}
	k := -2
	for _, o := range ordinals {
		if o.re.MatchString(t) {
			k = o.k
			break
		}
	}
	if k == -2 && pronounRe.MatchString(" "+t+" ") && len(strings.Fields(t)) <= 4 && !namedRe.MatchString(text) {
		k = 0 // «давай туда», «почём это» — but not «что это за Чайка»: that names its own thing
	}
	if k == -1 {
		k = len(titles) - 1
	}
	if k < 0 || k >= len(titles) {
		return Intent{}, false
	}
	q := titles[k]
	switch {
	case has(t, "сколько", "почём", "почем", "цена", "стоит", "дорог", "дорож", "дешевл"):
		return Intent{I: "price", Q: q, conf: 0.95}, true
	case has(t, "запиш", "идём", "идем", "пойд", "бронир", "календар", "давай", "берём", "берем", "позови", "хочу на", "хотим на"):
		return Intent{I: "book", Q: q, conf: 0.95}, true
	case has(t, "добав", "сохрани", "в копилку"):
		return Intent{I: "add_idea", Text: q, conf: 0.95}, true
	}
	return Intent{I: "about", Q: q, conf: 0.95}, true
}

// trivial: nothing for a model to add
func trivial(rule Intent, text string) bool {
	return rule.I == "chat" && isGreeting(text) || rule.I == "suggest" && rule.Mode == "random" && len(strings.Fields(text)) <= 3
}

// Warm keeps the local model loaded with its prompt prefix cached (called every few minutes).
func (as *Assistant) Warm(ctx context.Context) {
	if as.EmbedModel != "" && as.AI.Enabled() { // the embedding model stays loaded too
		ectx, ecancel := context.WithTimeout(ctx, 30*time.Second)
		_, _ = as.AI.Embed(ectx, as.EmbedModel, []string{"привет"})
		ecancel()
	}
	c := as.IntentModel()
	if !c.IsLocal() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	msgs := shotMsgs(as.now())
	if Tuned(c.M()) {
		msgs = msgs[:1]
	}
	_, _ = c.Structured(ctx, "warm", append(msgs, ai.Message{Role: "user", Content: "привет"}), intentSchema, 8)
}

// ValidIntent: a label from outside (the labeling agent) is a known intent in the trained format
func ValidIntent(b []byte) bool {
	var in Intent
	if json.Unmarshal(b, &in) != nil {
		return false
	}
	for _, n := range intentNames {
		if n == in.I {
			return true
		}
	}
	return false
}

// LooksLikeRequest: free text in the bot that asks for something («хотим почилить в субботу») goes to the
// assistant; a bare name («Чайка во МХТ») still lands in the bank as an idea.
func LooksLikeRequest(text string, now time.Time) bool {
	in := rulesIntent(text, now)
	switch {
	case in.I == "chat" || in.I == "add_idea" || in.conf < 0.5:
		return false
	case in.I != "find" && in.I != "suggest":
		return in.conf >= 0.8 // «сколько стоит Чайка», «запиши нас на джаз», «следи за билетами»
	}
	return wantRe.MatchString(strings.ToLower(text)) // a category or a place alone is an idea for the bank
}

var wantRe = regexp.MustCompile(`(?:^|\s)(?:хоч|хотим|хотели|давай|пойд|сход|сгоня|можно|подбер|найд|найти|ищ|есть\s+ли|что-нибудь|что-то|чего-нибудь|куда|почил|отдохн|развле|погуля|скучно|чем\s+заня)`)

func remove(l []string, v string) []string {
	var out []string
	for _, x := range l {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
