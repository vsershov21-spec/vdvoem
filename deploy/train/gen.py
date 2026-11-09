#!/usr/bin/env python3
"""Dataset for teaching the local model to understand requests to «Вдвоём».

Every example is built from slots (day, part of the day, budget, categories, moods, names…), so its label is
correct by construction: the same slots produce the phrase and the JSON. Phrases vary — word order, slang,
typos, no punctuation, emoji — so the model learns the meaning, not the template.

    python3 gen.py out/        → out/train.jsonl, out/valid.jsonl, out/test.jsonl (+ stats)

Splits do not share phrasings: test uses its own templates and its own names (see TEST_*), so the score on
test says how the model handles requests it has never seen. The held-out sets from internal/assistant
(intentHoldout, intentHoldout2) are never used here — the server eval keeps them clean.
"""
import json, os, random, sys, datetime as dt

R = random.Random(20261108)
WEEK = ["понедельник", "вторник", "среда", "четверг", "пятница", "суббота", "воскресенье"]
WEEK_ACC = ["в понедельник", "во вторник", "в среду", "в четверг", "в пятницу", "в субботу", "в воскресенье"]
MONTHS_GEN = ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"]

def system(today):
    # exactly what internal/assistant.intentSystem says (kept in sync by TestDatasetMatchesSystem)
    wd = ["понедельник", "вторник", "среда", "четверг", "пятница", "суббота", "воскресенье"][today.weekday()]
    return SYSTEM.replace("{DATE}", today.isoformat()).replace("{WD}", wd)

SYSTEM = open(os.path.join(os.path.dirname(__file__), "system.txt"), encoding="utf-8").read().strip() if os.path.exists(os.path.join(os.path.dirname(__file__), "system.txt")) else ""

def nearest(today, wd):  # the nearest such weekday, today included (as the app's rules do)
    return today + dt.timedelta(days=(wd - today.weekday()) % 7)

# ---------- slot vocabularies ----------

DAYS = [  # (phrase, kind, arg)
    ("сегодня", "rel", 0), ("завтра", "rel", 1), ("послезавтра", "rel", 2),
    *[(WEEK_ACC[i], "wd", i) for i in range(7)],
    ("на выходных", "wknd", None), ("в выходные", "wknd", None), ("в эти выходные", "wknd", None), ("на выхах", "wknd", None),
]
PARTS = [("утром", "morning"), ("с утра", "morning"), ("днём", "day"), ("днем", "day"), ("в обед", "day"), ("вечером", "evening"),
         ("вечерком", "evening"), ("после работы", "evening"), ("на вечер", "evening")]
BUDGET = [("до {n}", None), ("до {n} ₽", None), ("не дороже {n}", None), ("в пределах {n}", None), ("максимум {n}", None), ("до {k} тысяч", "k"), ("до {k}к", "k")]
CATS = {
    "food": ["поесть", "поужинать", "в ресторан", "в кафе", "на бранч", "позавтракать", "перекусить", "вкусно поесть", "в бар", "на ужин"],
    "culture": ["в театр", "на спектакль", "в музей", "на выставку", "на концерт", "в кино", "в оперу", "на балет", "в филармонию"],
    "walk": ["погулять", "на прогулку", "в парк", "пройтись", "по набережной"],
    "sport": ["на каток", "в бассейн", "на скалодром", "покататься на лыжах", "поиграть в теннис", "размяться"],
    "other": ["на квиз", "в караоке", "на стендап", "поиграть в настолки", "в боулинг"],
}
CAT_NOT = {"food": ["ресторанов", "ресторана", "кафе"], "culture": ["театра", "музеев", "музея", "кино", "концертов"],
           "walk": ["прогулок"], "sport": ["спорта"], "other": ["стендапа", "караоке", "квиза"]}
NOT_WORD = {"ресторанов": "ресторан", "ресторана": "ресторан", "кафе": "кафе", "театра": "театр", "музеев": "музей", "музея": "музей",
            "кино": "кино", "концертов": "концерт", "прогулок": "прогулк", "спорта": "спорт", "стендапа": "стендап", "караоке": "караоке", "квиза": "квиз"}
