package app

import (
	"context"
	"strings"

	"vdvoem/internal/store"
)

// About: everything the app knows about one event and whether it suits each of the two —
// percentages from their own taste models, the reasons behind them, and what they said themselves.
type About struct {
	Idea           store.Idea
	Match          Match
	MeMarks        int
	PartnerMarks   int
	MePros, MeCons []string
	PaPros, PaCons []string
	Said           []string // memories of either person that touch this event
}

func (a *App) About(ctx context.Context, c Couple, i store.Idea) About {
	t := a.Tastes(ctx, c)
	loc := c.Loc()
	it := itemOf(i)
	ab := About{Idea: i, Match: t.Of(i), MeMarks: t.Me.N, PartnerMarks: t.Partner.N}
	ab.MePros, ab.MeCons = t.Me.Reasons(it, loc, 3)
	if c.Partner != nil {
		ab.PaPros, ab.PaCons = t.Partner.Reasons(it, loc, 3)
	}
	keys := nameWords(i.Title + " " + i.Address + " " + strings.Join(i.Info.Genres, " ") + " " + strings.Join(TagsOf(i), " "))
	mem, _ := a.S.Memories(ctx, c.Space.ID)
	for _, m := range mem {
		for _, w := range nameWords(m.Text) {
			hit := false
			for _, k := range keys {
				if len([]rune(w)) >= 4 && (strings.HasPrefix(k, w) || strings.HasPrefix(w, k)) {
					hit = true
				}
			}
			if hit {
				who := c.Me.Name
				if c.Partner != nil && m.MemberID == c.Partner.ID {
					who = c.Partner.Name
				}
				ab.Said = append(ab.Said, who+": «"+clip(m.Text, 80)+"»")
				break
			}
		}
	}
	return ab
}
