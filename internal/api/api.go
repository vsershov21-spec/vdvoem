// Package api serves the Mini App: JSON under /api (auth by Telegram initData) and the static app under /app.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/ai"
	"vdvoem/internal/app"
	"vdvoem/internal/assistant"
	"vdvoem/internal/domain"
	"vdvoem/internal/live"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/stt"
	"vdvoem/internal/tgauth"
)

type API struct {
	App      *app.App
	Helper   *assistant.Assistant
	STT      *stt.Client // voice in the composer; nil — off
	BotToken string
	WebDir   string
	Log      *slog.Logger
	// DevToken lets e2e run without Telegram: requests carrying X-Dev-Token == DevToken may name a user
	// in X-Dev-User. Empty (production) disables it entirely.
	DevToken string
	Advance  func(time.Duration) // e2e clock (DEV_TOKEN only)
}

type ctxKey struct{}

func (a *API) Routes(mux *http.ServeMux) {
	a.AuthRoutes(mux)
	if a.DevToken != "" {
		mux.HandleFunc("POST /api/dev/advance", a.devAdvance)
	}
	h := func(pattern string, f func(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error)) {
		g := a.auth(f)
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			t := time.Now()
			g(&timedWriter{ResponseWriter: w, t: t}, r)
			d := time.Since(t)
			perf.route(pattern, d)
			if a.App != nil { // a couple's app is a few hundred requests a day: keep them all (7 days)
				a.App.S.AddRouteTime(context.WithoutCancel(r.Context()), pattern, d.Milliseconds(), t)
			}
		})
	}
	a.peopleRoutes(h)
	a.agentRoutes(mux)
	a.feedRoutes(mux, h)
	h("GET /api/me", a.me)
	h("GET /api/home", a.home)
	h("GET /api/ideas", a.ideas)
	h("POST /api/ideas", a.addIdea)
	h("GET /api/ideas/{id}", a.idea)
	h("PATCH /api/ideas/{id}", a.patchIdea)
	h("DELETE /api/ideas/{id}", a.deleteIdea)
	h("POST /api/ideas/{id}/wish", a.wish)
	h("POST /api/ideas/{id}/merge", a.merge)
	h("GET /api/inbox", a.inbox)
	h("GET /api/meet/{id}", a.meet)
	h("POST /api/meet/{id}/time", a.meetTime)
	h("POST /api/meet/{id}/stop", a.meetStop)
	h("POST /api/meet/{id}/cancel", a.meetCancel)
	h("POST /api/meet/{id}/buy", a.meetBuy)
	h("GET /api/free", a.free)
	h("POST /api/free", a.setFree)
	h("POST /api/busy-day", a.setBusyDay)
	h("GET /api/chores", a.chores)
	h("POST /api/chores", a.addChore)
	h("GET /api/chores/history", a.choreHistory)
	h("PATCH /api/chores/{id}", a.editChore)
	h("POST /api/chores/{id}/{act}", a.choreAct)
	h("GET /api/prefs", a.prefsV2)
	h("PUT /api/prefs", a.setPrefV2)
	h("POST /api/proposals", a.propose)
	h("POST /api/proposals/{id}/answer", a.answerProposal)
	h("POST /api/proposals/{id}/withdraw", a.withdrawProposal)
	h("POST /api/proposals/{id}/seen", a.seenProposal)
	h("POST /api/proposals/{id}/remind", a.remindProposal)
	h("POST /api/ideas/{id}/enrich", a.enrichIdea)
	h("POST /api/ideas/{id}/urgent", a.markUrgent)
	h("DELETE /api/ideas/{id}/urgent", a.cancelUrgent)
	h("POST /api/ideas/{id}/urgent/{answer}", a.answerUrgent)
	h("GET /api/ideas/{id}/options", a.options)
	h("POST /api/ideas/{id}/options", a.addOption)
	h("POST /api/ideas/{id}/options/suggest", a.suggestOptions)
	h("POST /api/ideas/{id}/options/{opt}/choose", a.chooseOption)
	h("DELETE /api/ideas/{id}/options/{opt}", a.delOption)
	h("POST /api/rounds", a.newRound)
	h("POST /api/rounds/with/{idea}", a.roundWith)
	h("GET /api/rounds/current", a.currentRound)
	h("GET /api/rounds/{id}", a.round)
	h("POST /api/rounds/{id}/{action}", a.roundAction)
	h("POST /api/rounds/{id}/tickets/file", a.ticketsFile)
	h("POST /api/route/{id}", a.addExtra) // not under /api/rounds/{id}/…: it would clash with /api/rounds/with/{idea}
	h("POST /api/extras/{id}/{answer}", a.answerExtra)
	h("DELETE /api/extras/{id}", a.delExtra)
	mux.HandleFunc("GET /api/rounds/{id}/ics", a.authRaw(a.ics))
	mux.HandleFunc("GET /api/live", a.authRaw(a.liveStream))
	mux.HandleFunc("POST /api/client-log", a.clientLog)
	h("GET /api/suggest", a.suggest)
	h("GET /api/history", a.history)
	h("GET /api/going", a.going)
	h("GET /api/settings", a.settings)
	h("PATCH /api/settings", a.patchSettings)
	h("POST /api/invite", a.invite)
	h("GET /api/assistant", a.chat)
	h("POST /api/assistant", a.askAssistant)
	h("POST /api/assistant/act", a.act)
	h("GET /api/assistant/hints", a.hints)
	h("POST /api/assistant/voice", a.voice)
	h("DELETE /api/assistant", a.clearChat)
	mux.HandleFunc("GET /app/", a.static)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/app/", http.StatusFound) })
}

// ---------- auth & plumbing ----------

func (a *API) user(r *http.Request) (tgauth.User, error) {
	if a.DevToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Dev-Token")), []byte(a.DevToken)) == 1 {
		name, _ := url.QueryUnescape(r.Header.Get("X-Dev-Name"))
		if id, err := strconv.ParseInt(r.Header.Get("X-Dev-User"), 10, 64); err == nil {
			return tgauth.User{ID: id, FirstName: name}, nil
		}
	}
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "tma ")
	d, err := tgauth.Validate(raw, a.BotToken, 24*time.Hour, a.App.Now())
	if err != nil {
		return tgauth.User{}, err
	}
	return d.User, nil
}

type httpErr struct {
	code int
	msg  string
}

func (e httpErr) Error() string { return e.msg }

func bad(msg string) error { return httpErr{400, msg} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// couple resolves who is asking: e2e dev header, Telegram initData (inside the Mini App), or a browser session cookie.
func (a *API) couple(r *http.Request) (app.Couple, error) {
	u, err := a.user(r)
	if err != nil {
		// no initData (Go trims "tma " to "tma"): a browser session cookie may be there instead
		if strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "tma")) == "" {
			if id, ok := a.sessionMember(r); ok {
				return a.App.CoupleByMember(r.Context(), id)
			}
		}
		return app.Couple{}, httpErr{401, "unauthorized"}
	}
	c, err := a.App.Couple(r.Context(), u.ID)
	if errors.Is(err, app.ErrNoSpace) {
		// opened the Mini App before /start: create a solo space, so the app is never a dead end
		name := u.FirstName
		if name == "" {
			name = "Я"
		}
		c, _, err = a.App.Start(r.Context(), app.Profile{TGID: u.ID, ChatID: u.ID, Name: name, Photo: u.PhotoURL}, "")
	}
	return c, err
}

func (a *API) auth(f func(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		c, err := a.couple(r)
		if err == nil {
			mut := r.Method != http.MethodGet
			if mut {
				r = r.WithContext(live.WithSource(r.Context(), c.Me.ID, clientID(r)))
			}
			var out any
			out, err = f(w, r, c)
			if err == nil {
				if mut { // the partner's open app refreshes now, not on the next poll
					a.App.Live.From(r.Context(), c.Space.ID)
				}
				if out != nil {
					writeJSON(w, 200, out)
				}
				return
			}
		}
		a.fail(w, r, err)
	}
}

