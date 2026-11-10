// Package bot turns Telegram updates into app calls.
package bot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/app"
	"vdvoem/internal/assistant"
	"vdvoem/internal/domain"
	"vdvoem/internal/live"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/stt"
	"vdvoem/internal/tg"
)

type Bot struct {
	App    *app.App
	Helper *assistant.Assistant
	TG     *tg.Client
	STT    *stt.Client // voice messages → text; nil or empty — voice is off
	Secret string
	Log    *slog.Logger
}

var Commands = map[string]string{
	"start":    "Начать / пригласить партнёра",
	"go":       "Выбрать, куда сходить",
	"surprise": "Удиви нас — случайная идея",
	"ask":      "Спросить помощника",
	"plans":    "Что запланировано",
	"invite":   "Новая ссылка для партнёра",
	"help":     "Как это работает",
}
var CommandOrder = []string{"go", "surprise", "ask", "plans", "invite", "help", "start"}

// ServeHTTP is the webhook endpoint. It answers 200 quickly; work runs with its own timeout.
func (b *Bot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), []byte(b.Secret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var u tg.Update
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&u); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if fresh, err := b.App.S.FirstSeen(ctx, u.UpdateID, b.App.Now()); err == nil && !fresh {
		w.WriteHeader(http.StatusOK)
		return
	}
	defer func() {
		if p := recover(); p != nil {
			b.Log.Error("panic in update", "panic", p)
		}
	}()
	b.Handle(ctx, u)
	w.WriteHeader(http.StatusOK)
}

func name(u *tg.User) string {
	if u == nil {
		return "Партнёр"
	}
	n := strings.TrimSpace(u.FirstName)
	if n == "" {
		n = u.Username
	}
	if n == "" {
		n = "Партнёр"
	}
	return n
}

func (b *Bot) Handle(ctx context.Context, u tg.Update) {
	var from int64
	if u.CallbackQuery != nil {
		from = u.CallbackQuery.From.ID
	} else if u.Message != nil && u.Message.From != nil {
		from = u.Message.From.ID
	}
	// whatever the bot changed (an idea, a vote, a confirmation) shows up in the open apps right away
	var space int64
	if from != 0 {
		if m, err := b.App.S.MemberByTG(ctx, from); err == nil {
			space = m.SpaceID
			ctx = live.WithSource(ctx, m.ID, "")
		}
	}
	defer func() {
		if space == 0 && from != 0 { // /start or an invite just created the space
			if m, err := b.App.S.MemberByTG(ctx, from); err == nil {
				space = m.SpaceID
			}
		}
		b.App.Live.From(ctx, space)
	}()
	switch {
	case u.CallbackQuery != nil:
		b.callback(ctx, u.CallbackQuery)
	case u.Message != nil && u.Message.Chat.Type == "private":
		b.message(ctx, u.Message)
	}
}

func (b *Bot) say(ctx context.Context, chat int64, text string, mk *tg.Markup) {
	if _, err := b.TG.Send(ctx, chat, text, tg.SendOpts{Markup: mk}); err != nil {
		b.Log.Warn("send", "err", err)
	}
}

func (b *Bot) openApp(path string) *tg.Markup {
	return tg.Keyboard(tg.Row(tg.App("Открыть «Вдвоём»", strings.TrimRight(b.App.PublicURL, "/")+"/app/"+path)))
}

