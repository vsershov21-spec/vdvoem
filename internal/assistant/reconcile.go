package assistant

import (
	"strconv"
	"strings"
)

// reconcile: the model picks the kind of request; every detail it adds must be backed by the words of
// the request, and what the rules read reliably (days, prices, categories, moods) fills the gaps.
// Measured on the eval: the small model defaults to «suggest random», invents days and parts of the day,
// and calls a question about plans «plan» — each such slip is caught here.
func reconcile(m, rule Intent, text string) Intent { return reconcileWith(m, rule, text, false) }

// reconcileTuned: for a fine-tuned model its choice of the kind of request stands (unless words contradict it);
// the details still have to be in the words, and the rules fill what it missed.
func reconcileTuned(m, rule Intent, text string) Intent { return reconcileWith(m, rule, text, true) }

func reconcileWith(m, rule Intent, text string, tuned bool) Intent {
	t := strings.ToLower(text)
	backed := map[string]bool{
		"random":   has(t, "удиви", "рандом", "случайн", "наугад", "сюрприз", "что выпадет", "любое", "неожидан", "придумай"),
		"mode":     has(t, "присыла", "предлагай", "перестань", "режим", "раз в неделю", "шли "),
		"plan":     has(t, "давай выберем", "выбрать вместе", "вместе выберем", "давай запланируем", "запланируй", "начни выбор", "выберем куда", "назначим", "голосован"),
		"overview": has(t, "план", "встреч", "запланир", "идём на этой", "идем на этой", "впереди", "ближайш"),
		"past":     has(t, "были", "ходили", "побыва", "прошл", "история", "раньше"),
		"remember": has(t, "люб", "нрав", "обожа", "терпеть", "ненавиж", "не ем", "не пью", "аллерг", "вегетариан", "веган", "предпочита", "запомни", "учти", "не переношу"),
		"book":     has(t, "запиш", "календар", "позови", "бронир", "броню", "идём на", "идем на", "пойдём на", "пойдем на", "берём", "берем", "внеси"),
		"goal":     has(t, "следи", "дай знать", "сообщи", "держи в курсе", "напомни когда", "когда появ", "когда откро", "когда приедет", "когда будут", "когда будет", "маякни", "мониторь", "отследи"),
		"add":      has(t, "добав", "сохрани", "занеси", "в копилку"),
		"budget":   has(t, "бюджет", "лимит"),
		"chores":   has(t, "дела", "убор", "мусор", "посуд", "готовит", "стирк", "по дому"),
	}
	if rule.conf >= 0.8 && !tuned {
		return rule // the pipeline never asks the model then; kept here so the function is the whole decision
	}
	specific := map[string]bool{"remember": true, "goal": true, "book": true, "suggest_mode": true, "chores": true, "past": true, "overview": true, "add_idea": true, "price": true, "about": true, "budget": true}
	if !tuned && (m.I == "find" || m.I == "suggest" || m.I == "chat") && specific[rule.I] && rule.conf >= 0.5 {
		return rule
	}
	if (m.I == "find" || m.I == "suggest") && backed["remember"] && !has(t, "?", "куда", "что ", "где", "найди", "покажи", "хоч") && memoryish(t) {
		return Intent{I: "remember", Text: strings.TrimSpace(text)}
	}
	if (m.I == "find" || m.I == "suggest") && backed["goal"] {
		return Intent{I: "goal", Text: strings.TrimSpace(text)}
	}
	if (m.I == "find" || m.I == "suggest") && has(t, "позови", "позвать") {
		if k := strings.Index(t, " на "); k >= 0 {
			return Intent{I: "book", Q: bookQ(text[k+len(" на "):])}
		}
	}
	if m.I == "suggest" && backed["mode"] {
		mode := "mix"
		switch {
		case has(t, "не присылай", "перестань", "выключи"):
			mode = "off"
		case has(t, "случайн", "рандом"):
			mode = "random"
		case has(t, "по оценк", "лучш"):
			mode = "best"
		}
		return Intent{I: "suggest_mode", Mode: mode}
	}
	reject := false
	switch m.I {
	case "suggest_mode":
		reject = !backed["mode"]
		if !reject {
			switch {
			case has(t, "не присылай", "перестань", "выключи"):
				m.Mode = "off"
			case has(t, "случайн", "рандом"):
				m.Mode = "random"
			case has(t, "по оценк", "лучш"):
				m.Mode = "best"
			}
		}
	case "plan":
		if !backed["plan"] {
			if backed["overview"] {
				m = Intent{I: "overview"}
			} else {
				reject = true
			}
		}
	case "past":
		if !backed["past"] {
			if backed["overview"] {
				m = Intent{I: "overview"}
			} else {
				reject = true
			}
		}
	case "remember":
		reject = !backed["remember"] || backed["add"]
	case "book":
		reject = !backed["book"]
	case "goal":
		reject = !backed["goal"] && rule.I != "goal"
	case "budget":
		if m.Rub == 0 {
			m.Rub = firstNumber(m.Text + " " + text)
		}
		reject = !backed["budget"] || m.Rub <= 0
	case "chat":
		if rule.I != "chat" && rule.conf >= 0.5 && !(tuned && isGreeting(text)) {
			reject = true
		}
	case "overview":
		reject = !backed["overview"] && !has(t, "что мы делаем", "что делаем", "куда идём", "куда идем")
	}
	if !reject && m.I != "chores" && backed["chores"] && rule.I == "chores" {
		reject = true
	}
	if !reject && backed["add"] && rule.I == "add_idea" {
		reject = true
	}
	if reject {
		return rule
	}
	if m.I == "suggest" {
		if m.Mode == "random" && (!backed["random"] || !tuned && (len(rule.Moods) > 0 || len(rule.Cats) > 0)) {
			m.Mode = "best"
		}
		if m.Mode != "random" && backed["random"] && len(rule.Moods) == 0 && len(rule.Cats) == 0 {
			m.Mode = "random"
		}
		if m.Mode != "random" && !(tuned && rule.I == "chat") && (len(rule.Moods) > 0 || len(rule.Cats) > 0 || rule.Date != "" || rule.Wknd || rule.Part != "" || rule.Max > 0 || len(m.Moods) > 0) {
			m.I = "find" // a request with conditions is a search, not one pick
		}
	}
	if m.I == "find" || m.I == "suggest" {
		m = slots(m, rule, t)
	}
	return m
}

