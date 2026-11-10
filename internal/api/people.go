package api

import (
	"net/http"
	"sort"
	"time"

	"vdvoem/internal/app"
	"vdvoem/internal/domain"
	"vdvoem/internal/leisure"
	"vdvoem/internal/store"
)

// Personal: what the assistant remembers about *me*, my learned taste; the couple's scout goals; the afisha.

type handler = func(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error)

func (a *API) peopleRoutes(h func(string, handler)) {
	h("GET /api/profile", a.profile)
	h("POST /api/memories", a.addMemory)
	h("DELETE /api/memories/{id}", a.delMemory)
	h("GET /api/goals", a.goals)
	h("POST /api/goals", a.addGoal)
	h("DELETE /api/goals/{id}", a.delGoal)
	h("GET /api/afisha", a.afisha)
	h("GET /api/triage", a.triage)
	h("GET /api/picks/{id}", a.pick)
	h("POST /api/picks/{id}/send", a.sendPick)
	h("GET /api/taste", a.taste)
	h("PUT /api/taste", a.setTaste)
}

// «Мой вкус»: the levels to choose from, my profile and cloud, and the partner's cloud to look at
func (a *API) taste(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	out := map[string]any{"spheres": leisure.Spheres, "me": a.App.Taste(r.Context(), c.Me.ID), "cloud": a.App.TasteCloud(r.Context(), c, c.Me.ID)}
	if c.Partner != nil {
		out["partner"] = map[string]any{"name": c.Partner.Name, "cloud": a.App.TasteCloud(r.Context(), c, c.Partner.ID)}
	}
	return out, nil
}

func (a *API) setTaste(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in app.MyTaste
	if err := body(r, &in); err != nil {
		return nil, err
	}
	t, err := a.App.SetTaste(r.Context(), c, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"me": t, "cloud": a.App.TasteCloud(r.Context(), c, c.Me.ID)}, nil
}

func (a *API) profile(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	me, _ := a.App.Profiles(r.Context(), c)
	mem, _ := a.App.S.Memories(r.Context(), c.Space.ID)
	mine := []map[string]any{}
	for _, m := range mem {
		if m.MemberID == c.Me.ID { // the partner's memories stay theirs
			mine = append(mine, map[string]any{"id": m.ID, "text": m.Text, "at": m.CreatedAt.Unix()})
		}
	}
	return map[string]any{"taste": me, "memories": mine}, nil
}

