package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"vdvoem/internal/ai"
	"vdvoem/internal/store"
)

// The intent eval: does a request get understood right, and how fast. It checks three readers —
// the rules alone, the model alone, and the pipeline the app really uses (rules → model when unsure).
// Dates in cases: TOD, TOM, SAT, SUN, FRI are replaced relative to the run's «today».

type intentCase struct {
	Q    string
	Want string // the expected intent as JSON: only the fields that matter
}

var intentCases = []intentCase{
	// find: moods
	{"хотим почилить", `{"i":"find","moods":["chill"]}`},
	{"давай просто отдохнём в субботу", `{"i":"find","date":"SAT","moods":["chill"]}`},
	{"устали, хочется расслабиться вечером", `{"i":"find","part":"evening","moods":["chill"]}`},
	{"что-нибудь спокойное на воскресенье", `{"i":"find","date":"SUN","moods":["chill"]}`},
	{"хочется движухи", `{"i":"find","moods":["active"]}`},
	{"куда-нибудь подвигаться, есть энергия", `{"i":"find","moods":["active"]}`},
	{"романтический вечер", `{"i":"find","moods":["romantic"]}`},
	{"что-то романтичное на годовщину", `{"i":"find","moods":["romantic"]}`},
	{"хочу культурно провести выходные", `{"i":"find","wknd":true,"moods":["culture"]}`},
	{"хочется поржать", `{"i":"find","moods":["fun"]}`},
	{"чем развлечься сегодня вечером", `{"i":"find","date":"TOD","part":"evening"}`},
	{"давай на свежий воздух", `{"i":"find","moods":["outdoor"]}`},
	{"не хотим выходить из дома", `{"i":"find","moods":["home"]}`},
	{"что можно бесплатно", `{"i":"find","moods":["free"]}`},
	{"хотим попробовать что-то новое", `{"i":"find","moods":["new"]}`},
	{"куда пойти ночью", `{"i":"find","moods":["night"]}`},
	{"чилл без денег на выходных", `{"i":"find","wknd":true,"moods":["chill","free"]}`},
	{"тихий вечер вдвоём, ничего шумного", `{"i":"find","part":"evening","moods":["chill"]}`},
	{"давай устроим что-то необычное", `{"i":"find","moods":["new"]}`},
	{"хочется вкусно поесть", `{"i":"find","cats":["food"]}`},
	// find: filters
	{"куда сходить в субботу вечером до 4000", `{"i":"find","date":"SAT","part":"evening","max":4000}`},
	{"что есть на выходных?", `{"i":"find","wknd":true}`},
	{"что интересного завтра?", `{"i":"find","date":"TOM"}`},
	{"куда сходить поесть", `{"i":"find","cats":["food"]}`},
	{"где поужинать в пятницу", `{"i":"find","date":"FRI","cats":["food"],"part":"evening"}`},
	{"какие спектакли идут на этой неделе", `{"i":"find","cats":["culture"]}`},
	{"концерты на выходных", `{"i":"find","wknd":true,"cats":["culture"]}`},
	{"выставки до 1500 рублей", `{"i":"find","cats":["culture"],"max":1500}`},
	{"куда сходить недорого", `{"i":"find","max":2000}`},
	{"что-нибудь без театра", `{"i":"find","not":["culture"]}`},
	{"только не музеи", `{"i":"find","not":["culture"]}`},
	{"погулять где-нибудь днём в воскресенье", `{"i":"find","date":"SUN","part":"day","cats":["walk"]}`},
	{"куда сходить утром", `{"i":"find","part":"morning"}`},
	{"спорт какой-нибудь на выходных", `{"i":"find","wknd":true,"cats":["sport"]}`},
	{"что посмотреть в кино", `{"i":"find","cats":["culture"]}`},
	{"есть ли стендап на этой неделе", `{"i":"find"}`},
	{"какие концерты будут весной", `{"i":"find","hz":"later"}`},
	{"что в анонсах на пару месяцев вперёд", `{"i":"find","hz":"later"}`},
	{"что нового нашёл скаут", `{"i":"find","scout":true}`},
	{"покажи афишу", `{"i":"find","scout":true}`},
	{"театр через месяц-два", `{"i":"find","hz":"later","cats":["culture"]}`},
	{"идеи на вечер вторника", `{"i":"find","part":"evening"}`},
	{"можно что-то до 3 тысяч на двоих", `{"i":"find","max":3000}`},
	{"нам бы что-нибудь в центре недорогое поесть", `{"i":"find","cats":["food"],"max":2000}`},
	{"куда пойти если дождь", `{"i":"find"}`},
	// suggest
	{"удиви нас", `{"i":"suggest","mode":"random"}`},
	{"выбери что-нибудь наугад", `{"i":"suggest","mode":"random"}`},
	{"рандомная идея", `{"i":"suggest","mode":"random"}`},
	{"посоветуй что-нибудь из копилки", `{"i":"suggest"}`},
	{"что нам больше всего понравится?", `{"i":"suggest"}`},
	{"давай случайное", `{"i":"suggest","mode":"random"}`},
	// price
	{"сколько стоит Щелкунчик?", `{"i":"price","q":"щелкунчик"}`},
	{"почём билеты на Гамлета", `{"i":"price","q":"гамлет"}`},
	{"какая цена на джазовый концерт", `{"i":"price","q":"джаз"}`},
	{"сколько будет стоить сходить в планетарий", `{"i":"price","q":"планетарий"}`},
	{"дорого ли в Белуге поужинать", `{"i":"price","q":"белуг"}`},
	{"а билеты на балет почём?", `{"i":"price","q":"балет"}`},
	{"стоимость выставки Айвазовского", `{"i":"price","q":"айвазовск"}`},
	{"узнай сколько стоит каток в Парке Горького", `{"i":"price","q":"каток"}`},
	{"есть ли ещё билеты на Лебединое озеро", `{"i":"price","q":"лебедин"}`},
	// book
	{"запиши нас на Щелкунчик", `{"i":"book","q":"щелкунчик"}`},
	{"идём на джаз в пятницу", `{"i":"book","q":"джаз"}`},
	{"давай пойдём на Гамлета, поставь в календарь", `{"i":"book","q":"гамлет"}`},
	{"добавь в календарь концерт Земфиры", `{"i":"book","q":"земфир"}`},
	{"бронируй стендап", `{"i":"book","q":"стендап"}`},
	{"берём билеты на балет", `{"i":"book","q":"балет"}`},
	{"хочу пойти с ней на выставку Айвазовского, позови её", `{"i":"book","q":"айвазовск"}`},
	// about
	{"расскажи про Чайку", `{"i":"about","q":"чайк"}`},
	{"кто играет в Мастере и Маргарите", `{"i":"about","q":"мастер"}`},
	{"подойдёт ли нам Мистер Икс?", `{"i":"about","q":"мистер"}`},
	{"о чём спектакль Пожары", `{"i":"about","q":"пожар"}`},
	{"стоит ли идти на Амадея", `{"i":"about","q":"амаде"}`},
	{"понравится ли Насте Кабала святош", `{"i":"about","q":"кабал"}`},
	// remember
	{"я не ем морепродукты", `{"i":"remember"}`},
	{"запомни: Настя не любит хоррор", `{"i":"remember"}`},
	{"мы не любим громкую музыку", `{"i":"remember"}`},
	{"обожаю грузинскую кухню", `{"i":"remember"}`},
	{"у меня аллергия на кошек, никаких котокафе", `{"i":"remember"}`},
	{"мне нравятся камерные концерты", `{"i":"remember"}`},
	{"терпеть не могу очереди", `{"i":"remember"}`},
	// goal
	{"следи за билетами на Земфиру", `{"i":"goal"}`},
	{"дай знать когда откроется каток", `{"i":"goal"}`},
	{"сообщи когда будет фестиваль фейерверков", `{"i":"goal"}`},
	{"найди нам концерт Би-2 на весну", `{"i":"goal"}`},
	{"хочу попасть на Щелкунчик в декабре, следи", `{"i":"goal"}`},
	// overview / past / chores / plan
	{"что у нас запланировано?", `{"i":"overview"}`},
	{"какие планы на неделю", `{"i":"overview"}`},
	{"когда наша следующая встреча", `{"i":"overview"}`},
	{"мы куда-то идём на этой неделе?", `{"i":"overview"}`},
	{"где мы были в последнее время", `{"i":"past"}`},
	{"куда мы ходили в прошлом месяце", `{"i":"past"}`},
	{"история наших встреч", `{"i":"past"}`},
	{"какие у нас дела по дому", `{"i":"chores"}`},
	{"что по уборке", `{"i":"chores"}`},
	{"кто выносит мусор на этой неделе", `{"i":"chores"}`},
	{"давай выберем куда пойти", `{"i":"plan"}`},
	{"начни выбор встречи", `{"i":"plan"}`},
	{"запланируй встречу", `{"i":"plan"}`},
	// add / budget / mode
	{"добавь каток в Парке Горького", `{"i":"add_idea"}`},
	{"сохрани: бранч в Пушкине", `{"i":"add_idea"}`},
	{"добавить идею — сходить в баню", `{"i":"add_idea"}`},
	{"поставь бюджет 5000", `{"i":"budget","rub":5000}`},
	{"давай бюджет 3000 на встречу", `{"i":"budget","rub":3000}`},
	{"присылай случайные идеи", `{"i":"suggest_mode","mode":"random"}`},
	{"не присылай больше идеи", `{"i":"suggest_mode","mode":"off"}`},
	{"предлагай лучшее по оценкам", `{"i":"suggest_mode","mode":"best"}`},
	// chat
	{"привет", `{"i":"chat"}`},
	{"что ты умеешь?", `{"i":"chat"}`},
	{"спасибо!", `{"i":"chat"}`},
}

