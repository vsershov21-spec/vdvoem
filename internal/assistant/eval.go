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

// Eval measures how well a model turns requests into the right tool call — the part that matters:
// the server does the rest. `vdvoem eval` runs it against AI_URL/AI_MODEL and prints a report.

type evalCase struct {
	Q     string
	Tool  string
	Check func(args map[string]any) bool
}

func arr(a map[string]any, k string) []string {
	var out []string
	if l, ok := a[k].([]any); ok {
		for _, x := range l {
			out = append(out, fmt.Sprint(x))
		}
	}
	return out
}
func hasV(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
func numV(a map[string]any, k string) float64 { f, _ := a[k].(float64); return f }
func strV(a map[string]any, k string) string  { s, _ := a[k].(string); return s }
func any_(a map[string]any) bool              { return true }

var evalCases = []evalCase{
	{"Что интересного на этих выходных?", "afisha", func(a map[string]any) bool { return a["weekend"] == true || strV(a, "date") != "" }},
	{"Что есть в субботу вечером до 3000?", "afisha", func(a map[string]any) bool { return numV(a, "max_price_rub") == 3000 && strV(a, "part") == "evening" }},
	{"А что-нибудь без театра?", "afisha", func(a map[string]any) bool {
		return hasV(arr(a, "exclude_categories"), "culture") || len(arr(a, "exclude_words")) > 0
	}},
	{"Покажи анонсы концертов на ближайшие месяцы", "afisha", func(a map[string]any) bool { return strV(a, "horizon") == "later" }},
	{"Куда сходить поесть в пятницу?", "afisha", func(a map[string]any) bool { return hasV(arr(a, "categories"), "food") }},
	{"Подешевле есть что-то?", "afisha", func(a map[string]any) bool { return numV(a, "max_price_rub") > 0 && numV(a, "max_price_rub") <= 3000 }},
	{"Что нашёл скаут на неделю?", "afisha", func(a map[string]any) bool { return strV(a, "horizon") != "later" }},
	{"Есть что-то на свежем воздухе в воскресенье днём?", "afisha", func(a map[string]any) bool { return hasV(arr(a, "categories"), "walk") || strV(a, "part") == "day" }},
	{"Хочу на выставку, но не дороже 1500", "afisha", func(a map[string]any) bool { return numV(a, "max_price_rub") == 1500 }},
	{"Какие спектакли есть в январе?", "afisha", func(a map[string]any) bool { return strV(a, "horizon") == "later" || strV(a, "query") != "" }},
	{"Запомни, что я не люблю стендап", "remember", func(a map[string]any) bool {
		return strings.Contains(strings.ToLower(strV(a, "text")), "стендап")
	}},
	{"Я не ем мясо, учитывай", "remember", func(a map[string]any) bool { return strings.Contains(strings.ToLower(strV(a, "text")), "мяс") }},
	{"По пятницам мы обычно устаём, лучше не предлагай", "remember", func(a map[string]any) bool { return strings.Contains(strings.ToLower(strV(a, "text")), "пятниц") }},
	{"Хотим сходить в театр в январе — следи за этим", "add_goal", func(a map[string]any) bool { return strings.Contains(strings.ToLower(strV(a, "text")), "театр") }},
	{"Дай знать, когда приедет Земфира", "add_goal", func(a map[string]any) bool { return strings.Contains(strings.ToLower(strV(a, "text")), "земфир") }},
	{"Следи за премьерами в Большом", "add_goal", func(a map[string]any) bool { return strings.Contains(strings.ToLower(strV(a, "text")), "больш") }},
	{"Удиви нас", "suggest", func(a map[string]any) bool { return strV(a, "mode") == "random" }},
	{"Что посоветуешь из нашей копилки на вечер?", "suggest", any_},
	{"Что у нас запланировано?", "overview", any_},
	{"Когда ближайшая встреча?", "overview", any_},
	{"Где мы были в последнее время?", "past_meetings", any_},
	{"Какие у нас дела по дому?", "chores", any_},
	{"Добавь каток в Парке Горького", "propose_add_idea", func(a map[string]any) bool { return strings.Contains(strings.ToLower(strV(a, "text")), "каток") }},
	{"Давай поставим бюджет 5000", "propose_budget", func(a map[string]any) bool { return numV(a, "rub") == 5000 }},
	{"Пусть бот присылает случайные идеи", "propose_suggest_mode", func(a map[string]any) bool { return strV(a, "mode") == "random" }},
	{"Давай выберем, куда пойти вместе", "propose_round", any_},
	{"Есть в копилке что-то грузинское?", "find_ideas", func(a map[string]any) bool {
		return strings.Contains(strings.ToLower(strV(a, "query")), "грузин")
	}},
	{"Что мы оба хотим?", "find_ideas", func(a map[string]any) bool { return strV(a, "wished_by") == "both" }},
	{"Хочу в кино в воскресенье вечером", "afisha", func(a map[string]any) bool { return strV(a, "part") == "evening" }},
	{"Найди что-нибудь романтичное до 5000 на выходных", "afisha", func(a map[string]any) bool { return numV(a, "max_price_rub") == 5000 }},
}

const evalSystem = `Ты — помощник в приложении «Вдвоём» для пары: Вова и Настя. Сейчас среда, 11 ноября 2026, 12:00, Москва. Пишет Вова.
Все данные бери только из инструментов. Про афишу, события, выходные, анонсы — afisha; «подешевле», «без театра», «только суббота» — фильтры afisha.
Подбор из копилки — suggest («удиви» — mode=random). Если человек сообщает о своём вкусе — remember. Если просит следить за чем-то в будущем — add_goal.
Изменить что-то ты не можешь, только предложить кнопкой через propose_*. Отвечай по-русски.`

type EvalResult struct {
	Q, Want, Got string
	OK           bool
	Ms           int64
	Args         string
}

// Eval runs every case once and writes a markdown report; returns the share of correct calls.
func Eval(ctx context.Context, c *ai.Client, w io.Writer) float64 {
	var rs []EvalResult
	ok := 0
	var total time.Duration
	for _, tc := range evalCases {
		t0 := time.Now()
		turn, err := c.Step(ctx, "eval", []ai.Message{{Role: "system", Content: evalSystem}, {Role: "user", Content: tc.Q}}, Tools)
		el := time.Since(t0)
		total += el
		r := EvalResult{Q: tc.Q, Want: tc.Tool, Ms: el.Milliseconds()}
		if err != nil {
			r.Got = "ошибка: " + err.Error()
		} else if len(turn.Calls) == 0 {
			r.Got = "текст без инструмента"
		} else {
			call := turn.Calls[0]
			r.Got, r.Args = call.Name, string(call.Args)
			var a map[string]any
			_ = json.Unmarshal(call.Args, &a)
			r.OK = call.Name == tc.Tool && tc.Check(a)
		}
		if r.OK {
			ok++
		}
		rs = append(rs, r)
		fmt.Fprintf(w, "%s %4.1fs  %-22s %s\n", map[bool]string{true: "✓", false: "✗"}[r.OK], el.Seconds(), r.Got, tc.Q)
	}
	share := float64(ok) / float64(len(evalCases))
	ms := make([]int64, len(rs))
	for k, r := range rs {
		ms[k] = r.Ms
	}
	sort.Slice(ms, func(a, b int) bool { return ms[a] < ms[b] })
	fmt.Fprintf(w, "\n## %s\nВерно: %d из %d (%.0f%%) · среднее %.1f с · медиана %.1f с · худшее %.1f с\n",
		c.Model, ok, len(evalCases), share*100, total.Seconds()/float64(len(evalCases)), float64(ms[len(ms)/2])/1000, float64(ms[len(ms)-1])/1000)
	return share
}

// ExportTrain writes accepted model exchanges plus the eval cases (as gold examples) in the chat format
// LoRA trainers take: {"messages":[system,user,assistant(tool_calls)]}.
func ExportTrain(ctx context.Context, st *store.Store, w io.Writer) (int, error) {
	ex, err := st.Examples(ctx, true)
	if err != nil {
		return 0, err
	}
	n := 0
	write := func(q, calls, answer string) {
		var cl []ai.ToolCall
		_ = json.Unmarshal([]byte(calls), &cl)
		var tcs []map[string]any
		for k, c := range cl {
			tcs = append(tcs, map[string]any{"id": fmt.Sprintf("call_%d", k), "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Args)}})
		}
		msgs := []map[string]any{{"role": "system", "content": evalSystem}, {"role": "user", "content": q}}
		if len(tcs) > 0 {
			msgs = append(msgs, map[string]any{"role": "assistant", "content": "", "tool_calls": tcs})
		} else {
			msgs = append(msgs, map[string]any{"role": "assistant", "content": answer})
		}
		b, _ := json.Marshal(map[string]any{"messages": msgs})
		fmt.Fprintln(w, string(b))
		n++
	}
	for _, e := range ex {
		write(e.Input, e.Calls, e.Answer)
	}
	return n, nil
}
