// Package assistant is the «Помощник» chat inside the app and the bot.
//
// The split is strict: the model understands the question and phrases the answer; the server computes
// everything (picks, windows, budget, history) through read tools. Anything that changes data is only
// *proposed* — it becomes a button, and a person presses it. The model can never vote, confirm a
// meeting or act for the partner. Without an AI key the same tools run behind simple rules.
package assistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"vdvoem/internal/ai"
	"vdvoem/internal/app"
	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

const (
	DailyLimit = 40 // questions per member per day
	maxSteps   = 4  // tool rounds per question
	history    = 12 // messages of context
)

type Card struct {
	Type     string `json:"type"` // idea | meeting
	IdeaID   int64  `json:"idea_id,omitempty"`
	RoundID  int64  `json:"round_id,omitempty"`
	Title    string `json:"title"`
	Meta     string `json:"meta,omitempty"`
	Image    string `json:"image,omitempty"`
	Category string `json:"category,omitempty"`
	Why      string `json:"why,omitempty"`
	When     string `json:"when,omitempty"`
	About    string `json:"about,omitempty"` // what it is, from the source — a line of detail right on the card
}

type Button struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"` // kind "open": the screen the button opens — the app goes there, nothing to run
}

type Reply struct {
	Text    string   `json:"text"`
	Cards   []Card   `json:"cards,omitempty"`
	Actions []Button `json:"actions,omitempty"`
	Open    string   `json:"open,omitempty"`   // the screen to open (an «open» button pressed)
	Intent  *Intent  `json:"intent,omitempty"` // how the request was understood — for follow-ups («а подешевле?»)
	Src     string   `json:"src,omitempty"`    // rules | model | rules-fallback
	Ms      int64    `json:"ms,omitempty"`
	Next    []string `json:"next,omitempty"` // what to ask next in one tap («А подешевле?», «Расскажи про первое»)
	Pick    *PickRef `json:"pick,omitempty"` // everything found, as a list to open
}

// PickRef: a «подборка» behind an answer — the chat shows the best few, the list holds them all
type PickRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	N     int    `json:"n"`
}

type Assistant struct {
	App        *app.App
	AI         *ai.Client
	EmbedModel string // search by meaning (e.g. bge-m3 in Ollama); "" — words only
	cache      bankCache
	sem        semIndex
}

// Tuned: model names starting with «vdvoem-» were fine-tuned on the intent dataset: they get the short prompt
// (no examples) — what they were trained on, and faster.
func Tuned(model string) bool { return strings.HasPrefix(model, "vdvoem-") }

// SetIntentModel switches the model (for everything: one model stays loaded on a CPU server); "" — back.
func (as *Assistant) SetIntentModel(model string) {
	if as.AI != nil {
		as.AI.SetModel(model)
	}
}

// IntentModel: what reads requests now
func (as *Assistant) IntentModel() *ai.Client { return as.AI }

var ErrLimit = errors.New("assistant_limit")

func (as *Assistant) now() time.Time {
	if as.App == nil {
		return time.Now()
	}
	return as.App.Now()
}

// Ask answers one question and stores both sides of the exchange.
func (as *Assistant) Ask(ctx context.Context, c app.Couple, text string) (Reply, error) {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > 600 {
		text = string(r[:600])
	}
	if text == "" {
		return as.hello(c), nil
	}
	day := time.Date(as.now().In(c.Loc()).Year(), as.now().In(c.Loc()).Month(), as.now().In(c.Loc()).Day(), 0, 0, 0, 0, c.Loc())
	if n, _ := as.App.S.ChatCountSince(ctx, c.Me.ID, day); n >= DailyLimit {
		return Reply{}, ErrLimit
	}
	prev, _ := as.App.S.Chat(ctx, c.Me.ID, history)
	_, _ = as.App.S.AddChat(ctx, c.Space.ID, c.Me.ID, "user", Reply{Text: text}, as.now())

	start := time.Now()
	run := &run{as: as, c: c}
	pt := lastTurn(prev, as.now())
	in, src, alt := as.understand(ctx, text, pt, as.now().In(c.Loc()))
	rep := run.do(ctx, in, text)
	rep.Intent, rep.Src, rep.Ms = &in, src, time.Since(start).Milliseconds()
	as.noteRephrase(ctx, c, in, text)
	var exID int64
	{ // every request goes to the journal (the owner sees it in the admin); a labelled example once a button is pressed
		b, _ := json.Marshal(in)
		if id, err := as.App.S.AddExample(ctx, c.Me.ID, text, "intent:"+string(b), rep.Text, as.now()); err == nil {
			exID = id
			_ = as.App.S.SetOutcome(ctx, id, "", src)
			for _, aid := range run.pending {
				_ = as.App.S.TagAction(ctx, aid, id)
			}
		}
	}
	if alt.I != "" { // the other reading, one tap away
		ab, _ := json.Marshal(alt)
		run.proposeAlt(ctx, map[string]any{"intent": json.RawMessage(ab), "text": text, "ex": exID}, "Или: "+lowerFirst(describe(alt)))
	}
	as.App.Log.Info("assistant", "intent", in.I, "src", src, "alt", alt.I, "ms", rep.Ms)
	rep.Cards, rep.Actions = run.cards, run.buttons
	rep.Pick = run.pick
	rep.Next = nextFor(in, rep.Cards)
	if in.I == "find" && !in.All && run.found > len(rep.Cards) && run.pick == nil {
		rep.Next = append([]string{fmt.Sprintf("Покажи все %d", run.found)}, rep.Next...)
		if len(rep.Next) > 4 {
			rep.Next = rep.Next[:4]
		}
	}
	if rep.Text == "" {
		rep.Text = "Готово."
	}
	if exID != 0 {
		model := ""
		if as.AI != nil && src != "rules" {
			model = as.AI.M()
		}
		var labels []string
		for _, b := range rep.Actions {
			labels = append(labels, b.Label)
		}
		_ = as.App.S.SetExampleMeta(ctx, exID, rep.Ms, model, strings.Join(labels, " | "), len(rep.Cards), "")
	}
	_, _ = as.App.S.AddChat(ctx, c.Space.ID, c.Me.ID, "assistant", rep, as.now())
	return rep, nil
}

