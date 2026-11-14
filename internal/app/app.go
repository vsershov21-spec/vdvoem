// Package app is the use-case layer: the bot and the Mini App both call it, so a vote or a confirmation
// means the same thing whichever button was pressed.
package app

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"vdvoem/internal/ai"
	"vdvoem/internal/domain"
	"vdvoem/internal/live"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// Notifier sends messages; *tg.Client implements it.
type Notifier interface {
	Send(ctx context.Context, chatID int64, html string, o tg.SendOpts) (tg.Message, error)
	SendDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) error
	SendPhoto(ctx context.Context, chatID int64, fileID, caption string) error
}

type App struct {
	S         *store.Store
	TG        Notifier
	AI        *ai.Client
	Fetch     *http.Client
	Now       func() time.Time
	PublicURL string // https://vdvoem.vershov.com
	BotName   string // vdvoem_bot
	Log       *slog.Logger
	NoKick    bool      // tests drive jobs through Tick
	Live      *live.Hub // open apps refresh in real time (nil = off)
}

var (
	ErrNoSpace   = errors.New("no_space")
	ErrNotPaired = errors.New("not_paired")
	ErrBadTime   = errors.New("bad_time")
	ErrForbidden = errors.New("forbidden")
	ErrNoRound   = errors.New("no_round")
)

func (a *App) now() time.Time { return a.Now() }

func (a *App) appURL(path string) string {
	return strings.TrimRight(a.PublicURL, "/") + "/app/" + strings.TrimLeft(path, "/")
}

func esc(s string) string { return html.EscapeString(s) }

// ---------- people ----------

type Couple struct {
	Space   store.Space
	Me      store.Member
	Partner *store.Member
}

func (c Couple) Loc() *time.Location { return c.Space.Loc() }

func (c Couple) PartnerName() string {
	if c.Partner == nil {
		return "партнёр"
	}
	return c.Partner.Name
}

func (a *App) Couple(ctx context.Context, tgID int64) (Couple, error) {
	m, err := a.S.MemberByTG(ctx, tgID)
	if errors.Is(err, store.ErrNotFound) {
		return Couple{}, ErrNoSpace
	}
	if err != nil {
		return Couple{}, err
	}
	return a.coupleOf(ctx, m)
}

func (a *App) coupleOf(ctx context.Context, m store.Member) (Couple, error) {
	sp, err := a.S.Space(ctx, m.SpaceID)
	if err != nil {
		return Couple{}, err
	}
	c := Couple{Space: sp, Me: m}
	ms, err := a.S.Members(ctx, sp.ID)
	if err != nil {
		return c, err
	}
	for i := range ms {
		if ms[i].ID != m.ID {
			p := ms[i]
			c.Partner = &p
		}
	}
	return c, nil
}

func (a *App) both(c Couple) []store.Member {
	out := []store.Member{c.Me}
	if c.Partner != nil {
		out = append(out, *c.Partner)
	}
	return out
}

func (a *App) send(ctx context.Context, chatID int64, text string, mk *tg.Markup) {
	if _, err := a.TG.Send(ctx, chatID, text, tg.SendOpts{Markup: mk}); err != nil {
		a.Log.Warn("send failed", "err", err)
	}
}

func (a *App) sendQuiet(ctx context.Context, chatID int64, text string, mk *tg.Markup) {
	if _, err := a.TG.Send(ctx, chatID, text, tg.SendOpts{Markup: mk, Silent: true}); err != nil {
		a.Log.Warn("send failed", "err", err)
	}
}

func code() string {
	b := make([]byte, 10)
	rand.Read(b)
	return strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))[:12]
}

type Profile struct {
	TGID   int64
	ChatID int64
	Name   string
	Photo  string
}

// Start handles /start: joins by invite, or creates a solo space with an invite link.
func (a *App) Start(ctx context.Context, p Profile, payload string) (Couple, string, error) {
	now := a.now()
	if c, err := a.Couple(ctx, p.TGID); err == nil {
		_ = a.S.UpdateMemberProfile(ctx, c.Me.ID, p.Name, p.Photo, p.ChatID)
		if strings.HasPrefix(payload, "inv_") && c.Partner == nil {
			// already in a solo space and opening someone else's invite: move into theirs if it is valid
			return a.join(ctx, p, strings.TrimPrefix(payload, "inv_"), &c)
		}
		return c, "", nil
	}
	if strings.HasPrefix(payload, "inv_") {
		return a.join(ctx, p, strings.TrimPrefix(payload, "inv_"), nil)
	}
	sp, err := a.S.CreateSpace(ctx, now)
	if err != nil {
		return Couple{}, "", err
	}
	m, err := a.S.AddMember(ctx, store.Member{SpaceID: sp.ID, TGUserID: p.TGID, ChatID: p.ChatID, Name: p.Name, PhotoURL: p.Photo}, now)
	if err != nil {
		return Couple{}, "", err
	}
	c, _ := a.coupleOf(ctx, m)
	link, err := a.NewInvite(ctx, c)
	return c, link, err
}

func (a *App) NewInvite(ctx context.Context, c Couple) (string, error) {
	cd := code()
	if err := a.S.CreateInvite(ctx, c.Space.ID, cd, a.now().Add(72*time.Hour)); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://t.me/%s?start=inv_%s", a.BotName, cd), nil
}

var ErrInviteUsed = errors.New("invite_invalid")

func (a *App) join(ctx context.Context, p Profile, cd string, solo *Couple) (Couple, string, error) {
	now := a.now()
	spaceID, err := a.S.UseInvite(ctx, cd, now)
	if err != nil {
		return Couple{}, "", ErrInviteUsed
	}
	if solo != nil {
		if solo.Space.ID == spaceID {
			return *solo, "", nil
		}
		// leave the empty solo space
		_ = a.S.DeleteSpace(ctx, solo.Space.ID)
	}
	m, err := a.S.AddMember(ctx, store.Member{SpaceID: spaceID, TGUserID: p.TGID, ChatID: p.ChatID, Name: p.Name, PhotoURL: p.Photo}, now)
	if err != nil {
		return Couple{}, "", err
	}
	c, err := a.coupleOf(ctx, m)
	if err == nil && c.Partner != nil {
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("<b>%s с тобой.</b> Теперь у вас общая копилка. Пришли ссылку на место или напиши, куда хочется.", esc(c.Me.Name)),
			tg.Keyboard(tg.Row(tg.App("Открыть «Вдвоём»", a.appURL("")))))
	}
	return c, "", err
}

// ---------- ideas ----------

type AddResult struct {
	Idea     store.Idea
	Existing *store.Idea // exact duplicate: nothing was created
	Similar  *store.Idea // similar title: created, the bot asks «Это то же?»
}

// AddIdea saves first and parses later: the reply must not wait for the network.
func (a *App) AddIdea(ctx context.Context, c Couple, text string) (AddResult, error) {
	now := a.now()
	text = strings.TrimSpace(text)
	link := parse.FirstURL(text)
	title := strings.TrimSpace(strings.Replace(text, link, "", 1))
	if title == "" && link != "" {
		title = hostTitle(link)
	}
	if r := []rune(title); len(r) > 80 {
		title = string(r[:80])
	}
	fp := domain.Fingerprint(link, title)
	if ex, err := a.S.IdeaByFingerprint(ctx, c.Space.ID, fp); err == nil {
		return AddResult{Existing: &ex}, nil
	}
	kind := domain.KindLeisure
	cat := parse.GuessCategory("", title)
	if cat == "home" {
		kind = domain.KindChore
	}
	i, err := a.S.CreateIdea(ctx, store.Idea{Idea: domain.Idea{SpaceID: c.Space.ID, AuthorID: c.Me.ID, Title: title, URL: link, Kind: kind, Category: cat}}, now)
	if err != nil {
		return AddResult{}, err
	}
	res := AddResult{Idea: i}
	if sim := a.similar(ctx, c.Space.ID, i); sim != nil {
		res.Similar = sim
	}
	_ = a.S.Enqueue(ctx, "parse", now, map[string]any{"idea": i.ID, "text": text, "notify": c.Me.ChatID, "similar": similarID(res.Similar)}, fmt.Sprintf("parse:%d", i.ID))
	a.Kick()
	return res, nil
}