// clientLog: the page reports its boot stages and script errors (sendBeacon), so a hang on a phone is visible in the log.
func (a *API) clientLog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Stage string `json:"stage"`
		Msg   string `json:"msg"`
		Ms    int    `json:"ms"`
		Boot  string `json:"b"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 2048)).Decode(&in)
	cut := func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	a.Log.Info("client", "stage", cut(in.Stage, 24), "msg", cut(in.Msg, 300), "ms", in.Ms)
	perf.boot(bootRec{At: time.Now(), Stage: cut(in.Stage, 24), Msg: cut(in.Msg, 120), Ms: in.Ms})
	if a.App != nil && in.Stage != "" {
		a.App.S.AddBoot(context.WithoutCancel(r.Context()), cut(in.Boot, 16), cut(in.Stage, 24), cut(in.Msg, 200), in.Ms, time.Now())
	}
	w.WriteHeader(204)
}

// clientID: a random id per open tab, so its own changes don't echo back to it.
func clientID(r *http.Request) string {
	c := r.Header.Get("X-Client")
	if len(c) > 40 {
		c = c[:40]
	}
	return c
}

// liveStream: server-sent events for the open app — «sync» (refresh) and «presence» (who is in the app).
func (a *API) liveStream(w http.ResponseWriter, r *http.Request, c app.Couple) error {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // the server timeouts would cut the stream
	_ = rc.SetReadDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Content-Encoding", "identity") // a compressing proxy (Caddy encode) leaves the stream alone
	w.WriteHeader(200)
	ch, cancel := a.App.Live.Subscribe(c.Space.ID, c.Me.ID)
	defer cancel()
	send := func(ev string, data any) bool {
		b, _ := json.Marshal(data)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("hello", map[string]any{"me": c.Me.ID}) {
		return nil
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return nil
			}
		case e, ok := <-ch:
			if !ok {
				return nil
			}
			var ok2 bool
			if e.Kind == "presence" { // as colours: the app knows people as «a» and «b»
				on := []string{}
				ms, _ := a.App.S.Members(r.Context(), c.Space.ID) // the partner may have joined since the stream opened
				for _, id := range e.On {
					for _, m := range ms {
						if m.ID == id {
							on = append(on, m.Color)
						}
					}
				}
				ok2 = send("presence", map[string]any{"online": on})
			} else {
				by := ""
				if e.By == c.Me.ID {
					by = c.Me.Color
				} else if e.By != 0 {
					by = map[string]string{"a": "b", "b": "a"}[c.Me.Color]
				}
				ok2 = send("sync", map[string]any{"by": by, "cid": e.CID})
			}
			if !ok2 {
				return nil
			}
		}
	}
}

func (a *API) authRaw(f func(w http.ResponseWriter, r *http.Request, c app.Couple) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := a.couple(r)
		if err == nil {
			err = f(w, r, c)
		}
		if err != nil {
			a.fail(w, r, err)
		}
	}
}

func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var he httpErr
	switch {
	case errors.As(err, &he):
		writeJSON(w, he.code, map[string]string{"error": he.msg})
	case errors.Is(err, store.ErrNotFound), errors.Is(err, app.ErrNoSpace):
		writeJSON(w, 404, map[string]string{"error": "not_found"})
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, 409, map[string]string{"error": "conflict"})
	case errors.Is(err, domain.ErrClosed):
		writeJSON(w, 409, map[string]string{"error": "closed"})
	case errors.Is(err, domain.ErrWrongState):
		writeJSON(w, 409, map[string]string{"error": "wrong_state"})
	case errors.Is(err, domain.ErrNotInTriple):
		writeJSON(w, 409, map[string]string{"error": "not_in_triple"})
	case errors.Is(err, app.ErrNotPaired):
		writeJSON(w, 409, map[string]string{"error": "not_paired"})
	case errors.Is(err, app.ErrBadTime):
		writeJSON(w, 400, map[string]string{"error": "bad_time"})
	case errors.Is(err, app.ErrRoundOpen):
		writeJSON(w, 409, map[string]string{"error": "round_open"})
	case errors.Is(err, app.ErrNoIdeas):
		writeJSON(w, 404, map[string]string{"error": "no_ideas"})
	case errors.Is(err, domain.ErrOwnProposal):
		writeJSON(w, 409, map[string]string{"error": "own_proposal"})
	case errors.Is(err, domain.ErrProposalClosed):
		writeJSON(w, 409, map[string]string{"error": "proposal_closed"})
	case errors.Is(err, app.ErrMeetState):
		writeJSON(w, 409, map[string]string{"error": "meet_state"})
	case errors.Is(err, app.ErrChoreState):
		writeJSON(w, 409, map[string]string{"error": "chore_state"})
	case errors.Is(err, app.ErrBadInput):
		writeJSON(w, 400, map[string]string{"error": "bad_input"})
	case errors.Is(err, app.ErrNotPending):
		writeJSON(w, 409, map[string]string{"error": "not_pending"})
	case errors.Is(err, app.ErrNotYours):
		writeJSON(w, 409, map[string]string{"error": "not_yours"})
	case errors.Is(err, assistant.ErrLimit):
		writeJSON(w, 429, map[string]string{"error": "limit"})
	default:
		a.Log.Error("api", "path", r.URL.Path, "err", err)
		writeJSON(w, 500, map[string]string{"error": "internal"})
	}
}

func body(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v); err != nil {
		return bad("bad_json")
	}
	return nil
}

func pathID(r *http.Request, k string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(k), 10, 64)
	if err != nil {
		return 0, bad("bad_id")
	}
	return id, nil
}

// ---------- views ----------

type personView struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Photo string `json:"photo,omitempty"`
}

func person(m store.Member) personView { return personView{m.Name, m.Color, m.PhotoURL} }

type ideaView struct {
	ID            int64        `json:"id"`
	Title         string       `json:"title"`
	Kind          string       `json:"kind"`
	Category      string       `json:"category"`
	URL           string       `json:"url,omitempty"`
	Address       string       `json:"address,omitempty"`
	Image         string       `json:"image,omitempty"`
	Note          string       `json:"note,omitempty"`
	PriceMin      int64        `json:"price_min,omitempty"`
	PriceMax      int64        `json:"price_max,omitempty"`
	DurationMin   int          `json:"duration_min,omitempty"`
	StartsAt      *int64       `json:"starts_at,omitempty"`
	EndsAt        *int64       `json:"ends_at,omitempty"`
	Deadline      *int64       `json:"deadline,omitempty"`
	Assignee      string       `json:"assignee,omitempty"`
	Status        string       `json:"status"`
	MyWish        string       `json:"my_wish"`
	PartnerMarked bool         `json:"partner_marked"`
	PartnerWish   *string      `json:"partner_wish,omitempty"` // only after I marked
	Mine          bool         `json:"mine"`
	Parsed        bool         `json:"parsed"`
	Meta          string       `json:"meta"`
	LikedBefore   bool         `json:"liked_before,omitempty"`
	Maps          string       `json:"maps"`    // Yandex Maps search for the idea
	Generic       bool         `json:"generic"` // no link and no address: needs a concrete place
	Options       []optionView `json:"options,omitempty"`
	Place         *placeView   `json:"place,omitempty"`
	CreatedAt     int64        `json:"created_at"`
	Source        string       `json:"source,omitempty"`  // scout
	Ours          bool         `json:"ours"`              // in «Наши идеи»: added by one of us, or a find one of us marked «хочу»
	Why           string       `json:"why,omitempty"`     // the scout's reason
	Info          *store.Info  `json:"info,omitempty"`    // description, genres, cast, age
	Horizon       string       `json:"horizon,omitempty"` // week | later
	OnSale        *int64       `json:"on_sale,omitempty"`
	Match         *matchView   `json:"match,omitempty"`
}

type matchView struct {
	Me      int    `json:"me"` // 0..100
	Partner int    `json:"partner"`
	Both    int    `json:"both"`
	Note    string `json:"note"`
}

func withMatch(v ideaView, t app.Tastes, i store.Idea, partner string) ideaView {
	m := t.Of(i)
	v.Match = &matchView{Me: int(m.Me * 100), Partner: int(m.Partner * 100), Both: int(m.Both * 100), Note: app.MatchNote(m, partner)}
	return v
}

func unix(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	u := t.Unix()
	return &u
}

func viewIdea(c app.Couple, i store.Idea) ideaView {
	me, p := i.WishA, i.WishB
	if c.Me.Color == "b" {
		me, p = i.WishB, i.WishA
	}
	v := ideaView{ID: i.ID, Title: i.Title, Kind: string(i.Kind), Category: i.Category, URL: i.URL, Address: i.Address, Image: i.ImageURL, Note: i.Note,
		PriceMin: i.PriceMin, PriceMax: i.PriceMax, DurationMin: i.DurationMin, StartsAt: unix(i.StartsAt), EndsAt: unix(i.EndsAt), Deadline: unix(i.Deadline),
		Assignee: i.Assignee, Status: string(i.Status), MyWish: string(me), PartnerMarked: p != domain.WishNone, Mine: i.AuthorID == c.Me.ID,
		Parsed: i.Parsed, Meta: app.IdeaMeta(i), LikedBefore: i.AgainYes > 0, CreatedAt: i.CreatedAt.Unix(),
		Generic: app.Generic(i), Source: i.Source, Why: i.Why, Horizon: i.Horizon, OnSale: unix(i.OnSale)}
	wants := func(w domain.Wish) bool { return w == domain.WishWant || w == domain.WishVery }
	v.Ours = i.Source != "scout" && i.Source != "channel" || wants(i.WishA) || wants(i.WishB)
	if !i.Info.Empty() {
		in := i.Info
		v.Info = &in
	}
	q := i.Title
	if i.Address != "" {
		q = i.Address
	}
	city := c.Space.City
	if city == "" {
		city = "Москва"
	}
	v.Maps = app.MapsLink(q, city)
	if me != domain.WishNone && p != domain.WishNone {
		s := string(p)
		v.PartnerWish = &s
	}
	return v
}

func (a *API) me(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	out := map[string]any{"me": person(c.Me), "space": map[string]any{"id": c.Space.ID, "status": c.Space.Status, "tz": c.Space.TZ}, "bot": a.App.BotName,
		"biometric_lock": c.Me.BiometricLock, "theme": c.Me.Theme, "now": a.App.Now().Unix(), "ai": a.App.AI.Enabled()}
	if c.Partner != nil {
		out["partner"] = person(*c.Partner)
	}
	return out, nil
}

func (a *API) home(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	ctx := r.Context()
	ideas, err := a.App.S.Ideas(ctx, c.Space.ID, domain.IdeaActive)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"me": person(c.Me), "paired": c.Partner != nil}
	if c.Partner != nil {
		out["partner"] = person(*c.Partner)
	}
	unmarked := []ideaView{}
	leisure, chores, both, nUnmarked := 0, 0, 0, 0
	going := a.goingIdeas(ctx, c)
	for _, i := range ideas {
		v := viewIdea(c, i)
		if i.Kind == domain.KindChore {
			chores++
			continue
		}
		leisure++
		if toMark(c, i, a.App.Now(), going) && len(unmarked) < 10 {
			unmarked = append(unmarked, v)
		}
		if (i.WishA == domain.WishWant || i.WishA == domain.WishVery) && (i.WishB == domain.WishWant || i.WishB == domain.WishVery) {
			both++
		}
	}
	fresh := 0
	for _, i := range ideas {
		if toMark(c, i, a.App.Now(), going) {
			nUnmarked++
		}
	}
	if drafts, err := a.App.S.Ideas(ctx, c.Space.ID, domain.IdeaDraft); err == nil {
		now := a.App.Now()
		for _, i := range drafts {
			if toMark(c, i, now, going) {
				nUnmarked++
			}
			if app.Afisha(i) && i.WishA == domain.WishNone && i.WishB == domain.WishNone && !(i.StartsAt != nil && i.StartsAt.Before(now) && (i.EndsAt == nil || i.EndsAt.Before(now))) {
				fresh++
			}
		}
	}
	out["counts"] = map[string]int{"ideas": leisure, "chores": chores, "both_want": both, "unmarked": nUnmarked, "fresh": fresh}
	out["taste_set"] = !a.App.Taste(ctx, c.Me.ID).Empty()
	if c.Partner == nil { // alone: is an invitation still waiting to be opened
		if t := a.App.S.PendingInvite(ctx, c.Space.ID, a.App.Now()); !t.IsZero() {
			out["invite_until"] = t.Unix()
		}
	}
	out["to_mark"] = unmarked
	out["urgent"] = a.App.UrgentForMe(ctx, c)
	out["inbox"] = a.App.Inbox(ctx, c)                // «Ждёт тебя»: proposals to answer and their answers
	out["chore_summary"] = a.App.ChoreSummary(ctx, c) // the one line about chores on the home screen
	out["going"] = a.goingRounds(ctx, c)
	if rd, err := a.App.S.OpenRound(ctx, c.Space.ID); err == nil {
		out["round"] = a.viewRound(ctx, c, rd)
	}
	if rd, err := a.App.S.NextPlanned(ctx, c.Space.ID); err == nil && rd.StartsAt.After(a.App.Now()) {
		out["next"] = a.viewRound(ctx, c, rd)
	}
	return out, nil
}

func (a *API) ideas(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	st := []domain.IdeaStatus{domain.IdeaActive}
	if r.URL.Query().Get("status") == "archived" {
		st = []domain.IdeaStatus{domain.IdeaArchived}
	}
	ideas, err := a.App.S.Ideas(r.Context(), c.Space.ID, st...)
	if err != nil {
		return nil, err
	}
	kind := r.URL.Query().Get("kind")
	out := []ideaView{}
	for _, i := range ideas {
		if kind != "" && string(i.Kind) != kind {
			continue
		}
		out = append(out, viewIdea(c, i))
	}
	return map[string]any{"ideas": out}, nil
}

func (a *API) addIdea(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, bad("empty")
	}
	res, err := a.App.AddIdea(r.Context(), c, in.Text)
	if err != nil {
		return nil, err
	}
	if res.Existing != nil {
		return map[string]any{"existing": viewIdea(c, *res.Existing)}, nil
	}
	out := map[string]any{"idea": viewIdea(c, res.Idea)}
	if res.Similar != nil {
		out["similar"] = viewIdea(c, *res.Similar)
	}
	return out, nil
}

func (a *API) idea(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	i, err := a.App.S.Idea(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	v := a.withPlaces(r.Context(), c, viewIdea(c, i), i)
	if i.Kind == domain.KindLeisure {
		v = withMatch(v, a.App.Tastes(r.Context(), c), i, c.PartnerName())
	}
	filled, enriching := a.filled(r.Context(), c, i)
	return map[string]any{"idea": v, "urgent": a.App.UrgentView(r.Context(), c, i.ID), "filled": filled, "enriching": enriching}, nil
}

// filled: for every field of the card that has a value — who it came from: me | partner | scout.
// enriching: the scout was asked to fill the gaps less than an hour ago and some are still empty.
func (a *API) filled(ctx context.Context, c app.Couple, i store.Idea) (map[string]string, bool) {
	edits := a.App.S.IdeaEdits(ctx, i.ID)
	who := func(id int64) string {
		if id == c.Me.ID {
			return "me"
		}
		return "partner"
	}
	auto := i.Source == "scout" || i.Parsed
	has := map[string]bool{"when": i.StartsAt != nil, "where": i.Address != "", "price": i.PriceMin > 0 || i.PriceMax > 0, "about": i.Info.About != "", "note": i.Note != ""}
	out := map[string]string{}
	gaps := false
	for f, ok := range has {
		switch e, edited := edits[f]; {
		case !ok:
			gaps = gaps || f == "where" || f == "price"
		case edited:
			out[f] = who(e.MemberID)
		case auto && f != "note":
			out[f] = "scout"
		default:
			out[f] = who(i.AuthorID)
		}
	}
	e, asked := edits["_enrich"]
	return out, asked && gaps && a.App.Now().Sub(e.At) < time.Hour
}

// enrichIdea: «Попросить скаута» — the page behind the idea is read again to fill what is missing
func (a *API) enrichIdea(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	i, err := a.App.S.Idea(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	now := a.App.Now()
	_ = a.App.S.MarkIdeaEdit(r.Context(), id, "_enrich", c.Me.ID, now)
	_ = a.App.S.Enqueue(r.Context(), "parse", now, map[string]any{"idea": i.ID, "text": strings.TrimSpace(i.Title + " " + i.URL)}, fmt.Sprintf("parse:%d:%d", i.ID, now.Unix()))
	a.App.Kick()
	return map[string]any{"enriching": true}, nil
}

func (a *API) patchIdea(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	cur, err := a.App.S.Idea(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	var in struct {
		Title       *string `json:"title"`
		Kind        *string `json:"kind"`
		Category    *string `json:"category"`
		Address     *string `json:"address"`
		Note        *string `json:"note"`
		URL         *string `json:"url"`
		PriceMin    *int64  `json:"price_min"`
		PriceMax    *int64  `json:"price_max"`
		DurationMin *int    `json:"duration_min"`
		StartsAt    *int64  `json:"starts_at"`
		About       *string `json:"about"` // the programme: what is included, when to gather
		EndsAt      *int64  `json:"ends_at"`
		Deadline    *int64  `json:"deadline"`
		Assignee    *string `json:"assignee"`
		Status      *string `json:"status"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	set := func(dst *string, src *string, max int) {
		if src != nil {
			s := strings.TrimSpace(*src)
			if rs := []rune(s); len(rs) > max {
				s = string(rs[:max])
			}
			*dst = s
		}
	}
	set(&cur.Title, in.Title, 80)
	set(&cur.Address, in.Address, 160)
	set(&cur.Note, in.Note, 500)
	set(&cur.URL, in.URL, 500)
	if cur.Title == "" {
		return nil, bad("title_required")
	}
	if in.Kind != nil && (*in.Kind == "leisure" || *in.Kind == "chore") {
		cur.Kind = domain.IdeaKind(*in.Kind)
	}
	if in.Category != nil {
		switch *in.Category {
		case "food", "culture", "walk", "sport", "home", "other":
			cur.Category = *in.Category
		}
	}
	if in.PriceMin != nil && *in.PriceMin >= 0 {
		cur.PriceMin = *in.PriceMin
	}
	if in.PriceMax != nil && *in.PriceMax >= 0 {
		cur.PriceMax = *in.PriceMax
	}
	if in.DurationMin != nil && *in.DurationMin >= 0 && *in.DurationMin <= 24*60 {
		cur.DurationMin = *in.DurationMin
	}
	tm := func(p *int64) *time.Time {
		if p == nil || *p == 0 {
			return nil
		}
		t := time.Unix(*p, 0)
		return &t
	}
	if in.StartsAt != nil {
		cur.StartsAt = tm(in.StartsAt)
	}
	if in.About != nil {
		t := strings.TrimSpace(*in.About)
		if rs := []rune(t); len(rs) > 600 {
			t = string(rs[:600])
		}
		cur.Info.About = t
	}
	if in.EndsAt != nil {
		cur.EndsAt = tm(in.EndsAt)
	}
	if in.Deadline != nil {
		cur.Deadline = tm(in.Deadline)
	}
	if in.Assignee != nil && (*in.Assignee == "" || *in.Assignee == "a" || *in.Assignee == "b") {
		cur.Assignee = *in.Assignee
	}
	if in.Status != nil {
		switch domain.IdeaStatus(*in.Status) {
		case domain.IdeaActive, domain.IdeaArchived, domain.IdeaDone:
			cur.Status = domain.IdeaStatus(*in.Status)
		}
	}
	cur.Fingerprint = domain.Fingerprint(cur.URL, cur.Title)
	if err := a.App.S.UpdateIdea(r.Context(), cur, a.App.Now()); err != nil {
		return nil, err
	}
	// who filled what: the card shows it next to every field
	for f, set := range map[string]bool{"title": in.Title != nil, "when": in.StartsAt != nil, "where": in.Address != nil,
		"price": in.PriceMin != nil || in.PriceMax != nil, "about": in.About != nil, "note": in.Note != nil} {
		if set {
			_ = a.App.S.MarkIdeaEdit(r.Context(), id, f, c.Me.ID, a.App.Now())
		}
	}
	i, err := a.App.S.Idea(r.Context(), c.Space.ID, id)
	return map[string]any{"idea": viewIdea(c, i)}, err
}