// held out: written after the rules were tuned on intentCases — shows how they (and the model) generalise
var intentHoldout = []intentCase{
	{"мы вымотались, хочется чего-то лёгкого", `{"i":"find","moods":["chill"]}`},
	{"подкинь вариантов на субботний вечер", `{"i":"find","date":"SAT","part":"evening"}`},
	{"во сколько обойдётся ужин в Пушкине?", `{"i":"price","q":"пушкин"}`},
	{"хочу удивить Настю чем-то необычным", `{"i":"find","moods":["new"]}`},
	{"есть что-нибудь для прогулки на природе", `{"i":"find","moods":["outdoor"]}`},
	{"где бы нам потанцевать", `{"i":"find","moods":["active"]}`},
	{"сколько стоит вход в Третьяковку", `{"i":"price","q":"третьяков"}`},
	{"внеси концерт Шамана в наш календарь", `{"i":"book","q":"шаман"}`},
	{"не люблю караоке", `{"i":"remember"}`},
	{"Настя вегетарианка", `{"i":"remember"}`},
	{"держи в курсе про Новогодний бал", `{"i":"goal"}`},
	{"какие у нас встречи впереди", `{"i":"overview"}`},
	{"давай что-нибудь до двух тысяч", `{"i":"find","max":2000}`},
	{"тянет на что-нибудь весёлое в пятницу", `{"i":"find","date":"FRI","moods":["fun"]}`},
	{"хотим уютно посидеть где-нибудь с вином", `{"i":"find","moods":["chill"]}`},
	{"что-то для души, музыку послушать", `{"i":"find","cats":["culture"]}`},
	{"куда сводить её на свидание", `{"i":"find","moods":["romantic"]}`},
	{"кинь случайную идейку", `{"i":"suggest","mode":"random"}`},
	{"кто у нас сегодня готовит", `{"i":"chores"}`},
	{"где мы уже успели побывать", `{"i":"past"}`},
	{"мы бы погуляли по городу вечером", `{"i":"find","part":"evening","cats":["walk"]}`},
	{"есть варианты без ресторанов?", `{"i":"find","not":["food"]}`},
	{"давай запишемся на мастер-класс по керамике", `{"i":"book","q":"керамик"}`},
	{"занеси в копилку квиз в баре", `{"i":"add_idea"}`},
	{"бюджет на свидания 7000", `{"i":"budget","rub":7000}`},
	{"какие премьеры в театрах этой зимой", `{"i":"find","hz":"later","cats":["culture"]}`},
	{"хочется экшена", `{"i":"find","moods":["active"]}`},
	{"просто поваляться дома и что-то посмотреть", `{"i":"find","moods":["home"]}`},
	{"билеты на Мастера и Маргариту дорогие?", `{"i":"price","q":"мастер"}`},
	{"что вообще за постановка Вишнёвый сад и кто там занят", `{"i":"about","q":"вишн"}`},
	{"нам зайдёт Ревизор в Калягина?", `{"i":"about","q":"ревизор"}`},
	{"ну что, куда сегодня?", `{"i":"find","date":"TOD"}`},
}

