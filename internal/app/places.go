package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"vdvoem/internal/domain"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

var ErrNoAI = errors.New("no_ai")

// MapsLink is a Yandex Maps search — every idea and every option has one, so «где это» is always one tap away.
func MapsLink(query, city string) string {
	q := strings.TrimSpace(query)
	if city != "" && !strings.Contains(strings.ToLower(q), strings.ToLower(city)) {
		q += ", " + city
	}
	return "https://yandex.ru/maps/?text=" + url.QueryEscape(q)
}

func (a *App) city(c Couple) string {
	if c.Space.City != "" {
		return c.Space.City
	}
	return "Москва"
}

// Generic: a leisure idea with no link and no address («каток») — it needs a concrete place.
func Generic(i store.Idea) bool {
	return i.Kind == domain.KindLeisure && i.URL == "" && i.Address == ""
}

// Place is where a meeting happens: the chosen option, else the idea itself.
type Place struct {
	Title, Address, URL, Maps string
	Chosen                    bool
}

func (a *App) PlaceOf(ctx context.Context, c Couple, i store.Idea) Place {
	if id, _ := a.S.ChosenOption(ctx, i.ID); id != 0 {
		if o, err := a.S.Option(ctx, id); err == nil {
			q := o.Title
			if o.Address != "" {
				q = o.Title + ", " + o.Address
			}
			return Place{Title: o.Title, Address: o.Address, URL: o.URL, Maps: MapsLink(q, a.city(c)), Chosen: true}
		}
	}
	q := i.Title
	if i.Address != "" {
		q = i.Address
	}
	return Place{Title: i.Title, Address: i.Address, URL: i.URL, Maps: MapsLink(q, a.city(c))}
}

func (a *App) optionIdea(ctx context.Context, c Couple, ideaID int64) (store.Idea, error) {
	return a.S.Idea(ctx, c.Space.ID, ideaID)
}

// AddOption: a link (parsed in the background) or just a name, from either of the two.
func (a *App) AddOption(ctx context.Context, c Couple, ideaID int64, text string) (store.Option, error) {
	i, err := a.optionIdea(ctx, c, ideaID)
	if err != nil {
		return store.Option{}, err
	}
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > 300 {
		text = string(r[:300])
	}
	link := parse.FirstURL(text)
	title := strings.TrimSpace(strings.Replace(text, link, "", 1))
	if title == "" && link != "" {
		title = hostTitle(link)
	}
	if title == "" {
		return store.Option{}, errors.New("empty")
	}
	if r := []rune(title); len(r) > 80 {
		title = string(r[:80])
	}
	o, err := a.S.AddOption(ctx, store.Option{IdeaID: i.ID, MemberID: c.Me.ID, Title: title, URL: link, Parsed: link == ""}, a.now())
	if err != nil {
		return o, err
	}
	if link != "" {
		_ = a.S.Enqueue(ctx, "option", a.now(), map[string]any{"option": o.ID, "space": c.Space.ID, "member": c.Me.ID}, fmt.Sprintf("option:%d", o.ID))
		a.Kick()
	} else {
		a.optionAdded(ctx, c, i, o)
	}
	return o, nil
}

func (a *App) optionAdded(ctx context.Context, c Couple, i store.Idea, o store.Option) {
	if c.Partner == nil {
		return
	}
	line := fmt.Sprintf("%s добавил(а) вариант места к «%s»: <b>%s</b>", esc(c.Me.Name), esc(i.Title), esc(o.Title))
	if o.Address != "" {
		line += "\n" + esc(o.Address)
	}
	a.sendQuiet(ctx, c.Partner.ChatID, line, tg.Keyboard(tg.Row(tg.App("Посмотреть варианты", a.appURL(fmt.Sprintf("idea/%d", i.ID))))))
}

