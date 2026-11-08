package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"vdvoem/internal/app"
	"vdvoem/internal/assistant"
)

// The scout agent (a scheduled Claude task) talks to the app from the server itself, through the admin
// shell: curl http://127.0.0.1:8099/agent/… These routes answer only direct loopback requests — anything that
// came through Caddy carries X-Forwarded-For and is refused, so nothing here is reachable from the internet.
// The agent can read taste briefs and add ideas; it cannot plan, vote, cancel or delete.

func local(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-Ip") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("Via") != "" {
			http.NotFound(w, r)
			return
		}
		f(w, r)
	}
}

func (a *API) agentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /agent/briefs", local(a.agentBriefs))
	mux.HandleFunc("POST /agent/picks", local(a.agentPicks))
	mux.HandleFunc("POST /agent/prices", local(a.agentPrices))
	mux.HandleFunc("POST /agent/goals/{id}/done", local(a.agentGoalDone))
	mux.HandleFunc("POST /agent/answers", local(a.agentAnswers))
	mux.HandleFunc("POST /agent/channel", local(a.agentChannel))
	mux.HandleFunc("GET /agent/model", local(a.agentModel))
	mux.HandleFunc("POST /agent/model", local(a.agentModel))
	mux.HandleFunc("GET /agent/learn", local(a.agentLearn))
	mux.HandleFunc("POST /agent/learn", local(a.agentLabel))
	mux.HandleFunc("GET /agent/train", local(a.agentTrain))
}

// agentLearn: how live requests ended over the last days, and the misses to label (GET ?days=7)
func (a *API) agentLearn(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 7
	}
	since := a.App.Now().AddDate(0, 0, -days)
	out, err := a.App.S.Outcomes(r.Context(), since)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	ms, _ := a.App.S.Misses(r.Context(), since)
	type miss struct {
		ID      int64           `json:"id"`
		Text    string          `json:"text"`
		Was     json.RawMessage `json:"was"`
		Outcome string          `json:"outcome"`
		Src     string          `json:"src"`
	}
	list := []miss{}
	for _, m := range ms {
		was := json.RawMessage(strings.TrimPrefix(m.Calls, "intent:"))
		if !json.Valid(was) {
			was = json.RawMessage("null")
		}
		list = append(list, miss{m.ID, m.Input, was, m.Outcome, m.Src})
	}
	writeJSON(w, 200, map[string]any{"days": days, "outcomes": out, "misses": list,
		"how": "POST /agent/learn {\"labels\":[{\"id\":<miss id>,\"intent\":{…the right reading, same format as was…}}]} — each becomes a training example; GET /agent/train — the dataset (JSONL) for the next fine-tune"})
}

// agentLabel: the right readings of missed requests
func (a *API) agentLabel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Labels []struct {
			ID     int64           `json:"id"`
			Intent json.RawMessage `json:"intent"`
		} `json:"labels"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	n, bad := 0, []int64{}
	for _, l := range in.Labels {
		if !assistant.ValidIntent(l.Intent) || a.App.S.LabelExample(r.Context(), l.ID, "intent:"+string(l.Intent), a.App.Now()) != nil {
			bad = append(bad, l.ID)
			continue
		}
		n++
	}
	writeJSON(w, 200, map[string]any{"labeled": n, "rejected": bad})
}

// agentTrain: gold cases + every accepted live example, in the fine-tune format (JSONL)
func (a *API) agentTrain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	_, _ = assistant.ExportIntents(r.Context(), a.App.S, w)
}

// agentModel: which model reads the assistant's requests; POST {"model":"vdvoem-intent"} switches (""/"default" — back)
func (a *API) agentModel(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			Model string `json:"model"`
		}
		if err := readJSON(r, &in); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		if in.Model == "default" {
			in.Model = ""
		}
		if err := a.App.S.SetSetting(r.Context(), "intent_model", in.Model); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		a.Helper.SetIntentModel(in.Model)
		go a.Helper.Warm(context.WithoutCancel(r.Context()))
	}
	c := a.Helper.IntentModel()
	m := ""
	if c != nil {
		m = c.M()
	}
	writeJSON(w, 200, map[string]any{"intent_model": m, "tuned": assistant.Tuned(m)})
}

// agentChannel: a channel's posts at once (an export) → events in «Афиша»
func (a *API) agentChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Space int64    `json:"space"`
		From  string   `json:"from"`
		Posts []string `json:"posts"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	n, err := a.App.ChannelImport(r.Context(), in.Space, in.From, in.Posts)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"added": n, "posts": len(in.Posts)})
}

func (a *API) agentBriefs(w http.ResponseWriter, r *http.Request) {
	b, err := a.App.ScoutBriefs(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"briefs": b, "now": a.App.Now().Format("2006-01-02T15:04:05-07:00"),
		"how": "POST /agent/picks {space, picks:[{title,url,image,address,category,starts,ends,price_rub,duration_min,why,score,horizon,on_sale,goal_id,about,genres,cast,age}]} — 15–20 per space, horizon week|later; POST /agent/prices {space, updates:[{idea_id,price_rub,ends,status,about,genres,cast,age,url}]} — also for briefs[].enrich; POST /agent/answers {space, answers:[{id,text,price_rub,url}]} — for briefs[].questions"})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func (a *API) agentPicks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Space int64           `json:"space"`
		Picks []app.ScoutPick `json:"picks"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json: " + err.Error()})
		return
	}
	res, err := a.App.ScoutPicks(r.Context(), in.Space, in.Picks)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"added": res.Added, "skipped": res.Skipped, "to_bank_and_bot": res.Promoted})
}

func (a *API) agentPrices(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Space   int64            `json:"space"`
		Updates []app.ScoutPrice `json:"updates"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	n, err := a.App.ScoutPrices(r.Context(), in.Space, in.Updates)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"updated": n})
}

func (a *API) agentAnswers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Space   int64             `json:"space"`
		Answers []app.ScoutAnswer `json:"answers"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	n, err := a.App.ScoutAnswers(r.Context(), in.Space, in.Answers)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"answered": n})
}

func (a *API) agentGoalDone(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sp, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	if err := a.App.S.CloseGoal(r.Context(), sp, id); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
