package api

import (
	"net/http"
	"sort"

	"vdvoem/internal/app"
	"vdvoem/internal/domain"
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
func (a *API) afisha(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	hz := r.URL.Query().Get("horizon")
	ideas, err := a.App.S.Ideas(r.Context(), c.Space.ID, domain.IdeaDraft, domain.IdeaActive)
	if err != nil {
		return nil, err
	}
	t := a.App.Tastes(r.Context(), c)
	type row struct {
		v ideaView
		s int
	}
	var rows []row
	counts := map[string]int{"week": 0, "later": 0}
	for _, i := range ideas {
		if i.Source != "scout" {
			continue
		}
		h := i.Horizon
		if h == "" {
			h = "week"
		}
		counts[h]++
		if hz != "" && h != hz {
			continue
		}
		v := withMatch(viewIdea(c, i), t, i, c.PartnerName())
		rows = append(rows, row{v, v.Match.Both})
	}
	sort.SliceStable(rows, func(x, y int) bool {
		if hz == "later" && rows[x].v.StartsAt != nil && rows[y].v.StartsAt != nil {
			return *rows[x].v.StartsAt < *rows[y].v.StartsAt
		}
		return rows[x].s > rows[y].s
	})
	out := []ideaView{}
	for _, x := range rows {
		out = append(out, x.v)
	}
	return map[string]any{"ideas": out, "counts": counts}, nil
}
