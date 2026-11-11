package ai

import (
	"context"
	"testing"
	"time"
)

// a background request in flight is cancelled when someone starts waiting; new background work waits its turn
func TestForegroundFirst(t *testing.T) {
	bgCtx, bgDone := gate(context.Background())
	fgCtx, fgDone := gate(Foreground(context.Background()))
	if bgCtx.Err() == nil {
		t.Fatal("background request not cancelled")
	}
	if fgCtx.Err() != nil {
		t.Fatal("foreground cancelled")
	}
	bgDone()
	started := make(chan time.Time, 1)
	go func() { _, done := gate(context.Background()); started <- time.Now(); done() }()
	time.Sleep(300 * time.Millisecond)
	select {
	case <-started:
		t.Fatal("background ran while someone waited")
	default:
	}
	t0 := time.Now()
	fgDone()
	at := <-started
	if at.Sub(t0) < quiet-200*time.Millisecond {
		t.Fatalf("background did not keep the quiet pause: %v", at.Sub(t0))
	}
	// a cancelled caller does not wait forever
	ctx, cancel := context.WithCancel(context.Background())
	_, d := gate(Foreground(context.Background()))
	cancel()
	if c, _ := gate(ctx); c.Err() == nil {
		t.Fatal("waited despite a cancelled context")
	}
	d()
}
