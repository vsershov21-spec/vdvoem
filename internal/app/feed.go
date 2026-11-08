package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// A permanent calendar for each person: one subscription link (webcal://…/cal/<token>.ics). Every planned
// meeting is there; a move or a cancel in the app reaches the phone's calendar on its next refresh.
// The token is the only key — it can be reset, which kills the old link.

var ErrNoFeed = errors.New("no_feed")

// FeedToken: this person's link token, created on first use.
func (a *App) FeedToken(ctx context.Context, c Couple, reset bool) (string, error) {
	key := fmt.Sprintf("cal:%d", c.Me.ID)
	t := a.S.Setting(ctx, key)
	if t != "" && !reset {
		return t, nil
	}
	if t != "" {
		_ = a.S.SetSetting(ctx, "caltoken:"+t, "")
	}
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t = hex.EncodeToString(b)
	if err := a.S.SetSetting(ctx, key, t); err != nil {
		return "", err
	}
	return t, a.S.SetSetting(ctx, "caltoken:"+t, strconv.FormatInt(c.Me.ID, 10))
}

// Subscribed: the phone has read the feed at least once — then the bot stops sending .ics files (no duplicates).
func (a *App) Subscribed(ctx context.Context, memberID int64) bool {
	return a.S.Setting(ctx, fmt.Sprintf("calseen:%d", memberID)) != ""
}

// Feed renders the calendar behind a token.
func (a *App) Feed(ctx context.Context, token string) ([]byte, error) {
	if len(token) != 36 {
		return nil, ErrNoFeed
	}
	id, _ := strconv.ParseInt(a.S.Setting(ctx, "caltoken:"+token), 10, 64)
	if id == 0 {
		return nil, ErrNoFeed
	}
	m, err := a.S.MemberByID(ctx, id)
	if err != nil {
		return nil, ErrNoFeed
	}
	c, err := a.coupleOf(ctx, m)
	if err != nil {
		return nil, err
	}
	_ = a.S.SetSetting(ctx, fmt.Sprintf("calseen:%d", m.ID), a.now().Format(time.RFC3339))
	rs, err := a.S.RecentRounds(ctx, c.Space.ID, 300)
	if err != nil {
		return nil, err
	}
	var evs []domain.ICS
	for _, r := range rs {
		if r.Chosen == 0 || r.StartsAt.IsZero() {
			continue
		}
		cancelled := r.State == domain.StateCancelled
		if !cancelled && r.State != domain.StatePlanned && r.State != domain.StateDone {
			continue
		}
		if r.StartsAt.Before(a.now().AddDate(0, -3, 0)) { // the last three months and everything ahead
			continue
		}
		evs = append(evs, a.icsEvent(ctx, c, r, r.Version, cancelled))
	}
	name := "Вдвоём"
	if c.Partner != nil {
		name = "Вдвоём · " + c.Me.Name + " и " + c.Partner.Name
	}
	return []byte(domain.Feed(name, evs)), nil
}

// One meeting as a link that opens the phone's calendar with the event filled in — an .ics served over https
// (iPhone: Safari shows «Добавить в календарь»; a file sent in Telegram only offers «поделиться»), and Google's
// own «новое событие» form. The person's feed token is the key, so the link works without logging in.

// tokenFor: a member's feed token (created on first use)
func (a *App) tokenFor(ctx context.Context, memberID int64) string {
	m, err := a.S.MemberByID(ctx, memberID)
	if err != nil {
		return ""
	}
	c, err := a.coupleOf(ctx, m)
	if err != nil {
		return ""
	}
	t, _ := a.FeedToken(ctx, c, false)
	return t
}

// EventLinks: «добавить в календарь» for one person: the .ics link and the Google Calendar form
func (a *App) EventLinks(ctx context.Context, c Couple, memberID int64, r domain.Round) (ics, google string) {
	if r.StartsAt.IsZero() {
		return "", ""
	}
	if t := a.tokenFor(ctx, memberID); t != "" {
		ics = fmt.Sprintf("%s/cal/%s/%d.ics", strings.TrimRight(a.PublicURL, "/"), t, r.ID)
	}
	e := a.icsEvent(ctx, c, r, r.Version, false)
	q := url.Values{}
	q.Set("action", "TEMPLATE")
	q.Set("text", e.Title)
	q.Set("dates", e.Start.UTC().Format("20060102T150405Z")+"/"+e.End.UTC().Format("20060102T150405Z"))
	q.Set("details", e.Desc)
	if e.Location != "" {
		q.Set("location", e.Location)
	}
	return ics, "https://calendar.google.com/calendar/render?" + q.Encode()
}

// EventICS: one meeting of the token's owner, for the «добавить в календарь» link
func (a *App) EventICS(ctx context.Context, token string, roundID int64) ([]byte, error) {
	if len(token) != 36 {
		return nil, ErrNoFeed
	}
	id, _ := strconv.ParseInt(a.S.Setting(ctx, "caltoken:"+token), 10, 64)
	if id == 0 {
		return nil, ErrNoFeed
	}
	m, err := a.S.MemberByID(ctx, id)
	if err != nil {
		return nil, ErrNoFeed
	}
	c, err := a.coupleOf(ctx, m)
	if err != nil {
		return nil, err
	}
	r, err := a.S.Round(ctx, c.Space.ID, roundID)
	if err != nil || r.StartsAt.IsZero() {
		return nil, ErrNoFeed
	}
	return a.ICS(ctx, c, r, r.Version, r.State == domain.StateCancelled), nil
}

// calKeys: the two «в календарь» buttons for a member (nothing when their calendar is subscribed)
func (a *App) calKeys(ctx context.Context, c Couple, m store.Member, r domain.Round) []tg.Button {
	if a.Subscribed(ctx, m.ID) {
		return nil
	}
	ics, g := a.EventLinks(ctx, c, m.ID, r)
	var row []tg.Button
	if ics != "" {
		row = append(row, tg.Link("📅 В календарь iPhone", ics))
	}
	if g != "" {
		row = append(row, tg.Link("Google Календарь", g))
	}
	return row
}
