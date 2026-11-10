// Package domain holds the rules of «Вдвоём»: who may do what, how three ideas are picked,
// when a round moves on. Pure Go, no I/O — everything here is tested with tables.
package domain

import (
	"fmt"
	"time"
)

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

// Slot is a part of a day: 0 morning (09–13), 1 day (13–18), 2 evening (18–23), 3 night (23–03 of the next day).
// At, if set, narrows it: «from 14:00» inside the day part (minutes from the day's midnight, so night hours run past 24·60).
type Slot struct {
	Day  time.Time // date at 00:00 in the space's timezone
	Part int
	At   int `json:",omitempty"`
}

const Parts = 4

var PartBounds = [Parts][2]int{{9, 13}, {13, 18}, {18, 23}, {23, 27}}

func (s Slot) Start(loc *time.Location) time.Time {
	y, m, d := s.Day.Date()
	if s.At > 0 {
		return time.Date(y, m, d, 0, s.At, 0, 0, loc)
	}
	return time.Date(y, m, d, PartBounds[s.Part][0], 0, 0, 0, loc)
}

func (s Slot) End(loc *time.Location) time.Time {
	y, m, d := s.Day.Date()
	return time.Date(y, m, d, PartBounds[s.Part][1], 0, 0, 0, loc)
}

// Key names the part of the day (two people match on it); Code adds the exact time when there is one: «2026-11-14:1@14:00».
func (s Slot) Key() string { return s.Day.Format("2006-01-02") + ":" + string(rune('0'+s.Part)) }

func (s Slot) Code() string {
	if s.At <= 0 {
		return s.Key()
	}
	return fmt.Sprintf("%s@%02d:%02d", s.Key(), s.At/60%24, s.At%60)
}

// ParseAt reads «14:00» for this part: minutes from midnight, or 0 when it does not fit (at least 30 minutes before the part ends)
func ParseAt(part int, hm string) int {
	var h, m int
	if part < 0 || part >= Parts {
		return 0
	}
	if n, _ := fmt.Sscanf(hm, "%d:%d", &h, &m); n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0
	}
	if h < PartBounds[part][0] && PartBounds[part][1] > 24 {
		h += 24 // night: 01:00 is the day after
	}
	at := h*60 + m
	if at < PartBounds[part][0]*60 || at > PartBounds[part][1]*60-30 {
		return 0
	}
	if at == PartBounds[part][0]*60 {
		return 0 // the part's own start: nothing to narrow
	}
	return at
}

func (s Slot) Minutes() int { return int(s.End(time.UTC).Sub(s.Start(time.UTC)).Minutes()) }
