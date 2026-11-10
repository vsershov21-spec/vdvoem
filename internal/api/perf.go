package api

import (
	"net/http"
	"sort"
	"sync"
	"time"
)

// perf: how long the app really takes, as the phones see it — the server time of every API route and the
// boot stages each page reports (html → sdk → app → first data). Kept in memory, read from the server only
// (GET /agent/perf), so a slow open in Telegram has numbers behind it instead of guesses.
type perfLog struct {
	mu     sync.Mutex
	routes map[string][]time.Duration // the last 200 per route
	boots  []bootRec                  // the last 300 boot reports
}

type bootRec struct {
	At    time.Time `json:"at"`
	Stage string    `json:"stage"`
	Msg   string    `json:"msg"`
	Ms    int       `json:"ms"`
}

var perf = &perfLog{routes: map[string][]time.Duration{}}

func (p *perfLog) route(name string, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := append(p.routes[name], d)
	if len(l) > 200 {
		l = l[len(l)-200:]
	}
	p.routes[name] = l
}

func (p *perfLog) boot(b bootRec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.boots = append(p.boots, b)
	if len(p.boots) > 300 {
		p.boots = p.boots[len(p.boots)-300:]
	}
}

func (a *API) agentPerf(w http.ResponseWriter, r *http.Request) {
	perf.mu.Lock()
	type row struct {
		Route         string `json:"route"`
		N             int    `json:"n"`
		P50, P90, Max int64  // ms
	}
	var rows []row
	for k, l := range perf.routes {
		s := append([]time.Duration(nil), l...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		rows = append(rows, row{k, len(s), s[len(s)/2].Milliseconds(), s[len(s)*9/10].Milliseconds(), s[len(s)-1].Milliseconds()})
	}
	boots := append([]bootRec(nil), perf.boots...)
	perf.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].P90 > rows[j].P90 })
	writeJSON(w, 200, map[string]any{"routes": rows, "boots": boots})
}
