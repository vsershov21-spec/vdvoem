package app

import (
	"context"
	"fmt"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/parse"
	"vdvoem/internal/store"
)

// Pictures and details for what the scout and channels bring: the event's own page gives its picture
// (og:image), a description and sometimes a price — so a card is not a grey gradient with a title.

type picJob struct {
	Idea  int64 `json:"idea"`
	Space int64 `json:"space"`
}

// wantPicture: queue a look at the event's page
func (a *App) wantPicture(ctx context.Context, i store.Idea, after ...time.Duration) {
	if i.URL == "" || i.ImageURL != "" {
		return
	}
	at := a.now()
	if len(after) > 0 {
		at = at.Add(after[0]) // spread out: one page fetch must not hold up reminders
	}
	_ = a.S.Enqueue(ctx, "picture", at, picJob{Idea: i.ID, Space: i.SpaceID}, fmt.Sprintf("picture:%d", i.ID))
}

func (a *App) runPicture(ctx context.Context, p picJob) error {
	i, err := a.S.Idea(ctx, p.Space, p.Idea)
	if err != nil || i.URL == "" || i.ImageURL != "" || a.Fetch == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	r, err := parse.Fetch(ctx, a.Fetch, i.URL)
	if err != nil {
		return nil // the page is gone or slow: the card keeps its gradient
	}
	changed := false
	if r.Image != "" {
		i.ImageURL, changed = safeURL(r.Image), true
	}
	if i.Info.About == "" && len([]rune(r.Description)) > 40 {
		i.Info.About, changed = sentences(r.Description, 400), true
		if i.Info.Src == "" {
			i.Info.Src = "page"
		}
	}
	if i.PriceMin == 0 && r.PriceRub > 0 {
		i.PriceMin, i.PriceMax, changed = r.PriceRub*200, r.PriceRub*200, true // a ticket → for two
	}
	if len(i.Info.Genres) == 0 {
		if g := GenresOf(i.Title + " " + r.Description); len(g) > 0 {
			i.Info.Genres, changed = g, true
		}
	}
	if changed {
		_ = a.S.UpdateIdea(ctx, i, a.now())
		a.Live.From(ctx, i.SpaceID)
	}
	return nil
}

// FillPictures: ideas already there get their pictures too (at start, in the background, a few at a time)
func (a *App) FillPictures(ctx context.Context) int {
	spaces, err := a.S.Spaces(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, sp := range spaces {
		ideas, _ := a.S.Ideas(ctx, sp.ID, domain.IdeaActive, domain.IdeaDraft)
		for _, i := range ideas {
			if i.URL != "" && i.ImageURL == "" && n < 400 {
				a.wantPicture(ctx, i, time.Duration(n)*20*time.Second)
				n++
			}
		}
	}
	return n
}