// ---------- the meeting card ----------

func (a *API) meetOut(r *http.Request, c app.Couple, id int64) (any, error) {
	rd, err := a.App.S.Round(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	buying := ""
	if v := a.App.S.SpaceSettings(r.Context(), c.Space.ID)[fmt.Sprintf("sys.buy:%d", id)]; v == fmt.Sprint(c.Me.ID) {
		buying = "me"
	} else if v != "" {
		buying = "partner"
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd), "proposals": a.App.MeetProposals(r.Context(), c, id), "buying": buying}, nil
}

func (a *API) meet(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return a.meetOut(r, c, id)
}

func (a *API) meetTime(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Start int64  `json:"start"` // unix, or a slot of the grid: day + part (+ "HH:MM")
		Day   string `json:"day"`
		Part  int    `json:"part"`
		At    string `json:"at"`
	}
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	start := time.Unix(in.Start, 0)
	if in.Start == 0 {
		if start, err = app.SlotStart(in.Day, in.Part, c.Loc()); err != nil {
			return nil, app.ErrBadInput
		}
		if hm, e := time.Parse("15:04", in.At); e == nil {
			y, m, d := start.Date()
			start = time.Date(y, m, d, hm.Hour(), hm.Minute(), 0, 0, c.Loc())
		}
	}
	if _, err := a.App.ProposeTime(r.Context(), c, id, start); err != nil {
		return nil, err
	}
	return a.meetOut(r, c, id)
}