func (b *Bot) message(ctx context.Context, m *tg.Message) {
	chat := m.Chat.ID
	text := strings.TrimSpace(m.Text)
	if cmd, arg := splitCmd(text); cmd == "/start" && strings.HasPrefix(arg, "login_") {
		if _, err := b.App.Couple(ctx, m.From.ID); errors.Is(err, app.ErrNoSpace) {
			_, _, _ = b.App.Start(ctx, app.Profile{TGID: m.From.ID, ChatID: m.Chat.ID, Name: name(m.From)}, "")
		}
		b.App.AskLogin(ctx, m.Chat.ID, strings.TrimPrefix(arg, "login_"))
		return
	}
	if strings.HasPrefix(text, "/start") {
		b.start(ctx, m)
		return
	}
	c, err := b.App.Couple(ctx, m.From.ID)
	if errors.Is(err, app.ErrNoSpace) {
		b.start(ctx, m)
		return
	}
	if err != nil {
		b.Log.Error("couple", "err", err)
		return
	}
	// a post forwarded from a channel (often a private one): read the events out of it
	if fp, ok := forwarded(m); ok {
		if err := b.App.Repost(ctx, c, chat, fp); err != nil {
			b.say(ctx, chat, "Не получилось принять пост — попробуй ещё раз.", nil)
			return
		}
		b.say(ctx, chat, "📨 Читаю пост из «"+html.EscapeString(fp.From)+"» — достану события, даты и цены и пришлю сюда.", nil)
		return
	}
	if v := m.Voice; v != nil || m.Audio != nil {
		if v == nil {
			v = m.Audio
		}
		b.voice(ctx, c, chat, v)
		return
	}
	// the tickets right after «Билеты есть»: a file, a screenshot or a link — kept with the meeting, sent to the partner
	if rid := b.App.AwaitingTickets(ctx, c); rid != 0 && (m.Document != nil || len(m.Photo) > 0 || parse.FirstURL(text) != "") {
		f := app.TicketFile{Kind: "link", URL: parse.FirstURL(text)}
		switch {
		case m.Document != nil:
			f = app.TicketFile{Kind: "document", FileID: m.Document.FileID, Name: m.Document.FileName}
		case len(m.Photo) > 0:
			f = app.TicketFile{Kind: "photo", FileID: m.Photo[len(m.Photo)-1].FileID}
		}
		if err := b.App.AttachTickets(ctx, c, rid, f); err != nil {
			b.say(ctx, chat, "Не получилось сохранить билеты — попробуй ещё раз или прикрепи их в приложении.", b.openApp(fmt.Sprintf("round/%d", rid)))
		}
		return
	}
	if m.Document != nil {
		b.say(ctx, chat, "Файлы я принимаю как билеты: нажми «Билеты есть» у встречи и пришли их следом.", nil)
		return
	}
	if len(m.Photo) > 0 {
		ok, _ := b.App.Photo(ctx, c, m.Photo[len(m.Photo)-1].FileID)
		if ok {
			b.say(ctx, chat, "Сохранил фото к вашей встрече.", nil)
		} else {
			b.say(ctx, chat, "Фото сохраняю к встречам — пришли его после того, как сходите.", nil)
		}
		return
	}
	// a reply to an idea card adds a concrete place to that idea
	if m.ReplyTo != nil && text != "" && !strings.HasPrefix(text, "/") {
		if ideaID, err := b.App.S.CardIdea(ctx, chat, m.ReplyTo.MessageID); err == nil {
			if _, err := b.App.AddOption(ctx, c, ideaID, text); err != nil {
				b.say(ctx, chat, "Не получилось добавить место — попробуй ещё раз.", nil)
			} else if parse.FirstURL(text) != "" {
				b.say(ctx, chat, "Принял ссылку — разбираю, что это за место…", nil)
			} else {
				b.say(ctx, chat, "Добавил вариант места: <b>"+html.EscapeString(text)+"</b>", nil)
			}
			return
		}
	}
	cmd, arg := splitCmd(text)
	switch cmd {
	case "/go":
		b.goRound(ctx, c, chat)
	case "/surprise":
		b.suggest(ctx, c, chat, domain.SuggestRandom, nil)
	case "/ask":
		if arg == "" {
			b.say(ctx, chat, "Спроси что угодно про ваши планы — например: <i>куда сходить в субботу вечером?</i>", b.openApp("assistant"))
			return
		}
		b.ask(ctx, c, chat, arg)
	case "/plans":
		b.ask(ctx, c, chat, "что у нас запланировано")
	case "/invite":
		if c.Partner != nil {
			b.say(ctx, chat, "Вы уже вдвоём.", nil)
			return
		}
		link, err := b.App.NewInvite(ctx, c)
		if err == nil {
			b.sendInvite(ctx, chat, link)
		}
	case "/help":
		b.say(ctx, chat, helpText, b.openApp(""))
	case "":
		b.free(ctx, c, chat, text)
	default:
		b.say(ctx, chat, "Не знаю такую команду. /help — что я умею.", nil)
	}
}