// noteRephrase: the same person asked again within a minute and a half without pressing anything, and the new
// request reads differently — the previous one was most likely misunderstood.
// A next question in a chain («расскажи про первое», then «сколько стоит?», then «а дела по дому?») is not a
// rephrase: only the same wish in other words is — some word of it comes back.
func (as *Assistant) noteRephrase(ctx context.Context, c app.Couple, in Intent, text string) {
	e, err := as.App.S.LastExample(ctx, c.Me.ID)
	if err != nil || e.Accepted || e.Outcome != "" || as.now().Sub(e.At) > 90*time.Second {
		return
	}
	var was Intent
	if json.Unmarshal([]byte(strings.TrimPrefix(e.Calls, "intent:")), &was) != nil || was.I == in.I || in.refined {
		return
	}
	if !sharesWord(e.Input, text) {
		return
	}
	_ = as.App.S.SetOutcome(ctx, e.ID, "rephrased", "")
}

var stopWords = map[string]bool{"что": true, "как": true, "есть": true, "нас": true, "нам": true, "мне": true, "это": true, "куда": true, "где": true, "или": true, "для": true, "все": true, "всё": true}

func sharesWord(a, b string) bool {
	stems := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(a), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if r := []rune(w); len(r) >= 4 && !stopWords[w] {
			stems[string(r[:4])] = true
		}
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(b), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if r := []rune(w); len(r) >= 4 && !stopWords[w] && stems[string(r[:4])] {
			return true
		}
	}
	return false
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToLower(string(r[:1])) + string(r[1:])
}

