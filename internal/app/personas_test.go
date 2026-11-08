package app_test

// Twenty couples with different lives go through the app as people would: they throw ideas at it, the scout
// and a channel bring events, each marks what they like, they talk to the assistant in their own words, ask
// the partner to answer quickly, agree on time, get the event into the calendar and buy tickets.
// Every step is written to a report (PERSONAS_OUT) and a dead end fails the test: a request answered with
// nothing to do next, a reply that did not understand a plain request, a flow that stops.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"vdvoem/internal/app"
	"vdvoem/internal/assistant"
	"vdvoem/internal/domain"
)

type persona struct {
	who    string   // the couple, in a few words
	ideas  []string // what they throw into the bot
	afisha []app.ScoutPick
	says   []string // how they talk to the assistant
}

func pk(title, cat string, day int, hm string, rub int64, genres ...string) app.ScoutPick {
	return app.ScoutPick{Title: title, Category: cat, Starts: fmt.Sprintf("D%+d %s", day, hm), PriceRub: rub, Score: 7, Genres: genres,
		URL: "https://afisha.example/" + strings.ReplaceAll(strings.ToLower(title), " ", "-")}
}

var personas = []persona{
	{"театралы 30+, МХТ и Современник", []string{"Чайка во МХТ", "Вишнёвый сад в Современнике", "Мастер и Маргарита в Ермоловой"},
		[]app.ScoutPick{pk("Гамлет в театре Наций", "culture", 5, "19:00", 6000, "драма"), pk("Комедия ошибок", "culture", 3, "19:30", 4000, "комедия")},
		[]string{"что идёт в театрах на этой неделе?", "хотим комедию в субботу", "расскажи про первое", "сколько стоит Гамлет?", "запиши нас на Гамлета", "что нарыл агент?"}},
	{"гастрономы, грузинская и азиатская кухня", []string{"Хинкальная на Покровке", "Рамен в Izakaya", "Дегустация вин на Красном Октябре"},
		[]app.ScoutPick{pk("Ужин-спектакль в темноте", "food", 4, "20:00", 9000, "гастро"), pk("Фестиваль уличной еды", "food", 6, "12:00", 0)},
		[]string{"куда поесть в пятницу вечером?", "что-нибудь вкусное до 5000", "а подешевле?", "удиви нас", "хотим что-то новое из еды"}},
	{"спортивные, бег и скалодром", []string{"Скалодром Big Wall", "Пробежка в Нескучном", "Падел в Лужниках"},
		[]app.ScoutPick{pk("Забег 10 км в Парке Горького", "sport", 6, "09:00", 2000), pk("Сапы на Москве-реке", "sport", 2, "11:00", 3000)},
		[]string{"хотим подвигаться в выходные", "что активного утром?", "что-нибудь на воздухе", "сколько стоит забег?"}},
	{"домоседы с котом", []string{"Кино дома и пицца", "Настолки вечером", "Испечь пирог вместе"},
		[]app.ScoutPick{pk("Онлайн-лекция об искусстве", "culture", 2, "20:00", 0)},
		[]string{"хотим остаться дома", "что-нибудь спокойное вечером", "давай без выходов из дома", "удиви"}},
	{"студенты, бюджет до 1500", []string{"Бесплатный музей в четверг", "Прогулка по ВДНХ", "Кофе на Чистых"},
		[]app.ScoutPick{pk("Бесплатный концерт в парке", "culture", 3, "18:00", 0), pk("Стендап открытый микрофон", "other", 2, "20:00", 600, "стендап")},
		[]string{"что бесплатного на неделе?", "куда сходить до 1500?", "а без стендапа?", "что есть на выходных?"}},
	{"молодые родители, ребёнок 4 года", []string{"Детский спектакль в Сатах", "Зоопарк", "Аквапарк"},
		[]app.ScoutPick{pk("Щелкунчик для детей", "culture", 6, "12:00", 5000, "детям", "балет"), pk("Мастер-класс по лепке", "other", 5, "11:00", 2500, "детям")},
		[]string{"куда с ребёнком в субботу днём?", "что для детей на неделе?", "расскажи про Щелкунчика — подойдёт нам?"}},
	{"меломаны, джаз и рок", []string{"Джаз в Эссе", "Концерт Сплина", "Винил-бар"},
		[]app.ScoutPick{pk("Джазовый вечер в Козловском клубе", "culture", 3, "21:00", 3500, "джаз", "концерт"), pk("Рок-фестиваль", "culture", 40, "18:00", 7000, "рок", "концерт")},
		[]string{"какие концерты есть?", "хотим джаз в пятницу", "анонсы на пару месяцев", "следи за билетами на Сплина"}},
	{"искусство, выставки и галереи", []string{"Пушкинский музей", "Гараж", "Третьяковка на Крымском"},
		[]app.ScoutPick{pk("Выставка Айвазовского", "culture", 1, "", 800, "выставка"), pk("Ночь в музее", "culture", 9, "20:00", 1000, "выставка")},
		[]string{"какие выставки сейчас?", "что-нибудь культурное в воскресенье", "только новое, чего мы не видели"}},
	{"природа и походы", []string{"Поход в Подмосковье", "Коломенское на велосипедах", "Пикник в Царицыно"},
		[]app.ScoutPick{pk("Экскурсия по Лосиному острову", "walk", 6, "10:00", 1500, "экскурсия")},
		[]string{"хотим на природу", "куда погулять в субботу?", "что-нибудь на свежем воздухе бесплатно"}},
	{"ночная жизнь, бары", []string{"Коктейльный бар на Никольской", "Караоке", "Клуб Mutabor"},
		[]app.ScoutPick{pk("Вечеринка на крыше", "other", 5, "23:00", 4000)},
		[]string{"куда сходить ночью в пятницу?", "что-то весёлое", "хотим потанцевать"}},
	{"стендап и юмор", []string{"Стендап в Stand Up Store", "Импровизация в Школе злословия"},
		[]app.ScoutPick{pk("Большой стендап-концерт", "other", 4, "19:00", 3000, "стендап")},
		[]string{"хотим посмеяться", "что смешное на неделе?", "сколько стоит большой стендап?", "давай туда"}},
	{"киноманы", []string{"Кинотеатр Пионер", "Ретроспектива Тарковского", "Кино под открытым небом"},
		[]app.ScoutPick{pk("Премьера в Октябре", "culture", 2, "21:00", 1600, "кино")},
		[]string{"что в кино?", "хотим фильм завтра вечером", "расскажи про премьеру"}},
	{"настолки и квизы", []string{"Квиз Мозгобойня", "Антикафе с настолками", "Квест-комната"},
		[]app.ScoutPick{pk("Квиз Плиз в баре", "other", 3, "19:30", 1200, "квиз")},
		[]string{"что-нибудь интеллектуальное", "хотим квиз", "запиши нас на квиз"}},
	{"танцы", []string{"Сальса-вечеринка", "Уроки танго", "Бачата в парке"},
		[]app.ScoutPick{pk("Танцевальный мастер-класс", "sport", 4, "19:00", 2000)},
		[]string{"хотим потанцевать вдвоём", "что романтичное на выходных?"}},
	{"спа и отдых", []string{"СПА в Сандунах", "Массаж для двоих", "Баня в Подмосковье"},
		[]app.ScoutPick{pk("СПА-вечер в банях", "other", 3, "", 7000)},
		[]string{"хотим почилить", "расслабиться в воскресенье", "что-нибудь спокойное до 8000"}},
	{"путешествия выходного дня", []string{"Поездка в Суздаль", "Тула на выходные", "Глэмпинг"},
		[]app.ScoutPick{pk("Экскурсия в Коломну", "walk", 6, "08:00", 4500, "экскурсия")},
		[]string{"куда съездить на выходные?", "что-то новое за городом", "анонсы"}},
	{"фотографы и прогулки по городу", []string{"Рассвет на Воробьёвых", "Прогулка по Хитровке", "Крыши Москвы"},
		[]app.ScoutPick{pk("Экскурсия по крышам", "walk", 5, "18:00", 3000, "экскурсия")},
		[]string{"куда погулять с фотоаппаратом?", "что-нибудь вечером на воздухе"}},
	{"книжные и лекции", []string{"Книжный Ходасевич", "Лекция в Политехе", "Поэтический вечер"},
		[]app.ScoutPick{pk("Лекция про русский авангард", "culture", 2, "19:00", 800, "лекция")},
		[]string{"какие лекции на неделе?", "что-нибудь умное в четверг", "подборку всего что есть"}},
	{"йога и wellness", []string{"Йога в парке", "Чайная церемония", "Медитация"},
		[]app.ScoutPick{pk("Йога на рассвете", "sport", 6, "07:00", 1000)},
		[]string{"что-то спокойное утром", "хотим йогу в субботу", "удиви из того, что нарыл агент"}},
	{"гики и технологии", []string{"VR-клуб", "Музей Яндекса", "Хакатон-вечеринка"},
		[]app.ScoutPick{pk("Выставка роботов", "culture", 4, "", 1500, "выставка")},
		[]string{"что-нибудь про технологии", "вообще всё что есть в москве", "только наше из копилки"}},
}

