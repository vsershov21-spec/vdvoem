package assistant

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Recall: before the model reads a request, it sees the few most similar requests that were understood right —
// the gold cases and the couple's own requests whose answer was used (a button pressed). The model learns
// from use without retraining; the fixed prefix stays cached, the recalled pairs go after it.

type bankCache struct {
	mu   sync.Mutex
	at   time.Time
	list []intentCase
}

func stems(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		r := []rune(w)
		if len(r) < 3 {
			continue
		}
		if len(r) > 5 {
			r = r[:5]
		}
		out[string(r)] = true
	}
	return out
}

func similarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return float64(n) / float64(len(a)+len(b)-n)
}

// bank: gold cases + accepted real requests (refreshed every 10 minutes)
func (as *Assistant) bank(ctx context.Context) []intentCase {
	as.cache.mu.Lock()
	defer as.cache.mu.Unlock()
	if as.cache.list != nil && time.Since(as.cache.at) < 10*time.Minute {
		return as.cache.list
	}
	list := append([]intentCase{}, intentCases...)
	if as.App != nil && as.App.S != nil {
		if ex, err := as.App.S.Examples(ctx, true); err == nil {
			for _, e := range ex {
				if strings.HasPrefix(e.Calls, "intent:") {
					list = append(list, intentCase{Q: e.Input, Want: strings.TrimPrefix(e.Calls, "intent:")})
				}
			}
		}
	}
	as.cache.list, as.cache.at = list, time.Now()
	return list
}

// recall: up to n most similar understood requests (an identical request is skipped, so the eval stays honest)
func recall(bank []intentCase, text string, n int) []intentCase {
	q := stems(text)
	type sc struct {
		c intentCase
		s float64
	}
	var all []sc
	lt := strings.ToLower(strings.TrimSpace(text))
	for _, c := range bank {
		if strings.ToLower(strings.TrimSpace(c.Q)) == lt {
			continue
		}
		if s := similarity(q, stems(c.Q)); s >= 0.2 {
			all = append(all, sc{c, s})
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].s > all[b].s })
	var out []intentCase
	for _, x := range all {
		if len(out) == n {
			break
		}
		out = append(out, x.c)
	}
	return out
}
