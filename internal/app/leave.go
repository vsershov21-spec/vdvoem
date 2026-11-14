package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// Leaving the pair, the file with one's own data, and deleting oneself. What happens to every table is in
// store/leave.go; here the two people are told.

const noteLeft int64 = -3 // the row «… вышел из пары» on the home screen of the one who stayed

// DeleteWord: what is typed to confirm «Удалить мои данные»
const DeleteWord = "удалить"

var ErrNotConfirmed = errors.New("not_confirmed")

// LeavePair: I leave. The pair's ideas, meetings and history stay with the partner; I get copies of the ideas
// I added and of my chores, in a space of my own. Returns me as I am now and how many ideas came along.
func (a *App) LeavePair(ctx context.Context, c Couple) (Couple, int, error) {
	if c.Partner == nil {
		return c, 0, ErrNotPaired
	}
	left, err := a.S.LeavePair(ctx, c.Me.ID, a.now())
	if errors.Is(err, store.ErrNotFound) {
		return c, 0, ErrNotPaired
	}
	if err != nil {
		return c, 0, err
	}
	a.toldLeft(ctx, c, left, false)
	if hadPlanned(left) && !a.Subscribed(ctx, c.Me.ID) {
		a.sendQuiet(ctx, c.Me.ChatID, "Если встреча уже в календаре телефона, удали её там.", nil)
	}
	nc, err := a.CoupleByMember(ctx, c.Me.ID)
	if err != nil {
		return c, left.Ideas, err
	}
	a.Live.From(ctx, nc.Space.ID)
	return nc, left.Ideas, nil
}

func hadPlanned(l store.Left) bool {
	for _, m := range l.Cancelled {
		if m.Planned {
			return true
		}
	}
	return false
}

// toldLeft: the one who stayed learns it in the two places — a row of «Ждёт тебя» and the same line in the bot.
// forgot: the leaver deleted their data, so their name is not written anywhere.
func (a *App) toldLeft(ctx context.Context, c Couple, left store.Left, forgot bool) {
	p := left.Partner
	if p == nil {
		return
	}
	sp, err := a.S.Space(ctx, left.PairSpace)
	if err != nil {
		return
	}
	title, author := fmt.Sprintf("%s %s из пары", c.Me.Name, Past(c.Me.Name, "вышел")), c.Me.ID
	if forgot {
		title, author = "Партнёр вышел из пары и удалил свои данные", 0
	}
	sub := "Идеи и история остались у тебя"
	if n := len(left.Cancelled); n > 0 {
		names := []string{}
		for _, m := range left.Cancelled {
			w := "«" + m.Title + "»"
			if m.Planned {
				w += " " + ruDate(m.StartsAt.In(sp.Loc()))
			}
			names = append(names, w)
		}
		head := "Встреча отменена: "
		if n > 1 {
			head = "Встречи отменены: "
		}
		sub += ". " + head + strings.Join(names, ", ")
	}
	if _, err := a.S.CreateProposal(ctx, domain.Proposal{SpaceID: left.PairSpace, Kind: domain.ProposalNote, RefID: noteLeft, AuthorID: author,
		Title: clip(title, 120), Sub: clip(sub, 120)}, a.now()); err != nil {
		a.Log.Warn("leave note", "err", err)
	}
	a.send(ctx, p.ChatID, fmt.Sprintf("<b>%s.</b> %s.", esc(title), esc(sub)), tg.Keyboard(tg.Row(tg.App("Открыть «Вдвоём»", a.appURL("")))))
	if hadPlanned(left) && !a.Subscribed(ctx, p.ID) {
		a.sendQuiet(ctx, p.ChatID, "Если встреча уже в календаре телефона, удали её там.", nil)
	}
	a.Live.From(ctx, left.PairSpace)
}

// ForgetMe: «Удалить мои данные». Cannot be undone, so the word must be typed. The partner keeps everything
// the pair had, without my name, marks and ratings; what was only mine is gone, and so am I: the next
// /start makes a new person.
func (a *App) ForgetMe(ctx context.Context, c Couple, word string) error {
	if strings.ToLower(strings.TrimSpace(word)) != DeleteWord {
		return ErrNotConfirmed
	}
	left, err := a.S.ForgetMember(ctx, c.Me.ID, a.now())
	if err != nil {
		return err
	}
	busyMem.mu.Lock()
	delete(busyMem.data, c.Me.ID)
	busyMem.mu.Unlock()
	a.toldLeft(ctx, c, left, true)
	if _, err := a.TG.Send(ctx, c.Me.ChatID, "Твои данные удалены. Захочешь вернуться, нажми /start.", tg.SendOpts{}); err != nil {
		a.Log.Warn("send failed", "err", err)
	}
	a.Live.From(ctx, c.Space.ID)
	return nil
}

// ---------- «Скачать мои идеи и историю» ----------

type exportIdea struct {
	Title   string `json:"название"`
	URL     string `json:"ссылка,omitempty"`
	Address string `json:"адрес,omitempty"`
	When    string `json:"когда,omitempty"`
	Price   string `json:"цена,omitempty"`
	Note    string `json:"заметка,omitempty"`
	Wish    string `json:"моя отметка,omitempty"`
	Added   string `json:"добавлена"`
}

