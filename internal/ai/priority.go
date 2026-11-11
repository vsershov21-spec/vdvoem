package ai

import (
	"context"
	"sync"
	"time"
)

// One local model on a CPU serves everything: a person waiting for the assistant must not queue behind background
// work (parsing a new idea, embeddings for search, a scout batch). A request marked Foreground cancels the
// background requests in flight (Ollama stops generating when the client goes) and holds new ones until it is done
// and a moment after; the background jobs simply retry later.

type fgKey struct{}

// Foreground marks a request someone is waiting for.
func Foreground(ctx context.Context) context.Context { return context.WithValue(ctx, fgKey{}, true) }

func isForeground(ctx context.Context) bool { v, _ := ctx.Value(fgKey{}).(bool); return v }

var pri = struct {
	sync.Mutex
	fg     int
	lastFg time.Time
	bg     map[int]context.CancelFunc
	next   int
}{bg: map[int]context.CancelFunc{}}

// quiet: how long background work keeps waiting after the last foreground request (the person may ask again)
const quiet = 3 * time.Second

// gate is called at the start of every model request; the returned context is the one to use, release at the end.
func gate(ctx context.Context) (context.Context, func()) {
	if isForeground(ctx) {
		pri.Lock()
		pri.fg++
		for _, cancel := range pri.bg {
			cancel()
		}
		pri.bg = map[int]context.CancelFunc{}
		pri.Unlock()
		return ctx, func() {
			pri.Lock()
			pri.fg--
			pri.lastFg = time.Now()
			pri.Unlock()
		}
	}
	for {
		pri.Lock()
		busy := pri.fg > 0 || time.Since(pri.lastFg) < quiet
		if !busy {
			c, cancel := context.WithCancel(ctx)
			id := pri.next
			pri.next++
			pri.bg[id] = cancel
			pri.Unlock()
			return c, func() {
				pri.Lock()
				delete(pri.bg, id)
				pri.Unlock()
				cancel()
			}
		}
		pri.Unlock()
		select {
		case <-ctx.Done():
			return ctx, func() {}
		case <-time.After(150 * time.Millisecond):
		}
	}
}