func (a *API) meetStop(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Idea int64  `json:"idea"`
		Text string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	if _, err := a.App.ProposeStop(r.Context(), c, id, in.Idea, in.Text); err != nil {
		return nil, err
	}
	return a.meetOut(r, c, id)
}

// meetBuy: «Куплю я» — the other one sees who is buying the tickets
func (a *API) meetBuy(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	if _, err := a.App.S.Round(r.Context(), c.Space.ID, id); err != nil {
		return nil, err
	}
	if err := a.App.S.SetSpaceSetting(r.Context(), c.Space.ID, fmt.Sprintf("sys.buy:%d", id), fmt.Sprint(c.Me.ID)); err != nil {
		return nil, err
	}
	a.App.Live.From(r.Context(), c.Space.ID)
	return a.meetOut(r, c, id)
}

func (a *API) meetCancel(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	if err := a.App.CancelMeet(r.Context(), c, id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// ---------- Планы: free windows and busy days ----------

func (a *API) free(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.App.Free(r.Context(), c), nil
}

func (a *API) setFree(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Day  string `json:"day"`
		Part int    `json:"part"`
		On   bool   `json:"on"`
	}
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	if err := a.App.SetFreeSlot(r.Context(), c, in.Day, in.Part, in.On); err != nil {
		return nil, err
	}
	return a.App.Free(r.Context(), c), nil
}

func (a *API) setBusyDay(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Day string `json:"day"`
		On  bool   `json:"on"`
	}
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	if err := a.App.SetBusyDay(r.Context(), c, in.Day, in.On); err != nil {
		return nil, err
	}
	return a.App.Free(r.Context(), c), nil
}

