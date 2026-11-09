package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"vdvoem/internal/domain"
)

// Each person can connect their own calendar by its private iCal link (Google: «Секретный адрес в формате
// iCal»; iPhone/iCloud: a shared calendar link; Yandex: «Экспорт → ссылка»). The app reads it every 10 minutes
// and knows which parts of which days are taken — for both: when choosing time, busy cells are greyed out,
// «мне подходит любое» skips them, and the partner sees «занят(а)» without any details.

var ErrBadCalendar = errors.New("bad_calendar")

type busyCache struct {
	mu   sync.Mutex
	data map[int64]busyEntry
}

type busyEntry struct {
	at    time.Time
	spans []domain.Span
}

var busyMem = busyCache{data: map[int64]busyEntry{}}

func normCalURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "webcal://") {
		raw = "https://" + strings.TrimPrefix(raw, "webcal://")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", ErrBadCalendar
	}
	return u.String(), nil
}

func (a *App) fetchSpans(ctx context.Context, link string, loc *time.Location) ([]domain.Span, int, error) {
	if a.Fetch == nil {
		return nil, 0, ErrBadCalendar
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", link, nil)
	req.Header.Set("Accept", "text/calendar")
	res, err := a.Fetch.Do(req)
	if err != nil {
		return nil, 0, ErrBadCalendar
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != 200 || !strings.Contains(string(b), "BEGIN:VCALENDAR") {
		return nil, 0, ErrBadCalendar
	}
	now := a.now()
	spans := domain.BusySpans(string(b), now.AddDate(0, 0, -1), now.AddDate(0, 0, 62), loc)
	return spans, strings.Count(string(b), "BEGIN:VEVENT"), nil
}

// ConnectBusy: my calendar for busy times; returns how many busy stretches it has in the next weeks
func (a *App) ConnectBusy(ctx context.Context, c Couple, raw string) (int, error) {
	link, err := normCalURL(raw)
	if err != nil {
		return 0, err
	}
	spans, _, err := a.fetchSpans(ctx, link, c.Loc())
	if err != nil {
		return 0, err
	}
	if err := a.S.SetSetting(ctx, fmt.Sprintf("busyurl:%d", c.Me.ID), link); err != nil {
		return 0, err
	}
	busyMem.mu.Lock()
	busyMem.data[c.Me.ID] = busyEntry{a.now(), spans}
	busyMem.mu.Unlock()
	a.Live.From(ctx, c.Space.ID)
	return len(spans), nil
}

func (a *App) DisconnectBusy(ctx context.Context, c Couple) error {
	busyMem.mu.Lock()
	delete(busyMem.data, c.Me.ID)
	busyMem.mu.Unlock()
	return a.S.SetSetting(ctx, fmt.Sprintf("busyurl:%d", c.Me.ID), "")
}

// BusyConnected: whether a person's calendar is connected (the link itself never leaves the server)
func (a *App) BusyConnected(ctx context.Context, memberID int64) bool {
	return a.S.Setting(ctx, fmt.Sprintf("busyurl:%d", memberID)) != ""
}

// BusySlots: the parts of days a person is busy (YYYY-MM-DD:part) from today for `days` days; nil — not connected
func (a *App) BusySlots(ctx context.Context, memberID int64, loc *time.Location, days int) map[string]bool {
	link := a.S.Setting(ctx, fmt.Sprintf("busyurl:%d", memberID))
	if link == "" {
		return nil
	}
	busyMem.mu.Lock()
	e, ok := busyMem.data[memberID]
	busyMem.mu.Unlock()
	if !ok || a.now().Sub(e.at) > 10*time.Minute {
		if spans, _, err := a.fetchSpans(ctx, link, loc); err == nil {
			e = busyEntry{a.now(), spans}
			busyMem.mu.Lock()
			busyMem.data[memberID] = e
			busyMem.mu.Unlock()
		} else if !ok {
			return nil
		}
	}
	return domain.BusySlots(e.spans, a.now().In(loc), days, loc)
}
