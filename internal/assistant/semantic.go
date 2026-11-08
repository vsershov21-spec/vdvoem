package assistant

import (
	"context"
	"strings"
	"sync"
	"time"

	"vdvoem/internal/ai"
	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

// Search by meaning. Words find «джаз» when it says «джаз»; a request like «что-нибудь душевное» or «как в
// прошлый раз, только на воздухе» needs meaning. Each idea gets a vector (title, genres, a line of description)
// once; a request is one vector more (~0.1–0.3 s on CPU). The vectors only rank and widen a search — dates,
// prices and the person's exclusions are still checked by the code. Off without EMBED_MODEL.

// semMin: below this a «match by meaning» is noise (bge-m3 on short Russian texts)
const semMin = 0.52

type semIndex struct {
	mu       sync.Mutex
	vecs     map[int64][]float32
	keys     map[int64]string
	indexing map[int64]bool // spaces being indexed in the background
}

func ideaText(i store.Idea) string {
	parts := []string{i.Title, catNames[i.Category]}
	if len(i.Info.Genres) > 0 {
		parts = append(parts, strings.Join(i.Info.Genres, ", "))
	}
	if a := []rune(i.Info.About); len(a) > 0 {
		parts = append(parts, string(a[:min(len(a), 200)]))
	} else if i.Why != "" {
		parts = append(parts, i.Why)
	}
	return strings.Join(parts, ". ")
}

// Semantic: similarity of the request to every indexed idea of the space (nil — off or not ready yet)
func (as *Assistant) Semantic(ctx context.Context, spaceID int64, text string) map[int64]float64 {
	if as.EmbedModel == "" || !as.AI.Enabled() || strings.TrimSpace(text) == "" {
		return nil
	}
	ideas, err := as.App.S.Ideas(ctx, spaceID, domain.IdeaDraft, domain.IdeaActive)
	if err != nil {
		return nil
	}
	s := &as.sem
	s.mu.Lock()
	if s.vecs == nil {
		s.vecs, s.keys, s.indexing = map[int64][]float32{}, map[int64]string{}, map[int64]bool{}
	}
	var missing []store.Idea
	for _, i := range ideas {
		if i.Kind == domain.KindLeisure && s.keys[i.ID] != ideaText(i) {
			missing = append(missing, i)
		}
	}
	busy := s.indexing[spaceID]
	if len(missing) > 0 && !busy {
		s.indexing[spaceID] = true
	}
	s.mu.Unlock()
	if len(missing) > 0 && !busy {
		go as.index(spaceID, missing)
	}
	qctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	qv, err := as.AI.Embed(qctx, as.EmbedModel, []string{text})
	if err != nil || len(qv) == 0 {
		return nil
	}
	out := map[int64]float64{}
	s.mu.Lock()
	for _, i := range ideas {
		if v, ok := s.vecs[i.ID]; ok {
			out[i.ID] = ai.Cos(qv[0], v)
		}
	}
	s.mu.Unlock()
	return out
}

// index embeds ideas in small batches in the background (never in the way of an answer)
func (as *Assistant) index(spaceID int64, ideas []store.Idea) {
	s := &as.sem
	defer func() { s.mu.Lock(); delete(s.indexing, spaceID); s.mu.Unlock() }()
	for k := 0; k < len(ideas); k += 16 {
		batch := ideas[k:min(k+16, len(ideas))]
		texts := make([]string, len(batch))
		for j, i := range batch {
			texts[j] = ideaText(i)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		vs, err := as.AI.Embed(ctx, as.EmbedModel, texts)
		cancel()
		if err != nil {
			if as.App != nil && as.App.Log != nil {
				as.App.Log.Warn("embed index", "err", err)
			}
			return
		}
		s.mu.Lock()
		for j, i := range batch {
			s.vecs[i.ID], s.keys[i.ID] = vs[j], texts[j]
		}
		s.mu.Unlock()
	}
}