MOODS = {
    "chill": ["почилить", "отдохнуть", "расслабиться", "что-то спокойное", "без напряга", "что-нибудь уютное", "выдохнуть", "лениво провести время",
              "что-то тихое", "перезагрузиться", "посидеть спокойно", "что-нибудь лёгкое", "отдохнуть от всего"],
    "active": ["подвигаться", "что-то активное", "движухи", "драйва", "размяться", "адреналина", "чего-то энергичного", "потанцевать"],
    "romantic": ["романтики", "что-то романтичное", "на свидание", "красивый вечер вдвоём", "на годовщину", "что-нибудь атмосферное"],
    "culture": ["что-то культурное", "для души", "что-нибудь умное", "приобщиться к прекрасному", "вдохновиться"],
    "fun": ["повеселиться", "поржать", "что-то весёлое", "посмеяться", "оторваться", "развлечься"],
    "food": ["вкусно поесть", "что-то вкусное", "гастро-что-нибудь"],
    "outdoor": ["на свежем воздухе", "на улице", "на природе", "подышать воздухом"],
    "home": ["дома", "не выходя из дома", "никуда не выходить", "под пледом"],
    "free": ["бесплатно", "без денег", "ничего не тратить", "на мели"],
    "new": ["что-то новое", "что-нибудь необычное", "чего мы ещё не пробовали", "что-то впервые", "что-нибудь экзотическое"],
    "night": ["ночью", "до утра", "поздно вечером, после 23"],
}
SHOWS = ["Чайка", "Вишнёвый сад", "Мастер и Маргарита", "Евгений Онегин", "Щелкунчик", "Лебединое озеро", "Мистер Икс", "Пожары", "Амадей",
         "Ревизор", "Кабала святош", "Мёртвые души", "Война и мир", "Бег", "Мадемуазель Нитуш", "Тёща с сюрпризом", "Служебный роман",
         "Король Лир", "Маскарад", "Укрощение строптивой", "Женитьба", "Дядюшкин сон", "Три сестры", "Анна Каренина", "Горе от ума",
         "Пиковая дама", "Кармен", "Жизель", "Травиата", "Ромео и Джульетта", "Двенадцать стульев", "Собачье сердце", "Отелло"]
EVENTS = ["джазовый концерт", "концерт Земфиры", "стендап в Сохо", "выставку Айвазовского", "квиз в баре", "каток в Парке Горького",
          "планетарий", "Ботанический сад", "фестиваль фейерверков", "концерт Би-2", "лекцию про космос", "мастер-класс по керамике",
          "бранч в Пушкине", "иммерсивное шоу", "органный концерт", "экскурсию по крышам"]
GOALS = ["следи за билетами на {x}", "дай знать, когда будет {x}", "сообщи, когда появятся билеты на {x}", "держи в курсе про {x}",
         "напомни, когда начнутся продажи на {x}", "следи, когда будет {x}", "скажи, когда появится {x}", "хочу попасть на {x}, следи"]
TEST_GOALS = ["мониторь {x}", "отследи для нас {x}", "как только будет {x} — маякни"]
PREFS = [  # (phrase, fact)
    "я не ем морепродукты", "мы не любим громкую музыку", "я обожаю джаз", "Настя терпеть не может хорроры", "у меня аллергия на кошек",
    "я вегетарианец", "мы любим камерные спектакли", "мне не нравятся мюзиклы", "я не пью алкоголь", "Настя любит грузинскую кухню",
    "нам нравятся небольшие уютные места", "я не люблю очереди", "мы не любим стендап", "обожаем итальянскую кухню", "я боюсь высоты",
    "мне нравится классическая музыка", "Настя не переносит шумные бары", "мы любим гулять по вечерам", "я ненавижу караоке",
]

# ---------- noise: how people actually type ----------