// forwarded: a channel post with enough text to hold an event
func forwarded(m *tg.Message) (app.ChannelPost, bool) {
	var from *tg.Chat
	if m.Forward != nil {
		from = m.Forward.Chat
		if from == nil {
			from = m.Forward.SenderChat
		}
	}
	if from == nil {
		from = m.FwdChat
	}
	text, ents := m.Text, m.Entities
	if text == "" {
		text, ents = m.Caption, m.CapEnts
	}
	if from == nil || len([]rune(strings.TrimSpace(text))) < 25 {
		return app.ChannelPost{}, false
	}
	p := app.ChannelPost{From: from.Title, Text: text}
	if p.From == "" {
		p.From = "канала"
	}
	if from.Username != "" && m.Forward != nil && m.Forward.MessageID != 0 {
		p.Link = fmt.Sprintf("https://t.me/%s/%d", from.Username, m.Forward.MessageID)
	}
	for _, e := range ents {
		if e.Type == "text_link" && strings.HasPrefix(e.URL, "http") && len(p.Links) < 20 {
			p.Links = append(p.Links, e.URL)
		}
	}
	return p, true
}

const helpText = `<b>Как это работает</b>
1. Кидайте в бота ссылки и идеи — куда сходить или что сделать. Я разберу и сложу в общую копилку.
2. Каждый отмечает: хочу, очень хочу или пропущу. Голос партнёра вы не видите, пока не отметите сами.
3. Когда пора выбрать (/go или сам по расписанию), каждый отмечает удобное время. Я нахожу пересечение и предлагаю три варианта.
4. Совпали время и идея — встреча сразу в плане у обоих, событие придёт в календарь. Передумали — отмена в одно касание.

Пересылайте сюда посты из каналов с афишей (даже закрытых) — я достану события, даты и цены в «Афишу».
Боитесь, что дата уйдёт? На карточке нажмите «🔥 Важно» — я буду напоминать партнёру, пока он не ответит.
Можно писать и говорить голосом: «хотим что-нибудь на выходных», «сколько стоит Чайка».

/surprise — случайная идея из копилки. /ask — спросить помощника. Раз в неделю я сам предложу что-нибудь — режим меняется в настройках.`

func splitCmd(t string) (string, string) {
	if !strings.HasPrefix(t, "/") {
		return "", t
	}
	cmd, arg, _ := strings.Cut(t, " ")
	if i := strings.Index(cmd, "@"); i > 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd), strings.TrimSpace(arg)
}

// free text: a link or a short note goes to the копилка; a question goes to the помощник.
func (b *Bot) free(ctx context.Context, c app.Couple, chat int64, text string) {
	if text == "" {
		return
	}
	if parse.FirstURL(text) == "" && (looksLikeQuestion(text) || assistant.LooksLikeRequest(text, b.App.Now())) {
		b.ask(ctx, c, chat, text)
		return
	}
	if what, ok := app.UrgentText(text); ok && parse.FirstURL(text) == "" && c.Partner != nil {
		// «срочно хочу на …»: the urgency goes on the idea (an existing one if there is), the words are not its title
		i, found := b.App.FindByName(ctx, c, what)
		if !found {
			res, err := b.App.AddIdea(ctx, c, what)
			if err != nil {
				b.say(ctx, chat, "Не получилось сохранить — попробуй ещё раз.", nil)
				return
			}
			i = res.Idea
			if res.Existing != nil {
				i = *res.Existing
			}
		}
		if _, err := b.App.MarkUrgent(ctx, c, i.ID, text); err != nil {
			b.Log.Warn("urgent", "err", err)
		}
		b.say(ctx, chat, fmt.Sprintf("🔥 Отметил как важное: <b>%s</b>. Буду напоминать %s, пока не ответит.", html.EscapeString(i.Title), app.NameDat(c.PartnerName())), b.openApp(fmt.Sprintf("idea/%d", i.ID)))
		return
	}
	res, err := b.App.AddIdea(ctx, c, text)
	if err != nil {
		b.Log.Error("add idea", "err", err)
		b.say(ctx, chat, "Не получилось сохранить — попробуй ещё раз.", nil)
		return
	}
	if res.Existing != nil {
		b.say(ctx, chat, fmt.Sprintf("Это уже есть в копилке: <b>%s</b>.", html.EscapeString(res.Existing.Title)), b.openApp(fmt.Sprintf("idea/%d", res.Existing.ID)))
		return
	}
}

