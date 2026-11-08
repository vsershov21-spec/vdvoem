package api

import (
	"errors"
	"net/http"
	"strings"

	"vdvoem/internal/app"
)

// The calendar subscription: GET /cal/<token>.ics (no login — the token is the key), and the person's own link.
func (a *API) feedRoutes(mux *http.ServeMux, h func(string, handler)) {
	mux.HandleFunc("GET /cal/{file}", func(w http.ResponseWriter, r *http.Request) {
		b, err := a.App.Feed(r.Context(), strings.TrimSuffix(r.PathValue("file"), ".ics"))
		if errors.Is(err, app.ErrNoFeed) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "try later", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	})
	h("GET /api/calendar/feed", a.feedLink(false))
	h("POST /api/calendar/feed/reset", a.feedLink(true))
}

func (a *API) feedLink(reset bool) func(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return func(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
		t, err := a.App.FeedToken(r.Context(), c, reset)
		if err != nil {
			return nil, err
		}
		https := strings.TrimRight(a.App.PublicURL, "/") + "/cal/" + t + ".ics"
		return map[string]any{"url": https, "webcal": "webcal://" + strings.TrimPrefix(strings.TrimPrefix(https, "https://"), "http://"),
			"subscribed": a.App.Subscribed(r.Context(), c.Me.ID)}, nil
	}
}