func similarID(i *store.Idea) int64 {
	if i == nil {
		return 0
	}
	return i.ID
}

func hostTitle(link string) string {
	h := strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
	h = strings.TrimPrefix(h, "www.")
	if i := strings.IndexAny(h, "/?#"); i > 0 {
		h = h[:i]
	}
	return "Ссылка с " + h
}

func (a *App) similar(ctx context.Context, spaceID int64, i store.Idea) *store.Idea {
	ideas, _ := a.S.Ideas(ctx, spaceID, domain.IdeaActive)
	best, bs := (*store.Idea)(nil), 0.0
	for k := range ideas {
		o := ideas[k]
		if o.ID == i.ID {
			continue
		}
		if s := domain.Similarity(o.Title, i.Title); s >= domain.SimilarThreshold && s > bs {
			best, bs = &o, s
		}
	}
	return best
}

func (a *App) SetWish(ctx context.Context, c Couple, ideaID int64, w domain.Wish) (store.Idea, error) {
	if !w.Valid() {
		return store.Idea{}, ErrForbidden
	}
	if err := a.S.SetWish(ctx, c.Space.ID, ideaID, c.Me.Color, w, a.now()); err != nil {
		return store.Idea{}, err
	}
	i, err := a.S.Idea(ctx, c.Space.ID, ideaID)
	if err == nil && i.Status == domain.IdeaDraft && w != domain.WishSkip { // a scout draft someone wants joins the bank
		i.Status = domain.IdeaActive
		_ = a.S.UpdateIdea(ctx, i, a.now())
	}
	a.Live.From(ctx, c.Space.ID)
	return i, err
}

// Merge: «Это то же» — the new idea goes away, the old one gets my wish if I had one.
func (a *App) Merge(ctx context.Context, c Couple, newID, intoID int64) error {
	n, err := a.S.Idea(ctx, c.Space.ID, newID)
	if err != nil {
		return err
	}
	if _, err := a.S.Idea(ctx, c.Space.ID, intoID); err != nil {
		return err
	}
	mine := n.WishA
	if c.Me.Color == "b" {
		mine = n.WishB
	}
	if mine != domain.WishNone {
		_ = a.S.SetWish(ctx, c.Space.ID, intoID, c.Me.Color, mine, a.now())
	}
	return a.S.DeleteIdea(ctx, c.Space.ID, newID)
}

func (a *App) UpdateIdea(ctx context.Context, c Couple, i store.Idea) (store.Idea, error) {
	cur, err := a.S.Idea(ctx, c.Space.ID, i.ID)
	if err != nil {
		return i, err
	}
	cur.Title, cur.Category, cur.Address, cur.Note, cur.URL = i.Title, i.Category, i.Address, i.Note, i.URL
	cur.PriceMin, cur.PriceMax, cur.DurationMin, cur.Kind = i.PriceMin, i.PriceMax, i.DurationMin, i.Kind
	cur.Deadline, cur.Assignee, cur.EndsAt = i.Deadline, i.Assignee, i.EndsAt
	if i.Status != "" {
		cur.Status = i.Status
	}
	if err := a.S.UpdateIdea(ctx, cur, a.now()); err != nil {
		return cur, err
	}
	return a.S.Idea(ctx, c.Space.ID, i.ID)
}

// ---------- the parse job ----------

type parsePayload struct {
	Idea    int64  `json:"idea"`
	Text    string `json:"text"`
	Notify  int64  `json:"notify"`
	Similar int64  `json:"similar"`
}

// mergeParsed applies parsed values only to fields nobody touched since parsing started.
func mergeParsed(orig, parsed store.Idea, st *store.Store, ctx context.Context) store.Idea {
	cur, err := st.IdeaAny(ctx, orig.ID)
	if err != nil {
		return parsed
	}
	out := cur
	pick := func(changed bool, apply func()) {
		if !changed {
			apply()
		}
	}
	pick(cur.Title != orig.Title, func() { out.Title = parsed.Title; out.Fingerprint = parsed.Fingerprint })
	pick(cur.Address != orig.Address, func() { out.Address = parsed.Address })
	pick(cur.ImageURL != orig.ImageURL, func() { out.ImageURL = parsed.ImageURL })
	pick(cur.Category != orig.Category || cur.Kind != orig.Kind, func() { out.Category, out.Kind = parsed.Category, parsed.Kind })
	pick(cur.PriceMin != orig.PriceMin || cur.PriceMax != orig.PriceMax, func() { out.PriceMin, out.PriceMax = parsed.PriceMin, parsed.PriceMax })
	pick(cur.DurationMin != orig.DurationMin, func() { out.DurationMin = parsed.DurationMin })
	pick(cur.Note != orig.Note, func() { out.Note = parsed.Note })
	pick(!timeEq(cur.EndsAt, orig.EndsAt), func() { out.EndsAt = parsed.EndsAt })
	pick(!timeEq(cur.StartsAt, orig.StartsAt), func() { out.StartsAt = parsed.StartsAt })
	return out
}