// ---------- chores ----------

type choreIn struct {
	Title   string `json:"title"`
	Note    string `json:"note"`
	Who     string `json:"who"`
	Due     *int64 `json:"due"` // unix; null clears it (with has_due)
	HasDue  bool   `json:"has_due"`
	DueTime bool   `json:"due_time"`
	Repeat  string `json:"repeat"`
}

func (in choreIn) app() app.ChoreIn {
	out := app.ChoreIn{Title: in.Title, Note: in.Note, Who: in.Who, DueTime: in.DueTime, Repeat: in.Repeat}
	if in.Due != nil {
		t := time.Unix(*in.Due, 0)
		out.Due = &t
	}
	return out
}

func (a *API) choresOut(r *http.Request, c app.Couple) (any, error) {
	list, sum, err := a.App.Chores(r.Context(), c)
	if err != nil {
		return nil, err
	}
	return map[string]any{"chores": list, "summary": sum}, nil
}

func (a *API) chores(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.choresOut(r, c)
}

func (a *API) addChore(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in choreIn
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	if _, err := a.App.AddChore(r.Context(), c, in.app()); err != nil {
		return nil, err
	}
	return a.choresOut(r, c)
}

func (a *API) editChore(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in choreIn
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	if _, err := a.App.EditChore(r.Context(), c, id, in.app(), in.HasDue || in.Due != nil); err != nil {
		return nil, err
	}
	return a.choresOut(r, c)
}

func (a *API) choreAct(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	if _, err := a.App.ChoreAct(r.Context(), c, id, r.PathValue("act")); err != nil {
		return nil, err
	}
	return a.choresOut(r, c)
}

