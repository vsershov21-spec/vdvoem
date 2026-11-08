// vdvoem — Telegram bot + Mini App for a couple to plan where to go and when.
//
// Config (env):
//
//	BOT_TOKEN       Telegram bot token (required)
//	PUBLIC_URL      https://vdvoem.vershov.com (required)
//	WEBHOOK_SECRET  random string, checked on every update (required)
//	LISTEN          127.0.0.1:8099
//	DB_PATH         ./data/vdvoem.db
//	WEB_DIR         ./web/dist
//	TG_API_BASE     https://api.telegram.org (emulator in tests)
//	AI_URL, AI_KEY, AI_MODEL   OpenAI-compatible endpoint (a local Ollama works); empty = rules only
//	AI_TIMEOUT      seconds for one model step (default 25, local 90)
//	TG_MODE         poll (default: the server fetches updates, no inbound connections) | webhook
//	SETUP=1         register commands, menu button, description (and the webhook in webhook mode) at start
//	DEV_TOKEN       e2e only: accept X-Dev-User when X-Dev-Token matches (never in production)
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"vdvoem/internal/ai"
	"vdvoem/internal/api"
	"vdvoem/internal/app"
	"vdvoem/internal/assistant"
	"vdvoem/internal/bot"
	"vdvoem/internal/live"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