def noisy(s):
    s = s.strip()
    r = R.random()
    if r < 0.25:
        s = s.lower().rstrip("?!.")
    elif r < 0.35:
        s = s[0].upper() + s[1:]
    if R.random() < 0.08:  # a typo: a dropped or doubled letter
        k = R.randrange(1, max(2, len(s) - 1))
        if s[k].isalpha():
            s = s[:k] + s[k + 1:] if R.random() < 0.5 else s[:k] + s[k] + s[k:]
    if R.random() < 0.08:
        s += R.choice([" плз", " пж", " 🙏", " )", " :)", " ❤️", "?", "??"])
    if R.random() < 0.05:
        s = s.replace("ё", "е")
    return s

def ex(text, intent, today):
    intent = {k: v for k, v in intent.items() if v not in (None, "", [], False, 0)}
    return {"messages": [{"role": "system", "content": system(today)}, {"role": "user", "content": noisy(text)},
                         {"role": "assistant", "content": json.dumps(intent, ensure_ascii=False, separators=(",", ":"))}]}

# ---------- generators ----------

def g_find(today, test=False):
    it = {"i": "find"}
    parts = []
    lead = R.choice(TEST_FIND_LEADS if test else FIND_LEADS)
    if R.random() < 0.6:
        d, kind, arg = R.choice(DAYS)
        parts.append(d)
        if kind == "rel":
            it["date"] = (today + dt.timedelta(days=arg)).isoformat()
        elif kind == "wd":
            it["date"] = nearest(today, arg).isoformat()
        else:
            it["wknd"] = True
    if R.random() < 0.35:
        p, part = R.choice(PARTS)
        parts.append(p)
        it["part"] = part
    cat = None
    if R.random() < 0.45:
        cat = R.choice(list(CATS))
        parts.append(R.choice(CATS[cat]))
        it["cats"] = [cat]
    if R.random() < 0.3:
        mood = R.choice([m for m in MOODS if not (m == "food" and cat == "food")])
        parts.append(R.choice(MOODS[mood]))
        it["moods"] = [mood]
    if R.random() < 0.3:
        tpl, kind = R.choice(BUDGET)
        if kind == "k":
            k = R.choice([2, 3, 4, 5, 6, 7, 10])
            parts.append(tpl.format(k=k)); it["max"] = k * 1000
        else:
            n = R.choice([1500, 2000, 2500, 3000, 3500, 4000, 5000, 6000, 8000])
            parts.append(tpl.format(n=n)); it["max"] = n
    elif R.random() < 0.08:
        parts.append(R.choice(["недорого", "подешевле", "бюджетно"])); it["max"] = 2000
    if R.random() < 0.15:
        nc = R.choice([c for c in CAT_NOT if c != cat])
        w = R.choice(CAT_NOT[nc])
        parts.append(R.choice(["без ", "только не ", "кроме "]) + w)
        it["not"] = [nc]; it["notw"] = [NOT_WORD[w]]
    if R.random() < 0.08:
        parts.append(R.choice(["через месяц", "на новогодние праздники", "весной", "в декабре", "через пару месяцев", "в анонсах"]))
        it["hz"] = "later"
    if not parts:
        parts.append(R.choice(MOODS["chill"])); it["moods"] = ["chill"]
    if R.random() < 0.3:
        R.shuffle(parts)
    text = lead.format(x=" ".join(parts))
    return ex(text, it, today)

FIND_LEADS = ["куда сходить {x}", "что поделать {x}", "хотим {x}", "давай {x}", "есть идеи {x}?", "что есть {x}?", "подбери что-нибудь {x}",
              "куда пойти {x}?", "чем заняться {x}", "найди что-нибудь {x}", "что-нибудь {x}", "{x}", "посоветуй {x}", "хочется {x}",
              "можно куда-то {x}?", "что интересного {x}", "вариант {x}?", "подкинь идей {x}", "куда бы нам {x}", "чего бы {x}"]
TEST_FIND_LEADS = ["есть что-то {x}", "мысли есть, куда {x}?", "глянь варианты {x}", "что предложишь {x}", "куда махнём {x}?",
                   "организуй нам что-нибудь {x}", "{x} — что есть?", "накидай вариантов {x}"]

