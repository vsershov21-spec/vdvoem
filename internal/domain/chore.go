package domain

import (
	"sort"
	"time"
)

// A chore lives in the «Дела» tab and nowhere else. Nothing disappears silently: every state has a place.
//
//	open      — in «Сегодня» / «Позже» (or «Просрочено»); done with a tick
//	ask       — the author asked the partner (or passed it on): waits for «Беру» / «Не могу»
//	declined  — the partner said «Не могу»: it is back with the author, who takes it or leaves it to anyone
//	done      — at the bottom of the tab for a week (the couple's setting), then in the profile's history
//	deleted   — nowhere; «Вернуть» for a few seconds
type ChoreState string

const (
	ChoreOpen     ChoreState = "open"
	ChoreAsk      ChoreState = "ask"
	ChoreDeclined ChoreState = "declined"
	ChoreDone     ChoreState = "done"
	ChoreDeleted  ChoreState = "deleted"
)

const (
	RepeatNone  = ""
	RepeatWeek  = "week"
	RepeatMonth = "month"
)

type Chore struct {
	ID, SpaceID, AuthorID int64
	Assignee              int64 // 0 — anyone
	Title, Note           string
	Due                   *time.Time
	DueTime               bool // the deadline has a time of day, not only a date
	Repeat                string
	State                 ChoreState
	ProposalID            int64 // the open request about it, while State is ask
	DoneBy                int64
	DoneAt                *time.Time
	CreatedAt             time.Time
}

func ValidRepeat(r string) bool { return r == RepeatNone || r == RepeatWeek || r == RepeatMonth }

func dayStart(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// Overdue: the deadline has passed and there is no tick. A date without a time lasts until the end of its day.
func (c Chore) Overdue(now time.Time, loc *time.Location) bool {
	if c.State != ChoreOpen || c.Due == nil {
		return false
	}
	if c.DueTime {
		return now.After(*c.Due)
	}
	return !now.Before(dayStart(*c.Due, loc).AddDate(0, 0, 1))
}

// Today: due on the reader's current day (and not overdue)
func (c Chore) Today(now time.Time, loc *time.Location) bool {
	return c.State == ChoreOpen && c.Due != nil && dayStart(*c.Due, loc).Equal(dayStart(now, loc)) && !c.Overdue(now, loc)
}

// Section of the tab the chore sits in
func (c Chore) Section(now time.Time, loc *time.Location) string {
	switch {
	case c.State == ChoreAsk || c.State == ChoreDeclined:
		return "ask"
	case c.State == ChoreDone:
		return "done"
	case c.State != ChoreOpen:
		return ""
	case c.Overdue(now, loc):
		return "overdue"
	case c.Today(now, loc):
		return "today"
	}
	return "later"
}

// InTab: a done chore stays in the tab for keepDays, then only the history has it
func (c Chore) InTab(now time.Time, keepDays int) bool {
	if c.State == ChoreDeleted {
		return false
	}
	if c.State != ChoreDone {
		return true
	}
	return c.DoneAt != nil && now.Before(c.DoneAt.AddDate(0, 0, keepDays))
}

// CanTick: mine or anyone's. The partner's chore is first taken over («Забрать себе»), never ticked for them.
func (c Chore) CanTick(me int64) bool {
	return c.State == ChoreOpen && (c.Assignee == me || c.Assignee == 0)
}

// Actions: the buttons of the card, by whose chore it is
func (c Chore) Actions(me int64, now time.Time, loc *time.Location) []string {
	var a []string
	switch c.State {
	case ChoreDone:
		return []string{"reopen"}
	case ChoreAsk:
		if c.AuthorID == me {
			return []string{"withdraw"}
		}
		return []string{"take", "decline"}
	case ChoreDeclined:
		if c.AuthorID == me {
			return []string{"self", "any"}
		}
		return nil
	case ChoreOpen:
		switch c.Assignee {
		case me:
			a = []string{"done", "pass"}
		case 0:
			a = []string{"take", "done"}
		default:
			a = []string{"nudge", "take"}
		}
		if c.Overdue(now, loc) {
			a = append(a, "move")
		}
	}
	return a
}

// NextDue: when the next occurrence of a repeating chore is due (nil — it does not repeat or has no date).
// «Каждый месяц» keeps the day of the month; the 31st in a short month becomes its last day.
func (c Chore) NextDue(loc *time.Location) *time.Time {
	if c.Due == nil || c.Repeat == RepeatNone {
		return nil
	}
	d := c.Due.In(loc)
	var n time.Time
	switch c.Repeat {
	case RepeatWeek:
		n = d.AddDate(0, 0, 7)
	case RepeatMonth:
		first := time.Date(d.Year(), d.Month()+1, 1, d.Hour(), d.Minute(), 0, 0, loc)
		last := first.AddDate(0, 1, -1).Day()
		day := d.Day()
		if day > last {
			day = last
		}
		n = time.Date(first.Year(), first.Month(), day, d.Hour(), d.Minute(), 0, 0, loc)
	default:
		return nil
	}
	return &n
}

// ChoreSummary: the one line on the home screen — what is mine (or anyone's) today and what is overdue
type ChoreSummary struct {
	Today   int      `json:"today"`
	Overdue int      `json:"overdue"`
	Titles  []string `json:"titles"`
}

func SummarizeChores(cs []Chore, me int64, now time.Time, loc *time.Location) ChoreSummary {
	s := ChoreSummary{Titles: []string{}}
	var over, today []string
	for _, c := range cs {
		if c.State != ChoreOpen || (c.Assignee != me && c.Assignee != 0) {
			continue
		}
		switch {
		case c.Overdue(now, loc):
			s.Overdue++
			over = append(over, c.Title)
		case c.Today(now, loc):
			s.Today++
			today = append(today, c.Title)
		}
	}
	s.Titles = append(append(s.Titles, over...), today...)
	return s
}

// SortChores: inside a section — the nearest deadline first, chores without a date last, then the oldest
func SortChores(cs []Chore) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		switch {
		case a.Due != nil && b.Due != nil && !a.Due.Equal(*b.Due):
			return a.Due.Before(*b.Due)
		case (a.Due == nil) != (b.Due == nil):
			return a.Due != nil
		}
		return a.ID < b.ID
	})
}
