package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"vdvoem/internal/app"
	"vdvoem/internal/passkey"
	"vdvoem/internal/store"
)

const cookieName = "vd_s"

func (a *API) secureCookie() bool { return strings.HasPrefix(a.App.PublicURL, "https://") }

func (a *API) setSession(w http.ResponseWriter, r *http.Request, memberID int64, method string) error {
	t, err := a.App.NewSession(r.Context(), memberID, method, r.UserAgent())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: t, Path: "/", HttpOnly: true, Secure: a.secureCookie(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(app.SessionTTL / time.Second)})
	return nil
}

func (a *API) sessionMember(r *http.Request) (int64, bool) {
	ck, err := r.Cookie(cookieName)
	if err != nil || len(ck.Value) < 32 {
		return 0, false
	}
	id, err := a.App.S.SessionMember(r.Context(), ck.Value, a.App.Now())
	return id, err == nil
}

// public wraps unauthenticated endpoints (the login screen).
func (a *API) public(f func(w http.ResponseWriter, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		// a login attempt from another origin is refused (CSRF on cookie-setting endpoints)
		if o := r.Header.Get("Origin"); o != "" && o != strings.TrimRight(a.App.PublicURL, "/") {
			writeJSON(w, 403, map[string]string{"error": "origin"})
			return
		}
		out, err := f(w, r)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	}
}

func (a *API) AuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/state", a.public(a.authState))
	mux.HandleFunc("POST /api/auth/telegram", a.public(a.tgStart))
	mux.HandleFunc("GET /api/auth/telegram/{code}", a.public(a.tgPoll))
	mux.HandleFunc("POST /api/auth/password", a.public(a.passLogin))
	mux.HandleFunc("POST /api/auth/passkey/begin", a.public(a.passkeyBegin))
	mux.HandleFunc("POST /api/auth/passkey/finish", a.public(a.passkeyFinish))
	mux.HandleFunc("POST /api/auth/logout", a.public(a.logout))

	h := func(pattern string, f func(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error)) {
		mux.HandleFunc(pattern, a.auth(f))
	}
	h("GET /api/security", a.security)
	h("POST /api/security/password", a.setPassword)
	h("DELETE /api/security/password", a.delPassword)
	h("POST /api/security/passkey/begin", a.regBegin)
	h("POST /api/security/passkey/finish", a.regFinish)
	h("DELETE /api/security/passkeys/{id}", a.delPasskey)
	h("PATCH /api/security/prefs", a.prefs)
	h("POST /api/security/logout-all", a.logoutAll)
	h("DELETE /api/security/sessions/{id}", a.delSession)
	h("GET /api/stats", a.stats)
	h("GET /api/calendar", a.calendar)
	h("POST /api/ideas/{id}/done", a.choreDone)
}

// authState tells the lamp screen whether this browser already has a session.
func (a *API) authState(w http.ResponseWriter, r *http.Request) (any, error) {
	_, ok := a.sessionMember(r)
	return map[string]any{"session": ok, "bot": a.App.BotName}, nil
}

func (a *API) tgStart(w http.ResponseWriter, r *http.Request) (any, error) {
	code, link, err := a.App.LoginCode(r.Context())
	return map[string]any{"code": code, "link": link, "expires_in": int(app.LoginCodeTTL / time.Second)}, err
}

func (a *API) tgPoll(w http.ResponseWriter, r *http.Request) (any, error) {
	id, alive, err := a.App.PollLogin(r.Context(), r.PathValue("code"))
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return map[string]any{"ok": false, "alive": alive}, nil
	}
	return map[string]any{"ok": true}, a.setSession(w, r, id, "telegram")
}

func (a *API) passLogin(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct{ Login, Password string }
	if err := body(r, &in); err != nil {
		return nil, err
	}
	id, err := a.App.PasswordLogin(r.Context(), in.Login, in.Password)
	switch {
	case errors.Is(err, app.ErrBadLogin):
		return nil, httpErr{401, "bad_login"}
	case errors.Is(err, app.ErrLocked):
		return nil, httpErr{429, "locked"}
	case err != nil:
		return nil, err
	}
	return map[string]any{"ok": true}, a.setSession(w, r, id, "password")
}

func (a *API) passkeyBegin(w http.ResponseWriter, r *http.Request) (any, error) {
	ch, err := a.App.PasskeyChallenge(r.Context(), 0, "login")
	return map[string]any{"challenge": ch, "rp_id": a.App.RP().ID, "timeout": 60000}, err
}