def g_scout(today, test=False):
    t = R.choice(["что нашёл скаут", "покажи афишу", "что в афише", "что новенького в афише", "что скаут нашёл на неделю", "свежие находки скаута",
                  "что там в афише на выходных"] if not test else ["есть что от скаута?", "открой подборку скаута", "что агент нарыл"])
    it = {"i": "find", "scout": True}
    if "выходн" in t:
        it["wknd"] = True
    return ex(t, it, today)

def g_suggest(today, test=False):
    if R.random() < 0.6:
        t = R.choice(LEADS) + R.choice(["удиви нас", "удиви", "рандом", "выбери наугад", "что-нибудь случайное", "давай случайное", "кинь случайную идею",
                      "сделай нам сюрприз", "доверимся случаю", "рандомная идея"] if not test else ["выбери за нас что угодно", "что выпадет — туда и пойдём", "крутани рулетку"])
        return ex(t, {"i": "suggest", "mode": "random"}, today)
    t = R.choice(LEADS) + R.choice(["посоветуй что-нибудь из копилки", "что лучше всего из наших идей", "какую идею из копилки выбрать", "что нам точно зайдёт из списка", "что нам больше всего понравится", "выбери лучшее из нашего списка", "что из копилки самое то",
                  "какая идея у нас в топе"] if not test else ["что из наших идей лучше всего подходит", "назови фаворита из копилки"])
    return ex(t, {"i": "suggest"}, today)

def g_price(today, test=False):
    x = R.choice(SHOWS + EVENTS)
    t = R.choice(["сколько стоит {x}", "почём {x}?", "какая цена на {x}", "сколько стоят билеты на {x}", "узнай цену на {x}", "дорого ли {x}?",
                  "есть ли ещё билеты на {x}", "сколько будет стоить {x}"] if not test else ["во что обойдётся {x}", "цена вопроса за {x}?", "{x} — сколько за двоих?"])
    return ex(t.format(x=x), {"i": "price", "q": x}, today)

def g_book(today, test=False):
    x = R.choice(SHOWS + EVENTS)
    t = R.choice(["запиши нас на {x}", "идём на {x}", "забронируй {x}", "добавь в календарь {x}", "поставь {x} в календарь", "берём билеты на {x}",
                  "давай пойдём на {x}, позови её", "позови Настю на {x}"] if not test else ["хотим на {x}, оформи", "внеси {x} нам в планы", "решено, {x}! запиши"])
    return ex(t.format(x=x), {"i": "book", "q": x}, today)

def g_about(today, test=False):
    x = R.choice(SHOWS + EVENTS)
    t = R.choice(["расскажи про {x}", "что за спектакль {x}", "о чём {x}?", "кто играет в {x}", "подойдёт ли нам {x}?", "стоит ли идти на {x}",
                  "понравится ли Насте {x}", "что это за {x}", "какой жанр у {x}"] if not test else ["{x} — это вообще что?", "нам зайдёт {x}?", "что известно про {x}"])
    return ex(t.format(x=x), {"i": "about", "q": x}, today)

def g_remember(today, test=False):
    p = R.choice(PREFS)
    t = R.choice(["{p}", "запомни, {p}", "учти: {p}", "имей в виду — {p}", "кстати, {p}"] if not test else ["на будущее: {p}", "просто чтобы ты знал, {p}"])
    text = t.format(p=p)
    return ex(text, {"i": "remember", "text": p}, today)

def g_goal(today, test=False):
    x = R.choice(SHOWS + EVENTS)
    return ex(R.choice(TEST_GOALS if test else GOALS).format(x=x), {"i": "goal", "text": x}, today)