func (a *API) choreHistory(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	list, err := a.App.ChoreHistory(r.Context(), c, r.URL.Query().Get("q"), r.URL.Query().Get("who"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"chores": list}, nil
}

// ---------- settings ----------

func (a *API) prefsV2(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.App.SettingsView(r.Context(), c), nil
}

func (a *API) setPrefV2(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	if err := a.App.SetSetting(r.Context(), c, in.Key, in.Value); err != nil {
		return nil, err
	}
	return a.App.SettingsView(r.Context(), c), nil
}

// «Ждёт тебя»: everything that waits for my answer, plus what I am waiting for myself
func (a *API) inbox(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return map[string]any{"rows": a.App.Inbox(r.Context(), c)}, nil
}

func (a *API) propose(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Kind    string          `json:"kind"`
		RefID   int64           `json:"ref_id"`
		Title   string          `json:"title"`
		Sub     string          `json:"sub"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := body(r, &in); err != nil {
		return nil, app.ErrBadInput
	}
	p, err := a.App.Propose(r.Context(), c, in.Kind, in.RefID, in.Title, in.Sub, string(in.Payload))
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": p.ID, "rows": a.App.Inbox(r.Context(), c)}, nil
}

func (a *API) answerProposal(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Yes bool `json:"yes"`
	}
	_ = body(r, &in)
	if _, err := a.App.AnswerProposal(r.Context(), c, id, in.Yes); err != nil {
		return nil, err
	}
	return map[string]any{"rows": a.App.Inbox(r.Context(), c)}, nil
}

func (a *API) withdrawProposal(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.proposalAct(r, c, a.App.WithdrawProposal)
}

func (a *API) seenProposal(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.proposalAct(r, c, a.App.SeenProposal)
}

func (a *API) remindProposal(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.proposalAct(r, c, a.App.RemindProposal)
}

func (a *API) proposalAct(r *http.Request, c app.Couple, f func(context.Context, app.Couple, int64) error) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	if err := f(r.Context(), c, id); err != nil {
		return nil, err
	}
	return map[string]any{"rows": a.App.Inbox(r.Context(), c)}, nil
}

func (a *API) markUrgent(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Note string `json:"note"`
	}
	_ = body(r, &in)
	if _, err := a.App.MarkUrgent(r.Context(), c, id, in.Note); err != nil {
		return nil, err
	}
	return map[string]any{"urgent": a.App.UrgentView(r.Context(), c, id)}, nil
}

func (a *API) cancelUrgent(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, a.App.CancelUrgent(r.Context(), c, id)
}

func (a *API) answerUrgent(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	msg, err := a.App.AnswerUrgent(r.Context(), c, id, r.PathValue("answer"))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"text": msg}
	if rd, err := a.App.S.OpenRound(r.Context(), c.Space.ID); err == nil {
		out["round_id"] = rd.ID
	}
	return out, nil
}

func (a *API) deleteIdea(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, a.App.S.DeleteIdea(r.Context(), c.Space.ID, id)
}

func (a *API) wish(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Wish string `json:"wish"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if !domain.Wish(in.Wish).Valid() {
		return nil, bad("bad_wish")
	}
	i, err := a.App.SetWish(r.Context(), c, id, domain.Wish(in.Wish))
	if err != nil {
		return nil, err
	}
	return map[string]any{"idea": viewIdea(c, i)}, nil
}

func (a *API) merge(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Into int64 `json:"into"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, a.App.Merge(r.Context(), c, id, in.Into)
}

// ---------- rounds ----------

type windowView struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

func (a *API) viewRound(ctx context.Context, c app.Couple, rd domain.Round) map[string]any {
	me, other := c.Me.Color, "b"
	if me == "b" {
		other = "a"
	}
	pick := func(col string, av, bv any) any {
		if col == "a" {
			return av
		}
		return bv
	}
	mySlots, _ := pick(me, rd.SlotsA, rd.SlotsB).([]domain.Slot)
	keys := []string{}
	for _, s := range mySlots {
		keys = append(keys, s.Code())
	}
	theirSlots, _ := pick(other, rd.SlotsA, rd.SlotsB).([]domain.Slot)
	theirKeys := []string{} // time is agreed in the open: each sees what the other proposed
	for _, s := range theirSlots {
		theirKeys = append(theirKeys, s.Code())
	}
	sentMe, _ := pick(me, rd.SentA, rd.SentB).(bool)
	sentOther, _ := pick(other, rd.SentA, rd.SentB).(bool)
	voteMe, _ := pick(me, rd.VoteA, rd.VoteB).(int64)
	voteOther, _ := pick(other, rd.VoteA, rd.VoteB).(int64)
	confMe, _ := pick(me, rd.ConfirmA, rd.ConfirmB).(bool)
	confOther, _ := pick(other, rd.ConfirmA, rd.ConfirmB).(bool)
	againMe, _ := pick(me, rd.AgainA, rd.AgainB).(string)
	againOther, _ := pick(other, rd.AgainA, rd.AgainB).(string) // how the partner liked it: the history shows both

	days := []string{}
	for _, d := range app.RoundDays(rd, c.Loc()) {
		days = append(days, d.Format("2006-01-02"))
	}
	first, last := app.SlotRange(a.App.Now(), c.Loc())
	out := map[string]any{"id": rd.ID, "today": first.Format("2006-01-02"), "horizon": last.Format("2006-01-02"), "state": string(rd.State), "version": rd.Version, "opened_at": rd.OpenedAt.Unix(), "expires_at": rd.ExpiresAt.Unix(),
		"days": days, "my_slots": keys, "partner_slots": theirKeys, "sent": sentMe, "partner_sent": sentOther, "voted": voteMe != 0, "partner_voted": voteOther != 0,
		"confirmed": confMe, "partner_confirmed": confOther, "again": againMe, "partner_again": againOther, "budget": rd.BudgetKop}
	if rd.State == domain.StateTime {
		// busy times from each person's own calendar: only «занят», never what
		n := int(last.Sub(first).Hours()/24) + 1
		list := func(m map[string]bool) []string {
			l := []string{}
			for k := range m {
				l = append(l, k)
			}
			sort.Strings(l)
			return l
		}
		if b := a.App.BusySlots(ctx, c.Me.ID, c.Loc(), n); b != nil {
			out["my_busy"] = list(b)
		}
		if c.Partner != nil {
			if b := a.App.BusySlots(ctx, c.Partner.ID, c.Loc(), n); b != nil {
				out["partner_busy"] = list(b)
			}
		}
	}
	if voteMe != 0 {
		out["my_vote"] = voteMe
	}
	if voteOther != 0 { // picks are made in the open: the partner's is visible at once
		out["partner_vote"] = voteOther
	}
	if sentMe && sentOther {
		wv := []windowView{}
		for _, w := range domain.CommonWindows(rd.SlotsA, rd.SlotsB, c.Loc()) {
			wv = append(wv, windowView{w.From.Unix(), w.To.Unix()})
		}
		out["windows"] = wv
	}
	trip := []ideaView{}
	for _, id := range rd.Triple {
		if i, err := a.App.S.Idea(ctx, c.Space.ID, id); err == nil {
			trip = append(trip, viewIdea(c, i))
		}
	}
	out["triple"] = trip
	if rd.Chosen != 0 {
		if i, err := a.App.S.Idea(ctx, c.Space.ID, rd.Chosen); err == nil {
			out["chosen"] = a.withPlaces(ctx, c, viewIdea(c, i), i)
		}
	}
	if !rd.StartsAt.IsZero() {
		out["starts_at"], out["ends_at"] = rd.StartsAt.Unix(), rd.EndsAt.Unix()
		out["when"] = domain.When(rd.StartsAt, c.Loc())
		if rd.State == domain.StatePlanned || rd.State == domain.StateConfirm {
			ics, g := a.App.EventLinks(ctx, c, c.Me.ID, rd)
			out["cal"] = map[string]any{"ics": ics, "google": g, "subscribed": a.App.Subscribed(ctx, c.Me.ID)}
			if i, err := a.App.S.Idea(ctx, c.Space.ID, rd.Chosen); err == nil {
				if l, u := app.BookLink(i); u != "" {
					out["book"] = map[string]string{"label": l, "url": u}
				}
			}
			switch t := a.App.Tickets(ctx, c.Space.ID, rd.ID); {
			case t == c.Me.ID:
				out["tickets"] = "me"
			case t != 0:
				out["tickets"] = "partner"
			}
			if f := a.App.TicketFile(ctx, c.Space.ID, rd.ID); f.Kind != "" {
				out["tickets_file"] = map[string]any{"kind": f.Kind, "url": a.App.TicketsLink(f), "name": f.Name, "mine": f.By == c.Me.ID}
			}
		}
	}
	if rd.State == domain.StateConfirm || rd.State == domain.StatePlanned || rd.State == domain.StateDone {
		route := []map[string]any{}
		for _, st := range a.App.Route(ctx, c, rd) {
			route = append(route, map[string]any{"id": st.Extra.ID, "idea": viewIdea(c, st.Idea), "starts_at": st.Start.Unix(), "ends_at": st.End.Unix(),
				"state": st.Extra.State, "mine": st.Extra.ProposedBy == c.Me.ID, "tight": st.Tight})
		}
		out["route"] = route
		if rd.State != domain.StateDone && len(route) < app.MaxExtras {
			sug := []ideaView{}
			for _, i := range a.App.SuggestExtras(ctx, c, rd) {
				sug = append(sug, viewIdea(c, i))
			}
			out["extra_suggest"] = sug
		}
	}
	if rd.State == domain.StateVote && rd.VotedBoth() {
		if cp := a.App.Compromise(ctx, c, rd); cp != nil {
			out["compromise"] = viewIdea(c, *cp)
		}
	}
	if ph, err := a.App.S.Photos(ctx, rd.ID); err == nil && len(ph) > 0 {
		out["photos"] = len(ph)
	}
	return out
}

func (a *API) newRound(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Day string `json:"day"`
	}
	if r.ContentLength > 0 {
		if err := body(r, &in); err != nil {
			return nil, err
		}
	}
	var days []string
	if in.Day != "" {
		d, ok := app.DaysFrom(in.Day, a.App.Now(), c.Loc())
		if !ok {
			return nil, bad("bad_day")
		}
		days = d
	}
	rd, err := a.App.GoOn(r.Context(), c, days)
	if err != nil {
		return nil, err
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) roundWith(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "idea")
	if err != nil {
		return nil, err
	}
	var rd domain.Round
	if r.URL.Query().Get("replace") == "1" {
		rd, err = a.App.SwapIdea(r.Context(), c, id)
	} else {
		// an event with its own date and time goes straight to «confirm» — no time picking — then to the calendar
		rd, err = a.App.BookFixed(r.Context(), c, id)
		if errors.Is(err, app.ErrNoTime) {
			rd, err = a.App.GoWith(r.Context(), c, id)
		}
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) currentRound(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	rd, err := a.App.S.OpenRound(r.Context(), c.Space.ID)
	if errors.Is(err, store.ErrNotFound) {
		return map[string]any{"round": nil}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) round(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	rd, err := a.App.S.Round(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) addExtra(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		IdeaID int64  `json:"idea_id"`
		Text   string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if _, err := a.App.ProposeExtra(r.Context(), c, id, in.IdeaID, in.Text); err != nil {
		return nil, extraErr(err)
	}
	rd, err := a.App.S.Round(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) answerExtra(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	ans := r.PathValue("answer")
	if ans != "accept" && ans != "decline" {
		return nil, bad("bad_answer")
	}
	rd, err := a.App.AnswerExtra(r.Context(), c, id, ans == "accept")
	if err != nil {
		return nil, extraErr(err)
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) delExtra(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	rd, err := a.App.RemoveExtra(r.Context(), c, id)
	if err != nil {
		return nil, extraErr(err)
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func extraErr(err error) error {
	switch {
	case errors.Is(err, app.ErrExtraState):
		return httpErr{409, "extra_state"}
	case errors.Is(err, app.ErrExtraLimit):
		return httpErr{409, "extra_limit"}
	case errors.Is(err, app.ErrExtraOwn):
		return httpErr{409, "extra_own"}
	}
	return err
}

func (a *API) roundAction(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Slots  []string `json:"slots"`
		IdeaID int64    `json:"idea_id"`
		Answer string   `json:"answer"`
		At     string   `json:"at"`
	}
	if r.ContentLength != 0 {
		if err := body(r, &in); err != nil {
			return nil, err
		}
	}
	ctx := r.Context()
	var rd domain.Round
	switch r.PathValue("action") {
	case "slots":
		rd, err = a.App.SubmitSlots(ctx, c, id, in.Slots)
	case "vote":
		rd, err = a.App.Vote(ctx, c, id, in.IdeaID)
	case "reshuffle":
		rd, err = a.App.Reshuffle(ctx, c, id)
	case "confirm":
		rd, err = a.App.Confirm(ctx, c, id)
	case "other-time":
		rd, err = a.App.OtherTime(ctx, c, id)
	case "time": // the exact time of an agreed meeting: {"at":"14:00"}
		rd, err = a.App.ExactTime(ctx, c, id, in.At)
	case "cancel":
		rd, err = a.App.Cancel(ctx, c, id)
	case "done":
		rd, err = a.App.Done(ctx, c, id, in.Answer)
	case "again":
		if in.Answer != "yes" && in.Answer != "no" {
			return nil, bad("bad_answer")
		}
		rd, err = a.App.Again(ctx, c, id, in.Answer)
	case "tickets":
		if err = a.App.GotTickets(ctx, c, id); err == nil {
			rd, err = a.App.S.Round(ctx, c.Space.ID, id)
		}
	default:
		return nil, httpErr{404, "not_found"}
	}
	if err != nil {
		return nil, err
	}
	fresh, ferr := a.App.S.Round(ctx, c.Space.ID, rd.ID)
	if ferr == nil {
		rd = fresh
	}
	return map[string]any{"round": a.viewRound(ctx, c, rd)}, nil
}

func (a *API) ics(w http.ResponseWriter, r *http.Request, c app.Couple) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rd, err := a.App.S.Round(r.Context(), c.Space.ID, id)
	if err != nil {
		return err
	}
	if rd.StartsAt.IsZero() {
		return httpErr{409, "no_time"}
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="vdvoem.ics"`)
	_, err = w.Write(a.App.ICS(r.Context(), c, rd, rd.Version, rd.State == domain.StateCancelled))
	return err
}