func timeEq(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func (a *App) runParse(ctx context.Context, p parsePayload) error {
	i, err := a.S.IdeaAny(ctx, p.Idea)
	if err != nil {
		return nil // deleted meanwhile
	}
	orig := i
	if i.URL != "" && a.Fetch != nil {
		r, err := parse.Fetch(ctx, a.Fetch, i.URL)
		if err != nil {
			a.Log.Info("link fetch failed", "idea", i.ID, "err", err)
		}
		if r.Title != "" && (strings.HasPrefix(i.Title, "Ссылка с ") || i.Title == "") {
			i.Title = r.Title
		}
		if r.Address != "" {
			i.Address = r.Address
		}
		if r.Image != "" {
			i.ImageURL = r.Image
		}
		if r.Category != "" && r.Category != "other" {
			i.Category = r.Category
		}
		if r.PriceRub > 0 {
			i.PriceMin = r.PriceRub * 2 * 100 // per person → for two, in kopecks
			i.PriceMax = i.PriceMin
		}
		if r.EndsAt != nil {
			i.EndsAt = r.EndsAt
		}
		if r.StartsAt != nil {
			i.StartsAt = r.StartsAt
		}
		if i.Note == "" && r.Description != "" {
			d := []rune(r.Description)
			if len(d) > 200 {
				d = d[:200]
			}
			i.Note = string(d)
		}
		i.Fingerprint = domain.Fingerprint(i.URL, i.Title)
	} else if a.AI.Enabled() {
		if g, err := a.AI.ParseIdea(ctx, p.Text); err == nil {
			i.Title, i.Category = g.Title, g.Category
			i.Kind = domain.IdeaKind(g.Kind)
			if g.PriceRub > 0 {
				i.PriceMin, i.PriceMax = g.PriceRub*100, g.PriceRub*100
			}
			if g.DurationMin > 0 {
				i.DurationMin = g.DurationMin
			}
		}
	}
	if i.DurationMin == 0 {
		i.DurationMin = defaultDuration(i.Category)
	}
	// parsing takes seconds: whatever a person edited meanwhile wins over the parser
	i = mergeParsed(orig, i, a.S, ctx)
	i.Parsed = true
	if err := a.S.UpdateIdea(ctx, i, a.now()); err != nil {
		return err
	}
	if p.Notify != 0 {
		a.sendIdeaCard(ctx, p.Notify, i, p.Similar)
	}
	// the partner learns quietly, batched: one notice per 3 hours
	m, err := a.S.IdeaAny(ctx, i.ID)
	if err == nil {
		ms, _ := a.S.Members(ctx, m.SpaceID)
		for _, mem := range ms {
			if mem.ID != m.AuthorID {
				slot := a.now().Unix() / (3 * 3600)
				_ = a.S.Enqueue(ctx, "partner_new", a.now().Add(10*time.Minute), map[string]any{"member": mem.ID, "space": m.SpaceID, "since": a.now().Add(-3 * time.Hour).Unix()},
					fmt.Sprintf("partner_new:%d:%d", mem.ID, slot))
			}
		}
	}
	return nil
}

func defaultDuration(cat string) int {
	switch cat {
	case "food":
		return 120
	case "culture":
		return 150
	case "walk":
		return 90
	case "sport":
		return 90
	case "home":
		return 60
	}
	return 120
}

var catName = map[string]string{"food": "Еда", "culture": "Культура", "walk": "Прогулка", "sport": "Спорт", "home": "Дела", "other": "Другое"}

func IdeaMeta(i store.Idea) string {
	parts := []string{catName[i.Category]}
	if i.Address != "" {
		parts = append(parts, i.Address)
	}
	if i.PriceMin > 0 {
		parts = append(parts, "≈ "+domain.Money(i.PriceMin)+" на двоих")
	}
	if i.DurationMin > 0 {
		parts = append(parts, durText(i.DurationMin))
	}
	return strings.Join(parts, " · ")
}

func durText(m int) string {
	if m%60 == 0 {
		return fmt.Sprintf("%d ч", m/60)
	}
	if m > 60 {
		return fmt.Sprintf("%.1f ч", float64(m)/60)
	}
	return fmt.Sprintf("%d мин", m)
}

// ideaBatch: several ideas in a row (a list pasted, a channel forwarded) — the first gets its card, the rest
// one summary a couple of minutes later, never a dozen identical messages
const ideaBatch = 2 * time.Minute

func (a *App) sendIdeaCard(ctx context.Context, chatID int64, i store.Idea, similar int64) {
	if similar == 0 {
		// the first two get their own card (a person typing ideas one by one sees each); from the third on — one summary
		var recent []store.Idea
		if all, err := a.S.Ideas(ctx, i.SpaceID, domain.IdeaActive, domain.IdeaDraft); err == nil {
			for _, x := range all {
				if x.ID != i.ID && x.AuthorID == i.AuthorID && x.CreatedAt.After(a.now().Add(-ideaBatch)) && earlier(x, i) {
					recent = append(recent, x)
				}
			}
		}
		if len(recent) >= 2 {
			sort.Slice(recent, func(p, q int) bool { return earlier(recent[p], recent[q]) })
			anchor := recent[1] // the last one that got a card
			_ = a.S.Enqueue(ctx, "idea_batch", recent[0].CreatedAt.Add(ideaBatch+30*time.Second), map[string]any{"chat": chatID, "space": i.SpaceID, "author": i.AuthorID, "first": anchor.ID},
				fmt.Sprintf("idea_batch:%d:%d", chatID, anchor.ID))
			return
		}
	}
	text := fmt.Sprintf("Сохранил в копилку:\n\n<b>%s</b>\n%s", esc(i.Title), esc(IdeaMeta(i)))
	rows := [][]tg.Button{
		tg.Row(tg.CB("Хочу", fmt.Sprintf("w:%d:want", i.ID)), tg.CB("Очень хочу", fmt.Sprintf("w:%d:very", i.ID))),
		tg.Row(tg.CB("Пропущу", fmt.Sprintf("w:%d:skip", i.ID)), tg.App("Исправить", a.appURL(fmt.Sprintf("idea/%d", i.ID)))),
	}
	if i.Kind == domain.KindChore { // a chore is done, not wanted
		rows = [][]tg.Button{tg.Row(tg.CB("✓ Сделано", fmt.Sprintf("cd:%d", i.ID)), tg.App("Исправить", a.appURL(fmt.Sprintf("idea/%d", i.ID))))}
	}
	if Generic(i) {
		city := "Москва"
		if sp, err := a.S.Space(ctx, i.SpaceID); err == nil && sp.City != "" {
			city = sp.City
		}
		text += "\n\n📍 Где именно? Ответь ссылкой (Яндекс Карты, 2ГИС, сайт) — или потом, когда соберётесь."
		place := []tg.Button{tg.Link("Найти на картах", MapsLink(i.Title, city))}
		if a.AI.Enabled() {
			place = append(place, tg.CB("✨ Подобрать места", fmt.Sprintf("sp:%d", i.ID)))
		}
		rows = append(rows, place)
	}
	if similar != 0 {
		if s, err := a.S.IdeaAny(ctx, similar); err == nil {
			text += fmt.Sprintf("\n\nПохоже на «%s» — она уже в копилке.", esc(s.Title))
			rows = append(rows, tg.Row(tg.CB("Это то же", fmt.Sprintf("mg:%d:%d", i.ID, s.ID)), tg.CB("Оставить обе", fmt.Sprintf("nm:%d", i.ID))))
		}
	}
	if m, err := a.TG.Send(ctx, chatID, text, tg.SendOpts{Markup: tg.Keyboard(rows...)}); err == nil {
		a.S.SaveCard(ctx, chatID, m.MessageID, i.ID)
	} else {
		a.Log.Warn("send failed", "err", err)
	}
}

func earlier(x, y store.Idea) bool {
	return x.CreatedAt.Before(y.CreatedAt) || x.CreatedAt.Equal(y.CreatedAt) && x.ID < y.ID
}

func (a *App) sendIdeaBatch(ctx context.Context, chat, space, author, first int64) error {
	all, err := a.S.Ideas(ctx, space, domain.IdeaActive, domain.IdeaDraft)
	if err != nil {
		return err
	}
	var anchor store.Idea
	for _, x := range all {
		if x.ID == first {
			anchor = x
		}
	}
	if anchor.ID == 0 {
		return nil
	}
	var lines []string
	n := 0
	for _, x := range all {
		if x.AuthorID != author || !earlier(anchor, x) || x.CreatedAt.Sub(anchor.CreatedAt) > 2*ideaBatch || !x.Parsed {
			continue
		}
		n++
		if len(lines) < 12 {
			lines = append(lines, "• "+esc(x.Title))
		}
	}
	if n == 0 {
		return nil
	}
	if n > len(lines) {
		lines = append(lines, fmt.Sprintf("…и ещё %d", n-len(lines)))
	}
	text := fmt.Sprintf("И ещё %d %s в копилке:\n%s", n, plural(n, "идея", "идеи", "идей"), strings.Join(lines, "\n"))
	_, err = a.TG.Send(ctx, chat, text, tg.SendOpts{Markup: tg.Keyboard(tg.Row(tg.App("Отметить по одной", a.appURL("triage"))))})
	return err
}

// ---------- rounds ----------

// SlotDays: Mon–Thu → the coming Fri, Sat, Sun; otherwise today and the next two days.
func SlotDays(now time.Time, loc *time.Location) []time.Time {
	l := now.In(loc)
	d0 := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	wd := int(l.Weekday())
	start := d0
	if wd >= 1 && wd <= 4 {
		start = d0.AddDate(0, 0, 5-wd)
	} else if l.Hour() >= 20 {
		start = d0.AddDate(0, 0, 1)
	}
	return []time.Time{start, start.AddDate(0, 0, 1), start.AddDate(0, 0, 2)}
}

func (a *App) Go(ctx context.Context, c Couple) (domain.Round, error) { return a.GoOn(ctx, c, nil) }

// GoOn starts a round; days (three YYYY-MM-DD) come from the calendar, nil means the coming weekend.
func (a *App) GoOn(ctx context.Context, c Couple, days []string) (domain.Round, error) {
	if c.Partner == nil {
		return domain.Round{}, ErrNotPaired
	}
	if r, err := a.S.OpenRound(ctx, c.Space.ID); err == nil {
		return r, nil
	}
	nr := domain.NewRound(c.Space.ID, a.now(), c.Space.BudgetKop)
	nr.Days = days
	r, err := a.S.CreateRound(ctx, nr, a.now())
	if errors.Is(err, store.ErrConflict) {
		return a.S.OpenRound(ctx, c.Space.ID)
	}
	if err != nil {
		return r, err
	}
	who := c.Me.Name
	for _, m := range a.both(c) {
		lead := "Давай выберем, куда сходить."
		if m.ID != c.Me.ID {
			lead = fmt.Sprintf("%s предлагает куда-нибудь сходить.", esc(who))
		}
		a.send(ctx, m.ChatID, lead+" <b>Когда тебе удобно?</b> Отметь в сетке — где совпадёте, там и подберу варианты.",
			tg.Keyboard(tg.Row(tg.App("Отметить время", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
	}
	return r, nil
}

// ScheduledGo opens a round for the weekly plan day (no-op if one is open or a meeting is planned soon).
func (a *App) ScheduledGo(ctx context.Context, sp store.Space) error {
	if _, err := a.S.OpenRound(ctx, sp.ID); err == nil {
		return nil
	}
	if r, err := a.S.NextPlanned(ctx, sp.ID); err == nil && r.StartsAt.After(a.now()) && r.StartsAt.Sub(a.now()) < 7*24*time.Hour {
		return nil
	}
	ms, err := a.S.Members(ctx, sp.ID)
	if err != nil || len(ms) < 2 {
		return err
	}
	r, err := a.S.CreateRound(ctx, domain.NewRound(sp.ID, a.now(), sp.BudgetKop), a.now())
	if err != nil {
		return err
	}
	for _, m := range ms {
		a.send(ctx, m.ChatID, "Время планировать выходные. <b>Когда вам удобно?</b>",
			tg.Keyboard(tg.Row(tg.App("Отметить время", a.appURL(fmt.Sprintf("round/%d", r.ID)))), tg.Row(tg.CB("Не в этот раз", fmt.Sprintf("x:%d", r.ID)))))
	}
	return nil
}

func (a *App) round(ctx context.Context, c Couple, id int64) (domain.Round, error) {
	if id == 0 {
		return a.S.OpenRound(ctx, c.Space.ID)
	}
	return a.S.Round(ctx, c.Space.ID, id)
}

// save retries are the caller's business: a version conflict surfaces as store.ErrConflict.
func (a *App) save(ctx context.Context, r domain.Round, ver int) error {
	err := a.S.SaveRound(ctx, r, ver, a.now())
	if err == nil {
		a.Live.From(ctx, r.SpaceID)
	}
	return err
}

// SlotHorizon: how far ahead free time can be marked (the round suggests three days, any day up to this is fine).
const (
	SlotHorizon = 60 // days
	maxSlots    = 3 * 31
)

// SlotRange is the first and last day a member may mark now.
func SlotRange(now time.Time, loc *time.Location) (time.Time, time.Time) {
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	return today, today.AddDate(0, 0, SlotHorizon)
}

// parseSlotKeys keeps valid "YYYY-MM-DD:part" keys inside the horizon whose time has not passed yet.
func parseSlotKeys(keys []string, loc *time.Location, now time.Time) []domain.Slot {
	first, last := SlotRange(now, loc)
	var out []domain.Slot
	seen := map[string]bool{}
	for _, k := range keys {
		key, hm, _ := strings.Cut(k, "@") // «2026-11-14:1» or, narrowed, «2026-11-14:1@14:00»
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || len(parts[1]) != 1 || parts[1][0] < '0' || int(parts[1][0]-'0') >= domain.Parts || seen[key] {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", parts[0], loc)
		if err != nil || d.Before(first) || d.After(last) {
			continue
		}
		sl := domain.Slot{Day: d, Part: int(parts[1][0] - '0')}
		if hm != "" {
			sl.At = domain.ParseAt(sl.Part, hm)
		}
		if !sl.End(loc).After(now) {
			continue
		}
		seen[key] = true
		out = append(out, sl)
		if len(out) == maxSlots {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start(loc).Before(out[j].Start(loc)) })
	return out
}

func (a *App) SubmitSlots(ctx context.Context, c Couple, roundID int64, keys []string) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver := r.Version
	slots := parseSlotKeys(keys, c.Loc(), a.now())
	both, err := r.SubmitSlots(c.Me.Color, slots, a.now())
	if err != nil {
		return r, err
	}
	if both {
		if err := a.startVoting(ctx, c, &r, nil); err != nil {
			return r, err
		}
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	if both && r.State == domain.StateConfirm { // the idea was set and the time now matches: that is the «да» of both
		return a.agree(ctx, c, r), nil
	}
	if both {
		a.announceVote(ctx, c, r)
	} else if c.Partner != nil { // the proposal in the open: the partner agrees in one tap or offers another time
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s предлагает время:\n%s\n\nПодходит — нажми. Нет — предложи своё.", esc(c.Me.Name), esc(slotList(slots, 6))),
			a.slotButtons(r.ID, slots, "Другое время"))
	}
	return r, nil
}

// AcceptSlot: «this time suits me» — the partner's slot joins mine, which makes the overlap.
func (a *App) AcceptSlot(ctx context.Context, c Couple, roundID int64, key string) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	mine := r.SlotsA
	if c.Me.Color == "b" {
		mine = r.SlotsB
	}
	keys := []string{key}
	base, _, _ := strings.Cut(key, "@")
	for _, s := range mine {
		if s.Key() != base {
			keys = append(keys, s.Code())
		}
	}
	return a.SubmitSlots(ctx, c, roundID, keys)
}

var partNames = [domain.Parts]string{"утро", "день", "вечер", "ночь"}

func slotLabel(s domain.Slot) string {
	w := partNames[s.Part]
	if s.At > 0 {
		w = fmt.Sprintf("с %02d:%02d", s.At/60%24, s.At%60)
	}
	return strings.ToLower(ruWeekdayShort(s.Day)) + " " + s.Day.Format("02.01") + " · " + w
}

func ruWeekdayShort(t time.Time) string {
	return [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}[t.Weekday()]
}

func slotList(ss []domain.Slot, n int) string {
	var l []string
	for k, s := range ss {
		if k == n {
			l = append(l, fmt.Sprintf("и ещё %d", len(ss)-n))
			break
		}
		l = append(l, "• "+slotLabel(s))
	}
	return strings.Join(l, "\n")
}

// slotButtons: up to four of the partner's slots as «✓ сб 15.11 · вечер», then «choose in the app»
func (a *App) slotButtons(roundID int64, ss []domain.Slot, other string) *tg.Markup {
	var rows [][]tg.Button
	for k, s := range ss {
		if k == 4 {
			break
		}
		rows = append(rows, tg.Row(tg.CB("✓ "+slotLabel(s), fmt.Sprintf("ts:%d:%s", roundID, s.Code()))))
	}
	rows = append(rows, tg.Row(tg.App(other, a.appURL(fmt.Sprintf("round/%d", roundID)))))
	return tg.Keyboard(rows...)
}

func (a *App) windows(c Couple, r domain.Round) []domain.Window {
	return domain.CommonWindows(r.SlotsA, r.SlotsB, c.Loc())
}

func (a *App) startVoting(ctx context.Context, c Couple, r *domain.Round, exclude map[int64]bool) error {
	wins := a.windows(c, *r)
	if len(wins) == 0 {
		return nil // stays in StateTime; announceVote tells them there is no overlap
	}
	triple, err := a.pick(ctx, c, *r, wins, exclude)
	if err != nil {
		return err
	}
	ids := make([]int64, len(triple))
	for k, s := range triple {
		ids[k] = s.Idea.ID
	}
	if r.Chosen != 0 { // «Другое время» kept the idea: skip the vote
		if r.State == domain.StateTime {
			r.State = domain.StateVote
		}
		if err := r.Choose(r.Chosen, a.now()); err != nil {
			return err
		}
		a.fixTime(ctx, c, r)
		return nil
	}
	if err := r.StartVoting(ids); err != nil {
		return err
	}
	return a.S.MarkShown(ctx, c.Space.ID, ids)
}

func (a *App) pick(ctx context.Context, c Couple, r domain.Round, wins []domain.Window, exclude map[int64]bool) ([]domain.Scored, error) {
	ideas, err := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive)
	if err != nil {
		return nil, err
	}
	recent := map[int64]bool{}
	rs, _ := a.S.RecentRounds(ctx, c.Space.ID, 3)
	for _, x := range rs {
		if x.ID == r.ID || x.State == domain.StateExpired {
			continue
		}
		for _, id := range x.Triple {
			recent[id] = true
		}
	}
	in := domain.PickInput{Windows: wins, BudgetKop: r.BudgetKop, RecentShown: recent, Exclude: exclude, Now: a.now()}
	for _, i := range ideas {
		in.Ideas = append(in.Ideas, i.Idea)
	}
	return domain.PickForTime(in, 6), nil
}

func (a *App) fixTime(ctx context.Context, c Couple, r *domain.Round) {
	i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	dur := domain.DefaultDuration
	if err == nil && i.DurationMin > 0 {
		dur = i.DurationMin
	}
	for _, w := range a.windows(c, *r) {
		if w.Minutes() >= dur {
			start := w.From
			if w.From.Hour() < 18 && w.To.Hour() >= 21 && (i.Category == "food" || i.Category == "culture") {
				// evening plans start in the evening even when the afternoon is free too
				start = time.Date(w.From.Year(), w.From.Month(), w.From.Day(), 19, 0, 0, 0, c.Loc())
				if start.Add(time.Duration(dur) * time.Minute).After(w.To) {
					start = w.From
				}
			}
			r.SetTime(start, dur)
			return
		}
	}
	if ws := a.windows(c, *r); len(ws) > 0 {
		r.SetTime(ws[0].From, dur)
	}
}

func (a *App) announceVote(ctx context.Context, c Couple, r domain.Round) {
	if r.State == domain.StateTime { // no overlap yet: not a dead end — each sees the other's time and picks one
		for _, m := range a.both(c) {
			theirs, name := r.SlotsB, "партнёр"
			if m.Color == "b" {
				theirs = r.SlotsA
			}
			for _, x := range a.both(c) {
				if x.ID != m.ID {
					name = x.Name
				}
			}
			a.send(ctx, m.ChatID, fmt.Sprintf("Пока не совпало. %s может:\n%s\n\nВыбери подходящее — и договорились. Или предложи другой день.", esc(name), esc(slotList(theirs, 6))),
				a.slotButtons(r.ID, theirs, "Предложить другое"))
		}
		return
	}
	if r.State == domain.StateConfirm { // the idea was chosen before and the time now matches
		a.agree(ctx, c, r)
		return
	}
	if len(r.Triple) == 0 {
		for _, m := range a.both(c) {
			a.send(ctx, m.ChatID, "В копилке пока нет идей под это время. Добавьте пару — и я соберу варианты.", nil)
		}
		return
	}
	var lines []string
	var rows [][]tg.Button
	for k, id := range r.Triple {
		i, err := a.S.Idea(ctx, c.Space.ID, id)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d. <b>%s</b>\n%s", k+1, esc(i.Title), esc(IdeaMeta(i))))
		if k%3 == 0 {
			rows = append(rows, nil)
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], tg.CB(fmt.Sprintf("%d", k+1), fmt.Sprintf("v:%d:%d", r.ID, id)))
	}
	rows = append(rows, tg.Row(tg.App("Открыть в приложении", a.appURL(fmt.Sprintf("round/%d", r.ID))), tg.CB("Другие", fmt.Sprintf("rs:%d", r.ID))))
	for _, m := range a.both(c) {
		a.send(ctx, m.ChatID, "Вы оба свободны. Куда идём? Выбери номер — выбор партнёра видно сразу.\n\n"+strings.Join(lines, "\n\n"), tg.Keyboard(rows...))
	}
}

// why: the AI may phrase the reason; the deterministic sentence is always the fallback.
func (a *App) why(ctx context.Context, c Couple, r domain.Round) string {
	if len(r.Triple) == 0 {
		return ""
	}
	wins := a.windows(c, r)
	trip, _ := a.pick(ctx, c, domain.Round{ID: r.ID, BudgetKop: r.BudgetKop}, wins, nil)
	var fav *domain.Scored
	for k := range trip {
		if trip[k].Idea.ID == r.Triple[0] {
			fav = &trip[k]
		}
	}
	if fav == nil {
		i, err := a.S.Idea(ctx, c.Space.ID, r.Triple[0])
		if err != nil {
			return ""
		}
		sc, sig := domain.Score(i.Idea, a.now())
		fav = &domain.Scored{Idea: i.Idea, Score: sc, Signals: sig}
	}
	base := domain.Why(*fav)
	if a.AI.Enabled() {
		facts := append([]string{base}, fav.Signals...)
		if s, err := a.AI.Why(ctx, fav.Idea.Title, facts); err == nil {
			return "Мой фаворит — «" + fav.Idea.Title + "»: " + lowerFirst(s)
		}
	}
	return "Мой фаворит — «" + fav.Idea.Title + "»: " + lowerFirst(base)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 1 && !(r[1] >= 'А' && r[1] <= 'Я') {
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}

func (a *App) Vote(ctx context.Context, c Couple, roundID, ideaID int64) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver := r.Version
	matched, err := r.Vote(c.Me.Color, ideaID, a.now())
	if err != nil {
		return r, err
	}
	if matched {
		a.fixTime(ctx, c, &r)
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	switch {
	case matched:
		return a.agree(ctx, c, r), nil
	case r.VotedBoth():
		a.announceMismatch(ctx, c, r)
	case c.Partner != nil:
		title := "ни один вариант"
		if i, err := a.S.Idea(ctx, c.Space.ID, ideaID); err == nil {
			title = "«" + i.Title + "»"
		}
		kb := tg.Keyboard(tg.Row(tg.App("Выбрать другое", a.appURL(fmt.Sprintf("round/%d", r.ID)))))
		if ideaID > 0 {
			kb = tg.Keyboard(tg.Row(tg.CB("Идём", fmt.Sprintf("v:%d:%d", r.ID, ideaID))), tg.Row(tg.App("Выбрать другое", a.appURL(fmt.Sprintf("round/%d", r.ID)))))
		}
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s за %s.", esc(c.Me.Name), esc(title)), kb)
	}
	return r, nil
}

// Compromise: an idea one voted for and the other marked «хочу» or «очень хочу».
func (a *App) Compromise(ctx context.Context, c Couple, r domain.Round) *store.Idea {
	for _, id := range []int64{r.VoteA, r.VoteB} {
		if id <= 0 {
			continue
		}
		i, err := a.S.Idea(ctx, c.Space.ID, id)
		if err == nil && i.WishA != domain.WishSkip && i.WishB != domain.WishSkip && i.WishA != domain.WishNone && i.WishB != domain.WishNone {
			return &i
		}
	}
	return nil
}

func (a *App) announceMismatch(ctx context.Context, c Couple, r domain.Round) {
	// picks are open and each already got «X за …» with «Идём»: a second message about the difference is noise.
	// Only the middle ground is worth a word.
	comp := a.Compromise(ctx, c, r)
	if comp == nil {
		return
	}
	for _, m := range a.both(c) {
		a.send(ctx, m.ChatID, fmt.Sprintf("Выбрали разное. Оба не против: <b>%s</b>.", esc(comp.Title)),
			tg.Keyboard(tg.Row(tg.CB("Идём туда", fmt.Sprintf("v:%d:%d", r.ID, comp.ID))), tg.Row(tg.App("Открыть в приложении", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
	}
}

func (a *App) Reshuffle(ctx context.Context, c Couple, roundID int64) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver := r.Version
	ex := map[int64]bool{}
	for _, id := range r.Shown {
		ex[id] = true
	}
	trip, err := a.pick(ctx, c, r, a.windows(c, r), ex)
	if err != nil {
		return r, err
	}
	if len(trip) == 0 {
		return r, errors.New("no_more_ideas")
	}
	ids := []int64{}
	for _, s := range trip {
		ids = append(ids, s.Idea.ID)
	}
	if err := r.Reshuffle(ids, a.now()); err != nil {
		return r, err
	}
	_ = a.S.MarkShown(ctx, c.Space.ID, ids)
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	a.announceVote(ctx, c, r)
	return r, nil
}

func (a *App) meetingRows(ctx context.Context, c Couple, r domain.Round) (store.Idea, string) {
	i, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	var rows []string
	rows = append(rows, "<b>Когда:</b> "+esc(domain.When(r.StartsAt, c.Loc()))+fmt.Sprintf("–%02d:%02d", r.EndsAt.In(c.Loc()).Hour(), r.EndsAt.In(c.Loc()).Minute()))
	if p := a.PlaceOf(ctx, c, i); p.Chosen {
		rows = append(rows, "<b>Где:</b> "+esc(p.Title)+addrLine(p.Address))
	} else if i.Address != "" {
		rows = append(rows, "<b>Где:</b> "+esc(i.Address))
	} else {
		rows = append(rows, "<b>Где:</b> место ещё не выбрано — добавьте варианты в карточке")
	}
	if i.PriceMin > 0 {
		rows = append(rows, "<b>Сколько:</b> ≈ "+domain.Money(i.PriceMin)+" на двоих")
	}
	if i.Category == "food" || i.Category == "culture" {
		rows = append(rows, "<b>Сделать заранее:</b> забронировать или купить билеты")
	}
	if rl := a.routeLine(ctx, c, r); rl != "" {
		rows = append(rows, rl)
	}
	return i, strings.Join(rows, "\n")
}

func (a *App) announceConfirm(ctx context.Context, c Couple, r domain.Round) {
	i, rows := a.meetingRows(ctx, c, r)
	for _, m := range a.both(c) {
		a.send(ctx, m.ChatID, fmt.Sprintf("Совпало! <b>%s</b>\n\n%s\n\nПодтверди — встреча станет запланированной, когда подтвердите оба.", esc(i.Title), rows),
			tg.Keyboard(tg.Row(tg.CB("Подтвердить", fmt.Sprintf("c:%d", r.ID)), tg.CB("Другое время", fmt.Sprintf("ot:%d", r.ID))),
				tg.Row(tg.App("Открыть встречу", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
	}
}

// agree: both chose the same idea and the time matches — that is the decision; the meeting is planned at once
// (one tap cancels it). No second «подтверди» after «совпало».
func (a *App) agree(ctx context.Context, c Couple, r domain.Round) domain.Round {
	ver := r.Version
	_, _ = r.Confirm("a", a.now())
	planned, err := r.Confirm("b", a.now())
	if err != nil || !planned {
		a.announceConfirm(ctx, c, r)
		return r
	}
	if err := a.save(ctx, r, ver); err != nil {
		a.Log.Warn("agree", "err", err)
		return r
	}
	a.planned(ctx, c, r, 0)
	return r
}

func (a *App) Confirm(ctx context.Context, c Couple, roundID int64) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	if r.State == domain.StatePlanned { // already agreed (a match plans by itself): nothing to confirm
		return r, nil
	}
	ver := r.Version
	planned, err := r.Confirm(c.Me.Color, a.now())
	if err != nil {
		return r, err
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	if planned {
		a.planned(ctx, c, r, 0)
	} else if c.Partner != nil {
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s %s. Ждём тебя — без твоего «да» встреча не запланируется.", esc(c.Me.Name), Past(c.Me.Name, "подтвердил")),
			tg.Keyboard(tg.Row(tg.CB("Подтвердить", fmt.Sprintf("c:%d", r.ID)), tg.CB("Другое время", fmt.Sprintf("ot:%d", r.ID)))))
	}
	return r, nil
}

func (a *App) ICS(ctx context.Context, c Couple, r domain.Round, seq int, cancelled bool) []byte {
	return []byte(a.icsEvent(ctx, c, r, seq, cancelled).Render())
}

func (a *App) icsEvent(ctx context.Context, c Couple, r domain.Round, seq int, cancelled bool) domain.ICS {
	i, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	desc := "Запланировано во «Вдвоём»."
	if i.Note != "" {
		desc = i.Note + "\n\n" + desc
	}
	p := a.PlaceOf(ctx, c, i)
	loc := p.Address
	if p.Chosen {
		loc = strings.Trim(p.Title+", "+p.Address, ", ")
		desc = "Место: " + p.Title + "\n" + p.Maps + "\n\n" + desc
	}
	if t := a.TicketsLink(a.TicketFile(ctx, c.Space.ID, r.ID)); t != "" { // the tickets open right from the event
		desc = "🎟 Билеты: " + t + "\n\n" + desc
	}
	link := p.URL
	if link == "" {
		link = p.Maps
	}
	e := domain.ICS{UID: fmt.Sprintf("round-%d@vdvoem", r.ID), Sequence: seq, Title: i.Title, Location: loc, URL: link, Desc: desc,
		Start: r.StartsAt, End: r.EndsAt, Stamp: a.now(), Cancelled: cancelled}
	for _, st := range a.AcceptedRoute(ctx, c, r) {
		sp := a.PlaceOf(ctx, c, st.Idea)
		l := st.Idea.Address
		if sp.Chosen {
			l = strings.Trim(sp.Title+", "+sp.Address, ", ")
		}
		e.More = append(e.More, domain.ICS{UID: fmt.Sprintf("extra-%d@vdvoem", st.Extra.ID), Sequence: seq, Title: st.Idea.Title, Location: l,
			URL: sp.Maps, Desc: "После «" + i.Title + "». Запланировано во «Вдвоём».", Start: st.Start, End: st.End})
	}
	return e
}

func (a *App) planned(ctx context.Context, c Couple, r domain.Round, seq int) {
	i, rows := a.meetingRows(ctx, c, r)
	for _, m := range a.both(c) {
		keys := [][]tg.Button{}
		if cal := a.calKeys(ctx, c, m, r); len(cal) > 0 {
			keys = append(keys, cal)
		}
		if b := a.bookButton(i); b != nil {
			keys = append(keys, tg.Row(*b, tg.CB("✓ Билеты есть", fmt.Sprintf("tk:%d", r.ID))))
		}
		keys = append(keys, tg.Row(tg.App("Открыть", a.appURL(fmt.Sprintf("round/%d", r.ID))), tg.CB("Перенести", fmt.Sprintf("ot:%d", r.ID)), tg.CB("Отменить", fmt.Sprintf("xq:%d", r.ID))))
		tail := ""
		if a.Subscribed(ctx, m.ID) {
			tail = "\n\nУже в твоём календаре — он подключён. Напомню за 2 часа."
		} else {
			tail = "\n\nНажми «В календарь» — событие откроется уже заполненным. Напомню за 2 часа."
		}
		if a.bookButton(i) != nil {
			tail += "\n🎟 Не забудь про билеты: «" + bookWord(i) + "». Когда возьмёшь — нажми «Билеты есть», чтобы не купить дважды."
		}
		a.send(ctx, m.ChatID, fmt.Sprintf("<b>Мы идём! 🎉</b> %s\n\n%s%s", esc(i.Title), rows, tail), tg.Keyboard(keys...))
	}
	remind := domain.QuietShift(r.StartsAt.Add(-2*time.Hour), c.Loc())
	if remind.After(a.now()) {
		_ = a.S.Enqueue(ctx, "remind", remind, map[string]any{"round": r.ID, "space": c.Space.ID, "at": r.StartsAt.Unix()}, fmt.Sprintf("remind:%d:%d", r.ID, r.StartsAt.Unix()))
	}
	ask := domain.QuietShift(r.EndsAt.Add(3*time.Hour), c.Loc())
	_ = a.S.Enqueue(ctx, "after", ask, map[string]any{"round": r.ID, "space": c.Space.ID, "at": r.StartsAt.Unix()}, fmt.Sprintf("after:%d:%d", r.ID, r.StartsAt.Unix()))
}

func (a *App) OtherTime(ctx context.Context, c Couple, roundID int64) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver, wasPlanned := r.Version, r.State == domain.StatePlanned
	if err := r.OtherTime(a.now()); err != nil {
		return r, err
	}
	if err := a.save(ctx, r, ver); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return r, err
		}
		return r, err
	}
	for _, m := range a.both(c) {
		lead := "Ищем другое время."
		if m.ID != c.Me.ID {
			lead = fmt.Sprintf("%s просит другое время.", esc(c.Me.Name))
		}
		if wasPlanned {
			lead += " Прежняя встреча снята."
		}
		a.send(ctx, m.ChatID, lead+" Отметь, когда удобно — идея остаётся та же.",
			tg.Keyboard(tg.Row(tg.App("Отметить время", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
	}
	return r, nil
}

// ExactTime: the meeting is agreed (or tickets are bought) and the real time is known — «в 14:00», not «днём».
// The same day, the same idea, no new agreement: the time moves, the calendar follows, the partner is told.
func (a *App) ExactTime(ctx context.Context, c Couple, roundID int64, hm string) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	if r.State != domain.StatePlanned && r.State != domain.StateConfirm || r.StartsAt.IsZero() {
		return r, domain.ErrWrongState
	}
	var h, m int
	if n, _ := fmt.Sscanf(hm, "%d:%d", &h, &m); n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return r, ErrBadTime
	}
	loc := c.Loc()
	cur := r.StartsAt.In(loc)
	day := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, loc)
	if cur.Hour() < 6 { // a night meeting is on the evening's date
		day = day.AddDate(0, 0, -1)
	}
	if h < 6 {
		h += 24 // «01:00» after an evening: the night after it
	}
	start := day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	if start.Equal(r.StartsAt) {
		return r, nil
	}
	if !start.After(a.now()) {
		return r, ErrBadTime
	}
	ver, dur := r.Version, int(r.EndsAt.Sub(r.StartsAt).Minutes())
	r.SetTime(start, dur)
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	title := "встреча"
	if i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen); err == nil {
		title = "«" + i.Title + "»"
	}
	when := fmt.Sprintf("%s %s в %s", strings.ToLower(ruWeekdayShort(start)), start.Format("02.01"), start.Format("15:04"))
	if c.Partner != nil {
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("🕑 %s %s время: %s — <b>%s</b>. В календаре уже поправлено.", esc(c.Me.Name), Past(c.Me.Name, "уточнил"), esc(title), when),
			tg.Keyboard(tg.Row(tg.App("Открыть встречу", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
	}
	return r, nil
}

func (a *App) Cancel(ctx context.Context, c Couple, roundID int64) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver, was := r.Version, r.State
	if err := r.Cancel(); err != nil {
		return r, err
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	for _, m := range a.both(c) {
		who := "Выбор отменён"
		if m.ID != c.Me.ID {
			who = esc(c.Me.Name) + " " + Past(c.Me.Name, "отменил")
		}
		a.send(ctx, m.ChatID, who+" выбор. Ничего страшного — идеи остаются в копилке.", nil)
		if was == domain.StatePlanned && !a.Subscribed(ctx, m.ID) {
			a.sendQuiet(ctx, m.ChatID, "Если встреча уже в календаре — удали её там. Чтобы такое происходило само, подключи календарь в разделе «Календарь».", nil)
		}
	}
	return r, nil
}

func (a *App) Done(ctx context.Context, c Couple, roundID int64, answer string) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver := r.Version
	switch answer {
	case "moved":
		return a.OtherTime(ctx, c, roundID)
	case "no":
		if err := r.Cancel(); err != nil {
			return r, err
		}
	default:
		if err := r.MarkDone(c.Me.Color, "yes"); err != nil {
			return r, err
		}
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	if answer == "yes" || answer == "" {
		_ = a.S.AfterMeeting(ctx, c.Space.ID, r.Chosen, r.StartsAt, "")
		for _, m := range a.both(c) {
			a.send(ctx, m.ChatID, "Здорово, что сходили! Пришли фото в ответ на это сообщение — оно будет только у вас двоих. <b>Повторим?</b>",
				tg.Keyboard(tg.Row(tg.CB("Да, ещё раз", fmt.Sprintf("ag:%d:yes", r.ID)), tg.CB("Хватит одного", fmt.Sprintf("ag:%d:no", r.ID)))))
		}
	}
	return r, nil
}

func (a *App) Again(ctx context.Context, c Couple, roundID int64, answer string) (domain.Round, error) {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return r, err
	}
	ver := r.Version
	if err := r.SetAgain(c.Me.Color, answer); err != nil {
		return r, err
	}
	if err := a.save(ctx, r, ver); err != nil {
		return r, err
	}
	_ = a.S.AfterMeeting(ctx, c.Space.ID, r.Chosen, r.StartsAt, answer)
	if r.AgainA != "" && r.AgainB != "" {
		text := "Вы оба ответили. "
		switch {
		case r.AgainA == "yes" && r.AgainB == "yes":
			text += "Повторим — идея вернулась в копилку с пометкой «понравилось»."
		case r.AgainA == "no" && r.AgainB == "no":
			text += "Одного раза хватило — больше не предложу."
		default:
			text += "Мнения разошлись — предложу её реже."
		}
		for _, m := range a.both(c) {
			a.sendQuiet(ctx, m.ChatID, text, nil)
		}
	}
	return r, nil
}

// Photo attaches a picture to the latest meeting (done or planned).
func (a *App) Photo(ctx context.Context, c Couple, fileID string) (bool, error) {
	rs, err := a.S.RecentRounds(ctx, c.Space.ID, 5)
	if err != nil {
		return false, err
	}
	for _, r := range rs {
		if r.State == domain.StateDone || r.State == domain.StatePlanned {
			if err := a.S.AddPhoto(ctx, r.ID, c.Me.ID, fileID, a.now()); err != nil {
				return false, err
			}
			if c.Partner != nil {
				_ = a.TG.SendPhoto(ctx, c.Partner.ChatID, fileID, esc(c.Me.Name)+" "+Past(c.Me.Name, "добавил")+" фото со встречи.")
			}
			return true, nil
		}
	}
	return false, nil
}

// ---------- background ----------

// Tick moves time-driven things: expiry, nudges, weekly plan day, queued jobs.
func (a *App) Tick(ctx context.Context) {
	now := a.now()
	if now.Minute() == 17 && now.Second() < 70 && now.Hour() == 4 { // once a night: old boot and route timings go
		a.S.PruneObserve(ctx, now)
	}
	rounds, err := a.S.ActiveRounds(ctx)
	if err != nil {
		a.Log.Error("tick rounds", "err", err)
	}
	for _, r := range rounds {
		sp, err := a.S.Space(ctx, r.SpaceID)
		if err != nil {
			continue
		}
		ms, _ := a.S.Members(ctx, r.SpaceID)
		ver := r.Version
		if r.Tick(now) {
			if a.S.SaveRound(ctx, r, ver, now) == nil {
				for _, m := range ms {
					a.sendQuiet(ctx, m.ChatID, "Выбор закрылся: двое суток без ответа. Не сложилось — попробуем в следующий раз.", tg.Keyboard(tg.Row(tg.CB("Давай куда-нибудь", "go"))))
				}
			}
			continue
		}
		if r.ReminderDue(now) && domain.QuietShift(now, sp.Loc()).Equal(now) {
			r.Reminders++
			if a.S.SaveRound(ctx, r, ver, now) == nil {
				for _, m := range ms {
					for _, w := range r.Waiting() {
						if w == m.Color {
							a.send(ctx, m.ChatID, reminderText(r), tg.Keyboard(tg.Row(tg.App("Открыть", a.appURL(fmt.Sprintf("round/%d", r.ID))))))
						}
					}
				}
			}
		}
	}
	a.urgentTick(ctx, now)
	spaces, _ := a.S.Spaces(ctx)
	for _, sp := range spaces {
		l := now.In(sp.Loc())
		if int(l.Weekday()) == sp.PlanWeekday && l.Hour() == sp.PlanHour {
			key := fmt.Sprintf("plan:%d:%s", sp.ID, l.Format("2006-01-02"))
			_ = a.S.Enqueue(ctx, "plan", now, map[string]any{"space": sp.ID}, key)
		}
		if int(l.Weekday()) == sp.SuggestWeekday && l.Hour() == sp.SuggestHour {
			key := fmt.Sprintf("suggest:%d:%s", sp.ID, l.Format("2006-01-02"))
			_ = a.S.Enqueue(ctx, "suggest", now, map[string]any{"space": sp.ID}, key)
		}
	}
	a.runJobs(ctx)
}

func reminderText(r domain.Round) string {
	switch r.State {
	case domain.StateTime:
		return "Напоминаю: отметь, когда тебе удобно. Без твоего ответа не подберу варианты."
	case domain.StateVote:
		return "Напоминаю: три варианта ждут твоего голоса."
	case domain.StateConfirm:
		return "Напоминаю: встреча ждёт твоего подтверждения. Молчание не считается согласием."
	}
	return "Напоминаю про выбор."
}

// Kick runs due jobs right away in the background (the parse job after a new idea),
// instead of waiting for the next tick. Claim() makes concurrent runs safe.
func (a *App) Kick() {
	if a.NoKick {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		a.runJobs(ctx)
	}()
}

func (a *App) runJobs(ctx context.Context) {
	jobs, err := a.S.Claim(ctx, a.now(), 20)
	if err != nil {
		a.Log.Error("claim", "err", err)
		return
	}
	if len(jobs) > 0 {
		defer a.Live.ChangedAll() // parsed links, reminders, suggestions: open apps refresh
	}
	for _, j := range jobs {
		err := a.runJob(ctx, j)
		msg := ""
		if err != nil {
			msg = err.Error()
			a.Log.Warn("job failed", "kind", j.Kind, "id", j.ID, "err", err)
		}
		_ = a.S.JobDone(ctx, j.ID, a.now(), msg)
	}
}

func (a *App) runJob(ctx context.Context, j store.Job) error {
	type rp struct {
		Round int64 `json:"round"`
		Space int64 `json:"space"`
		At    int64 `json:"at"`
	}
	switch j.Kind {
	case "option":
		var p struct {
			Option int64 `json:"option"`
			Space  int64 `json:"space"`
			Member int64 `json:"member"`
		}
		jsonUnmarshal(j.Payload, &p)
		return a.runOptionParse(ctx, p.Option, p.Space, p.Member)
	case "post":
		var p postJob
		if err := jsonUnmarshal(j.Payload, &p); err != nil {
			return nil
		}
		return a.runPost(ctx, p)
	case "picture":
		var p picJob
		if err := jsonUnmarshal(j.Payload, &p); err != nil {
			return nil
		}
		return a.runPicture(ctx, p)
	case "parse":
		var p parsePayload
		if err := jsonUnmarshal(j.Payload, &p); err != nil {
			return nil
		}
		return a.runParse(ctx, p)
	case "plan", "suggest":
		var p struct {
			Space int64 `json:"space"`
		}
		jsonUnmarshal(j.Payload, &p)
		sp, err := a.S.Space(ctx, p.Space)
		if err != nil {
			return nil
		}
		if j.Kind == "suggest" {
			return a.nudge(ctx, sp)
		}
		return a.ScheduledGo(ctx, sp)
	case "remind", "after":
		var p rp
		jsonUnmarshal(j.Payload, &p)
		r, err := a.S.Round(ctx, p.Space, p.Round)
		if err != nil || r.State != domain.StatePlanned || r.StartsAt.Unix() != p.At {
			return nil // moved or cancelled meanwhile: the old reminder stays silent
		}
		sp, _ := a.S.Space(ctx, p.Space)
		ms, _ := a.S.Members(ctx, p.Space)
		i, _ := a.S.Idea(ctx, p.Space, r.Chosen)
		for _, m := range ms {
			if j.Kind == "remind" {
				cp, _ := a.coupleOf(ctx, m)
				pl := a.PlaceOf(ctx, cp, i)
				text := fmt.Sprintf("Через 2 часа: <b>%s</b>, %s.", esc(i.Title), esc(domain.When(r.StartsAt, sp.Loc())))
				if pl.Chosen {
					text += "\n📍 " + esc(pl.Title) + addrLine(pl.Address)
				} else if i.Address != "" {
					text += "\n" + esc(i.Address)
				}
				if rl := a.routeLine(ctx, cp, r); rl != "" {
					text += "\n" + rl
				}
				a.send(ctx, m.ChatID, text, tg.Keyboard(tg.Row(tg.Link("На карте", pl.Maps), tg.CB("Перенести", fmt.Sprintf("ot:%d", r.ID)))))
			} else {
				a.send(ctx, m.ChatID, fmt.Sprintf("Как прошло — <b>%s</b>? Сходили?", esc(i.Title)),
					tg.Keyboard(tg.Row(tg.CB("Да", fmt.Sprintf("d:%d:yes", r.ID)), tg.CB("Перенесли", fmt.Sprintf("d:%d:moved", r.ID)), tg.CB("Не получилось", fmt.Sprintf("d:%d:no", r.ID)))))
			}
		}
		return nil
	case "scout_note":
		var p struct {
			Idea int64  `json:"idea"`
			Kind string `json:"kind"`
			Days int    `json:"days"`
		}
		if err := jsonUnmarshal(j.Payload, &p); err != nil {
			return nil
		}
		return a.runScoutNote(ctx, p.Idea, p.Kind, p.Days)
	case "idea_batch":
		var p struct {
			Chat, Space, Author, First int64
		}
		if err := jsonUnmarshal(j.Payload, &p); err != nil {
			return nil
		}
		return a.sendIdeaBatch(ctx, p.Chat, p.Space, p.Author, p.First)
	case "partner_new":
		var p struct {
			Member int64 `json:"member"`
			Space  int64 `json:"space"`
			Since  int64 `json:"since"`
		}
		jsonUnmarshal(j.Payload, &p)
		ideas, _ := a.S.Ideas(ctx, p.Space, domain.IdeaActive)
		ms, _ := a.S.Members(ctx, p.Space)
		var to *store.Member
		for k := range ms {
			if ms[k].ID == p.Member {
				to = &ms[k]
			}
		}
		if to == nil {
			return nil
		}
		var fresh []string
		for _, i := range ideas {
			if i.AuthorID != p.Member && i.CreatedAt.Unix() >= p.Since {
				fresh = append(fresh, "• "+esc(i.Title))
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		sort.Strings(fresh)
		a.sendQuiet(ctx, to.ChatID, "Новое в копилке:\n"+strings.Join(fresh, "\n"), tg.Keyboard(tg.Row(tg.App("Посмотреть и отметить", a.appURL("")))))
		return nil
	}
	return nil
}
