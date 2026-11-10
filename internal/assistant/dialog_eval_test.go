package assistant

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// without a model (the rules alone) most refinements must already keep the request
func TestDialogsRulesOnly(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 11, 10, 20, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	share := EvalDialogs(context.Background(), &Assistant{}, &buf, now)
	t.Log(buf.String())
	if share < 0.9 {
		t.Fatalf("rules keep only %.0f%% of the dialogues", share*100)
	}
}