type exportMeet struct {
	Title string `json:"что"`
	When  string `json:"когда,omitempty"`
	Rate  string `json:"моя оценка,omitempty"`
}

type exportChore struct {
	Title string `json:"дело"`
	Done  string `json:"сделано,omitempty"`
}

type Export struct {
	At       string        `json:"выгружено"`
	Name     string        `json:"имя"`
	Ideas    []exportIdea  `json:"мои идеи"`
	Meetings []exportMeet  `json:"история встреч"`
	Chores   []exportChore `json:"сделанные дела"`
	Taste    *MyTaste      `json:"мой вкус,omitempty"`
	Memories []string      `json:"память помощника,omitempty"`
}

var wishWords = map[domain.Wish]string{domain.WishWant: "хочу", domain.WishVery: "очень хочу", domain.WishSkip: "не моё"}
var rateWords = map[string]string{"yes": "понравилось", "no": "не очень", domain.AgainSkip: "не ходили"}

// ExportData: what is mine, as a file a person can read. From the partner there is only what the two share
// by nature — the name of an idea we went to and the date of the meeting. Their marks, ratings and what they
// typed into my ideas are not here.
func (a *App) ExportData(ctx context.Context, c Couple) (Export, error) {
	loc := c.Loc()
	day := func(t time.Time) string { return t.In(loc).Format("02.01.2006") }
	at := func(t time.Time) string { return t.In(loc).Format("02.01.2006 15:04") }
	out := Export{At: at(a.now()), Name: c.Me.Name, Ideas: []exportIdea{}, Meetings: []exportMeet{}, Chores: []exportChore{}}
	ideas, err := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive, domain.IdeaDraft, domain.IdeaDone)
	if err != nil {
		return out, err
	}
	for _, i := range ideas {
		if i.AuthorID != c.Me.ID || i.Source != "" || i.Kind == domain.KindChore {
			continue
		}
		theirs := func(field string) bool { // the partner typed this field
			e, ok := a.S.IdeaEdits(ctx, i.ID)[field]
			return ok && e.MemberID != c.Me.ID
		}
		e := exportIdea{Title: i.Title, URL: i.URL, Added: day(i.CreatedAt)}
		if !theirs("where") {
			e.Address = i.Address
		}
		if !theirs("note") {
			e.Note = i.Note
		}
		if i.StartsAt != nil && !theirs("when") {
			e.When = at(*i.StartsAt)
		}
		if (i.PriceMin > 0 || i.PriceMax > 0) && !theirs("price") {
			e.Price = priceWords(i.PriceMin, i.PriceMax)
		}
		w := i.WishA
		if c.Me.Color == "b" {
			w = i.WishB
		}
		e.Wish = wishWords[w]
		out.Ideas = append(out.Ideas, e)
	}
	rs, err := a.S.RecentRounds(ctx, c.Space.ID, 1000)
	if err != nil {
		return out, err
	}
	for k := len(rs) - 1; k >= 0; k-- { // oldest first
		r := rs[k]
		if r.State != domain.StateDone && !(r.State == domain.StatePlanned && r.StartsAt.Before(a.now())) {
			continue
		}
		m := exportMeet{Title: "Встреча"}
		if i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen); err == nil {
			m.Title = i.Title
		}
		if !r.StartsAt.IsZero() {
			m.When = at(r.StartsAt)
		}
		mine := r.AgainA
		if c.Me.Color == "b" {
			mine = r.AgainB
		}
		m.Rate = rateWords[mine]
		out.Meetings = append(out.Meetings, m)
	}
	done, err := a.S.DoneChores(ctx, c.Space.ID, "", c.Me.ID, 5000)
	if err != nil {
		return out, err
	}
	for k := len(done) - 1; k >= 0; k-- {
		ch := exportChore{Title: done[k].Title}
		if done[k].DoneAt != nil {
			ch.Done = day(*done[k].DoneAt)
		}
		out.Chores = append(out.Chores, ch)
	}
	if t := a.Taste(ctx, c.Me.ID); !t.Empty() {
		out.Taste = &t
	}
	if ms, err := a.S.Memories(ctx, c.Space.ID); err == nil {
		for k := len(ms) - 1; k >= 0; k-- {
			if ms[k].MemberID == c.Me.ID {
				out.Memories = append(out.Memories, ms[k].Text)
			}
		}
	}
	return out, nil
}

func priceWords(min, max int64) string {
	rub := func(k int64) string { return fmt.Sprintf("%d ₽", k/100) }
	switch {
	case min > 0 && max > min:
		return rub(min) + " – " + rub(max)
	case min > 0:
		return rub(min)
	}
	return "до " + rub(max)
}

// SendExport: the file goes to the person's chat with the bot
func (a *App) SendExport(ctx context.Context, c Couple) error {
	ex, err := a.ExportData(ctx, c)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return err
	}
	name := "vdvoem-" + a.now().In(c.Loc()).Format("2006-01-02") + ".json"
	caption := fmt.Sprintf("Твои идеи и история: идей %d, встреч %d, сделанных дел %d.", len(ex.Ideas), len(ex.Meetings), len(ex.Chores))
	return a.TG.SendDocument(ctx, c.Me.ChatID, name, b, caption)
}