// slots: keep what the text supports, take the rest from the rules
func slots(m, rule Intent, t string) Intent {
	dayWords := has(t, "сегодня", "завтра", "послезавтра", "понедельн", "вторник", "сред", "четверг", "пятниц", "суббот", "воскрес", "выходн", ".", "числа")
	if rule.Date != "" || rule.Wknd {
		m.Date, m.Wknd = rule.Date, rule.Wknd
	} else if !dayWords {
		m.Date, m.Wknd = "", false
	}
	if !has(t, "выходн") && rule.Date == "" {
		m.Wknd = false
	}
	if rule.Part != "" {
		m.Part = rule.Part
	} else if !has(t, "утр", "днём", "днем", "обед", "вечер", "ночь", "ночью", "ужин") {
		m.Part = ""
	}
	if rule.Max > 0 {
		m.Max = rule.Max
	} else if m.Max > 0 && firstNumber(t) == 0 && !has(t, "недорог", "дешев", "бюджетн", "тысяч") {
		m.Max = 0
	}
	if len(rule.Cats) > 0 {
		m.Cats = rule.Cats
	} else {
		m.Cats = keepBacked(m.Cats, t, map[string][]string{"food": {"поесть", "еда", "ужин", "обед", "кухн", "ресторан", "кафе", "бар", "вин", "бранч", "завтрак"},
			"culture": {"театр", "спектак", "музе", "выстав", "концерт", "кино", "опер", "балет", "музык", "премьер", "культур", "филармон"},
			"walk":    {"гуля", "прогул", "парк", "природ", "воздух", "набережн"}, "sport": {"спорт", "каток", "лыж", "бассейн", "актив"}})
	}
	m.Not = uniq(append(m.Not, rule.Not...))
	m.NotW = uniq(append(rule.NotW, m.NotW...))
	m.Moods = uniq(append(rule.Moods, m.Moods...))
	if len(rule.Moods) > 0 { // the rules found the mood in the words; the model's extra ones are guesses
		m.Moods = rule.Moods
	}
	if rule.Hz != "" {
		m.Hz = rule.Hz
	} else if m.Hz == "later" && !has(t, "анонс", "через", "месяц", "весн", "лет", "зим", "осен", "январ", "феврал", "март", "апрел", "декабр", "новый год") {
		m.Hz = ""
	}
	m.Scout = rule.Scout
	if m.Q != "" && !strings.Contains(t, strings.ToLower(firstWord(m.Q))) {
		m.Q = rule.Q // a word the person did not say is not a search term
	}
	return m
}

// memoryish: a statement about oneself, not a request («обожаю грузинскую кухню», «у меня аллергия на кошек»)
func memoryish(t string) bool {
	return has(t, "я ", "мы ", "мне ", "нам ", "у меня", "у нас", "обожа", "терпеть", "не люблю", "люблю", "ненавиж")
}

func keepBacked(cats []string, t string, words map[string][]string) []string {
	var out []string
	for _, c := range cats {
		if has(t, words[c]...) {
			out = append(out, c)
		}
	}
	return out
}

func uniq(l []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range l {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	r := []rune(f[0])
	if len(r) > 5 {
		r = r[:len(r)-2]
	}
	return string(r)
}

func firstNumber(s string) int64 {
	cur := ""
	for _, r := range s + " " {
		if r >= '0' && r <= '9' {
			cur += string(r)
			continue
		}
		if r == ' ' && cur != "" && len(cur) < 3 {
			continue // «7 000»
		}
		if len(cur) >= 3 {
			n, _ := strconv.ParseInt(cur, 10, 64)
			return n
		}
		cur = ""
	}
	return 0
}
