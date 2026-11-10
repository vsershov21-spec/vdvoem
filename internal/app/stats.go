package app

import (
	"context"
	"sort"
	"time"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
)

// RoundDays: the three days a round offers for slots.
func RoundDays(r domain.Round, loc *time.Location) []time.Time {
	if len(r.Days) == 3 {
		var out []time.Time
		for _, s := range r.Days {
			if d, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
				out = append(out, d)
			}
		}
		if len(out) == 3 {
			return out
		}
	}
	return SlotDays(r.OpenedAt, loc)
}

// DaysFrom: the chosen day and the next two (a round started from the calendar).
func DaysFrom(day string, now time.Time, loc *time.Location) ([]string, bool) {
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return nil, false
	}
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	if d.Before(today) || d.After(today.AddDate(0, 2, 0)) {
		return nil, false
	}
	return []string{d.Format("2006-01-02"), d.AddDate(0, 0, 1).Format("2006-01-02"), d.AddDate(0, 0, 2).Format("2006-01-02")}, true
}

type MonthPoint struct {
	Month   string `json:"month"` // 2026-11
	Label   string `json:"label"` // ноя
	Done    int    `json:"done"`
	Planned int    `json:"planned"`
}

type Stats struct {
	Months       []MonthPoint   `json:"months"`
	Meetings     int            `json:"meetings"`
	Hours        float64        `json:"hours"`
	SpentKop     int64          `json:"spent"`
	Ideas        int            `json:"ideas"`
	BothWant     int            `json:"both_want"`
	BothMarked   int            `json:"both_marked"`
	Match        int            `json:"match"` // % of ideas both marked that both want
	Liked        int            `json:"liked"`
	Unmarked     int            `json:"unmarked"`
	ThisMonth    int            `json:"this_month"`
	WeekActivity []int          `json:"week"` // ideas added in each of the last 7 days
	Categories   map[string]int `json:"categories"`
}

var monShort = []string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

func want(w domain.Wish) bool { return w == domain.WishWant || w == domain.WishVery }

func (a *App) Stats(ctx context.Context, c Couple) (Stats, error) {
	loc := c.Loc()
	now := a.now().In(loc)
	st := Stats{Categories: map[string]int{}, WeekActivity: make([]int, 7)}
	idx := map[string]int{}
	for k := 5; k >= 0; k-- {
		m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -k, 0)
		key := m.Format("2006-01")
		idx[key] = len(st.Months)
		st.Months = append(st.Months, MonthPoint{Month: key, Label: monShort[m.Month()-1]})
	}
	rs, err := a.S.RecentRounds(ctx, c.Space.ID, 500)
	if err != nil {
		return st, err
	}
	for _, r := range rs {
		if r.StartsAt.IsZero() || (r.State != domain.StateDone && r.State != domain.StatePlanned) {
			continue
		}
		key := r.StartsAt.In(loc).Format("2006-01")
		i, ok := idx[key]
		if r.State == domain.StateDone {
			st.Meetings++
			st.Hours += r.EndsAt.Sub(r.StartsAt).Hours()
			if i, err := a.S.Idea(ctx, c.Space.ID, r.Chosen); err == nil {
				st.SpentKop += i.PriceMin
			}
			if key == now.Format("2006-01") {
				st.ThisMonth++
			}
			if ok {
				st.Months[i].Done++
			}
		} else if ok {
			st.Months[i].Planned++
		}
	}
	ideas, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	for _, i := range ideas {
		if d := int(day0.Sub(time.Date(i.CreatedAt.In(loc).Year(), i.CreatedAt.In(loc).Month(), i.CreatedAt.In(loc).Day(), 0, 0, 0, 0, loc)).Hours() / 24); d >= 0 && d < 7 {
			st.WeekActivity[6-d]++
		}
		if i.Kind != domain.KindLeisure {
			continue
		}
		st.Ideas++
		st.Categories[i.Category]++
		if i.AgainYes > 0 {
			st.Liked++
		}
		me := i.WishA
		if c.Me.Color == "b" {
			me = i.WishB
		}
		if me == domain.WishNone {
			st.Unmarked++
		}
		if i.WishA != domain.WishNone && i.WishB != domain.WishNone {
			st.BothMarked++
			if want(i.WishA) && want(i.WishB) {
				st.BothWant++
			}
		}
	}
	if st.BothMarked > 0 {
		st.Match = st.BothWant * 100 / st.BothMarked
	}
	st.Hours = float64(int(st.Hours*10)) / 10
	return st, nil
}