var version = "dev"

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(os.Args) == 3 && os.Args[1] == "backup" {
		// online-safe copy of the database: vdvoem backup /path/to/file.db
		db, err := sql.Open("sqlite", "file:"+env("DB_PATH", "./data/vdvoem.db")+"?_pragma=busy_timeout(10000)")
		if err == nil {
			_, err = db.Exec(`VACUUM INTO ?`, os.Args[2])
		}
		if err != nil {
			log.Error("backup failed", "err", err)
			os.Exit(1)
		}
		log.Info("backup ok", "to", os.Args[2])
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "eval" {
		// how well the configured model picks tools: vdvoem eval [model …] (several models → a comparison)
		models := os.Args[2:]
		if len(models) == 0 {
			models = []string{env("AI_MODEL", "")}
		}
		for _, m := range models {
			c := &ai.Client{URL: env("AI_URL", ""), Key: env("AI_KEY", ""), Model: m, Timeout: aiTimeout()}
			if !c.Enabled() {
				log.Error("AI_URL and a model are required")
				os.Exit(2)
			}
			assistant.Eval(context.Background(), c, os.Stdout)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "export-train" {
		db, err := sql.Open("sqlite", "file:"+env("DB_PATH", "./data/vdvoem.db")+"?_pragma=busy_timeout(10000)&mode=ro")
		if err == nil {
			var f *os.File
			if f, err = os.Create(os.Args[2]); err == nil {
				var n int
				n, err = assistant.ExportTrain(context.Background(), &store.Store{DB: db}, f)
				f.Close()
				log.Info("training examples written", "n", n, "to", os.Args[2])
			}
		}
		if err != nil {
			log.Error("export failed", "err", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "version" {
		os.Stdout.WriteString(version + "\n")
		return
	}
	token, public, secret := env("BOT_TOKEN", ""), strings.TrimRight(env("PUBLIC_URL", ""), "/"), env("WEBHOOK_SECRET", "")
	if token == "" || public == "" || secret == "" {
		log.Error("BOT_TOKEN, PUBLIC_URL and WEBHOOK_SECRET are required")
		os.Exit(2)
	}
	dbPath := env("DB_PATH", "./data/vdvoem.db")
	_ = os.MkdirAll(dirOf(dbPath), 0o700)
	// pragmas in the DSN apply to every pooled connection; one connection keeps SQLite writes serialized
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	st, err := store.Open(db)
	if err != nil {
		log.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.DB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := tg.New(env("TG_API_BASE", ""), token)
	aiClient := &ai.Client{URL: env("AI_URL", ""), Key: env("AI_KEY", ""), Model: env("AI_MODEL", ""), Timeout: aiTimeout()}
	aiClient.Log = func(task, provider, in, out string, ok bool, ms int64) {
		st.LogAI(context.Background(), task, provider, in, out, ok, ms, time.Now())
	}
	botName := "vdvoem_bot"
	if me, err := client.Me(ctx); err == nil && me.Username != "" {
		botName = me.Username
	} else if err != nil {
		log.Warn("getMe failed", "err", err)
	}

	hub := live.New()
	a := &app.App{Live: hub, S: st, TG: client, AI: aiClient, Fetch: parse.Client(false), Now: time.Now, PublicURL: public, BotName: botName, Log: log}
	helper := &assistant.Assistant{App: a, AI: aiClient}
	b := &bot.Bot{App: a, Helper: helper, TG: client, Secret: secret, Log: log}
	ap := &api.API{App: a, Helper: helper, BotToken: token, WebDir: env("WEB_DIR", "./web/dist"), Log: log}
	if t := env("DEV_TOKEN", ""); t != "" {
		if len(t) < 24 {
			log.Error("DEV_TOKEN must be at least 24 characters")
			os.Exit(2)
		}
		ap.DevToken = t
		var off atomic.Int64
		a.Now = func() time.Time { return time.Now().Add(time.Duration(off.Load())) }
		ap.Advance = func(d time.Duration) { off.Add(int64(d)) }
		a.Fetch = parse.Client(true) // e2e serves link pages from localhost
		log.Warn("DEV_TOKEN set: e2e users accepted — never in production")
	}

	mux := http.NewServeMux()
	mux.Handle("POST /tg/webhook", b)
	ap.Routes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := st.DB.PingContext(r.Context()); err != nil {
			http.Error(w, "db", 503)
			return
		}
		late, _ := st.LateJobs(r.Context(), time.Now(), 10*time.Minute)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"late_jobs":` + itoa(late) + `,"ai":` + boolStr(aiClient.Enabled()) + `}`))
	})

	srv := &http.Server{Addr: env("LISTEN", "127.0.0.1:8099"), Handler: accessLog(log, secure(mux)), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second}

	mode := env("TG_MODE", "poll")
	if env("SETUP", "") == "1" {
		setup(ctx, client, public, secret, mode == "webhook", log)
	}
	if mode != "webhook" {
		go b.Poll(ctx)
	}

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		a.Tick(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				func() {
					defer func() {
						if p := recover(); p != nil {
							log.Error("panic in tick", "panic", p)
						}
					}()
					tc, cancel := context.WithTimeout(ctx, 25*time.Second)
					a.Tick(tc)
					cancel()
				}()
			}
		}
	}()

	srv.RegisterOnShutdown(hub.Close) // open live streams end, so shutdown does not wait for them
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()
	log.Info("listening", "version", version, "addr", srv.Addr, "public", public, "bot", botName, "ai", aiClient.Enabled())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

func setup(ctx context.Context, c *tg.Client, public, secret string, webhook bool, log *slog.Logger) {
	steps := []struct {
		name string
		f    func() error
	}{
		{"webhook", func() error {
			if !webhook {
				return nil // polling mode: Poll() removes any webhook
			}
			return c.SetWebhook(ctx, public+"/tg/webhook", secret)
		}},
		{"commands", func() error { return c.SetCommands(ctx, bot.Commands, bot.CommandOrder) }},
		{"menu", func() error { return c.SetMenuButton(ctx, "Вдвоём", public+"/app/") }},
		{"description", func() error {
			return c.SetDescription(ctx, "Решаем вдвоём, куда сходить и когда — без «не знаю, а ты?»",
				"Кидайте ссылки и идеи в общую копилку, отмечайте «хочу», а я найду общее свободное время, предложу три варианта и пришлю событие в календарь. Подтверждают оба — молчание не считается согласием.")
		}},
	}
	for _, s := range steps {
		if err := s.f(); err != nil {
			log.Warn("setup step failed", "step", s.name, "err", err)
		} else {
			log.Info("setup ok", "step", s.name)
		}
	}
}

// secure adds headers that fit a Telegram Mini App (it is framed by Telegram's web client).
// rec remembers the status for the access log; Unwrap keeps Flush working for the live stream.
type rec struct {
	http.ResponseWriter
	code int
}

func (r *rec) WriteHeader(c int)           { r.code = c; r.ResponseWriter.WriteHeader(c) }
func (r *rec) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// accessLog: one line per page/API request (no query strings, no bodies) — to see what a phone actually loads.
func accessLog(log *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/tg/webhook" {
			h.ServeHTTP(w, r)
			return
		}
		t0 := time.Now()
		rw := &rec{ResponseWriter: w, code: 200}
		h.ServeHTTP(rw, r)
		ua := r.UserAgent()
		if len(ua) > 90 {
			ua = ua[len(ua)-90:]
		}
		log.Info("http", "m", r.Method, "p", r.URL.Path, "s", rw.code, "ms", time.Since(t0).Milliseconds(), "ua", ua)
	})
}

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "frame-ancestors https://web.telegram.org https://*.telegram.org 'self'")
		h.ServeHTTP(w, r)
	})
}

// aiTimeout: AI_TIMEOUT seconds; a local model on the server's CPU needs up to a minute.
func aiTimeout() time.Duration {
	if n, err := strconv.Atoi(env("AI_TIMEOUT", "")); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if strings.Contains(env("AI_URL", ""), "127.0.0.1") || strings.Contains(env("AI_URL", ""), "localhost") {
		return 90 * time.Second
	}
	return 0
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
