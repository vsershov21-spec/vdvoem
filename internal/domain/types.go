// Package domain holds the rules of «Вдвоём»: who may do what, how three ideas are picked,
// when a round moves on. Pure Go, no I/O — everything here is tested with tables.
package domain

import "time"

type Wish string

const (
	WishNone Wish = ""
	WishSkip Wish = "skip"
	WishWant Wish = "want"
	WishVery Wish = "very"
)

func (w Wish) Valid() bool { return w == WishNone || w == WishSkip || w == WishWant || w == WishVery }

type IdeaKind string

const (
	KindLeisure IdeaKind = "leisure"
	KindChore   IdeaKind = "chore"
)

type IdeaStatus string

const (
	IdeaDraft    IdeaStatus = "draft"
	IdeaActive   IdeaStatus = "active"
	IdeaDone     IdeaStatus = "done"
	IdeaArchived IdeaStatus = "archived"
)

// Idea is one entry of the couple's piggy bank. Money is in kopecks, durations in minutes.
type Idea struct {
	ID          int64
	SpaceID     int64
	AuthorID    int64
	Title       string
	Kind        IdeaKind
	Category    string // food, culture, walk, sport, home, other
	URL         string
	Address     string
	ImageURL    string
	PriceMin    int64 // kopecks for two; 0 = unknown/free
	PriceMax    int64
	DurationMin int
	StartsAt    *time.Time // events: when it starts being available
	EndsAt      *time.Time // events: last day
	Deadline    *time.Time // chores
	Assignee    string     // chores: "a", "b", "both"
	Status      IdeaStatus
	Fingerprint string
	CreatedAt   time.Time
	AgainYes    int  // answers «Повторим? Да» after past meetings
	AgainNo     int  // answers «Нет»
	EverShown   bool // has been in any triple
	LastMetAt   *time.Time
	// Wishes by member color: "a" and "b".
	WishA, WishB Wish
}

// Slot is a part of a day: 0 morning (09–13), 1 day (13–18), 2 evening (18–23).
type Slot struct {
	Day  time.Time // date at 00:00 in the space's timezone
	Part int
}

var PartBounds = [3][2]int{{9, 13}, {13, 18}, {18, 23}}

func (s Slot) Start(loc *time.Location) time.Time {
	y, m, d := s.Day.Date()
	return time.Date(y, m, d, PartBounds[s.Part][0], 0, 0, 0, loc)
}

func (s Slot) End(loc *time.Location) time.Time {
	y, m, d := s.Day.Date()
	return time.Date(y, m, d, PartBounds[s.Part][1], 0, 0, 0, loc)
}

func (s Slot) Key() string { return s.Day.Format("2006-01-02") + ":" + string(rune('0'+s.Part)) }

func (s Slot) Minutes() int { return (PartBounds[s.Part][1] - PartBounds[s.Part][0]) * 60 }