// nextFor: follow-ups that make sense after this answer — each one is understood without the model
func nextFor(in Intent, cards []Card) []string {
	ideas := 0
	for _, c := range cards {
		if c.Type == "idea" {
			ideas++
		}
	}
	var out []string
	switch {
	case in.I == "suggest" && in.Mode == "random":
		out = append(out, "Удиви ещё раз", "Расскажи про это", "Что интересного на неделе?")
		return out
	}
	switch in.I {
	case "find", "suggest":
		if ideas > 0 {
			out = append(out, "Расскажи про первое")
		}
		if in.Max == 0 || in.Max > 1500 {
			out = append(out, "А подешевле?")
		}
		if in.Date == "" && !in.Wknd {
			out = append(out, "А на выходных?")
		} else {
			out = append(out, "А на неделе?")
		}
		if !in.Scout && in.Whose == "" {
			out = append(out, "Только новое")
		}
	case "about":
		if ideas > 0 {
			out = append(out, "Сколько это стоит?", "Давай туда")
		}
		out = append(out, "Что ещё есть на неделе?")
	case "price":
		if ideas > 0 {
			out = append(out, "Расскажи про это", "Давай туда")
		}
		out = append(out, "Что-нибудь подешевле на неделе")
	case "overview", "past", "chores", "plan", "book":
		out = append(out, "Что интересного на неделе?", "Удиви нас")
	case "chat":
		out = append(out, "Что интересного на неделе?", "Куда сходить в субботу вечером?", "Удиви нас")
	default: // remember, goal, budget… — done; what next
		out = append(out, "Что интересного на неделе?", "Что нашёл скаут?", "Удиви нас")
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func (as *Assistant) hello(c app.Couple) Reply {
	return Reply{Text: "Я помогаю вам двоим выбирать, куда сходить, и договариваться. Можно просто сказать, например:\n• Что интересного на неделе?\n• Хотим почилить в субботу вечером\n• Расскажи про Чайку — нам подойдёт?\n• Сколько стоит Щелкунчик?\n• Запиши нас на джаз в пятницу\n• Следи за билетами на Земфиру\nМожно и голосом — в боте просто пришли голосовое."}
}

// Turn: the previous exchange — what was asked, how it was understood, what was shown — for follow-ups
// («а подешевле?») and references («расскажи про второе»).
type Turn struct {
	Text   string
	In     Intent
	Titles []string
}

func lastTurn(prev []store.ChatMsg, now time.Time) Turn {
	for k := len(prev) - 1; k >= 0; k-- {
		if prev[k].Role != "assistant" {
			continue
		}
		if now.Sub(prev[k].CreatedAt) > 2*time.Hour {
			return Turn{}
		}
		var r Reply
		if json.Unmarshal(prev[k].Body, &r) != nil || r.Intent == nil {
			return Turn{}
		}
		t := Turn{In: *r.Intent}
		for _, c := range r.Cards {
			if c.Type == "idea" && c.Title != "" {
				t.Titles = append(t.Titles, c.Title)
			}
		}
		if k > 0 && prev[k-1].Role == "user" {
			var u Reply
			if json.Unmarshal(prev[k-1].Body, &u) == nil {
				t.Text = u.Text
			}
		}
		return t
	}
	return Turn{}
}

func lastIntent(prev []store.ChatMsg, now time.Time) Intent { return lastTurn(prev, now).In }

// History for the chat screen.
func (as *Assistant) History(ctx context.Context, c app.Couple) ([]map[string]any, error) {
	msgs, err := as.App.S.Chat(ctx, c.Me.ID, 50)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, m := range msgs {
		var r Reply
		_ = json.Unmarshal(m.Body, &r)
		out = append(out, map[string]any{"id": m.ID, "role": m.Role, "at": m.CreatedAt.Unix(), "reply": r})
	}
	return out, nil
}

// ---------- one question ----------

type run struct {
	as        *Assistant
	c         app.Couple
	cards     []Card
	buttons   []Button
	seen      map[string]bool
	exampleID int64
	pending   []string // action ids proposed in this run (tagged with the example once it is saved)
	from      string   // where a pick comes from: "" — the bank, afisha — the scout and channels, all
	found     int      // how many fit a search (more than shown → «Покажи все»)
	pick      *PickRef
}

func (r *run) reset() { r.cards, r.buttons, r.seen = nil, nil, nil }

func (r *run) systemPrompt() string {
	loc := r.c.Loc()
	now := r.as.now().In(loc)
	budget := "не задан"
	if r.c.Space.BudgetKop > 0 {
		budget = domain.Money(r.c.Space.BudgetKop)
	}
	return fmt.Sprintf(`Ты — помощник в приложении «Вдвоём» для пары: %s и %s. Ты помогаешь им решить, куда сходить и когда, и не забывать дела.
Сейчас %s, %s, часовой пояс %s. Бюджет на встречу: %s. Пишет %s.
Правила:
- Все данные бери только из инструментов. Не выдумывай места, цены, даты и прошлые встречи.
- Подбор идей делает инструмент suggest — не ранжируй сам. Если просят «удиви», «случайно» — suggest с mode=random.
- Изменить что-то ты не можешь: только предложить кнопкой через propose_*. Человек нажмёт сам. Не говори «я добавил» — говори «добавить?».
- Голосовать, подтверждать встречу или отвечать за партнёра нельзя — это делают они сами.
- Если в копилке нет подходящего — честно скажи и предложи добавить идею (propose_add_idea), можно с примерами, что поискать.
- Про афишу, события, «что на выходных», анонсы — инструмент afisha; запросы «подешевле», «без театра», «только суббота» — это фильтры afisha.
- Если человек сообщает о своём вкусе — remember. Если просит следить за чем-то в будущем — add_goal.
- Отвечай по-русски, коротко: 1–3 предложения. Карточки идей приложение покажет само — не перечисляй их подробно.%s`,
		r.c.Me.Name, r.c.PartnerName(), domain.When(now, loc), now.Format("2006"), loc.String(), budget, r.c.Me.Name, r.memoryNote())
}

// memoryNote: what each person told the assistant, so answers respect it.
func (r *run) memoryNote() string {
	mem, _ := r.as.App.S.Memories(context.Background(), r.c.Space.ID)
	var mine, theirs []string
	for _, m := range mem {
		if m.MemberID == r.c.Me.ID {
			mine = append(mine, m.Text)
		} else {
			theirs = append(theirs, m.Text)
		}
	}
	out := ""
	if len(mine) > 0 {
		out += "\nЧто помнишь о " + r.c.Me.Name + ": " + strings.Join(limitS(mine, 8), "; ") + "."
	}
	if len(theirs) > 0 {
		out += "\nЧто помнишь о " + r.c.PartnerName() + ": " + strings.Join(limitS(theirs, 8), "; ") + "."
	}
	return out
}

func limitS(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (r *run) withModel(ctx context.Context, prev []store.ChatMsg, text string) (Reply, error) {
	msgs := []ai.Message{{Role: "system", Content: r.systemPrompt()}}
	for _, m := range prev {
		var rp Reply
		if json.Unmarshal(m.Body, &rp) == nil && rp.Text != "" {
			msgs = append(msgs, ai.Message{Role: m.Role, Content: rp.Text})
		}
	}
	msgs = append(msgs, ai.Message{Role: "user", Content: text})
	var calls []ai.ToolCall
	for step := 0; step < maxSteps; step++ {
		t, err := r.as.AI.Step(ctx, "assistant", msgs, Tools)
		if err != nil {
			return Reply{}, err
		}
		if len(t.Calls) == 0 {
			r.example(ctx, text, calls, t.Content)
			return Reply{Text: t.Content}, nil
		}
		calls = append(calls, t.Calls...)
		msgs = append(msgs, ai.Message{Role: "assistant", Content: t.Content, ToolCalls: t.Calls})
		for _, call := range t.Calls {
			out := r.exec(ctx, call.Name, call.Args)
			b, _ := json.Marshal(out)
			msgs = append(msgs, ai.Message{Role: "tool", ToolCallID: call.ID, Content: string(b)})
		}
	}
	return Reply{Text: "Вот что нашёл."}, nil
}

// example keeps the exchange as a training example for the local model (questions, tool calls, answer);
// it becomes «accepted» when a proposed button from it is pressed.
func (r *run) example(ctx context.Context, text string, calls []ai.ToolCall, answer string) {
	b, _ := json.Marshal(calls)
	id, err := r.as.App.S.AddExample(ctx, r.c.Me.ID, text, string(b), answer, r.as.now())
	if err == nil {
		r.exampleID = id
		if len(r.pending) > 0 {
			for _, aid := range r.pending {
				_ = r.as.App.S.TagAction(ctx, aid, id)
			}
		}
	}
}

// ---------- tools ----------

func obj(props map[string]any, req ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(req) > 0 {
		o["required"] = req
	}
	return o
}

func str(desc string, enum ...string) map[string]any {
	m := map[string]any{"type": "string", "description": desc}
	if len(enum) > 0 {
		m["enum"] = enum
	}
	return m
}

func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

var Tools = []ai.Tool{
	{Name: "overview", Description: "Сводка пары: ближайшая встреча, идущий выбор и его этап, сколько идей в копилке, бюджет.", Parameters: obj(map[string]any{})},
	{Name: "find_ideas", Description: "Поиск идей в копилке пары.", Parameters: obj(map[string]any{
		"query":         str("слова из названия или адреса"),
		"category":      str("категория", "food", "culture", "walk", "sport", "home", "other"),
		"max_price_rub": num("максимум на двоих, ₽"),
		"wished_by":     str("кто отметил «хочу»", "both", "me", "partner", "nobody"),
		"limit":         num("сколько вернуть, до 8"),
	})},
	{Name: "suggest", Description: "Серверный подбор: лучшие идеи по оценкам обоих или случайная. Учитывает пропуски, бюджет, длительность, сроки событий.", Parameters: obj(map[string]any{
		"mode":       str("best — по оценке, random — случайно", "best", "random"),
		"date":       str("день YYYY-MM-DD, если назван"),
		"part":       str("часть дня", "morning", "day", "evening"),
		"budget_rub": num("бюджет на двоих, ₽, если назван"),
		"category":   str("если просят конкретный тип", "food", "culture", "walk", "sport", "other"),
		"count":      num("1–3"),
	})},
	{Name: "afisha", Description: "Поиск по находкам скаута (афиша недели и анонсы на 1–3 месяца) и, если надо, по копилке. Сервер сам ранжирует по вкусам обоих.", Parameters: obj(map[string]any{
		"date":               str("день YYYY-MM-DD, если назван"),
		"weekend":            map[string]any{"type": "boolean", "description": "ближайшие выходные"},
		"part":               str("часть дня", "morning", "day", "evening"),
		"max_price_rub":      num("максимум на двоих, ₽"),
		"categories":         map[string]any{"type": "array", "items": str("", "food", "culture", "walk", "sport", "other")},
		"exclude_categories": map[string]any{"type": "array", "items": str("", "food", "culture", "walk", "sport", "other")},
		"exclude_words":      map[string]any{"type": "array", "items": str("слово, которого не должно быть: «стендап», «опера»")},
		"query":              str("слово из названия, если ищут конкретное"),
		"horizon":            str("week — ближайшее, later — анонсы на потом, any", "week", "later", "any"),
		"include_bank":       map[string]any{"type": "boolean", "description": "искать и в копилке, не только у скаута"},
	})},
	{Name: "remember", Description: "Запомнить предпочтение того, кто пишет («не люблю стендап», «по пятницам устаём»). Сохраняется сразу, видно ему в настройках.", Parameters: obj(map[string]any{"text": str("предпочтение одной фразой, от первого лица")}, "text")},
	{Name: "add_goal", Description: "Дать скауту цель на будущее: «театр в январе», «концерт Земфиры, когда приедет». Сохраняется сразу.", Parameters: obj(map[string]any{"text": str("что искать")}, "text")},
	{Name: "past_meetings", Description: "Где пара была недавно и хотят ли повторить.", Parameters: obj(map[string]any{"limit": num("до 10")})},
	{Name: "chores", Description: "Бытовые дела пары: кто делает, срок.", Parameters: obj(map[string]any{})},
	{Name: "propose_add_idea", Description: "Предложить кнопку «Добавить в копилку». Не добавляет само.", Parameters: obj(map[string]any{"text": str("название и/или ссылка")}, "text")},
	{Name: "propose_plan", Description: "Предложить кнопку «Идём сюда»: откроет подбор времени для этой идеи у обоих.", Parameters: obj(map[string]any{"idea_id": num("id идеи")}, "idea_id")},
	{Name: "propose_round", Description: "Предложить кнопку «Выбрать вместе»: обычный выбор — время, затем три варианта.", Parameters: obj(map[string]any{})},
	{Name: "propose_wish", Description: "Предложить отметку своей оценки идеи.", Parameters: obj(map[string]any{"idea_id": num("id"), "wish": str("оценка", "want", "very", "skip")}, "idea_id", "wish")},
	{Name: "propose_budget", Description: "Предложить изменить бюджет на встречу.", Parameters: obj(map[string]any{"rub": num("₽ на двоих, 0 — без лимита")}, "rub")},
	{Name: "propose_suggest_mode", Description: "Предложить, как бот сам присылает идеи раз в неделю.", Parameters: obj(map[string]any{"mode": str("best, random, mix (через неделю), off", "best", "random", "mix", "off")}, "mode")},
}

func (r *run) exec(ctx context.Context, name string, raw json.RawMessage) any {
	var a struct {
		Query       string `json:"query"`
		Category    string `json:"category"`
		MaxPriceRub int64  `json:"max_price_rub"`
		WishedBy    string `json:"wished_by"`
		Limit       int    `json:"limit"`
		Mode        string `json:"mode"`
		Date        string `json:"date"`
		Part        string `json:"part"`
		BudgetRub   int64  `json:"budget_rub"`
		Count       int    `json:"count"`
		Text        string `json:"text"`
		IdeaID      int64  `json:"idea_id"`
		Wish        string `json:"wish"`
		Rub         int64  `json:"rub"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return map[string]string{"error": "bad arguments"}
	}
	switch name {
	case "afisha":
		var q afishaQ
		_ = json.Unmarshal(raw, &q)
		return r.afisha(ctx, q)
	case "remember":
		m, err := r.as.App.Remember(ctx, r.c, a.Text)
		if err != nil {
			return map[string]string{"error": "empty"}
		}
		return map[string]any{"saved": m.Text}
	case "add_goal":
		g, err := r.as.App.AddGoal(ctx, r.c, a.Text)
		if err != nil {
			return map[string]string{"error": "empty"}
		}
		return map[string]any{"goal": g.Text, "note": "скаут будет искать это каждую неделю, в том числе на месяцы вперёд"}
	case "overview":
		return r.overview(ctx)
	case "find_ideas":
		return r.findIdeas(ctx, a.Query, a.Category, a.MaxPriceRub, a.WishedBy, a.Limit)
	case "suggest":
		return r.suggest(ctx, a.Mode, a.Date, a.Part, a.BudgetRub, a.Category, a.Count)
	case "past_meetings":
		return r.past(ctx, a.Limit)
	case "chores":
		return r.chores(ctx)
	case "propose_add_idea":
		if strings.TrimSpace(a.Text) == "" {
			return map[string]string{"error": "empty"}
		}
		return r.propose(ctx, "add_idea", map[string]any{"text": a.Text}, "Добавить «"+short(a.Text, 28)+"»")
	case "propose_plan":
		i, err := r.as.App.S.Idea(ctx, r.c.Space.ID, a.IdeaID)
		if err != nil {
			return map[string]string{"error": "no such idea"}
		}
		return r.propose(ctx, "plan", map[string]any{"idea_id": i.ID}, "Идём: "+short(i.Title, 26))
	case "propose_round":
		return r.propose(ctx, "round", map[string]any{}, "Выбрать вместе")
	case "propose_wish":
		w := domain.Wish(a.Wish)
		if !w.Valid() || w == domain.WishNone {
			return map[string]string{"error": "bad wish"}
		}
		i, err := r.as.App.S.Idea(ctx, r.c.Space.ID, a.IdeaID)
		if err != nil {
			return map[string]string{"error": "no such idea"}
		}
		label := map[domain.Wish]string{domain.WishWant: "Хочу", domain.WishVery: "Очень хочу", domain.WishSkip: "Пропущу"}[w]
		return r.propose(ctx, "wish", map[string]any{"idea_id": i.ID, "wish": a.Wish}, label+": "+short(i.Title, 24))
	case "propose_budget":
		if a.Rub < 0 || a.Rub > 1_000_000 {
			return map[string]string{"error": "bad budget"}
		}
		label := "Бюджет " + domain.Money(a.Rub*100)
		if a.Rub == 0 {
			label = "Без лимита бюджета"
		}
		return r.propose(ctx, "budget", map[string]any{"rub": a.Rub}, label)
	case "propose_suggest_mode":
		m := domain.SuggestMode(a.Mode)
		if !m.Valid() {
			return map[string]string{"error": "bad mode"}
		}
		return r.propose(ctx, "suggest_mode", map[string]any{"mode": a.Mode}, modeLabel[m])
	}
	return map[string]string{"error": "unknown tool"}
}

var modeLabel = map[domain.SuggestMode]string{domain.SuggestBest: "Присылать лучшее", domain.SuggestRandom: "Присылать случайное",
	domain.SuggestMix: "Через неделю: лучшее и случайное", domain.SuggestOff: "Не присылать идеи"}

func short(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

func token() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (r *run) propose(ctx context.Context, kind string, payload map[string]any, label string) any {
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	b, _ := json.Marshal(payload)
	key := kind + string(b)
	if r.seen[key] || len(r.buttons) >= 4 {
		return map[string]string{"status": "button already shown"}
	}
	r.seen[key] = true
	act := store.Action{ID: token(), SpaceID: r.c.Space.ID, MemberID: r.c.Me.ID, Kind: kind, Payload: b}
	if err := r.as.App.S.SaveAction(ctx, act, r.as.now()); err != nil {
		return map[string]string{"error": "cannot save"}
	}
	bt := Button{ID: act.ID, Label: label, Kind: kind}
	if kind == "open" {
		bt.Path, _ = payload["path"].(string)
	}
	r.buttons = append(r.buttons, bt)
	r.pending = append(r.pending, act.ID)
	return map[string]string{"status": "button shown to the user; nothing changed yet"}
}

// proposeAlt: the «или …» button with the other reading; always fits (a fifth button at most)
func (r *run) proposeAlt(ctx context.Context, payload map[string]any, label string) {
	b, _ := json.Marshal(payload)
	act := store.Action{ID: token(), SpaceID: r.c.Space.ID, MemberID: r.c.Me.ID, Kind: "clarify", Payload: b}
	if r.as.App.S.SaveAction(ctx, act, r.as.now()) == nil {
		r.buttons = append(r.buttons, Button{ID: act.ID, Label: label, Kind: "clarify"})
	}
}

func (r *run) addCard(cd Card) {
	for _, x := range r.cards {
		if x.Type == cd.Type && x.IdeaID == cd.IdeaID && x.RoundID == cd.RoundID {
			return
		}
	}
	if cd.Type == "idea" && cd.When == "" && cd.IdeaID != 0 { // the day on the card itself — «на выходных» is checkable at a glance
		if i, err := r.as.App.S.Idea(context.Background(), r.c.Space.ID, cd.IdeaID); err == nil && i.StartsAt != nil && (i.EndsAt == nil || i.EndsAt.Sub(*i.StartsAt) < 30*time.Hour) {
			cd.When = domain.When(*i.StartsAt, r.c.Loc())
		}
	}
	if len(r.cards) < 6 {
		r.cards = append(r.cards, cd)
	}
}

func ideaCard(i store.Idea, why string) Card {
	about := strings.TrimSpace(i.Info.About)
	if r := []rune(about); len(r) > 140 {
		about = strings.TrimSpace(string(r[:137])) + "…"
	}
	if about == "" && len(i.Info.Genres) > 0 {
		about = strings.Join(i.Info.Genres, ", ")
	}
	return Card{Type: "idea", IdeaID: i.ID, Title: i.Title, Meta: app.IdeaMeta(i), Image: i.ImageURL, Category: i.Category, Why: why, About: about}
}

func (r *run) myWish(i store.Idea) (me, partner domain.Wish) {
	if r.c.Me.Color == "b" {
		return i.WishB, i.WishA
	}
	return i.WishA, i.WishB
}

// Ours: an idea of the couple — one of them added it or marked it; otherwise it is news from the scout or a
// channel that neither has seen yet. The assistant keeps the two apart.
func Ours(i store.Idea) bool {
	return !app.Afisha(i) || i.WishA != domain.WishNone || i.WishB != domain.WishNone || i.Status == domain.IdeaActive && i.Source == ""
}

func (r *run) brief(i store.Idea) map[string]any {
	me, p := r.myWish(i)
	m := map[string]any{"id": i.ID, "title": i.Title, "category": i.Category, "my_wish": string(me), "partner_wish": string(p)}
	if i.PriceMin > 0 {
		m["price_rub"] = i.PriceMin / 100
	}
	if i.DurationMin > 0 {
		m["duration_min"] = i.DurationMin
	}
	if i.Address != "" {
		m["address"] = i.Address
	}
	if i.EndsAt != nil {
		m["ends"] = i.EndsAt.In(r.c.Loc()).Format("2006-01-02")
	}
	if i.StartsAt != nil {
		m["starts_at"] = i.StartsAt.Unix()
	}
	m["ours"] = Ours(i)
	if i.Kind == domain.KindChore {
		m["kind"] = "chore"
	}
	return m
}

func (r *run) overview(ctx context.Context) any {
	s := r.as.App.S
	out := map[string]any{"partner": r.c.PartnerName(), "paired": r.c.Partner != nil}
	ideas, _ := s.Ideas(ctx, r.c.Space.ID, domain.IdeaActive)
	leisure, chores, unmarked := 0, 0, 0
	for _, i := range ideas {
		if i.Kind == domain.KindChore {
			chores++
			continue
		}
		leisure++
		if me, _ := r.myWish(i); me == domain.WishNone {
			unmarked++
		}
	}
	out["ideas"], out["chores"], out["ideas_i_have_not_marked"] = leisure, chores, unmarked
	if r.c.Space.BudgetKop > 0 {
		out["budget_rub"] = r.c.Space.BudgetKop / 100
	}
	if rd, err := s.NextPlanned(ctx, r.c.Space.ID); err == nil && rd.StartsAt.After(r.as.now()) {
		i, _ := s.Idea(ctx, r.c.Space.ID, rd.Chosen)
		when := domain.When(rd.StartsAt, r.c.Loc())
		out["next_meeting"] = map[string]any{"title": i.Title, "when": when, "address": i.Address}
		r.addCard(Card{Type: "meeting", RoundID: rd.ID, IdeaID: i.ID, Title: i.Title, Meta: app.IdeaMeta(i), Image: i.ImageURL, Category: i.Category, When: when})
	}
	if rd, err := s.OpenRound(ctx, r.c.Space.ID); err == nil {
		st := map[string]any{"stage": string(rd.State), "waiting_for": waitingNames(r.c, rd.Waiting())}
		out["open_choice"] = st
	}
	return out
}

func waitingNames(c app.Couple, colors []string) []string {
	var out []string
	for _, col := range colors {
		if col == c.Me.Color {
			out = append(out, c.Me.Name+" (тот, кто спрашивает)")
		} else if c.Partner != nil {
			out = append(out, c.Partner.Name)
		}
	}
	return out
}

func (r *run) findIdeas(ctx context.Context, q, cat string, maxRub int64, by string, limit int) any {
	if limit <= 0 || limit > 8 {
		limit = 5
	}
	ideas, _ := r.as.App.S.Ideas(ctx, r.c.Space.ID, domain.IdeaActive)
	q = strings.ToLower(strings.TrimSpace(q))
	var found []map[string]any
	for _, i := range ideas {
		if cat != "" && i.Category != cat {
			continue
		}
		if maxRub > 0 && i.PriceMin > maxRub*100 {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(i.Title+" "+i.Address+" "+i.Note), q) && domain.Similarity(q, i.Title) < domain.SimilarThreshold {
			continue
		}
		me, p := r.myWish(i)
		want := func(w domain.Wish) bool { return w == domain.WishWant || w == domain.WishVery }
		switch by {
		case "both":
			if !want(me) || !want(p) {
				continue
			}
		case "me":
			if !want(me) {
				continue
			}
		case "partner":
			if !want(p) {
				continue
			}
		case "nobody":
			if me != domain.WishNone || p != domain.WishNone {
				continue
			}
		}
		found = append(found, r.brief(i))
		if len(found) <= 4 {
			r.addCard(ideaCard(i, ""))
		}
		if len(found) >= limit {
			break
		}
	}
	return map[string]any{"ideas": found, "total_in_kopilka": len(ideas)}
}

var partHours = map[string][2]int{"morning": {9, 13}, "day": {13, 18}, "evening": {18, 23}}

func (r *run) suggest(ctx context.Context, mode, date, part string, budgetRub int64, cat string, count int) any {
	s := r.as.App.S
	loc := r.c.Loc()
	ideas, _ := s.Ideas(ctx, r.c.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	in := domain.PickInput{BudgetKop: r.c.Space.BudgetKop, Now: r.as.now()}
	if budgetRub > 0 {
		in.BudgetKop = budgetRub * 100
	}
	planned := s.PlannedIdeas(ctx, r.c.Space.ID)
	for _, i := range ideas {
		if cat != "" && i.Category != cat || planned[i.ID] || i.Kind != domain.KindLeisure {
			continue
		}
		switch {
		case r.from == "afisha" && !app.Afisha(i), r.from == "" && i.Status != domain.IdeaActive:
			continue
		case i.StartsAt != nil && i.StartsAt.Before(r.as.now()) && (i.EndsAt == nil || i.EndsAt.Before(r.as.now())):
			continue
		}
		i.Status = domain.IdeaActive
		in.Ideas = append(in.Ideas, i.Idea)
	}
	if date != "" {
		d, err := time.ParseInLocation("2006-01-02", date, loc)
		if err == nil {
			h := [2]int{9, 23}
			if ph, ok := partHours[part]; ok {
				h = ph
			}
			in.Windows = []domain.Window{{From: d.Add(time.Duration(h[0]) * time.Hour), To: d.Add(time.Duration(h[1]) * time.Hour)}}
		}
	} else if ph, ok := partHours[part]; ok {
		for _, d := range app.SlotDays(r.as.now(), loc) {
			in.Windows = append(in.Windows, domain.Window{From: d.Add(time.Duration(ph[0]) * time.Hour), To: d.Add(time.Duration(ph[1]) * time.Hour)})
		}
	}
	var picks []domain.Scored
	if mode == "random" {
		if p, ok := domain.PickOne(in, domain.SuggestRandom, randSrc(r.as.now())); ok {
			picks = []domain.Scored{p}
		}
	} else {
		picks = domain.Candidates(in)
		if count <= 0 || count > 3 {
			count = 3
		}
		if len(picks) > count {
			picks = picks[:count]
		}
	}
	if len(picks) == 0 {
		return map[string]any{"ideas": []any{}, "note": "в копилке нет подходящих идей под эти условия", "total_in_kopilka": len(ideas)}
	}
	var out []map[string]any
	for k, p := range picks {
		full, _ := s.Idea(ctx, r.c.Space.ID, p.Idea.ID)
		why := domain.Why(p)
		b := r.brief(full)
		b["why"], b["score"] = why, p.Score
		out = append(out, b)
		r.addCard(ideaCard(full, why))
		if k == 0 {
			r.propose(ctx, "plan", map[string]any{"idea_id": full.ID}, "Идём: "+short(full.Title, 26))
		}
	}
	return map[string]any{"ideas": out, "mode": mode}
}

func (r *run) past(ctx context.Context, limit int) any {
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	rs, _ := r.as.App.S.RecentRounds(ctx, r.c.Space.ID, 30)
	var out []map[string]any
	for _, rd := range rs {
		if rd.State != domain.StateDone {
			continue
		}
		i, err := r.as.App.S.Idea(ctx, r.c.Space.ID, rd.Chosen)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"title": i.Title, "when": domain.When(rd.StartsAt, r.c.Loc()), "again_a": rd.AgainA, "again_b": rd.AgainB})
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{"meetings": out}
}

func (r *run) chores(ctx context.Context) any {
	ideas, _ := r.as.App.S.Ideas(ctx, r.c.Space.ID, domain.IdeaActive)
	var out []map[string]any
	for _, i := range ideas {
		if i.Kind != domain.KindChore {
			continue
		}
		m := r.brief(i)
		who := "кто угодно"
		switch i.Assignee {
		case r.c.Me.Color:
			who = r.c.Me.Name
		case "a", "b":
			who = r.c.PartnerName()
		}
		m["who"] = who
		if i.Deadline != nil {
			m["deadline"] = i.Deadline.In(r.c.Loc()).Format("2006-01-02")
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(a, b int) bool { return fmt.Sprint(out[a]["deadline"]) < fmt.Sprint(out[b]["deadline"]) })
	return map[string]any{"chores": out}
}

// ---------- rules fallback (no model) ----------

var (
	budgetRe = regexp.MustCompile(`(?:до|за|бюджет|не дороже)\s*(\d[\d\s]{2,6})`)
	addRe    = regexp.MustCompile(`(?i)^(?:добавь|добавить|сохрани)[:\s,-]+(.+)$`)
)

func has(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func (r *run) withRules(ctx context.Context, text string) Reply {
	t := strings.ToLower(text)
	if mem := memoryFromText(text); mem != "" {
		r.exec(ctx, "remember", mustJSON(map[string]any{"text": mem}))
		return Reply{Text: "Запомнил: «" + short(mem, 80) + "». Буду учитывать — и скаут тоже. Посмотреть и удалить можно в «Мы → Помощник помнит»."}
	}
	if g := goalFromText(text); g != "" {
		r.exec(ctx, "add_goal", mustJSON(map[string]any{"text": g}))
		return Reply{Text: "Скаут будет следить: «" + short(g, 80) + "». Напишу, когда найдётся — даже если это через пару месяцев."}
	}
	if has(t, "афиш", "скаут", "анонс", "событи", "мероприят", "концерт", "спектак", "что на выходн", "что в суббот", "что в воскрес", "подешевле", "без ") {
		q := afishaFromText(t, r.as.now().In(r.c.Loc()))
		res := r.afisha(ctx, q).(map[string]any)
		if l, _ := res["events"].([]map[string]any); len(l) > 0 {
			return Reply{Text: "Вот что подходит — сверху то, что зайдёт вам обоим. Можно уточнить: «подешевле», «без театра», «только вечером»."}
		}
		return Reply{Text: "Под эти условия ничего не нашёл. Ослабь фильтры — или скажи «следи за …», и скаут поищет специально."}
	}
	if m := addRe.FindStringSubmatch(strings.TrimSpace(text)); m != nil {
		r.exec(ctx, "propose_add_idea", mustJSON(map[string]any{"text": m[1]}))
		return Reply{Text: "Добавить это в копилку?"}
	}
	switch {
	case has(t, "удиви", "рандом", "случайн", "наугад", "сюрприз"):
		res := r.exec(ctx, "suggest", mustJSON(map[string]any{"mode": "random"}))
		return Reply{Text: textFor(res, "🎲 Вот что выпало из вашей копилки.")}
	case has(t, "куда", "посоветуй", "предложи", "что делать", "чем заняться", "сходить", "идея"):
		args := map[string]any{"mode": "best"}
		if d := dayFrom(t, r.as.now().In(r.c.Loc())); d != "" {
			args["date"] = d
		}
		switch {
		case has(t, "вечер"):
			args["part"] = "evening"
		case has(t, "утр"):
			args["part"] = "morning"
		case has(t, "днём", "днем", "обед"):
			args["part"] = "day"
		}
		if m := budgetRe.FindStringSubmatch(t); m != nil {
			n, _ := strconv.ParseInt(strings.ReplaceAll(m[1], " ", ""), 10, 64)
			args["budget_rub"] = n
		}
		for k, c := range map[string]string{"поесть": "food", "ресторан": "food", "кафе": "food", "ужин": "food", "театр": "culture", "музей": "culture", "кино": "culture", "выставк": "culture", "гулять": "walk", "прогул": "walk", "спорт": "sport", "каток": "sport"} {
			if strings.Contains(t, k) {
				args["category"] = c
			}
		}
		res := r.exec(ctx, "suggest", mustJSON(args))
		return Reply{Text: textFor(res, "Подобрал по вашим оценкам — первый вариант мой фаворит.")}
	case has(t, "дела", "убор", "задач", "по дому"):
		res := r.chores(ctx).(map[string]any)
		if l, _ := res["chores"].([]map[string]any); len(l) == 0 {
			return Reply{Text: "Дел в списке нет."}
		} else {
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
	case has(t, "были", "история", "сходили", "прошл"):
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
	case has(t, "план", "когда", "встреч", "что у нас", "ближайш"):
		o := r.overview(ctx).(map[string]any)
		txt := "Ничего не запланировано."
		if nm, ok := o["next_meeting"].(map[string]any); ok {
			txt = fmt.Sprintf("Ближайшая встреча: %s, %s.", nm["title"], nm["when"])
		}
		if oc, ok := o["open_choice"].(map[string]any); ok {
			txt += " Идёт выбор — ждём: " + strings.Join(oc["waiting_for"].([]string), ", ") + "."
		} else if _, ok := o["next_meeting"]; !ok {
			r.exec(ctx, "propose_round", mustJSON(nil))
		}
		return Reply{Text: txt}
	}
	h := r.as.hello(r.c)
	h.Text = "Я пока понимаю простые просьбы. " + h.Text
	return h
}

func textFor(res any, ok string) string {
	m, _ := res.(map[string]any)
	if l, _ := m["ideas"].([]map[string]any); len(l) > 0 {
		return ok
	}
	return "Под эти условия в копилке ничего нет. Добавьте пару идей — или ослабьте условия."
}

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("{}")
	}
	b, _ := json.Marshal(v)
	return b
}

var weekdays = map[string]time.Weekday{"понедельник": time.Monday, "вторник": time.Tuesday, "сред": time.Wednesday, "четверг": time.Thursday,
	"пятниц": time.Friday, "суббот": time.Saturday, "воскресень": time.Sunday}

var wordRes = map[string]*regexp.Regexp{} // filled at start, read-only after

func init() {
	for _, w := range []string{"послезавтра", "сегодня", "завтра"} {
		wordRes[w] = regexp.MustCompile(`(?:^|[^а-яёa-z])` + w + `(?:$|[^а-яёa-z])`)
	}
}

func wordRe(w string) *regexp.Regexp { return wordRes[w] }

func dayFrom(t string, now time.Time) string {
	d0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch { // whole words: «позавтракать» is not «завтра»
	case wordRe("послезавтра").MatchString(t):
		return d0.AddDate(0, 0, 2).Format("2006-01-02")
	case wordRe("сегодня").MatchString(t):
		return d0.Format("2006-01-02")
	case wordRe("завтра").MatchString(t):
		return d0.AddDate(0, 0, 1).Format("2006-01-02")
	}
	for w, wd := range weekdays {
		if strings.Contains(t, w) {
			diff := (int(wd) - int(now.Weekday()) + 7) % 7
			return d0.AddDate(0, 0, diff).Format("2006-01-02")
		}
	}
	return ""
}

// ---------- pressing a button ----------

// Act runs a proposed action once, as the person who pressed it.
// clarify: the person picked the other reading. The first guess becomes a miss, the chosen one a training
// example for the same words, and the answer is a full reply (cards, buttons) — not a toast.
func (as *Assistant) clarify(ctx context.Context, c app.Couple, payload []byte) (Reply, error) {
	var p struct {
		Intent Intent `json:"intent"`
		Text   string `json:"text"`
		Ex     int64  `json:"ex"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.Intent.I == "" {
		return Reply{Text: "Эта кнопка устарела — спроси ещё раз."}, nil
	}
	if p.Ex != 0 {
		_ = as.App.S.SetOutcome(ctx, p.Ex, "clarified", "")
	}
	start := time.Now()
	run := &run{as: as, c: c}
	rep := run.do(ctx, p.Intent, p.Text)
	b, _ := json.Marshal(p.Intent)
	if id, err := as.App.S.AddExample(ctx, c.Me.ID, p.Text, "intent:"+string(b), rep.Text, as.now()); err == nil {
		_ = as.App.S.AcceptExample(ctx, id)
		_ = as.App.S.SetOutcome(ctx, id, "chosen", "user")
	}
	in := p.Intent
	rep.Intent, rep.Src, rep.Ms = &in, "user", time.Since(start).Milliseconds()
	rep.Cards, rep.Actions, rep.Next, rep.Pick = run.cards, run.buttons, nextFor(in, run.cards), run.pick
	if rep.Text == "" {
		rep.Text = "Готово."
	}
	_, _ = as.App.S.AddChat(ctx, c.Space.ID, c.Me.ID, "assistant", rep, as.now())
	return rep, nil
}

func (as *Assistant) Act(ctx context.Context, c app.Couple, id string) (Reply, error) {
	a, err := as.App.S.TakeAction(ctx, id, c.Me.ID, as.now())
	if err != nil {
		return Reply{Text: "Эта кнопка уже сработала или устарела."}, nil
	}
	var p struct {
		Text   string `json:"text"`
		IdeaID int64  `json:"idea_id"`
		Wish   string `json:"wish"`
		Rub    int64  `json:"rub"`
		Mode   string `json:"mode"`
	}
	_ = json.Unmarshal(a.Payload, &p)
	if a.Kind == "clarify" {
		return as.clarify(ctx, c, a.Payload)
	}
	if a.Example != 0 { // the model's proposal was good enough to press: a training example
		_ = as.App.S.AcceptExample(ctx, a.Example)
	}
	var rep Reply
	switch a.Kind {
	case "scout_ask": // new things come from the scout: it searches this on its next round (every morning)
		if err := as.App.AskScout(ctx, c, p.Text, 0); err != nil {
			return rep, err
		}
		rep.Text = "🔭 Попросил скаута поискать: " + strings.TrimPrefix(p.Text, "Найти новое: ") + ". Он ищет каждое утро — найденное пришлю сюда и в бот, оно появится в Афише."
	case "add_idea":
		res, err := as.App.AddIdea(ctx, c, p.Text)
		if err != nil {
			return rep, err
		}
		if res.Existing != nil {
			rep.Text = "Это уже есть в копилке: «" + res.Existing.Title + "»."
		} else {
			rep.Text = "Добавил в копилку. Сейчас разберу подробности и пришлю карточку."
		}
	case "plan":
		_, err := as.App.GoWith(ctx, c, p.IdeaID)
		switch {
		case errors.Is(err, app.ErrNotPaired):
			rep.Text = "Сначала пригласи партнёра — планировать вдвоём интереснее."
		case errors.Is(err, app.ErrRoundOpen):
			rep.Text = "У вас уже идёт выбор — сначала завершите его."
		case err != nil:
			return rep, err
		default:
			rep.Text = "Отправил вам обоим: осталось отметить время."
		}
	case "book":
		_, err := as.App.BookFixed(ctx, c, p.IdeaID)
		if errors.Is(err, app.ErrNoTime) {
			if _, err = as.App.GoWith(ctx, c, p.IdeaID); err == nil {
				rep.Text = "У события нет точного времени — отправил вам обоим выбрать, когда."
				break
			}
		}
		switch {
		case errors.Is(err, app.ErrNotPaired):
			rep.Text = "Сначала пригласи партнёра."
		case errors.Is(err, app.ErrRoundOpen):
			rep.Text = "У вас уже идёт выбор — сначала завершите его."
		case err != nil:
			return rep, err
		default:
			rep.Text = "Позвал " + app.NameAcc(c.PartnerName()) + ". Как только подтвердит — событие придёт вам обоим в календарь."
		}
	case "round":
		if _, err := as.App.Go(ctx, c); errors.Is(err, app.ErrNotPaired) {
			rep.Text = "Сначала пригласи партнёра."
		} else if err != nil {
			return rep, err
		} else {
			rep.Text = "Начали выбор — отметьте время."
		}
	case "wish":
		if _, err := as.App.SetWish(ctx, c, p.IdeaID, domain.Wish(p.Wish)); err != nil {
			return rep, err
		}
		rep.Text = "Отметил."
	case "budget":
		sp := c.Space
		sp.BudgetKop = p.Rub * 100
		if err := as.App.S.UpdateSpace(ctx, sp); err != nil {
			return rep, err
		}
		rep.Text = "Бюджет обновлён."
	case "suggest_mode":
		sp := c.Space
		sp.SuggestMode = p.Mode
		if err := as.App.S.UpdateSpace(ctx, sp); err != nil {
			return rep, err
		}
		rep.Text = "Готово: " + strings.ToLower(modeLabel[domain.SuggestMode(p.Mode)]) + "."
	case "open": // a button from before buttons knew their screen: say where to go
		var o struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(a.Payload, &o)
		rep.Text, rep.Open = "Открываю", o.Path
		return rep, nil
	default:
		rep.Text = "Не знаю такое действие."
	}
	_, _ = as.App.S.AddChat(ctx, c.Space.ID, c.Me.ID, "assistant", rep, as.now())
	return rep, nil
}

func randSrc(now time.Time) *mrand.Rand { return mrand.New(mrand.NewSource(now.UnixNano())) }

// Phrases: what the composer completes while typing — each one is understood without the model
var Phrases = []string{
	"Что интересного на неделе?", "Что есть на выходных?", "Куда сходить в субботу вечером?", "Куда сходить в пятницу вечером?",
	"Куда сходить сегодня вечером?", "Куда сходить завтра?", "Хотим почилить", "Хотим что-нибудь активное", "Что-нибудь романтичное",
	"Что-нибудь до 3000 ₽", "Что-нибудь бесплатное", "Удиви нас", "Что нашёл скаут?", "Что у нас запланировано?", "Где мы были?",
	"Анонсы на пару месяцев", "Какие дела по дому?", "Поставь бюджет 10000 ₽ в месяц", "Запомни, что я не люблю стендап",
	"Следи за билетами на ", "Сколько стоит ", "Расскажи про ", "Запиши нас на ", "Добавь в копилку ",
	"Театр на выходных", "Концерт на неделе", "Выставка в воскресенье", "Ресторан в пятницу вечером", "Прогулка в субботу днём",
}

// Hints: phrases plus the titles the couple can ask about (the bank and the afisha), for completion while typing
func (as *Assistant) Hints(ctx context.Context, c app.Couple) (map[string]any, error) {
	ideas, err := as.App.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDraft)
	if err != nil {
		return nil, err
	}
	now := as.now()
	titles := []string{}
	seen := map[string]bool{}
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure || i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Before(now.Add(-24*time.Hour)) {
			continue
		}
		k := strings.ToLower(i.Title)
		if !seen[k] && len(titles) < 300 {
			seen[k] = true
			titles = append(titles, i.Title)
		}
	}
	return map[string]any{"phrases": Phrases, "titles": titles}, nil
}