func (a *App) runOptionParse(ctx context.Context, optID, spaceID, memberID int64) error {
	o, err := a.S.Option(ctx, optID)
	if err != nil {
		return nil
	}
	if a.Fetch != nil && o.URL != "" {
		if r, err := parse.Fetch(ctx, a.Fetch, o.URL); err == nil || r.Title != "" {
			if r.Title != "" && (strings.HasPrefix(o.Title, "Ссылка с ") || o.Title == "") {
				o.Title = r.Title
			}
			if r.Address != "" {
				o.Address = r.Address
			}
			if r.Image != "" {
				o.ImageURL = r.Image
			}
			if r.PriceRub > 0 {
				o.PriceKop = r.PriceRub * 2 * 100
			}
		}
	}
	if strings.HasPrefix(o.Title, "Ссылка с ") {
		if t := slugTitle(o.URL); t != "" {
			o.Title = t
		}
	}
	o.Parsed = true
	if err := a.S.UpdateOption(ctx, o); err != nil {
		return err
	}
	m, err := a.S.MemberByID(ctx, memberID)
	if err != nil {
		return nil
	}
	c, err := a.coupleOf(ctx, m)
	if err != nil {
		return nil
	}
	i, err := a.S.Idea(ctx, spaceID, o.IdeaID)
	if err != nil {
		return nil
	}
	a.send(ctx, m.ChatID, fmt.Sprintf("Добавил вариант места к «%s»: <b>%s</b>%s", esc(i.Title), esc(o.Title), addrLine(o.Address)), nil)
	a.optionAdded(ctx, c, i, o)
	return nil
}

func addrLine(addr string) string {
	if addr == "" {
		return ""
	}
	return "\n" + esc(addr)
}

// slugTitle: «yandex.ru/maps/org/katok_v_parke/123» → «katok v parke» is better than nothing when the page refuses robots.
func slugTitle(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for k, p := range parts {
		if (p == "org" || p == "firm") && k+1 < len(parts) {
			s := strings.ReplaceAll(parts[k+1], "_", " ")
			if s != "" && !strings.ContainsAny(s, "0123456789") {
				return strings.ToUpper(s[:1]) + s[1:]
			}
		}
	}
	return ""
}

// SuggestPlaces: the assistant names three concrete places; each gets a map search, never an invented address.
func (a *App) SuggestPlaces(ctx context.Context, c Couple, ideaID int64) ([]store.Option, error) {
	if !a.AI.Enabled() {
		return nil, ErrNoAI
	}
	i, err := a.optionIdea(ctx, c, ideaID)
	if err != nil {
		return nil, err
	}
	guesses, err := a.AI.SuggestPlaces(ctx, i.Title, a.city(c))
	if err != nil {
		return nil, err
	}
	have, _ := a.S.Options(ctx, i.ID)
	seen := map[string]bool{}
	for _, o := range have {
		seen[strings.ToLower(o.Title)] = true
	}
	var out []store.Option
	for _, g := range guesses {
		if seen[strings.ToLower(g.Name)] {
			continue
		}
		note := "Предложил помощник — проверь на карте"
		if g.Why != "" {
			note = g.Why + " · предложил помощник, проверь на карте"
		}
		o, err := a.S.AddOption(ctx, store.Option{IdeaID: i.ID, Title: g.Name, URL: MapsLink(g.Name, a.city(c)), Note: note, Parsed: true}, a.now())
		if err == nil {
			out = append(out, o)
		}
	}
	return out, nil
}

// ChooseOption: «Берём этот». If the meeting is already planned, both get an updated calendar event.
func (a *App) ChooseOption(ctx context.Context, c Couple, ideaID, optID int64) error {
	i, err := a.optionIdea(ctx, c, ideaID)
	if err != nil {
		return err
	}
	if optID != 0 {
		o, err := a.S.Option(ctx, optID)
		if err != nil || o.IdeaID != i.ID {
			return store.ErrNotFound
		}
	}
	if err := a.S.ChooseOption(ctx, i.ID, optID); err != nil {
		return err
	}
	p := a.PlaceOf(ctx, c, i)
	if c.Partner != nil && optID != 0 {
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("%s выбрал(а) место для «%s»: <b>%s</b>%s", esc(c.Me.Name), esc(i.Title), esc(p.Title), addrLine(p.Address)),
			tg.Keyboard(tg.Row(tg.Link("На карте", p.Maps), tg.App("Открыть", a.appURL(fmt.Sprintf("idea/%d", i.ID))))))
	}
	if r, err := a.S.NextPlanned(ctx, c.Space.ID); err == nil && r.Chosen == i.ID && r.StartsAt.After(a.now()) {
		ics := a.ICS(ctx, c, r, r.Version+1, false)
		for _, m := range a.both(c) {
			if a.Subscribed(ctx, m.ID) {
				continue
			}
			_ = a.TG.SendDocument(ctx, m.ChatID, "vdvoem.ics", ics, "Место обновлено — событие в календаре тоже.")
		}
	}
	return nil
}