// held out #2 — written after reconcile was tuned on the model's mistakes; nothing is tuned against these
var intentHoldout2 = []intentCase{
	{"давай сегодня без напряга, просто посидим где-нибудь", `{"i":"find","date":"TOD","moods":["chill"]}`},
	{"есть что-то интересное в воскресенье днём?", `{"i":"find","date":"SUN","part":"day"}`},
	{"хочу сводить Настю в оперу", `{"i":"find","cats":["culture"]}`},
	{"какой-нибудь концерт до 5 тысяч", `{"i":"find","cats":["culture"],"max":5000}`},
	{"во сколько начинается Мастер и Маргарита", `{"i":"about","q":"мастер"}`},
	{"сколько стоит поход в Ботанический сад", `{"i":"price","q":"ботанич"}`},
	{"поставь Вишнёвый сад нам в календарь", `{"i":"book","q":"вишн"}`},
	{"терпеть не могу оперу", `{"i":"remember"}`},
	{"Настя любит джаз", `{"i":"remember"}`},
	{"напиши, когда будут билеты на Щелкунчик", `{"i":"goal"}`},
	{"что мы делаем в эти выходные", `{"i":"overview"}`},
	{"куда мы ходили в октябре", `{"i":"past"}`},
	{"чья очередь мыть посуду", `{"i":"chores"}`},
	{"запиши идею: покататься на теплоходе", `{"i":"add_idea"}`},
	{"давай лимит 6000 на свидание", `{"i":"budget","rub":6000}`},
	{"не надо присылать мне идеи", `{"i":"suggest_mode","mode":"off"}`},
	{"давай вместе выберем, куда пойти в выходные", `{"i":"plan"}`},
	{"придумай что-нибудь неожиданное", `{"i":"suggest","mode":"random"}`},
	{"нужно что-то активное после работы", `{"i":"find","moods":["active"]}`},
	{"хочется романтики в пятницу вечером", `{"i":"find","date":"FRI","part":"evening","moods":["romantic"]}`},
	{"можно что-нибудь на свежем воздухе бесплатно", `{"i":"find","moods":["outdoor"]}`},
	{"где-нибудь вкусно позавтракать в субботу", `{"i":"find","date":"SAT","cats":["food"]}`},
	{"что идёт в театрах на новогодние праздники", `{"i":"find","cats":["culture"],"hz":"later"}`},
	{"а без мюзиклов?", `{"i":"find"}`},
	{"какие выставки сейчас идут", `{"i":"find","cats":["culture"]}`},
	{"про что спектакль Чайка?", `{"i":"about","q":"чайк"}`},
	{"какой состав в Пожарах", `{"i":"about","q":"пожар"}`},
	{"это нам вообще подойдёт — Мистер Икс?", `{"i":"about","q":"мистер"}`},
	{"привет, что нового?", `{"i":"chat"}`},
	{"спасибо, ты лучший", `{"i":"chat"}`},
}