func (a *API) passkeyFinish(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		passkey.Assertion
		Challenge string `json:"challenge"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	id, err := a.App.PasskeyLogin(r.Context(), in.Assertion, in.Challenge)
	if err != nil {
		a.Log.Info("passkey login failed", "err", err)
		return nil, httpErr{401, "bad_passkey"}
	}
	return map[string]any{"ok": true}, a.setSession(w, r, id, "passkey")
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) (any, error) {
	if ck, err := r.Cookie(cookieName); err == nil {
		_ = a.App.S.DeleteSession(r.Context(), ck.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureCookie(), SameSite: http.SameSiteLaxMode})
	return map[string]bool{"ok": true}, nil
}

// ---------- security settings ----------

func (a *API) security(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	ctx := r.Context()
	out := map[string]any{"biometric_lock": c.Me.BiometricLock, "theme": c.Me.Theme, "rp_id": a.App.RP().ID}
	if p, err := a.App.S.PasswordOf(ctx, c.Me.ID); err == nil {
		out["login"] = p.Login
	}
	keys, _ := a.App.S.Passkeys(ctx, c.Me.ID)
	kv := []map[string]any{}
	for _, k := range keys {
		m := map[string]any{"id": k.ID, "name": k.Name, "created_at": k.CreatedAt.Unix()}
		if k.UsedAt != nil {
			m["used_at"] = k.UsedAt.Unix()
		}
		kv = append(kv, m)
	}
	out["passkeys"] = kv
	ss, _ := a.App.S.Sessions(ctx, c.Me.ID, a.App.Now())
	cur := ""
	if ck, err := r.Cookie(cookieName); err == nil {
		cur = store.HashToken(ck.Value)
	}
	sv := []map[string]any{}
	for _, s := range ss {
		sv = append(sv, map[string]any{"id": s.ID, "method": s.Method, "device": device(s.UA), "seen_at": s.SeenAt.Unix(), "current": s.ID == cur})
	}
	out["sessions"] = sv
	return out, nil
}

func device(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "iphone"):
		return "iPhone"
	case strings.Contains(l, "ipad"):
		return "iPad"
	case strings.Contains(l, "android"):
		return "Android"
	case strings.Contains(l, "mac os"):
		return "Mac"
	case strings.Contains(l, "windows"):
		return "Windows"
	case strings.Contains(l, "linux"):
		return "Linux"
	}
	return "Браузер"
}

func (a *API) setPassword(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct{ Login, Password string }
	if err := body(r, &in); err != nil {
		return nil, err
	}
	switch err := a.App.SetPassword(r.Context(), c, in.Login, in.Password); {
	case errors.Is(err, app.ErrBadLoginID):
		return nil, bad("bad_login_name")
	case errors.Is(err, app.ErrWeakPass):
		return nil, bad("weak_password")
	case errors.Is(err, app.ErrLoginTaken):
		return nil, httpErr{409, "login_taken"}
	case err != nil:
		return nil, err
	}
	return a.security(w, r, c)
}

func (a *API) delPassword(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	if err := a.App.S.DeletePassword(r.Context(), c.Me.ID); err != nil {
		return nil, err
	}
	return a.security(w, r, c)
}

func (a *API) regBegin(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	ch, err := a.App.PasskeyChallenge(r.Context(), c.Me.ID, "register")
	if err != nil {
		return nil, err
	}
	keys, _ := a.App.S.Passkeys(r.Context(), c.Me.ID)
	ex := []string{}
	for _, k := range keys {
		ex = append(ex, k.ID)
	}
	return map[string]any{"challenge": ch, "rp": map[string]string{"id": a.App.RP().ID, "name": "Вдвоём"},
		"user":    map[string]string{"id": passkey.B64([]byte("m" + itoa(c.Me.ID))), "name": c.Me.Name, "display_name": c.Me.Name},
		"exclude": ex, "timeout": 60000}, nil
}

func (a *API) regFinish(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		passkey.Registration
		Challenge string `json:"challenge"`
		Name      string `json:"name"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if err := a.App.AddPasskey(r.Context(), c, in.Registration, in.Challenge, in.Name); err != nil {
		a.Log.Info("passkey registration failed", "err", err)
		return nil, bad("bad_passkey")
	}
	return a.security(w, r, c)
}

func (a *API) delPasskey(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	if err := a.App.S.DeletePasskey(r.Context(), c.Me.ID, r.PathValue("id")); err != nil {
		return nil, err
	}
	return a.security(w, r, c)
}

func (a *API) prefs(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	var in struct {
		BiometricLock *bool   `json:"biometric_lock"`
		Theme         *string `json:"theme"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if in.BiometricLock != nil {
		c.Me.BiometricLock = *in.BiometricLock
	}
	if in.Theme != nil && (*in.Theme == "" || *in.Theme == "light" || *in.Theme == "dark") {
		c.Me.Theme = *in.Theme
	}
	if err := a.App.S.SetMemberPrefs(r.Context(), c.Me.ID, c.Me.BiometricLock, c.Me.Theme); err != nil {
		return nil, err
	}
	return a.security(w, r, c)
}

func (a *API) logoutAll(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return map[string]bool{"ok": true}, a.App.S.DeleteSessions(r.Context(), c.Me.ID)
}

func (a *API) delSession(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	if err := a.App.S.DeleteSessionByID(r.Context(), c.Me.ID, r.PathValue("id")); err != nil {
		return nil, err
	}
	return a.security(w, r, c)
}

// ---------- dashboard data ----------

func (a *API) stats(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	return a.App.Stats(r.Context(), c)
}

func (a *API) calendar(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	ev, err := a.App.Calendar(r.Context(), c, r.URL.Query().Get("month"))
	if ev == nil {
		ev = []app.CalEvent{}
	}
	return map[string]any{"events": ev, "today": a.App.Now().In(c.Loc()).Format("2006-01-02")}, err
}

func (a *API) choreDone(w http.ResponseWriter, r *http.Request, c app.Couple) (any, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	i, err := a.App.ChoreDone(r.Context(), c, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return map[string]any{"idea": viewIdea(c, i)}, err
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
