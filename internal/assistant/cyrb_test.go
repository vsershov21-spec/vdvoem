package assistant

import "testing"

// Go's \b only knows ASCII letters: after a Cyrillic letter it never matches, so «обо мне» and «до 5к» need an explicit edge
func TestCyrillicEdges(t *testing.T) {
	for _, s := range []string{"обо мне", "Обо мне", "расскажи обо мне", "обо мне?"} {
		if !tasteRe.MatchString(s) {
			t.Errorf("taste: %q", s)
		}
	}
	if tasteRe.MatchString("обо мнений") {
		t.Error("taste matched «обо мнений»")
	}
	for _, s := range []string{"до 5к", "за 3к на двоих"} {
		if m := kRe.FindStringSubmatch(s); m == nil {
			t.Errorf("budget: %q", s)
		}
	}
}
