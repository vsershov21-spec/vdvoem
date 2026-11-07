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
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

type Bot struct {
	App    *app.App
	Helper *assistant.Assistant
	TG     *tg.Client
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
	if len(m.Photo) > 0 {
		ok, _ := b.App.Photo(ctx, c, m.Photo[len(m.Photo)-1].FileID)
		if ok {
			b.say(ctx, chat, "Сохранил фото к вашей встрече.", nil)
		} else {
			b.say(ctx, chat, "Фото сохраняю к встречам — пришли его после того, как сходите.", nil)
		}
		return
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

const helpText = `<b>Как это работает</b>
1. Кидайте в бота ссылки и идеи — куда сходить или что сделать. Я разберу и сложу в общую копилку.
2. Каждый отмечает: хочу, очень хочу или пропущу. Голос партнёра вы не видите, пока не отметите сами.
3. Когда пора выбрать (/go или сам по расписанию), каждый отмечает удобное время. Я нахожу пересечение и предлагаю три варианта.
4. Совпало — подтвердите оба, и я пришлю событие в календарь. Без двух «да» ничего не планируется.

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
	if parse.FirstURL(text) == "" && looksLikeQuestion(text) {
		b.ask(ctx, c, chat, text)
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
		b.say(ctx, m.Chat.ID, fmt.Sprintf("Привет, %s! Вы с %s теперь вдвоём. Кидайте сюда ссылки и идеи — я сложу их в общую копилку.", html.EscapeString(c.Me.Name), html.EscapeString(c.Partner.Name)), b.openApp(""))
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
		if errors.Is(err, app.ErrRoundOpen) {
			toast, err = "У вас уже идёт выбор", nil
		}
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
	case "aa":
		rep, e := b.Helper.Act(ctx, c, arg(1))
		err = e
		if e == nil {
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