func (a *API) addMemory(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	m, err := a.App.Remember(r.Context(), c, in.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": m.ID}, nil
}

func (a *API) delMemory(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, a.App.S.DeleteMemory(r.Context(), c.Me.ID, id)
}

func (a *API) goals(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	gs, err := a.App.S.Goals(r.Context(), c.Space.ID)
	out := []map[string]any{}
	for _, g := range gs {
		out = append(out, map[string]any{"id": g.ID, "text": g.Text, "mine": g.MemberID == c.Me.ID, "at": g.CreatedAt.Unix()})
	}
	return map[string]any{"goals": out}, err
}

func (a *API) addGoal(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	g, err := a.App.AddGoal(r.Context(), c, in.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": g.ID}, nil
}

func (a *API) delGoal(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, a.App.S.CloseGoal(r.Context(), c.Space.ID, id)
}

// afisha: what the scout found, ranked by how much both of you would like it.
// afisha: everything there is to go to, from every source — the scout's finds, posts from channels, the
// couple's own ideas — filtered and sorted: by how much you both would like it (each person's learned taste),
// by date or by price. ?horizon=week|later|all &source=all|scout|channel|ours &sort=match|date|price
// &cat=culture &genre=комедия &fresh=1 (only what neither of you has marked)
func (a *API) afisha(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	q := r.URL.Query()
	hz, src, srt, cat, genre := q.Get("horizon"), q.Get("source"), q.Get("sort"), q.Get("cat"), q.Get("genre")
	fresh := q.Get("fresh") == "1"
	if hz == "" {
		hz = "all"
	}
	if src == "" {
		src = "all"
	}
	ideas, err := a.App.S.Ideas(r.Context(), c.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	if err != nil {
		return nil, err
	}
	now := a.App.Now()
	t := a.App.Tastes(r.Context(), c)
	going := a.goingIdeas(r.Context(), c)
	sourceOf := func(i store.Idea) string {
		switch i.Source {
		case "scout", "channel":
			return i.Source
		}
		return "ours"
	}
	type row struct {
		v ideaView
		i store.Idea
	}
	var rows []row
	counts := map[string]int{"week": 0, "later": 0, "all": 0, "scout": 0, "channel": 0, "ours": 0, "fresh": 0}
	genres := map[string]int{}
	cats := map[string]int{}
	for _, i := range ideas {
		if i.Kind != domain.KindLeisure || going[i.ID] {
			continue
		}
		if i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Before(now) {
			continue // over
		}
		h := i.Horizon
		if h == "" {
			h = "week"
		}
		if i.StartsAt != nil && i.StartsAt.Sub(now) > 14*24*time.Hour {
			h = "later"
		}
		so := sourceOf(i)
		isFresh := i.WishA == domain.WishNone && i.WishB == domain.WishNone && so != "ours"
		// counts follow the other filters, so each chip says what it would show
		if (src == "all" || src == so) && (!fresh || isFresh) {
			counts[h]++
			counts["all"]++
		}
		if hz == "all" || h == hz {
			counts[so]++
			if isFresh {
				counts["fresh"]++
			}
		}
		if hz != "all" && h != hz || src != "all" && so != src || fresh && !isFresh {
			continue
		}
		for _, g := range i.Info.Genres {
			genres[g]++
		}
		cats[i.Category]++
		if cat != "" && i.Category != cat || genre != "" && !containsStr(i.Info.Genres, genre) {
			continue
		}
		rows = append(rows, row{withMatch(viewIdea(c, i), t, i, c.PartnerName()), i})
	}
	at := func(x row) int64 {
		if x.i.StartsAt != nil {
			return x.i.StartsAt.Unix()
		}
		return 1 << 62
	}
	sort.SliceStable(rows, func(x, y int) bool {
		switch srt {
		case "date":
			return at(rows[x]) < at(rows[y])
		case "price":
			px, py := rows[x].i.PriceMin, rows[y].i.PriceMin
			if px == 0 {
				px = 1 << 50
			}
			if py == 0 {
				py = 1 << 50
			}
			return px < py
		}
		return rows[x].v.Match.Both > rows[y].v.Match.Both
	})
	out := []ideaView{}
	for _, x := range rows {
		out = append(out, x.v)
	}
	type kv struct {
		K string `json:"k"`
		N int    `json:"n"`
	}
	top := func(m map[string]int, n int) []kv {
		l := []kv{}
		for k, v := range m {
			l = append(l, kv{k, v})
		}
		sort.Slice(l, func(x, y int) bool { return l[x].N > l[y].N || l[x].N == l[y].N && l[x].K < l[y].K })
		if len(l) > n {
			l = l[:n]
		}
		return l
	}
	return map[string]any{"ideas": out, "counts": counts, "genres": top(genres, 14), "cats": top(cats, 6)}, nil
}

func containsStr(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// toMark: an idea waiting for my mark — the one rule behind the badge, «Отметь N идей» and «Разбор · 1 из N»
func toMark(c app.Couple, i store.Idea, now time.Time, going map[int64]bool) bool {
	if i.Kind != domain.KindLeisure || going[i.ID] || i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Before(now) {
		return false
	}
	return viewIdea(c, i).MyWish == ""
}

// triage: everything I have not marked yet — the bank and the afisha — soonest first (a date about to pass
// matters most), then what suits us both best. «Разобрать всё» goes through it card by card.
func (a *API) triage(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	ideas, err := a.App.S.Ideas(r.Context(), c.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	if err != nil {
		return nil, err
	}
	now := a.App.Now()
	t := a.App.Tastes(r.Context(), c)
	going := a.goingIdeas(r.Context(), c)
	type row struct {
		v    ideaView
		soon int64
	}
	var rows []row
	for _, i := range ideas {
		if !toMark(c, i, now, going) {
			continue
		}
		v := withMatch(viewIdea(c, i), t, i, c.PartnerName())
		soon := int64(1 << 62)
		if i.StartsAt != nil && i.StartsAt.Sub(now) < 21*24*time.Hour {
			soon = i.StartsAt.Unix()
		}
		rows = append(rows, row{v, soon})
	}
	sort.SliceStable(rows, func(x, y int) bool {
		if rows[x].soon != rows[y].soon {
			return rows[x].soon < rows[y].soon
		}
		return rows[x].v.Match != nil && rows[y].v.Match != nil && rows[x].v.Match.Both > rows[y].v.Match.Both
	})
	out := make([]ideaView, 0, len(rows))
	for _, x := range rows {
		out = append(out, x.v)
	}
	return map[string]any{"ideas": out, "total": len(out)}, nil
}

// pick: a «подборка» from the assistant — every idea in it, as they are now (marks, match), best first
func (a *API) pick(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	p, err := a.App.S.Pick(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	t := a.App.Tastes(r.Context(), c)
	now := a.App.Now()
	out := []ideaView{}
	for _, iid := range p.IDs {
		i, err := a.App.S.Idea(r.Context(), c.Space.ID, iid)
		if err != nil || i.Status == domain.IdeaArchived || i.EndsAt != nil && i.EndsAt.Before(now) || i.EndsAt == nil && i.StartsAt != nil && i.StartsAt.Before(now) {
			continue
		}
		out = append(out, withMatch(viewIdea(c, i), t, i, c.PartnerName()))
	}
	return map[string]any{"id": p.ID, "title": p.Title, "ideas": out, "by_me": p.MemberID == c.Me.ID, "created_at": p.CreatedAt.Unix()}, nil
}

func (a *API) sendPick(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, a.App.SendPick(r.Context(), c, id)
}