SIMPLE = {
    "overview": (["что у нас запланировано?", "какие планы на неделю", "когда следующая встреча", "мы куда-то идём на этой неделе?", "что у нас в планах",
                  "какие у нас встречи впереди", "что дальше по плану", "что мы делаем в выходные", "у нас что-то стоит на субботу?", "когда мы идём в театр",
                  "какое ближайшее свидание", "есть планы на завтра?", "что в календаре", "покажи наши планы"], ["что там у нас по планам?", "напомни, куда мы собирались"]),
    "past": (["где мы были в последнее время", "куда мы ходили", "история наших встреч", "где мы уже были", "что мы посетили в прошлом месяце",
              "куда мы ходили в октябре", "где были на прошлых выходных", "когда мы последний раз были в театре", "что понравилось из прошлых встреч"],
             ["вспомни, где мы бывали", "наши прошлые свидания"]),
    "chores": (["какие у нас дела по дому", "что по уборке", "кто выносит мусор", "чья очередь мыть посуду", "какие домашние дела", "что нужно сделать дома",
                "кто сегодня готовит", "что с бытовыми делами", "кто стирает на этой неделе", "список дел по квартире", "что горит по дому"],
               ["что по хозяйству?", "кто сегодня дежурит по дому"]),
    "plan": (["давай выберем, куда пойти", "начни выбор встречи", "запланируй встречу", "давай вместе выберем", "выберем куда сходить",
              "давай назначим свидание", "давай запланируем выходные вместе", "запусти голосование куда пойти", "пора что-то запланировать вдвоём"],
             ["пора выбрать свидание", "запусти выбор"]),
    "chat": (["привет", "что ты умеешь?", "спасибо!", "кто ты", "как пользоваться", "спасибо, ты супер", "приветик", "добрый вечер", "хорошо, понял",
              "ок", "класс, спасибо", "ты кто вообще", "помоги", "ага", "здравствуй", "как дела?", "отлично!"], ["хай", "благодарю!", "окей, принято"]),
}

LEADS = ["", "", "", "слушай, ", "а ", "подскажи, ", "скажи, ", "эй, ", "кстати, ", "ну и ", "помощник, "]

def g_simple(today, test=False):
    k = R.choice(list(SIMPLE))
    t = R.choice(SIMPLE[k][1 if test else 0])
    if k != "chat":
        t = R.choice(LEADS) + t
    return ex(t, {"i": k}, today)

def g_add(today, test=False):
    x = R.choice(EVENTS + ["покататься на теплоходе", "пикник в Коломенском", "сходить в баню", "ужин на крыше"])
    t = R.choice(["добавь {x}", "сохрани: {x}", "добавь в копилку {x}", "запиши идею — {x}"] if not test else ["закинь в идеи {x}", "{x} — в копилку"])
    return ex(t.format(x=x), {"i": "add_idea", "text": x}, today)

def g_budget(today, test=False):
    n = R.choice([2000, 3000, 4000, 5000, 6000, 7000, 10000])
    t = R.choice(["поставь бюджет {n}", "бюджет {n} на встречу", "давай лимит {n}", "сделай бюджет {n} рублей"] if not test else ["тратим не больше {n} за раз", "ограничь встречи суммой {n}"])
    return ex(t.format(n=n), {"i": "budget", "rub": n}, today)

def g_mode(today, test=False):
    m, t = R.choice([("random", "присылай случайные идеи"), ("best", "предлагай лучшее по оценкам"), ("off", "не присылай больше идеи"),
                     ("off", "перестань присылать предложения"), ("mix", "присылай то лучшее, то случайное")] if not test else
                    [("off", "хватит слать идеи"), ("random", "шли рандомные варианты раз в неделю")])
    return ex(t, {"i": "suggest_mode", "mode": m}, today)

# ---------- dialogue: the second message is read in the light of the first ----------