func (a *API) suggest(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var ex []int64
	for _, p := range strings.Split(r.URL.Query().Get("exclude"), ",") {
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			ex = append(ex, id)
		}
	}
	s, err := a.App.SuggestFrom(r.Context(), c, domain.SuggestMode(r.URL.Query().Get("mode")), ex, r.URL.Query().Get("from"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"idea": viewIdea(c, s.Idea), "why": s.Why, "mode": string(s.Mode), "from": r.URL.Query().Get("from")}, nil
}

// going: «Мы идём» — meetings both said yes to, still ahead (the soonest first), and a choice that has agreed on
// time and waits for the last «да»
func (a *API) going(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return map[string]any{"meetings": a.goingRounds(r.Context(), c)}, nil
}

func (a *API) goingRounds(ctx context.Context, c app.Couple) []map[string]any {
	rs, _ := a.App.S.RecentRounds(ctx, c.Space.ID, 60)
	now := a.App.Now()
	var list []domain.Round
	for _, rd := range rs {
		if rd.State == domain.StatePlanned && rd.StartsAt.After(now.Add(-2*time.Hour)) || rd.State == domain.StateConfirm {
			list = append(list, rd)
		}
	}
	sort.SliceStable(list, func(x, y int) bool { return list[x].StartsAt.Before(list[y].StartsAt) })
	out := []map[string]any{}
	for _, rd := range list {
		out = append(out, a.viewRound(ctx, c, rd))
	}
	return out
}

// goingIdeas: ideas already on the way (planned, or in a choice that is going on) — not asked to be marked again
func (a *API) goingIdeas(ctx context.Context, c app.Couple) map[int64]bool {
	rs, _ := a.App.S.RecentRounds(ctx, c.Space.ID, 60)
	now := a.App.Now()
	out := map[int64]bool{}
	for _, rd := range rs {
		if rd.Chosen != 0 && (rd.State == domain.StatePlanned && rd.StartsAt.After(now) || rd.State == domain.StateConfirm || rd.State == domain.StateTime || rd.State == domain.StateVote) {
			out[rd.Chosen] = true
		}
	}
	return out
}

func (a *API) history(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	rs, err := a.App.S.RecentRounds(r.Context(), c.Space.ID, 40)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, rd := range rs {
		if rd.State == domain.StateDone || (rd.State == domain.StatePlanned && rd.StartsAt.Before(a.App.Now())) {
			out = append(out, a.viewRound(r.Context(), c, rd))
		}
	}
	return map[string]any{"meetings": out}, nil
}

func (a *API) settings(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	sp := c.Space
	return map[string]any{"city": sp.City, "tz": sp.TZ, "plan_weekday": sp.PlanWeekday, "plan_hour": sp.PlanHour, "budget": sp.BudgetKop,
		"suggest_mode": sp.SuggestMode, "suggest_weekday": sp.SuggestWeekday, "suggest_hour": sp.SuggestHour, "paired": c.Partner != nil}, nil
}