func caseDate(code string, now time.Time) string {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	next := func(w time.Weekday) time.Time { return d.AddDate(0, 0, (int(w)-int(d.Weekday())+7)%7) }
	switch code {
	case "TOD":
		return d.Format("2006-01-02")
	case "TOM":
		return d.AddDate(0, 0, 1).Format("2006-01-02")
	case "SAT":
		return next(time.Saturday).Format("2006-01-02")
	case "SUN":
		return next(time.Sunday).Format("2006-01-02")
	case "FRI":
		return next(time.Friday).Format("2006-01-02")
	}
	return code
}

// matches: the intent is the same and every field the case names is right (the rest may be anything)
func matches(got Intent, want Intent, now time.Time) bool {
	if got.I != want.I {
		return false
	}
	if want.Date != "" && got.Date != caseDate(want.Date, now) && !(got.Wknd && (want.Date == "SAT" || want.Date == "SUN")) {
		return false
	}
	if want.Wknd && !got.Wknd && got.Date != caseDate("SAT", now) && got.Date != caseDate("SUN", now) {
		return false
	}
	if want.Part != "" && got.Part != want.Part {
		return false
	}
	if want.Max > 0 && (got.Max == 0 || got.Max > want.Max*3/2) {
		return false
	}
	if want.Rub > 0 && got.Rub != want.Rub {
		return false
	}
	if want.Mode != "" && got.Mode != want.Mode {
		return false
	}
	if want.Hz != "" && got.Hz != want.Hz {
		return false
	}
	if want.Scout && !got.Scout {
		return false
	}
	sub := func(need, have []string) bool {
		for _, n := range need {
			if !contains(have, n) {
				return false
			}
		}
		return true
	}
	if !sub(want.Cats, got.Cats) || !sub(want.Not, got.Not) {
		return false
	}
	if len(want.Moods) > 0 && !sub(want.Moods[:1], got.Moods) { // the main mood must be caught
		return false
	}
	if want.Q != "" && !strings.Contains(strings.ToLower(got.Q+" "+got.Text), want.Q) {
		return false
	}
	return true
}