type CalEvent struct {
	Date     string `json:"date"`
	Kind     string `json:"kind"` // planned | done | deadline | ends | open
	Title    string `json:"title"`
	Time     string `json:"time,omitempty"`
	At       int64  `json:"at,omitempty"`
	RoundID  int64  `json:"round_id,omitempty"`
	IdeaID   int64  `json:"idea_id,omitempty"`
	Category string `json:"category,omitempty"`
}

// Calendar lists what happens in a month: meetings, chore deadlines, events that end.
func (a *App) Calendar(ctx context.Context, c Couple, month string) ([]CalEvent, error) {
	loc := c.Loc()
	m, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil {
		n := a.now().In(loc)
		m = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	}
	from, to := m, m.AddDate(0, 1, 0)
	in := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }
	var out []CalEvent
	rs, err := a.S.RecentRounds(ctx, c.Space.ID, 500)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		if r.StartsAt.IsZero() || !in(r.StartsAt.In(loc)) || (r.State != domain.StateDone && r.State != domain.StatePlanned && r.State != domain.StateConfirm) {
			continue
		}
		i, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
		kind := "planned"
		if r.State == domain.StateDone {
			kind = "done"
		} else if r.State == domain.StateConfirm {
			kind = "open"
		}
		t := r.StartsAt.In(loc)
		out = append(out, CalEvent{Date: t.Format("2006-01-02"), Kind: kind, Title: i.Title, Time: t.Format("15:04"), At: t.Unix(), RoundID: r.ID, IdeaID: i.ID, Category: i.Category})
	}
	ideas, _ := a.S.Ideas(ctx, c.Space.ID, domain.IdeaActive)
	for _, i := range ideas {
		if i.Kind == domain.KindChore && i.Deadline != nil && in(i.Deadline.In(loc)) {
			out = append(out, CalEvent{Date: i.Deadline.In(loc).Format("2006-01-02"), Kind: "deadline", Title: i.Title, IdeaID: i.ID, Category: i.Category, At: i.Deadline.Unix()})
		}
		if i.Kind == domain.KindLeisure && i.EndsAt != nil && in(i.EndsAt.In(loc)) {
			out = append(out, CalEvent{Date: i.EndsAt.In(loc).Format("2006-01-02"), Kind: "ends", Title: i.Title, IdeaID: i.ID, Category: i.Category, At: i.EndsAt.Unix()})
		}
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].Date != out[y].Date {
			return out[x].Date < out[y].Date
		}
		return out[x].At < out[y].At
	})
	return out, nil
}

// ChoreDone marks a chore done (it leaves the list but stays in history).
func (a *App) ChoreDone(ctx context.Context, c Couple, id int64) (store.Idea, error) {
	i, err := a.S.Idea(ctx, c.Space.ID, id)
	if err != nil {
		return i, err
	}
	i.Status = domain.IdeaDone
	if err := a.S.UpdateIdea(ctx, i, a.now()); err != nil {
		return i, err
	}
	if c.Partner != nil {
		a.sendQuiet(ctx, c.Partner.ChatID, esc(c.Me.Name)+" "+Past(c.Me.Name, "сделал")+": <b>"+esc(i.Title)+"</b> ✓", nil)
	}
	return i, nil
}