func (a *API) patchSettings(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		City           *string `json:"city"`
		TZ             *string `json:"tz"`
		PlanWeekday    *int    `json:"plan_weekday"`
		PlanHour       *int    `json:"plan_hour"`
		Budget         *int64  `json:"budget"`
		SuggestMode    *string `json:"suggest_mode"`
		SuggestWeekday *int    `json:"suggest_weekday"`
		SuggestHour    *int    `json:"suggest_hour"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	sp := c.Space
	if in.City != nil {
		sp.City = strings.TrimSpace(*in.City)
	}
	if in.TZ != nil {
		if _, err := time.LoadLocation(*in.TZ); err != nil {
			return nil, bad("bad_tz")
		}
		sp.TZ = *in.TZ
	}
	wd := func(p *int, dst *int) {
		if p != nil && *p >= 0 && *p <= 6 {
			*dst = *p
		}
	}
	hr := func(p *int, dst *int) {
		if p != nil && *p >= 9 && *p <= 22 { // never at night
			*dst = *p
		}
	}
	wd(in.PlanWeekday, &sp.PlanWeekday)
	hr(in.PlanHour, &sp.PlanHour)
	wd(in.SuggestWeekday, &sp.SuggestWeekday)
	hr(in.SuggestHour, &sp.SuggestHour)
	if in.Budget != nil && *in.Budget >= 0 {
		sp.BudgetKop = *in.Budget
	}
	if in.SuggestMode != nil {
		if !domain.SuggestMode(*in.SuggestMode).Valid() {
			return nil, bad("bad_mode")
		}
		sp.SuggestMode = *in.SuggestMode
	}
	if err := a.App.S.UpdateSpace(r.Context(), sp); err != nil {
		return nil, err
	}
	c.Space = sp
	return a.settings(w, r, c)
}

func (a *API) invite(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	if c.Partner != nil {
		return nil, httpErr{409, "already_paired"}
	}
	link, err := a.App.NewInvite(r.Context(), c)
	return map[string]string{"link": link}, err
}

// ---------- помощник ----------

func (a *API) chat(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	h, err := a.Helper.History(r.Context(), c)
	return map[string]any{"messages": h, "ai": a.Helper.AI.Enabled(), "limit": assistant.DailyLimit, "voice": a.STT.Enabled()}, err
}

func (a *API) hints(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.Helper.Hints(r.Context(), c)
}

// voice: a recording from the composer (multipart «audio») → heard text → the usual answer
func (a *API) voice(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	if !a.STT.Enabled() {
		return nil, httpErr{404, "voice_off"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, stt.MaxBytes+64<<10)
	f, h, err := r.FormFile("audio")
	if err != nil {
		return nil, bad("bad_audio")
	}
	defer f.Close()
	audio, err := io.ReadAll(io.LimitReader(f, stt.MaxBytes+1))
	if err != nil || len(audio) > stt.MaxBytes {
		return nil, bad("audio_too_long")
	}
	text, err := a.STT.Text(r.Context(), audio, h.Filename)
	if errors.Is(err, stt.ErrEmpty) {
		return map[string]any{"heard": ""}, nil
	}
	if err != nil {
		a.Log.Warn("voice", "err", err)
		return nil, httpErr{502, "voice_failed"}
	}
	ctx, cancel := context.WithTimeout(ai.Foreground(context.WithoutCancel(r.Context())), 90*time.Second)
	defer cancel()
	rep, err := a.Helper.Ask(ctx, c, text)
	return map[string]any{"heard": text, "reply": rep}, err
}

func (a *API) askAssistant(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	// the answer is worked out to the end and saved even if the app leaves the screen or drops the connection
	ctx, cancel := context.WithTimeout(ai.Foreground(context.WithoutCancel(r.Context())), 90*time.Second) // someone waits: first in the model's queue
	defer cancel()
	rep, err := a.Helper.Ask(ctx, c, in.Text)
	if err != nil && a.App != nil { // a failure is a row in the journal too: the owner sees what broke and when
		if id, e := a.App.S.AddExample(context.WithoutCancel(r.Context()), c.Me.ID, in.Text, "error", "", time.Now()); e == nil {
			_ = a.App.S.SetExampleMeta(context.WithoutCancel(r.Context()), id, 0, "", "", 0, clipErr(err))
		}
	}
	return map[string]any{"reply": rep}, err
}

func (a *API) act(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	rep, err := a.Helper.Act(ai.Foreground(r.Context()), c, in.ID)
	return map[string]any{"reply": rep}, err
}

// ticketsFile: the tickets from the app — a file (multipart «file», ≤ 15 MB) or a link ({"url": …})
func (a *API) ticketsFile(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, 15<<20+64<<10)
		f, h, err := r.FormFile("file")
		if err != nil {
			return nil, bad("bad_file")
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 15<<20+1))
		if err != nil || len(data) > 15<<20 || len(data) == 0 {
			return nil, bad("bad_file")
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
		defer cancel()
		if err := a.App.UploadTickets(ctx, c, id, h.Filename, data); err != nil {
			return nil, err
		}
	} else {
		var in struct {
			URL string `json:"url"`
		}
		if err := body(r, &in); err != nil || parse.FirstURL(in.URL) == "" {
			return nil, bad("bad_url")
		}
		if err := a.App.AttachTickets(r.Context(), c, id, app.TicketFile{Kind: "link", URL: parse.FirstURL(in.URL)}); err != nil {
			return nil, err
		}
	}
	rd, err := a.App.S.Round(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"round": a.viewRound(r.Context(), c, rd)}, nil
}

func (a *API) clearChat(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return map[string]bool{"ok": true}, a.App.S.ClearChat(r.Context(), c.Me.ID)
}

// ---------- static Mini App ----------

func (a *API) static(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/app/")
	p := filepath.Join(a.WebDir, filepath.FromSlash(filepath.Clean("/"+rel)))
	if strings.HasPrefix(rel, "assets/") {
		if _, err := os.Stat(p); err != nil {
			// an app opened before an update still asks for the old hashed files: serve them from a previous release
			if old := a.oldAsset(rel); old != "" {
				p = old
			} else {
				http.NotFound(w, r) // never index.html for a script: the page would hang on Telegram's loading screen
				return
			}
		}
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		if strings.HasPrefix(rel, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if rel == "telegram-web-app.js" || strings.HasSuffix(rel, ".png") || strings.HasSuffix(rel, ".svg") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		if strings.HasSuffix(rel, ".webmanifest") {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		http.ServeFile(w, r, p)
		return
	}
	w.Header().Set("Cache-Control", "no-store") // the page always comes fresh; hashed assets are cached forever
	http.ServeFile(w, r, filepath.Join(a.WebDir, "index.html"))
}

// oldAsset looks for a hashed asset in the other releases next to the current one (/opt/vdvoem/releases/*/web).
func (a *API) oldAsset(rel string) string {
	name := filepath.Base(rel)
	if name == "." || strings.ContainsAny(name, `/\`) {
		return ""
	}
	web, err := filepath.EvalSymlinks(a.WebDir)
	if err != nil {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(web)), "*", "web", "assets", name))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// devAdvance moves the e2e clock forward and runs the scheduler (only with DEV_TOKEN).
func (a *API) devAdvance(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Dev-Token")), []byte(a.DevToken)) != 1 || a.Advance == nil {
		http.Error(w, "forbidden", 403)
		return
	}
	var in struct {
		Minutes int    `json:"minutes"`
		To      string `json:"to"` // RFC3339: jump to this moment
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.To != "" {
		if t, err := time.Parse(time.RFC3339, in.To); err == nil && t.After(a.App.Now()) {
			a.Advance(t.Sub(a.App.Now()))
		}
	} else {
		a.Advance(time.Duration(in.Minutes) * time.Minute)
	}
	a.App.NoKick = true
	a.App.Tick(r.Context())
	a.App.Tick(r.Context())
	a.App.NoKick = false
	writeJSON(w, 200, map[string]any{"now": a.App.Now().Format(time.RFC3339)})
}

type optionView struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Address string `json:"address,omitempty"`
	Image   string `json:"image,omitempty"`
	Note    string `json:"note,omitempty"`
	Price   int64  `json:"price,omitempty"`
	Author  string `json:"author"` // a | b | ai
	Chosen  bool   `json:"chosen"`
	Parsed  bool   `json:"parsed"`
	Maps    string `json:"maps"`
}

type placeView struct {
	Title   string `json:"title"`
	Address string `json:"address,omitempty"`
	URL     string `json:"url,omitempty"`
	Maps    string `json:"maps"`
	Chosen  bool   `json:"chosen"`
}

// withPlaces adds the «Где именно» options and the chosen place to an idea view.
func (a *API) withPlaces(ctx context.Context, c app.Couple, v ideaView, i store.Idea) ideaView {
	opts, _ := a.App.S.Options(ctx, i.ID)
	chosen, _ := a.App.S.ChosenOption(ctx, i.ID)
	city := c.Space.City
	if city == "" {
		city = "Москва"
	}
	colors := map[int64]string{c.Me.ID: c.Me.Color}
	if c.Partner != nil {
		colors[c.Partner.ID] = c.Partner.Color
	}
	v.Options = []optionView{}
	for _, o := range opts {
		au := colors[o.MemberID]
		if o.MemberID == 0 {
			au = "ai"
		}
		q := o.Title
		if o.Address != "" {
			q = o.Title + ", " + o.Address
		}
		v.Options = append(v.Options, optionView{ID: o.ID, Title: o.Title, URL: o.URL, Address: o.Address, Image: o.ImageURL, Note: o.Note, Price: o.PriceKop,
			Author: au, Chosen: o.ID == chosen, Parsed: o.Parsed, Maps: app.MapsLink(q, city)})
	}
	p := a.App.PlaceOf(ctx, c, i)
	v.Place = &placeView{Title: p.Title, Address: p.Address, URL: p.URL, Maps: p.Maps, Chosen: p.Chosen}
	return v
}

func (a *API) options(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	i, err := a.App.S.Idea(r.Context(), c.Space.ID, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"idea": a.withPlaces(r.Context(), c, viewIdea(c, i), i)}, nil
}

func (a *API) addOption(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, bad("empty")
	}
	if _, err := a.App.AddOption(r.Context(), c, id, in.Text); err != nil {
		return nil, err
	}
	return a.options(w, r, c)
}

func (a *API) suggestOptions(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	if _, err := a.App.SuggestPlaces(r.Context(), c, id); errors.Is(err, app.ErrNoAI) {
		return nil, httpErr{409, "no_ai"}
	} else if err != nil {
		a.Log.Info("suggest places failed", "err", err)
		return nil, httpErr{502, "no_places"}
	}
	return a.options(w, r, c)
}

func (a *API) chooseOption(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	opt, err := pathID(r, "opt")
	if err != nil {
		return nil, err
	}
	if err := a.App.ChooseOption(r.Context(), c, id, opt); err != nil {
		return nil, err
	}
	return a.options(w, r, c)
}

func (a *API) delOption(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	opt, err := pathID(r, "opt")
	if err != nil {
		return nil, err
	}
	if _, err := a.App.S.Idea(r.Context(), c.Space.ID, id); err != nil {
		return nil, err
	}
	if err := a.App.S.DeleteOption(r.Context(), id, opt); err != nil {
		return nil, err
	}
	return a.options(w, r, c)
}

func clipErr(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// timedWriter stamps the server's own time on the response (X-Server-Ms), so a phone that waited long can tell
// whether the time went to the network or to the server
type timedWriter struct {
	http.ResponseWriter
	t    time.Time
	done bool
}

func (w *timedWriter) stamp() {
	if !w.done {
		w.done = true
		w.Header().Set("X-Server-Ms", strconv.FormatInt(time.Since(w.t).Milliseconds(), 10))
	}
}
func (w *timedWriter) WriteHeader(c int)           { w.stamp(); w.ResponseWriter.WriteHeader(c) }
func (w *timedWriter) Write(b []byte) (int, error) { w.stamp(); return w.ResponseWriter.Write(b) }
func (w *timedWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