FOLLOW = [  # (phrase, how it changes the previous search)
    ("а подешевле?", "cheaper"), ("а можно подешевле", "cheaper"), ("подешевле есть?", "cheaper"),
    ("а в воскресенье?", ("date", 6)), ("а в субботу?", ("date", 5)), ("а в пятницу?", ("date", 4)), ("а завтра?", ("rel", 1)), ("а сегодня?", ("rel", 0)),
    ("а вечером?", ("part", "evening")), ("а утром?", ("part", "morning")), ("а днём?", ("part", "day")),
    ("а без театра?", ("not", "culture", "театр")), ("только не рестораны", ("not", "food", "ресторан")), ("а без стендапа", ("not", "other", "стендап")),
    ("а что-нибудь поспокойнее?", ("mood", "chill")), ("а поактивнее?", ("mood", "active")), ("а что-то романтичное?", ("mood", "romantic")),
    ("а на выходных?", "wknd"),
]
TEST_FOLLOW = [("а если дешевле?", "cheaper"), ("а в воскресенье можно?", ("date", 6)), ("а вечерком?", ("part", "evening")), ("и без театров", ("not", "culture", "театр"))]

def follow(prev, how, today):
    it = json.loads(json.dumps(prev))
    it.pop("scout", None)
    if how == "cheaper":
        it["max"] = it["max"] * 2 // 3 if it.get("max") else 3000
    elif how == "wknd":
        it.pop("date", None); it["wknd"] = True
    elif how[0] == "date":
        it.pop("wknd", None); it["date"] = nearest(today, how[1]).isoformat()
    elif how[0] == "rel":
        it.pop("wknd", None); it["date"] = (today + dt.timedelta(days=how[1])).isoformat()
    elif how[0] == "part":
        it["part"] = how[1]
    elif how[0] == "not":
        it["not"] = sorted(set(it.get("not", []) + [how[1]])); it["notw"] = it.get("notw", []) + [how[2]]
        if how[1] in it.get("cats", []):
            it["cats"] = [c for c in it["cats"] if c != how[1]]
    elif how[0] == "mood":
        it["moods"] = [how[1]]
    return it

def g_dialog(today, test=False):
    first = g_find(today, test)
    prev = json.loads(first["messages"][2]["content"])
    if R.random() < 0.2:  # a new topic after a search: not merged
        nxt = R.choice([g_price, g_about, g_book, g_simple])(today, test)
        msgs = first["messages"] + nxt["messages"][1:]
        return {"messages": msgs}
    phrase, how = R.choice(TEST_FOLLOW if test else FOLLOW)
    it = {k: v for k, v in follow(prev, how, today).items() if v not in (None, "", [], False, 0)}
    return {"messages": first["messages"] + [{"role": "user", "content": noisy(phrase)},
                                             {"role": "assistant", "content": json.dumps(it, ensure_ascii=False, separators=(",", ":"))}]}

# periods in words → from/to (as the app's rules count them: the past part of a period is cut off)
MON_GEN = ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"]
MON_PREP = ["январе", "феврале", "марте", "апреле", "мае", "июне", "июле", "августе", "сентябре", "октябре", "ноябре", "декабре"]

def typo(w):  # a voice message or a fast thumb: one letter off
    if len(w) < 5 or R.random() > 0.25:
        return w
    k = R.randrange(1, len(w) - 1)
    return w[:k] + R.choice("аеиоуя") + w[k + 1:]

def month_span(today, m, part):
    import calendar
    y = today.year + (1 if m < today.month else 0)
    last = calendar.monthrange(y, m)[1]
    a, b = {"start": (1, 10), "mid": (10, 20), "end": (20, last), "all": (1, last)}[part]
    f, t = dt.date(y, m, a), dt.date(y, m, b)
    return max(f, today), t

