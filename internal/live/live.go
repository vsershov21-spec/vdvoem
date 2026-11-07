// Package live pushes «something changed» to the couple's open Mini Apps (server-sent events)
// and tracks who of the two is in the app right now.
package live

import (
	"context"
	"sync"
	"time"
)

type Event struct {
	Kind string // "sync" | "presence"
	By   int64  // member who caused it (0 = the server: a job, a reminder)
	CID  string // client id of the tab that caused it — that tab skips the echo
	On   []int64
}

type sub struct {
	space, member int64
	ch            chan Event
}

type Hub struct {
	mu      sync.Mutex
	subs    map[int64]map[*sub]bool
	pending map[int64]*Event // coalesced per space
	closed  bool
	Delay   time.Duration // coalescing window
}

func New() *Hub {
	return &Hub{subs: map[int64]map[*sub]bool{}, pending: map[int64]*Event{}, Delay: 150 * time.Millisecond}
}

// Subscribe registers an open app; cancel when the connection ends.
func (h *Hub) Subscribe(space, member int64) (<-chan Event, func()) {
	if h == nil {
		ch := make(chan Event)
		return ch, func() {}
	}
	s := &sub{space: space, member: member, ch: make(chan Event, 8)}
	h.mu.Lock()
	if h.subs[space] == nil {
		h.subs[space] = map[*sub]bool{}
	}
	h.subs[space][s] = true
	h.mu.Unlock()
	h.presence(space)
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[space], s)
			if len(h.subs[space]) == 0 {
				delete(h.subs, space)
			}
			h.mu.Unlock()
			h.presence(space)
		})
	}
}

// Online lists members with the app open.
func (h *Hub) Online(space int64) []int64 {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.online(space)
}

func (h *Hub) online(space int64) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for s := range h.subs[space] {
		if !seen[s.member] {
			seen[s.member] = true
			out = append(out, s.member)
		}
	}
	return out
}

func (h *Hub) presence(space int64) {
	h.mu.Lock()
	ev := Event{Kind: "presence", On: h.online(space)}
	h.deliver(space, ev)
	h.mu.Unlock()
}

// Changed tells the space's open apps to refresh. Bursts within Delay collapse into one event.
func (h *Hub) Changed(space, by int64, cid string) {
	if h == nil || space == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subs[space]) == 0 {
		return
	}
	if p := h.pending[space]; p != nil {
		if p.By != by || p.CID != cid { // mixed sources: everyone refreshes
			p.By, p.CID = 0, ""
		}
		return
	}
	h.pending[space] = &Event{Kind: "sync", By: by, CID: cid}
	time.AfterFunc(h.Delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if p := h.pending[space]; p != nil {
			delete(h.pending, space)
			h.deliver(space, *p)
		}
	})
}

// ChangedAll refreshes every space with an open app (background jobs that don't know their space).
func (h *Hub) ChangedAll() {
	if h == nil {
		return
	}
	h.mu.Lock()
	spaces := make([]int64, 0, len(h.subs))
	for sp := range h.subs {
		spaces = append(spaces, sp)
	}
	h.mu.Unlock()
	for _, sp := range spaces {
		h.Changed(sp, 0, "")
	}
}

func (h *Hub) deliver(space int64, ev Event) {
	for s := range h.subs[space] {
		select {
		case s.ch <- ev:
		default: // a stuck client misses one event; its next sync or reconnect refreshes everything
		}
	}
}

// Close ends every stream (server shutdown).
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sp, m := range h.subs {
		for s := range m {
			close(s.ch)
		}
		delete(h.subs, sp)
	}
}

type srcKey struct{}
type source struct {
	by  int64
	cid string
}

// WithSource marks who is changing things in this request, so the change event carries it.
func WithSource(ctx context.Context, by int64, cid string) context.Context {
	return context.WithValue(ctx, srcKey{}, source{by, cid})
}

// From publishes a change for the space on behalf of whoever the context says.
func (h *Hub) From(ctx context.Context, space int64) {
	s, _ := ctx.Value(srcKey{}).(source)
	h.Changed(space, s.by, s.cid)
}