func looksLikeQuestion(t string) bool {
	l := strings.ToLower(strings.TrimSpace(t))
	if strings.HasSuffix(l, "?") {
		return true
	}
	for _, p := range []string{"куда ", "что ", "когда ", "где ", "посоветуй", "предложи", "удиви", "подскажи", "помоги", "какие ", "сколько ", "покажи", "напомни"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

func (b *Bot) ask(ctx context.Context, c app.Couple, chat int64, q string) {
	rep, err := b.Helper.Ask(ctx, c, q)
	if errors.Is(err, assistant.ErrLimit) {
		b.say(ctx, chat, "На сегодня вопросов достаточно — продолжим завтра.", nil)
		return
	}
	if err != nil {
		b.Log.Error("assistant", "err", err)
		b.say(ctx, chat, "Не получилось ответить — попробуй ещё раз.", nil)
		return
	}
	b.reply(ctx, chat, rep)
}

// voice: a spoken request — heard locally, then answered like a typed one (with what was heard on top)
func (b *Bot) voice(ctx context.Context, c app.Couple, chat int64, v *tg.Voice) {
	if !b.STT.Enabled() {
		b.say(ctx, chat, "Голосовые я пока не слушаю — напиши, пожалуйста, текстом.", nil)
		return
	}
	if v.Duration > 60 {
		b.say(ctx, chat, "Это длинновато — скажи покороче, до минуты.", nil)
		return
	}
	b.TG.Typing(ctx, chat)
	audio, err := b.TG.File(ctx, v.FileID, stt.MaxBytes)
	var text string
	if err == nil {
		text, err = b.STT.Text(ctx, audio, "voice.ogg")
	}
	if errors.Is(err, stt.ErrEmpty) {
		b.say(ctx, chat, "Не расслышал — повтори, пожалуйста, или напиши.", nil)
		return
	}
	if err != nil {
		b.Log.Warn("voice", "err", err)
		b.say(ctx, chat, "Не получилось разобрать голосовое — напиши текстом.", nil)
		return
	}
	rep, err := b.Helper.Ask(ctx, c, text)
	if errors.Is(err, assistant.ErrLimit) {
		b.say(ctx, chat, "На сегодня вопросов достаточно — продолжим завтра.", nil)
		return
	}
	if err != nil {
		b.Log.Error("assistant", "err", err)
		b.say(ctx, chat, "Не получилось ответить — попробуй ещё раз.", nil)
		return
	}
	rep.Text = "🎙 «" + text + "»\n\n" + rep.Text
	b.reply(ctx, chat, rep)
}

// reply: the assistant's answer as one message — text, up to three cards, its buttons
func (b *Bot) reply(ctx context.Context, chat int64, rep assistant.Reply) {
	var sb strings.Builder
	sb.WriteString(html.EscapeString(rep.Text))
	for k, cd := range rep.Cards {
		if k >= 3 {
			break
		}
		sb.WriteString("\n\n<b>" + html.EscapeString(cd.Title) + "</b>")
		if cd.When != "" {
			sb.WriteString("\n" + html.EscapeString(cd.When))
		}
		if cd.Meta != "" {
			sb.WriteString("\n" + html.EscapeString(cd.Meta))
		}
		if cd.Why != "" {
			sb.WriteString("\n<i>" + html.EscapeString(cd.Why) + "</i>")
		}
	}
	var rows [][]tg.Button
	if rep.Pick != nil {
		rows = append(rows, tg.Row(tg.App(fmt.Sprintf("📋 Вся подборка · %d", rep.Pick.N), strings.TrimRight(b.App.PublicURL, "/")+fmt.Sprintf("/app/pick/%d", rep.Pick.ID))))
	}
	for _, a := range rep.Actions {
		rows = append(rows, tg.Row(tg.CB(a.Label, "aa:"+a.ID)))
	}
	rows = append(rows, tg.Row(tg.App("Продолжить в приложении", strings.TrimRight(b.App.PublicURL, "/")+"/app/assistant")))
	b.say(ctx, chat, sb.String(), tg.Keyboard(rows...))
}

func (b *Bot) start(ctx context.Context, m *tg.Message) {
	_, payload := splitCmd(strings.TrimSpace(m.Text))
	c, link, err := b.App.Start(ctx, app.Profile{TGID: m.From.ID, ChatID: m.Chat.ID, Name: name(m.From)}, payload)
	switch {
	case errors.Is(err, app.ErrInviteUsed):
		b.say(ctx, m.Chat.ID, "Ссылка-приглашение уже использована или устарела. Попроси партнёра прислать новую: /invite.", nil)
		return
	case errors.Is(err, store.ErrFull):
		b.say(ctx, m.Chat.ID, "В этом пространстве уже двое.", nil)
		return
	case err != nil:
		b.Log.Error("start", "err", err)
		b.say(ctx, m.Chat.ID, "Что-то пошло не так. Попробуй /start ещё раз.", nil)
		return
	}
	if c.Partner != nil {
		b.say(ctx, m.Chat.ID, fmt.Sprintf("Привет, %s! Вы с %s теперь вдвоём. Кидайте сюда ссылки и идеи — я сложу их в общую копилку.", html.EscapeString(c.Me.Name), html.EscapeString(app.NameIns(c.Partner.Name))), b.openApp(""))
		return
	}
	b.say(ctx, m.Chat.ID, fmt.Sprintf("Привет, %s! Я помогаю паре решить, куда сходить и когда — без бесконечного «не знаю, а ты?».", html.EscapeString(c.Me.Name)), nil)
	if link == "" {
		link, _ = b.App.NewInvite(ctx, c)
	}
	b.sendInvite(ctx, m.Chat.ID, link)
}

func (b *Bot) sendInvite(ctx context.Context, chat int64, link string) {
	share := "https://t.me/share/url?url=" + urlEscape(link) + "&text=" + urlEscape("Давай планировать выходные вместе — в «Вдвоём»")
	b.say(ctx, chat, "Пригласи партнёра — ссылка действует 3 дня и сработает один раз:\n"+link,
		tg.Keyboard(tg.Row(tg.Link("Отправить приглашение", share)), tg.Row(tg.App("Открыть «Вдвоём»", strings.TrimRight(b.App.PublicURL, "/")+"/app/"))))
}

func (b *Bot) goRound(ctx context.Context, c app.Couple, chat int64) {
	if _, err := b.App.Go(ctx, c); errors.Is(err, app.ErrNotPaired) {
		b.say(ctx, chat, "Сначала пригласи партнёра: /invite", nil)
	} else if err != nil {
		b.Log.Error("go", "err", err)
	}
}

func (b *Bot) suggest(ctx context.Context, c app.Couple, chat int64, mode domain.SuggestMode, chain []int64) {
	s, err := b.App.Suggest(ctx, c, mode, chain)
	if errors.Is(err, app.ErrNoIdeas) {
		msg := "В копилке пока нечего предложить. Пришли ссылку на место или напиши идею."
		if len(chain) > 0 {
			msg = "Больше подходящих идей нет — вы посмотрели всё, что есть под это время и бюджет."
		}
		b.say(ctx, chat, msg, nil)
		return
	}
	if err != nil {
		b.Log.Error("suggest", "err", err)
		return
	}
	b.App.SendSuggestion(ctx, chat, s, chain, "")
}

// ---------- buttons ----------

func (b *Bot) callback(ctx context.Context, q *tg.CallbackQuery) {
	toast := ""
	defer func() { _ = b.TG.Answer(ctx, q.ID, toast) }()
	c, err := b.App.Couple(ctx, q.From.ID)
	if err != nil {
		toast = "Сначала /start"
		return
	}
	chat := q.From.ID
	if q.Message != nil {
		chat = q.Message.Chat.ID
	}
	p := strings.Split(q.Data, ":")
	id := func(k int) int64 {
		if k < len(p) {
			n, _ := strconv.ParseInt(p[k], 10, 64)
			return n
		}
		return 0
	}
	arg := func(k int) string {
		if k < len(p) {
			return p[k]
		}
		return ""
	}
	err = nil
	switch p[0] {
	case "w":
		_, err = b.App.SetWish(ctx, c, id(1), domain.Wish(arg(2)))
		toast = map[string]string{"want": "Хочу ✓", "very": "Очень хочу ✓", "skip": "Пропущу"}[arg(2)]
		if err == nil && q.Message != nil {
			_ = b.TG.Edit(ctx, chat, q.Message.MessageID, q.Message.Text+"\n\n<i>Твоя отметка: "+strings.ToLower(toast)+"</i>", nil)
		}
	case "cd": // a chore done right from its card
		_, err = b.App.ChoreDone(ctx, c, id(1))
		toast = "Сделано ✓"
		if err == nil && q.Message != nil {
			_ = b.TG.Edit(ctx, chat, q.Message.MessageID, q.Message.Text+"\n\n<i>✓ Сделано</i>", nil)
		}
	case "mg":
		err = b.App.Merge(ctx, c, id(1), id(2))
		toast = "Объединил"
	case "nm":
		toast = "Оставил обе"
	case "go":
		b.goRound(ctx, c, chat)
	case "v":
		_, err = b.App.Vote(ctx, c, id(1), id(2))
		toast = "Голос принят"
	case "rs":
		_, err = b.App.Reshuffle(ctx, c, id(1))
	case "ts": // «this time suits me»: ts:<round>:<yyyy-mm-dd>:<part>[@hh:mm]
		_, err = b.App.AcceptSlot(ctx, c, id(1), strings.Join(p[2:], ":"))
		toast = "Подходит ✓"
		if err == nil && q.Message != nil {
			_ = b.TG.Edit(ctx, chat, q.Message.MessageID, q.Message.Text+"\n\n<i>✓ Время выбрано</i>", nil)
		}
	case "tk": // «билеты есть»
		err = b.App.GotTickets(ctx, c, id(1))
		toast = "Отметил — партнёр знает, что билеты есть"
	case "ur": // «важно»: ur:<idea>:yes|no|later
		var msg string
		msg, err = b.App.AnswerUrgent(ctx, c, id(1), arg(2))
		switch {
		case errors.Is(err, app.ErrNotPending):
			toast, err = "Уже отвечено", nil
		case errors.Is(err, app.ErrNotYours):
			toast, err = "Это твоя просьба — ждём ответа партнёра", nil
		case err == nil:
			toast = msg
			if arg(2) != "later" && q.Message != nil {
				_ = b.TG.Edit(ctx, chat, q.Message.MessageID, html.EscapeString(q.Message.Text)+"\n\n<i>"+html.EscapeString(msg)+"</i>", nil)
			}
		}
	case "ex": // «А потом?»: the partner answers a proposed next stop
		_, err = b.App.AnswerExtra(ctx, c, id(1), arg(2) == "y")
		if errors.Is(err, app.ErrExtraOwn) {
			toast, err = "Это твоё предложение — ждём ответа партнёра", nil
		} else if err == nil {
			toast = map[bool]string{true: "Добавили в маршрут", false: "Хорошо, без этого"}[arg(2) == "y"]
		}
	case "c":
		_, err = b.App.Confirm(ctx, c, id(1))
		toast = "Подтверждено"
	case "ot":
		_, err = b.App.OtherTime(ctx, c, id(1))
	case "x", "xq":
		_, err = b.App.Cancel(ctx, c, id(1))
	case "d":
		_, err = b.App.Done(ctx, c, id(1), arg(2))
	case "ag":
		_, err = b.App.Again(ctx, c, id(1), arg(2))
		toast = "Запомнил"
	case "sg":
		_, err = b.App.GoWith(ctx, c, id(1))
		if errors.Is(err, app.ErrRoundOpen) { // no dead end: offer to put this idea into the open choice
			err = nil
			if i, e := b.App.S.Idea(ctx, c.Space.ID, id(1)); e == nil {
				b.say(ctx, chat, fmt.Sprintf("У вас уже идёт выбор. Взять <b>%s</b> вместо него? Отмеченное время сохранится.", html.EscapeString(i.Title)),
					tg.Keyboard(tg.Row(tg.CB("Да, берём это", fmt.Sprintf("sw:%d", i.ID)), tg.CB("Оставить как есть", "nop"))))
			}
		}
	case "sw":
		_, err = b.App.SwapIdea(ctx, c, id(1))
	case "nop":
		toast = "Оставил как есть"
	case "sn":
		b.suggest(ctx, c, chat, domain.SuggestMode(arg(1)), app.ParseExclude(arg(2)))
	case "lg":
		if e := b.App.ConfirmLogin(ctx, c, arg(1)); e != nil {
			toast = "Ссылка устарела — открой страницу входа ещё раз"
		} else {
			toast = "Готово — возвращайся в браузер"
			if q.Message != nil {
				_ = b.TG.Edit(ctx, chat, q.Message.MessageID, "✅ Вход в браузере подтверждён.", nil)
			}
		}
	case "lgx":
		toast = "Хорошо, вход отменён"
		if q.Message != nil {
			_ = b.TG.Edit(ctx, chat, q.Message.MessageID, "Вход отменён.", nil)
		}
	case "sp":
		opts, e := b.App.SuggestPlaces(ctx, c, id(1))
		switch {
		case errors.Is(e, app.ErrNoAI):
			toast = "Помощник без AI — пришли ссылку или найди на карте"
		case e != nil || len(opts) == 0:
			toast = "Не нашёл вариантов — пришли ссылку на место"
		default:
			var lines []string
			var rows [][]tg.Button
			for _, o := range opts {
				lines = append(lines, "• <b>"+html.EscapeString(o.Title)+"</b> — "+html.EscapeString(strings.Split(o.Note, " · ")[0]))
				rows = append(rows, tg.Row(tg.CB("Берём: "+short(o.Title, 28), fmt.Sprintf("ch:%d:%d", id(1), o.ID)), tg.Link("Карта", o.URL)))
			}
			b.say(ctx, chat, "Вот что можно выбрать. Адреса проверь на карте — я мог ошибиться:\n\n"+strings.Join(lines, "\n"), tg.Keyboard(rows...))
		}
	case "ch":
		err = b.App.ChooseOption(ctx, c, id(1), id(2))
		toast = "Место выбрано"
	case "aa":
		rep, e := b.Helper.Act(ctx, c, arg(1))
		err = e
		if e == nil && rep.Intent != nil { // «или: …» — the other reading answers as a message
			b.reply(ctx, chat, rep)
			toast = "Понял"
		} else if e == nil {
			toast = short(rep.Text, 180)
		}
	}
	if err != nil {
		toast = humanErr(err)
		if toast == "" {
			b.Log.Warn("callback", "data", q.Data, "err", err)
			toast = "Не получилось. Попробуй ещё раз."
		}
	}
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func humanErr(err error) string {
	switch {
	case errors.Is(err, domain.ErrClosed):
		return "Этот выбор уже закрыт."
	case errors.Is(err, domain.ErrWrongState):
		return "Этот шаг уже пройден."
	case errors.Is(err, domain.ErrNotInTriple):
		return "Этого варианта уже нет в тройке."
	case errors.Is(err, store.ErrConflict):
		return "Партнёр как раз ответил — нажми ещё раз."
	case errors.Is(err, app.ErrNotPaired):
		return "Сначала пригласи партнёра."
	case errors.Is(err, store.ErrNotFound), errors.Is(err, app.ErrNoRound):
		return "Не нашёл — возможно, уже удалено."
	}
	return ""
}

func urlEscape(s string) string {
	var sb strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("-_.~", c) >= 0 {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

// Poll receives updates by long polling — for servers that Telegram cannot reach (inbound filtered).
func (b *Bot) Poll(ctx context.Context) {
	if err := b.TG.DeleteWebhook(ctx); err != nil {
		b.Log.Warn("deleteWebhook", "err", err)
	}
	var offset int64
	backoff := time.Second
	b.Log.Info("polling Telegram for updates")
	for ctx.Err() == nil {
		ups, err := b.TG.GetUpdates(ctx, offset, 50*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.Log.Warn("getUpdates", "err", err, "retry_in", backoff.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			func() {
				hctx, cancel := context.WithTimeout(ctx, 40*time.Second)
				defer cancel()
				defer func() {
					if p := recover(); p != nil {
						b.Log.Error("panic in update", "panic", p)
					}
				}()
				if fresh, err := b.App.S.FirstSeen(hctx, u.UpdateID, b.App.Now()); err == nil && !fresh {
					return
				}
				b.Handle(hctx, u)
			}()
		}
	}
}
