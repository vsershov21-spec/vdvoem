package app

import (
	"context"
	"errors"
	"strings"

	"vdvoem/internal/store"
)

var ErrEmpty = errors.New("empty")

// Remember keeps a preference a person told the assistant. It is theirs: only they see and delete it.
func (a *App) Remember(ctx context.Context, c Couple, text string) (store.Memory, error) {
	text = clip(strings.TrimSpace(text), 200)
	if text == "" {
		return store.Memory{}, ErrEmpty
	}
	ms, _ := a.S.Memories(ctx, c.Space.ID)
	for _, m := range ms {
		if m.MemberID == c.Me.ID && strings.EqualFold(m.Text, text) {
			return m, nil
		}
	}
	return a.S.AddMemory(ctx, c.Space.ID, c.Me.ID, text, a.now())
}

// AddGoal: something the scout watches for, also months ahead («театр в январе»). Shared by the couple.
func (a *App) AddGoal(ctx context.Context, c Couple, text string) (store.Goal, error) {
	text = clip(strings.TrimSpace(text), 160)
	if text == "" {
		return store.Goal{}, ErrEmpty
	}
	g, err := a.S.AddGoal(ctx, c.Space.ID, c.Me.ID, text, a.now())
	if err == nil && c.Partner != nil {
		a.send(ctx, c.Partner.ChatID, "🎯 "+esc(c.Me.Name)+" попросил скаута следить: <b>"+esc(text)+"</b>", nil)
	}
	return g, err
}