type intentScore struct {
	ok  int
	ms  []int64
	bad []string
}

func (s *intentScore) add(ok bool, el time.Duration, q string, got Intent) {
	s.ms = append(s.ms, el.Milliseconds())
	if ok {
		s.ok++
		return
	}
	b, _ := json.Marshal(got)
	s.bad = append(s.bad, q+" → "+string(b))
}

func (s *intentScore) line(name string, n int) string {
	if len(s.ms) == 0 {
		return name + ": —"
	}
	ms := append([]int64(nil), s.ms...)
	sort.Slice(ms, func(a, b int) bool { return ms[a] < ms[b] })
	var sum int64
	for _, m := range ms {
		sum += m
	}
	return fmt.Sprintf("%-10s верно %3d из %d (%3.0f%%) · среднее %.2f с · p90 %.2f с · худшее %.2f с", name, s.ok, n, float64(s.ok)*100/float64(n),
		float64(sum)/float64(len(ms))/1000, float64(ms[len(ms)*9/10])/1000, float64(ms[len(ms)-1])/1000)
}

// EvalIntents: the report for one model. Returns the pipeline's share of right answers.
func EvalIntents(ctx context.Context, c *ai.Client, w io.Writer) float64 {
	as := &Assistant{AI: c}
	if Tuned(c.Model) {
		as.SetIntentModel(c.Model)
	}
	now := time.Now().In(time.FixedZone("MSK", 3*3600))
	var rules, model, pipe intentScore
	as.Warm(ctx)
	var hRules, hModel, hPipe intentScore
	var h2Rules, h2Model, h2Pipe intentScore
	all := append(append(append([]intentCase{}, intentCases...), intentHoldout...), intentHoldout2...)
	for k, tc := range all {
		held := k >= len(intentCases)
		r2, m2, p2 := &hRules, &hModel, &hPipe
		if k >= len(intentCases)+len(intentHoldout) {
			r2, m2, p2 = &h2Rules, &h2Model, &h2Pipe
		}
		var want Intent
		_ = json.Unmarshal([]byte(tc.Want), &want)
		if held {
			t0 := time.Now()
			ri := rulesIntent(tc.Q, now)
			r2.add(matches(ri, want, now), time.Since(t0), tc.Q, ri)
			t0 = time.Now()
			mi, err := modelIntent(ctx, c, tc.Q, now, recall(as.bank(ctx), tc.Q, 3))
			el := time.Since(t0)
			if err != nil {
				mi = Intent{I: "error"}
			}
			m2.add(matches(mi, want, now), el, tc.Q, mi)
			t0 = time.Now()
			pi, src := as.Understand(ctx, tc.Q, Intent{}, now)
			el = time.Since(t0)
			ok := matches(pi, want, now)
			p2.add(ok, el, tc.Q+" ["+src+"]", pi)
			fmt.Fprintf(w, "%s %5.2fs %-14s (новый) %s\n", map[bool]string{true: "✓", false: "✗"}[ok], el.Seconds(), src, tc.Q)
			continue
		}
		t0 := time.Now()
		ri := rulesIntent(tc.Q, now)
		rules.add(matches(ri, want, now), time.Since(t0), tc.Q, ri)
		t0 = time.Now()
		mi, err := modelIntent(ctx, c, tc.Q, now, recall(as.bank(ctx), tc.Q, 3))
		el := time.Since(t0)
		if err != nil {
			mi = Intent{I: "error"}
		}
		model.add(matches(mi, want, now), el, tc.Q, mi)
		t0 = time.Now()
		pi, src := as.Understand(ctx, tc.Q, Intent{}, now)
		el = time.Since(t0)
		ok := matches(pi, want, now)
		pipe.add(ok, el, tc.Q+" ["+src+"]", pi)
		fmt.Fprintf(w, "%s %5.2fs %-14s %s\n", map[bool]string{true: "✓", false: "✗"}[ok], el.Seconds(), src, tc.Q)
	}
	n := len(intentCases)
	fmt.Fprintf(w, "\n## %s · %d запросов\n%s\n%s\n%s\n", c.Model, n, rules.line("правила", n), model.line("модель", n), pipe.line("вместе", n))
	h := len(intentHoldout)
	fmt.Fprintf(w, "\n### Новые запросы (правила под них не настраивались) · %d\n%s\n%s\n%s\n", h, hRules.line("правила", h), hModel.line("модель", h), hPipe.line("вместе", h))
	h2 := len(intentHoldout2)
	fmt.Fprintf(w, "\n### Новые запросы №2 (ни правила, ни сверка под них не настраивались) · %d\n%s\n%s\n%s\n", h2, h2Rules.line("правила", h2), h2Model.line("модель", h2), h2Pipe.line("вместе", h2))
	pipe.bad = append(pipe.bad, hPipe.bad...)
	pipe.bad = append(pipe.bad, h2Pipe.bad...)
	model.bad = append(model.bad, hModel.bad...)
	fmt.Fprintf(w, "\nОшибки (вместе):\n")
	for _, b := range pipe.bad {
		fmt.Fprintln(w, "  ✗ "+b)
	}
	fmt.Fprintf(w, "\nОшибки модели:\n")
	for _, b := range model.bad {
		fmt.Fprintln(w, "  ✗ "+b)
	}
	// one line for scripts: the score that decides whether a model is installed — requests never tuned for
	held := float64(hPipe.ok+h2Pipe.ok) / float64(h+h2)
	lat := append(append([]int64{}, hPipe.ms...), h2Pipe.ms...)
	sort.Slice(lat, func(a, b int) bool { return lat[a] < lat[b] })
	p90 := 0.0
	if len(lat) > 0 {
		p90 = float64(lat[len(lat)*9/10]) / 1000
	}
	b, _ := json.Marshal(map[string]any{"model": c.Model, "held": held, "p90": p90, "gold": float64(pipe.ok) / float64(n)})
	fmt.Fprintf(w, "SUMMARY %s\n", b)
	return held
}

// ExportIntents: gold cases + real requests whose answer was used (a button pressed), in the chat format
// LoRA trainers take — the system prompt, the request, the intent JSON.
func ExportIntents(ctx context.Context, st *store.Store, w io.Writer) (int, error) {
	now := time.Now().In(time.FixedZone("MSK", 3*3600))
	sys := intentSystem(now)
	n := 0
	write := func(q, a string) {
		b, _ := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "system", "content": sys}, {"role": "user", "content": q}, {"role": "assistant", "content": a}}})
		fmt.Fprintln(w, string(b))
		n++
	}
	for _, tc := range intentCases {
		a := tc.Want
		for _, code := range []string{"TOD", "TOM", "SAT", "SUN", "FRI"} {
			a = strings.ReplaceAll(a, `"`+code+`"`, `"`+caseDate(code, now)+`"`)
		}
		write(tc.Q, a)
	}
	if st != nil {
		ex, err := st.Examples(ctx, true)
		if err != nil {
			return n, err
		}
		for _, e := range ex {
			if strings.HasPrefix(e.Calls, "intent:") {
				write(e.Input, strings.TrimPrefix(e.Calls, "intent:"))
			}
		}
	}
	return n, nil
}
