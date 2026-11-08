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
	"sync/atomic"
	"time"

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
}

type Button struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

type Reply struct {
	Text    string   `json:"text"`
	Cards   []Card   `json:"cards,omitempty"`
	Actions []Button `json:"actions,omitempty"`
	Intent  *Intent  `json:"intent,omitempty"` // how the request was understood — for follow-ups («а подешевле?»)
	Src     string   `json:"src,omitempty"`    // rules | model | rules-fallback
	Ms      int64    `json:"ms,omitempty"`
}

type Assistant struct {
	App    *app.App
	AI     *ai.Client
	cache  bankCache
	intent atomic.Pointer[ai.Client] // a model fine-tuned for understanding requests (vdvoem-*), if installed
}

// Tuned: model names starting with «vdvoem-» were fine-tuned on the intent dataset: they get the short prompt
// (no examples) — what they were trained on, and faster.
func Tuned(model string) bool { return strings.HasPrefix(model, "vdvoem-") }

// SetIntentModel switches the model that reads requests (empty — back to the general one).
func (as *Assistant) SetIntentModel(model string) {
	if model == "" || as.AI == nil {
		as.intent.Store(nil)
		return
	}
	c := *as.AI
	c.Model = model
	as.intent.Store(&c)
}

// IntentModel: what reads requests now
func (as *Assistant) IntentModel() *ai.Client {
	if c := as.intent.Load(); c != nil {
		return c
	}
	return as.AI
}

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
	in, src := as.Understand(ctx, text, lastIntent(prev, as.now()), as.now().In(c.Loc()))
	rep := run.do(ctx, in, text)
	rep.Intent, rep.Src, rep.Ms = &in, src, time.Since(start).Milliseconds()
	if in.I != "chat" { // a labelled example for the model: accepted once a button from it is pressed
		b, _ := json.Marshal(in)
		if id, err := as.App.S.AddExample(ctx, c.Me.ID, text, "intent:"+string(b), rep.Text, as.now()); err == nil {
			for _, aid := range run.pending {
				_ = as.App.S.TagAction(ctx, aid, id)
			}
		}
	}
	as.App.Log.Info("assistant", "intent", in.I, "src", src, "ms", rep.Ms)
	rep.Cards, rep.Actions = run.cards, run.buttons
	if rep.Text == "" {
		rep.Text = "Готово."
	}
	_, _ = as.App.S.AddChat(ctx, c.Space.ID, c.Me.ID, "assistant", rep, as.now())
	return rep, nil
}

func (as *Assistant) hello(c app.Couple) Reply {
	return Reply{Text: "Я помогаю планировать вам двоим. Спроси, например:\n• Куда сходить в субботу вечером до 4000 ₽?\n• Удиви нас\n• Что у нас запланировано?\n• Где мы были в последнее время?\n• Добавь: каток в Парке Горького"}
}

// the previous request's meaning, if the last answer is recent enough to be followed up
func lastIntent(prev []store.ChatMsg, now time.Time) Intent {
	for k := len(prev) - 1; k >= 0; k-- {
		if prev[k].Role != "assistant" {
			continue
		}
		if now.Sub(prev[k].CreatedAt) > 2*time.Hour {
			return Intent{}
		}
		var r Reply
		if json.Unmarshal(prev[k].Body, &r) == nil && r.Intent != nil {
			return *r.Intent
		}
		return Intent{}
	}
	return Intent{}
}

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
	r.buttons = append(r.buttons, Button{ID: act.ID, Label: label, Kind: kind})
	r.pending = append(r.pending, act.ID)
	return map[string]string{"status": "button shown to the user; nothing changed yet"}
}

func (r *run) addCard(cd Card) {
	for _, x := range r.cards {
		if x.Type == cd.Type && x.IdeaID == cd.IdeaID && x.RoundID == cd.RoundID {
			return
		}
	}
	if len(r.cards) < 6 {
		r.cards = append(r.cards, cd)
	}
}

func ideaCard(i store.Idea, why string) Card {
	return Card{Type: "idea", IdeaID: i.ID, Title: i.Title, Meta: app.IdeaMeta(i), Image: i.ImageURL, Category: i.Category, Why: why}
}

func (r *run) myWish(i store.Idea) (me, partner domain.Wish) {
	if r.c.Me.Color == "b" {
		return i.WishB, i.WishA
	}
	return i.WishA, i.WishB
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
	ideas, _ := s.Ideas(ctx, r.c.Space.ID, domain.IdeaActive)
	in := domain.PickInput{BudgetKop: r.c.Space.BudgetKop, Now: r.as.now()}
	if budgetRub > 0 {
		in.BudgetKop = budgetRub * 100
	}
	for _, i := range ideas {
		if cat != "" && i.Category != cat {
			continue
		}
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
	if a.Example != 0 { // the model's proposal was good enough to press: a training example
		_ = as.App.S.AcceptExample(ctx, a.Example)
	}
	var rep Reply
	switch a.Kind {
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
	default:
		rep.Text = "Не знаю такое действие."
	}
	_, _ = as.App.S.AddChat(ctx, c.Space.ID, c.Me.ID, "assistant", rep, as.now())
	return rep, nil
}

func randSrc(now time.Time) *mrand.Rand { return mrand.New(mrand.NewSource(now.UnixNano())) }