def g_period(today, test=False):
    leads = ["что есть", "куда сходить", "какие есть мероприятия", "что интересного", "подскажи, что есть", "что будет"] if not test else \
        ["есть варианты", "чем заняться", "что можно посмотреть", "куда выбраться"]
    lead = R.choice(leads)
    k = R.random()
    if k < 0.2:
        mon = today - dt.timedelta(days=today.weekday()) + dt.timedelta(days=7)
        f, t, words = mon, mon + dt.timedelta(days=6), R.choice(["на следующей неделе", "через неделю"])
    elif k < 0.3:
        import calendar
        f, t = today, dt.date(today.year, today.month, calendar.monthrange(today.year, today.month)[1])
        words = R.choice(["до конца месяца", "в этом месяце"])
    else:
        m = R.randrange(1, 13)
        part = R.choice(["start", "mid", "end", "all", "all"])
        if part == "all":
            words = "в " + typo(MON_PREP[m - 1])
        else:
            words = {"start": "в начале", "mid": "в середине", "end": "в конце"}[part] + " " + typo(MON_GEN[m - 1])
        f, t = month_span(today, m, part)
        if t < f:
            return g_period(today, test)
    it = {"i": "find", "from": f.isoformat(), "to": t.isoformat()}
    text = R.choice(["{l} {w}?", "{w} {l}?", "{l} {w}, куда мы можем сходить вдвоём"]).format(l=lead, w=words)
    return ex(text, it, today)

def g_whole(today, test=False):
    k = R.random()
    if k < 0.4:
        t, it = R.choice(["подборку всего что есть", "покажи вообще всё", "всё что есть в москве", "полный список", "что вообще идёт"] if not test else
                         ["выдай всё списком", "давай весь список"]), {"i": "find", "all": True}
    elif k < 0.7:
        t, it = R.choice(["только новое", "что мы ещё не видели", "чего нет в копилке", "свежее от скаута, чего мы не знаем"] if not test else
                         ["покажи то, о чём мы не слышали"]), {"i": "find", "whose": "new"}
    else:
        t, it = R.choice(["только наше", "что у нас в копилке", "наши идеи", "из того, что мы сами добавили"] if not test else
                         ["что мы хотели раньше"]), {"i": "find", "whose": "ours"}
    return ex(t, it, today)

GENS = [(g_period, 8), (g_whole, 4), (g_find, 40), (g_scout, 3), (g_suggest, 7), (g_price, 7), (g_book, 7), (g_about, 7), (g_remember, 6), (g_goal, 5), (g_simple, 14),
        (g_add, 3), (g_budget, 2), (g_mode, 2), (g_dialog, 14)]

def build(n, test=False):
    out, seen = [], set()
    weights = [w for _, w in GENS]
    while len(out) < n:
        g = R.choices([g for g, _ in GENS], weights)[0]
        today = dt.date(2026, 11, 1) + dt.timedelta(days=R.randrange(0, 150))
        e = g(today, test)
        key = " | ".join(m["content"].lower() for m in e["messages"][1::2])
        if key in seen:
            continue
        seen.add(key)
        out.append(e)
    return out

def main():
    out = sys.argv[1] if len(sys.argv) > 1 else "out"
    os.makedirs(out, exist_ok=True)
    train, valid = build(2600), None
    valid, train = train[:300], train[300:]
    global SHOWS, EVENTS, PREFS
    SHOWS, EVENTS = TEST_SHOWS, TEST_EVENTS  # test uses names the model never saw in training
    test = build(250, test=True)
    for name, data in [("train", train), ("valid", valid), ("test", test)]:
        with open(os.path.join(out, name + ".jsonl"), "w", encoding="utf-8") as f:
            for e in data:
                f.write(json.dumps(e, ensure_ascii=False) + "\n")
    stats = {}
    for e in train:
        k = json.loads(e["messages"][-1]["content"])["i"] + ("+dialog" if len(e["messages"]) > 3 else "")
        stats[k] = stats.get(k, 0) + 1
    print(f"train {len(train)} · valid {len(valid)} · test {len(test)}", json.dumps(stats, ensure_ascii=False))

TEST_SHOWS = ["Гамлет", "Дама с собачкой", "Идиот", "Чайка по-новому", "Борис Годунов", "Иоланта", "Спящая красавица", "Ханума", "Тартюф", "Три мушкетёра"]
TEST_EVENTS = ["концерт Шамана", "выставку Шагала", "фестиваль уличной еды", "ночь музеев", "ледовое шоу", "концерт Сплина", "вечер джаза на крыше"]

if __name__ == "__main__":
    main()