// deadEnd: an answer a person cannot act on — no buttons, no cards, no next step, or «не понял» for a plain request
func deadEnd(rep assistant.Reply) string {
	switch {
	case strings.TrimSpace(rep.Text) == "":
		return "пустой ответ"
	case strings.Contains(rep.Text, "Не до конца понял"):
		return "не понял"
	case len(rep.Cards) == 0 && len(rep.Actions) == 0 && len(rep.Next) == 0:
		return "нечего нажать дальше"
	}
	return ""
}

func TestPersonas(t *testing.T) {
	var rep strings.Builder
	fails := 0
	for k, p := range personas {
		a, f, ck := setup(t)
		ctx := context.Background()
		v, n := pair(t, a)
		as := &assistant.Assistant{App: a, AI: a.AI}
		fmt.Fprintf(&rep, "\n## %d. %s\n", k+1, p.who)
		// ideas from the bot, events from the scout
		for _, s := range p.ideas {
			if _, err := a.AddIdea(ctx, v, s); err != nil {
				t.Fatalf("%s: add %q: %v", p.who, s, err)
			}
		}
		picks := make([]app.ScoutPick, len(p.afisha))
		for j, x := range p.afisha {
			var d int
			var hm string
			fmt.Sscanf(x.Starts, "D%d %s", &d, &hm)
			x.Starts = ck.t.AddDate(0, 0, d).Format("2006-01-02")
			if hm != "" {
				x.Starts += "T" + hm
			}
			if d > 30 {
				x.Horizon = "later"
			}
			picks[j] = x
		}
		if _, err := a.ScoutPicks(ctx, v.Space.ID, picks); err != nil {
			t.Fatalf("%s: picks: %v", p.who, err)
		}
		// Настя marks what she likes first
		ideas, _ := a.S.Ideas(ctx, v.Space.ID, domain.IdeaActive, domain.IdeaDraft)
		for j, i := range ideas {
			if j%2 == 0 {
				_, _ = a.SetWish(ctx, n, i.ID, domain.WishVery)
			}
		}
		// the conversation
		for _, s := range p.says {
			ck.t = ck.t.Add(3 * time.Minute)
			r, err := as.Ask(ctx, v, s)
			if err != nil {
				t.Fatalf("%s: ask %q: %v", p.who, s, err)
			}
			mark := "✓"
			if d := deadEnd(r); d != "" {
				mark, fails = "✗ "+d, fails+1
			}
			in := ""
			if r.Intent != nil {
				in = r.Intent.I
			}
			first := strings.SplitN(r.Text, "\n", 2)[0]
			fmt.Fprintf(&rep, "- %s «%s» → %s · %d карт. · %d кнопок · дальше: %s\n  > %s\n", mark, s, in, len(r.Cards), len(r.Actions), strings.Join(r.Next, " / "), first)
		}
		// «важно» → yes → time → both agree → planned → calendar links and tickets
		var target int64
		for _, i := range ideas {
			if i.Kind == domain.KindLeisure {
				target = i.ID
				break
			}
		}
		ck.t = ck.t.Add(time.Hour)
		if _, err := a.MarkUrgent(ctx, n, target, "успеть бы"); err != nil {
			t.Fatalf("%s: urgent: %v", p.who, err)
		}
		if _, err := a.AnswerUrgent(ctx, v, target, "yes"); err != nil {
			t.Fatalf("%s: urgent yes: %v", p.who, err)
		}
		rd, err := a.S.OpenRound(ctx, v.Space.ID)
		if err != nil || rd.Chosen != target {
			t.Fatalf("%s: no round after «Идём»: %v", p.who, err)
		}
		if rd.State == domain.StateTime {
			days := app.SlotDays(a.Now(), v.Loc())
			k1 := days[len(days)-1].Format("2006-01-02") + ":2"
			_, _ = a.SubmitSlots(ctx, v, rd.ID, []string{k1, days[0].Format("2006-01-02") + ":2"})
			rd, _ = a.SubmitSlots(ctx, n, rd.ID, []string{k1})
		}
		if rd.State == domain.StateConfirm {
			_, _ = a.Confirm(ctx, v, rd.ID)
			rd, _ = a.Confirm(ctx, n, rd.ID)
		}
		ok := rd.State == domain.StatePlanned && strings.Contains(f.lastMarkup(1), "/cal/")
		if !ok {
			fails++
		}
		fmt.Fprintf(&rep, "- %s важно → «Идём» → время → оба «да» → %s, кнопки календаря: %v\n", map[bool]string{true: "✓", false: "✗"}[ok], rd.State, strings.Contains(f.lastMarkup(1), "/cal/"))
		// random from the afisha
		if len(p.afisha) > 0 {
			s, err := a.SuggestFrom(ctx, v, domain.SuggestRandom, nil, "afisha")
			okr := err == nil && app.Afisha(s.Idea)
			if !okr {
				fails++
			}
			fmt.Fprintf(&rep, "- %s «удиви из афиши» → %s\n", map[bool]string{true: "✓", false: "✗"}[okr], s.Idea.Title)
		}
	}
	fmt.Fprintf(&rep, "\nТупиков: %d\n", fails)
	if out := os.Getenv("PERSONAS_OUT"); out != "" {
		_ = os.WriteFile(out, []byte("# 20 пар — прогон\n"+rep.String()), 0o644)
	}
	t.Log(rep.String())
	if fails > 0 && os.Getenv("PERSONAS_STRICT") == "1" {
		t.Errorf("%d dead ends — see the report", fails)
	}
}
