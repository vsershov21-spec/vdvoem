package domain

import (
	"math/rand"
	"sort"
	"time"
)

// SuggestMode — how the bot picks a proactive suggestion.
type SuggestMode string

const (
	SuggestBest   SuggestMode = "best"   // highest score
	SuggestRandom SuggestMode = "random" // «Удиви нас»: random, weighted so better ideas come up more often
	SuggestMix    SuggestMode = "mix"    // alternates: odd weeks best, even weeks random
	SuggestOff    SuggestMode = "off"
)

func (m SuggestMode) Valid() bool {
	switch m {
	case SuggestBest, SuggestRandom, SuggestMix, SuggestOff:
		return true
	}
	return false
}

// Resolve turns mix into a concrete mode for this moment.
func (m SuggestMode) Resolve(now time.Time) SuggestMode {
	if m != SuggestMix {
		return m
	}
	if _, w := now.ISOWeek(); w%2 == 0 {
		return SuggestRandom
	}
	return SuggestBest
}

// Candidates: eligible leisure ideas scored; with no windows the time filter is the coming weekend (any length fits).
func Candidates(in PickInput) []Scored {
	if len(in.Windows) == 0 {
		in.Windows = []Window{{From: in.Now, To: in.Now.Add(7 * 24 * time.Hour)}}
	}
	var out []Scored
	for _, i := range in.Ideas {
		w, ok := Eligible(i, in)
		if !ok {
			continue
		}
		s, sig := Score(i, in.Now)
		if in.BudgetKop > 0 && i.PriceMin > 0 {
			sig = append(sig, "fits_budget")
		}
		out = append(out, Scored{Idea: i, Score: s, Signals: sig, Window: w})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		if !out[a].Idea.CreatedAt.Equal(out[b].Idea.CreatedAt) {
			return out[a].Idea.CreatedAt.Before(out[b].Idea.CreatedAt)
		}
		return out[a].Idea.ID < out[b].Idea.ID
	})
	return out
}

// PickOne chooses a single idea. Random is weighted: weight = max(1, score+3), so a «очень хотим»
// idea comes up several times more often than one nobody marked, but nothing eligible is impossible.
func PickOne(in PickInput, mode SuggestMode, rng *rand.Rand) (Scored, bool) {
	c := Candidates(in)
	if len(c) == 0 {
		return Scored{}, false
	}
	if mode.Resolve(in.Now) != SuggestRandom || rng == nil {
		return c[0], true
	}
	total := 0
	for _, s := range c {
		total += weight(s.Score)
	}
	n := rng.Intn(total)
	for _, s := range c {
		n -= weight(s.Score)
		if n < 0 {
			s.Signals = append(s.Signals, "random")
			return s, true
		}
	}
	return c[len(c)-1], true
}

func weight(score int) int {
	if w := score + 3; w > 1 {
		return w
	}
	return 1
}
