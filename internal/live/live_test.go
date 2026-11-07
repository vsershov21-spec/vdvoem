package live

import (
	"testing"
	"time"
)

func next(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func TestHub(t *testing.T) {
	h := New()
	h.Delay = 20 * time.Millisecond
	a, cancelA := h.Subscribe(1, 10)
	if e := next(t, a); e.Kind != "presence" || len(e.On) != 1 {
		t.Fatalf("%+v", e)
	}
	b, cancelB := h.Subscribe(1, 11)
	next(t, a) // presence: both
	if e := next(t, b); len(e.On) != 2 {
		t.Fatalf("both online: %+v", e)
	}
	other, _ := h.Subscribe(2, 20)
	next(t, other)
	// a burst collapses into one sync, only for space 1
	h.Changed(1, 10, "tab1")
	h.Changed(1, 10, "tab1")
	h.Changed(1, 10, "tab1")
	if e := next(t, b); e.Kind != "sync" || e.CID != "tab1" || e.By != 10 {
		t.Fatalf("%+v", e)
	}
	next(t, a)
	select {
	case e := <-b:
		t.Fatalf("burst must be one event: %+v", e)
	case e := <-other:
		t.Fatalf("other space must not hear it: %+v", e)
	case <-time.After(60 * time.Millisecond):
	}
	// mixed sources: the echo filter is dropped
	h.Changed(1, 10, "tab1")
	h.Changed(1, 11, "tab2")
	if e := next(t, a); e.CID != "" || e.By != 0 {
		t.Fatalf("%+v", e)
	}
	next(t, b)
	cancelB()
	if e := next(t, a); len(e.On) != 1 || e.On[0] != 10 {
		t.Fatalf("Настя left: %+v", e)
	}
	cancelA()
	cancelA() // twice is fine
	if len(h.Online(1)) != 0 {
		t.Fatal("nobody online")
	}
	h.Close()
	if _, ok := <-other; ok {
		t.Fatal("closed on shutdown")
	}
}
